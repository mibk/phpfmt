package naive

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"slices"
	"strings"
	"text/tabwriter"

	"mibk.dev/phpfmt/token"
)

type Options uint8

const (
	// TrailingComma enables adding trailing commas in all [] and () scopes.
	TrailingComma Options = 1 << iota

	// AlignColumns causes Fprint to align elements in columns using spaces.
	AlignColumns

	// PHP74Compat switches formatting to PHP 7.4 compatibility mode.
	//
	// - This will remove support for trailing comma.
	// - The concat operator (.) is formatted according to PHP 7.4 precedence rules.
	PHP74Compat

	// Standard is the default, “standard” formatting style.
	Standard = TrailingComma | AlignColumns
)

// Fprint pretty-prints an AST node to w.
func Fprint(w io.Writer, node any, options Options) error {
	tw := tabwriter.NewWriter(w, 0, 0, 1, ' ', tabwriter.StripEscape)

	// TODO: Make it concurent safe?
	if options&PHP74Compat > 0 {
		options &= ^TrailingComma

		// Before PHP 8.0, the concatenation operator
		// had the same precedence as + or -.
		prev := opPrec[token.Concat]
		opPrec[token.Concat] = 6
		defer func() {
			opPrec[token.Concat] = prev
		}()
	}

	p := &printer{config: options}
	p.print(node)
	if p.err != nil {
		return p.err
	}

	buf := bufio.NewWriter(tw)
	justIndented := false
	var prevIndentation indentation
	var err error
	for _, tok := range p.tokens {
		if err != nil {
			return err
		}
		switch tok := tok.(type) {
		default:
			err = fmt.Errorf("unsupported type %T", tok)
		case token.Token:
			justIndented = false
			if strings.Contains(tok.Text, "\n") {
				buf.Flush()
				tw.Flush()
			}
			buf.WriteByte(tabwriter.Escape)
			_, err = buf.WriteString(tok.Text)
			buf.WriteByte(tabwriter.Escape)
		case indentation:
			justIndented = true
			if tok != prevIndentation {
				prevIndentation = tok
				buf.Flush()
				tw.Flush()
			}
			buf.WriteByte(tabwriter.Escape)
			for i := 0; i < int(tok); i++ {
				buf.WriteByte('\t')
			}
			err = buf.WriteByte(tabwriter.Escape)
		case whitespace:
			if tok == nextcol && options&AlignColumns == 0 {
				tok = space
			}
			if !justIndented || tok == newline {
				err = buf.WriteByte(byte(tok))
			}
		}
	}

	if err := buf.Flush(); err != nil {
		return err
	}
	return tw.Flush()
}

type indentation int

type printer struct {
	config Options

	tokens []any
	err    error // sticky

	prevIndent indentation
	indent     indentation

	alignNextAssign bool
	removeNextWS    bool
	rmWSBeforeParen bool
	mightDeindent   bool

	maxPrec           int
	rmSpaceAfterScope bool

	scopeType token.Type
	multiline bool
	scopeOpen token.Type
}

type whitespace byte

const (
	nextcol whitespace = '\v'
	newline whitespace = '\n'
	space   whitespace = ' '
)

func (p *printer) print(args ...any) {
	for _, arg := range args {
		if p.err != nil {
			return
		}

		switch arg := arg.(type) {
		default:
			p.err = fmt.Errorf("unsupported type %T", arg)
		case *File:
			if d := arg.htmlPreamble; d != nil {
				d.Text = strings.TrimLeft(d.Text, " \t\n")
				p.print(*d)
			}
			p.print(arg.scope)
			fixed := p.removeAnyWS()
			if d := p.removeLast(token.InlineHTML); d != nil {
				d := d.(token.Token)
				d.Text = strings.TrimRight(d.Text, " \t\n")
				p.print(d)
			}
			if !fixed {
				p.print(newline)
			}
		case *scope:
			switch arg.open {
			case token.Lparen:
				switch last := p.lastToken(); last {
				case token.Rparen, token.Rbrack,
					token.Declare, token.Class, token.Function, token.Fn:
					p.removeAnyWS()
				}
			case token.Lbrack:
				switch last := p.lastToken(); last {
				case token.Rparen, token.Rbrack:
					p.removeLast(space)
				}
			case token.Lbrace:
				if arg.kind == token.Lbrace {
					// For implicit blocks, do nothing.
					break
				}
				nl := p.removeAnyWS()
				switch arg.kind {
				case token.Arrow, token.DoubleColon:
				case token.OpenTag, token.Class, token.Interface, token.Trait, token.Enum,
					token.Function:
					if !nl {
						p.print(newline, p.indent)
					}
					p.alignNextAssign = false
				default:
					p.print(space)
				}
			}
			if arg.open != token.Lbrace && p.rmWSBeforeParen {
				p.removeLast(space)
			}
			p.print(arg.open)
			if arg.commentTag != nil {
				p.print(space, *arg.commentTag)
			}
			if arg.open == token.OpenTag {
				if arg.multiline {
					p.print(newline)
				} else {
					p.print(nextcol)
				}
			}
			if arg.indented {
				p.indent++
			}
			if arg.multiline && len(arg.nodes) > 0 {
				p.print(newline, p.indent)
			} else if arg.oneliner() {
				p.print(space)
			}

			m := p.multiline
			t := p.scopeType
			o := p.scopeOpen
			p.scopeType = arg.kind
			p.multiline = arg.multiline || arg.open == token.OpenTag
			p.scopeOpen = arg.open
			for _, x := range arg.nodes {
				p.print(x)
			}
			p.multiline = m
			p.scopeType = t
			p.scopeOpen = o

			// This prevents bad formatting of a switch stmt
			// ending with an empty branch.
			p.removeLast(p.indent)
			p.removeLast(newline)
			p.removeNextWS = false

			if arg.indented {
				p.indent--
			}

			if arg.oneliner() {
				p.removeLast(space)
				p.print(space)
			} else if arg.multiline || arg.offsetEndParen {
				if p.config&TrailingComma > 0 && arg.fixComma && len(arg.nodes) > 0 {
					p.removeLast(space)
					c := p.removeLast(token.Comment)
					p.removeLast(space)
					p.removeLast(nextcol)
					p.removeLast(token.Comma)
					if p.lastIsToken() {
						p.print(token.Comma)
					}
					if c != nil {
						p.print(c)
					}
				}
				p.print(newline, p.indent)
			} else {
				p.removeLast(space)
				p.removeLast(token.Comma)
			}
			if arg.kind == token.For && arg.close == token.Rparen {
				s1 := p.removeLast(token.Semicolon)
				s2 := p.removeLast(token.Semicolon)
				if s2 != nil {
					p.removeLast(space)
					paren := p.removeLast(token.Lparen)
					if paren != nil {
						// This is a special case for infinite loops.
						// Let's follow the K&R-derived style.
						p.print(token.Token{Type: token.Lparen, Text: "(;;"})
						s1 = nil
					} else {
						p.print(s2, space)
					}
				}
				if s1 != nil {
					p.print(s1)
				}
			}
			p.print(arg.close)
			if arg.close == token.Rbrace && !isFetchOperator(arg.kind) ||
				arg.close == token.Rparen && arg.kind != token.OpenTag {
				p.print(space)
			}
			p.rmWSBeforeParen = false
		case *ternaryMiddle:
			p.removeLast(space)
			p.print(space, token.Qmark, space)
			p.rmWSBeforeParen = false
			hasAny := false
			indented := false
			for _, x := range arg.nodes {
				tok, ok := x.(token.Token)
				if !ok || tok.Type != token.Whitespace {
					hasAny = true
				} else if !indented && tok.Type == token.Whitespace &&
					strings.Contains(tok.Text, "\n") {
					indented = true
					p.indent++
					*arg.extraIndented++
					*arg.doesContinue = true
				}
				p.print(x)
			}
			if arg.stmtAlreadyIndented {
				if p.removeLast(p.indent) != nil {
					p.print(p.indent - 1)
				}
			}
			p.removeLast(space)
			if hasAny {
				p.print(space)
			}
			p.print(token.Colon, space)
			p.rmWSBeforeParen = false
		case *stmt:
			var extraIndented indentation
			fatArrow := false
			stmtReallyIndented := false
			mightContinue := false
			doesContinue := false
			if arg.isLabel {
				if p.removeLast(p.indent) != nil {
					p.print(p.indent - 1)
				}
				p.indent--
				extraIndented--
			}
			maxPrec := -1
			p.maxPrec = maxPrec
			recalcPrecAfter := false
			hadSpecialParamChar := false
			for index, x := range arg.nodes {
				if maxPrec == -1 {
					if arg.kind == token.Class || arg.kind == token.Function || arg.kind == token.Fn {
						p.maxPrec = len(opTable)
					} else {
						maxPrec = analyseOps(arg.nodes)
						p.maxPrec = maxPrec
					}
				}
				addSpace := false
				switch x := x.(type) {
				case token.Token:
					if p.rmSpaceAfterScope {
						p.rmSpaceAfterScope = false
						p.removeNextWS = true
					}
					switch rest := arg.nodes[index+1:]; x.Type {
					case token.DoubleArrow, token.Assign:
						// With these, change the stmt kind to change
						// handling operator spacing.
						// TODO: Find a better solution?
						maxPrec = -1
						arg.kind = x.Type
					case token.Colon:
						p.removeLast(space)
					case token.Not:
						// A hack to add a space after the ! unary op,
						// to emphasize that instanceof has a higher precedence.
						if nextOperatorIs(rest, token.Instanceof) {
							addSpace = true
						}
					case token.At:
						// And this solves the infamous:
						//	- 2**2
						// and perhaps some others?
						// The @ token represents all unary operators.
						// See (*parser).parseStmt.
						if nextOperatorIs(rest, token.Pow) {
							addSpace = true
							p.maxPrec = max(p.maxPrec, 2)
						}
					case token.BitAnd, token.Ellipsis:
						hadSpecialParamChar = true
					case token.Var:
						if hadSpecialParamChar && arg.kind == token.Function {
							p.removeLast(space)
							ell := p.removeLast(token.Ellipsis)
							p.removeLast(space)
							amp := p.removeLast(token.BitAnd)
							p.removeLast(space)
							if p.lastToken() != token.Lparen {
								p.print(space)
							}
							if amp != nil {
								// Disguise the token so no blank is added after it.
								amp := amp.(token.Token)
								amp.Type = token.At
								p.print(amp)
							}
							if ell != nil {
								p.print(ell)
							}
						}
					}

					if !p.multiline {
						if x.Type == token.Whitespace && strings.Contains(x.Text, "\n") {
							stmtReallyIndented = true
						}
						p.print(x)
						if addSpace {
							p.print(space)
						}
						continue
					}
					switch x.Type {
					case token.Comment:
						if !arg.isLabel || !isLineComment(x) {
							break
						}
						if p.prevIndent > p.indent {
							p.removeLast(p.indent)
							p.print(p.indent + 1)
						}
					case token.DocComment:
					case token.Whitespace:
						if !strings.Contains(x.Text, "\n") {
							break
						}
						if fatArrow {
							p.indent++
							extraIndented++
						} else if !doesContinue && mightContinue {
							p.indent++
							extraIndented++
							doesContinue = true
						}
					case token.Colon:
						mightContinue = false
						if doesContinue {
							p.indent--
							extraIndented--
							doesContinue = false
						}
					case token.DoubleArrow:
						fatArrow = true
					default:
						fatArrow = false
						mightContinue = true
					}
				case *ternaryMiddle:
					p.maxPrec = analyseOps(x.nodes)
					recalcPrecAfter = true
					x.stmtAlreadyIndented = doesContinue || stmtReallyIndented
					x.extraIndented = &extraIndented
					x.doesContinue = &doesContinue
				case *scope:
					maxPrec = -1
					if doesContinue && x.multiline && x.open == token.Lbrace {
						p.indent--
						extraIndented--
						doesContinue = false
						mightContinue = false
					}
				}
				p.print(x)
				if addSpace {
					p.print(space)
				}
				if recalcPrecAfter {
					recalcPrecAfter = false
					maxPrec = analyseOps(arg.nodes[index:])
					p.maxPrec = maxPrec
				}
			}
			p.indent -= extraIndented
			if arg.isLabel {
				p.print(newline, p.indent)
				p.removeNextWS = true
			}
			p.alignNextAssign = false
			switch arg.kind {
			case token.Namespace, token.Declare:
				p.print(newline, newline, p.indent)
				p.removeNextWS = true
			}
		case token.Token:
			if arg.Type == token.Whitespace {
				if i := strings.LastIndexByte(arg.Text, '\n'); i >= 0 {
					p.prevIndent = 0
					for _, r := range arg.Text[i+1:] {
						if r != '\t' {
							break
						}
						p.prevIndent++
					}
					if p.removeNextWS {
						continue
					}
					if strings.Contains(arg.Text[:i], "\n") {
						p.print(newline)
					}
					p.print(newline, p.indent)
				} else if !p.removeNextWS {
					p.removeLast(space)
					// Do not print space after Foo::{$expr}
					if p.lastToken() != token.Rbrace {
						p.print(space)
					}
				}
				continue
			}
			p.removeNextWS = false
			p.rmWSBeforeParen = false
			printSpaceAfter := false
			switch arg.Type {
			case token.Illegal:
				log.Printf("WARN: unknown token: %q", arg.Text)
			case token.OpenTag:
				printSpaceAfter = true
			case token.Comment:
				if !isLineComment(arg) {
					// TODO: Or ensure spaces around always?
					if last := p.lastToken(); last != token.Lparen && last != token.Lbrack {
						p.removeLast(space)
						p.print(space)
					}
					printSpaceAfter = true
					break
				}
				if s := strings.TrimSpace(arg.Text); s == "//" &&
					p.lastToken() != token.Comment {
					// Ignore this one.
					continue
				}
				p.mightDeindent = p.prevIndent < p.indent
				p.removeLast(space)
				if !p.justIndented() {
					p.print(nextcol)
				}
			case token.If:
				if p.lastToken() == token.Else {
					p.removeAnyWS()
				}
			case token.Else, token.Catch, token.Finally:
				if p.lastToken() == token.Rbrace {
					p.removeAnyWS()
					p.print(space)
				}
			case token.Use:
				p.removeLast(space)
				if r := p.removeLast(token.Rparen); r != nil {
					p.print(r, space)
				}
			case token.Const, token.Case:
				p.alignNextAssign = true
			case token.Static:
				p.rmWSBeforeParen = true
				fallthrough
			case token.Private, token.Protected, token.Public,
				token.Readonly, token.Final:
				if p.scopeOpen == token.Lbrace {
					p.alignNextAssign = true
				}
			case token.Assign:
				p.removeLast(space)
				if p.alignNextAssign {
					p.print(nextcol)
				} else if p.scopeType != token.Declare {
					p.print(space)
				}
			case token.DoubleArrow:
				p.removeLast(space)
				if p.multiline {
					p.print(nextcol)
				} else {
					p.print(space)
				}
			case token.Semicolon, token.Comma:
				p.removeLast(space)
			case token.Backslash:
				if ws := p.removeLast(space); ws != nil && p.lastToken() != token.Ident {
					p.print(ws)
				}
				p.removeNextWS = true
			case token.Arrow, token.QmarkArrow, token.DoubleColon:
				p.removeLast(space)
				fallthrough
			case token.Qmark, token.BitNot, token.At, token.Not, token.Dollar, token.Ellipsis:
				p.removeNextWS = true
			case metaTokenCast:
				switch last := p.lastToken(); last {
				// TODO: The ] feels like a hack,
				// and it's duplicated for ++ -- below.
				case token.Ident, token.Var, token.Rbrack:
					p.removeLast(space)
				}
				if add, ok := decideOpSpaces(p.maxPrec, arg.Type); ok {
					printSpaceAfter = add
					p.removeNextWS = !add
				}
			case token.Var:
				if last := p.lastToken(); last == token.Ident {
					p.removeLast(space)
					p.print(space)
				}
				fallthrough
			case token.Ident:
				p.rmWSBeforeParen = true
			case token.Inc, token.Dec:
				switch last := p.lastToken(); last {
				// TODO: The ] feels like a hack.
				// Once formatting operators is done,
				// remove it.
				case token.Ident, token.Var, token.Rbrack:
					p.removeLast(space)
				default:
					p.removeNextWS = true
				}
			default:
				if add, ok := decideOpSpaces(p.maxPrec, arg.Type); ok {
					p.removeLast(space)
					if add {
						p.print(space)
						printSpaceAfter = true
					} else {
						p.removeNextWS = true
					}
				} else if spacesAround(arg.Type) {
					p.removeLast(space)
					p.print(space)
				}
			}

			arg.Pos = token.Pos{}
			p.tokens = append(p.tokens, arg)

			switch arg.Type {
			case token.Assign:
				if p.scopeType == token.Declare {
					p.removeNextWS = true
					break
				}
				fallthrough
			case token.Comma:
				p.print(space)
			default:
				if !p.removeNextWS && (printSpaceAfter || spaceAfter(arg.Type)) {
					p.print(space)
				}
			}
		case token.Type:
			if arg == token.EOF {
				break
			}
			p.tokens = append(p.tokens, token.Token{Type: arg, Text: arg.String()})
		case indentation:
			p.tokens = append(p.tokens, arg)
		case whitespace:
			if arg == newline {
				p.removeLast(space)
			}
			p.tokens = append(p.tokens, arg)
		}
	}
}

func (p *printer) lastToken() token.Type {
	for _, tok := range slices.Backward(p.tokens) {
		if _, ok := tok.(whitespace); ok {
			continue
		}
		if _, ok := tok.(indentation); ok {
			continue
		}
		t, ok := tok.(token.Token)
		if !ok {
			break
		}
		return t.Type
	}
	return token.Illegal
}

func (p *printer) lastIsToken() bool {
	for _, tok := range slices.Backward(p.tokens) {
		_, ok := tok.(token.Token)
		return ok
	}
	return false
}

func (p *printer) justIndented() bool {
	for _, tok := range slices.Backward(p.tokens) {
		_, ok := tok.(indentation)
		return ok
	}
	return false
}

func (p *printer) removeAnyWS() (fixed bool) {
	for len(p.tokens) > 0 {
		i := len(p.tokens) - 1
		_, isWS := p.tokens[i].(whitespace)
		_, isIndent := p.tokens[i].(indentation)
		if !isWS && !isIndent {
			if c, ok := p.tokens[i].(token.Token); ok && isLineComment(c) {
				p.print(newline, p.indent)
				return true
			}
			return false
		}
		p.tokens = p.tokens[:i]
	}
	return false
}

func (p *printer) removeLast(tok any) any {
	if len(p.tokens) == 0 {
		return nil
	}

	last := p.tokens[len(p.tokens)-1]
	if last == tok {
		p.tokens = p.tokens[:len(p.tokens)-1]
		return last
	}
	if typ, ok := tok.(token.Type); ok {
		if lastTok, ok := last.(token.Token); ok && lastTok.Type == typ {
			p.tokens = p.tokens[:len(p.tokens)-1]
			return last
		}
	}
	return nil
}

func isLineComment(tok token.Token) bool {
	return tok.Type == token.Comment && !strings.HasPrefix(tok.Text, "/*")
}

func spacesAround(typ token.Type) bool {
	switch typ {
	case token.As,
		token.Implements,
		token.Instanceof,
		token.Insteadof,
		token.Coalesce,
		token.AddAssign,
		token.SubAssign,
		token.MulAssign,
		token.QuoAssign,
		token.RemAssign,
		token.PowAssign,
		token.AndAssign,
		token.OrAssign,
		token.XorAssign,
		token.ShlAssign,
		token.ShrAssign,
		token.ConcatAssign,
		token.CoalesceAssign,
		token.LowPrecAnd,
		token.LowPrecOr,
		token.LowPrecXor:
		return true
	default:
		return false
	}
}

func spaceAfter(typ token.Type) bool {
	switch typ {
	case token.Colon:
		fallthrough
	case token.Abstract,
		token.Case,
		token.Catch,
		token.Clone,
		token.Do,
		token.DoubleArrow,
		token.Echo,
		token.Extends,
		token.Final,
		token.Finally,
		token.For,
		token.Foreach,
		token.From,
		token.Function,
		token.Global,
		token.If,
		token.Match,
		token.Namespace,
		token.New,
		token.Print,
		token.Private,
		token.Protected,
		token.Public,
		token.Readonly,
		token.Return,
		token.Semicolon,
		token.Static,
		token.Switch,
		token.Throw,
		token.Try,
		token.Use,
		token.While,
		token.Yield:
		return true
	default:
		return spacesAround(typ)
	}
}
