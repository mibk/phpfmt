package phpdoc

import (
	"io"

	"mibk.dev/phpfmt/phpdoc/internal/token"
	"mibk.dev/phpfmt/phpdoc/phptype"
)

func ParseType(r io.Reader) (phptype.Type, error) {
	p := &parser{scan: token.NewScanner(r)}
	p.next()
	typ := p.parseType()
	return typ, p.err
}
