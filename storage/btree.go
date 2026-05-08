package storage

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
)

const (
	PAGE_SIZE = 4096
	HEADER    = 4
	POINTER   = 8
	OFFSET    = 2
	KLEN      = 2
	VLEN      = 2
	KVHEADER  = 4
	MAXKEYLEN = 1000
	MAXVALLEN = 3000

	BNODE_NODE = 1
	BNODE_LEAF = 2

	CMP_GT = 3
	CMP_GE = 2
	CMP_LT = 1
	CMP_LE = 0
)

func assert(condition bool) {
	if !condition {
		panic("assertion failed!")
	}
}

type BTree struct {
	Root uint64
	get  func(uint64) []byte
	new  func([]byte) uint64
	del  func(uint64)
}

type BIter struct {
	tree  *BTree
	path  []BNode
	pos   []int32
	valid bool
}

func init() {
	node1max := HEADER + POINTER + OFFSET + (KVHEADER + MAXKEYLEN + MAXVALLEN)
	if node1max > PAGE_SIZE {
		log.Fatalln("size of node exceeds the size of page")
	}
}

type BNode []byte

func (b BNode) btype() uint16 {
	return binary.LittleEndian.Uint16(b[0:2])
}

func (b BNode) nkeys() uint16 {
	return binary.LittleEndian.Uint16(b[2:4])
}

func (b BNode) getptr(idx uint16) uint64 {
	if idx >= b.nkeys() {
		log.Fatalf("Out of bounds whilst fetching pointer, index: %d, nkeys: %d", idx, b.nkeys())
	}
	sbyte := HEADER + (idx * 8)
	return binary.LittleEndian.Uint64(b[sbyte:])
}

func (b BNode) setptr(idx uint16, ptr uint64) {
	if idx >= b.nkeys() {
		log.Fatalf("Pointer set out of bounds, index: %d, nkeys: %d", idx, b.nkeys())
	}
	sbyte := HEADER + (idx * 8)
	binary.LittleEndian.PutUint64(b[sbyte:], ptr)
}

func (b BNode) offsetPos(idx uint16) uint16 {
	if idx > b.nkeys() {
		log.Fatalf("out of bounds, idx: %d, nkeys: %d", idx, b.nkeys())
	}
	sbyte := HEADER + (b.nkeys() * POINTER) + ((idx - 1) * OFFSET)
	return sbyte
}

func (b BNode) getOffset(idx uint16) uint16 {
	if idx == 0 {
		return 0
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

func (node BNode) setHeader(btype uint16, nkeys uint16) {
	binary.LittleEndian.PutUint16(node[0:2], btype)
	binary.LittleEndian.PutUint16(node[2:4], nkeys)
}

func (node BNode) kvPos(idx uint16) uint16 {
	return HEADER + 8*node.nkeys() + 2*node.nkeys() + node.getOffset(idx)
}

func (node BNode) getKey(idx uint16) []byte {
	pos := node.kvPos(idx)
	klen := binary.LittleEndian.Uint16(node[pos:])
	return node[pos+4:][:klen]
}

func (node BNode) getVal(idx uint16) []byte {
	pos := node.kvPos(idx)
	klen := binary.LittleEndian.Uint16(node[pos+0:])
	vlen := binary.LittleEndian.Uint16(node[pos+2:])
	return node[pos+4+klen:][:vlen]
}

func (node BNode) nbytes() uint16 {
	return node.kvPos(node.nkeys())
}

func nodeLookupLE(node BNode, key []byte) uint16 {
	low := uint16(1)
	high := node.nkeys()
	found := uint16(0)

	for low < high {
		mid := low + (high-low)/2
		cmp := bytes.Compare(node.getKey(mid), key)

		if cmp == 0 {
			found = mid
			break
		} else if cmp < 0 {
			found = mid
			low = mid + 1
		} else {
			high = mid
		}
	}
	return found
}

func nodeAppendKV(new BNode, idx uint16, ptr uint64, key []byte, val []byte) {
	new.setptr(idx, ptr)
	pos := new.kvPos(idx)

	binary.LittleEndian.PutUint16(new[pos+0:], uint16(len(key)))
	binary.LittleEndian.PutUint16(new[pos+2:], uint16(len(val)))

	copy(new[pos+KVHEADER:], key)
	copy(new[pos+KVHEADER+uint16(len(key)):], val)

	new.setOffset(idx+1, new.getOffset(idx)+KVHEADER+uint16(len(key)+len(val)))
}

func (node BNode) kvStart() uint16 {
	return HEADER + POINTER*node.nkeys() + OFFSET*node.nkeys()
}

func nodeAppendRange(new BNode, old BNode, dstNew uint16, srcOld uint16, n uint16) {
	if dstNew+n > new.nkeys() {
		log.Fatalf("out of bounds new node, idx: %d, nkeys: %d", dstNew+n, new.nkeys())
	}
	if srcOld+n > old.nkeys() {
		log.Fatalf("out of bounds old node, idx: %d, nkeys: %d", srcOld+n, old.nkeys())
	}
	if n == 0 {
		return
	}

	for i := range n {
		new.setptr(dstNew+uint16(i), uint64(old.getptr(srcOld+uint16(i))))
	}

	dstBegin := new.getOffset(dstNew)
	srcBegin := old.getOffset(srcOld)
	shiftDistance := dstBegin - srcBegin

	for i := uint16(1); i <= n; i++ {
		oldOffset := old.getOffset(srcOld + i)
		new.setOffset(dstNew+i, oldOffset+shiftDistance)
	}

	oldStart := old.kvStart()
	begin := old.getOffset(srcOld) + oldStart
	end := old.getOffset(srcOld+n) + oldStart

	newStart := new.kvStart()
	newBegin := new.getOffset(dstNew) + newStart

	copy(new[newBegin:], old[begin:end])
}

func leafInsert(new BNode, old BNode, idx uint16, key []byte, val []byte) {
	new.setHeader(BNODE_LEAF, old.nkeys()+1)
	nodeAppendRange(new, old, 0, 0, idx)
	nodeAppendKV(new, idx, 0, key, val)
	nodeAppendRange(new, old, idx+1, idx, old.nkeys()-idx)
}

func leafUpdate(new BNode, old BNode, idx uint16, key []byte, val []byte) {
	new.setHeader(old.btype(), old.nkeys())
	nodeAppendRange(new, old, 0, 0, idx)
	nodeAppendKV(new, idx, 0, key, val)
	nodeAppendRange(new, old, idx+1, idx+1, old.nkeys()-(idx+1))
}

func leafDelete(new BNode, old BNode, idx uint16) {
	new.setHeader(BNODE_LEAF, old.nkeys()-1)
	nodeAppendRange(new, old, 0, 0, idx)
	nodeAppendRange(new, old, idx, idx+1, old.nkeys()-idx-1)
}

func nodeReplaceKidN(tree *BTree, new BNode, old BNode, idx uint16, kids ...BNode) {
	inc := uint16(len(kids))
	new.setHeader(BNODE_NODE, old.nkeys()+inc-1)

	nodeAppendRange(new, old, 0, 0, idx)

	for i, kidNode := range kids {
		nodeAppendKV(new, idx+uint16(i), tree.new(kidNode), kidNode.getKey(0), nil)
	}
	nodeAppendRange(new, old, idx+inc, idx+1, old.nkeys()-(idx+1))
}

func nodeReplace2Kid(new BNode, old BNode, idx uint16, mergedPtr uint64, key []byte) {
	new.setHeader(old.btype(), old.nkeys()-1)
	nodeAppendRange(new, old, 0, 0, idx)
	nodeAppendKV(new, idx, mergedPtr, key, nil)
	nodeAppendRange(new, old, idx+1, idx+2, old.nkeys()-(idx+2))
}

func nodeMerge(new, left, right BNode) {
	new.setHeader(left.btype(), left.nkeys()+right.nkeys())
	nodeAppendRange(new, left, 0, 0, left.nkeys())
	nodeAppendRange(new, right, left.nkeys(), 0, right.nkeys())
}

func nodeSplit2(left BNode, right BNode, old BNode) uint16 {
	if old.nbytes() <= PAGE_SIZE {
		return 0
	}

	if old.nbytes() > 2*PAGE_SIZE {
		idx := old.nkeys()
		size := uint16(HEADER)

		for size < PAGE_SIZE && idx > 0 {
			posLeft := old.kvPos(idx - 1)
			posRight := old.kvPos(idx)
			size += (posRight - posLeft) + POINTER + OFFSET
			idx--
		}
		idx++

		left.setHeader(old.btype(), idx)
		right.setHeader(old.btype(), old.nkeys()-idx)

		nodeAppendRange(left, old, 0, 0, idx)
		nodeAppendRange(right, old, 0, idx, old.nkeys()-idx)
		return idx
	} else {
		idx := uint16(0)
		halfSize := old.nbytes() / 2
		size := uint16(HEADER)

		for size < halfSize && idx < old.nkeys() {
			posRight := old.kvPos(idx + 1)
			posLeft := old.kvPos(idx)
			size += (posRight - posLeft) + POINTER + OFFSET
			idx++
		}

		left.setHeader(old.btype(), idx)
		right.setHeader(old.btype(), old.nkeys()-idx)

		nodeAppendRange(left, old, 0, 0, idx)
		nodeAppendRange(right, old, 0, idx, old.nkeys()-idx)
		return idx
	}
}

func nodeSplit3(old BNode) (uint16, [3]BNode) {
	if old.nbytes() <= PAGE_SIZE {
		old = old[:PAGE_SIZE]
		return 1, [3]BNode{old}
	}

	left := BNode(make([]byte, 2*PAGE_SIZE))
	right := BNode(make([]byte, PAGE_SIZE))
	_ = nodeSplit2(left, right, old)

	if left.nbytes() <= PAGE_SIZE {
		left = left[:PAGE_SIZE]
		return 2, [3]BNode{left, right}
	}

	leftleft := BNode(make([]byte, PAGE_SIZE))
	middle := BNode(make([]byte, PAGE_SIZE))
	_ = nodeSplit2(leftleft, middle, left)
	assert(leftleft.nbytes() <= PAGE_SIZE)

	return 3, [3]BNode{leftleft, middle, right}
}

func shouldMerge(tree *BTree, node BNode, idx uint16, updated BNode) (int, BNode) {
	if updated.nbytes() > PAGE_SIZE/4 {
		return 0, BNode{}
	}

	if idx > 0 {
		leftSib := BNode(tree.get(node.getptr(idx - 1)))
		merged := leftSib.nbytes() + updated.nbytes() - HEADER
		if merged <= PAGE_SIZE {
			return -1, leftSib
		}
	}

	if idx+1 < node.nkeys() {
		rightSib := BNode(tree.get(node.getptr(idx + 1)))
		merged := rightSib.nbytes() + updated.nbytes() - HEADER
		if merged <= PAGE_SIZE {
			return 1, rightSib
		}
	}

	return 0, BNode{}
}

func treeInsert(tree *BTree, node BNode, key []byte, val []byte) BNode {
	new := BNode(make([]byte, 2*PAGE_SIZE))
	idx := nodeLookupLE(node, key)

	switch node.btype() {
	case BNODE_LEAF:
		if bytes.Equal(key, node.getKey(idx)) {
			leafUpdate(new, node, idx, key, val)
		} else {
			if cmp := bytes.Compare(key, node.getKey(0)); cmp < 0 && idx == 0 {
				new.setHeader(BNODE_LEAF, node.nkeys()+1)
				nodeAppendKV(new, 0, 0, key, val)
				nodeAppendRange(new, node, 1, 0, node.nkeys())
			} else {
				leafInsert(new, node, idx+1, key, val)
			}
		}
	case BNODE_NODE:
		nodeInsert(tree, new, node, idx, key, val)
	default:
		log.Panicln("invalid node header(bad node)!")
	}
	return new
}

func nodeInsert(tree *BTree, new BNode, node BNode, idx uint16, key []byte, val []byte) {
	kptr := node.getptr(idx)
	knode := treeInsert(tree, tree.get(kptr), key, val)
	nsplit, split := nodeSplit3(knode)
	tree.del(kptr)
	nodeReplaceKidN(tree, new, node, idx, split[:nsplit]...)
}

func treeDelete(tree *BTree, node BNode, key []byte) BNode {
	idx := nodeLookupLE(node, key)

	switch node.btype() {
	case BNODE_LEAF:
		if !bytes.Equal(key, node.getKey(idx)) {
			return BNode{}
		}
		new := BNode(make([]byte, PAGE_SIZE))
		leafDelete(new, node, idx)
		return new
	case BNODE_NODE:
		return nodeDelete(tree, node, idx, key)
	default:
		log.Panicln("invalid node header(bad node)!")
	}
	return BNode{}
}

func nodeDelete(tree *BTree, node BNode, idx uint16, key []byte) BNode {
	kptr := node.getptr(idx)
	updated := treeDelete(tree, tree.get(kptr), key)
	if len(updated) == 0 {
		return BNode{}
	}

	new := BNode(make([]byte, PAGE_SIZE))
	mergeDir, sibling := shouldMerge(tree, node, idx, updated)
	switch {
	case mergeDir < 0:
		merged := BNode(make([]byte, PAGE_SIZE))
		nodeMerge(merged, sibling, updated)
		tree.del(node.getptr(idx - 1))
		nodeReplace2Kid(new, node, idx-1, tree.new(merged), merged.getKey(0))
	case mergeDir > 0:
		merged := BNode(make([]byte, PAGE_SIZE))
		nodeMerge(merged, updated, sibling)
		tree.del(node.getptr(idx + 1))
		nodeReplace2Kid(new, node, idx, tree.new(merged), merged.getKey(0))
	case mergeDir == 0 && updated.nkeys() == 0:
		if node.nkeys() == 1 && idx == 0 {
			new.setHeader(BNODE_NODE, 0)
		}
	case mergeDir == 0 && updated.nkeys() > 0:
		nodeReplaceKidN(tree, new, node, idx, updated)
	}
	return new
}

func (t BTree) getVal(node BNode, key []byte) ([]byte, error) {
	switch node.btype() {
	case BNODE_LEAF:
		idx := nodeLookupLE(node, key)
		if bytes.Equal(node.getKey(idx), key) {
			return node.getVal(idx), nil
		}
		return nil, fmt.Errorf("key not present %v", key)

	case BNODE_NODE:
		idx := nodeLookupLE(node, key)
		cptr := node.getptr(idx)
		cnode := BNode(t.get(cptr))
		return t.getVal(cnode, key)
	}
	return nil, fmt.Errorf("invalid node header")
}

func (t *BTree) GetVal(key []byte) ([]byte, error) {
	if t.Root == 0 {
		return nil, fmt.Errorf("btree is empty")
	}
	return t.getVal(t.get(t.Root), key)
}

func (t *BTree) Insert(key, val []byte) {
	if t.Root == 0 {
		new := BNode(make([]byte, PAGE_SIZE))
		new.setHeader(BNODE_LEAF, 2)
		nodeAppendKV(new, 0, 0, nil, nil)
		nodeAppendKV(new, 1, 0, key, val)
		t.Root = t.new(new)
		return
	}

	node := treeInsert(t, t.get(t.Root), key, val)
	nsplit, split := nodeSplit3(node)

	rootnode := BNode(t.get(t.Root))
	if rootnode.btype() != BNODE_LEAF {
		t.del(t.Root)
	}

	if nsplit > 1 {
		newRoot := BNode(make([]byte, PAGE_SIZE))
		newRoot.setHeader(BNODE_NODE, nsplit)
		for i := 0; i < int(nsplit); i++ {
			childNode := split[i]
			if rootnode.btype() == BNODE_LEAF && i == 0 {
				modChildNode := BNode(make([]byte, PAGE_SIZE))
				modChildNode.setHeader(childNode.btype(), childNode.nkeys()-1)
				nodeAppendRange(modChildNode, childNode, 0, 1, childNode.nkeys()-1)
				childNode = modChildNode
			}
			ptr := t.new(childNode)
			childKey := childNode.getKey(0)
			nodeAppendKV(newRoot, uint16(i), ptr, childKey, nil)
		}
		t.Root = t.new(newRoot)
	} else {
		t.Root = t.new(split[0])
	}
}

func (tree *BTree) Delete(key []byte) bool {
	if tree.Root == 0 {
		log.Panicln("root node is empty")
		return false
	}

	updatedNode := treeDelete(tree, BNode(tree.get(tree.Root)), key)
	if len(updatedNode) == 0 {
		return false
	}

	tree.del(tree.Root)

	if updatedNode.btype() == BNODE_NODE && updatedNode.nkeys() == 1 {
		tree.Root = updatedNode.getptr(0)
	} else {
		tree.Root = tree.new(updatedNode)
	}
	return true
}

func (tree *BTree) SeekLE(key []byte) *BIter {
	iter := &BIter{tree: tree, valid: true}
	for ptr := tree.Root; ptr != 0; {
		node := BNode(tree.get(ptr))
		idx := nodeLookupLE(node, key)
		iter.path = append(iter.path, node)
		iter.pos = append(iter.pos, int32(idx))
		ptr = node.getptr(idx)
	}
	return iter
}

func (i *BIter) Deref() ([]byte, []byte) {
	if i.tree.Root == 0 || len(i.path) == 0 {
		return nil, nil
	}
	node := i.path[len(i.pos)-1]
	idx := i.pos[len(i.pos)-1]
	key := node.getKey(uint16(idx))
	val := node.getVal(uint16(idx))
	return key, val
}

func (i *BIter) Valid() bool {
	if !i.valid || i.tree.Root == 0 {
		return false
	}
	node := i.path[len(i.pos)-1]
	pos := i.pos[len(i.pos)-1]
	if pos < 0 || uint16(pos) > node.nkeys()-1 {
		return false
	}
	return true
}

func (i *BIter) LastNode() bool {
	for p, node := range i.path {
		nkeys := int32(node.nkeys())
		if i.pos[p] != nkeys-1 {
			return false
		}
	}
	return true
}

func (i *BIter) StartNode() bool {
	for p := range i.path {
		if i.pos[p] != 0 {
			return false
		}
	}
	return true
}

func (i *BIter) Next() {
	if i.LastNode() && i.valid {
		i.valid = false
		return
	}
	if i.StartNode() && !i.valid {
		i.valid = true
		return
	}
	iterNext(i, len(i.path)-1)
}

func (i *BIter) Prev() {
	if i.StartNode() && i.valid {
		i.valid = false
		return
	}
	if i.LastNode() && !i.valid {
		i.valid = true
		return
	}
	iterPrev(i, len(i.path)-1)
}

func iterNext(iter *BIter, level int) {
	if iter.pos[level]+1 < int32(iter.path[level].nkeys()) {
		iter.pos[level]++
	} else if level > 0 {
		iterNext(iter, level-1)
	} else {
		iter.valid = false
		return
	}

	if level+1 < len(iter.pos) {
		node := iter.path[level]
		knode := BNode(iter.tree.get(node.getptr(uint16(iter.pos[level]))))
		iter.path[level+1] = knode
		iter.pos[level+1] = 0
	}
}

func iterPrev(iter *BIter, level int) {
	if iter.pos[level] > 0 {
		iter.pos[level]--
	} else if level > 0 {
		iterPrev(iter, level-1)
	} else {
		iter.valid = false
		return
	}

	if level+1 < len(iter.pos) && iter.pos[len(iter.pos)-1] != -1 {
		node := iter.path[level]
		knode := BNode(iter.tree.get(node.getptr(uint16(iter.pos[level]))))
		iter.path[level+1] = knode
		iter.pos[level+1] = int32(knode.nkeys() - 1)
	}
}
