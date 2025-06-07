package naive

import "mibk.dev/phpfmt/token"

// See https://www.php.net/manual/en/language.operators.precedence.php
var opTable = [...][]token.Type{
	1: {token.Pow},
	2: {metaTokenCast},
	4: {token.Not},
	5: {token.Mul, token.Quo, token.Rem},
	6: {token.Add, token.Sub},
	7: {token.BitShl, token.BitShr},
	8: {token.Concat},
	9: { // Equalize these artificially.
		// 9
		token.Lt, token.Gt, token.Leq, token.Geq,
		// 10
		token.Eq, token.Neq, token.Identical, token.NotIdentical, token.Spaceship,
	},
	11: {token.BitAnd},
	12: {token.BitXor},
	13: {token.BitOr},
	14: {token.And},
	15: {token.Or},
	16: {token.Coalesce},
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

// analyseOps returns the maximum precedence of binary expressions in tokens.
func analyseOps(tokens []any) (max int) {
	hasLowPrec := false
	defer func() {
		if hasLowPrec && max > 9 {
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
			// so it won't tighten up all the other operators.
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
			return opPrec[token.Concat]
		}
		if tok.Type != token.Whitespace {
			last = tok.Type
		}

		prec, ok := opPrec[tok.Type]
		if !ok {
			continue
		}
		// Let's say with level-9, the precedence is obvious.
		if prec > max && prec != 9 {
			max = prec
		}
	}
	return
}

func decideOpSpaces(max int, op token.Type) (add, ok bool) {
	if max < 0 {
		return false, false
	}
	prec, ok := opPrec[op]
	return prec >= max, ok
}

func nextOperatorIs(tokens []any, want token.Type) bool {
	for _, y := range tokens {
		op, ok := y.(token.Token)
		if !ok {
			break
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
