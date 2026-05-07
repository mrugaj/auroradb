package engine

import (
	"auroradb/db"
	"auroradb/parser"
	"auroradb/types"
	"fmt"
	"strings"
)

type QLEvalContext struct {
	env types.Record
	out types.Value
	err error
}

func qlEval(ctx *QLEvalContext, node parser.QLNode) {
	switch node.Type {
	case parser.QL_STR, parser.QL_I64:
		ctx.out = node.Value
	case parser.QL_SYM:
		if v := ctx.env.Get(string(node.Str)); v != nil {
			ctx.out = *v
		} else {
			qlErr(ctx, "unknown column: %s", node.Str)
		}
	case parser.QL_CMP_GE, parser.QL_CMP_LE, parser.QL_CMP_LT, parser.QL_CMP_GT, parser.QL_CMP_EQ:
		qlCmp(ctx, &node, node.Type)
	case parser.QL_OR, parser.QL_AND:
		qlOrAnd(ctx, &node, node.Type)
	case parser.QL_ADD, parser.QL_SUB, parser.QL_MUL, parser.QL_DIV, parser.QL_MOD:
		qlNumeric(ctx, &node, node.Type)
	case parser.QL_NEG:
		qlEval(ctx, node.Kids[0])
		if ctx.out.Type == parser.QL_I64 {
			ctx.out.I64 = -ctx.out.I64
		} else {
			qlErr(ctx, "QL_NEG type error")
		}
	}
}

func qlNumeric(ctx *QLEvalContext, node *parser.QLNode, ntype uint32) {
	left, right, err := evalLeftRightKids(ctx, node)
	if right == nil {
		right = &types.Value{}
	}
	if err != nil {
		return
	}

	if left.Type != parser.QL_I64 {
		qlErr(ctx, "invalid kid nodes: %v and %v", left, right)
		return
	}

	out := types.Value{Type: parser.QL_I64}
	var res int64
	a := left.I64
	b := right.I64
	switch ntype {
	case parser.QL_ADD:
		res = a + b
	case parser.QL_SUB:
		res = a - b
	case parser.QL_MUL:
		res = a * b
	case parser.QL_DIV:
		res = a / b
	case parser.QL_MOD:
		res = a % b
	case parser.QL_NEG:
		res = -a
	default:
		qlErr(ctx, "invalid node type: %d", ntype)
		return
	}
	out.I64 = res
	ctx.out = out
}

func qlOrAnd(ctx *QLEvalContext, node *parser.QLNode, cmp uint32) {
	left, right, err := evalLeftRightKids(ctx, node)
	if err != nil {
		return
	}

	if left.Type != parser.QL_BOOL {
		qlErr(ctx, "invalid kid nodes: %v and %v", left, right)
		return
	}

	res := types.Value{Type: parser.QL_BOOL}
	res.I64 = 0

	a := false
	b := false

	if right.I64 == 1 {
		b = true
	}
	if left.I64 == 1 {
		a = true
	}

	switch cmp {
	case parser.QL_OR:
		if a || b {
			res.I64 = 1
		}
	case parser.QL_AND:
		if a && b {
			res.I64 = 1
		}
	}

	ctx.out = res
}

func qlCmp(ctx *QLEvalContext, node *parser.QLNode, cmp uint32) {
	left, right, err := evalLeftRightKids(ctx, node)
	if err != nil {
		return
	}

	if left.Type != parser.QL_I64 && left.Type != parser.QL_STR {
		qlErr(ctx, "invalid kid nodes: %v and %v", left, right)
		return
	}

	res := types.Value{Type: parser.QL_BOOL}
	res.I64 = 0
	if left.Type == parser.QL_I64 {
		a := left.I64
		b := right.I64
		switch cmp {
		case parser.QL_CMP_EQ:
			if a == b {
				res.I64 = 1
			}
		case parser.QL_CMP_GE:
			if a == b || a > b {
				res.I64 = 1
			}
		case parser.QL_CMP_LE:
			if a == b || a < b {
				res.I64 = 1
			}
		case parser.QL_CMP_GT:
			if a > b {
				res.I64 = 1
			}
		case parser.QL_CMP_LT:
			if a < b {
				res.I64 = 1
			}
		}
		ctx.out = res
	} else if left.Type == parser.QL_STR {
		a := left.Str
		b := right.Str
		c := strings.Compare(string(a), string(b))
		switch cmp {
		case parser.QL_CMP_EQ:
			if c == 0 {
				res.I64 = 1
			}
		case parser.QL_CMP_GE:
			if c == 0 || c == 1 {
				res.I64 = 1
			}
		case parser.QL_CMP_LE:
			if c == 0 || c == -1 {
				res.I64 = 1
			}
		case parser.QL_CMP_GT:
			if c == 1 {
				res.I64 = 1
			}
		case parser.QL_CMP_LT:
			if c == -1 {
				res.I64 = 1
			}
		}
		ctx.out = res
	} else {
		qlErr(ctx, "invalid type: %d, expected string, number", left.Type)
	}
}

func evalLeftRightKids(ctx *QLEvalContext, node *parser.QLNode) (*types.Value, *types.Value, error) {
	ctxLeft := &QLEvalContext{env: ctx.env}
	qlEval(ctxLeft, node.Kids[0])
	if ctxLeft.err != nil {
		qlErr(ctx, "evaluating node: %v: err: %w", node.Kids[0], ctxLeft.err)
		return nil, nil, fmt.Errorf("evaluating node: %v: err: %w", node.Kids[0], ctxLeft.err)
	}

	if len(node.Kids) == 1 {
		return &ctxLeft.out, nil, nil
	}

	ctxRight := &QLEvalContext{env: ctx.env}
	qlEval(ctxRight, node.Kids[1])
	if ctxRight.err != nil {
		qlErr(ctx, "evaluating node: %v: err: %w", node.Kids[1], ctxLeft.err)
		return nil, nil, fmt.Errorf("evaluating node: %v: err: %w", node.Kids[1], ctxLeft.err)
	}

	if ctxLeft.out.Type != ctxRight.out.Type {
		qlErr(ctx, "comparing different types: %d and %d", ctxLeft.out.Type, ctxRight.out.Type)
		return nil, nil, fmt.Errorf("comparing different types: %d and %d", ctxLeft.out.Type, ctxRight.out.Type)
	}

	return &ctxLeft.out, &ctxRight.out, nil
}

func qlErr(ctx *QLEvalContext, args ...interface{}) {
	err := ctx.err
	if err != nil {
		format := err.Error() + " : " + args[0].(string)
		ctx.err = fmt.Errorf(format, args[1:]...)
	} else {
		ctx.err = fmt.Errorf(args[0].(string), args[1:]...)
	}
}

func QLEvalOutput(rec *types.Record, exprs []parser.QLNode, names []string) (*types.Record, error) {
	if len(names) > 0 && names[0] == "*" {
		return rec, nil
	}
	out := &types.Record{}
	for i, expr := range exprs {
		ctx := QLEvalContext{
			env: *rec,
		}
		qlEval(&ctx, expr)
		if ctx.err != nil {
			return nil, ctx.err
		}

		switch ctx.out.Type {
		case parser.QL_BOOL, parser.QL_I64:
			out.AddI64(names[i], ctx.out.I64)
		case parser.QL_STR:
			out.AddStr(names[i], ctx.out.Str)
		default:
			return nil, fmt.Errorf("invalid expression output type: %d", ctx.out.Type)
		}
	}
	return out, nil
}

func QLEvalRecordBool(rec *types.Record, expr parser.QLNode) error {
	ctx := QLEvalContext{
		env: *rec,
	}
	qlEval(&ctx, expr)
	if ctx.out.Type != parser.QL_BOOL {
		return fmt.Errorf("expression does not result in boolean outcome")
	}

	val := ctx.out
	if val.I64 == 0 {
		return fmt.Errorf("invalid")
	}
	return nil
}

func qlCreateTable(req *parser.QLCreateTable, tx *db.DBTX) error {
	return tx.TableNew(&req.Def)
}

func qlStmt(p *parser.Parser, r interface{}, tx *db.DBTX) (res interface{}, err error) {
	switch val := r.(type) {
	case *parser.QLCreateTable:
		err = qlCreateTable(val, tx)
	case *parser.QLSelect:
		sc, err := qlSelect(val, tx)
		if err != nil {
			return nil, err
		}
		resArr := []types.Record{}
		for sc.Valid() {
			rec, err := sc.Deref()
			if err != nil {
				return nil, err
			}
			resArr = append(resArr, *rec)
			sc.Next()
		}
		return resArr, nil

	case *parser.QLUpdate:
		err = qlUpdate(val, tx)

	case *parser.QLInsert:
		if val.IsUpsert {
			err = qlUpsert(val, tx)
		} else {
			err = qlInsert(val, tx)
		}

	case *parser.QLDelete:
		res, err = qlDelete(val, tx)

	default:
		return nil, fmt.Errorf("invalid result type: %T", val)
	}

	if err != nil {
		return nil, err
	}
	return res, nil
}

func qlInsert(req *parser.QLInsert, tx *db.DBTX) error {
	rec := types.Record{}
	err := populateRecord(&rec, req.Values, req.Name)
	if err != nil {
		return err
	}
	return tx.Insert(req.Table, rec)
}

func qlUpsert(req *parser.QLInsert, tx *db.DBTX) error {
	rec := types.Record{}
	err := populateRecord(&rec, req.Values, req.Name)
	if err != nil {
		return err
	}
	ok := tx.Upsert(req.Table, rec)
	if !ok {
		return fmt.Errorf("error upserting record")
	}
	return nil
}

func populateRecord(rec *types.Record, vals []parser.QLNode, names []string) error {
	for i, val := range vals {
		switch val.Type {
		case parser.QL_STR:
			rec.AddStr(names[i], val.Str)
		case parser.QL_I64:
			rec.AddI64(names[i], val.I64)
		default:
			return fmt.Errorf("invalid type: %d", val.Type)
		}
	}
	return nil
}

func qlDelete(req *parser.QLDelete, tx *db.DBTX) (int, error) {
	iterator, err := newQlScanFilter(&req.QLScan, tx)
	if err != nil {
		return 0, fmt.Errorf("getting scanner: %w", err)
	}
	count := 0
	for iterator.Valid() {
		rec, err := iterator.Deref()
		if err != nil {
			return count, fmt.Errorf("getting record: %w", err)
		}
		ok := tx.Delete(req.Table, rec)
		if !ok {
			return count, fmt.Errorf("unable to delete record: %v", rec)
		}
		iterator.Next()
		count++
	}
	return count, nil
}

func qlUpdate(req *parser.QLUpdate, tx *db.DBTX) error {
	iterator, err := newQlScanFilter(&req.QLScan, tx)
	if err != nil {
		return fmt.Errorf("getting scanner: %w", err)
	}

	tdef, err := db.GetTableDef(tx, req.Table)
	if err != nil {
		return fmt.Errorf("getting table definition: %w", err)
	}
	for _, col := range tdef.Cols {
		found := false
		for _, i := range req.Name {
			if i == col {
				found = true
				break
			}
		}
		if !found {
			val := parser.QLNode{}
			val.Type = parser.QL_SYM
			val.Str = []byte(col)
			req.Name = append(req.Name, col)
			req.Values = append(req.Values, val)
		}
	}

	for iterator.Valid() {
		rec, err := iterator.Deref()
		if err != nil {
			return err
		}
		newrec, err := QLEvalOutput(&rec, req.Values, req.Name)
		if err != nil {
			return err
		}
		reorderedRec := types.Record{}
		for i, col := range tdef.Cols {
			switch tdef.Types[i] {
			case types.TYPE_INT64:
				reorderedRec.AddI64(col, newrec.Get(col).I64)
			case types.TYPE_BYTES:
				reorderedRec.AddStr(col, newrec.Get(col).Str)
			default:
				return fmt.Errorf("invalid record type")
			}
		}
		ok := tx.Update(req.Table, reorderedRec)
		if !ok {
			return fmt.Errorf("error updating the table")
		}
		iterator.Next()
	}
	return nil
}

func ExecuteQuery(query string, database *db.DB) (interface{}, error) {
	tx := database.NewTX()
	p := &parser.Parser{
		Input: []byte(query),
	}
	stmt := parser.PStmt(p)
	if stmt == nil {
		return nil, fmt.Errorf("failed to parse statement or syntax error: %v", p.Err)
	}
	database.Begin(tx)
	res, err := qlStmt(p, stmt, tx)

	if err != nil {
		database.Abort(tx)
	} else {
		err = database.Commit(tx)
	}
	return res, err
}
