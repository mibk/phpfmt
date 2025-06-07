package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"mibk.dev/phpfmt/naive"
)

var defaultOptions naive.Options

func init() {
	opts := strings.Split(os.Getenv("PHPFMT"), ",")
	for _, opt := range opts {
		switch opt = strings.TrimSpace(opt); opt {
		default:
			log.Printf("phpfmt: Unknown option %q", opt)
		case "base":
		case "":
			defaultOptions = naive.Standard
		case "comma":
			defaultOptions |= naive.TrailingComma
		case "align":
			defaultOptions |= naive.AlignColumns
		}
	}
}

const (
	defaultPHPVersion = 50400
	targetPHPVersion  = 80000
)

var minVerCache = map[string]int{}

func findMinPHPVersion(dir string) (minVer int, error error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return 0, err
	}

	for parent, ver := range minVerCache {
		if _, err := filepath.Rel(parent, dir); err == nil {
			return ver, nil
		}
	}

	var b []byte
	for dir != "" {
		if ver, ok := minVerCache[dir]; ok {
			return ver, nil
		}

		comp := filepath.Join(dir, "composer.json")
		b, err = os.ReadFile(comp)
		if os.IsNotExist(err) {
			old := dir
			dir = filepath.Dir(dir)
			if dir != old {
				continue
			}
			// Root.
			return defaultPHPVersion, nil
		}
		if err != nil {
			return 0, err
		}
		break
	}

	if len(b) == 0 {
		return defaultPHPVersion, nil
	}

	defer func() {
		minVerCache[dir] = minVer
	}()

	var proj struct {
		Require map[string]string
	}
	if err := json.Unmarshal(b, &proj); err != nil {
		return 0, err
	}

	ver, ok := strings.CutPrefix(proj.Require["php"], ">=")
	if !ok {
		if ver, ok = strings.CutPrefix(proj.Require["php"], "^"); !ok {
			return defaultPHPVersion, nil
		}
	}

	ver = strings.TrimSpace(ver)
	maj, min, ok := strings.Cut(ver, ".")
	if !ok {
		return defaultPHPVersion, nil
	}

	majInt, _ := strconv.Atoi(maj)
	minInt, _ := strconv.Atoi(min)
	return majInt*10000 + minInt*100, nil
}
