package db

import (
	"auroradb/index"
	"auroradb/storage"
	"auroradb/types"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
)

type Scanner struct {
	iter        *storage.BIter
	Key1        types.Record
	Key2        types.Record
	Tdef        types.TableDef
	key1Include bool
	key2Include bool
	index       int
	db          *DBTX
	key1        []byte
	key2        []byte
}

func (sc *Scanner) Next() {
	sc.iter.Next()
}

func (sc *Scanner) Prev() {
	sc.iter.Prev()
}

func (sc *Scanner) Valid() bool {
	if !sc.iter.Valid() {
		return false
	}

	key, _ := sc.iter.Deref()
	cmp := bytes.Compare(key, sc.key1)
	if cmp == 0 && !sc.key1Include {
		return false
	}
	if cmp < 0 {
		return false
	}

	cmp = bytes.Compare(key, sc.key2)
	if cmp == 0 && !sc.key2Include {
		return false
	}
	if cmp > 0 {
		return false
	}

	return true
}

func (sc *Scanner) kvToRecord(tdef *types.TableDef, key, val []byte, idx int) (*types.Record, error) {
	if idx != 0 {
		rec := index.GetPKFromSecKey(tdef, key, idx)
		_, err := dbGet(sc.db, tdef, rec)
		if err != nil {
			return nil, fmt.Errorf("getting secondary index row: %w", err)
		}
		return rec, nil
	}

	rec := &types.Record{}
	keys := types.DecodeKey(key, *tdef)
	for i, k := range keys {
		switch k.Type {
		case types.TYPE_BYTES:
			rec.AddStr(tdef.Cols[i], k.Str)
		case types.TYPE_INT64:
			rec.AddI64(tdef.Cols[i], k.I64)
		default:
			panic("invalid key type")
		}
	}

	err := types.DecodeVal(tdef, rec, val)
	if err != nil {
		return nil, fmt.Errorf("decoding values: %w", err)
	}
	return rec, nil
}

func (sc *Scanner) Deref() (*types.Record, error) {
	if !sc.Valid() {
		return nil, fmt.Errorf("out of range")
	}
	key, val := sc.iter.Deref()
	rec, err := sc.kvToRecord(&sc.Tdef, key, val, sc.index)
	if err != nil {
		return nil, fmt.Errorf("getting row: %w", err)
	}
	return rec, nil
}

type DB struct {
	Path string
	kv   *storage.KV
}

type DBTX struct {
	kv *storage.KVTX
	db *DB
}

func NewDB(path string, kv *storage.KV) *DB {
	return &DB{
		Path: path,
		kv:   kv,
	}
}

func (db *DB) NewTX() *DBTX {
	return &DBTX{
		kv: storage.NewKVTX(),
	}
}

func (db *DB) Begin(tx *DBTX) {
	tx.db = db
	db.kv.Begin(tx.kv)
}

func (db *DB) Commit(tx *DBTX) error {
	return db.kv.Commit(tx.kv)
}

func (db *DB) Abort(tx *DBTX) {
	db.kv.Abort(tx.kv)
}

func (db *DBTX) NewScanner(tdef types.TableDef, key1, key2 types.Record, key1inc, key2inc bool, idx int) *Scanner {
	var key1Bytes []byte
	var key2Bytes []byte

	if idx == 0 {
		key1Bytes = types.EncodeKey(tdef.Prefixes[0], types.GetPKs(key1, tdef.Pkeys).Vals)
		key2Bytes = types.EncodeKey(tdef.Prefixes[0], types.GetPKs(key2, tdef.Pkeys).Vals)
	} else {
		key1vals := make([]types.Value, 0)
		key2vals := make([]types.Value, 0)
		for _, col := range tdef.Indexes[idx] {
			val1 := key1.Get(col)
			val2 := key2.Get(col)

			key1vals = append(key1vals, *val1)
			key2vals = append(key2vals, *val2)
		}

		key1Bytes = types.EncodeKey(tdef.Prefixes[idx], key1vals)
		key2Bytes = types.EncodeKey(tdef.Prefixes[idx], key2vals)
	}

	cmp := bytes.Compare(key1Bytes, key2Bytes)
	if cmp > 0 {
		key1Bytes, key2Bytes = key2Bytes, key1Bytes
		key1, key2 = key2, key1
		key1inc, key2inc = key2inc, key1inc
	}

	if idx != 0 {
		if key1inc {
			key1Bytes = append(key1Bytes, byte(0x00))
		} else {
			key1Bytes = append(key1Bytes, byte(0xff))
		}

		if key2inc {
			key2Bytes = append(key2Bytes, byte(0xff))
		} else {
			key2Bytes = append(key2Bytes, byte(0x00))
		}
	}

	sc := &Scanner{
		iter:        db.kv.Seek(key1Bytes),
		Key1:        key1,
		Key2:        key2,
		Tdef:        tdef,
		index:       idx,
		key1:        key1Bytes,
		key2:        key2Bytes,
		key1Include: key1inc,
		key2Include: key2inc,
		db:          db,
	}

	key, _ := sc.iter.Deref()
	cmp = bytes.Compare(key, sc.key1)
	if cmp < 0 && idx != 0 {
		sc.Next()
	}

	key, _ = sc.iter.Deref()
	if key == nil {
		return sc
	}

	cmp = bytes.Compare(key, sc.key1)
	for cmp <= 0 && !sc.key1Include {
		sc.Next()
		key, _ = sc.iter.Deref()
		if key == nil {
			break
		}
		cmp = bytes.Compare(key, sc.key1)
	}

	return sc
}

func (db *DBTX) TableNew(tdef *types.TableDef) error {
	rec := &types.Record{}
	rec.AddStr("name", []byte(tdef.Name))

	recpks := types.GetPKs(*rec, tdef.Pkeys)
	ok, _ := dbGet(db, types.TDEF_TABLE, recpks)
	if ok {
		return fmt.Errorf("table already exists")
	}

	prefixrec := &types.Record{}
	prefixrec.AddStr("key", []byte("prefix"))
	ok, _ = dbGet(db, types.TDEF_META, prefixrec)

	var curPrefix int64
	if !ok {
		curPrefix = 3
	} else {
		curPrefix = prefixrec.Get("value").I64
	}

	Prefixes := make([]uint32, 0)
	for range tdef.Indexes {
		Prefixes = append(Prefixes, uint32(curPrefix))
		curPrefix++
	}

	tdef.Prefixes = make([]uint32, len(Prefixes))
	copy(tdef.Prefixes, Prefixes)

	prefixrec.AddI64("data", curPrefix)
	dbSet(db, types.TDEF_META, *prefixrec)

	data, err := json.Marshal(tdef)
	if err != nil {
		return fmt.Errorf("marshalling table definition: %w", err)
	}
	rec.AddStr("def", data)

	dbSet(db, types.TDEF_TABLE, *rec)
	return nil
}

func (db *DBTX) Get(table string, rec *types.Record) (bool, error) {
	tdef, err := GetTableDef(db, table)
	if err != nil {
		return false, err
	}
	if tdef == nil {
		return false, fmt.Errorf("table not found: %s", table)
	}
	return dbGet(db, tdef, rec)
}

func (db *DBTX) Insert(table string, rec types.Record) error {
	tdef, err := GetTableDef(db, table)
	if err != nil {
		return err
	}
	if tdef == nil {
		return fmt.Errorf("table not found: %s", table)
	}

	tempRec := &types.Record{}
	for i, val := range rec.Vals[:tdef.Pkeys] {
		switch val.Type {
		case types.TYPE_BYTES:
			tempRec.AddStr(rec.Cols[i], val.Str)
		case types.TYPE_INT64:
			tempRec.AddI64(rec.Cols[i], val.I64)
		}
	}

	ok, _ := dbGet(db, tdef, tempRec)
	if !ok {
		return dbSet(db, tdef, rec)
	}
	return fmt.Errorf("key already exists")
}

func (db *DBTX) Update(table string, rec types.Record) bool {
	tdef, err := GetTableDef(db, table)
	if err != nil {
		return false
	}
	getRec := types.GetPKs(rec, tdef.Pkeys)
	ok, _ := dbGet(db, tdef, getRec)
	if !ok {
		return false
	}

	ok = dbDel(db, tdef, *getRec)
	if !ok {
		return false
	}

	err = dbSet(db, tdef, rec)
	if err != nil {
		fmt.Printf("updating the record: %s", err)
		return false
	}

	return true
}

func (db *DBTX) Upsert(table string, rec types.Record) bool {
	tdef, err := GetTableDef(db, table)
	if err != nil {
		return false
	}

	getRec := types.GetPKs(rec, tdef.Pkeys)
	ok, _ := dbGet(db, tdef, getRec)
	if ok {
		return db.Update(table, rec)
	} else {
		err := db.Insert(table, rec)
		if err != nil {
			log.Printf("inserting record: %s", err)
			return false
		}
	}
	return true
}

func (db *DBTX) Delete(table string, rec types.Record) bool {
	tdef, err := GetTableDef(db, table)
	if err != nil {
		return false
	}
	return dbDel(db, tdef, rec)
}

func dbDel(db *DBTX, tdef *types.TableDef, rec types.Record) bool {
	values, err := types.GetRecordVals(tdef, rec, tdef.Pkeys)
	if err != nil {
		return false
	}

	key := types.EncodeKey(tdef.Prefixes[0], values[:tdef.Pkeys])
	ok, _ := dbGet(db, tdef, &rec)
	if !ok {
		return false
	}

	if len(tdef.Indexes) > 1 {
		keys := index.GetSecondaryIndexesKeys(tdef, rec)
		for _, k := range keys {
			ok := db.kv.Del(k)
			if !ok {
				return false
			}
		}
	}
	return db.kv.Del(key)
}

func dbSet(db *DBTX, tdef *types.TableDef, rec types.Record) error {
	values, err := types.GetRecordVals(tdef, rec, tdef.Pkeys)
	if err != nil {
		return fmt.Errorf("getting record values: %w", err)
	}

	key := types.EncodeKey(tdef.Prefixes[0], values[:tdef.Pkeys])
	val := types.EncodeVal(values[tdef.Pkeys:])
	db.kv.Set(key, val)

	if len(tdef.Indexes) > 1 {
		keys := index.GetSecondaryIndexesKeys(tdef, rec)
		for _, k := range keys {
			db.kv.Set(k, []byte{})
		}
	}
	return nil
}

func dbGet(db *DBTX, tdef *types.TableDef, rec *types.Record) (bool, error) {
	values, err := types.GetRecordVals(tdef, *rec, tdef.Pkeys)
	if err != nil {
		return false, fmt.Errorf("getting record values: %w", err)
	}

	key := types.EncodeKey(tdef.Prefixes[0], values[:tdef.Pkeys])
	val, err := db.kv.Get(key)
	if err != nil {
		return false, fmt.Errorf("getting key(%s): %w", key, err)
	}

	err = types.DecodeVal(tdef, rec, val)
	if err != nil {
		return false, fmt.Errorf("decoding values: %w", err)
	}
	return true, nil
}

func GetTableDef(db *DBTX, table string) (*types.TableDef, error) {
	if table == "@table" {
		return types.TDEF_TABLE, nil
	} else if table == "@meta" {
		return types.TDEF_META, nil
	}
	rec := (&types.Record{}).AddStr("name", []byte(table))
	ok, err := dbGet(db, types.TDEF_TABLE, rec)
	if err != nil || !ok {
		return nil, fmt.Errorf("getting table definition: %w", err)
	}
	tdef := &types.TableDef{}
	err = json.Unmarshal(rec.Get("def").Str, tdef)
	if err != nil {
		return nil, fmt.Errorf("decoding table definition: %w", err)
	}
	return tdef, nil
}
