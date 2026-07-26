package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/GalileoNio/Mao/internal/compiler"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "mao:", err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	if len(arguments) == 0 {
		return usageError()
	}

	// Parse --pure flag
	pure := false
	filtered := arguments[:0]
	for _, arg := range arguments {
		if arg == "--pure" {
			pure = true
		} else {
			filtered = append(filtered, arg)
		}
	}
	arguments = filtered

	if len(arguments) == 0 {
		return usageError()
	}
	command, targets := arguments[0], arguments[1:]
	switch command {
	case "emit-go":
		if len(targets) == 0 {
			return errors.New("emit-go requires at least one .mao file")
		}
		return emitFiles(targets)
	case "fmt":
		if len(targets) == 0 {
			targets = []string{"."}
		}
		return formatFiles(targets)
	case "build", "check", "test":
		if len(targets) == 0 {
			targets = []string{"."}
		}
		return invokePackageCommand(command, targets, pure)
	case "run":
		if len(targets) != 1 {
			return errors.New("run requires one .mao file or package directory")
		}
		return invokePackageCommand(command, targets, pure)
	default:
		return fmt.Errorf("unknown command %q\n%s", command, usageText())
	}
}

func usageError() error {
	return errors.New(usageText())
}

func usageText() string {
	return `usage:
  mao build [--pure] [packages]
  mao run [--pure] <file-or-package>
  mao test [--pure] [packages]
  mao fmt [files-or-directories]
  mao check [--pure] [packages]
  mao emit-go <files>`
}

func emitFiles(targets []string) error {
	files, err := maoFiles(targets)
	if err != nil {
		return err
	}
	groups := make(map[string]map[string][]byte)
	for _, filename := range files {
		source, err := os.ReadFile(filename)
		if err != nil {
			return err
		}
		directory := filepath.Dir(filename)
		if groups[directory] == nil {
			groups[directory] = make(map[string][]byte)
		}
		groups[directory][filename] = source
	}
	generatedFiles := make(map[string][]byte)
	for _, sources := range groups {
		generated, err := compiler.CompilePackage(sources)
		if err != nil {
			return err
		}
		for filename, output := range generated {
			generatedFiles[filename] = output
		}
	}
	for index, filename := range files {
		generated := generatedFiles[filename]
		if index > 0 {
			fmt.Fprintln(os.Stdout)
		}
		if len(files) > 1 {
			fmt.Fprintf(os.Stdout, "// generated from %s\n", filename)
		}
		if _, err := os.Stdout.Write(generated); err != nil {
			return err
		}
	}
	return nil
}

func formatFiles(targets []string) error {
	files, err := maoFiles(targets)
	if err != nil {
		return err
	}
	for _, filename := range files {
		source, err := os.ReadFile(filename)
		if err != nil {
			return err
		}
		if err := compiler.CheckSyntax(filename, source); err != nil {
			return err
		}
		formatted := normalizeWhitespace(source)
		if !bytes.Equal(source, formatted) {
			info, err := os.Stat(filename)
			if err != nil {
				return err
			}
			if err := os.WriteFile(filename, formatted, info.Mode().Perm()); err != nil {
				return err
			}
		}
	}
	return nil
}

func normalizeWhitespace(source []byte) []byte {
	source = bytes.TrimRight(source, "\r\n")
	lines := bytes.Split(source, []byte("\n"))
	for index := range lines {
		lines[index] = bytes.TrimRight(lines[index], " \t\r")
	}
	return append(bytes.Join(lines, []byte("\n")), '\n')
}

func invokePackageCommand(command string, targets []string, pure bool) error {
	root, err := findModuleRoot(targets[0])
	if err != nil {
		return err
	}
	work, err := os.MkdirTemp("", "mao-build-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	if err := materializeModule(root, work); err != nil {
		return err
	}

	translatedTargets := make([]string, len(targets))
	for index, target := range targets {
		targetPath, recursive := splitPackagePattern(target)
		absolute, err := filepath.Abs(targetPath)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, absolute)
		if err != nil {
			return err
		}
		if strings.HasSuffix(relative, ".mao") {
			relative = filepath.Dir(relative)
		}
		if relative == "." {
			translatedTargets[index] = "."
		} else {
			translatedTargets[index] = "./" + filepath.ToSlash(relative)
		}
		if recursive {
			if translatedTargets[index] == "." {
				translatedTargets[index] = "./..."
			} else {
				translatedTargets[index] += "/..."
			}
		}
	}

	goArguments := []string{command}
	switch command {
	case "check":
		goArguments = []string{"test", "-tags=mao", "-run", "^$"}
	case "run":
		goArguments = []string{"run", "-tags=mao"}
	case "build", "test":
		goArguments = []string{command, "-tags=mao"}
	}
	goArguments = append(goArguments, translatedTargets...)
	goCommand := exec.Command("go", goArguments...)
	goCommand.Dir = work
	goCommand.Stdin = os.Stdin
	goCommand.Stdout = os.Stdout
	goCommand.Stderr = os.Stderr
	if err := goCommand.Run(); err != nil {
		return fmt.Errorf("go %s failed: %w", strings.Join(goArguments, " "), err)
	}
	return nil
}

func materializeModule(root, destination string) error {
	groups := make(map[string]map[string][]byte)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if relative != "." && (entry.Name() == ".git" || entry.Name() == ".mao-cache") {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(destination, relative), 0o700)
		}
		extension := filepath.Ext(path)
		if extension != ".go" && extension != ".mao" && entry.Name() != "go.mod" && entry.Name() != "go.sum" {
			return nil
		}
		target := filepath.Join(destination, relative)
		if extension == ".mao" {
			source, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			directory := filepath.Dir(path)
			if groups[directory] == nil {
				groups[directory] = make(map[string][]byte)
			}
			groups[directory][path] = source
			return nil
		}
		return copyFile(path, target)
	})
	if err != nil {
		return err
	}
	for _, sources := range groups {
		generatedFiles, err := compiler.CompilePackage(sources)
		if err != nil {
			return err
		}
		for sourceName, generated := range generatedFiles {
			relative, err := filepath.Rel(root, sourceName)
			if err != nil {
				return err
			}
			target := filepath.Join(destination, generatedFilename(relative))
			if err := os.WriteFile(target, generated, 0o600); err != nil {
				return err
			}
		}
	}
	return nil
}

func generatedFilename(source string) string {
	if strings.HasSuffix(source, "_test.mao") {
		return strings.TrimSuffix(source, "_test.mao") + "_mao_test.go"
	}
	return strings.TrimSuffix(source, ".mao") + "_mao.go"
}

func copyFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.Create(destination)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		return err
	}
	return output.Close()
}

func compileFile(filename string) ([]byte, error) {
	source, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return compiler.Compile(filename, source)
}

func maoFiles(targets []string) ([]string, error) {
	var result []string
	for _, target := range targets {
		targetPath, _ := splitPackagePattern(target)
		info, err := os.Stat(targetPath)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			if filepath.Ext(targetPath) != ".mao" {
				return nil, fmt.Errorf("%s is not a .mao file", targetPath)
			}
			result = append(result, targetPath)
			continue
		}
		err = filepath.WalkDir(targetPath, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == ".mao-cache") {
				return filepath.SkipDir
			}
			if !entry.IsDir() && filepath.Ext(path) == ".mao" {
				result = append(result, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	slices.Sort(result)
	result = slices.Compact(result)
	if len(result) == 0 {
		return nil, errors.New("no .mao files found")
	}
	return result, nil
}

func findModuleRoot(target string) (string, error) {
	targetPath, _ := splitPackagePattern(target)
	absolute, err := filepath.Abs(targetPath)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absolute)
	if err == nil && !info.IsDir() {
		absolute = filepath.Dir(absolute)
	}
	for {
		moduleFile := filepath.Join(absolute, "go.mod")
		if info, err := os.Stat(moduleFile); err == nil && !info.IsDir() {
			return absolute, nil
		}
		parent := filepath.Dir(absolute)
		if parent == absolute {
			return "", fmt.Errorf("cannot locate go.mod for %s", target)
		}
		absolute = parent
	}
}

func splitPackagePattern(target string) (string, bool) {
	slashed := filepath.ToSlash(target)
	if !strings.HasSuffix(slashed, "/...") {
		return target, false
	}
	base := strings.TrimSuffix(slashed, "/...")
	if base == "" {
		base = "."
	}
	return filepath.FromSlash(base), true
}
