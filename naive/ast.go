package naive

import (
	"slices"

	"mibk.dev/phpfmt/token"
)

type File struct {
	htmlPreamble *token.Token
	scope        *scope
}

type scope struct {
	kind           token.Type
	open, close    token.Type
	commentTag     *token.Token
	multiline      bool
	offsetEndParen bool
	indented       bool
	fixComma       bool
	nodes          []*stmt
}

type stmt struct {
	kind       token.Type
	isLabel    bool
	multiline  bool
	trailingNL bool
	nodes      []any
}

func (s *scope) oneliner() bool {
	return s.open == token.Lbrace && !s.multiline && !isFetchOperator(s.kind) && len(s.nodes) > 0
}

func (s *stmt) lastTok() token.Type {
	for _, x := range slices.Backward(s.nodes) {
		tok, ok := x.(token.Token)
		if !ok {
			if _, ok := x.(*ternaryMiddle); ok {
				return token.Colon
			}
			// TODO: A better one?
			return token.Rparen
		}
		if tok.Type == token.Whitespace {
			continue
		}
		return tok.Type
	}
	return token.Illegal
}

type ternaryMiddle struct {
	stmtAlreadyIndented bool
	extraIndented       *indentation
	doesContinue        *bool
	nodes               []any
}

func isFetchOperator(typ token.Type) bool {
	return typ == token.Arrow || typ == token.DoubleColon
}

func canUseAsCast(tok token.Token) bool {
	switch tok.Text {
	case "bool", "int", "float", "string", "array", "object", "unset":
		return tok.Type == token.Ident
	default:
		return false
	}
}
