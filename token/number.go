package token

import "strings"

func (s *Scanner) scanNumber(r rune) Token {
	if r == '0' {
		switch r := s.peek(); {
		case isDigit(r):
			return s.scanOctal()
		case r == 'x' || r == 'X':
			return s.scanHexa(s.read())
		case r == 'b' || r == 'B':
			return s.scanBinary(s.read())
		}
	}
	b := new(strings.Builder)
	b.WriteRune(r)
	if !s.scanDecimal(b) {
		return Token{Type: Illegal, Text: b.String()}
	}
	tok := Token{Type: Int}
	switch s.peek() {
	case '.':
		b.WriteRune(s.read())
		fallthrough
	case 'e', 'E':
		return s.scanFloat(b)
	}
	tok.Text = b.String()
	return tok
}

func (s *Scanner) scanDecimal(b *strings.Builder) bool {
	for {
		if b.Len() > 0 && s.peek() == '_' {
			b.WriteRune(s.read())
			if !isDigit(s.peek()) {
				b.WriteRune(s.read())
				return false
			}
		}
		if !isDigit(s.peek()) {
			break
		}
		b.WriteRune(s.read())

	}
	return b.Len() > 0
}

func (s *Scanner) scanOctal() Token {
	var b strings.Builder
	for {
		switch r := s.peek(); r {
		default:
			return Token{Type: Int, Text: "0" + b.String()}
		case '8', '9':
			return s.errorf("invalid digit %c in octal literal", r)
		case '0', '1', '2', '3', '4', '5', '6', '7':
			b.WriteRune(s.read())
		}
	}
}

func (s *Scanner) scanHexa(delim rune) Token {
	var b strings.Builder
	for {
		switch r := s.peek(); {
		default:
			return Token{Type: Int, Text: "0" + string(delim) + b.String()}
		case isDigit(r) || 'a' <= r && r <= 'f' || 'A' <= r && r <= 'F':
			b.WriteRune(s.read())
		}
	}
}

func (s *Scanner) scanBinary(delim rune) Token {
	var b strings.Builder
	for {
		switch r := s.peek(); r {
		default:
			return Token{Type: Int, Text: "0" + string(delim) + b.String()}
		case '0', '1':
			b.WriteRune(s.read())
		}
	}
}

func (s *Scanner) scanFloat(b *strings.Builder) Token {
	if !s.scanDecimal(b) {
		return Token{Type: Illegal, Text: b.String()}
	}
	if r := s.peek(); r == 'e' || r == 'E' {
		b.WriteRune(s.read())
		if r := s.peek(); r == '+' || r == '-' {
			b.WriteRune(s.read())
		}
		if !s.scanDecimal(b) {
			return Token{Type: Illegal, Text: b.String()}
		}
	}
	return Token{Type: Float, Text: b.String()}
}

func isDigit(r rune) bool { return '0' <= r && r <= '9' }
