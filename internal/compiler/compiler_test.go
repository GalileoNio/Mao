package compiler

import (
	"bytes"
	"go/format"
	goparser "go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestCompileTableProgram(t *testing.T) {
	source := []byte(`package main

import "fmt"

func main() {
	values := [1, 1, 2]
	values[3] = 4
	values.DeleteAt(1)
	fmt.Println(len(values), values.get(2, 0))
}
`)

	generated, err := Compile("table.mao", source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := goparser.ParseFile(token.NewFileSet(), "table.go", generated, goparser.AllErrors); err != nil {
		t.Fatalf("generated invalid Go: %v\n%s", err, generated)
	}
	output := string(generated)
	for _, expected := range []string{
		`maort.NewTable[int, int]()`,
		`values.Set(3, 4)`,
		`values.DeleteAt(1)`,
		`values.Len()`,
		`values.GetLazy(2, func() int`,
	} {
		if !strings.Contains(output, expected) {
			t.Errorf("generated Go does not contain %q:\n%s", expected, output)
		}
	}
}

func TestCompileNullableTable(t *testing.T) {
	source := []byte(`package main

func main() {
	values := ["cat": null, "dog": 5]
	first := values["cat"]
	missing := values["fox"]
}
`)

	generated, err := Compile("nullable.mao", source)
	if err != nil {
		t.Fatal(err)
	}
	output := string(generated)
	for _, expected := range []string{
		`maort.NewTable[string, maort.Optional[int]]()`,
		`maort.Null[int]()`,
		`maort.Some[int](5)`,
		`maort.Flatten[int](values.Index("cat"))`,
		`maort.Flatten[int](values.Index("fox"))`,
	} {
		if !strings.Contains(output, expected) {
			t.Errorf("generated Go does not contain %q:\n%s", expected, output)
		}
	}
}

func TestCompileExplicitOptional(t *testing.T) {
	source := []byte(`package main

func main() {
	int? empty = null
	int? count = 0
	count = null
}
`)

	generated, err := Compile("optional.mao", source)
	if err != nil {
		t.Fatal(err)
	}
	output := string(generated)
	if strings.Count(output, `maort.Null[int]`) != 2 {
		t.Fatalf("expected two null optionals:\n%s", output)
	}
	if !strings.Contains(output, `maort.Some[int](0)`) {
		t.Fatalf("expected wrapped zero optional:\n%s", output)
	}
}

func TestRejectUntypedNull(t *testing.T) {
	_, err := Compile("null.mao", []byte("package main\nfunc main() { value := null }\n"))
	if err == nil || !strings.Contains(err.Error(), "explicit nullable target type") {
		t.Fatalf("expected untyped null error, got %v", err)
	}
}

func TestCompileControlFlowAndWidening(t *testing.T) {
	source := []byte(`package main

func widen(int64 value) int64 {
	return value
}

func count(int limit) int {
	total := 0
	for index := 0; index < limit; index++ {
		if index == 2 {
			continue
		}
		total += index
	}
	return total
}

func main() {
	int32 small = 7
	int64 large = widen(small)
}
`)
	generated, err := Compile("control.mao", source)
	if err != nil {
		t.Fatal(err)
	}
	output := string(generated)
	for _, expected := range []string{
		`for index := 0; index < limit; index++`,
		`total += index`,
		`widen(int64(small))`,
	} {
		if !strings.Contains(output, expected) {
			t.Errorf("generated Go does not contain %q:\n%s", expected, output)
		}
	}
}

func TestCompileTypesAndPackageDeclarations(t *testing.T) {
	source := []byte("package model\n\n" +
		"type Person struct {\n" +
		"    string Name `json:\"name\"`\n" +
		"    int Age\n" +
		"}\n\n" +
		"func (Person person) Label() string {\n" +
		"    return person.Name\n" +
		"}\n\n" +
		"func NewPerson() Person {\n" +
		"    return Person{Name: \"Mao\", Age: 3}\n" +
		"}\n\n" +
		"type Count = int64\n" +
		"const DefaultAge = 3\n" +
		"table<string, int> Ages\n")
	generated, err := Compile("model.mao", source)
	if err != nil {
		t.Fatal(err)
	}
	output := string(generated)
	for _, expected := range []string{
		"type Person struct",
		`Name string ` + "`json:\"name\"`",
		"type Count = int64",
		"func (person Person) Label() string",
		`return Person{Name: "Mao", Age: 3}`,
		"const DefaultAge = 3",
		"var Ages maort.Table[string, int] = maort.NewTable[string, int]()",
	} {
		if !strings.Contains(output, expected) {
			t.Errorf("generated Go does not contain %q:\n%s", expected, output)
		}
	}
}

func TestCompileGenericDefinitionsAndCalls(t *testing.T) {
	source := []byte(`package main

func identity<T any>(T value) T {
	return value
}

func main() {
	int explicit = identity<int>(3)
	int inferred = identity(4)
}
`)
	generated, err := Compile("generic.mao", source)
	if err != nil {
		t.Fatal(err)
	}
	output := string(generated)
	for _, expected := range []string{
		`func identity[T any](value T) T`,
		`identity[int](3)`,
		`identity(4)`,
	} {
		if !strings.Contains(output, expected) {
			t.Errorf("generated Go does not contain %q:\n%s", expected, output)
		}
	}
}

func TestCompileMultipleAssignment(t *testing.T) {
	source := []byte(`package main

func pair() (int, string) {
	return 1, "one"
}

func main() {
	number, word := pair()
	number, word = 2, "two"
}
`)
	generated, err := Compile("multiple.mao", source)
	if err != nil {
		t.Fatal(err)
	}
	output := string(generated)
	if !strings.Contains(output, `number, word := pair()`) ||
		!strings.Contains(output, `number, word = 2, "two"`) {
		t.Fatalf("multiple assignment was not preserved:\n%s", output)
	}
}

func TestMultipleAssignmentPropagatesResultTypes(t *testing.T) {
	source := []byte(`package main

func pair() (int32, string) {
	return 1, "one"
}

func main() {
	number, word := pair()
	int64 widened = number
	string copied = word
}
`)
	generated, err := Compile("multiple-types.mao", source)
	if err != nil {
		t.Fatal(err)
	}
	output := string(generated)
	if !strings.Contains(output, `var widened int64 = int64(number)`) ||
		!strings.Contains(output, `var copied string = word`) {
		t.Fatalf("multiple result types were not propagated:\n%s", output)
	}
}

func TestNumericWideningBoundaries(t *testing.T) {
	allowed := [][2]string{
		{"int8", "int16"}, {"int8", "int32"}, {"int8", "int64"},
		{"int16", "int32"}, {"int16", "int64"}, {"int32", "int64"},
		{"byte", "uint16"}, {"uint8", "uint32"}, {"uint16", "uint64"},
		{"uint32", "uint64"}, {"int", "int64"}, {"uint", "uint64"},
		{"uintptr", "uint64"}, {"float32", "float"},
	}
	for _, pair := range allowed {
		name := pair[0] + "_to_" + pair[1]
		t.Run(name, func(t *testing.T) {
			source := []byte("package main\nfunc main() { " + pair[0] +
				" source = 1\n" + pair[1] + " target = source\n_ = target }\n")
			if _, err := Compile(name+".mao", source); err != nil {
				t.Fatalf("safe widening was rejected: %v", err)
			}
		})
	}

	rejected := [][2]string{
		{"int64", "int32"}, {"uint64", "uint32"}, {"int32", "uint64"},
		{"uint32", "int64"}, {"int", "float"}, {"float", "float32"},
		{"float", "int64"}, {"int64", "int"},
	}
	for _, pair := range rejected {
		name := pair[0] + "_to_" + pair[1]
		t.Run(name, func(t *testing.T) {
			source := []byte("package main\nfunc main() { " + pair[0] +
				" source = 1\n" + pair[1] + " target = source\n_ = target }\n")
			_, err := Compile(name+".mao", source)
			if err == nil || !strings.Contains(err.Error(), "explicit conversion") {
				t.Fatalf("unsafe conversion was not rejected: %v", err)
			}
		})
	}
}

func TestNamedTypesRequireExplicitConversion(t *testing.T) {
	source := []byte(`package main
type Small int32
type Large int64
func main() {
	Small source = 1
	Large target = source
}
`)
	_, err := Compile("named.mao", source)
	if err == nil || !strings.Contains(err.Error(), "explicit conversion") {
		t.Fatalf("expected named-type conversion error, got %v", err)
	}
}

func TestOptionalNarrowingIsInvalidatedByAssignment(t *testing.T) {
	valid := []byte(`package main
func main() {
	int? value = 1
	if value != null {
		int exact = value
		_ = exact
	}
}
`)
	if _, err := Compile("narrow.mao", valid); err != nil {
		t.Fatal(err)
	}

	invalid := []byte(`package main
func main() {
	int? value = 1
	if value != null {
		value = 2
		int exact = value
	}
}
`)
	_, err := Compile("narrow-invalid.mao", invalid)
	if err == nil || !strings.Contains(err.Error(), "explicit conversion") {
		t.Fatalf("expected invalidated narrowing error, got %v", err)
	}
}

func TestRejectStaticallyIncomparableTableKey(t *testing.T) {
	source := []byte("package main\nfunc main() { table<int[], int> values = [] }\n")
	_, err := Compile("keys.mao", source)
	if err == nil || !strings.Contains(err.Error(), "table key type int[] is not comparable") {
		t.Fatalf("expected incomparable key diagnostic, got %v", err)
	}
}

func TestWidenImportedFunctionArguments(t *testing.T) {
	source := []byte(`package main

import "time"

func main() {
	int32 seconds = 1
	_ = time.Unix(seconds, 0)
}
`)
	generated, err := Compile("imported.mao", source)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(generated), `time.Unix(int64(seconds), 0)`) {
		t.Fatalf("imported argument was not widened:\n%s", generated)
	}
}

func TestCompileInterfacesFunctionsAndChannels(t *testing.T) {
	source := []byte(`package main

type Reader interface {
	Read(byte[] data) (int, error)
}

type Transform func(int value) string

func total(int... values) int {
	result := 0
	for _, value := range values {
		result += value
	}
	return result
}

func main() {
	chan<int> events = make(chan<int>, 1)
	events <- 7
	value := <-events
	_ = func(int item) int { return item + value }(3)
	int[] native = int[]{1, 2}
	_ = total(native...)
	switch value {
	case 7:
		value++
	default:
		value = 0
	}
	select {
	case events <- value:
	default:
	}
}
`)
	generated, err := Compile("types.mao", source)
	if err != nil {
		t.Fatal(err)
	}
	output := string(generated)
	for _, expected := range []string{
		`Read(data []byte) (int, error)`,
		`type Transform func(value int) string`,
		`var events chan int = make(chan int, 1)`,
		`events <- 7`,
		`func total(values ...int) int`,
		`total(native...)`,
		`switch value`,
		`select {`,
	} {
		if !strings.Contains(output, expected) {
			t.Errorf("generated Go does not contain %q:\n%s", expected, output)
		}
	}
}

func TestNativeMapTypeParsing(t *testing.T) {
	tokens, err := scan("map.mao", []byte(
		"package main\nfunc main() { string:int[] values = map([]) }\n",
	))
	if err != nil {
		t.Fatal(err)
	}
	tree, err := newParser(tokens).parseProgram()
	if err != nil {
		t.Fatal(err)
	}
	declaration := tree.functions[0].body[0].(variableDeclaration)
	if declaration.typ.kind != "map" {
		t.Fatalf("expected map type, got %#v", declaration.typ)
	}
}

func TestRejectGoMapTypeSyntaxWithMigrationHint(t *testing.T) {
	source := []byte("package main\nfunc main() { map[string]int values }\n")
	_, err := Compile("go-map.mao", source)
	if err == nil || !strings.Contains(err.Error(), "use the Mao front type K:V[]") {
		t.Fatalf("expected native map syntax hint, got %v", err)
	}
}

func TestNativeMapBuiltinsRemainAvailable(t *testing.T) {
	source := []byte(`package main
func main() {
	string:int[] values = string:int[]{"cat": 3}
	delete(values, "cat")
	clear(values)
}
`)
	generated, err := Compile("native-builtins.mao", source)
	if err != nil {
		t.Fatal(err)
	}
	output := string(generated)
	if !strings.Contains(output, `delete(values, "cat")`) ||
		!strings.Contains(output, `clear(values)`) {
		t.Fatalf("Go native collection builtins were changed:\n%s", output)
	}
}

func TestImportedGenericAPI(t *testing.T) {
	source := []byte(`package main
import "slices"
func main() {
	int[] source = int[]{1, 2}
	int[] cloned = slices.Clone<int>(source)
	_ = cloned
}
`)
	generated, err := Compile("generic-api.mao", source)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(generated), `slices.Clone[int](source)`) {
		t.Fatalf("generic Go API was not emitted correctly:\n%s", generated)
	}
}

func TestExplicitTableTargetRejectsIncompatibleItems(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{"key", `table<int, int> values = ["cat": 1]`},
		{"value", `table<string, int> values = ["cat": "old"]`},
		{"null", `table<string, int> values = ["cat": null]`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := []byte("package main\nfunc main() { " + test.source + "\n_ = values }\n")
			if _, err := Compile(test.name+".mao", source); err == nil {
				t.Fatal("expected incompatible table item error")
			}
		})
	}
}

func TestSelectParsing(t *testing.T) {
	source := []byte(
		"package main\nfunc main() { chan<int> events\nint value = 1\nselect { case events <- value: default: } }\n",
	)
	tokens, err := scan("select.mao", source)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := newParser(tokens).parseProgram()
	if err != nil {
		t.Fatal(err)
	}
	selection := tree.functions[0].body[2].(selectStatement)
	send := selection.cases[0].communication.(sendStatement)
	if send.channel.(identifier).name != "events" || send.value.(identifier).name != "value" {
		t.Fatalf("unexpected send statement: %#v", send)
	}
	if _, err := Compile("select.mao", source); err != nil {
		t.Fatal(err)
	}
}

func TestSwitchInitAndSelectReceiveScopes(t *testing.T) {
	source := []byte(`package main
func main() {
	int value = 1
	switch candidate := value + 1; candidate {
	case 2:
		int copy = candidate
		_ = copy
	}
	chan<int32> events = make(chan<int32>, 1)
	select {
	case received := <-events:
		int64 widened = received
		_ = widened
	default:
	}
}
`)
	generated, err := Compile("scopes.mao", source)
	if err != nil {
		t.Fatal(err)
	}
	output := string(generated)
	for _, expected := range []string{
		`switch candidate := value + 1; candidate`,
		`case received := <-events:`,
		`var widened int64 = int64(received)`,
	} {
		if !strings.Contains(output, expected) {
			t.Errorf("generated Go does not contain %q:\n%s", expected, output)
		}
	}
}

func TestNullAndNilCanBeShadowed(t *testing.T) {
	source := []byte(`package main

func main() {
	int null = 4
	int nil = 5
	_ = null + nil
}
`)
	generated, err := Compile("shadow.mao", source)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(generated), `var null int = 4`) ||
		!strings.Contains(string(generated), `var nil int = 5`) {
		t.Fatalf("shadowed identifiers were not emitted:\n%s", generated)
	}

	_, err = Compile("nil.mao", []byte("package main\nfunc main() { _ = nil }\n"))
	if err == nil || !strings.Contains(err.Error(), "use null") {
		t.Fatalf("expected unshadowed nil diagnostic, got %v", err)
	}
}

func TestGeneratedGoIsStableAndFormatted(t *testing.T) {
	source := []byte("package main\nfunc main() { values := [1, 2, 3] }\n")
	first, err := Compile("stable.mao", source)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Compile("stable.mao", source)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("the same Mao source generated different Go output")
	}
	formatted, err := format.Source(first)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, formatted) {
		t.Fatalf("generated Go was not gofmt-stable:\n%s", first)
	}
}

func TestCompilePackageSharesFunctionsAndAliasesAcrossMaoFiles(t *testing.T) {
	sources := map[string][]byte{
		"types.mao": []byte(`package shared
type Count = int32
func count() Count { return 3 }
`),
		"use.mao": []byte(`package shared
func widened() int64 {
	value := count()
	return value
}
`),
	}
	generated, err := CompilePackage(sources)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(generated["use.mao"]), `return int64(value)`) {
		t.Fatalf("cross-file alias or function type was not used:\n%s", generated["use.mao"])
	}

	sources["other.mao"] = []byte("package other\n")
	_, err = CompilePackage(sources)
	if err == nil || !strings.Contains(err.Error(), "does not match package") {
		t.Fatalf("expected package mismatch error, got %v", err)
	}
}

func TestNullSupportsGoNilableTypes(t *testing.T) {
	source := []byte(`package main

func main() {
	int[] values = null
	int:string[] settings = null
	*int pointer = null
	chan<int> events = null
	func() callback = null
	if values == null {}
}
`)
	if _, err := Compile("nilable.mao", source); err != nil {
		t.Fatal(err)
	}
}

func TestRejectChineseSyntaxForCurrentStage(t *testing.T) {
	_, err := Compile("future.mao", []byte("package main\nfunc main() { fmt。Println(1) }\n"))
	if err == nil || !strings.Contains(err.Error(), "non-ASCII source syntax is not enabled") {
		t.Fatalf("expected current-stage Chinese syntax error, got %v", err)
	}
}
