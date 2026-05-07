package types

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
)

const (
	TYPE_BYTES = 1
	TYPE_INT64 = 2

	MODE_UPDATE  = 0 // update only
	MODE_INSERT  = 1 // insert only
	MODE_UPDSERT = 2 // update or insert
)

type Value struct {
	Type uint32 `json:"type"`
	I64  int64  `json:"int64"`
	Str  []byte `json:"str"`
}

type Record struct {
	Cols []string
	Vals []Value
}

func (r *Record) AddStr(col string, val []byte) *Record {
	r.Cols = append(r.Cols, col)
	r.Vals = append(r.Vals, Value{
		Type: TYPE_BYTES,
		Str:  val,
	})
	return r
}

func (r *Record) AddI64(col string, val int64) *Record {
	r.Cols = append(r.Cols, col)
	r.Vals = append(r.Vals, Value{
		Type: TYPE_INT64,
		I64:  val,
	})
	return r
}

func (r *Record) Get(col string) *Value {
	for i, c := range r.Cols {
		if c == col {
			return &r.Vals[i]
		}
	}
	return nil
}

func (r *Record) ToString() []string {
	res := []string{}
	for _, val := range r.Vals {
		if val.Type == TYPE_BYTES {
			res = append(res, string(val.Str))
		} else {
			res = append(res, fmt.Sprintf("%d", val.I64))
		}
	}
	return res
}

type TableDef struct {
	Name     string     `json:"name"`
	Cols     []string   `json:"cols"`
	Types    []uint32   `json:"types"`
	Pkeys    int        `json:"pkeys"`
	Prefixes []uint32   `json:"prefixes"`
	Indexes  [][]string `json:"indexes"`
}

var TDEF_TABLE = &TableDef{
	Name:     "@table",
	Cols:     []string{"name", "def"},
	Types:    []uint32{TYPE_BYTES, TYPE_BYTES},
	Pkeys:    1,
	Prefixes: []uint32{2},
	Indexes:  [][]string{{"name"}},
}

var TDEF_META = &TableDef{
	Name:     "@meta",
	Cols:     []string{"key", "value"},
	Types:    []uint32{TYPE_BYTES, TYPE_BYTES},
	Pkeys:    1,
	Prefixes: []uint32{1},
	Indexes:  [][]string{{"key"}},
}

func EncodeKey(prefix uint32, pks []Value) []byte {
	delimiter := byte(0x00)
	key := make([]byte, 4)
	binary.BigEndian.PutUint32(key, prefix)
	for _, val := range pks {
		switch val.Type {
		case TYPE_BYTES:
			key = append(key, SerializeBytes(val.Str)...)
			key = append(key, delimiter)
		case TYPE_INT64:
			key = append(key, SerializeInt(val.I64)...)
		default:
			panic("invalid value type")
		}
	}
	return key
}

func DecodeKey(encodedBytes []byte, tdef TableDef) []Value {
	encodedBytes = encodedBytes[4:]

	pks := make([][]byte, 0)
	bytePtr := 0
	for _, keyType := range tdef.Types[:tdef.Pkeys] {
		key := make([]byte, 0)
		switch keyType {
		case TYPE_BYTES:
			for encodedBytes[bytePtr] != byte(0x00) {
				key = append(key, encodedBytes[bytePtr])
				bytePtr++
			}
			bytePtr++
		case TYPE_INT64:
			key = append(key, encodedBytes[bytePtr:bytePtr+8]...)
			bytePtr += 8
		default:
			panic("invalid key type")
		}
		pks = append(pks, key)
	}

	vals := make([]Value, 0)
	for i, keyType := range tdef.Types[:tdef.Pkeys] {
		val := &Value{}
		switch keyType {
		case TYPE_INT64:
			val.Type = TYPE_INT64
			val.I64 = DeserializeInt(pks[i])
		case TYPE_BYTES:
			val.Type = TYPE_BYTES
			val.Str = DeserializeBytes(pks[i])
		}
		vals = append(vals, *val)
	}

	return vals
}

func SerializeBytes(s []byte) []byte {
	encodedBytes := make([]byte, 0)
	for _, b := range s {
		switch byte(b) {
		case 0x00:
			encodedBytes = append(encodedBytes, []byte{0x01, 0x01}...)
		case 0x01:
			encodedBytes = append(encodedBytes, []byte{0x01, 0x02}...)
		default:
			encodedBytes = append(encodedBytes, byte(b))
		}
	}
	return encodedBytes
}

func DeserializeBytes(encodedBytes []byte) []byte {
	decodedBytes := make([]byte, 0)
	escape := false
	for _, b := range encodedBytes {
		if b == 0x01 && !escape {
			escape = true
			continue
		}

		if escape {
			escape = false
			switch b {
			case 0x01:
				decodedBytes = append(decodedBytes, byte(0x00))
			case 0x02:
				decodedBytes = append(decodedBytes, byte(0x01))
			default:
				panic("invalid escape character")
			}
		} else {
			decodedBytes = append(decodedBytes, b)
		}
	}
	return decodedBytes
}

func SerializeInt(num int64) []byte {
	if num == -9223372036854775808 {
		panic("number is too small, serializing causes collusion with delimiter")
	}

	res := make([]byte, 8)
	binary.BigEndian.PutUint64(res, uint64(num)^(1<<63))
	return res
}

func DeserializeInt(encodedNum []byte) int64 {
	num := binary.BigEndian.Uint64(encodedNum)
	num = num ^ (1 << 63)
	return int64(num)
}

func EncodeVal(vals []Value) []byte {
	data, err := json.Marshal(vals)
	if err != nil {
		panic("mashaling json data")
	}
	return data
}

func DecodeVal(tdef *TableDef, rec *Record, val []byte) error {
	decodedVals := make([]Value, 0)
	err := json.Unmarshal(val, &decodedVals)
	if err != nil {
		return fmt.Errorf("unmashaing value into record: %w", err)
	}
	for i, val := range decodedVals {
		colName := tdef.Cols[i+tdef.Pkeys]
		rec.Cols = append(rec.Cols, colName)
		rec.Vals = append(rec.Vals, val)
	}
	return nil
}

func GetPKs(rec Record, Pkeys int) *Record {
	newRec := &Record{}
	for i := 0; i < Pkeys; i++ {
		switch rec.Vals[i].Type {
		case TYPE_BYTES:
			newRec.AddStr(rec.Cols[i], rec.Vals[i].Str)
		case TYPE_INT64:
			newRec.AddI64(rec.Cols[i], rec.Vals[i].I64)
		}
	}
	return newRec
}

func GetRecordVals(tdef *TableDef, rec Record, n int) ([]Value, error) {
	if len(rec.Cols) < n || n != tdef.Pkeys {
		return nil, fmt.Errorf("invalid number of columns in record")
	}
	return rec.Vals, nil
}
