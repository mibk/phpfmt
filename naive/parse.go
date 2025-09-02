package naive

import (
	"cmp"
	"fmt"
	"io"
	"slices"
	"strings"

	"mibk.dev/phpfmt/token"
)

const (
	metaTokenCast = 1024 + iota
)

// SyntaxError records an error and the position it occurred on.
type SyntaxError struct {
	Line, Column int
	Err          error
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("line:%d:%d: %v", e.Line, e.Column, e.Err)
}

type parser struct {
	scan *token.Scanner

	err  error
	tok  token.Token
	prev token.Token
	alt  *token.Token // on backup
}

// Parse parses a single PHP file. If an error occurs while parsing
// (except io errors), the returned error will be of type *SyntaxError.
func Parse(r io.Reader, php74Compat bool) (*File, error) {
	p := &parser{scan: token.NewScanner(r, php74Compat)}
	p.next() // init
	doc := p.parseFile()
	if p.err != nil {
		return nil, p.err
	}
	return doc, nil
}

func (p *parser) next() {
	if p.tok.Type == token.EOF {
		return
	}
	if p.alt != nil {
		p.tok, p.alt = *p.alt, nil
		return
	}
	p.tok = p.scan.Next()
	if p.tok.Type == token.EOF && p.err == nil {
		err := p.scan.Err()
		if se, ok := err.(*token.ScanError); ok {
			// Make sure we always return *SyntaxError.
			p.err = &SyntaxError{
				Line:   se.Pos.Line,
				Column: se.Pos.Column,
				Err:    se.Err,
			}
		} else if err != nil {
			p.errorf("scan: %v", err)
		}
	}
}

func (p *parser) got(typ token.Type) bool {
	if p.tok.Type == typ {
		p.next()
		return true
	}
	return false
}

func (p *parser) errorf(format string, args ...interface{}) {
	if p.err == nil {
		p.tok.Type = token.EOF
		se := &SyntaxError{Err: fmt.Errorf(format, args...)}
		se.Line, se.Column = p.tok.Pos.Line, p.tok.Pos.Column
		p.err = se
	}
}

func (p *parser) parseFile() *File {
	file := new(File)
	if text := p.tok; p.got(token.InlineHTML) {
		file.htmlPreamble = &text
	}
	if !p.got(token.OpenTag) {
		p.errorf("expecting %v, found %v", token.OpenTag, p.tok)
		return nil
	}

	file.scope = p.parseScope(token.Illegal, token.OpenTag)
	file.scope.indented = false
	file.scope.offsetEndParen = false
	return file
}

func (p *parser) parseScope(kind, open token.Type) (s *scope) {
	defer func() {
		if s.open == token.Lbrace && s.kind != token.Fn && (len(s.nodes) == 0 || !isFetchOperator(s.kind)) {
			s.multiline = true
		}
		if s.multiline {
			s.indented = true
		}
	}()
	s = &scope{kind: kind, open: open}
	switch open {
	default:
		panic(fmt.Sprintf("unknown pair for %v", open))
	case token.OpenTag:
		s.close = token.EOF
	case token.Lbrace:
		s.close = token.Rbrace
		if kind == token.Match {
			s.fixComma = true
		}
	case token.Lparen:
		s.close = token.Rparen
		switch kind {
		case token.Ident, token.Var:
			kind = token.Illegal
			fallthrough
		case token.Function:
			s.fixComma = true
		}
	case token.Lbrack:
		s.close = token.Rbrack
		s.fixComma = true
	}

	if p.tok.Type == token.Whitespace {
		s.multiline = strings.Contains(p.tok.Text, "\n")
		p.next()
	}
	if !s.multiline && p.tok.Type == token.Comment && isLineComment(p.tok) {
		s.commentTag = new(token.Token)
		*s.commentTag = p.tok
		p.next()
		p.got(token.Whitespace)
		s.multiline = true
	}

	sep := token.Semicolon
	if s.fixComma {
		sep = token.Comma
	}
	for {
		stmt := p.parseStmt(sep)
		if tsep := p.tok; p.got(sep) {
			stmt.nodes = append(stmt.nodes, tsep)
		}
		if len(stmt.nodes) > 0 {
			if p.tok.Type == token.Whitespace && !strings.Contains(p.tok.Text, "\n") {
				p.next()
				// Attach trailing comment.
				if isLineComment(p.tok) {
					stmt.nodes = append(stmt.nodes, p.tok)
					p.next()
				}
			}
			s.nodes = append(s.nodes, stmt)
		}
		if s.open != token.Lbrace {
			stmt.isLabel = false
		}
		if stmt.multiline {
			s.indented = true
		}

		if s.open == token.Lparen && s.kind == token.Function {
			stmt.kind = token.Function
		} else if s.open == token.Lbrace && s.kind == token.Class {
			stmt.kind = token.Class
		}

		switch typ := p.tok.Type; typ {
		case s.close:
			s.offsetEndParen = s.indented && stmt.trailingNL
			p.next()
			return s
		case token.EOF, token.Rparen, token.Rbrace, token.Rbrack:
			p.errorf("unexpected %v", typ)
			return s
		}
	}
	return s
}

func (p *parser) parseStmt(separators ...token.Type) (s *stmt) {
	s = new(stmt)
	nextScope := token.OpenTag
	for {
		if p.tok.Type.IsKeyword() {
			switch last := s.lastTok(); last {
			case token.Arrow, token.DoubleColon, token.Function, token.Const:
				p.tok.Type = token.Ident
			}
		}
		switch typ := p.tok.Type; typ {
		case token.EOF, token.Rparen, token.Rbrace, token.Rbrack:
			if len(s.nodes) > 0 {
				if tok, ok := s.nodes[len(s.nodes)-1].(token.Token); ok && tok.Type == token.Whitespace {
					s.nodes = s.nodes[:len(s.nodes)-1]
					s.trailingNL = strings.Contains(tok.Text, "\n")
				}
			}
			return s
		case token.OpenTag:
			s.nodes = append(s.nodes, p.tok)
			p.next()
			return s
		case token.Declare,
			token.Namespace,
			token.Class, token.Interface, token.Trait, token.Enum,
			token.Function, token.Fn,
			token.If, token.Else, token.Switch, token.Match,
			token.For, token.Foreach, token.Do, token.While,
			token.Try, token.Catch, token.Finally,
			token.Hash, token.Arrow, token.DoubleColon:
			nextScope = typ
			s.kind = cmp.Or(s.kind, typ)
			s.nodes = append(s.nodes, p.tok)
			p.next()
		case token.Lparen:
			scope := nextScope
			for _, v := range slices.Backward(s.nodes) {
				switch tok, _ := v.(token.Token); tok.Type {
				case token.Whitespace:
					continue
				case token.Echo, token.Print, token.Static:
					scope = token.Ident
				case token.Ident, token.Var:
					if nextScope != token.Function {
						scope = tok.Type
					}
				case token.Class, token.Function:
					// Let's use something that always places { on the same line.
					nextScope = token.Fn
				}
				break
			}
			p.next()
			sub := p.parseScope(scope, typ)
			if sub.close == token.Rparen && len(sub.nodes) == 1 {
				stmt := sub.nodes[0]
				if len(stmt.nodes) == 1 {
					tok, ok := stmt.nodes[0].(token.Token)
					if ok && canUseAsCast(tok) {
						s.nodes = append(s.nodes, token.Token{Type: metaTokenCast, Text: "(" + tok.Text + ")"})
						break
					}
				}
			}
			s.nodes = append(s.nodes, sub)
		case token.Lbrace, token.Lbrack:
			s.kind = cmp.Or(s.kind, typ)
			p.next()
			sub := p.parseScope(nextScope, typ)
			s.nodes = append(s.nodes, sub)
			if typ == token.Lbrace {
				// In most cases, } marks an end of a statement.
				// There are some exceptions.
				switch {
				case isFetchOperator(sub.kind),
					sub.kind == token.Match,
					sub.kind == token.Fn:
					continue
				case sub.kind == token.Do:
					if p.tok.Type == token.Whitespace {
						p.next()
					}
					continue
				}
				return s
			} else if typ == token.Lbrack && s.kind == token.Hash {
				return s
			}
		case token.Qmark:
			qmark := p.tok
			p.next()
			m := p.parseStmt(token.Colon, token.Semicolon, token.Comma)
			if p.got(token.Colon) {
				s.nodes = append(s.nodes, &ternaryMiddle{nodes: m.nodes})
			} else {
				s.nodes = append(s.nodes, qmark)
				s.nodes = append(s.nodes, m.nodes...)
			}
		case token.Colon:
			if slices.Contains(separators, typ) {
				return s
			}

			// A colon changes the meaning of the previous token.
			// E.g., foo(return: true) is valid; "return" in this context
			// is a regular named parameter.
			// Note that this also switches off the default case,
			// but that's OK; it is a kind of a label anyway.
			for i, v := range slices.Backward(s.nodes) {
				switch tok, _ := v.(token.Token); {
				case tok.Type == token.Whitespace:
					continue
				case tok.Type.IsKeyword():
					tok.Type = token.Ident
					s.nodes[i] = tok
				}
				break
			}
			s.nodes = append(s.nodes, p.tok)
			p.next()
			for _, v := range s.nodes {
				switch tok, _ := v.(token.Token); tok.Type {
				case token.Whitespace, token.Comment:
					continue
				case token.Ident, token.Case:
					s.isLabel = true
				}
				break
			}
			if s.isLabel {
				return s
			}
		case token.BitAnd, token.Add, token.Sub:
			switch last := s.lastTok(); {
			case last == token.Illegal, last == token.Colon,
				isOperator(last),
				last.IsKeyword():
				// A hack to consider it a unary op.
				p.tok.Type = token.At
			}
			s.nodes = append(s.nodes, p.tok)
			p.next()
		case token.Whitespace:
			if strings.Contains(p.tok.Text, "\n") {
				s.multiline = true
			}
			fallthrough
		default:
			if slices.Contains(separators, typ) {
				return s
			}
			s.nodes = append(s.nodes, p.tok)
			p.next()
		}
	}
}
