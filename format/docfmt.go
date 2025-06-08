package format

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"mibk.dev/phpfmt/phpdoc"
	"mibk.dev/phpfmt/token"
)

func formatDocs(filename string, src []byte) ([]byte, error) {
	scan := token.NewScanner(bytes.NewReader(src), true)
	var out bytes.Buffer
	w := &stickyErrWriter{w: &out}
	var ws string
	firstDoc := true
	var doc *phpdoc.Block
Loop:
	for {
		tok := scan.Next()
		if doc != nil {
			ws = ""
			if tok.Type == token.Whitespace {
				i := strings.LastIndexByte(tok.Text, '\n')
				doc.Indent = tok.Text[i+1:]
				if i >= 0 {
					tok.Text = tok.Text[:i]
				}
			}
			if err := phpdoc.Fprint(w, doc); err != nil {
				return nil, fmt.Errorf("%s: printing doc: %v", filename, err)
			}
			io.WriteString(w, doc.Indent)
			doc = nil
			if tok.Type == token.Whitespace {
				if firstDoc {
					firstDoc = false
				} else {
					continue
				}
			}
		}
		if tok.Type == token.DocComment {
			var err error
			doc, err = phpdoc.Parse(strings.NewReader(tok.Text))
			if err == nil {
				continue
			}
			pos := _Pos{Line: tok.Pos.Line, Column: tok.Pos.Column}
			if se, ok := err.(*phpdoc.SyntaxError); ok {
				pos = pos.Add(_Pos{Line: se.Line, Column: se.Column})
				err = se.Err
			}
			return nil, fmt.Errorf("%s:%v: %v", filename, pos, err)
		}
		if ws != "" {
			io.WriteString(w, ws)
			ws = ""
		}
		switch tok.Type {
		case token.EOF:
			break Loop
		case token.Whitespace:
			i := strings.LastIndexByte(tok.Text, '\n')
			io.WriteString(w, tok.Text[:i+1])
			ws = tok.Text[i+1:]
		case token.Namespace, token.Class, token.Interface, token.Trait, token.Enum:
			// Turn "file doc" off after these.
			firstDoc = false
			fallthrough
		default:
			io.WriteString(w, tok.Text)
		}
	}
	if err := scan.Err(); err != nil {
		var scanErr *token.ScanError
		if errors.As(err, &scanErr) {
			return nil, fmt.Errorf("%s:%v: %v", filename, scanErr.Pos, scanErr.Err)
		}
		return nil, fmt.Errorf("formatting %q: %v", filename, err)
	}
	return out.Bytes(), w.err
}

type stickyErrWriter struct {
	w   io.Writer
	err error
}

func (w *stickyErrWriter) Write(p []byte) (n int, err error) {
	if w.err != nil {
		return 0, w.err
	}
	n, w.err = w.w.Write(p)
	return n, w.err
}

type _Pos struct {
	Line, Column int
}

func (p _Pos) Add(q _Pos) _Pos {
	if q.Line == 1 {
		p.Column += q.Column - 1
	} else {
		p.Line += q.Line - 1
		p.Column = q.Column
	}
	return p
}

func (p _Pos) String() string {
	return fmt.Sprintf("%d:%d", p.Line, p.Column)
}
