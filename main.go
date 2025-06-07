package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"mibk.dev/phpfmt/format"
	"mibk.dev/phpfmt/naive"
)

func usage() {
	fmt.Fprintf(os.Stderr, "usage: phpfmt [-w] [path ...]\n")
	fmt.Fprintf(os.Stderr, "  -w	write result to (source) file instead of stdout\n")
	os.Exit(2)
}

var inPlace = flag.Bool("w", false, "write to file")

func main() {
	log.SetPrefix("phpfmt: ")
	log.SetFlags(0)
	flag.Usage = usage
	flag.Parse()

	if flag.NArg() == 0 {
		if *inPlace {
			log.Fatal("cannot use -w with standard input")
		}
		if err := format.Pipe("<stdin>", os.Stdout, os.Stdin, defaultOptions); err != nil {
			log.Fatal(err)
		}
		return
	}

	for _, filename := range flag.Args() {
		f, err := os.Open(filename)
		if err != nil {
			log.Fatal(err)
		}
		fi, err := f.Stat()
		if err != nil {
			log.Fatal(err)
		}

		if !fi.IsDir() {
			formatFile(filename, fi.Mode().Perm(), f)
			continue
		}

		err = filepath.WalkDir(filename, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				log.Fatal(err)
			}
			if d.IsDir() {
				return nil
			}
			switch filepath.Ext(d.Name()) {
			default:
				return nil
			case ".php", ".phpt":
			}

			f, err := os.Open(path)
			if err != nil {
				return err
			}
			return formatFile(path, d.Type().Perm(), f)
		})
		if err != nil {
			log.Fatal(err)
		}

	}
}

func formatFile(path string, perm fs.FileMode, data io.ReadCloser) error {
	ver, err := findMinPHPVersion(filepath.Dir(path))
	if err != nil {
		return err
	}
	opts := defaultOptions
	if ver < targetPHPVersion {
		opts |= naive.PHP74Compat
	}

	buf := new(bytes.Buffer)
	if err := format.Pipe(path, buf, data, opts); err != nil {
		return err
	}
	if err := data.Close(); err != nil {
		return err
	}

	if *inPlace {
		return os.WriteFile(path, buf.Bytes(), perm)
	} else {
		_, err := io.Copy(os.Stdout, buf)
		return err
	}
}
