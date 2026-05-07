package parser

import (
	"auroradb/types"
	"fmt"
	"math"
)

const (
	QL_UINT   = 0
	QL_STR    = types.TYPE_BYTES
	QL_I64    = types.TYPE_INT64
	QL_SYM    = 3
	QL_CMP_GE = 10
	QL_CMP_GT = 11
	QL_CMP_LE = 12
	QL_CMP_LT = 13
	QL_OR     = 14
	QL_AND    = 15
	QL_ADD    = 16
	QL_SUB    = 17
	QL_MOD    = 18
	QL_MUL    = 19
	QL_DIV    = 20
	QL_BOOL   = 21
	QL_CMP_EQ = 22
	QL_NEG    = 30
	QL_TUP    = 40
)

type QLNode struct {
	types.Value
	Kids []QLNode
}

type QLScan struct {
	Table  string
	Key1   *QLNode
	Key2   *QLNode
	Filter *QLNode
	Offset int
	Limit  int
}

type QLSelect struct {
	QLScan
	Name   []string
	Output []QLNode
}

type QLUpdate struct {
	QLScan
	Name   []string
	Values []QLNode
}

type QLInsert struct {
	Table    string
	Name     []string
	Values   []QLNode
	IsUpsert bool
}

type QLDelete struct {
	QLScan
}

type QLCreateTable struct {
	Def types.TableDef
}

func PStmt(p *Parser) (r interface{}) {
	switch {
	case pkeyword(p, "create", "table"):
		r = pCreateTable(p)
	case pkeyword(p, "select"):
		r = pSelect(p)
	case pkeyword(p, "insert", "into"):
		r = pInsert(p)
	case pkeyword(p, "upsert", "into"):
		r = pUpsert(p)
	case pkeyword(p, "update"):
		r = pUpdate(p)
	case pkeyword(p, "delete"):
		r = pDelete(p)
	default:
		fmt.Println("invalid query statement: ", string(p.Input))
		r = nil
	}
	return r
}

func pCreateTable(p *Parser) *QLCreateTable {
	tdef := &types.TableDef{}
	n := QLNode{}
	pStr(p, &n)
	tdef.Name = string(n.Str)
	if !pkeyword(p, "(") {
		pErr(p, nil, "invalid create table syntax")
		return nil
	}

	cols := [][]string{}
	indexes := [][]string{}
	pks := []string{}

	nexttkn := peekNextToken(p)
	for nexttkn != "index" && nexttkn != "primary" {
		nameNode := QLNode{}
		typeNode := QLNode{}

		pStr(p, &nameNode)
		pStr(p, &typeNode)

		col := []string{string(nameNode.Str), string(typeNode.Str)}
		cols = append(cols, col)
		if !pkeyword(p, ",") {
			pErr(p, nil, "invalid create table syntax")
		}
		nexttkn = peekNextToken(p)
	}

	for nexttkn != "primary" {
		pkeyword(p, "index")
		idx := &QLNode{}
		if !pkeyword(p, "(") {
			pErr(p, nil, "invalid create table syntax (declaring index)")
		}
		pExprTuple(p, idx)
		if !pkeyword(p, ")") {
			pErr(p, nil, "invalid create table syntax (declaring index)")
		}
		if !pkeyword(p, ",") {
			pErr(p, nil, "invalid create table syntax (declaring index)")
		}

		i := []string{}
		if len(idx.Kids) == 0 {
			i = append(i, string(idx.Str))
		} else {
			for _, kid := range idx.Kids {
				i = append(i, string(kid.Str))
			}
		}
		indexes = append(indexes, i)
		nexttkn = peekNextToken(p)
	}

	if !pkeyword(p, "primary", "key") {
		pErr(p, nil, "invalid syntax, primary keys declaration")
	}
	idx := &QLNode{}
	if !pkeyword(p, "(") {
		pErr(p, nil, "no primary key")
	}
	pExprTuple(p, idx)
	if !pkeyword(p, ")") {
		pErr(p, nil, "invalid create table syntax")
	}
	if !pkeyword(p, ")") {
		pErr(p, nil, "invalid create table syntax")
	}
	if !pkeyword(p, ";") {
		pErr(p, nil, "invalid create table syntax")
	}
	if len(idx.Kids) == 0 {
		pks = append(pks, string(idx.Str))
	} else {
		for _, kid := range idx.Kids {
			pks = append(pks, string(kid.Str))
		}
	}

	tdef.Pkeys = len(pks)
	for _, col := range cols {
		name := col[0]
		var colType uint32
		switch col[1] {
		case "int":
			colType = types.TYPE_INT64
		case "bytes", "string":
			colType = types.TYPE_BYTES
		default:
			pErr(p, nil, "invalid type")
		}
		tdef.Cols = append(tdef.Cols, name)
		tdef.Types = append(tdef.Types, colType)
	}

	tdef.Indexes = append(tdef.Indexes, pks)
	tdef.Indexes = append(tdef.Indexes, indexes...)

	return &QLCreateTable{Def: *tdef}
}

func pSelect(p *Parser) *QLSelect {
	stmt := QLSelect{}
	if pkeyword(p, "*") {
		stmt.Name = append(stmt.Name, "*")
	} else {
		pSelectExprList(p, &stmt)
	}

	if !pkeyword(p, "from") {
		pErr(p, nil, "expected `from` table")
	}
	stmt.Table = pMustSym(p)
	pScan(p, &stmt.QLScan)
	if p.Err != nil {
		return nil
	}
	if !pkeyword(p, ";") {
		pErr(p, nil, "invalid select query syntax")
	}
	return &stmt
}

func pInsert(p *Parser) *QLInsert {
	stmt := QLInsert{IsUpsert: false}
	stmt.Table = pMustSym(p)
	if !pkeyword(p, "(") {
		pErr(p, nil, "invalid insert query syntax")
	}
	colNames := &QLNode{}
	pExprTuple(p, colNames)
	if !pkeyword(p, ")") {
		pErr(p, nil, "invalid insert query syntax")
	}
	if !pkeyword(p, "values") {
		pErr(p, nil, "invalid insert query syntax")
	}
	if !pkeyword(p, "(") {
		pErr(p, nil, "invalid insert query syntax")
	}
	vals := &QLNode{}
	pExprTuple(p, vals)
	if !pkeyword(p, ")") {
		pErr(p, nil, "invalid insert query syntax")
	}
	if !pkeyword(p, ";") {
		pErr(p, nil, "invalid insert query syntax")
	}

	for _, name := range colNames.Kids {
		stmt.Name = append(stmt.Name, string(name.Str))
	}
	stmt.Values = append(stmt.Values, vals.Kids...)
	return &stmt
}

func pUpsert(p *Parser) *QLInsert {
	stmt := pInsert(p)
	if stmt != nil {
		stmt.IsUpsert = true
	}
	return stmt
}

func pUpdate(p *Parser) *QLUpdate {
	stmt := QLUpdate{}
	stmt.Table = pMustSym(p)
	if !pkeyword(p, "set") {
		pErr(p, nil, "invalid update query syntax expected 'set' ")
	}
	pUpdateExprList(p, &stmt)
	pScan(p, &stmt.QLScan)
	if p.Err != nil {
		return nil
	}
	if !pkeyword(p, ";") {
		pErr(p, nil, "invalid update query syntax")
	}
	return &stmt
}

func pDelete(p *Parser) *QLDelete {
	stmt := QLDelete{}
	if !pkeyword(p, "from") {
		pErr(p, nil, "expected `from` table")
	}
	stmt.Table = pMustSym(p)
	pScan(p, &stmt.QLScan)
	if p.Err != nil {
		return nil
	}
	if !pkeyword(p, ";") {
		pErr(p, nil, "invalid delete query syntax")
	}
	return &stmt
}

func pSelectExprList(p *Parser, stmt *QLSelect) {
	pSelectExpr(p, stmt)
	for pkeyword(p, ",") {
		pSelectExpr(p, stmt)
	}
}

func pSelectExpr(p *Parser, node *QLSelect) {
	expr := QLNode{}
	pExprOr(p, &expr)
	name := ""
	if pkeyword(p, "as") {
		name = pMustSym(p)
	}
	node.Name = append(node.Name, name)
	node.Output = append(node.Output, expr)
}

func pUpdateExprList(p *Parser, stmt *QLUpdate) {
	pUpdateExpr(p, stmt)
	for pkeyword(p, ",") {
		pUpdateExpr(p, stmt)
	}
}

func pUpdateExpr(p *Parser, stmt *QLUpdate) {
	col := &QLNode{}
	pSym(p, col)
	stmt.Name = append(stmt.Name, string(col.Str))
	if !pkeyword(p, "=") {
		pErr(p, nil, "invalid update query syntax expected '=' ")
	}
	expr := &QLNode{}
	pExprOr(p, expr)
	stmt.Values = append(stmt.Values, *expr)
}

func pScan(p *Parser, node *QLScan) {
	if pkeyword(p, "index", "by") {
		pIndexBy(p, node)
	}
	if pkeyword(p, "filter") {
		node.Filter = &QLNode{}
		pExprOr(p, node.Filter)
	}
	node.Offset, node.Limit = 0, math.MaxInt64
	if pkeyword(p, "offset") {
		pOffset(p, node)
	}
	if pkeyword(p, "limit") {
		pLimit(p, node)
	}
}

func pIndexBy(p *Parser, node *QLScan) {
	conditions := []QLNode{{}}
	if pkeyword(p, "(") {
		pExprTuple(p, &conditions[0])
		if !pkeyword(p, ")") {
			pErr(p, nil, "tuple is not closed")
		}
		node.Key1 = &conditions[0]
		node.Key2 = nil
		return
	}

	pExprCmp(p, &conditions[0])
	for pkeyword(p, "and") {
		new := QLNode{}
		pExprCmp(p, &new)
		conditions = append(conditions, new)
	}

	if len(conditions) > 2 {
		pErr(p, nil, "more than two conditions")
	}
	if len(conditions) == 1 {
		node.Key1 = &conditions[0]
		node.Key2 = nil
		return
	}
	node.Key1 = &conditions[0]
	node.Key2 = &conditions[1]
}

func pOffset(p *Parser, node *QLScan) {
	p.skipSpace()
	res := QLNode{}
	ok := pNum(p, &res)
	if !ok {
		pErr(p, &res, "expected number after offset")
	}
	node.Offset = int(res.I64)
}

func pLimit(p *Parser, node *QLScan) {
	p.skipSpace()
	res := QLNode{}
	ok := pNum(p, &res)
	if !ok {
		pErr(p, &res, "expected number after limit")
	}
	node.Limit = int(res.I64)
}

func pExprTuple(p *Parser, node *QLNode) {
	kids := []QLNode{{}}
	pExprOr(p, &kids[0])
	for pkeyword(p, ",") {
		kids = append(kids, QLNode{})
		pExprOr(p, &kids[len(kids)-1])
	}

	if len(kids) > 1 {
		node.Type = QL_TUP
		node.Kids = kids
	} else {
		*node = kids[0]
	}
}

func pExprBinOp(p *Parser, node *QLNode, ops []string, types []uint32, next func(*Parser, *QLNode)) {
	left := QLNode{}
	next(p, &left)

	for more := true; more; {
		more = false
		for i := range ops {
			if pkeyword(p, ops[i]) {
				new := QLNode{Value: valWithType(types[i])}
				new.Kids = []QLNode{left, {}}
				next(p, &new.Kids[1])

				left = new
				more = true
				break
			}
		}
	}
	*node = left
}

func valWithType(t uint32) types.Value {
	return types.Value{Type: t}
}

func pExprOr(p *Parser, node *QLNode) {
	pExprBinOp(p, node, []string{"or"}, []uint32{QL_OR}, pExprAnd)
}
func pExprAnd(p *Parser, node *QLNode) {
	pExprBinOp(p, node, []string{"and"}, []uint32{QL_AND}, pExprCmp)
}
func pExprCmp(p *Parser, node *QLNode) {
	pExprBinOp(p, node, []string{"<=", ">=", "==", "<", ">"}, []uint32{QL_CMP_LE, QL_CMP_GE, QL_CMP_EQ, QL_CMP_LT, QL_CMP_GT}, pExprAdd)
}
func pExprAdd(p *Parser, node *QLNode) {
	pExprBinOp(p, node, []string{"+", "-"}, []uint32{QL_ADD, QL_SUB}, pExprMul)
}
func pExprMul(p *Parser, node *QLNode) {
	pExprBinOp(p, node, []string{"*", "/", "%"}, []uint32{QL_MUL, QL_DIV, QL_MOD}, pExprUnOp)
}
func pExprUnOp(p *Parser, node *QLNode) {
	switch {
	case pkeyword(p, "-"):
		node.Type = QL_NEG
		node.Kids = []QLNode{{}}
		pExprAtom(p, &node.Kids[0])
	default:
		pExprAtom(p, node)
	}
}
func pExprAtom(p *Parser, node *QLNode) {
	switch {
	case pkeyword(p, "("):
		pExprTuple(p, node)
		if !pkeyword(p, ")") {
			pErr(p, node, "unclosed paranthesis")
		}
	case pSym(p, node):
	case pNum(p, node):
	case pStr(p, node):
	default:
		pErr(p, node, "expected symbol, number or string")
	}
}
