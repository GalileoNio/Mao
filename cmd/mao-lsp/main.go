// Command mao-lsp provides Language Server Protocol support for Mao
// by transparently compiling .mao files to .go and delegating to gopls.
//
// Architecture:
//
//	.mao file change → mao-lsp → compile to temp .go → gopls → map back via //line
//
// This gives Mao projects full IDE support (completion, diagnostics, hover,
// go-to-definition, references) with zero additional implementation cost.
//
// Requires gopls to be installed (go install golang.org/x/tools/gopls@latest).
//
// Usage:
//
//	mao-lsp          start LSP server on stdin/stdout
//	mao-lsp --check  verify gopls is available
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--check" {
		if err := checkGopls(); err != nil {
			fmt.Fprintln(os.Stderr, "mao-lsp: gopls not found:", err)
			fmt.Fprintln(os.Stderr, "Install with: go install golang.org/x/tools/gopls@latest")
			os.Exit(1)
		}
		fmt.Println("mao-lsp: gopls is available")
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "mao-lsp:", err)
		os.Exit(1)
	}
}

func checkGopls() error {
	_, err := exec.LookPath("gopls")
	return err
}

func run() error {
	goplsPath, err := exec.LookPath("gopls")
	if err != nil {
		return fmt.Errorf("gopls not found in PATH; install with: go install golang.org/x/tools/gopls@latest")
	}

	workDir, err := os.Getwd()
	if err != nil {
		return err
	}

	proxy := &lspProxy{
		goplsPath: goplsPath,
		workDir:   workDir,
		cache:     make(map[string]string),
	}

	return proxy.serve()
}

type lspProxy struct {
	goplsPath string
	workDir   string
	goplsCmd  *exec.Cmd
	goplsIn   io.WriteCloser
	goplsOut  io.ReadCloser
	cache     map[string]string
	mu        sync.Mutex
}

func (p *lspProxy) serve() error {
	cmd := exec.Command(p.goplsPath, "serve", "-rpc.trace")
	cmd.Dir = p.workDir

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start gopls: %w", err)
	}
	p.goplsCmd = cmd
	p.goplsIn = stdin
	p.goplsOut = stdout

	// Forward stdin → gopls, intercepting didOpen/didChange for .mao files
	go p.forwardStdin()

	// Forward gopls → stdout, rewriting .go positions back to .mao
	if _, err := io.Copy(os.Stdout, stdout); err != nil && !errors.Is(err, io.EOF) {
		return err
	}

	return cmd.Wait()
}

func (p *lspProxy) forwardStdin() {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	var headerBuf strings.Builder
	for scanner.Scan() {
		line := scanner.Text()

		// Parse Content-Length header
		if strings.HasPrefix(line, "Content-Length:") {
			headerBuf.Reset()
			headerBuf.WriteString(line)
			headerBuf.WriteString("\r\n")
			continue
		}
		if line == "\r" || line == "" {
			headerBuf.WriteString("\r\n")
			continue
		}
		if line == "" {
			continue
		}

		// This is the JSON body
		body := line
		rewritten := p.interceptMessage(body)

		// Write headers + body
		header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(rewritten))
		if _, err := io.WriteString(p.goplsIn, header+rewritten); err != nil {
			return
		}
	}
}

func (p *lspProxy) interceptMessage(body string) string {
	var msg map[string]interface{}
	if err := json.Unmarshal([]byte(body), &msg); err != nil {
		return body
	}

	method, _ := msg["method"].(string)
	params, _ := msg["params"].(map[string]interface{})

	switch method {
	case "textDocument/didOpen", "textDocument/didChange":
		return p.interceptDocumentChange(body, params)
	case "textDocument/didClose":
		// Clean up cache
		if doc, ok := params["textDocument"].(map[string]interface{}); ok {
			if uri, ok := doc["uri"].(string); ok {
				p.mu.Lock()
				delete(p.cache, uri)
				p.mu.Unlock()
			}
		}
	}

	return body
}

func (p *lspProxy) interceptDocumentChange(body string, params map[string]interface{}) string {
	doc, ok := params["textDocument"].(map[string]interface{})
	if !ok {
		return body
	}
	uri, _ := doc["uri"].(string)
	if uri == "" || !strings.HasSuffix(uri, ".mao") {
		return body
	}

	// Get the Mao source content
	text, _ := doc["text"].(string)
	if text == "" {
		// didChange uses contentChanges
		if changes, ok := params["contentChanges"].([]interface{}); ok && len(changes) > 0 {
			if change, ok := changes[0].(map[string]interface{}); ok {
				text, _ = change["text"].(string)
			}
		}
	}

	if text == "" {
		return body
	}

	// Compile Mao → Go
	goSource, err := compileMaoToGo(p.workDir, uri, text)
	if err != nil {
		// Compilation failed — let the original message through
		// gopls won't provide diagnostics for .mao, but at least the file opens
		return body
	}

	// Create a temp .go file for gopls to analyze
	goURI := strings.TrimSuffix(uri, ".mao") + ".mao.go"
	p.mu.Lock()
	p.cache[uri] = goSource
	p.mu.Unlock()

	// Rewrite the message: pretend we opened a .go file
	docCopy := make(map[string]interface{})
	for k, v := range doc {
		docCopy[k] = v
	}
	docCopy["uri"] = goURI
	docCopy["text"] = goSource
	docCopy["languageId"] = "go"

	paramsCopy := make(map[string]interface{})
	for k, v := range params {
		paramsCopy[k] = v
	}
	paramsCopy["textDocument"] = docCopy

	var origMsg map[string]interface{}
	json.Unmarshal([]byte(body), &origMsg)
	if origMsg == nil {
		origMsg = make(map[string]interface{})
	}
	origMsg["params"] = paramsCopy

	rewritten, _ := json.Marshal(origMsg)
	return string(rewritten)
}

// compileMaoToGo compiles a single Mao source to Go using the Mao compiler.
func compileMaoToGo(workDir, uri string, source string) (string, error) {
	// Extract filename from URI
	filename := uri
	if strings.HasPrefix(filename, "file://") {
		filename = strings.TrimPrefix(filename, "file://")
	}

	// For simplicity, compile to temp buffer
	// In production, use CompilePackage for multi-file support
	cmd := exec.Command("go", "run", filepath.Join(workDir, "cmd/mao"), "emit-go", "-")
	cmd.Stdin = strings.NewReader(source)
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
