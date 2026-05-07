package engine

import (
	"auroradb/db"
	"auroradb/parser"
	"auroradb/types"
	"fmt"
	"math"
)

type RecordIter interface {
	Valid() bool
	Next()
	Deref(*types.Record) error
}

type qlScanIter struct {
	req *parser.QLScan
	sc  *db.Scanner
	rec types.Record
	err error
}

func newQlScanIter(req *parser.QLScan, sc *db.Scanner) *qlScanIter {
	return &qlScanIter{
		req: req,
		sc:  sc,
		rec: types.Record{},
		err: nil,
	}
}

func (iter *qlScanIter) Valid() bool {
	return iter.sc.Valid()
}

func (iter *qlScanIter) Next() {
	iter.sc.Next()
}

func (iter *qlScanIter) Prev() {
	iter.sc.Prev()
}

func (iter *qlScanIter) Deref() (*types.Record, error) {
	rec, err := iter.sc.Deref()
	if err != nil {
		iter.err = err
		return nil, err
	}
	iter.rec = *rec
	var res error
	if iter.req.Filter != nil {
		res = QLEvalRecordBool(rec, *iter.req.Filter)
	}
	if res != nil {
		iter.err = res
		return nil, res
	}
	iter.err = nil
	return rec, nil
}

type qlScanFilter struct {
	sc     *qlScanIter
	limit  int
	offset int
	idx    int
}

func newQlScanFilter(req *parser.QLScan, tx *db.DBTX) (*qlScanFilter, error) {
	sc, err := qlScanInit(req, tx)
	if err != nil {
		return nil, err
	}
	iterator := newQlScanIter(req, sc)
	filter := &qlScanFilter{
		sc:     iterator,
		limit:  req.Limit,
		offset: req.Offset,
		idx:    0,
	}
	for filter.idx != filter.offset {
		filter.Next()
	}
	_, err = filter.sc.Deref()
	for err != nil && err.Error() != "out of range" {
		filter.sc.Next()
		_, err = filter.sc.Deref()
	}
	return filter, nil
}

func (iter *qlScanFilter) Valid() bool {
	if iter.idx >= iter.limit {
		return false
	}
	return iter.sc.Valid()
}

func (iter *qlScanFilter) Next() {
	iter.idx++
	iter.sc.Next()
	_, err := iter.sc.Deref()
	for err != nil && err.Error() != "out of range" {
		iter.sc.Next()
		_, err = iter.sc.Deref()
	}
}

func (iter *qlScanFilter) Prev() {
	if iter.idx >= 0 {
		iter.idx--
		iter.sc.Prev()
		_, err := iter.sc.Deref()
		for err != nil {
			iter.Prev()
			_, err = iter.sc.Deref()
		}
	}
}

func (iter *qlScanFilter) Deref() (types.Record, error) {
	iter.sc.Deref()
	return iter.sc.rec, iter.sc.err
}

type qlScanSelect struct {
	iter *qlScanFilter
	sel  *parser.QLSelect
}

func qlSelect(sel *parser.QLSelect, tx *db.DBTX) (*qlScanSelect, error) {
	iterator, err := newQlScanFilter(&sel.QLScan, tx)
	if err != nil {
		return nil, fmt.Errorf("creating scan filter: %w", err)
	}
	return &qlScanSelect{
		iter: iterator,
		sel:  sel,
	}, nil
}

func (sc *qlScanSelect) Next() {
	sc.iter.Next()
}

func (sc *qlScanSelect) Prev() {
	sc.iter.Prev()
}

func (sc *qlScanSelect) Valid() bool {
	return sc.iter.Valid()
}

func (sc *qlScanSelect) Deref() (*types.Record, error) {
	rec, err := sc.iter.Deref()
	if err != nil {
		return nil, err
	}
	return QLEvalOutput(&rec, sc.sel.Output, sc.sel.Name)
}

func qlScanInit(req *parser.QLScan, tx *db.DBTX) (*db.Scanner, error) {
	tname := req.Table
	tdef, err := db.GetTableDef(tx, tname)
	if err != nil || tdef == nil {
		return nil, fmt.Errorf("no table named: %s", tname)
	}

	if req.Key1 == nil && req.Key2 == nil {
		return fullRangeIndexScanner(tdef, tx, 0)
	}

	key1node := req.Key1
	if len(key1node.Kids) == 0 && req.Key2 == nil {
		key1col := key1node.Str
		index, err := getColsIndex(tdef, string(key1col))
		if err != nil {
			return nil, fmt.Errorf("getting column index: %w", err)
		}
		return fullRangeIndexScanner(tdef, tx, index)
	}

	if key1node.Type == parser.QL_TUP {
		kids := key1node.Kids
		cols := []string{}
		for _, kid := range kids {
			if kid.Type != parser.QL_SYM {
				return nil, fmt.Errorf("invalid index by arguments, expected symbols")
			}
			cols = append(cols, string(kid.Str))
		}
		index, err := getColsIndex(tdef, cols...)
		if err != nil {
			return nil, fmt.Errorf("getting column index: %w", err)
		}
		return fullRangeIndexScanner(tdef, tx, index)
	}

	key1col := key1node.Kids[0].Str
	var valType uint32
	for i, col := range tdef.Cols {
		if string(key1col) == col {
			valType = tdef.Types[i]
		}
	}

	index, err := getColsIndex(tdef, string(key1col))
	if err != nil {
		return nil, fmt.Errorf("getting column index: %w", err)
	}

	if req.Key2 == nil {
		startRec := &types.Record{}
		err := addValToRecord(valType, startRec, string(key1col), key1node.Kids[1].Value)
		if err != nil {
			return nil, fmt.Errorf("adding start record: %w", err)
		}

		endRec := &types.Record{}
		if key1node.Type == parser.QL_CMP_GE || key1node.Type == parser.QL_CMP_GT {
			err := addValToRecord(valType, endRec, string(key1col), MaxValue())
			if err != nil {
				return nil, fmt.Errorf("adding end record: %w", err)
			}
		} else if key1node.Type == parser.QL_CMP_LE || key1node.Type == parser.QL_CMP_LT {
			err := addValToRecord(valType, endRec, string(key1col), MinValue())
			if err != nil {
				return nil, fmt.Errorf("adding end record: %w", err)
			}
		} else if key1node.Type == parser.QL_CMP_EQ {
			endRec = startRec
		}

		key1inc := false
		key2inc := false
		if key1node.Type == parser.QL_CMP_GE || key1node.Type == parser.QL_CMP_LE || key1node.Type == parser.QL_CMP_EQ {
			key1inc = true
		}
		if key1node.Type == parser.QL_CMP_EQ {
			key2inc = true
		}
		sc := tx.NewScanner(*tdef, *startRec, *endRec, key1inc, key2inc, index)
		return sc, nil
	}

	key2node := req.Key2
	key2col := key2node.Kids[0].Str

	if key1node.Type == parser.QL_CMP_GE || key1node.Type == parser.QL_CMP_GT {
		if key2node.Type != parser.QL_CMP_LE && key2node.Type != parser.QL_CMP_LT {
			return nil, fmt.Errorf("invalid range conditions")
		}
	}

	if key2node.Type == parser.QL_CMP_GE || key2node.Type == parser.QL_CMP_GT {
		if key1node.Type != parser.QL_CMP_LE && key1node.Type != parser.QL_CMP_LT {
			return nil, fmt.Errorf("invalid range conditions")
		}
	}

	if string(key1col) != string(key2col) {
		return nil, fmt.Errorf("index by should include conditions for the same column")
	}

	startRec := &types.Record{}
	endRec := &types.Record{}

	if valType == parser.QL_STR {
		startRec.AddStr(string(key1col), key1node.Kids[1].Str)
		endRec.AddStr(string(key2col), key2node.Kids[1].Str)
	} else if valType == parser.QL_I64 {
		startRec.AddI64(string(key1col), key1node.Kids[1].I64)
		endRec.AddI64(string(key2col), key2node.Kids[1].I64)
	} else {
		return nil, fmt.Errorf("invalid column type: %d", valType)
	}

	var key1inc bool
	var key2inc bool
	if key1node.Type == parser.QL_CMP_GE || key1node.Type == parser.QL_CMP_LE {
		key1inc = true
	}
	if key2node.Type == parser.QL_CMP_GE || key2node.Type == parser.QL_CMP_LE {
		key2inc = true
	}

	sc := tx.NewScanner(*tdef, *startRec, *endRec, key1inc, key2inc, index)
	return sc, nil
}

func fullRangeIndexScanner(tdef *types.TableDef, tx *db.DBTX, idx int) (*db.Scanner, error) {
	startRec := &types.Record{}
	endRec := &types.Record{}

	for _, col := range tdef.Indexes[idx] {
		var colType uint32
		for i, c := range tdef.Cols {
			if col == c {
				colType = tdef.Types[i]
			}
		}
		err := addValToRecord(colType, startRec, col, MinValue())
		if err != nil {
			return nil, fmt.Errorf("adding start record: %w", err)
		}
		err = addValToRecord(colType, endRec, col, MaxValue())
		if err != nil {
			return nil, fmt.Errorf("adding end record: %w", err)
		}
	}
	sc := tx.NewScanner(*tdef, *startRec, *endRec, false, false, idx)
	return sc, nil
}

func getColsIndex(tdef *types.TableDef, cols ...string) (int, error) {
	if len(cols) == 0 {
		return -1, fmt.Errorf("no columns provided")
	}
	if len(cols) == 1 {
		for i, idx := range tdef.Indexes {
			if cols[0] == idx[0] {
				return i, nil
			}
		}
		return -1, fmt.Errorf("there are no columns having index: %v", cols[0])
	} else {
		for i, idx := range tdef.Indexes {
			found := true
			if len(idx) < len(cols) {
				break
			}
			for j, col := range cols {
				if col != idx[j] {
					found = false
					break
				}
			}
			if found {
				return i, nil
			}
		}
		for i, idx := range tdef.Indexes {
			count := 0
			for _, col := range cols {
				for _, k := range idx {
					if col == k {
						count++
					}
				}
			}
			if count == len(cols) {
				return i, nil
			}
		}
		return -1, fmt.Errorf("there are no columns having indexes: %v", cols)
	}
}

func addValToRecord(valType uint32, rec *types.Record, col string, val types.Value) error {
	if valType == parser.QL_STR {
		rec.AddStr(col, val.Str)
	} else if valType == parser.QL_I64 {
		rec.AddI64(col, val.I64)
	} else {
		return fmt.Errorf("invalid column type: %d", valType)
	}
	return nil
}

func MaxValue() types.Value {
	return types.Value{
		I64: math.MaxInt64,
		Str: []byte{0xff, 0xff, 0xff, 0xff},
	}
}

func MinValue() types.Value {
	return types.Value{
		I64: math.MinInt64 + 1,
		Str: []byte{0x00, 0x00, 0x00, 0x00},
	}
}
