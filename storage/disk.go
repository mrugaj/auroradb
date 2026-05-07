// / file: storage/disk.go
package storage

import (
	"auroradb/util"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

type mmap struct {
	total  int
	chunks [][]byte
}

type KV struct {
	Path string
	fd   int
	tree BTree
	mmap mmap

	page struct {
		flushed uint64
		nappend uint64
		updates map[uint64][]byte
	}

	free struct {
		headPage uint64
		headSeq  uint64
		tailPage uint64
		tailSeq  uint64
		maxSeq   uint64
	}

	failed bool
}

const DB_SIG = "BuildYourOwnDB06"

func readRoot(db *KV, fileSize int64) error {
	if fileSize == 0 {
		db.page.flushed = 2
		db.free.headPage = 1
		db.free.tailPage = 1
		db.free.headSeq = 0
		db.free.tailSeq = 0
		db.free.maxSeq = 0
		return nil
	}

	loadMeta(db, db.mmap.chunks[0])
	return nil
}

func loadMeta(db *KV, data []byte) {
	if len(data) < 96 {
		panic("database file is corrupted: file size too small for meta page")
	}

	sig := data[:16]
	if string(sig) != DB_SIG {
		panic("invalid database file signature")
	}

	db.tree.Root = binary.LittleEndian.Uint64(data[16:24])
	db.page.flushed = binary.LittleEndian.Uint64(data[24:32])
	db.free.headPage = binary.LittleEndian.Uint64(data[32:40])
	db.free.headSeq = binary.LittleEndian.Uint64(data[40:48])
	db.free.tailPage = binary.LittleEndian.Uint64(data[48:56])
	db.free.tailSeq = binary.LittleEndian.Uint64(data[56:64])
	db.free.maxSeq = binary.LittleEndian.Uint64(data[64:72])
}

func saveMeta(db *KV) []byte {
	var data [96]byte
	copy(data[:16], []byte(DB_SIG))
	binary.LittleEndian.PutUint64(data[16:], db.tree.Root)
	binary.LittleEndian.PutUint64(data[24:], db.page.flushed)
	binary.LittleEndian.PutUint64(data[32:], db.free.headPage)
	binary.LittleEndian.PutUint64(data[40:], db.free.headSeq)
	binary.LittleEndian.PutUint64(data[48:], db.free.tailPage)
	binary.LittleEndian.PutUint64(data[56:], db.free.tailSeq)
	binary.LittleEndian.PutUint64(data[64:], db.free.maxSeq)
	return data[:]
}

func (db *KV) Open() error {
	fd, err := createFileSync(db.Path)
	if err != nil {
		return err
	}
	db.fd = fd

	fi, err := os.Stat(db.Path)
	if err != nil {
		return err
	}
	fileSize := fi.Size()

	if err := extendMmap(db, int(fileSize)); err != nil {
		return err
	}

	db.page.updates = make(map[uint64][]byte)
	db.tree.get = db.pageRead
	db.tree.new = db.pageAlloc
	db.tree.del = db.freePushTail

	if err := readRoot(db, fileSize); err != nil {
		return err
	}

	return nil
}

func (db *KV) Close() {
	if db.fd != 0 {
		syscall.Close(db.fd)
	}
	for _, chunk := range db.mmap.chunks {
		syscall.Munmap(chunk)
	}
}

func (db *KV) pageRead(ptr uint64) []byte {
	if node, ok := db.page.updates[ptr]; ok {
		return node
	}

	start := uint64(0)
	for _, chunk := range db.mmap.chunks {
		end := start + uint64(len(chunk))/PAGE_SIZE
		if ptr < end {
			offset := PAGE_SIZE * (ptr - start)
			return chunk[offset : offset+PAGE_SIZE]
		}
		start = end
	}
	panic(fmt.Sprintf("bad page pointer: %d", ptr))
}

func (db *KV) pageAlloc(node []byte) uint64 {
	ptr, head := db.freePopHead()
	if head != 0 {
		db.freePushTail(head)
	}
	if ptr != 0 {
		db.page.updates[ptr] = node
		return ptr
	}
	return db.pageAppend(node)
}

func (db *KV) pageAppend(node []byte) uint64 {
	ptr := db.page.flushed + db.page.nappend
	db.page.nappend++
	db.page.updates[ptr] = node
	return ptr
}

func (db *KV) pageWrite(ptr uint64) []byte {
	if node, ok := db.page.updates[ptr]; ok {
		return node
	}
	newNode := make([]byte, PAGE_SIZE)
	copy(newNode, db.pageRead(ptr))
	db.page.updates[ptr] = newNode
	return newNode
}

func (db *KV) freePopHead() (uint64, uint64) {
	if db.free.headSeq > db.free.maxSeq {
		return 0, 0
	}
	if db.free.headSeq == db.free.tailSeq {
		ptr := db.free.headPage
		db.free.headPage = 0
		db.free.tailPage = 0
		db.free.tailSeq = 0
		db.free.headSeq = 0
		return ptr, 0
	}

	node := db.pageRead(db.free.headPage)
	cap := uint64((PAGE_SIZE - 8) / 8)
	idx := db.free.headSeq % cap
	ptr := binary.LittleEndian.Uint64(node[8+idx*8:])
	db.free.headSeq++

	head := uint64(0)
	if db.free.headSeq%cap == 0 {
		head = db.free.headPage
		db.free.headPage = binary.LittleEndian.Uint64(node[0:])
	}
	return ptr, head
}

func (db *KV) freePushTail(ptr uint64) {
	if db.free.tailPage == 0 {
		newNode := make([]byte, PAGE_SIZE)
		newptr := db.pageAppend(newNode)
		db.free.headPage = newptr
		db.free.tailPage = newptr
		db.free.tailSeq = 0
		db.free.headSeq = 0
		binary.LittleEndian.PutUint64(newNode[8:], ptr)
		db.free.tailSeq++
		return
	}

	node := db.pageWrite(db.free.tailPage)
	cap := uint64((PAGE_SIZE - 8) / 8)
	idx := db.free.tailSeq % cap
	binary.LittleEndian.PutUint64(node[8+idx*8:], ptr)
	db.free.tailSeq++

	if db.free.tailSeq%cap == 0 {
		next, head := db.freePopHead()
		if next == 0 {
			next = db.pageAppend(make([]byte, PAGE_SIZE))
		}

		node = db.pageWrite(db.free.tailPage)
		binary.LittleEndian.PutUint64(node[0:], next)
		db.free.tailPage = next
		if head != 0 {
			newNode := db.pageWrite(db.free.tailPage)
			binary.LittleEndian.PutUint64(newNode[8:], head)
			db.free.tailSeq++
		}
	}
}

func createFileSync(file string) (int, error) {
	flags := os.O_RDONLY | syscall.O_DIRECTORY
	dirfd, err := syscall.Open(filepath.Dir(file), flags, 0o644)
	if err != nil {
		return -1, fmt.Errorf("open directory: %w", err)
	}
	defer syscall.Close(dirfd)

	flags = os.O_RDWR | os.O_CREATE
	fd, err := unix.Openat(dirfd, filepath.Base(file), flags, 0o644)
	if err != nil {
		return -1, fmt.Errorf("open file: %w", err)
	}

	err = syscall.Fsync(dirfd)
	if err != nil {
		_ = syscall.Close(fd)
		return -1, fmt.Errorf("fsync directory: %w", err)
	}
	return fd, nil
}

func extendMmap(db *KV, size int) error {
	if size <= db.mmap.total {
		return nil
	}

	alloc := db.mmap.total
	if alloc < 64<<20 {
		alloc = 64 << 20
	}

	for db.mmap.total+alloc < size {
		alloc *= 2
	}
	chunk, err := syscall.Mmap(
		db.fd, int64(db.mmap.total), alloc,
		syscall.PROT_READ, syscall.MAP_SHARED,
	)
	if err != nil {
		return fmt.Errorf("mmap: %w", err)
	}
	db.mmap.total += alloc
	db.mmap.chunks = append(db.mmap.chunks, chunk)
	return nil
}

func (db *KV) Begin(tx *KVTX) {
	tx.db = db
	tx.meta = saveMeta(db)
	tx.snapshot.Root = db.tree.Root
	tx.snapshot.get = db.pageRead

	tx.delSet = util.NewSet()
	tx.writeSet = util.NewSet()
	tx.readSet = util.NewSet()
	tx.writes = make(map[string]bool)
	tx.deletes = make(map[string]bool)

	pages := make(map[uint64][]byte)
	nPages := uint64(0)

	tx.pending.new = func(node []byte) uint64 {
		nPages++
		pages[nPages] = node
		return nPages
	}
	tx.pending.get = func(ptr uint64) []byte {
		if node, ok := pages[ptr]; ok {
			return node
		}
		return nil
	}
	tx.pending.del = func(u uint64) {}
}

func (db *KV) Commit(tx *KVTX) error {
	for key := range tx.writes {
		val, _ := tx.pending.GetVal([]byte(key))
		db.tree.Insert([]byte(key), val)
	}
	for key := range tx.deletes {
		db.tree.Delete([]byte(key))
	}

	err := db.updateFile()
	if err != nil {
		db.Abort(tx)
		return err
	}
	return nil
}

func (db *KV) Abort(tx *KVTX) {
	loadMeta(db, tx.meta)
	db.page.nappend = 0
	db.page.updates = make(map[uint64][]byte)
}

func (db *KV) updateFile() error {
	size := int(db.page.flushed+db.page.nappend) * PAGE_SIZE
	if err := extendMmap(db, size); err != nil {
		return err
	}

	for ptr, page := range db.page.updates {
		offset := int64(ptr * PAGE_SIZE)
		if _, err := syscall.Pwrite(db.fd, page, offset); err != nil {
			return err
		}
	}

	if err := syscall.Fsync(db.fd); err != nil {
		return err
	}

	if _, err := syscall.Pwrite(db.fd, saveMeta(db), 0); err != nil {
		return fmt.Errorf("write meta page: %w", err)
	}

	if err := syscall.Fsync(db.fd); err != nil {
		return err
	}

	db.page.flushed += db.page.nappend
	db.page.nappend = 0
	db.page.updates = make(map[uint64][]byte)
	db.free.maxSeq = db.free.tailSeq
	return nil
}

func (db *KV) Get(key []byte) ([]byte, bool) {
	val, err := db.tree.GetVal(key)
	if err != nil {
		return nil, false
	}
	return val, true
}

func (db *KV) Set(key []byte, val []byte) {
	db.tree.Insert(key, val)
}

func (db *KV) Del(key []byte) bool {
	return db.tree.Delete(key)
}

type KVTX struct {
	db       *KV
	meta     []byte
	snapshot BTree
	pending  BTree

	delSet   *util.Set
	writeSet *util.Set
	readSet  *util.Set

	writes  map[string]bool
	deletes map[string]bool
}

func NewKVTX() *KVTX {
	return &KVTX{
		delSet:   util.NewSet(),
		writeSet: util.NewSet(),
		readSet:  util.NewSet(),
		writes:   make(map[string]bool),
		deletes:  make(map[string]bool),
	}
}

func (tx *KVTX) Get(key []byte) ([]byte, error) {
	if tx.delSet.Has(key) {
		return nil, fmt.Errorf("getting deleted key")
	}
	tx.readSet.Set(key)

	val, err := tx.pending.GetVal(key)
	if err == nil {
		return val, nil
	}
	return tx.snapshot.GetVal(key)
}

func (tx *KVTX) Set(key []byte, val []byte) {
	if tx.delSet.Has(key) {
		tx.delSet.Del(key)
		delete(tx.deletes, string(key))
	}
	tx.writeSet.Set(key)
	tx.writes[string(key)] = true
	tx.pending.Insert(key, val)
}

func (tx *KVTX) Del(key []byte) bool {
	if tx.delSet.Has(key) {
		return false
	}
	tx.delSet.Set(key)
	tx.deletes[string(key)] = true
	if tx.writeSet.Has(key) {
		tx.writeSet.Del(key)
		return tx.pending.Delete(key)
	}
	return true
}

func (tx *KVTX) Seek(key []byte) *BIter {
	return tx.snapshot.SeekLE(key)
}
