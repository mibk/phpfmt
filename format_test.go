package main_test

import (
	"bytes"
	"flag"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"mibk.dev/phpfmt/format"
	"mibk.dev/phpfmt/naive"
	"rsc.io/diff"
)

var rewriteGolden = flag.Bool("f", false, "write golden files")

func TestFmt(t *testing.T) {
	files, err := filepath.Glob("testdata/*.input")
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range files {
		test := strings.TrimSuffix(name, ".input")
		t.Run(test, func(t *testing.T) {
			testFmt(t, name)
		})
	}
}

func testFmt(t *testing.T, filename string) {
	t.Helper()
	input, err := ioutil.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}

	goldenName := strings.TrimSuffix(filename, ".input") + ".golden"
	golden, _ := ioutil.ReadFile(goldenName)

	t.Log(filename)
	t.Log(goldenName)

	opts := naive.Standard
	firstLine, _, _ := strings.Cut(string(input), "\n")
	if _, cfg, ok := strings.Cut(firstLine, "// PHP"); ok {
		cfg = strings.TrimSpace(cfg)
		if v, err := strconv.Atoi(cfg); err == nil {
			if v < 80000 {
				opts |= naive.PHP74Compat
			}
		} else if cfg == "-align" {
			opts &= ^naive.AlignColumns
		}
	}

	got := fmtInput(t, input, opts)
	// TODO: Do not require running fmtInput twice.
	got = fmtInput(t, got, opts)

	if *rewriteGolden {
		os.WriteFile(goldenName, got, 0o644)
		return
	}

	if !bytes.Equal(got, golden) {
		diff := diff.Format(string(got), string(golden))
		t.Errorf("lines don't match (-got +want)\n%s", diff)
	}
}

func fmtInput(t *testing.T, src []byte, opts naive.Options) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	if err := format.Pipe("<test>", buf, bytes.NewReader(src), opts); err != nil {
		t.Errorf("unexpected err: %v", err)
	}
	return buf.Bytes()

}
