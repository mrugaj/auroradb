package index

import (
	"auroradb/types"
)

func GetSecondaryIndexesKeys(tdef *types.TableDef, rec types.Record) [][]byte {
	keys := make([][]byte, 0)
	for i, idx := range tdef.Indexes[1:] {
		pks := make([]types.Value, 0)
		for _, col := range idx {
			val := rec.Get(col)
			if val.Type == types.TYPE_BYTES && len(val.Str) == 0 {
				val.Str = []byte{0xff}
			}
			pks = append(pks, *val)
		}
		pks = append(pks, rec.Vals[:tdef.Pkeys]...)
		key := types.EncodeKey(tdef.Prefixes[i+1], pks)
		keys = append(keys, key)
	}
	return keys
}

func GetPKFromSecKey(tdef *types.TableDef, key []byte, idx int) *types.Record {
	key = key[4:]

	pks := make([][]byte, 0)
	bytePtr := 0
	valueTypes := make([]uint32, 0)
	for _, col := range tdef.Indexes[idx] {
		for i, c := range tdef.Cols {
			if col == c {
				valueTypes = append(valueTypes, tdef.Types[i])
				break
			}
		}
	}

	for _, keyType := range valueTypes {
		switch keyType {
		case types.TYPE_BYTES:
			for key[bytePtr] != byte(0x00) {
				bytePtr++
			}
			bytePtr++
		case types.TYPE_INT64:
			bytePtr += 8
		default:
			panic("invalid key type")
		}
	}

	for _, keyType := range tdef.Types[:tdef.Pkeys] {
		pk := make([]byte, 0)
		switch keyType {
		case types.TYPE_BYTES:
			for key[bytePtr] != byte(0x00) {
				pk = append(pk, key[bytePtr])
				bytePtr++
			}
			bytePtr++
		case types.TYPE_INT64:
			pk = append(pk, key[bytePtr:bytePtr+8]...)
			bytePtr += 8
		default:
			panic("invalid key type")
		}
		pks = append(pks, pk)
	}

	rec := &types.Record{}
	for i, keyType := range tdef.Types[:tdef.Pkeys] {
		switch keyType {
		case types.TYPE_INT64:
			val := types.DeserializeInt(pks[i])
			rec.AddI64(tdef.Cols[i], val)
		case types.TYPE_BYTES:
			val := types.DeserializeBytes(pks[i])
			rec.AddStr(tdef.Cols[i], val)
		}
	}
	return rec
}
