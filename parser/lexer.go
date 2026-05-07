package parser

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

var KeyWords = []string{
	"create", "table", "select", "index", "by", "upsert",
	"insert", "filter", "into", "delete", "from", "update", ",",
	"(", ")", "-", "as", "or", "and", "+", "*", "/", "%", ";",
	">=", "<=", "==", "=", ">", "<", "offset", "limit", "primary", "key",
	"values", "set",
}

func isValidKeyword(s string) bool {
	for _, tkn := range KeyWords {
		if strings.EqualFold(s, tkn) {
			return true
		}
	}
	return false
}

type Parser struct {
	Input []byte
	Idx   int
	Err   error
}

func (p *Parser) skipSpace() {
	for p.Idx < len(p.Input) && unicode.IsSpace(rune(p.Input[p.Idx])) {
		p.Idx++
	}
}

func peekNextToken(p *Parser) string {
	p.skipSpace()
	start := p.Idx
	end := start + 1
	for end < len(p.Input) && (isStr(p.Input[end]) || isNum(p.Input[end])) {
		end++
	}
	return string(p.Input[start:end])
}

func pkeyword(p *Parser, kwds ...string) bool {
	save := p.Idx
	for _, kw := range kwds {
		p.skipSpace()
		start := p.Idx
		end := start + len(kw)

		if end > len(p.Input) {
			p.Idx = save
			return false
		}

		ok := strings.EqualFold(string(p.Input[start:end]), kw)
		sym := isValidKeyword(strings.ToLower(kw))

		if !ok || !sym {
			p.Idx = save
			return false
		}
		p.Idx = end
	}
	return true
}

func isSym(ch byte) bool {
	r := rune(ch)
	return unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_'
}

func isSymStart(ch byte) bool {
	r := rune(ch)
	return r == '@'
}

func isStr(ch byte) bool {
	r := rune(ch)
	return unicode.IsLetter(r) || unicode.IsNumber(r)
}

func isNum(ch byte) bool {
	r := rune(ch)
	return unicode.IsNumber(r)
}

func pNum(p *Parser, node *QLNode) bool {
	p.skipSpace()
	ptr := p.Idx
	if ptr >= len(p.Input) {
		return false
	}
	for ptr < len(p.Input) && isNum(p.Input[ptr]) {
		ptr++
	}

	node.Type = QL_I64
	num, err := strconv.Atoi(string(p.Input[p.Idx:ptr]))
	if err != nil {
		return false
	}
	node.I64 = int64(num)
	p.Idx = ptr
	return true
}

func pStr(p *Parser, node *QLNode) bool {
	p.skipSpace()
	ptr := p.Idx
	if ptr >= len(p.Input) {
		return false
	}

	for ptr < len(p.Input) && isStr(p.Input[ptr]) {
		ptr++
	}
	if isValidKeyword(strings.ToLower(string(p.Input[p.Idx:ptr]))) {
		return false
	}
	node.Type = QL_STR
	node.Str = p.Input[p.Idx:ptr]
	p.Idx = ptr
	return true
}

func pSym(p *Parser, node *QLNode) bool {
	p.skipSpace()
	ptr := p.Idx
	if ptr >= len(p.Input) || !isSymStart(p.Input[ptr]) {
		return false
	}
	p.Idx++
	ptr++
	for ptr < len(p.Input) && isSym(p.Input[ptr]) {
		ptr++
	}
	if isValidKeyword(strings.ToLower(string(p.Input[p.Idx:ptr]))) {
		return false
	}

	node.Type = QL_SYM
	node.Str = p.Input[p.Idx:ptr]
	p.Idx = ptr
	return true
}

func pMustSym(p *Parser) string {
	p.skipSpace()
	ptr := p.Idx
	if ptr >= len(p.Input) {
		return ""
	}
	for ptr < len(p.Input) && isSym(p.Input[ptr]) {
		ptr++
	}
	sym := string(p.Input[p.Idx:ptr])
	p.Idx = ptr
	return sym
}

func pErr(p *Parser, node *QLNode, msg string) {
	if p.Err == nil {
		p.Err = fmt.Errorf("%s", msg)
	} else {
		p.Err = fmt.Errorf("%w: %s", p.Err, msg)
	}
}
