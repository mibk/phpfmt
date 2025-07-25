package phpdoc

import (
	"fmt"
	"io"
	"strings"

	"mibk.dev/phpfmt/phpdoc/internal/token"
	"mibk.dev/phpfmt/phpdoc/phptype"
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

// Parse parses a single PHPDoc comment.
func Parse(r io.Reader) (*Block, error) {
	p := &parser{scan: token.NewScanner(r)}
	p.next0() // init
	doc := p.parseDoc()
	if p.err != nil {
		return nil, p.err
	}
	return doc, nil
}

func (p *parser) backup() {
	if p.alt != nil {
		panic("cannot backup twice")
	}
	p.alt = new(token.Token)
	*p.alt = p.tok
	p.tok = p.prev
}

func (p *parser) next0() {
	if p.tok.Type == token.EOF {
		return
	}
	if p.alt != nil {
		p.tok, p.alt = *p.alt, nil
		return
	}
	p.tok = p.scan.Next()
}

// next is like next0 but skips whitespace.
func (p *parser) next() {
	p.prev = p.tok
	p.next0()
	p.consume(token.Whitespace)
}

func (p *parser) expect(typ token.Type) {
	if p.tok.Type != typ {
		p.errorf("expecting %v, found %v", typ, p.tok)
	}
	p.next()
}

func (p *parser) got(typ token.Type) bool {
	if p.tok.Type == typ {
		p.next()
		return true
	}
	return false
}

func (p *parser) consume(types ...token.Type) {
	if len(types) == 0 {
		panic("no token types to consume provided")
	}

	for ; len(types) > 0; types = types[1:] {
		if p.tok.Type == types[0] {
			p.next0()
		}
	}
}

func (p *parser) errorf(format string, args ...interface{}) {
	if p.err == nil {
		p.tok.Type = token.EOF
		se := &SyntaxError{Err: fmt.Errorf(format, args...)}
		se.Line, se.Column = p.tok.Pos.Line, p.tok.Pos.Column
		p.err = se
	}
}

// The syntax comments roughly follow the notation as defined at
// https://golang.org/ref/spec#Notation.

// PHPDoc = "/**" [ newline ] Line { newline Line } [ newline ] "*/" .
func (p *parser) parseDoc() *Block {
	doc := new(Block)
	for {
		p.consume(token.Newline)
		if p.tok.Type != token.Whitespace {
			break
		}
		doc.Indent = p.tok.Text
		p.next0()
	}
	p.expect(token.OpenDoc)
	if !p.got(token.Newline) {
		doc.PreferOneline = true
	}
	doc.Lines = p.parseLines()
	p.expect(token.CloseDoc)
	return doc
}

func (p *parser) parseLines() []Line {
	var lines []Line
	for p.tok.Type != token.CloseDoc {
		lines = append(lines, p.parseLine())
		if !p.got(token.Newline) {
			break
		}
	}
	return lines
}

// Line     = [ asterisk ] ( TextLine | Tag ) .
// TextLine = Desc .
func (p *parser) parseLine() Line {
	p.consume(token.Whitespace)
	var b strings.Builder
	if p.tok.Type == token.Asterisk {
		b.WriteString(p.tok.Text)
		p.next0()
	}
	if p.tok.Type == token.Whitespace {
		b.WriteString(p.tok.Text)
		p.next0()
	}
	if p.tok.Type == token.Tag {
		return p.parseTag()
	} else {
		return &TextLine{Value: p.parseDesc(&b)}
	}
}

// Tag = ParamTag |
//
//	ReturnTag |
//	PropertyTag |
//	MethodTag |
//	VarTag |
//	ThrowsTag |
//	ImplementsTag |
//	TemplateTag |
//	TypeDefTag |
//	OtherTag .
func (p *parser) parseTag() Tag {
	name := p.tok.Text
	p.expect(token.Tag)

	switch name {
	case "@param":
		return p.parseParamTag()
	case "@return":
		return p.parseReturnTag()
	case "@property", "@property-read", "@property-write":
		return p.parsePropertyTag(name)
	case "@method":
		return p.parseMethodTag()
	case "@var":
		return p.parseVarTag()
	case "@throws":
		return p.parseThrowsTag()
	case "@extends":
		return p.parseExtendsTag()
	case "@implements":
		return p.parseImplementsTag()
	case "@uses":
		return p.parseUsesTag()
	case "@template":
		return p.parseTemplateTag()
	case "@phpstan-type":
		return p.parseTypeDefTag()
	default:
		return p.parseOtherTag(name[1:])
	}
}

// ParamTag = "@param" Param [ Desc ] .
func (p *parser) parseParamTag() *ParamTag {
	tag := new(ParamTag)
	tag.Param = p.parseParam(true)
	tag.Desc = p.parseDesc(nil)
	return tag
}

// ReturnTag = "@return" PHPType [ Desc ] .
func (p *parser) parseReturnTag() *ReturnTag {
	tag := new(ReturnTag)
	tag.Type = p.parseType()
	tag.Desc = p.parseDesc(nil)
	return tag
}

// PropertyTag = ( "@property" | "@property-read" | "@property-write" ) PHPType varname [ Desc ] .
func (p *parser) parsePropertyTag(name string) *PropertyTag {
	tag := new(PropertyTag)
	tag.Type = p.parseType()
	tag.Var = strings.TrimPrefix(p.tok.Text, "$")
	p.expect(token.Var)
	tag.Desc = p.parseDesc(nil)

	switch {
	case strings.HasSuffix(name, "-read"):
		tag.ReadOnly = true
	case strings.HasSuffix(name, "-write"):
		tag.WriteOnly = true
	}
	return tag
}

// MethodTag = "@method" [ PHPType ] ident "(" [ ParamList [ "," ] ] ")" [ Desc ] .
func (p *parser) parseMethodTag() *MethodTag {
	tag := new(MethodTag)
	tag.Static = p.got(token.Static)
	tag.Result = p.parseType()
	tag.Name = p.tok.Text
	if !p.got(token.Ident) {
		if id, ok := tag.Result.(*phptype.Named); ok && !id.Global && len(id.Parts) == 1 {
			// Result type wasn't defined, and we we thought
			// was the result type was actually the method name.
			tag.Result = nil
			tag.Name = id.Parts[0]
		} else {
			p.expect(token.Ident)
		}
	}
	p.expect(token.Lparen)
	tag.Params = p.parseParamList()
	if p.got(token.Colon) {
		// Warn about putting result type *after* param list.
		p.errorf("unexpected %v, expecting description", token.Colon)
	}
	tag.Desc = p.parseDesc(nil)
	return tag
}

// VarTag = "@var" PHPType [ varname ] [ Desc ] .
func (p *parser) parseVarTag() *VarTag {
	tag := new(VarTag)
	tag.Type = p.parseType()
	if p.tok.Type == token.Var {
		tag.Var = p.tok.Text[1:]
		p.next()
	}
	tag.Desc = p.parseDesc(nil)
	return tag
}

// ThrowsTag = "@throws" PHPType [ Desc ] .
func (p *parser) parseThrowsTag() *ThrowsTag {
	tag := new(ThrowsTag)
	tag.Class = p.parseType()
	tag.Desc = p.parseDesc(nil)
	return tag
}

// ExtendsTag = "@extends" PHPType [ Desc ] .
func (p *parser) parseExtendsTag() *ExtendsTag {
	tag := new(ExtendsTag)
	tag.Class = p.parseType()
	tag.Desc = p.parseDesc(nil)
	return tag
}

// ImplementsTag = "@implements" PHPType [ Desc ] .
func (p *parser) parseImplementsTag() *ImplementsTag {
	tag := new(ImplementsTag)
	tag.Interface = p.parseType()
	tag.Desc = p.parseDesc(nil)
	return tag
}

// UsesTag = "@uses" PHPType [ Desc ] .
func (p *parser) parseUsesTag() *UsesTag {
	tag := new(UsesTag)
	tag.Trait = p.parseType()
	tag.Desc = p.parseDesc(nil)
	return tag
}

// TemplateTag = "@template" ident [ ( "of | "as" ) PHPType ] [ Desc ] .
func (p *parser) parseTemplateTag() *TemplateTag {
	tag := new(TemplateTag)
	tag.Param = p.tok.Text
	p.expect(token.Ident)
	if p.tok.Type == token.Ident && (p.tok.Text == "of" || p.tok.Text == "as") {
		p.next()
		tag.Bound = p.parseType()
	}
	tag.Desc = p.parseDesc(nil)
	return tag
}

// TypeDefTag = "@phpstan-type" ident PHPType [ Desc ] .
func (p *parser) parseTypeDefTag() *TypeDefTag {
	tag := new(TypeDefTag)
	tag.Name = p.tok.Text
	p.expect(token.Ident)
	tag.Type = p.parseType()
	tag.Desc = p.parseDesc(nil)
	return tag
}

// OtherTag = tagname [ Desc ] .
func (p *parser) parseOtherTag(name string) *OtherTag {
	tag := &OtherTag{Name: name}
	tag.Desc = p.parseDesc(nil)
	return tag
}

// PHPType = AtomicType | UnionType | IntersectType .
func (p *parser) parseType() phptype.Type {
	typ := p.parseAtomicType()
	switch p.tok.Type {
	case token.Or:
		return p.parseUnionType(typ)
	case token.And:
		return p.parseIntersectType(typ)
	}
	return typ
}

// UnionType = AtomicType "|" AtomicType { "|" AtomicType } .
func (p *parser) parseUnionType(init phptype.Type) phptype.Type {
	union := &phptype.Union{Types: make([]phptype.Type, 0, 2)}
	union.Types = append(union.Types, init)

	for p.got(token.Or) {
		typ := p.parseAtomicType()
		union.Types = append(union.Types, typ)
	}
	return union
}

// IntersectType = AtomicType "&" AtomicType { "&" AtomicType } .
func (p *parser) parseIntersectType(init phptype.Type) phptype.Type {
	intersect := &phptype.Intersect{Types: make([]phptype.Type, 0, 2)}
	intersect.Types = append(intersect.Types, init)

	for p.got(token.And) {
		typ, ok := p.tryParseAtomicType()
		if !ok {
			p.backup()
			break
		}
		intersect.Types = append(intersect.Types, typ)
	}
	return intersect
}

// AtomicType   = ParenType | ThisType | BasicType | GenericType | NullableType | ArrayType .
// ThisType     = "$this" .
// BasicType    = NamedType | CallableType | ArrayShapeType | ObjectShapeType | ConstFetch | LitType .
// ArrayType    = AtomicType "[" "]" .
// NullableType = "?" ( BasicType | GenericType ) .
func (p *parser) parseAtomicType() phptype.Type {
	typ, ok := p.tryParseAtomicType()
	if !ok {
		p.errorf("expecting %v or basic type, found %v", token.Lparen, p.tok)
	}
	return typ
}

func (p *parser) tryParseAtomicType() (_ phptype.Type, ok bool) {
	var typ phptype.Type
	if p.got(token.Lparen) {
		typ = p.parseParenType()
	} else if p.got(token.This) {
		typ = new(phptype.This)
	} else {
		nullable := p.got(token.Qmark)
		if p.got(token.Array) {
			typ = p.parseArrayShapeType()
		} else if p.got(token.Object) {
			typ = p.parseObjectShapeType()
		} else if p.got(token.Callable) {
			typ = p.parseCallableType()
		} else if typ, ok = p.parseNamedType(); !ok {
			if typ, ok = p.parseLitType(); !ok {
				return nil, false
			}
		}
		if ok && p.got(token.DoubleColon) {
			if nullable {
				p.errorf("constant fetch cannot be nullable")
				return nil, false
			}
			cf := &phptype.ConstFetch{Class: typ, Name: p.tok.Text}
			switch p.tok.Type {
			case token.Ident, token.Asterisk:
				cf.Name = p.tok.Text
			default:
				p.errorf("unexpected %v, expecting %v", p.tok, token.Ident)
			}
			p.next0()
			if cf.Name != "*" && p.got(token.Asterisk) {
				cf.Name += "*"
			}
			p.consume(token.Whitespace)
			if p.got(token.Asterisk) {
				p.errorf("invalid position of *, did you mean to write %s*?", cf.Name)
			}
			typ = cf
		} else if p.got(token.Lt) {
			// TODO: Forbid generic params for arrays with a shape?
			typ = p.parseGenericType(typ)
		}
		if nullable {
			typ = &phptype.Nullable{Type: typ}
		}
	}
	for p.got(token.Lbrack) {
		p.expect(token.Rbrack)
		typ = &phptype.Array{Elem: typ}
	}
	if p.got(token.DoubleColon) {
		p.errorf("unexpected %v", token.DoubleColon)
		return nil, false
	}
	return typ, true
}

// ParenType = "(" PHPType ")" .
func (p *parser) parseParenType() phptype.Type {
	typ := new(phptype.Paren)
	typ.Type = p.parseType()
	p.expect(token.Rparen)
	return typ
}

// CallableType  = callable [ FuncSignature ] .
// FuncSignature = "(" [ ParamList [ "," ] ] ")" [ ":" PHPType ] .
func (p *parser) parseCallableType() phptype.Type {
	typ := new(phptype.Callable)
	if !p.got(token.Lparen) {
		return typ
	}
	typ.Params = p.parseParamList()
	if p.got(token.Colon) {
		typ.Result = p.parseType()
	}
	return typ
}

// ParamList = Param [ "=" LitType ] { "," Param [ "=" LitType ] } .
func (p *parser) parseParamList() []*phptype.Param {
	var params []*phptype.Param
	for !p.got(token.Rparen) && !p.got(token.EOF) {
		par := p.parseParam(false)
		if p.got(token.Assign) {
			lit, ok := p.parseLitType()
			if !ok {
				p.errorf("expecting literal value, found %v", p.tok)
			}
			par.Default = lit
		}
		params = append(params, par)
		if p.got(token.Rparen) {
			break
		}
		p.expect(token.Comma)
	}
	return params
}

// Param = PHPType [ [ "&" ] [ "..." ] varname ] .
func (p *parser) parseParam(needVar bool) *phptype.Param {
	par := new(phptype.Param)
	par.Type = p.parseType()
	if p.got(token.And) {
		needVar = true
		par.ByRef = true
	}
	if p.got(token.Ellipsis) {
		needVar = true
		par.Variadic = true
	}
	if v := strings.TrimPrefix(p.tok.Text, "$"); p.got(token.Var) {
		par.Name = v
	} else if needVar {
		p.expect(token.Var)
	}
	return par
}

// ArrayShapeType = array [ ArrayShape ] .
// ArrayShape     = "{" KeyType { "," KeyType } [ "," ] "}" .
// KeyType        = [ ArrayKey [ "?" ] ":" ] PHPType .
// ArrayKey       = string | ident | decimal .
func (p *parser) parseArrayShapeType() phptype.Type {
	typ := new(phptype.ArrayShape)
	if p.got(token.Lbrace) {
	Elems:
		for {
			elem := new(phptype.ArrayElem)
			switch p.tok.Type {
			case token.String, token.Ident, token.Int:
				elem.Key = p.tok.Text
				p.next()
				elem.Optional = p.got(token.Qmark)
				if !p.got(token.Colon) {
					elem.Key = ""
					p.backup()
				}
			case token.Rbrace:
				// Allow trailing comma.
				if len(typ.Elems) > 0 {
					break Elems
				}
			}
			had := p.tok
			elem.Type = p.parseType()
			if p.err != nil {
				p.err = nil
				p.errorf("expecting array shape key, or value; found %v", had)
			}
			typ.Elems = append(typ.Elems, elem)
			if !p.got(token.Comma) {
				break
			}
		}
		p.expect(token.Rbrace)
	}
	return typ
}

// ObjectShapeType = object [ ObjectShape ] .
// ObjectShape     = "{" KeyType { "," KeyType } [ "," ] "}" .
// ObjectKeyType   = ObjectKey [ "?" ] ":" PHPType .
// ObjectKey       = ident .
func (p *parser) parseObjectShapeType() phptype.Type {
	typ := new(phptype.ObjectShape)
	if p.got(token.Lbrace) {
	Elems:
		for {
			elem := new(phptype.ObjectElem)
			switch p.tok.Type {
			case token.Ident:
				elem.Key = p.tok.Text
				p.next()
			case token.Rbrace:
				// Allow trailing comma.
				if len(typ.Elems) > 0 {
					break Elems
				}
				fallthrough
			default:
				p.errorf("expecting %v, found %v", token.Ident, p.tok)
				return nil
			}
			elem.Optional = p.got(token.Qmark)
			p.expect(token.Colon)
			elem.Type = p.parseType()
			typ.Elems = append(typ.Elems, elem)
			if !p.got(token.Comma) {
				break
			}
		}
		p.expect(token.Rbrace)
	}
	return typ
}

// GenericType = BasicType "<" PHPType { "," PHPType } [ "," ] ">" .
func (p *parser) parseGenericType(base phptype.Type) phptype.Type {
	var params []phptype.Type
	for {
		if len(params) > 0 && p.tok.Type == token.Gt {
			// Allow trailing comma.
			break
		}
		t := p.parseType()
		params = append(params, t)
		if !p.got(token.Comma) {
			break
		}
	}
	p.expect(token.Gt)
	return &phptype.Generic{Base: base, TypeParams: params}
}

// NamedType = static | [ "\\" ] ident { "\\" ident } .
func (p *parser) parseNamedType() (_ *phptype.Named, ok bool) {
	id := new(phptype.Named)
	switch p.tok.Type {
	default:
		return nil, false
	case token.Static:
		id.Parts = append(id.Parts, p.tok.Text)
		p.next()
		return id, true
	case token.Backslash, token.Ident:
	}
	if p.got(token.Backslash) {
		id.Global = true
	}
	for {
		id.Parts = append(id.Parts, p.tok.Text)
		p.expect(token.Ident)
		if !p.got(token.Backslash) {
			break
		}
	}
	return id, true
}

// LitType = string | decimal | ident .
func (p *parser) parseLitType() (_ *phptype.Literal, ok bool) {
	lit := new(phptype.Literal)
	switch p.tok.Type {
	case token.String, token.Int, token.Ident:
		lit.Value = p.tok.Text
		p.next()
		return lit, true
	default:
		return nil, false
	}
}

// Desc = { any } .
func (p *parser) parseDesc(b *strings.Builder) string {
	if b == nil {
		b = new(strings.Builder)
	}
LOOP:
	for {
		switch p.tok.Type {
		case token.Newline, token.CloseDoc, token.EOF:
			break LOOP
		}
		b.WriteString(p.tok.Text)
		p.next0()
	}
	return strings.TrimSpace(b.String())
}
