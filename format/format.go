package format

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"slices"
	"strings"

	"mibk.dev/phpfmt/naive"
)

// Pipe reads PHP source code from in, formats it, and writes the result to out.
// The format can be sliglthly tweaked using opts. (See [naive.Options].)
// The filename argument is used to set the “filename” in error messages.
func Pipe(filename string, out io.Writer, in io.Reader, opts naive.Options) error {
	src, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	var b bytes.Buffer
	if err := formatCode(filename, &b, bytes.NewReader(src), opts); err != nil {
		return err
	}

	code := orderUseStmts(b.Bytes())

	if opts&naive.AlignColumns > 0 {
		withdoc, err := formatDocs(filename, code)
		if err != nil {
			log.Println("WARN:", err)
		} else {
			code = withdoc
		}
	}

	_, err = out.Write(code)
	return err
}

func formatCode(filename string, out io.Writer, in io.Reader, opts naive.Options) error {
	file, err := naive.Parse(in)
	if se, ok := err.(*naive.SyntaxError); ok {
		return fmt.Errorf("%s:%d:%d: %v", filename, se.Line, se.Column, se.Err)
	} else if err != nil {
		return err
	}
	return naive.Fprint(out, file, opts)
}

var slashes = strings.NewReplacer("\\", ";")

func orderUseStmts(src []byte) []byte {
	var b bytes.Buffer
	var stmts []string

	flush := func() {
		slices.SortFunc(stmts, func(a, b string) int {
			return strings.Compare(slashes.Replace(a), slashes.Replace(b))
		})
		for _, stmt := range stmts {
			b.WriteString(stmt)
		}
		stmts = stmts[:0]
	}

	const use = "use "
	for line := range bytes.Lines(src) {
		line := string(line)
		if strings.HasPrefix(line, use) && !strings.Contains(line, "{") {
			line = strings.TrimPrefix(line, use)
			line = strings.TrimLeft(line, "\\")
			stmts = append(stmts, use+line)
			continue
		}
		flush()
		b.WriteString(line)
	}
	flush()
	return b.Bytes()
}
