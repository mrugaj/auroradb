package storage

import (
	"bytes"
	"encoding/binary"
	"log"
)

const (
	PAGE_SIZE = 4096
	HEADER    = 4
	POINTER   = 8
	OFFSET    = 2
	KLEN      = 2
	VLEN      = 2
	KVHEADER  = 4 // keylen = 2, vallen = 2
	MAXKEYLEN = 1000
	MAXVALLEN = 3000

	// header
	BNODE_NODE = 1
	BNODE_LEAF = 2

	// comparitions
	CMP_GT = 3 // >
	CMP_GE = 2 // >=
	CMP_LT = 1 // <
	CMP_LE = 0 // <=
)

// assert panics if the condition is false.
func assert(condition bool) {
	if !condition {
		panic("assertion failed!")
	}
}

// | type (2B) | nkeys (2B) | pointers | offsets | key-values |

type BTree struct {
	Root uint64 // pointer to the page

	//callbacks
	get func(uint64) []byte // get the page
	new func([]byte) uint64 // allocate a new page
	del func(uint64)        // delete a page
}

// BIter is used to iterate though the leaf nodes
type BIter struct {
	tree  *BTree  // pointer to Btree
	path  []BNode // slice of nodes that from root to leaf both inclusive
	pos   []int32 // it indicates the index of child node
	valid bool
}

func init() {
	// size of largest possible node
	node1max := HEADER + POINTER + OFFSET + (KVHEADER + MAXKEYLEN + MAXVALLEN)

	// check if the max node fits in a page
	if node1max > PAGE_SIZE {
		log.Fatalln("size of node exceeds the size of page")
	}
}

// BNode is just a raw slice of bytes.
type BNode []byte

// Hence allowing us to dump it directly to disk without serialization
// format used to store bytes is LittleEndian

// Used to determine if it is a leaf node or internal
func (b BNode) btype() uint16 {
	return binary.LittleEndian.Uint16(b[0:2])
}

// Returns number of keys in the node
func (b BNode) nkeys() uint16 {
	return binary.LittleEndian.Uint16(b[2:4])
}

// getPtr extracts the child pointer at the specific index
func (b BNode) getptr(idx uint16) uint64 {
	if idx >= b.nkeys() {
		log.Fatalf("Out of bounds whilst fetching pointer, index: %d, nkeys: %d", idx, b.nkeys())
	}
	sbyte := HEADER + (idx * 8)
	return binary.LittleEndian.Uint64(b[sbyte:])
}

// setptr set the poiter at a given index to the specified value
func (b BNode) setptr(idx uint16, ptr uint64) {
	if idx >= b.nkeys() {
		log.Fatalf("Pointer set out of bounds , index: %d, nkeys: %d", idx, b.nkeys())
	}
	sbyte := HEADER + (idx * 8)
	binary.LittleEndian.PutUint64(b[sbyte:], ptr)
}

// offsetPos calculates where the offset for a given KV pair is stored
func (b BNode) offsetPos(idx uint16) uint16 {
	if idx > b.nkeys() {
		log.Fatalf("out of bounds, idx: %d, nkeys: %d", idx, b.nkeys())
	}

	sbyte := HEADER + (b.nkeys() * POINTER) + ((idx - 1) * OFFSET)
	return sbyte
}

// getOffset returns offset of a kv parir relative to the 1st kv pair
func (b BNode) getOffset(idx uint16) uint16 {
	if idx == 0 {
		return 0 // first offset always starts at 0
	}
	return binary.LittleEndian.Uint16(b[b.offsetPos(idx):])
}
func (b BNode) setOffset(idx uint16, val uint16) {
	if idx > b.nkeys() {
		log.Fatalf("out of bounds setting offset, idx: %d, nkeys: %d", idx, b.nkeys())
	}
	sbyte := b.offsetPos(idx)
	binary.LittleEndian.PutUint16(b[sbyte:], val)
}

// setHeader configures the first 4 bytes of the node
func (node BNode) setHeader(btype uint16, nkeys uint16) {
	binary.LittleEndian.PutUint16(node[0:2], btype)
	binary.LittleEndian.PutUint16(node[2:4], nkeys)

}

//| klen (2B) | vlen (2B) | key | val |

// kvPos calculates the exact byte position of the KV pair within the node
func (node BNode) kvPos(idx uint16) uint16 {
	// Skip header, pointers, and the offset list itself, then add the specific KV offset
	return HEADER + 8*node.nkeys() + 2*node.nkeys() + node.getOffset(idx)
}

// getKey extracts the key byte slice
func (node BNode) getKey(idx uint16) []byte {
	pos := node.kvPos(idx)
	klen := binary.LittleEndian.Uint16(node[pos:])
	afterHeader := node[pos+4:]    // Step 1: Skip the header
	exactKey := afterHeader[:klen] // Step 2: Grab only the key's letters
	return exactKey                // can be written as node[pos+4:][:klen]
}

// getVal extracts the value byte slice
func (node BNode) getVal(idx uint16) []byte {
	pos := node.kvPos(idx)
	klen := binary.LittleEndian.Uint16(node[pos+0:])
	vlen := binary.LittleEndian.Uint16(node[pos+2:])
	return node[pos+4+klen:][:vlen]
}

// nbytes conveniently returns the total size of the node in bytes by looking at the end of the very last KV pair
func (node BNode) nbytes() uint16 {
	return node.kvPos(node.nkeys())
}

func nodeLookupLE(node BNode, key []byte) uint16 {
	//nkeys := node.nkeys()// last key
	low := uint16(1)
	high := node.nkeys()
	found := uint16(0) // first key

	// The first key is a copy from the parent node, thus it's always less than or equal to the key.
	// Binary search...
	for low < high {

		mid := low + (high-low)/2

		// Compare
		cmp := bytes.Compare(node.getKey(mid), key)

		if cmp == 0 {
			// Exact match found
			found = mid
			break
		} else if cmp < 0 {
			// Node key is smaller than target
			found = mid
			low = mid + 1
		} else {
			// Node key is larger than target
			high = mid // prevents uint16 underflow
		}
	}
	return found
}

// nodeAppendKV copies a new kv pair into a specified position of a new node
func nodeAppendKV(new BNode, idx uint16, ptr uint64, key []byte, val []byte) {
	// set ptr
	new.setptr(idx, ptr)

	// set kv
	pos := new.kvPos(idx)

	// set kv headers
	binary.LittleEndian.PutUint16(new[pos+0:], uint16(len(key)))
	binary.LittleEndian.PutUint16(new[pos+2:], uint16(len(val)))

	// set kv data
	copy(new[pos+KVHEADER:], key)
	copy(new[pos+KVHEADER+uint16(len(key)):], val)

	// the offset of the next key
	new.setOffset(idx+1, new.getOffset(idx)+KVHEADER+uint16(len(key)+len(val)))
}

// Helper function to find exactly where the KV data starts
func (node BNode) kvStart() uint16 {
	return HEADER + POINTER*node.nkeys() + OFFSET*node.nkeys()
}

// nodeAppendRange copies multiple KVs into the position from the old node
// we dont call nodeAppendKV 'n' times as it forces the computer to do a massive amount of unnecessary work which may lead to a performance tank.
func nodeAppendRange(
	new BNode, old BNode,
	dstNew uint16, srcOld uint16, n uint16,
) {
	if dstNew+n > new.nkeys() {
		log.Fatalf("out of bounds new node, idx: %d, nkeys: %d", dstNew+n, new.nkeys())
	}
	if srcOld+n > old.nkeys() {
		log.Fatalf("out of bounds old node, idx: %d, nkeys: %d", srcOld+n, old.nkeys())
	}
	if n == 0 {
		return
	}

	// 1. Copy Pointers
	for i := range n {

		new.setptr(dstNew+uint16(i), uint64(old.getptr(srcOld+uint16(i))))
	}

	// 2. Calculate and Set Offsets
	dstBegin := new.getOffset(dstNew)
	srcBegin := old.getOffset(srcOld)
	shiftDistance := dstBegin - srcBegin

	for i := uint16(1); i <= n; i++ {
		oldOffset := old.getOffset(srcOld + i)
		new.setOffset(dstNew+i, oldOffset+shiftDistance)
	}

	// 3. Copy Raw KV Data
	oldStart := old.kvStart()
	begin := old.getOffset(srcOld) + oldStart
	end := old.getOffset(srcOld+n) + oldStart

	newStart := new.kvStart()
	newBegin := new.getOffset(dstNew) + newStart

	copy(new[newBegin:], old[begin:end])
}

// leafInsert adds a new key to a leaf node by copying the old data into a new node
func leafInsert(
	new BNode, old BNode, idx uint16,
	key []byte, val []byte,
) {
	new.setHeader(BNODE_LEAF, old.nkeys()+1) // setup the header
	nodeAppendRange(new, old, 0, 0, idx)
	nodeAppendKV(new, idx, 0, key, val)
	nodeAppendRange(new, old, idx+1, idx, old.nkeys()-idx)
}

func nodeReplaceKidN(
	tree *BTree, new BNode, old BNode, idx uint16,
	kids ...BNode,
) {
	// number of child nodes
	inc := uint16(len(kids))

	//The new node will have 'inc' new keys, minus the 1 old key we are replacing
	new.setHeader(BNODE_NODE, old.nkeys()+inc-1)

	// append kv's from 0 to given index
	nodeAppendRange(new, old, 0, 0, idx)

	// append new kv or multiple keys at give index
	for i, node := range kids {
		nodeAppendKV(new, idx+uint16(i), tree.new(node), node.getKey(0), nil)
		// ^position    ^pointer        ^key             ^val
	}
	// append kv's from index + 1 to all of the indexes
	nodeAppendRange(new, old, idx+inc, idx+1, old.nkeys()-(idx+1))
}

// Worst-case split: Inserting a max-size KV (1000B key + 3000B val) 
// into a nearly full node can force it to split into 3 separate nodes 
// to respect the 4KB page limit [cite: 790-792].
// nodeSplit2 splits an oversized node into 2. 
// Returns the median idx.
func nodeSplit2(left BNode, right BNode, old BNode)  {
	// 1. BASE CASE
    // If the node isn't actually oversized (i.e. it fits in 1 page), then we can just copy it over to the left node and return.
    if old.nbytes() <= PAGE_SIZE {
        return 
    }

    var idx uint16

	// 2. EXTREME OVERSIZE SCENARIO (> 8192 bytes if your page is 4096)
    if old.nbytes() > 2*PAGE_SIZE {
        idx = old.nkeys()											// Start at the very end of the items
        size := uint16(HEADER)										// Start counting bytes (every node needs a header)
        
        posRight := old.kvPos(idx)									// Find the exact byte where the very last item ends
        
		// Loop BACKWARDS from the end of the node
        for size < PAGE_SIZE && idx > 0 {
            posLeft := old.kvPos(idx - 1)							// Find where this specific item begins
			// Calculate how much space this item takes up:
            // (End Byte - Start Byte) + 8 bytes for the Pointer + 2 bytes for the Offset
            size += (posRight - posLeft) + POINTER + OFFSET

            posRight = posLeft 										// Move our "end" marker backwards for the next loop
            idx--													// Move to the previous item
        }
		// Because the loop stops *after* it goes one step too far, we add 1 back to idx to get the correct cut point.
        idx++

		// 3. NORMAL OVERSIZE SCENARIO (between 4096 and 8192 bytes)
    } else {
        idx = 0
        halfSize := old.nbytes() / 2	// Find the exact halfway point of the memory
        size := uint16(HEADER)       	// Start counting bytes from the header
        
        posLeft := old.kvPos(idx)		// Find where the very first item begins

   			// Loop FORWARDS from the beginning of the node
        for size < halfSize && idx < old.nkeys() {
            posRight := old.kvPos(idx + 1)// Find where this specific item ends

			// Accumulate the size of the item just like we did above
            size += (posRight - posLeft) + POINTER + OFFSET

            posLeft = posRight // Move our "start" marker forwards for the next loop
            idx++              // Move to the next item
        }
    }

  
    // 4. EXECUTE THE SPLIT
    // Now that we know exactly where the cut happens (idx), we configure the new nodes.

    left.setHeader(old.btype(), idx)               // Left node gets 'idx' number of items
    right.setHeader(old.btype(), old.nkeys()-idx)  // Right node gets whatever is leftover

    // 5. BULK COPY THE DATA

    // Paste the first half into the left node
    nodeAppendRange(left, old, 0, 0, idx)
    // Paste the second half into the right node
    nodeAppendRange(right, old, 0, idx, old.nkeys()-idx)
    
}

// nodeSplit3 splits a node if it's too big. The results are 1 to 3 nodes.
func nodeSplit3(old BNode) (uint16, [5]BNode) {
	if old.nbytes() <= PAGE_SIZE {
		old = old[:PAGE_SIZE]
		return 1, [5]BNode{old} // Not split, fits perfectly
	}
    
	left := BNode(make([]byte, 2*PAGE_SIZE)) // Might be split again later
	right := BNode(make([]byte, PAGE_SIZE))
	nodeSplit2(left, right, old)
    
	if left.nbytes() <= PAGE_SIZE {
		left = left[:PAGE_SIZE]
		return 2, [5]BNode{left, right} // Split into 2 nodes
	}
    
    // If the left node is STILL too big, split it one more time!
	leftleft := BNode(make([]byte, PAGE_SIZE))
	middle := BNode(make([]byte, PAGE_SIZE))
	nodeSplit2(leftleft, middle, left)
	assert(leftleft.nbytes() <= PAGE_SIZE)
    
	return 3, [5]BNode{leftleft, middle, right} // Split into 3 nodes
}
