package naive

import "mibk.dev/phpfmt/token"

const obviousPrecLevel = 10

// See https://www.php.net/manual/en/language.operators.precedence.php
var opTable = [...][]token.Type{
	1: {token.Pow},
	2: {metaTokenCast},
	4: {token.Not},
	5: {token.Mul, token.Quo, token.Rem},
	6: {token.Add, token.Sub},
	7: {token.BitShl, token.BitShr},
	8: {token.Concat},
	9: {token.Pipe},
	obviousPrecLevel: { // Equalize these artificially.
		// 10
		token.Lt, token.Gt, token.Leq, token.Geq,
		// 11
		token.Eq, token.Neq, token.Identical, token.NotIdentical, token.Spaceship,
	},
	12: {token.BitAnd},
	13: {token.BitXor},
	14: {token.BitOr},
	15: {token.And},
	16: {token.Or},
	17: {token.Coalesce},
}

var opPrec map[token.Type]int

func init() {
	opPrec = make(map[token.Type]int)
	for prec, level := range opTable {
		for _, op := range level {
			opPrec[op] = prec
		}
	}
}

// prec returns the precedence of op, using the printer’s concat override.
func (p *printer) prec(op token.Type) (int, bool) {
	if op == token.Concat {
		return p.concatPrec, true
	}
	prec, ok := opPrec[op]
	return prec, ok
}

// analyseOps returns the maximum precedence of binary expressions in tokens.
func (p *printer) analyseOps(tokens []any) (max int) {
	hasLowPrec := false
	defer func() {
		if hasLowPrec && max > obviousPrecLevel {
			max = 22
		}
	}()

	var last token.Type
	for _, tok := range tokens {
		if _, ok := tok.(*ternaryMiddle); ok {
			break
		}
		tok, ok := tok.(token.Token)
		if !ok {
			last = token.Illegal
			continue
		}

		switch tok.Type {
		case token.And, token.Or:
			// These are obvious enough. Exclude them from analysis,
			// so it won’t tighten up all the other operators.
			continue
		case token.LowPrecAnd, token.LowPrecOr, token.LowPrecXor:
			hasLowPrec = true
			continue
		}

		switch {
		case tok.Type == token.Concat && last == token.Int,
			tok.Type == token.Int && last == token.Concat:
			// Ensure we keep these blanks
			// so they aren’t converted into floats.
			return p.concatPrec
		}
		if tok.Type != token.Whitespace {
			last = tok.Type
		}

		prec, ok := p.prec(tok.Type)
		if !ok {
			continue
		}
		if prec > max && prec != obviousPrecLevel {
			max = prec
		}
	}
	return max
}

func (p *printer) decideOpSpaces(max int, op token.Type) (add, ok bool) {
	if max < 0 {
		return false, false
	}
	prec, ok := p.prec(op)
	return prec >= max, ok
}

func nextOperatorIs(tokens []any, want token.Type) bool {
	for _, y := range tokens {
		op, ok := y.(token.Token)
		if !ok {
			continue
		}
		if op.Type == want {
			return true
		}
		if _, ok := opPrec[op.Type]; ok {
			// TODO: Recognize more show-stopper ops?
			break
		}
		if isOperator(op.Type) {
			break
		}
	}
	return false
}

func isOperator(typ token.Type) bool {
	// TODO: Make definition for "operator" more stable.
	switch typ {
	case token.Arrow, token.QmarkArrow, token.DoubleColon:
		return false
	}
	return token.Add <= typ && typ <= token.Spaceship
}
