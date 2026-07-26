package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeWhitespaceIsIdempotent(t *testing.T) {
	source := []byte("package main  \r\n\r\nfunc main() {\t\n}\n\n")
	once := normalizeWhitespace(source)
	twice := normalizeWhitespace(once)
	if !bytes.Equal(once, twice) {
		t.Fatalf("formatter is not idempotent:\n%q\n%q", once, twice)
	}
	if string(once) != "package main\n\nfunc main() {\n}\n" {
		t.Fatalf("unexpected formatting result %q", once)
	}
}

func TestMaoFilesAcceptsGoRecursivePackagePattern(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(nested, "main.mao")
	if err := os.WriteFile(filename, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	files, err := maoFiles([]string{filepath.Join(root, "...")})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != filename {
		t.Fatalf("unexpected recursive files: %#v", files)
	}
}

func TestSplitPackagePattern(t *testing.T) {
	base, recursive := splitPackagePattern("./examples/...")
	if !recursive || filepath.ToSlash(base) != "./examples" {
		t.Fatalf("unexpected pattern split: base=%q recursive=%v", base, recursive)
	}
	base, recursive = splitPackagePattern("./examples")
	if recursive || base != "./examples" {
		t.Fatalf("ordinary path was changed: base=%q recursive=%v", base, recursive)
	}
}
