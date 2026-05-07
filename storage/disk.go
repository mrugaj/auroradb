package storage

import(
	"fmt"
	"os"
	"syscall"
	"path/filepath"
	"golang.org/x/sys/unix"
	"encoding/binary"

)

// mmap holds the memory-mapped file chunks.
// We use multiple chunks because the file will grow over time!
type mmap struct {
	total  int      // total mapped size
	chunks [][]byte // mapped memory slices
}

type KV struct {
	Path string
	fd   int
	tree BTree
	mmap mmap
    
	page struct {
		flushed uint64   // total number of pages currently saved to disk
		temp    [][]byte // newly allocated pages that haven't been saved yet
	}
}

// DB_SIG is the 16-byte magic signature to identify our file format
const DB_SIG = "BuildYourOwnDB06" 

// readRoot handles the database startup sequence
func readRoot(db *KV, fileSize int64) error {
    if fileSize == 0 { 
        // SCENARIO 1: Brand new database!
        // Just reserve Page 0 for the meta page. 
        // The actual B-Tree root will be created on the very first Insert.
        db.page.flushed = 1 
        return nil
    }

    // SCENARIO 2: Existing database!
    // The meta page is ALWAYS at the very beginning of the first mapped chunk.
    // We pass only the first chunk instead of the whole 2D slice.
    loadMeta(db, db.mmap.chunks[0])
    
    return nil
}

// loadMeta parses the Meta Page (Page 0)
func loadMeta(db *KV, data []byte) {
    // 0. Safety Check: Ensure the file isn't corrupted/truncated
    if len(data) < 32 {
        panic("database file is corrupted: file size too small for meta page")
    }

    // 1. Validate the magic signature (Bytes 0-15)
    sig := data[:16]
    if string(sig) != DB_SIG { // Make sure DB_SIG is defined somewhere!
        panic("invalid database file signature")
    }

    // 2. Extract the tree root pointer (Bytes 16-23)
    db.tree.Root = binary.LittleEndian.Uint64(data[16:24])

    // 3. Extract the total number of flushed pages (Bytes 24-31)
    db.page.flushed = binary.LittleEndian.Uint64(data[24:32])
}

// Open initializes the database, opens the file, and wires up the B-Tree callbacks.
func (db *KV) Open() error {
    // 1. Safely open the file
    fd, err := createFileSync(db.Path)
    if err != nil {
        return err
    }
    db.fd = fd

    // Check the initial file size
    fi, err := os.Stat(db.Path)
    if err != nil {
        return err
    }
    fileSize := fi.Size()

    // 2. Map the file into memory
    if err := extendMmap(db, int(fileSize)); err != nil {
        return err
    }

    // 3. Wire up the B-Tree callbacks to our physical page functions
    db.tree.get = db.pageRead
    db.tree.new = db.pageAppend
    db.tree.del = func(uint64) {} // Ignored for now

    // 4. Read the Meta Page to find the root!
    if err := readRoot(db, fileSize); err != nil {
        return err
    }
    
    return nil
}

// pageRead acts as `BTree.get`. It translates a page number (ptr) into a memory slice.
func (db *KV) pageRead(ptr uint64) []byte {
	start := uint64(0)
	for _, chunk := range db.mmap.chunks {
		end := start + uint64(len(chunk))/PAGE_SIZE
		if ptr < end {
			offset := PAGE_SIZE * (ptr - start)
			return chunk[offset : offset+PAGE_SIZE]
		}
		start = end
	}
	panic("bad ptr")
}

// pageAppend acts as `BTree.new`. It temporarily holds new pages in memory.
func (db *KV) pageAppend(node []byte) uint64 {
	ptr := db.page.flushed + uint64(len(db.page.temp))
	db.page.temp = append(db.page.temp, node)
	return ptr
}

// createFileSync opens or creates a file and fsyncs the parent directory to guarantee durability.
func createFileSync(file string) (int, error) {
	
	// 1. Obtain the directory file descriptor
	flags := os.O_RDONLY | syscall.O_DIRECTORY
	dirfd, err := syscall.Open(filepath.Dir(file), flags, 0o644)
	if err != nil {
		return -1, fmt.Errorf("open directory: %w", err)
	}
	defer syscall.Close(dirfd)

	// 2. Open or create the target file using Openat
	flags = os.O_RDWR | os.O_CREATE
	fd, err := unix.Openat(dirfd, filepath.Base(file), flags, 0o644)
	if err != nil {
		return -1, fmt.Errorf("open file: %w", err)
	}

	// 3. Fsync the directory
	err = syscall.Fsync(dirfd);
	if err != nil {
		_ = syscall.Close(fd) // Clean up if the directory sync fails
		return -1, fmt.Errorf("fsync directory: %w", err)
	}

	return fd, nil
}

// extendMmap expands the mapped memory to cover the growing file.
func extendMmap(db *KV, size int) error {
	if size <= db.mmap.total {
		return nil // We already have enough mapped space
	}

	// Double the address space to avoid mapping too often.
	// Minimum map size is 64MB (64 << 20).
	alloc := max(db.mmap.total, 64<<20)

	for db.mmap.total+alloc < size {
		alloc *= 2 
	}

	// Call the mmap system call
	chunk, err := syscall.Mmap(
		db.fd, int64(db.mmap.total), alloc,
		syscall.PROT_READ, syscall.MAP_SHARED, // Read-only mapping
	)
	if err != nil {
		return fmt.Errorf("mmap: %w", err)
	}

	db.mmap.total += alloc
	db.mmap.chunks = append(db.mmap.chunks, chunk)
	return nil
}

