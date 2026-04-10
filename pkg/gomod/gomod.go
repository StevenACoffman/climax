// Package gomod locates the Go module root and computes import paths.
package gomod

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Info holds the result of locating a Go module.
type Info struct {
	Root   string // directory containing go.mod
	Module string // module path declared in go.mod
}

// Find walks up from dir searching for a go.mod file.
// It returns the module root and path for the first go.mod found.
func Find(dir string) (Info, error) {
	for {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil {
			mod, parseErr := parseModule(data)
			if parseErr != nil {
				return Info{}, parseErr
			}
			return Info{Root: dir, Module: mod}, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return Info{}, fmt.Errorf("no go.mod found in %s or any parent directory", dir)
		}
		dir = parent
	}
}

// ImportPath returns the Go import path for dir within this module.
func (i Info) ImportPath(dir string) (string, error) {
	rel, err := filepath.Rel(i.Root, dir)
	if err != nil {
		return "", fmt.Errorf("computing relative path: %w", err)
	}
	if rel == "." {
		return i.Module, nil
	}
	return i.Module + "/" + filepath.ToSlash(rel), nil
}

func parseModule(data []byte) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if after, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(after), nil
		}
	}
	return "", errors.New("module directive not found in go.mod")
}
