package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInteropProgramRuns(t *testing.T) {
	root := projectRoot(t)
	sourceName := filepath.Join(root, "examples", "interop", "main.mao")
	source, err := os.ReadFile(sourceName)
	if err != nil {
		t.Fatal(err)
	}
	generated, err := Compile(sourceName, source)
	if err != nil {
		t.Fatal(err)
	}

	output, err := runGenerated(t, root, generated)
	if err != nil {
		t.Fatalf("generated program failed: %v\n%s\n%s", err, generated, output)
	}
	if string(output) != "10 800\n[1 2] [1 2] 1\n[<nil> 0] [9 0] <nil> 9\n2 0\n9 1\n" {
		t.Fatalf("unexpected output %q", output)
	}
}

func TestGeneratedDiagnosticsReferenceMaoSource(t *testing.T) {
	root := projectRoot(t)
	source := []byte("package main\nfunc main() {\n\t_ = missingName\n}\n")
	generated, err := Compile("broken.mao", source)
	if err != nil {
		t.Fatal(err)
	}
	output, err := runGenerated(t, root, generated)
	if err == nil || !strings.Contains(string(output), "broken.mao:") {
		t.Fatalf("Go diagnostic did not reference Mao source:\n%s", output)
	}
}

func TestGeneratedPanicReferencesMaoSource(t *testing.T) {
	root := projectRoot(t)
	source := []byte("package main\nfunc main() {\n\tpanic(\"boom\")\n}\n")
	generated, err := Compile("panic.mao", source)
	if err != nil {
		t.Fatal(err)
	}
	output, err := runGenerated(t, root, generated)
	if err == nil || !strings.Contains(string(output), "panic.mao:") {
		t.Fatalf("panic stack did not reference Mao source:\n%s", output)
	}
}

func TestTableToArrayRejectsLengthMismatch(t *testing.T) {
	root := projectRoot(t)
	source := []byte(`package main
func main() {
	values := [1, 2]
	int[3] fixed = values
	_ = fixed
}
`)
	generated, err := Compile("array-length.mao", source)
	if err != nil {
		t.Fatal(err)
	}
	output, err := runGenerated(t, root, generated)
	if err == nil || !strings.Contains(string(output), "mao table length does not match array") {
		t.Fatalf("array length mismatch did not panic as specified:\n%s", output)
	}
}

func projectRoot(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
}

func runGenerated(t *testing.T, root string, generated []byte) ([]byte, error) {
	t.Helper()
	work := t.TempDir()
	module := []byte("module mao.test\n\ngo 1.26\n\nrequire github.com/GalileoNio/Mao v0.0.0\n\nreplace github.com/GalileoNio/Mao => " +
		filepath.ToSlash(root) + "\n")
	if err := os.WriteFile(filepath.Join(work, "go.mod"), module, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "main.go"), generated, 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = work
	command.Env = append(os.Environ(), "GOCACHE=/tmp/mao-go-build-cache")
	return command.CombinedOutput()
}
