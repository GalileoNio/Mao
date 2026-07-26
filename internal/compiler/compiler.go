package compiler

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/importer"
	"go/token"
	"go/types"
	pathpkg "path"
	"path/filepath"
	"strconv"
	"strings"
	"text/scanner"
	"unicode"
	"unicode/utf8"
)

const runtimeImport = "github.com/GalileoNio/Mao/runtime"

type sourceToken struct {
	kind rune
	text string
	pos  scanner.Position
}

type program struct {
	packageName string
	imports     []importDeclaration
	types       []typeDeclaration
	globals     []statement
	functions   []function
}

type importDeclaration struct {
	name string
	path string
}

type typeDeclaration struct {
	name       string
	alias      bool
	parameters []field
	typ        maoType
}

type function struct {
	receiver   *field
	name       string
	typeParams []field
	params     []field
	results    []field
	body       []statement
}

type field struct {
	name string
	typ  maoType
	tag  string
}

type statement interface {
	statementNode()
}

type shortDeclaration struct {
	name  string
	value expression
}

func (shortDeclaration) statementNode() {}

type multiAssignment struct {
	left  []expression
	right []expression
	op    string
}

func (multiAssignment) statementNode() {}

type variableDeclaration struct {
	name  string
	typ   maoType
	value expression
}

func (variableDeclaration) statementNode() {}

type constantDeclaration struct {
	name  string
	value expression
}

func (constantDeclaration) statementNode() {}

type declarationGroup struct {
	kind         string
	declarations []statement
}

func (declarationGroup) statementNode() {}

type assignment struct {
	left  expression
	right expression
	op    string
}

func (assignment) statementNode() {}

type expressionStatement struct {
	value expression
}

func (expressionStatement) statementNode() {}

type returnStatement struct {
	values []expression
}

func (returnStatement) statementNode() {}

type blockStatement struct {
	body []statement
}

func (blockStatement) statementNode() {}

type ifStatement struct {
	initial   statement
	condition expression
	body      blockStatement
	otherwise statement
}

func (ifStatement) statementNode() {}

type rangeStatement struct {
	key      string
	value    string
	iterable expression
	body     blockStatement
}

func (rangeStatement) statementNode() {}

type forStatement struct {
	initial   statement
	condition expression
	post      statement
	body      blockStatement
}

func (forStatement) statementNode() {}

type branchStatement struct {
	kind  string
	label string
}

func (branchStatement) statementNode() {}

type labeledStatement struct {
	label string
	body  statement
}

func (labeledStatement) statementNode() {}

type incrementStatement struct {
	value expression
	op    string
}

func (incrementStatement) statementNode() {}

type actionStatement struct {
	kind  string
	value expression
}

func (actionStatement) statementNode() {}

type sendStatement struct {
	channel expression
	value   expression
}

func (sendStatement) statementNode() {}

type switchStatement struct {
	initial statement
	value   expression
	cases   []caseClause
}

func (switchStatement) statementNode() {}

type caseClause struct {
	values []expression
	body   []statement
}

type selectStatement struct {
	cases []selectClause
}

func (selectStatement) statementNode() {}

type selectClause struct {
	communication statement
	body          []statement
}

type expression interface {
	expressionNode()
}

type identifier struct{ name string }

func (identifier) expressionNode() {}

type basicLiteral struct {
	kind  rune
	value string
}

func (basicLiteral) expressionNode() {}

type nullLiteral struct{}

func (nullLiteral) expressionNode() {}

type selectorExpression struct {
	receiver expression
	name     string
}

func (selectorExpression) expressionNode() {}

type callExpression struct {
	function  expression
	arguments []expression
	ellipsis  bool
}

func (callExpression) expressionNode() {}

type indexExpression struct {
	receiver expression
	key      expression
}

func (indexExpression) expressionNode() {}

type sliceExpression struct {
	receiver expression
	low      expression
	high     expression
	maximum  expression
}

func (sliceExpression) expressionNode() {}

type typeExpression struct {
	typ maoType
}

func (typeExpression) expressionNode() {}

type genericExpression struct {
	base      expression
	arguments []maoType
}

func (genericExpression) expressionNode() {}

type typeAssertionExpression struct {
	receiver expression
	typ      maoType
}

func (typeAssertionExpression) expressionNode() {}

type functionLiteral struct {
	params  []field
	results []field
	body    blockStatement
}

func (functionLiteral) expressionNode() {}

type compositeLiteral struct {
	typ   expression
	items []compositeItem
}

func (compositeLiteral) expressionNode() {}

type compositeItem struct {
	key   expression
	value expression
}

type unaryExpression struct {
	operator string
	value    expression
}

func (unaryExpression) expressionNode() {}

type binaryExpression struct {
	left     expression
	operator string
	right    expression
}

func (binaryExpression) expressionNode() {}

type tableItem struct {
	key      expression
	value    expression
	explicit bool
}

type tableLiteral struct {
	items []tableItem
}

func (tableLiteral) expressionNode() {}

type maoType struct {
	kind    string
	name    string
	key     *maoType
	value   *maoType
	element *maoType
	args    []maoType
	fields  []field
	params  []field
	results []field
	length  int
}

func basicType(name string) maoType {
	if name == "float" {
		name = "float64"
	}
	return maoType{kind: "basic", name: name}
}

func unknownType() maoType {
	return maoType{kind: "unknown"}
}

func nullType() maoType {
	return maoType{kind: "null"}
}

func tableType(key, value maoType) maoType {
	return maoType{kind: "table", key: &key, value: &value}
}

func optionalType(element maoType) maoType {
	if element.kind == "optional" {
		return element
	}
	return maoType{kind: "optional", element: &element}
}

func entryType(key, value maoType) maoType {
	return maoType{kind: "entry", key: &key, value: &value}
}

func sliceType(element maoType) maoType {
	return maoType{kind: "slice", element: &element}
}

func Compile(filename string, source []byte) ([]byte, error) {
	tree, err := parse(filename, source)
	if err != nil {
		return nil, err
	}
	return emit(filename, tree, nil, nil)
}

func CheckSyntax(filename string, source []byte) error {
	_, err := parse(filename, source)
	return err
}

func CompilePackage(sources map[string][]byte) (map[string][]byte, error) {
	trees := make(map[string]program, len(sources))
	tokenSets := make(map[string][]sourceToken, len(sources))
	typeNames := make(map[string]bool)
	functions := make(map[string]function)
	aliases := make(map[string]maoType)
	packageName := ""
	for filename, source := range sources {
		tokens, err := scan(filename, source)
		if err != nil {
			return nil, err
		}
		tokenSets[filename] = tokens
		for name := range collectTypeNames(tokens) {
			typeNames[name] = true
		}
	}
	for filename, tokens := range tokenSets {
		sourceParser := newParser(tokens)
		for name := range typeNames {
			sourceParser.typeNames[name] = true
		}
		tree, err := sourceParser.parseProgram()
		if err != nil {
			return nil, err
		}
		if packageName == "" {
			packageName = tree.packageName
		} else if tree.packageName != packageName {
			return nil, fmt.Errorf(
				"%s: package %s does not match package %s", filename, tree.packageName, packageName,
			)
		}
		trees[filename] = tree
		for _, declaration := range tree.functions {
			if declaration.receiver == nil {
				functions[declaration.name] = declaration
			}
		}
		for _, declaration := range tree.types {
			if declaration.alias {
				aliases[declaration.name] = declaration.typ
			}
		}
	}
	result := make(map[string][]byte, len(sources))
	for filename, tree := range trees {
		generated, err := emit(filename, tree, functions, aliases)
		if err != nil {
			return nil, err
		}
		result[filename] = generated
	}
	return result, nil
}

func parse(filename string, source []byte) (program, error) {
	tokens, err := scan(filename, source)
	if err != nil {
		return program{}, err
	}
	sourceParser := newParser(tokens)
	for name := range collectTypeNames(tokens) {
		sourceParser.typeNames[name] = true
	}
	tree, err := sourceParser.parseProgram()
	if err != nil {
		return program{}, err
	}
	return tree, nil
}

func collectTypeNames(tokens []sourceToken) map[string]bool {
	result := make(map[string]bool)
	depth := 0
	for index, current := range tokens {
		switch current.kind {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		}
		if depth == 0 && current.kind == scanner.Ident && current.text == "type" &&
			index+1 < len(tokens) && tokens[index+1].kind == scanner.Ident {
			result[tokens[index+1].text] = true
		}
	}
	return result
}

func emit(
	filename string,
	tree program,
	functions map[string]function,
	aliases map[string]maoType,
) ([]byte, error) {
	emitter := newEmitter(filename)
	emitter.functions = functions
	emitter.aliases = aliases
	generated, err := emitter.emitProgram(tree)
	if err != nil {
		return nil, err
	}
	lineDirective := "//line " + filepath.ToSlash(filename) + ":1\n"
	return append([]byte(lineDirective), generated...), nil
}

func scan(filename string, source []byte) ([]sourceToken, error) {
	var lexer scanner.Scanner
	lexer.Init(bytes.NewReader(source))
	lexer.Filename = filename
	lexer.Mode = scanner.ScanIdents | scanner.ScanInts | scanner.ScanFloats |
		scanner.ScanChars | scanner.ScanStrings | scanner.ScanRawStrings |
		scanner.ScanComments | scanner.SkipComments
	lexer.Whitespace &^= 1 << '\n'

	var scanErrors []string
	lexer.Error = func(lexer *scanner.Scanner, message string) {
		scanErrors = append(scanErrors, fmt.Sprintf("%s: %s", lexer.Position, message))
	}

	var tokens []sourceToken
	for {
		kind := lexer.Scan()
		text := lexer.TokenText()
		position := lexer.Position
		if kind > utf8.RuneSelf && kind != scanner.EOF {
			scanErrors = append(scanErrors, fmt.Sprintf(
				"%s: non-ASCII source syntax is not enabled in this implementation stage",
				position,
			))
		}
		if kind == scanner.Ident {
			for _, character := range text {
				if character > utf8.RuneSelf {
					scanErrors = append(scanErrors, fmt.Sprintf(
						"%s: non-ASCII identifiers are not enabled in this implementation stage",
						position,
					))
					break
				}
			}
		}
		tokens = append(tokens, sourceToken{kind: kind, text: text, pos: position})
		if kind == scanner.EOF {
			break
		}
	}
	if len(scanErrors) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(scanErrors, "\n"))
	}
	return tokens, nil
}

type parser struct {
	tokens    []sourceToken
	index     int
	typeNames map[string]bool
	scopes    []map[string]bool
}

func newParser(tokens []sourceToken) *parser {
	return &parser{
		tokens: tokens, typeNames: make(map[string]bool),
		scopes: []map[string]bool{make(map[string]bool)},
	}
}

func (parser *parser) parseProgram() (program, error) {
	parser.skipBreaks()
	if !parser.matchWord("package") {
		return program{}, parser.errorf("expected package declaration")
	}
	packageName, err := parser.expectIdentifier()
	if err != nil {
		return program{}, err
	}
	result := program{packageName: packageName}
	parser.requireBreak()

	for {
		parser.skipBreaks()
		if !parser.matchWord("import") {
			break
		}
		imports, err := parser.parseImport()
		if err != nil {
			return program{}, err
		}
		result.imports = append(result.imports, imports...)
		parser.requireBreak()
	}

	for {
		parser.skipBreaks()
		if parser.current().kind == scanner.EOF {
			break
		}
		if parser.current().kind == scanner.Ident && parser.current().text == "func" {
			function, err := parser.parseFunction()
			if err != nil {
				return program{}, err
			}
			result.functions = append(result.functions, function)
			continue
		}
		if parser.matchWord("type") {
			declaration, err := parser.parseTypeDeclaration()
			if err != nil {
				return program{}, err
			}
			result.types = append(result.types, declaration)
			parser.typeNames[declaration.name] = true
			parser.requireBreak()
			continue
		}
		if parser.current().kind == scanner.Ident &&
			(parser.current().text == "const" || parser.current().text == "var") {
			declaration, err := parser.parseStatement()
			if err != nil {
				return program{}, err
			}
			result.globals = append(result.globals, declaration)
			parser.requireBreak()
			continue
		}
		declaration, ok, err := parser.tryVariableDeclaration()
		if err != nil {
			return program{}, err
		}
		if !ok {
			return program{}, parser.errorf("expected function, type, constant, or variable declaration")
		}
		result.globals = append(result.globals, declaration)
		parser.requireBreak()
	}
	return result, nil
}

func (parser *parser) parseTypeDeclaration() (typeDeclaration, error) {
	name, err := parser.expectIdentifier()
	if err != nil {
		return typeDeclaration{}, err
	}
	parameters, err := parser.parseTypeParameters()
	if err != nil {
		return typeDeclaration{}, err
	}
	alias := parser.match('=')
	typ, err := parser.parseType()
	if err != nil {
		return typeDeclaration{}, err
	}
	return typeDeclaration{name: name, alias: alias, parameters: parameters, typ: typ}, nil
}

func (parser *parser) parseImport() ([]importDeclaration, error) {
	parser.skipBreaks()
	if parser.match('(') {
		var imports []importDeclaration
		for {
			parser.skipBreaks()
			if parser.match(')') {
				return imports, nil
			}
			declaration, err := parser.parseImportSpec()
			if err != nil {
				return nil, err
			}
			imports = append(imports, declaration)
			parser.skipBreaks()
		}
	}
	declaration, err := parser.parseImportSpec()
	if err != nil {
		return nil, err
	}
	return []importDeclaration{declaration}, nil
}

func (parser *parser) parseImportSpec() (importDeclaration, error) {
	name := ""
	if parser.current().kind == scanner.Ident &&
		(parser.peek(1).kind == scanner.String || parser.peek(1).kind == scanner.RawString) {
		name = parser.current().text
		parser.index++
	} else if parser.match('.') {
		name = "."
	}
	path, err := parser.expectString()
	return importDeclaration{name: name, path: path}, err
}

func (parser *parser) parseFunction() (function, error) {
	if !parser.matchWord("func") {
		return function{}, parser.errorf("expected func declaration")
	}
	var receiver *field
	if parser.match('(') {
		fields, err := parser.parseFields(')')
		if err != nil {
			return function{}, err
		}
		if len(fields) != 1 || fields[0].name == "" {
			return function{}, parser.errorf("method receiver must have one type and name")
		}
		receiver = &fields[0]
	}
	name, err := parser.expectIdentifier()
	if err != nil {
		return function{}, err
	}
	typeParameters, err := parser.parseTypeParameters()
	if err != nil {
		return function{}, err
	}
	if err := parser.expect('('); err != nil {
		return function{}, err
	}
	params, err := parser.parseFields(')')
	if err != nil {
		return function{}, err
	}
	var results []field
	if parser.match('(') {
		results, err = parser.parseFields(')')
		if err != nil {
			return function{}, err
		}
	} else if parser.typeCanStart() {
		typ, err := parser.parseType()
		if err != nil {
			return function{}, err
		}
		results = []field{{typ: typ}}
	}
	parser.pushScope()
	if receiver != nil {
		parser.declare(receiver.name)
	}
	for _, field := range params {
		parser.declare(field.name)
	}
	for _, field := range results {
		parser.declare(field.name)
	}
	body, err := parser.parseBlock()
	parser.popScope()
	if err != nil {
		return function{}, err
	}
	return function{
		receiver: receiver, name: name, typeParams: typeParameters,
		params: params, results: results, body: body.body,
	}, nil
}

func (parser *parser) parseTypeParameters() ([]field, error) {
	if !parser.match('<') {
		return nil, nil
	}
	var result []field
	for {
		name, err := parser.expectIdentifier()
		if err != nil {
			return nil, err
		}
		constraint, err := parser.parseConstraint()
		if err != nil {
			return nil, err
		}
		result = append(result, field{name: name, typ: constraint})
		if parser.match('>') {
			return result, nil
		}
		if err := parser.expect(','); err != nil {
			return nil, err
		}
	}
}

func (parser *parser) parseConstraint() (maoType, error) {
	var terms []maoType
	for {
		approximate := parser.match('~')
		term, err := parser.parseType()
		if err != nil {
			return maoType{}, err
		}
		if approximate {
			element := term
			term = maoType{kind: "approximate", element: &element}
		}
		terms = append(terms, term)
		if !parser.match('|') {
			break
		}
	}
	if len(terms) == 1 {
		return terms[0], nil
	}
	return maoType{kind: "union", args: terms}, nil
}

func (parser *parser) parseFields(end rune) ([]field, error) {
	parser.skipBreaks()
	if parser.match(end) {
		return nil, nil
	}
	var result []field
	for {
		parser.skipBreaks()
		typ, err := parser.parseType()
		if err != nil {
			return nil, err
		}
		if parser.current().kind == '.' && parser.peek(1).kind == '.' && parser.peek(2).kind == '.' {
			parser.index += 3
			element := typ
			typ = maoType{kind: "variadic", element: &element}
		}
		name := ""
		if parser.current().kind == scanner.Ident {
			name = parser.current().text
			parser.index++
		}
		result = append(result, field{name: name, typ: typ})
		parser.skipBreaks()
		if parser.match(end) {
			return result, nil
		}
		if err := parser.expect(','); err != nil {
			return nil, err
		}
	}
}

func (parser *parser) parseBlock() (blockStatement, error) {
	parser.skipBreaks()
	if err := parser.expect('{'); err != nil {
		return blockStatement{}, err
	}
	parser.pushScope()
	defer parser.popScope()
	var result blockStatement
	for {
		parser.skipBreaks()
		if parser.match('}') {
			return result, nil
		}
		if parser.current().kind == scanner.EOF {
			return blockStatement{}, parser.errorf("unterminated block")
		}
		statement, err := parser.parseStatement()
		if err != nil {
			return blockStatement{}, err
		}
		result.body = append(result.body, statement)
		if parser.current().kind != '\n' && parser.current().kind != ';' && parser.current().kind != '}' {
			return blockStatement{}, parser.errorf("expected end of statement")
		}
	}
}

func (parser *parser) pushScope() {
	parser.scopes = append(parser.scopes, make(map[string]bool))
}

func (parser *parser) popScope() {
	parser.scopes = parser.scopes[:len(parser.scopes)-1]
}

func (parser *parser) declare(name string) {
	if name != "" && name != "_" {
		parser.scopes[len(parser.scopes)-1][name] = true
	}
}

func (parser *parser) isDeclared(name string) bool {
	for index := len(parser.scopes) - 1; index >= 0; index-- {
		if parser.scopes[index][name] {
			return true
		}
	}
	return false
}

func (parser *parser) parseStatement() (statement, error) {
	if parser.matchWord("const") {
		if parser.match('(') {
			group := declarationGroup{kind: "const"}
			for {
				parser.skipBreaks()
				if parser.match(')') {
					return group, nil
				}
				declaration, err := parser.parseConstantDeclaration()
				if err != nil {
					return nil, err
				}
				group.declarations = append(group.declarations, declaration)
				if parser.current().kind != '\n' && parser.current().kind != ';' &&
					parser.current().kind != ')' {
					return nil, parser.errorf("expected end of constant declaration")
				}
			}
		}
		return parser.parseConstantDeclaration()
	}
	if parser.matchWord("var") {
		if parser.match('(') {
			group := declarationGroup{kind: "var"}
			for {
				parser.skipBreaks()
				if parser.match(')') {
					return group, nil
				}
				declaration, ok, err := parser.tryVariableDeclaration()
				if err != nil {
					return nil, err
				}
				if !ok {
					return nil, parser.errorf("expected type-front variable declaration")
				}
				group.declarations = append(group.declarations, declaration)
				if parser.current().kind != '\n' && parser.current().kind != ';' &&
					parser.current().kind != ')' {
					return nil, parser.errorf("expected end of variable declaration")
				}
			}
		}
		declaration, ok, err := parser.tryVariableDeclaration()
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, parser.errorf("expected type-front variable declaration after var")
		}
		return declaration, nil
	}
	if parser.matchWord("return") {
		if parser.atBreak() || parser.current().kind == '}' {
			return returnStatement{}, nil
		}
		values, err := parser.parseExpressionList()
		return returnStatement{values: values}, err
	}
	if parser.matchWord("defer") {
		value, err := parser.parseExpression()
		return actionStatement{kind: "defer", value: value}, err
	}
	if parser.matchWord("go") {
		value, err := parser.parseExpression()
		return actionStatement{kind: "go", value: value}, err
	}
	if parser.matchWord("if") {
		var initial statement
		var condition expression
		var err error
		if parser.hasSemicolonBeforeBlock() {
			initial, err = parser.parseStatement()
			if err == nil {
				err = parser.expect(';')
			}
		}
		if err == nil {
			condition, err = parser.parseExpression()
		}
		if err != nil {
			return nil, err
		}
		body, err := parser.parseBlock()
		if err != nil {
			return nil, err
		}
		beforeElse := parser.index
		parser.skipBreaks()
		var otherwise statement
		if parser.matchWord("else") {
			if parser.current().kind == scanner.Ident && parser.current().text == "if" {
				otherwise, err = parser.parseStatement()
			} else {
				var block blockStatement
				block, err = parser.parseBlock()
				otherwise = block
			}
			if err != nil {
				return nil, err
			}
		} else {
			parser.index = beforeElse
		}
		return ifStatement{initial: initial, condition: condition, body: body, otherwise: otherwise}, nil
	}
	if parser.matchWord("switch") {
		var initial statement
		var value expression
		var err error
		if parser.current().kind != '{' {
			if parser.hasSemicolonBeforeBlock() {
				initial, err = parser.parseStatement()
				if err == nil {
					err = parser.expect(';')
				}
			}
			if err == nil && parser.current().kind != '{' {
				value, err = parser.parseExpression()
			}
			if err != nil {
				return nil, err
			}
		}
		parser.skipBreaks()
		if err := parser.expect('{'); err != nil {
			return nil, err
		}
		result := switchStatement{initial: initial, value: value}
		for {
			parser.skipBreaks()
			if parser.match('}') {
				return result, nil
			}
			clause := caseClause{}
			if parser.matchWord("case") {
				clause.values, err = parser.parseExpressionList()
				if err != nil {
					return nil, err
				}
			} else if !parser.matchWord("default") {
				return nil, parser.errorf("expected case, default, or }")
			}
			if err := parser.expect(':'); err != nil {
				return nil, err
			}
			for {
				parser.skipBreaks()
				if parser.current().kind == '}' ||
					(parser.current().kind == scanner.Ident &&
						(parser.current().text == "case" || parser.current().text == "default")) {
					break
				}
				statement, err := parser.parseStatement()
				if err != nil {
					return nil, err
				}
				clause.body = append(clause.body, statement)
				if parser.current().kind != '\n' && parser.current().kind != ';' &&
					parser.current().kind != '}' {
					return nil, parser.errorf("expected end of case statement")
				}
			}
			result.cases = append(result.cases, clause)
		}
	}
	if parser.matchWord("select") {
		parser.skipBreaks()
		if err := parser.expect('{'); err != nil {
			return nil, err
		}
		result := selectStatement{}
		for {
			parser.skipBreaks()
			if parser.match('}') {
				return result, nil
			}
			clause := selectClause{}
			if parser.matchWord("case") {
				communication, err := parser.parseStatement()
				if err != nil {
					return nil, err
				}
				clause.communication = communication
			} else if !parser.matchWord("default") {
				return nil, parser.errorf("expected case, default, or }")
			}
			if err := parser.expect(':'); err != nil {
				return nil, err
			}
			for {
				parser.skipBreaks()
				if parser.current().kind == '}' ||
					(parser.current().kind == scanner.Ident &&
						(parser.current().text == "case" || parser.current().text == "default")) {
					break
				}
				statement, err := parser.parseStatement()
				if err != nil {
					return nil, err
				}
				clause.body = append(clause.body, statement)
				if parser.current().kind != '\n' && parser.current().kind != ';' &&
					parser.current().kind != '}' {
					return nil, parser.errorf("expected end of select statement")
				}
			}
			result.cases = append(result.cases, clause)
		}
	}
	if parser.matchWord("for") {
		if parser.current().kind == '{' {
			body, err := parser.parseBlock()
			return forStatement{body: body}, err
		}
		if parser.hasSemicolonBeforeBlock() {
			var initial statement
			var condition expression
			var post statement
			var err error
			if parser.current().kind != ';' {
				initial, err = parser.parseStatement()
				if err != nil {
					return nil, err
				}
			}
			if err := parser.expect(';'); err != nil {
				return nil, err
			}
			if parser.current().kind != ';' {
				condition, err = parser.parseExpression()
				if err != nil {
					return nil, err
				}
			}
			if err := parser.expect(';'); err != nil {
				return nil, err
			}
			if parser.current().kind != '{' {
				post, err = parser.parseStatement()
				if err != nil {
					return nil, err
				}
			}
			body, err := parser.parseBlock()
			return forStatement{initial: initial, condition: condition, post: post, body: body}, err
		}
		start := parser.index
		if parser.current().kind == scanner.Ident {
			key := parser.current().text
			parser.index++
			value := ""
			if parser.match(',') {
				var err error
				value, err = parser.expectIdentifier()
				if err != nil {
					return nil, err
				}
			}
			if parser.match(':') && parser.match('=') && parser.matchWord("range") {
				iterable, err := parser.parseExpression()
				if err != nil {
					return nil, err
				}
				body, err := parser.parseBlock()
				return rangeStatement{key: key, value: value, iterable: iterable, body: body}, err
			}
		}
		parser.index = start
		condition, err := parser.parseExpression()
		if err != nil {
			return nil, err
		}
		body, err := parser.parseBlock()
		return forStatement{condition: condition, body: body}, err
	}
	if parser.matchWord("break") {
		label := ""
		if parser.current().kind == scanner.Ident {
			label = parser.current().text
			parser.index++
		}
		return branchStatement{kind: "break", label: label}, nil
	}
	if parser.matchWord("continue") {
		label := ""
		if parser.current().kind == scanner.Ident {
			label = parser.current().text
			parser.index++
		}
		return branchStatement{kind: "continue", label: label}, nil
	}
	if parser.matchWord("fallthrough") {
		return branchStatement{kind: "fallthrough"}, nil
	}
	if parser.matchWord("goto") {
		label, err := parser.expectIdentifier()
		return branchStatement{kind: "goto", label: label}, err
	}
	if declaration, ok, err := parser.tryMultiShortDeclaration(); ok || err != nil {
		return declaration, err
	}
	if declaration, ok, err := parser.tryVariableDeclaration(); ok || err != nil {
		return declaration, err
	}
	if parser.current().kind == scanner.Ident && parser.peek(1).kind == ':' &&
		parser.peek(2).kind != '=' {
		label := parser.current().text
		parser.index += 2
		parser.skipBreaks()
		body, err := parser.parseStatement()
		return labeledStatement{label: label, body: body}, err
	}
	if parser.current().kind == scanner.Ident && parser.peek(1).kind == ':' && parser.peek(2).kind == '=' {
		name := parser.current().text
		parser.index += 3
		value, err := parser.parseExpression()
		if err == nil {
			parser.declare(name)
		}
		return shortDeclaration{name: name, value: value}, err
	}

	left, err := parser.parseExpression()
	if err != nil {
		return nil, err
	}
	if parser.match(',') {
		leftValues := []expression{left}
		for {
			value, err := parser.parseExpression()
			if err != nil {
				return nil, err
			}
			leftValues = append(leftValues, value)
			if !parser.match(',') {
				break
			}
		}
		if !parser.match('=') {
			return nil, parser.errorf("multiple expressions require assignment")
		}
		rightValues, err := parser.parseExpressionList()
		return multiAssignment{left: leftValues, right: rightValues, op: "="}, err
	}
	if parser.match('=') {
		right, err := parser.parseExpression()
		return assignment{left: left, right: right, op: "="}, err
	}
	if parser.current().kind == '<' && parser.peek(1).kind == '-' {
		parser.index += 2
		right, err := parser.parseExpression()
		return sendStatement{channel: left, value: right}, err
	}
	if operator, width := parser.assignmentOperator(); width > 0 {
		parser.index += width
		right, err := parser.parseExpression()
		return assignment{left: left, right: right, op: operator}, err
	}
	if parser.current().kind == '+' && parser.peek(1).kind == '+' {
		parser.index += 2
		return incrementStatement{value: left, op: "++"}, nil
	}
	if parser.current().kind == '-' && parser.peek(1).kind == '-' {
		parser.index += 2
		return incrementStatement{value: left, op: "--"}, nil
	}
	return expressionStatement{value: left}, nil
}

func (parser *parser) parseConstantDeclaration() (statement, error) {
	name, err := parser.expectIdentifier()
	if err != nil {
		return nil, err
	}
	if err := parser.expect('='); err != nil {
		return nil, err
	}
	value, err := parser.parseExpression()
	if err == nil {
		parser.declare(name)
	}
	return constantDeclaration{name: name, value: value}, err
}

func (parser *parser) tryMultiShortDeclaration() (statement, bool, error) {
	start := parser.index
	if parser.current().kind != scanner.Ident {
		return nil, false, nil
	}
	var names []string
	for {
		name, err := parser.expectIdentifier()
		if err != nil {
			parser.index = start
			return nil, false, nil
		}
		names = append(names, name)
		if !parser.match(',') {
			break
		}
	}
	if len(names) < 2 || !parser.match(':') || !parser.match('=') {
		parser.index = start
		return nil, false, nil
	}
	values, err := parser.parseExpressionList()
	if err != nil {
		return nil, true, err
	}
	left := make([]expression, len(names))
	for index, name := range names {
		left[index] = identifier{name: name}
		parser.declare(name)
	}
	return multiAssignment{left: left, right: values, op: ":="}, true, nil
}

func (parser *parser) hasSemicolonBeforeBlock() bool {
	depth := 0
	for offset := 0; ; offset++ {
		current := parser.peek(offset)
		switch current.kind {
		case scanner.EOF, '\n':
			return false
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		case ';':
			if depth == 0 {
				return true
			}
		case '{':
			if depth == 0 {
				return false
			}
		}
	}
}

func (parser *parser) assignmentOperator() (string, int) {
	first, second, third := parser.current().kind, parser.peek(1).kind, parser.peek(2).kind
	if second == '=' && strings.ContainsRune("+-*/%&|^", first) {
		return string(first) + "=", 2
	}
	if first == '<' && second == '<' && third == '=' {
		return "<<=", 3
	}
	if first == '>' && second == '>' && third == '=' {
		return ">>=", 3
	}
	if first == '&' && second == '^' && third == '=' {
		return "&^=", 3
	}
	return "", 0
}

func (parser *parser) tryVariableDeclaration() (statement, bool, error) {
	start := parser.index
	if parser.current().kind == scanner.Ident && parser.current().text == "map" &&
		parser.peek(1).kind == '[' {
		return nil, true, parser.errorf(
			"Go map[K]V type syntax is not supported; use the Mao front type K:V[]",
		)
	}
	if !parser.typeCanStart() {
		return nil, false, nil
	}
	typ, err := parser.parseType()
	if err != nil {
		parser.index = start
		return nil, false, nil
	}
	if parser.current().kind != scanner.Ident {
		parser.index = start
		return nil, false, nil
	}
	name := parser.current().text
	parser.index++
	var value expression
	if parser.match('=') {
		value, err = parser.parseExpression()
		if err != nil {
			return nil, true, err
		}
	}
	parser.declare(name)
	return variableDeclaration{name: name, typ: typ, value: value}, true, nil
}

func (parser *parser) typeCanStart() bool {
	return parser.current().kind == scanner.Ident || parser.current().kind == '(' ||
		parser.current().kind == '*' ||
		(parser.current().kind == '<' && parser.peek(1).kind == '-')
}

func (parser *parser) parseType() (maoType, error) {
	if parser.current().kind == '<' && parser.peek(1).kind == '-' {
		parser.index += 2
		element, err := parser.parseType()
		if err != nil {
			return maoType{}, err
		}
		return maoType{kind: "channel", name: "receive", element: &element}, nil
	}
	if parser.match('*') {
		element, err := parser.parseType()
		if err != nil {
			return maoType{}, err
		}
		return maoType{kind: "pointer", element: &element}, nil
	}
	if parser.match('(') {
		result, err := parser.parseType()
		if err != nil {
			return maoType{}, err
		}
		if err := parser.expect(')'); err != nil {
			return maoType{}, err
		}
		return parser.parseTypeSuffixes(result)
	}
	name, err := parser.expectIdentifier()
	if err != nil {
		return maoType{}, err
	}
	if parser.current().kind == '.' && parser.peek(1).kind == scanner.Ident {
		parser.index++
		member, err := parser.expectIdentifier()
		if err != nil {
			return maoType{}, err
		}
		name += "." + member
	}
	var result maoType
	if name == "table" {
		if err := parser.expect('<'); err != nil {
			return maoType{}, err
		}
		key, err := parser.parseType()
		if err != nil {
			return maoType{}, err
		}
		if err := parser.expect(','); err != nil {
			return maoType{}, err
		}
		value, err := parser.parseType()
		if err != nil {
			return maoType{}, err
		}
		if err := parser.expect('>'); err != nil {
			return maoType{}, err
		}
		result = tableType(key, value)
	} else if name == "struct" {
		parser.skipBreaks()
		if err := parser.expect('{'); err != nil {
			return maoType{}, err
		}
		result = maoType{kind: "struct"}
		for {
			parser.skipBreaks()
			if parser.match('}') {
				break
			}
			fieldType, err := parser.parseType()
			if err != nil {
				return maoType{}, err
			}
			fieldName, err := parser.expectIdentifier()
			if err != nil {
				return maoType{}, err
			}
			tag := ""
			if parser.current().kind == scanner.RawString {
				tag = parser.current().text
				parser.index++
			}
			result.fields = append(result.fields, field{name: fieldName, typ: fieldType, tag: tag})
			if parser.current().kind != '\n' && parser.current().kind != ';' && parser.current().kind != '}' {
				return maoType{}, parser.errorf("expected end of struct field")
			}
		}
	} else if name == "interface" {
		parser.skipBreaks()
		if err := parser.expect('{'); err != nil {
			return maoType{}, err
		}
		result = maoType{kind: "interface"}
		for {
			parser.skipBreaks()
			if parser.match('}') {
				break
			}
			methodName, err := parser.expectIdentifier()
			if err != nil {
				return maoType{}, err
			}
			if !parser.match('(') {
				result.fields = append(result.fields, field{
					typ: maoType{kind: "named", name: methodName},
				})
			} else {
				params, err := parser.parseFields(')')
				if err != nil {
					return maoType{}, err
				}
				results, err := parser.parseResultFields()
				if err != nil {
					return maoType{}, err
				}
				result.fields = append(result.fields, field{
					name: methodName, typ: maoType{kind: "function", params: params, results: results},
				})
			}
			if parser.current().kind != '\n' && parser.current().kind != ';' && parser.current().kind != '}' {
				return maoType{}, parser.errorf("expected end of interface method")
			}
		}
	} else if name == "func" {
		if err := parser.expect('('); err != nil {
			return maoType{}, err
		}
		params, err := parser.parseFields(')')
		if err != nil {
			return maoType{}, err
		}
		results, err := parser.parseResultFields()
		if err != nil {
			return maoType{}, err
		}
		result = maoType{kind: "function", params: params, results: results}
	} else if name == "chan" {
		direction := "both"
		if parser.current().kind == '<' && parser.peek(1).kind == '-' {
			parser.index += 2
			direction = "send"
		}
		var element maoType
		if parser.match('<') {
			element, err = parser.parseType()
			if err == nil {
				err = parser.expect('>')
			}
		} else {
			element, err = parser.parseType()
		}
		if err != nil {
			return maoType{}, err
		}
		result = maoType{kind: "channel", name: direction, element: &element}
	} else {
		if isBasicType(name) {
			result = basicType(name)
		} else {
			result = maoType{kind: "named", name: name}
		}
		if parser.match('<') {
			for {
				argument, err := parser.parseType()
				if err != nil {
					return maoType{}, err
				}
				result.args = append(result.args, argument)
				if parser.match('>') {
					break
				}
				if err := parser.expect(','); err != nil {
					return maoType{}, err
				}
			}
			result.kind = "generic"
		}
	}
	if parser.match(':') {
		value, err := parser.parseType()
		if err != nil {
			return maoType{}, err
		}
		if value.kind != "slice" {
			return maoType{}, parser.errorf("native map type must end in []")
		}
		keyType := result
		valueType := *value.element
		result = maoType{kind: "map", key: &keyType, value: &valueType}
	}
	return parser.parseTypeSuffixes(result)
}

func (parser *parser) parseResultFields() ([]field, error) {
	if parser.match('(') {
		return parser.parseFields(')')
	}
	if parser.current().kind == scanner.Ident {
		switch parser.peek(1).kind {
		case '=', ',', ')':
			return nil, nil
		}
	}
	if parser.typeCanStart() {
		typ, err := parser.parseType()
		return []field{{typ: typ}}, err
	}
	return nil, nil
}

func isBasicType(name string) bool {
	switch name {
	case "any", "bool", "byte", "float", "float32", "float64", "int", "int8",
		"int16", "int32", "int64", "rune", "string", "uint", "uint8",
		"uint16", "uint32", "uint64", "uintptr":
		return true
	default:
		return false
	}
}

func (parser *parser) parseTypeSuffixes(result maoType) (maoType, error) {
	for {
		if parser.match('?') {
			result = optionalType(result)
			continue
		}
		if parser.match('[') {
			if parser.match(']') {
				result = sliceType(result)
				continue
			}
			if parser.current().kind != scanner.Int {
				return maoType{}, parser.errorf("expected array length")
			}
			length, err := strconv.Atoi(parser.current().text)
			if err != nil {
				return maoType{}, parser.errorf("invalid array length")
			}
			parser.index++
			if err := parser.expect(']'); err != nil {
				return maoType{}, err
			}
			element := result
			result = maoType{kind: "array", element: &element, length: length}
			continue
		}
		break
	}
	return result, nil
}

func (parser *parser) parseExpressionList() ([]expression, error) {
	var values []expression
	for {
		value, err := parser.parseExpression()
		if err != nil {
			return nil, err
		}
		values = append(values, value)
		if !parser.match(',') {
			return values, nil
		}
	}
}

func (parser *parser) parseExpression() (expression, error) {
	return parser.parseBinaryExpression(1)
}

func (parser *parser) parseBinaryExpression(minimumPrecedence int) (expression, error) {
	value, err := parser.parseUnaryExpression()
	if err != nil {
		return nil, err
	}
	for {
		operator, width := parser.binaryOperator()
		precedence := binaryPrecedence(operator)
		if precedence < minimumPrecedence {
			return value, nil
		}
		parser.index += width
		right, err := parser.parseBinaryExpression(precedence + 1)
		if err != nil {
			return nil, err
		}
		value = binaryExpression{left: value, operator: operator, right: right}
	}
}

func (parser *parser) parseUnaryExpression() (expression, error) {
	if operator, width := parser.unaryOperator(); width > 0 {
		parser.index += width
		value, err := parser.parseUnaryExpression()
		if err != nil {
			return nil, err
		}
		return unaryExpression{operator: operator, value: value}, nil
	}
	value, err := parser.parsePrimary()
	if err != nil {
		return nil, err
	}
	for {
		switch {
		case parser.current().kind == '.' &&
			(parser.peek(1).kind == scanner.Ident || parser.peek(1).kind == '('):
			parser.index++
			if parser.match('(') {
				typ, err := parser.parseType()
				if err != nil {
					return nil, err
				}
				if err := parser.expect(')'); err != nil {
					return nil, err
				}
				value = typeAssertionExpression{receiver: value, typ: typ}
				continue
			}
			name, err := parser.expectIdentifier()
			if err != nil {
				return nil, err
			}
			value = selectorExpression{receiver: value, name: name}
		case parser.match('('):
			var arguments []expression
			parser.skipBreaks()
			if !parser.match(')') {
				argumentIndex := 0
				ellipsis := false
				for {
					var argument expression
					var err error
					if name, ok := value.(identifier); ok &&
						(name.name == "make" || name.name == "new") && argumentIndex == 0 {
						var typ maoType
						typ, err = parser.parseType()
						argument = typeExpression{typ: typ}
					} else {
						argument, err = parser.parseExpression()
					}
					if err != nil {
						return nil, err
					}
					argumentIndex++
					arguments = append(arguments, argument)
					parser.skipBreaks()
					if parser.current().kind == '.' && parser.peek(1).kind == '.' &&
						parser.peek(2).kind == '.' {
						parser.index += 3
						ellipsis = true
						parser.skipBreaks()
						if err := parser.expect(')'); err != nil {
							return nil, err
						}
						value = callExpression{
							function: value, arguments: arguments, ellipsis: true,
						}
						break
					}
					if parser.match(')') {
						break
					}
					if err := parser.expect(','); err != nil {
						return nil, err
					}
					parser.skipBreaks()
				}
				if ellipsis {
					continue
				}
			}
			value = callExpression{function: value, arguments: arguments}
		case parser.match('['):
			var first expression
			var err error
			if parser.current().kind != ':' {
				first, err = parser.parseExpression()
				if err != nil {
					return nil, err
				}
			}
			if parser.match(':') {
				var high, maximum expression
				if parser.current().kind != ':' && parser.current().kind != ']' {
					high, err = parser.parseExpression()
					if err != nil {
						return nil, err
					}
				}
				if parser.match(':') {
					if parser.current().kind != ']' {
						maximum, err = parser.parseExpression()
						if err != nil {
							return nil, err
						}
					}
				}
				if err := parser.expect(']'); err != nil {
					return nil, err
				}
				value = sliceExpression{receiver: value, low: first, high: high, maximum: maximum}
				continue
			}
			if err := parser.expect(']'); err != nil {
				return nil, err
			}
			value = indexExpression{receiver: value, key: first}
		case parser.current().kind == '<':
			start := parser.index
			parser.index++
			var arguments []maoType
			for {
				argument, err := parser.parseType()
				if err != nil {
					parser.index = start
					break
				}
				arguments = append(arguments, argument)
				if parser.match('>') {
					if parser.current().kind == '(' || parser.current().kind == '.' {
						value = genericExpression{base: value, arguments: arguments}
					} else {
						parser.index = start
					}
					break
				}
				if !parser.match(',') {
					parser.index = start
					break
				}
			}
			if parser.index == start {
				return value, nil
			}
		case parser.current().kind == '{' && parser.isTypeExpression(value):
			parser.index++
			literal := compositeLiteral{typ: value}
			parser.skipBreaks()
			if parser.match('}') {
				value = literal
				continue
			}
			for {
				first, err := parser.parseExpression()
				if err != nil {
					return nil, err
				}
				item := compositeItem{value: first}
				if parser.match(':') {
					item.key = first
					item.value, err = parser.parseExpression()
					if err != nil {
						return nil, err
					}
				}
				literal.items = append(literal.items, item)
				parser.skipBreaks()
				if parser.match('}') {
					break
				}
				if err := parser.expect(','); err != nil {
					return nil, err
				}
				parser.skipBreaks()
				if parser.match('}') {
					break
				}
			}
			value = literal
		default:
			return value, nil
		}
	}
}

func (parser *parser) isTypeExpression(value expression) bool {
	switch expression := value.(type) {
	case identifier:
		first, _ := utf8.DecodeRuneInString(expression.name)
		return parser.typeNames[expression.name] || unicode.IsUpper(first)
	case selectorExpression, genericExpression, typeExpression:
		return true
	default:
		return false
	}
}

func (parser *parser) unaryOperator() (string, int) {
	switch parser.current().kind {
	case '+', '-', '!', '^', '*', '&':
		return parser.current().text, 1
	case '<':
		if parser.peek(1).kind == '-' {
			return "<-", 2
		}
	}
	return "", 0
}

func (parser *parser) binaryOperator() (string, int) {
	first := parser.current().kind
	second := parser.peek(1).kind
	if (first == '+' || first == '-') && second == first {
		return "", 0
	}
	if first == '<' && second == '-' {
		return "", 0
	}
	if second == '=' && strings.ContainsRune("+-*/%&|^", first) {
		return "", 0
	}
	switch {
	case first == '|' && second == '|':
		return "||", 2
	case first == '&' && second == '&':
		return "&&", 2
	case first == '=' && second == '=':
		return "==", 2
	case first == '!' && second == '=':
		return "!=", 2
	case first == '<' && second == '=':
		return "<=", 2
	case first == '>' && second == '=':
		return ">=", 2
	case first == '<' && second == '<':
		return "<<", 2
	case first == '>' && second == '>':
		return ">>", 2
	case first == '&' && second == '^':
		return "&^", 2
	case strings.ContainsRune("<>+-|^*/%&", first):
		return string(first), 1
	default:
		return "", 0
	}
}

func binaryPrecedence(operator string) int {
	switch operator {
	case "||":
		return 1
	case "&&":
		return 2
	case "==", "!=", "<", "<=", ">", ">=":
		return 3
	case "+", "-", "|", "^":
		return 4
	case "*", "/", "%", "<<", ">>", "&", "&^":
		return 5
	default:
		return 0
	}
}

func (parser *parser) parsePrimary() (expression, error) {
	current := parser.current()
	switch current.kind {
	case scanner.Ident:
		if (isBasicType(current.text) &&
			(parser.peek(1).kind == '[' || parser.peek(1).kind == '?' || parser.peek(1).kind == ':')) ||
			(current.text == "table" && parser.peek(1).kind == '<') {
			typ, err := parser.parseType()
			if err != nil {
				return nil, err
			}
			return typeExpression{typ: typ}, nil
		}
		if current.text == "func" {
			parser.index++
			if err := parser.expect('('); err != nil {
				return nil, err
			}
			params, err := parser.parseFields(')')
			if err != nil {
				return nil, err
			}
			var results []field
			if parser.match('(') {
				results, err = parser.parseFields(')')
			} else if parser.typeCanStart() && parser.current().kind != '{' {
				var typ maoType
				typ, err = parser.parseType()
				results = []field{{typ: typ}}
			}
			if err != nil {
				return nil, err
			}
			body, err := parser.parseBlock()
			return functionLiteral{params: params, results: results, body: body}, err
		}
		parser.index++
		switch current.text {
		case "null":
			if !parser.isDeclared("null") {
				return nullLiteral{}, nil
			}
			return identifier{name: "null"}, nil
		case "nil":
			if !parser.isDeclared("nil") {
				return nil, parser.errorf("nil is not predeclared in Mao; use null")
			}
			return identifier{name: "nil"}, nil
		case "true", "false":
			return basicLiteral{kind: scanner.Ident, value: current.text}, nil
		default:
			return identifier{name: current.text}, nil
		}
	case scanner.Int, scanner.Float, scanner.Char, scanner.String, scanner.RawString:
		parser.index++
		return basicLiteral{kind: current.kind, value: current.text}, nil
	case '[':
		return parser.parseTableLiteral()
	case '(':
		parser.index++
		value, err := parser.parseExpression()
		if err != nil {
			return nil, err
		}
		if err := parser.expect(')'); err != nil {
			return nil, err
		}
		return value, nil
	default:
		return nil, parser.errorf("expected expression")
	}
}

func (parser *parser) parseTableLiteral() (expression, error) {
	if err := parser.expect('['); err != nil {
		return nil, err
	}
	parser.skipBreaks()
	result := tableLiteral{}
	if parser.match(']') {
		return result, nil
	}
	for {
		first, err := parser.parseExpression()
		if err != nil {
			return nil, err
		}
		item := tableItem{value: first}
		if parser.match(':') {
			item.explicit = true
			item.key = first
			if parser.current().kind == ',' || parser.current().kind == ']' || parser.atBreak() {
				item.value = nullLiteral{}
			} else {
				item.value, err = parser.parseExpression()
				if err != nil {
					return nil, err
				}
			}
		}
		result.items = append(result.items, item)
		parser.skipBreaks()
		if parser.match(']') {
			return result, nil
		}
		if err := parser.expect(','); err != nil {
			return nil, err
		}
		parser.skipBreaks()
		if parser.match(']') {
			return result, nil
		}
	}
}

func (parser *parser) expect(kind rune) error {
	if parser.current().kind != kind {
		return parser.errorf("expected %q", string(kind))
	}
	parser.index++
	return nil
}

func (parser *parser) expectIdentifier() (string, error) {
	if parser.current().kind != scanner.Ident {
		return "", parser.errorf("expected identifier")
	}
	value := parser.current().text
	parser.index++
	return value, nil
}

func (parser *parser) expectString() (string, error) {
	if parser.current().kind != scanner.String && parser.current().kind != scanner.RawString {
		return "", parser.errorf("expected import path string")
	}
	value, err := strconv.Unquote(parser.current().text)
	if err != nil {
		return "", parser.errorf("invalid string literal")
	}
	parser.index++
	return value, nil
}

func (parser *parser) match(kind rune) bool {
	if parser.current().kind != kind {
		return false
	}
	parser.index++
	return true
}

func (parser *parser) matchWord(word string) bool {
	if parser.current().kind != scanner.Ident || parser.current().text != word {
		return false
	}
	parser.index++
	return true
}

func (parser *parser) current() sourceToken {
	if parser.index >= len(parser.tokens) {
		return sourceToken{kind: scanner.EOF}
	}
	return parser.tokens[parser.index]
}

func (parser *parser) peek(offset int) sourceToken {
	index := parser.index + offset
	if index >= len(parser.tokens) {
		return sourceToken{kind: scanner.EOF}
	}
	return parser.tokens[index]
}

func (parser *parser) skipBreaks() {
	for parser.current().kind == '\n' || parser.current().kind == ';' {
		parser.index++
	}
}

func (parser *parser) requireBreak() {
	parser.skipBreaks()
}

func (parser *parser) atBreak() bool {
	return parser.current().kind == '\n' || parser.current().kind == ';'
}

func (parser *parser) errorf(format string, arguments ...any) error {
	return fmt.Errorf("%s: %s", parser.current().pos, fmt.Sprintf(format, arguments...))
}

type emitter struct {
	filename     string
	needsRuntime bool
	variables    map[string]maoType
	narrowed     map[string]bool
	resultTypes  []maoType
	functions    map[string]function
	globals      map[string]maoType
	imported     map[string]*types.Package
	aliases      map[string]maoType
}

func newEmitter(filename string) *emitter {
	return &emitter{filename: filename}
}

func (emitter *emitter) emitProgram(source program) ([]byte, error) {
	file := &ast.File{Name: ast.NewIdent(source.packageName)}
	var declarations []ast.Decl
	if emitter.functions == nil {
		emitter.functions = make(map[string]function, len(source.functions))
	}
	if emitter.aliases == nil {
		emitter.aliases = make(map[string]maoType)
	}
	emitter.imported = make(map[string]*types.Package)
	for _, declaration := range source.imports {
		name := declaration.name
		if name == "" {
			name = pathpkg.Base(declaration.path)
		}
		if name == "_" || name == "." {
			continue
		}
		if imported, err := importer.Default().Import(declaration.path); err == nil {
			emitter.imported[name] = imported
		}
	}
	for _, function := range source.functions {
		if function.receiver == nil {
			emitter.functions[function.name] = function
		}
	}
	emitter.variables = make(map[string]maoType)
	emitter.narrowed = make(map[string]bool)
	emitter.globals = make(map[string]maoType)
	for _, declaration := range source.types {
		if declaration.alias {
			emitter.aliases[declaration.name] = declaration.typ
		}
		spec := &ast.TypeSpec{
			Name: ast.NewIdent(declaration.name),
			Type: emitter.emitType(declaration.typ),
		}
		if len(declaration.parameters) > 0 {
			spec.TypeParams = emitter.emitFieldList(declaration.parameters)
		}
		if declaration.alias {
			spec.Assign = token.Pos(1)
		}
		declarations = append(declarations, &ast.GenDecl{
			Tok: token.TYPE, Specs: []ast.Spec{spec},
		})
	}
	for _, global := range source.globals {
		declaration, err := emitter.emitGlobal(global)
		if err != nil {
			return nil, err
		}
		declarations = append(declarations, declaration)
	}
	for _, function := range source.functions {
		emitter.variables = cloneTypes(emitter.globals)
		emitter.narrowed = make(map[string]bool)
		emitter.resultTypes = nil
		params := &ast.FieldList{}
		for _, field := range function.params {
			if field.name != "" {
				emitter.variables[field.name] = field.typ
			}
			params.List = append(params.List, emitter.emitField(field))
		}
		var typeParameters *ast.FieldList
		if len(function.typeParams) > 0 {
			typeParameters = emitter.emitFieldList(function.typeParams)
		}
		var receiver *ast.FieldList
		if function.receiver != nil {
			receiver = &ast.FieldList{List: []*ast.Field{emitter.emitField(*function.receiver)}}
			emitter.variables[function.receiver.name] = function.receiver.typ
		}
		var results *ast.FieldList
		if len(function.results) > 0 {
			results = &ast.FieldList{}
		}
		for _, field := range function.results {
			emitter.resultTypes = append(emitter.resultTypes, field.typ)
			if field.name != "" {
				emitter.variables[field.name] = field.typ
			}
			results.List = append(results.List, emitter.emitField(field))
		}
		body := &ast.BlockStmt{}
		for _, statement := range function.body {
			generated, err := emitter.emitStatement(statement)
			if err != nil {
				return nil, err
			}
			body.List = append(body.List, generated)
		}
		declarations = append(declarations, &ast.FuncDecl{
			Name: ast.NewIdent(function.name),
			Recv: receiver,
			Type: &ast.FuncType{TypeParams: typeParameters, Params: params, Results: results},
			Body: body,
		})
	}

	imports := append([]importDeclaration(nil), source.imports...)
	if emitter.needsRuntime {
		imports = append(imports, importDeclaration{name: "maort", path: runtimeImport})
	}
	if len(imports) > 0 {
		specs := make([]ast.Spec, 0, len(imports))
		seen := make(map[string]bool)
		for _, declaration := range imports {
			if seen[declaration.path] {
				continue
			}
			seen[declaration.path] = true
			spec := &ast.ImportSpec{
				Path: &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(declaration.path)},
			}
			if declaration.path == runtimeImport {
				spec.Name = ast.NewIdent("maort")
			} else if declaration.name != "" {
				spec.Name = ast.NewIdent(declaration.name)
			}
			specs = append(specs, spec)
		}
		declarations = append([]ast.Decl{&ast.GenDecl{Tok: token.IMPORT, Specs: specs}}, declarations...)
	}
	file.Decls = declarations

	var output bytes.Buffer
	fileSet := token.NewFileSet()
	if err := format.Node(&output, fileSet, file); err != nil {
		return nil, fmt.Errorf("%s: format generated Go: %w", emitter.filename, err)
	}
	return output.Bytes(), nil
}

func (emitter *emitter) emitFieldList(fields []field) *ast.FieldList {
	result := &ast.FieldList{}
	for _, field := range fields {
		result.List = append(result.List, emitter.emitField(field))
	}
	return result
}

func (emitter *emitter) emitGlobal(source statement) (ast.Decl, error) {
	generated, err := emitter.emitStatement(source)
	if err != nil {
		return nil, err
	}
	declaration, ok := generated.(*ast.DeclStmt)
	if !ok {
		return nil, fmt.Errorf("%s: invalid package-level statement", emitter.filename)
	}
	generatedDeclaration, ok := declaration.Decl.(ast.Decl)
	if !ok {
		return nil, fmt.Errorf("%s: invalid package-level declaration", emitter.filename)
	}
	switch statement := source.(type) {
	case variableDeclaration:
		emitter.globals[statement.name] = statement.typ
	case constantDeclaration:
		emitter.globals[statement.name] = emitter.variables[statement.name]
	case declarationGroup:
		for _, member := range statement.declarations {
			switch declaration := member.(type) {
			case variableDeclaration:
				emitter.globals[declaration.name] = declaration.typ
			case constantDeclaration:
				emitter.globals[declaration.name] = emitter.variables[declaration.name]
			}
		}
	}
	return generatedDeclaration, nil
}

func cloneTypes(source map[string]maoType) map[string]maoType {
	result := make(map[string]maoType, len(source))
	for name, typ := range source {
		result[name] = typ
	}
	return result
}

func (emitter *emitter) multipleResultTypes(values []expression, count int) []maoType {
	if len(values) == count {
		result := make([]maoType, len(values))
		for index, value := range values {
			result[index] = emitter.inferExpression(value)
		}
		return result
	}
	if len(values) != 1 {
		return nil
	}
	switch value := values[0].(type) {
	case callExpression:
		if name, ok := value.function.(identifier); ok {
			if signature, exists := emitter.functions[name.name]; exists {
				result := make([]maoType, len(signature.results))
				for index, field := range signature.results {
					result[index] = field.typ
				}
				return result
			}
		}
		if selector, ok := value.function.(selectorExpression); ok {
			if signature, exists := emitter.importedFunction(selector); exists {
				result := make([]maoType, signature.Results().Len())
				for index := range result {
					result[index] = maoTypeFromGo(signature.Results().At(index).Type())
				}
				return result
			}
		}
	case unaryExpression:
		if value.operator == "<-" && count == 2 {
			return []maoType{
				unaryResultType(value.operator, emitter.inferExpression(value.value)),
				basicType("bool"),
			}
		}
	case indexExpression:
		receiver := emitter.inferExpression(value.receiver)
		if receiver.kind == "map" && count == 2 {
			return []maoType{*receiver.value, basicType("bool")}
		}
	case typeAssertionExpression:
		if count == 2 {
			return []maoType{value.typ, basicType("bool")}
		}
	}
	return nil
}

func (emitter *emitter) emitField(source field) *ast.Field {
	result := &ast.Field{Type: emitter.emitType(source.typ)}
	if source.name != "" {
		result.Names = []*ast.Ident{ast.NewIdent(source.name)}
	}
	if source.tag != "" {
		result.Tag = &ast.BasicLit{Kind: token.STRING, Value: source.tag}
	}
	return result
}

func (emitter *emitter) emitStatement(source statement) (ast.Stmt, error) {
	switch statement := source.(type) {
	case shortDeclaration:
		value, typ, err := emitter.emitExpression(statement.value, nil)
		if err != nil {
			return nil, err
		}
		if typ.kind == "null" {
			return nil, fmt.Errorf("%s: null requires an explicit nullable target type", emitter.filename)
		}
		emitter.variables[statement.name] = typ
		delete(emitter.narrowed, statement.name)
		return &ast.AssignStmt{
			Lhs: []ast.Expr{ast.NewIdent(statement.name)},
			Tok: token.DEFINE,
			Rhs: []ast.Expr{value},
		}, nil
	case multiAssignment:
		result := &ast.AssignStmt{Tok: token.ASSIGN}
		if statement.op == ":=" {
			result.Tok = token.DEFINE
		}
		leftTypes := make([]maoType, 0, len(statement.left))
		for _, value := range statement.left {
			generated, _, err := emitter.emitExpression(value, nil)
			if err != nil {
				return nil, err
			}
			result.Lhs = append(result.Lhs, generated)
			leftTypes = append(leftTypes, emitter.inferExpression(value))
		}
		rightTypes := emitter.multipleResultTypes(statement.right, len(statement.left))
		for index, value := range statement.right {
			var generated ast.Expr
			var actual maoType
			var err error
			if result.Tok == token.ASSIGN && len(statement.right) == len(statement.left) &&
				index < len(leftTypes) && leftTypes[index].kind != "unknown" {
				generated, actual, err = emitter.emitValue(value, leftTypes[index])
			} else {
				generated, actual, err = emitter.emitExpression(value, nil)
			}
			if err != nil {
				return nil, err
			}
			result.Rhs = append(result.Rhs, generated)
			if len(statement.right) == len(statement.left) {
				if len(rightTypes) <= index {
					rightTypes = append(rightTypes, actual)
				} else if rightTypes[index].kind == "unknown" {
					rightTypes[index] = actual
				}
			}
		}
		if result.Tok == token.DEFINE {
			for index, value := range statement.left {
				name, ok := value.(identifier)
				if !ok || name.name == "_" {
					continue
				}
				typ := unknownType()
				if index < len(rightTypes) {
					typ = rightTypes[index]
				}
				emitter.variables[name.name] = typ
				delete(emitter.narrowed, name.name)
			}
		}
		return result, nil
	case variableDeclaration:
		valueType := statement.typ
		var values []ast.Expr
		if statement.value != nil {
			value, _, err := emitter.emitValue(statement.value, valueType)
			if err != nil {
				return nil, fmt.Errorf("initialize %s: %w", statement.name, err)
			}
			values = []ast.Expr{value}
		} else if valueType.kind == "table" {
			emitter.needsRuntime = true
			values = []ast.Expr{&ast.CallExpr{Fun: &ast.IndexListExpr{
				X: &ast.SelectorExpr{X: ast.NewIdent("maort"), Sel: ast.NewIdent("NewTable")},
				Indices: []ast.Expr{
					emitter.emitType(*valueType.key),
					emitter.emitType(*valueType.value),
				},
			}}}
		}
		emitter.variables[statement.name] = valueType
		delete(emitter.narrowed, statement.name)
		spec := &ast.ValueSpec{
			Names:  []*ast.Ident{ast.NewIdent(statement.name)},
			Type:   emitter.emitType(valueType),
			Values: values,
		}
		return &ast.DeclStmt{Decl: &ast.GenDecl{Tok: token.VAR, Specs: []ast.Spec{spec}}}, nil
	case constantDeclaration:
		if !constantExpression(statement.value) {
			return nil, fmt.Errorf("%s: const initializer is not a constant expression", emitter.filename)
		}
		value, typ, err := emitter.emitExpression(statement.value, nil)
		if err != nil {
			return nil, err
		}
		if typ.kind == "null" {
			return nil, fmt.Errorf("%s: null is not a valid constant value", emitter.filename)
		}
		emitter.variables[statement.name] = typ
		delete(emitter.narrowed, statement.name)
		spec := &ast.ValueSpec{
			Names:  []*ast.Ident{ast.NewIdent(statement.name)},
			Values: []ast.Expr{value},
		}
		return &ast.DeclStmt{Decl: &ast.GenDecl{Tok: token.CONST, Specs: []ast.Spec{spec}}}, nil
	case declarationGroup:
		tokenKind := token.VAR
		if statement.kind == "const" {
			tokenKind = token.CONST
		}
		group := &ast.GenDecl{Tok: tokenKind, Lparen: token.Pos(1)}
		for _, declaration := range statement.declarations {
			generated, err := emitter.emitStatement(declaration)
			if err != nil {
				return nil, err
			}
			statement, ok := generated.(*ast.DeclStmt)
			if !ok {
				return nil, fmt.Errorf("%s: invalid grouped declaration", emitter.filename)
			}
			generatedGroup := statement.Decl.(*ast.GenDecl)
			group.Specs = append(group.Specs, generatedGroup.Specs...)
		}
		return &ast.DeclStmt{Decl: group}, nil
	case assignment:
		if index, ok := statement.left.(indexExpression); ok {
			receiverType := emitter.inferExpression(index.receiver)
			if receiverType.kind == "table" {
				if statement.op != "=" {
					return nil, fmt.Errorf(
						"%s: compound assignment through a table index is not supported; read, update, and assign explicitly",
						emitter.filename,
					)
				}
				receiver, _, err := emitter.emitExpression(index.receiver, nil)
				if err != nil {
					return nil, err
				}
				key, _, err := emitter.emitValue(index.key, *receiverType.key)
				if err != nil {
					return nil, err
				}
				value, _, err := emitter.emitValue(statement.right, *receiverType.value)
				if err != nil {
					return nil, err
				}
				return &ast.ExprStmt{X: &ast.CallExpr{
					Fun:  &ast.SelectorExpr{X: receiver, Sel: ast.NewIdent("Set")},
					Args: []ast.Expr{key, value},
				}}, nil
			}
		}
		expectedType := emitter.inferExpression(statement.left)
		var left ast.Expr
		var err error
		if name, ok := statement.left.(identifier); ok {
			left = ast.NewIdent(name.name)
			delete(emitter.narrowed, name.name)
		} else {
			left, _, err = emitter.emitExpression(statement.left, nil)
			if err != nil {
				return nil, err
			}
		}
		right, _, err := emitter.emitValue(statement.right, expectedType)
		if err != nil {
			return nil, err
		}
		return &ast.AssignStmt{
			Lhs: []ast.Expr{left}, Tok: operatorToken(statement.op), Rhs: []ast.Expr{right},
		}, nil
	case expressionStatement:
		value, _, err := emitter.emitExpression(statement.value, nil)
		return &ast.ExprStmt{X: value}, err
	case returnStatement:
		result := &ast.ReturnStmt{}
		for index, value := range statement.values {
			var generated ast.Expr
			var err error
			if index < len(emitter.resultTypes) {
				generated, _, err = emitter.emitValue(value, emitter.resultTypes[index])
			} else {
				generated, _, err = emitter.emitExpression(value, nil)
			}
			if err != nil {
				return nil, err
			}
			result.Results = append(result.Results, generated)
		}
		return result, nil
	case blockStatement:
		return emitter.emitBlock(statement)
	case ifStatement:
		previousVariables := emitter.variables
		previousNarrowed := emitter.narrowed
		emitter.variables = cloneTypes(previousVariables)
		emitter.narrowed = cloneBools(previousNarrowed)
		result := &ast.IfStmt{}
		var err error
		if statement.initial != nil {
			result.Init, err = emitter.emitStatement(statement.initial)
			if err != nil {
				emitter.variables, emitter.narrowed = previousVariables, previousNarrowed
				return nil, err
			}
		}
		condition, _, err := emitter.emitExpression(statement.condition, nil)
		if err != nil {
			emitter.variables, emitter.narrowed = previousVariables, previousNarrowed
			return nil, err
		}
		if name, ok := emitter.nonNullCondition(statement.condition); ok {
			emitter.narrowed[name] = true
		}
		body, err := emitter.emitBlock(statement.body)
		if err != nil {
			emitter.variables, emitter.narrowed = previousVariables, previousNarrowed
			return nil, err
		}
		result.Cond, result.Body = condition, body
		if statement.otherwise != nil {
			result.Else, err = emitter.emitStatement(statement.otherwise)
			if err != nil {
				emitter.variables, emitter.narrowed = previousVariables, previousNarrowed
				return nil, err
			}
		}
		emitter.variables, emitter.narrowed = previousVariables, previousNarrowed
		return result, nil
	case rangeStatement:
		previousVariables := emitter.variables
		emitter.variables = cloneTypes(previousVariables)
		iterable, typ, err := emitter.emitExpression(statement.iterable, nil)
		if err != nil {
			emitter.variables = previousVariables
			return nil, err
		}
		if typ.kind == "table" {
			iterable = &ast.SelectorExpr{X: iterable, Sel: ast.NewIdent("Range")}
			emitter.variables[statement.key] = *typ.key
			if statement.value != "" {
				emitter.variables[statement.value] = *typ.value
			}
		}
		body, err := emitter.emitBlock(statement.body)
		emitter.variables = previousVariables
		if err != nil {
			return nil, err
		}
		rangeStatement := &ast.RangeStmt{
			Key:  ast.NewIdent(statement.key),
			Tok:  token.DEFINE,
			X:    iterable,
			Body: body,
		}
		if statement.value != "" {
			rangeStatement.Value = ast.NewIdent(statement.value)
		}
		return rangeStatement, nil
	case forStatement:
		previousVariables := emitter.variables
		previousNarrowed := emitter.narrowed
		emitter.variables = cloneTypes(previousVariables)
		emitter.narrowed = cloneBools(previousNarrowed)
		result := &ast.ForStmt{}
		var err error
		if statement.initial != nil {
			result.Init, err = emitter.emitStatement(statement.initial)
			if err != nil {
				emitter.variables, emitter.narrowed = previousVariables, previousNarrowed
				return nil, err
			}
		}
		if statement.condition != nil {
			result.Cond, _, err = emitter.emitExpression(statement.condition, nil)
		}
		if err == nil && statement.post != nil {
			result.Post, err = emitter.emitStatement(statement.post)
		}
		if err == nil {
			result.Body, err = emitter.emitBlock(statement.body)
		}
		emitter.variables, emitter.narrowed = previousVariables, previousNarrowed
		return result, err
	case branchStatement:
		result := &ast.BranchStmt{Tok: token.Lookup(statement.kind)}
		if statement.label != "" {
			result.Label = ast.NewIdent(statement.label)
		}
		return result, nil
	case labeledStatement:
		body, err := emitter.emitStatement(statement.body)
		if err != nil {
			return nil, err
		}
		return &ast.LabeledStmt{Label: ast.NewIdent(statement.label), Stmt: body}, nil
	case incrementStatement:
		value, _, err := emitter.emitExpression(statement.value, nil)
		if err != nil {
			return nil, err
		}
		return &ast.IncDecStmt{X: value, Tok: operatorToken(statement.op)}, nil
	case actionStatement:
		value, _, err := emitter.emitExpression(statement.value, nil)
		if err != nil {
			return nil, err
		}
		call, ok := value.(*ast.CallExpr)
		if !ok {
			return nil, fmt.Errorf("%s: %s requires a function call", emitter.filename, statement.kind)
		}
		if statement.kind == "defer" {
			return &ast.DeferStmt{Call: call}, nil
		}
		return &ast.GoStmt{Call: call}, nil
	case sendStatement:
		channel, channelType, err := emitter.emitExpression(statement.channel, nil)
		if err != nil {
			return nil, err
		}
		var value ast.Expr
		if channelType.kind == "channel" {
			value, _, err = emitter.emitValue(statement.value, *channelType.element)
		} else {
			value, _, err = emitter.emitExpression(statement.value, nil)
		}
		if err != nil {
			return nil, fmt.Errorf("send on %s: %w", expressionName(statement.channel), err)
		}
		return &ast.SendStmt{Chan: channel, Value: value}, nil
	case switchStatement:
		previousVariables := emitter.variables
		previousNarrowed := emitter.narrowed
		emitter.variables = cloneTypes(previousVariables)
		emitter.narrowed = cloneBools(previousNarrowed)
		defer func() {
			emitter.variables = previousVariables
			emitter.narrowed = previousNarrowed
		}()
		result := &ast.SwitchStmt{Body: &ast.BlockStmt{}}
		var err error
		if statement.initial != nil {
			result.Init, err = emitter.emitStatement(statement.initial)
			if err != nil {
				return nil, err
			}
		}
		if statement.value != nil {
			result.Tag, _, err = emitter.emitExpression(statement.value, nil)
			if err != nil {
				return nil, err
			}
		}
		for _, clause := range statement.cases {
			clauseVariables := emitter.variables
			clauseNarrowed := emitter.narrowed
			emitter.variables = cloneTypes(clauseVariables)
			emitter.narrowed = cloneBools(clauseNarrowed)
			generatedClause := &ast.CaseClause{}
			for _, value := range clause.values {
				generated, _, err := emitter.emitExpression(value, nil)
				if err != nil {
					emitter.variables = clauseVariables
					emitter.narrowed = clauseNarrowed
					return nil, err
				}
				generatedClause.List = append(generatedClause.List, generated)
			}
			for _, bodyStatement := range clause.body {
				generated, err := emitter.emitStatement(bodyStatement)
				if err != nil {
					emitter.variables = clauseVariables
					emitter.narrowed = clauseNarrowed
					return nil, err
				}
				generatedClause.Body = append(generatedClause.Body, generated)
			}
			emitter.variables = clauseVariables
			emitter.narrowed = clauseNarrowed
			result.Body.List = append(result.Body.List, generatedClause)
		}
		return result, nil
	case selectStatement:
		result := &ast.SelectStmt{Body: &ast.BlockStmt{}}
		for _, clause := range statement.cases {
			clauseVariables := emitter.variables
			clauseNarrowed := emitter.narrowed
			emitter.variables = cloneTypes(clauseVariables)
			emitter.narrowed = cloneBools(clauseNarrowed)
			generatedClause := &ast.CommClause{}
			if clause.communication != nil {
				communication, err := emitter.emitStatement(clause.communication)
				if err != nil {
					emitter.variables = clauseVariables
					emitter.narrowed = clauseNarrowed
					return nil, err
				}
				generatedClause.Comm = communication
			}
			for _, bodyStatement := range clause.body {
				generated, err := emitter.emitStatement(bodyStatement)
				if err != nil {
					emitter.variables = clauseVariables
					emitter.narrowed = clauseNarrowed
					return nil, err
				}
				generatedClause.Body = append(generatedClause.Body, generated)
			}
			emitter.variables = clauseVariables
			emitter.narrowed = clauseNarrowed
			result.Body.List = append(result.Body.List, generatedClause)
		}
		return result, nil
	default:
		return nil, fmt.Errorf("unsupported statement %T", source)
	}
}

func constantExpression(source expression) bool {
	switch expression := source.(type) {
	case identifier, basicLiteral:
		return true
	case unaryExpression:
		return constantExpression(expression.value)
	case binaryExpression:
		return constantExpression(expression.left) && constantExpression(expression.right)
	case callExpression:
		if len(expression.arguments) != 1 || !constantExpression(expression.arguments[0]) {
			return false
		}
		if name, ok := expression.function.(identifier); ok {
			return isBasicType(name.name)
		}
		_, ok := expression.function.(typeExpression)
		return ok
	default:
		return false
	}
}

func (emitter *emitter) emitBlock(source blockStatement) (*ast.BlockStmt, error) {
	previousVariables := emitter.variables
	previousNarrowed := emitter.narrowed
	emitter.variables = cloneTypes(previousVariables)
	emitter.narrowed = cloneBools(previousNarrowed)
	defer func() {
		emitter.variables = previousVariables
		emitter.narrowed = previousNarrowed
	}()
	result := &ast.BlockStmt{}
	for _, statement := range source.body {
		generated, err := emitter.emitStatement(statement)
		if err != nil {
			return nil, err
		}
		result.List = append(result.List, generated)
	}
	return result, nil
}

func cloneBools(source map[string]bool) map[string]bool {
	result := make(map[string]bool, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func (emitter *emitter) nonNullCondition(source expression) (string, bool) {
	binary, ok := source.(binaryExpression)
	if !ok || binary.operator != "!=" {
		return "", false
	}
	if name, ok := binary.left.(identifier); ok {
		if _, null := binary.right.(nullLiteral); null && emitter.variables[name.name].kind == "optional" {
			return name.name, true
		}
	}
	if name, ok := binary.right.(identifier); ok {
		if _, null := binary.left.(nullLiteral); null && emitter.variables[name.name].kind == "optional" {
			return name.name, true
		}
	}
	return "", false
}

func (emitter *emitter) emitValue(source expression, expected maoType) (ast.Expr, maoType, error) {
	value, actual, err := emitter.emitExpression(source, &expected)
	if err != nil {
		return nil, maoType{}, err
	}
	resolvedExpected := emitter.resolveAlias(expected)
	resolvedActual := emitter.resolveAlias(actual)
	if resolvedExpected.kind == "optional" {
		emitter.needsRuntime = true
		if resolvedActual.kind == "optional" {
			return value, expected, nil
		}
		if resolvedActual.kind == "null" {
			return &ast.CallExpr{Fun: &ast.IndexExpr{
				X:     &ast.SelectorExpr{X: ast.NewIdent("maort"), Sel: ast.NewIdent("Null")},
				Index: emitter.emitType(*resolvedExpected.element),
			}}, expected, nil
		}
		return &ast.CallExpr{
			Fun: &ast.IndexExpr{
				X:     &ast.SelectorExpr{X: ast.NewIdent("maort"), Sel: ast.NewIdent("Some")},
				Index: emitter.emitType(*resolvedExpected.element),
			},
			Args: []ast.Expr{value},
		}, expected, nil
	}
	if resolvedActual.kind == "table" {
		switch resolvedExpected.kind {
		case "slice":
			if resolvedActual.value.kind == "optional" {
				return nil, maoType{}, fmt.Errorf(
					"%s: converting table values of type %s to %s requires values(defaultValue)",
					emitter.filename, typeName(*resolvedActual.value), typeName(resolvedExpected),
				)
			}
			values, err := emitter.emitConvertedValues(value, resolvedActual, *resolvedExpected.element)
			if err != nil {
				return nil, maoType{}, err
			}
			return values, expected, nil
		case "map":
			if resolvedActual.value.kind == "optional" {
				return nil, maoType{}, fmt.Errorf(
					"%s: converting nullable table to %s requires map(tableValue, defaultValue)",
					emitter.filename, typeName(resolvedExpected),
				)
			}
			if !sameType(*resolvedActual.key, *resolvedExpected.key) {
				return nil, maoType{}, fmt.Errorf(
					"%s: cannot convert table key %s to map key %s",
					emitter.filename, typeName(*resolvedActual.key), typeName(*resolvedExpected.key),
				)
			}
			emitter.needsRuntime = true
			if sameType(*resolvedActual.value, *resolvedExpected.value) {
				return &ast.CallExpr{
					Fun:  &ast.SelectorExpr{X: ast.NewIdent("maort"), Sel: ast.NewIdent("ToMap")},
					Args: []ast.Expr{value},
				}, expected, nil
			}
			if !canWiden(*resolvedActual.value, *resolvedExpected.value) {
				return nil, maoType{}, fmt.Errorf(
					"%s: cannot convert table value %s to map value %s",
					emitter.filename, typeName(*resolvedActual.value), typeName(*resolvedExpected.value),
				)
			}
			return &ast.CallExpr{
				Fun: &ast.SelectorExpr{
					X: ast.NewIdent("maort"), Sel: ast.NewIdent("ConvertMap"),
				},
				Args: []ast.Expr{
					value, emitter.emitConverter(*resolvedActual.value, *resolvedExpected.value),
				},
			}, expected, nil
		case "array":
			if resolvedActual.value.kind == "optional" {
				return nil, maoType{}, fmt.Errorf(
					"%s: converting nullable table to %s requires values(defaultValue)",
					emitter.filename, typeName(resolvedExpected),
				)
			}
			values, err := emitter.emitConvertedValues(value, resolvedActual, *resolvedExpected.element)
			if err != nil {
				return nil, maoType{}, err
			}
			return emitter.emitTableToArray(values, expected), expected, nil
		}
	}
	if resolvedActual.kind == "null" && resolvedExpected.kind != "unknown" &&
		!(resolvedExpected.kind == "basic" && resolvedExpected.name == "any") {
		if isGoNilable(resolvedExpected) {
			return value, expected, nil
		}
		return nil, maoType{}, fmt.Errorf("%s: null cannot be assigned to %s", emitter.filename, typeName(expected))
	}
	if resolvedExpected.kind == "unknown" || resolvedActual.kind == "unknown" ||
		sameType(resolvedActual, resolvedExpected) ||
		(resolvedExpected.kind == "basic" && resolvedExpected.name == "any") {
		return value, actual, nil
	}
	if literalCanUseType(source, resolvedExpected) {
		return value, expected, nil
	}
	if canWiden(resolvedActual, resolvedExpected) {
		return &ast.CallExpr{Fun: emitter.emitType(expected), Args: []ast.Expr{value}}, expected, nil
	}
	return nil, maoType{}, fmt.Errorf(
		"%s: cannot use %s (%s) as %s without an explicit conversion",
		emitter.filename, expressionName(source), typeName(actual), typeName(expected),
	)
}

func isGoNilable(typ maoType) bool {
	switch typ.kind {
	case "pointer", "slice", "map", "channel", "function", "interface":
		return true
	default:
		return false
	}
}

func (emitter *emitter) resolveAlias(source maoType) maoType {
	seen := make(map[string]bool)
	for source.kind == "named" && emitter.aliases[source.name].kind != "" && !seen[source.name] {
		seen[source.name] = true
		source = emitter.aliases[source.name]
	}
	return source
}

func expressionName(source expression) string {
	switch expression := source.(type) {
	case identifier:
		return expression.name
	case callExpression:
		if selector, ok := expression.function.(selectorExpression); ok {
			return selector.name + " call result"
		}
		if name, ok := expression.function.(identifier); ok {
			return name.name + " call result"
		}
		return "call result"
	case selectorExpression:
		return expression.name
	case basicLiteral:
		return expression.value
	default:
		return "expression"
	}
}

func (emitter *emitter) emitConvertedValues(
	tableValue ast.Expr,
	tableType maoType,
	target maoType,
) (ast.Expr, error) {
	if sameType(*tableType.value, target) {
		return &ast.CallExpr{
			Fun: &ast.SelectorExpr{X: tableValue, Sel: ast.NewIdent("Values")},
		}, nil
	}
	if !canWiden(*tableType.value, target) {
		return nil, fmt.Errorf(
			"%s: cannot convert table value %s to %s",
			emitter.filename, typeName(*tableType.value), typeName(target),
		)
	}
	emitter.needsRuntime = true
	return &ast.CallExpr{
		Fun: &ast.SelectorExpr{
			X: ast.NewIdent("maort"), Sel: ast.NewIdent("ConvertValues"),
		},
		Args: []ast.Expr{tableValue, emitter.emitConverter(*tableType.value, target)},
	}, nil
}

func (emitter *emitter) emitConverter(source, target maoType) ast.Expr {
	return &ast.FuncLit{
		Type: &ast.FuncType{
			Params: &ast.FieldList{List: []*ast.Field{{
				Names: []*ast.Ident{ast.NewIdent("value")}, Type: emitter.emitType(source),
			}}},
			Results: &ast.FieldList{List: []*ast.Field{{Type: emitter.emitType(target)}}},
		},
		Body: &ast.BlockStmt{List: []ast.Stmt{&ast.ReturnStmt{
			Results: []ast.Expr{&ast.CallExpr{
				Fun: emitter.emitType(target), Args: []ast.Expr{ast.NewIdent("value")},
			}},
		}}},
	}
}

func (emitter *emitter) emitTableToArray(values ast.Expr, expected maoType) ast.Expr {
	arrayType := emitter.emitType(expected)
	slice := sliceType(*expected.element)
	body := &ast.BlockStmt{List: []ast.Stmt{
		&ast.IfStmt{
			Cond: &ast.BinaryExpr{
				X: &ast.CallExpr{
					Fun: ast.NewIdent("len"), Args: []ast.Expr{ast.NewIdent("values")},
				},
				Op: token.NEQ,
				Y:  &ast.BasicLit{Kind: token.INT, Value: strconv.Itoa(expected.length)},
			},
			Body: &ast.BlockStmt{List: []ast.Stmt{&ast.ExprStmt{X: &ast.CallExpr{
				Fun: ast.NewIdent("panic"),
				Args: []ast.Expr{&ast.BasicLit{
					Kind: token.STRING, Value: strconv.Quote("mao table length does not match array"),
				}},
			}}}},
		},
		&ast.DeclStmt{Decl: &ast.GenDecl{Tok: token.VAR, Specs: []ast.Spec{
			&ast.ValueSpec{Names: []*ast.Ident{ast.NewIdent("result")}, Type: arrayType},
		}}},
		&ast.ExprStmt{X: &ast.CallExpr{
			Fun: ast.NewIdent("copy"),
			Args: []ast.Expr{
				&ast.SliceExpr{X: ast.NewIdent("result")},
				ast.NewIdent("values"),
			},
		}},
		&ast.ReturnStmt{Results: []ast.Expr{ast.NewIdent("result")}},
	}}
	return &ast.CallExpr{
		Fun: &ast.FuncLit{
			Type: &ast.FuncType{
				Params: &ast.FieldList{List: []*ast.Field{{
					Names: []*ast.Ident{ast.NewIdent("values")}, Type: emitter.emitType(slice),
				}}},
				Results: &ast.FieldList{List: []*ast.Field{{Type: arrayType}}},
			},
			Body: body,
		},
		Args: []ast.Expr{values},
	}
}

func literalCanUseType(source expression, expected maoType) bool {
	literal, ok := source.(basicLiteral)
	if !ok {
		return false
	}
	if expected.kind == "named" {
		return literal.kind == scanner.Int || literal.kind == scanner.Float ||
			literal.kind == scanner.Char || literal.kind == scanner.String
	}
	if expected.kind != "basic" {
		return false
	}
	if literal.kind == scanner.Int {
		return isInteger(expected.name) || expected.name == "float32" || expected.name == "float64"
	}
	return literal.kind == scanner.Float && (expected.name == "float32" || expected.name == "float64")
}

func canWiden(source, target maoType) bool {
	if source.kind != "basic" || target.kind != "basic" {
		return false
	}
	if source.name == "float32" && target.name == "float64" {
		return true
	}
	signed := map[string]int{"int8": 8, "int16": 16, "int32": 32, "rune": 32, "int64": 64}
	unsigned := map[string]int{"uint8": 8, "byte": 8, "uint16": 16, "uint32": 32, "uint64": 64}
	if source.name == "int" && target.name == "int64" {
		return true
	}
	if (source.name == "uint" || source.name == "uintptr") && target.name == "uint64" {
		return true
	}
	if sourceWidth, ok := signed[source.name]; ok {
		targetWidth, exists := signed[target.name]
		return exists && sourceWidth < targetWidth
	}
	if sourceWidth, ok := unsigned[source.name]; ok {
		targetWidth, exists := unsigned[target.name]
		return exists && sourceWidth < targetWidth
	}
	return false
}

func isInteger(name string) bool {
	switch name {
	case "byte", "int", "int8", "int16", "int32", "int64", "rune",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr":
		return true
	default:
		return false
	}
}

func (emitter *emitter) emitNullComparison(source binaryExpression) (ast.Expr, maoType, bool, error) {
	var optional expression
	if _, ok := source.left.(nullLiteral); ok {
		optional = source.right
	} else if _, ok := source.right.(nullLiteral); ok {
		optional = source.left
	} else {
		return nil, maoType{}, false, nil
	}
	if source.operator != "==" && source.operator != "!=" {
		return nil, maoType{}, true, fmt.Errorf("%s: null only supports == and !=", emitter.filename)
	}
	value, typ, err := emitter.emitExpression(optional, nil)
	if err != nil {
		return nil, maoType{}, true, err
	}
	if isGoNilable(typ) {
		operator := token.EQL
		if source.operator == "!=" {
			operator = token.NEQ
		}
		return &ast.BinaryExpr{X: value, Op: operator, Y: ast.NewIdent("nil")},
			basicType("bool"), true, nil
	}
	if typ.kind != "optional" {
		return nil, maoType{}, true, fmt.Errorf(
			"%s: cannot compare %s with null", emitter.filename, typeName(typ),
		)
	}
	check := ast.Expr(&ast.CallExpr{Fun: &ast.SelectorExpr{X: value, Sel: ast.NewIdent("IsNull")}})
	if source.operator == "!=" {
		check = &ast.UnaryExpr{Op: token.NOT, X: check}
	}
	return check, basicType("bool"), true, nil
}

func (emitter *emitter) emitExpression(source expression, expected *maoType) (ast.Expr, maoType, error) {
	switch expression := source.(type) {
	case identifier:
		if typ, exists := emitter.variables[expression.name]; exists {
			if emitter.narrowed[expression.name] && typ.kind == "optional" {
				return &ast.CallExpr{Fun: &ast.SelectorExpr{
					X: ast.NewIdent(expression.name), Sel: ast.NewIdent("Value"),
				}}, *typ.element, nil
			}
			return ast.NewIdent(expression.name), typ, nil
		}
		return ast.NewIdent(expression.name), unknownType(), nil
	case basicLiteral:
		switch expression.kind {
		case scanner.Int:
			return &ast.BasicLit{Kind: token.INT, Value: expression.value}, basicType("int"), nil
		case scanner.Float:
			return &ast.BasicLit{Kind: token.FLOAT, Value: expression.value}, basicType("float64"), nil
		case scanner.Char:
			return &ast.BasicLit{Kind: token.CHAR, Value: expression.value}, basicType("rune"), nil
		case scanner.String, scanner.RawString:
			return &ast.BasicLit{Kind: token.STRING, Value: expression.value}, basicType("string"), nil
		default:
			return ast.NewIdent(expression.value), basicType("bool"), nil
		}
	case nullLiteral:
		return ast.NewIdent("nil"), nullType(), nil
	case tableLiteral:
		return emitter.emitTable(expression, expected)
	case typeExpression:
		return emitter.emitType(expression.typ), expression.typ, nil
	case genericExpression:
		base, _, err := emitter.emitExpression(expression.base, nil)
		if err != nil {
			return nil, maoType{}, err
		}
		arguments := make([]ast.Expr, len(expression.arguments))
		for index, argument := range expression.arguments {
			arguments[index] = emitter.emitType(argument)
		}
		return &ast.IndexListExpr{X: base, Indices: arguments}, unknownType(), nil
	case typeAssertionExpression:
		receiver, _, err := emitter.emitExpression(expression.receiver, nil)
		if err != nil {
			return nil, maoType{}, err
		}
		return &ast.TypeAssertExpr{X: receiver, Type: emitter.emitType(expression.typ)}, expression.typ, nil
	case functionLiteral:
		previousVariables := emitter.variables
		previousResults := emitter.resultTypes
		emitter.variables = cloneTypes(previousVariables)
		emitter.resultTypes = nil
		params := emitter.emitFieldList(expression.params)
		var results *ast.FieldList
		if len(expression.results) > 0 {
			results = emitter.emitFieldList(expression.results)
		}
		for _, parameter := range expression.params {
			if parameter.name != "" {
				emitter.variables[parameter.name] = parameter.typ
			}
		}
		for _, result := range expression.results {
			emitter.resultTypes = append(emitter.resultTypes, result.typ)
		}
		body, err := emitter.emitBlock(expression.body)
		emitter.variables = previousVariables
		emitter.resultTypes = previousResults
		if err != nil {
			return nil, maoType{}, err
		}
		return &ast.FuncLit{
			Type: &ast.FuncType{Params: params, Results: results},
			Body: body,
		}, unknownType(), nil
	case compositeLiteral:
		typ, resultType, err := emitter.emitExpression(expression.typ, nil)
		if err != nil {
			return nil, maoType{}, err
		}
		result := &ast.CompositeLit{Type: typ}
		for _, item := range expression.items {
			value, _, err := emitter.emitExpression(item.value, nil)
			if err != nil {
				return nil, maoType{}, err
			}
			if item.key != nil {
				key, _, err := emitter.emitExpression(item.key, nil)
				if err != nil {
					return nil, maoType{}, err
				}
				value = &ast.KeyValueExpr{Key: key, Value: value}
			}
			result.Elts = append(result.Elts, value)
		}
		return result, resultType, nil
	case unaryExpression:
		value, valueType, err := emitter.emitExpression(expression.value, nil)
		if err != nil {
			return nil, maoType{}, err
		}
		resultType := unaryResultType(expression.operator, valueType)
		return &ast.UnaryExpr{Op: operatorToken(expression.operator), X: value}, resultType, nil
	case binaryExpression:
		if generated, typ, ok, err := emitter.emitNullComparison(expression); ok || err != nil {
			return generated, typ, err
		}
		left, leftType, err := emitter.emitExpression(expression.left, nil)
		if err != nil {
			return nil, maoType{}, err
		}
		right, _, err := emitter.emitExpression(expression.right, &leftType)
		if err != nil {
			return nil, maoType{}, err
		}
		resultType := leftType
		if binaryPrecedence(expression.operator) == 3 || expression.operator == "&&" || expression.operator == "||" {
			resultType = basicType("bool")
		}
		return &ast.BinaryExpr{
			X: left, Op: operatorToken(expression.operator), Y: right,
		}, resultType, nil
	case selectorExpression:
		receiver, receiverType, err := emitter.emitExpression(expression.receiver, nil)
		if err != nil {
			return nil, maoType{}, err
		}
		name := expression.name
		resultType := unknownType()
		if receiverType.kind == "table" {
			switch name {
			case "get":
				name = "Get"
				resultType = *receiverType.value
			case "has":
				name = "Has"
				resultType = basicType("bool")
			case "at":
				name = "At"
				resultType = entryType(*receiverType.key, *receiverType.value)
			case "clear":
				name = "Clear"
			case "keys":
				name = "Keys"
				resultType = sliceType(*receiverType.key)
			case "values":
				name = "Values"
				resultType = sliceType(*receiverType.value)
			case "Delete", "DeleteAt":
			default:
				resultType = unknownType()
			}
		} else if receiverType.kind == "entry" {
			switch name {
			case "key":
				name = "Key"
				resultType = *receiverType.key
			case "value":
				name = "Value"
				resultType = *receiverType.value
			}
		}
		return &ast.SelectorExpr{X: receiver, Sel: ast.NewIdent(name)}, resultType, nil
	case callExpression:
		if selector, ok := expression.function.(selectorExpression); ok {
			receiverType := emitter.inferExpression(selector.receiver)
			if receiverType.kind == "table" {
				if selector.name == "get" && len(expression.arguments) == 2 {
					receiver, _, err := emitter.emitExpression(selector.receiver, nil)
					if err != nil {
						return nil, maoType{}, err
					}
					key, _, err := emitter.emitValue(expression.arguments[0], *receiverType.key)
					if err != nil {
						return nil, maoType{}, err
					}
					fallback, _, err := emitter.emitValue(expression.arguments[1], *receiverType.value)
					if err != nil {
						return nil, maoType{}, err
					}
					return &ast.CallExpr{
						Fun: &ast.SelectorExpr{X: receiver, Sel: ast.NewIdent("GetLazy")},
						Args: []ast.Expr{
							key,
							&ast.FuncLit{
								Type: &ast.FuncType{
									Params:  &ast.FieldList{},
									Results: &ast.FieldList{List: []*ast.Field{{Type: emitter.emitType(*receiverType.value)}}},
								},
								Body: &ast.BlockStmt{List: []ast.Stmt{
									&ast.ReturnStmt{Results: []ast.Expr{fallback}},
								}},
							},
						},
					}, *receiverType.value, nil
				}
				if selector.name == "values" {
					return emitter.emitValuesCall(selector.receiver, receiverType, expression.arguments)
				}
				if selector.name == "has" || selector.name == "at" ||
					selector.name == "Delete" || selector.name == "DeleteAt" ||
					selector.name == "clear" || selector.name == "keys" {
					return emitter.emitTableMethodCall(
						selector.receiver, selector.name, receiverType, expression.arguments,
					)
				}
			}
		}
		if name, ok := expression.function.(identifier); ok && name.name == "table" {
			return emitter.emitTableConversion(expression.arguments)
		}
		if name, ok := expression.function.(identifier); ok && name.name == "map" {
			return emitter.emitMapConversion(expression.arguments, expected)
		}
		if name, ok := expression.function.(identifier); ok && name.name == "len" && len(expression.arguments) == 1 {
			argument, argumentType, err := emitter.emitExpression(expression.arguments[0], nil)
			if err != nil {
				return nil, maoType{}, err
			}
			if argumentType.kind == "table" {
				return &ast.CallExpr{
					Fun: &ast.SelectorExpr{X: argument, Sel: ast.NewIdent("Len")},
				}, basicType("int"), nil
			}
		}
		if name, ok := expression.function.(identifier); ok {
			if signature, exists := emitter.functions[name.name]; exists {
				variadic := len(signature.params) > 0 &&
					signature.params[len(signature.params)-1].typ.kind == "variadic"
				if (!variadic && len(expression.arguments) != len(signature.params)) ||
					(variadic && len(expression.arguments) < len(signature.params)-1) {
					return nil, maoType{}, fmt.Errorf(
						"%s: %s expects %d arguments, got %d",
						emitter.filename, name.name, len(signature.params), len(expression.arguments),
					)
				}
				call := &ast.CallExpr{Fun: ast.NewIdent(name.name)}
				if expression.ellipsis {
					call.Ellipsis = token.Pos(1)
				}
				for index, argument := range expression.arguments {
					parameterIndex := index
					if parameterIndex >= len(signature.params) {
						parameterIndex = len(signature.params) - 1
					}
					expectedType := signature.params[parameterIndex].typ
					if expectedType.kind == "variadic" {
						if expression.ellipsis {
							expectedType = sliceType(*expectedType.element)
						} else {
							expectedType = *expectedType.element
						}
					}
					var generated ast.Expr
					var err error
					if isTypeParameter(expectedType, signature.typeParams) {
						generated, _, err = emitter.emitExpression(argument, nil)
					} else {
						generated, _, err = emitter.emitValue(argument, expectedType)
					}
					if err != nil {
						return nil, maoType{}, err
					}
					call.Args = append(call.Args, generated)
				}
				if len(signature.results) == 1 {
					if isTypeParameter(signature.results[0].typ, signature.typeParams) {
						return call, unknownType(), nil
					}
					return call, signature.results[0].typ, nil
				}
				return call, unknownType(), nil
			}
		}
		if selector, ok := expression.function.(selectorExpression); ok {
			if signature, exists := emitter.importedFunction(selector); exists {
				return emitter.emitImportedCall(
					expression.function, expression.arguments, expression.ellipsis, signature,
				)
			}
		}
		function, functionType, err := emitter.emitExpression(expression.function, nil)
		if err != nil {
			return nil, maoType{}, err
		}
		call := &ast.CallExpr{Fun: function}
		if expression.ellipsis {
			call.Ellipsis = token.Pos(1)
		}
		for _, argument := range expression.arguments {
			generated, _, err := emitter.emitExpression(argument, nil)
			if err != nil {
				return nil, maoType{}, err
			}
			call.Args = append(call.Args, generated)
		}
		return call, functionType, nil
	case indexExpression:
		receiver, receiverType, err := emitter.emitExpression(expression.receiver, nil)
		if err != nil {
			return nil, maoType{}, err
		}
		var expectedKey *maoType
		if receiverType.kind == "table" {
			expectedKey = receiverType.key
		}
		var key ast.Expr
		if expectedKey != nil {
			key, _, err = emitter.emitValue(expression.key, *expectedKey)
		} else {
			key, _, err = emitter.emitExpression(expression.key, nil)
		}
		if err != nil {
			return nil, maoType{}, err
		}
		if receiverType.kind == "table" {
			emitter.needsRuntime = true
			resultType := optionalType(*receiverType.value)
			indexCall := &ast.CallExpr{
				Fun:  &ast.SelectorExpr{X: receiver, Sel: ast.NewIdent("Index")},
				Args: []ast.Expr{key},
			}
			if receiverType.value.kind == "optional" {
				return &ast.CallExpr{
					Fun: &ast.IndexExpr{
						X:     &ast.SelectorExpr{X: ast.NewIdent("maort"), Sel: ast.NewIdent("Flatten")},
						Index: emitter.emitType(*receiverType.value.element),
					},
					Args: []ast.Expr{indexCall},
				}, resultType, nil
			}
			return indexCall, resultType, nil
		}
		return &ast.IndexExpr{X: receiver, Index: key}, unknownType(), nil
	case sliceExpression:
		receiver, receiverType, err := emitter.emitExpression(expression.receiver, nil)
		if err != nil {
			return nil, maoType{}, err
		}
		generated := &ast.SliceExpr{X: receiver, Slice3: expression.maximum != nil}
		if expression.low != nil {
			generated.Low, _, err = emitter.emitExpression(expression.low, nil)
		}
		if err == nil && expression.high != nil {
			generated.High, _, err = emitter.emitExpression(expression.high, nil)
		}
		if err == nil && expression.maximum != nil {
			generated.Max, _, err = emitter.emitExpression(expression.maximum, nil)
		}
		return generated, receiverType, err
	default:
		return nil, maoType{}, fmt.Errorf("unsupported expression %T", source)
	}
}

func isTypeParameter(typ maoType, parameters []field) bool {
	if typ.kind != "named" {
		return false
	}
	for _, parameter := range parameters {
		if parameter.name == typ.name {
			return true
		}
	}
	return false
}

func operatorToken(operator string) token.Token {
	switch operator {
	case "+":
		return token.ADD
	case "-":
		return token.SUB
	case "*":
		return token.MUL
	case "/":
		return token.QUO
	case "%":
		return token.REM
	case "&":
		return token.AND
	case "|":
		return token.OR
	case "^":
		return token.XOR
	case "<<":
		return token.SHL
	case ">>":
		return token.SHR
	case "&^":
		return token.AND_NOT
	case "&&":
		return token.LAND
	case "||":
		return token.LOR
	case "<-":
		return token.ARROW
	case "++":
		return token.INC
	case "--":
		return token.DEC
	case "==":
		return token.EQL
	case "<":
		return token.LSS
	case ">":
		return token.GTR
	case "=":
		return token.ASSIGN
	case "+=":
		return token.ADD_ASSIGN
	case "-=":
		return token.SUB_ASSIGN
	case "*=":
		return token.MUL_ASSIGN
	case "/=":
		return token.QUO_ASSIGN
	case "%=":
		return token.REM_ASSIGN
	case "&=":
		return token.AND_ASSIGN
	case "|=":
		return token.OR_ASSIGN
	case "^=":
		return token.XOR_ASSIGN
	case "<<=":
		return token.SHL_ASSIGN
	case ">>=":
		return token.SHR_ASSIGN
	case "&^=":
		return token.AND_NOT_ASSIGN
	case "!":
		return token.NOT
	case "!=":
		return token.NEQ
	case "<=":
		return token.LEQ
	case ">=":
		return token.GEQ
	default:
		return token.ILLEGAL
	}
}

func (emitter *emitter) importedFunction(
	selector selectorExpression,
) (*types.Signature, bool) {
	packageName, ok := selector.receiver.(identifier)
	if !ok {
		return nil, false
	}
	imported := emitter.imported[packageName.name]
	if imported == nil {
		return nil, false
	}
	object := imported.Scope().Lookup(selector.name)
	function, ok := object.(*types.Func)
	if !ok {
		return nil, false
	}
	signature, ok := function.Type().(*types.Signature)
	return signature, ok
}

func (emitter *emitter) emitImportedCall(
	functionSource expression,
	arguments []expression,
	ellipsis bool,
	signature *types.Signature,
) (ast.Expr, maoType, error) {
	required := signature.Params().Len()
	if (!signature.Variadic() && len(arguments) != required) ||
		(signature.Variadic() && len(arguments) < required-1) {
		return nil, maoType{}, fmt.Errorf(
			"%s: imported function expects %d arguments, got %d",
			emitter.filename, required, len(arguments),
		)
	}
	function, _, err := emitter.emitExpression(functionSource, nil)
	if err != nil {
		return nil, maoType{}, err
	}
	call := &ast.CallExpr{Fun: function}
	if ellipsis {
		call.Ellipsis = token.Pos(1)
	}
	for index, argument := range arguments {
		parameterIndex := index
		if parameterIndex >= required {
			parameterIndex = required - 1
		}
		expected := maoTypeFromGo(signature.Params().At(parameterIndex).Type())
		if signature.Variadic() && parameterIndex == required-1 && !ellipsis {
			if expected.kind == "slice" {
				expected = *expected.element
			}
		}
		var generated ast.Expr
		if expected.kind == "unknown" {
			generated, _, err = emitter.emitExpression(argument, nil)
		} else {
			generated, _, err = emitter.emitValue(argument, expected)
		}
		if err != nil {
			return nil, maoType{}, err
		}
		call.Args = append(call.Args, generated)
	}
	result := unknownType()
	if signature.Results().Len() == 1 {
		result = maoTypeFromGo(signature.Results().At(0).Type())
	}
	return call, result, nil
}

func maoTypeFromGo(source types.Type) maoType {
	switch typ := source.(type) {
	case *types.Basic:
		switch typ.Kind() {
		case types.Bool:
			return basicType("bool")
		case types.Int:
			return basicType("int")
		case types.Int8:
			return basicType("int8")
		case types.Int16:
			return basicType("int16")
		case types.Int32:
			return basicType("int32")
		case types.Int64:
			return basicType("int64")
		case types.Uint:
			return basicType("uint")
		case types.Uint8:
			return basicType("uint8")
		case types.Uint16:
			return basicType("uint16")
		case types.Uint32:
			return basicType("uint32")
		case types.Uint64:
			return basicType("uint64")
		case types.Uintptr:
			return basicType("uintptr")
		case types.Float32:
			return basicType("float32")
		case types.Float64:
			return basicType("float64")
		case types.String:
			return basicType("string")
		default:
			return unknownType()
		}
	case *types.Slice:
		return sliceType(maoTypeFromGo(typ.Elem()))
	case *types.Array:
		element := maoTypeFromGo(typ.Elem())
		return maoType{kind: "array", element: &element, length: int(typ.Len())}
	case *types.Map:
		key, value := maoTypeFromGo(typ.Key()), maoTypeFromGo(typ.Elem())
		return maoType{kind: "map", key: &key, value: &value}
	case *types.Pointer:
		element := maoTypeFromGo(typ.Elem())
		return maoType{kind: "pointer", element: &element}
	case *types.Alias:
		return maoTypeFromGo(types.Unalias(typ))
	case *types.Named:
		name := typ.Obj().Name()
		if pkg := typ.Obj().Pkg(); pkg != nil {
			name = pkg.Name() + "." + name
		}
		return maoType{kind: "named", name: name}
	case *types.Interface, *types.Signature, *types.Chan, *types.TypeParam:
		return unknownType()
	default:
		return unknownType()
	}
}

func (emitter *emitter) emitTableMethodCall(
	receiverSource expression,
	name string,
	receiverType maoType,
	arguments []expression,
) (ast.Expr, maoType, error) {
	expectedCount := 0
	methodName := name
	resultType := unknownType()
	var argumentType *maoType
	switch name {
	case "has":
		expectedCount, methodName, resultType, argumentType = 1, "Has", basicType("bool"), receiverType.key
	case "at":
		indexType := basicType("int")
		expectedCount, methodName = 1, "At"
		resultType, argumentType = entryType(*receiverType.key, *receiverType.value), &indexType
	case "Delete":
		expectedCount, argumentType = 1, receiverType.key
	case "DeleteAt":
		indexType := basicType("int")
		expectedCount, argumentType = 1, &indexType
	case "clear":
		methodName = "Clear"
	case "keys":
		methodName, resultType = "Keys", sliceType(*receiverType.key)
	}
	if len(arguments) != expectedCount {
		return nil, maoType{}, fmt.Errorf(
			"%s: %s expects %d arguments, got %d",
			emitter.filename, name, expectedCount, len(arguments),
		)
	}
	receiver, _, err := emitter.emitExpression(receiverSource, nil)
	if err != nil {
		return nil, maoType{}, err
	}
	call := &ast.CallExpr{Fun: &ast.SelectorExpr{X: receiver, Sel: ast.NewIdent(methodName)}}
	if expectedCount == 1 {
		argument, _, err := emitter.emitValue(arguments[0], *argumentType)
		if err != nil {
			return nil, maoType{}, err
		}
		call.Args = []ast.Expr{argument}
	}
	return call, resultType, nil
}

func (emitter *emitter) emitTable(source tableLiteral, expected *maoType) (ast.Expr, maoType, error) {
	emitter.needsRuntime = true
	keyType, valueType := emitter.inferTable(source)
	if expected != nil && expected.kind == "table" {
		keyType = *expected.key
		valueType = *expected.value
	}
	if !staticallyComparable(keyType) {
		return nil, maoType{}, fmt.Errorf(
			"%s: table key type %s is not comparable", emitter.filename, typeName(keyType),
		)
	}
	resultType := tableType(keyType, valueType)
	goTableType := emitter.emitType(resultType)
	tableName := ast.NewIdent("tableValue")

	body := &ast.BlockStmt{}
	constructor := &ast.CallExpr{Fun: &ast.IndexListExpr{
		X:       &ast.SelectorExpr{X: ast.NewIdent("maort"), Sel: ast.NewIdent("NewTable")},
		Indices: []ast.Expr{emitter.emitType(keyType), emitter.emitType(valueType)},
	}}
	body.List = append(body.List, &ast.AssignStmt{
		Lhs: []ast.Expr{tableName},
		Tok: token.DEFINE,
		Rhs: []ast.Expr{constructor},
	})

	implicitKey := 0
	for _, item := range source.items {
		var keySource expression
		if item.explicit {
			keySource = item.key
		} else {
			keySource = basicLiteral{kind: scanner.Int, value: strconv.Itoa(implicitKey)}
			implicitKey++
		}
		key, _, err := emitter.emitValue(keySource, keyType)
		if err != nil {
			return nil, maoType{}, err
		}
		value, _, err := emitter.emitValue(item.value, valueType)
		if err != nil {
			return nil, maoType{}, err
		}
		body.List = append(body.List, &ast.ExprStmt{X: &ast.CallExpr{
			Fun:  &ast.SelectorExpr{X: tableName, Sel: ast.NewIdent("Set")},
			Args: []ast.Expr{key, value},
		}})
	}
	body.List = append(body.List, &ast.ReturnStmt{Results: []ast.Expr{tableName}})

	function := &ast.FuncLit{
		Type: &ast.FuncType{
			Params:  &ast.FieldList{},
			Results: &ast.FieldList{List: []*ast.Field{{Type: goTableType}}},
		},
		Body: body,
	}
	return &ast.CallExpr{Fun: function}, resultType, nil
}

func staticallyComparable(typ maoType) bool {
	switch typ.kind {
	case "slice", "map", "function":
		return false
	case "optional", "array":
		return staticallyComparable(*typ.element)
	case "struct":
		for _, field := range typ.fields {
			if !staticallyComparable(field.typ) {
				return false
			}
		}
		return true
	default:
		return true
	}
}

func typeName(source maoType) string {
	switch source.kind {
	case "basic":
		if source.name == "float64" {
			return "float"
		}
		return source.name
	case "named":
		return source.name
	case "generic":
		parts := make([]string, len(source.args))
		for index, argument := range source.args {
			parts[index] = typeName(argument)
		}
		return source.name + "<" + strings.Join(parts, ", ") + ">"
	case "optional":
		return typeName(*source.element) + "?"
	case "table":
		return "table<" + typeName(*source.key) + ", " + typeName(*source.value) + ">"
	case "slice":
		return typeName(*source.element) + "[]"
	case "pointer":
		return "*" + typeName(*source.element)
	case "channel":
		switch source.name {
		case "send":
			return "chan<- " + typeName(*source.element)
		case "receive":
			return "<-chan " + typeName(*source.element)
		default:
			return "chan " + typeName(*source.element)
		}
	case "function":
		return "func"
	case "map":
		return typeName(*source.key) + ":" + typeName(*source.value) + "[]"
	case "array":
		return fmt.Sprintf("%s[%d]", typeName(*source.element), source.length)
	default:
		return source.kind
	}
}

func (emitter *emitter) emitValuesCall(
	receiverSource expression,
	receiverType maoType,
	arguments []expression,
) (ast.Expr, maoType, error) {
	receiver, _, err := emitter.emitExpression(receiverSource, nil)
	if err != nil {
		return nil, maoType{}, err
	}
	valueType := *receiverType.value
	if valueType.kind != "optional" {
		if len(arguments) != 0 {
			return nil, maoType{}, fmt.Errorf("%s: values(defaultValue) requires nullable table values", emitter.filename)
		}
		return &ast.CallExpr{
			Fun: &ast.SelectorExpr{X: receiver, Sel: ast.NewIdent("Values")},
		}, sliceType(valueType), nil
	}
	emitter.needsRuntime = true
	if len(arguments) == 0 {
		return &ast.CallExpr{
			Fun:  &ast.SelectorExpr{X: ast.NewIdent("maort"), Sel: ast.NewIdent("OptionalValues")},
			Args: []ast.Expr{receiver},
		}, sliceType(basicType("any")), nil
	}
	if len(arguments) != 1 {
		return nil, maoType{}, fmt.Errorf("%s: values accepts zero or one argument", emitter.filename)
	}
	fallback, _, err := emitter.emitValue(arguments[0], *valueType.element)
	if err != nil {
		return nil, maoType{}, err
	}
	return &ast.CallExpr{
		Fun:  &ast.SelectorExpr{X: ast.NewIdent("maort"), Sel: ast.NewIdent("OptionalValuesOr")},
		Args: []ast.Expr{receiver, fallback},
	}, sliceType(*valueType.element), nil
}

func (emitter *emitter) emitTableConversion(arguments []expression) (ast.Expr, maoType, error) {
	if len(arguments) != 1 {
		return nil, maoType{}, fmt.Errorf("%s: table conversion accepts one value", emitter.filename)
	}
	value, typ, err := emitter.emitExpression(arguments[0], nil)
	if err != nil {
		return nil, maoType{}, err
	}
	emitter.needsRuntime = true
	switch typ.kind {
	case "slice":
		return &ast.CallExpr{
			Fun:  &ast.SelectorExpr{X: ast.NewIdent("maort"), Sel: ast.NewIdent("FromSlice")},
			Args: []ast.Expr{value},
		}, tableType(basicType("int"), *typ.element), nil
	case "array":
		return emitter.emitArrayToTable(value, typ), tableType(basicType("int"), *typ.element), nil
	case "map":
		return &ast.CallExpr{
			Fun:  &ast.SelectorExpr{X: ast.NewIdent("maort"), Sel: ast.NewIdent("FromMap")},
			Args: []ast.Expr{value},
		}, tableType(*typ.key, *typ.value), nil
	case "table":
		return value, typ, nil
	default:
		return nil, maoType{}, fmt.Errorf(
			"%s: table conversion requires a Go slice, array, or map; got %s",
			emitter.filename, typeName(typ),
		)
	}
}

func (emitter *emitter) emitArrayToTable(value ast.Expr, typ maoType) ast.Expr {
	resultType := tableType(basicType("int"), *typ.element)
	return &ast.CallExpr{
		Fun: &ast.FuncLit{
			Type: &ast.FuncType{
				Params: &ast.FieldList{List: []*ast.Field{{
					Names: []*ast.Ident{ast.NewIdent("native")}, Type: emitter.emitType(typ),
				}}},
				Results: &ast.FieldList{List: []*ast.Field{{Type: emitter.emitType(resultType)}}},
			},
			Body: &ast.BlockStmt{List: []ast.Stmt{&ast.ReturnStmt{Results: []ast.Expr{
				&ast.CallExpr{
					Fun: &ast.SelectorExpr{
						X: ast.NewIdent("maort"), Sel: ast.NewIdent("FromArray"),
					},
					Args: []ast.Expr{
						&ast.CallExpr{
							Fun: ast.NewIdent("len"), Args: []ast.Expr{ast.NewIdent("native")},
						},
						&ast.FuncLit{
							Type: &ast.FuncType{
								Params: &ast.FieldList{List: []*ast.Field{{
									Names: []*ast.Ident{ast.NewIdent("index")},
									Type:  ast.NewIdent("int"),
								}}},
								Results: &ast.FieldList{List: []*ast.Field{{
									Type: emitter.emitType(*typ.element),
								}}},
							},
							Body: &ast.BlockStmt{List: []ast.Stmt{&ast.ReturnStmt{
								Results: []ast.Expr{&ast.IndexExpr{
									X: ast.NewIdent("native"), Index: ast.NewIdent("index"),
								}},
							}}},
						},
					},
				},
			}}}},
		},
		Args: []ast.Expr{value},
	}
}

func (emitter *emitter) emitMapConversion(
	arguments []expression,
	expected *maoType,
) (ast.Expr, maoType, error) {
	if len(arguments) < 1 || len(arguments) > 2 {
		return nil, maoType{}, fmt.Errorf("%s: map conversion accepts a table and optional default value", emitter.filename)
	}
	tableValue, tableValueType, err := emitter.emitExpression(arguments[0], nil)
	if err != nil {
		return nil, maoType{}, err
	}
	if tableValueType.kind != "table" {
		return nil, maoType{}, fmt.Errorf("%s: map conversion requires a table", emitter.filename)
	}
	emitter.needsRuntime = true
	valueType := *tableValueType.value
	if valueType.kind != "optional" {
		if len(arguments) != 1 {
			return nil, maoType{}, fmt.Errorf("%s: map(table, defaultValue) requires nullable values", emitter.filename)
		}
		resultType := maoType{kind: "map", key: tableValueType.key, value: tableValueType.value}
		if expected != nil && expected.kind == "map" {
			if !sameType(*tableValueType.key, *expected.key) {
				return nil, maoType{}, fmt.Errorf("%s: map conversion cannot change key type", emitter.filename)
			}
			if sameType(valueType, *expected.value) {
				resultType = *expected
			} else if canWiden(valueType, *expected.value) {
				return &ast.CallExpr{
					Fun: &ast.SelectorExpr{
						X: ast.NewIdent("maort"), Sel: ast.NewIdent("ConvertMap"),
					},
					Args: []ast.Expr{
						tableValue, emitter.emitConverter(valueType, *expected.value),
					},
				}, *expected, nil
			}
		}
		return &ast.CallExpr{
			Fun:  &ast.SelectorExpr{X: ast.NewIdent("maort"), Sel: ast.NewIdent("ToMap")},
			Args: []ast.Expr{tableValue},
		}, resultType, nil
	}
	if len(arguments) == 1 {
		return &ast.CallExpr{
			Fun:  &ast.SelectorExpr{X: ast.NewIdent("maort"), Sel: ast.NewIdent("OptionalMap")},
			Args: []ast.Expr{tableValue},
		}, maoType{kind: "map", key: tableValueType.key, value: pointerType(basicType("any"))}, nil
	}
	fallback, _, err := emitter.emitValue(arguments[1], *valueType.element)
	if err != nil {
		return nil, maoType{}, err
	}
	element := *valueType.element
	return &ast.CallExpr{
		Fun:  &ast.SelectorExpr{X: ast.NewIdent("maort"), Sel: ast.NewIdent("OptionalMapOr")},
		Args: []ast.Expr{tableValue, fallback},
	}, maoType{kind: "map", key: tableValueType.key, value: &element}, nil
}

func pointerType(source maoType) *maoType {
	return &source
}

func (emitter *emitter) inferTable(source tableLiteral) (maoType, maoType) {
	if len(source.items) == 0 {
		return basicType("any"), basicType("any")
	}
	keyTypes := make([]maoType, 0, len(source.items))
	valueTypes := make([]maoType, 0, len(source.items))
	implicitKey := 0
	for _, item := range source.items {
		if item.explicit {
			keyTypes = append(keyTypes, emitter.inferExpression(item.key))
		} else {
			keyTypes = append(keyTypes, basicType("int"))
			implicitKey++
		}
		valueTypes = append(valueTypes, emitter.inferExpression(item.value))
	}
	return commonType(keyTypes, false), commonType(valueTypes, true)
}

func commonType(types []maoType, nullable bool) maoType {
	result := unknownType()
	hasNull := false
	for _, typ := range types {
		if typ.kind == "null" {
			hasNull = true
			continue
		}
		if result.kind == "unknown" {
			result = typ
			continue
		}
		if !sameType(result, typ) {
			result = basicType("any")
		}
	}
	if result.kind == "unknown" {
		return basicType("any")
	}
	if nullable && hasNull && result.kind == "basic" && result.name != "any" {
		return optionalType(result)
	}
	return result
}

func sameType(left, right maoType) bool {
	if left.kind != right.kind {
		return false
	}
	if left.kind == "basic" {
		return canonicalBasic(left.name) == canonicalBasic(right.name)
	}
	if left.name != right.name {
		return false
	}
	switch left.kind {
	case "optional", "slice", "pointer", "channel", "variadic":
		return sameType(*left.element, *right.element)
	case "table", "entry", "map":
		return sameType(*left.key, *right.key) && sameType(*left.value, *right.value)
	case "generic":
		if len(left.args) != len(right.args) {
			return false
		}
		for index := range left.args {
			if !sameType(left.args[index], right.args[index]) {
				return false
			}
		}
		return true
	default:
		return true
	}
}

func canonicalBasic(name string) string {
	switch name {
	case "byte":
		return "uint8"
	case "rune":
		return "int32"
	case "float":
		return "float64"
	default:
		return name
	}
}

func (emitter *emitter) inferExpression(source expression) maoType {
	switch expression := source.(type) {
	case identifier:
		if typ, exists := emitter.variables[expression.name]; exists {
			return typ
		}
		return unknownType()
	case basicLiteral:
		switch expression.kind {
		case scanner.Int:
			return basicType("int")
		case scanner.Float:
			return basicType("float64")
		case scanner.Char:
			return basicType("rune")
		case scanner.String, scanner.RawString:
			return basicType("string")
		default:
			return basicType("bool")
		}
	case nullLiteral:
		return nullType()
	case unaryExpression:
		return unaryResultType(expression.operator, emitter.inferExpression(expression.value))
	case binaryExpression:
		if binaryPrecedence(expression.operator) == 3 ||
			expression.operator == "&&" || expression.operator == "||" {
			return basicType("bool")
		}
		return emitter.inferExpression(expression.left)
	case tableLiteral:
		key, value := emitter.inferTable(expression)
		return tableType(key, value)
	case indexExpression:
		receiver := emitter.inferExpression(expression.receiver)
		if receiver.kind == "table" {
			return optionalType(*receiver.value)
		}
	case sliceExpression:
		return emitter.inferExpression(expression.receiver)
	case typeExpression:
		return expression.typ
	case typeAssertionExpression:
		return expression.typ
	case functionLiteral:
		return unknownType()
	case compositeLiteral:
		return emitter.inferExpression(expression.typ)
	case selectorExpression:
		receiver := emitter.inferExpression(expression.receiver)
		if receiver.kind == "entry" {
			if expression.name == "key" {
				return *receiver.key
			}
			if expression.name == "value" {
				return *receiver.value
			}
		}
	case callExpression:
		if name, ok := expression.function.(identifier); ok && name.name == "table" &&
			len(expression.arguments) == 1 {
			sourceType := emitter.inferExpression(expression.arguments[0])
			switch sourceType.kind {
			case "slice", "array":
				return tableType(basicType("int"), *sourceType.element)
			case "map":
				return tableType(*sourceType.key, *sourceType.value)
			}
		}
		if name, ok := expression.function.(identifier); ok && name.name == "map" &&
			len(expression.arguments) >= 1 {
			sourceType := emitter.inferExpression(expression.arguments[0])
			if sourceType.kind == "table" {
				value := *sourceType.value
				if value.kind == "optional" {
					if len(expression.arguments) == 1 {
						value = basicType("any")
					} else {
						value = *value.element
					}
				}
				return maoType{kind: "map", key: sourceType.key, value: &value}
			}
		}
		if selector, ok := expression.function.(selectorExpression); ok {
			receiver := emitter.inferExpression(selector.receiver)
			if receiver.kind == "table" {
				switch selector.name {
				case "get":
					return *receiver.value
				case "has":
					return basicType("bool")
				case "at":
					return entryType(*receiver.key, *receiver.value)
				case "keys":
					return sliceType(*receiver.key)
				case "values":
					return sliceType(*receiver.value)
				}
			}
		}
	}
	return unknownType()
}

func unaryResultType(operator string, operand maoType) maoType {
	switch operator {
	case "<-", "*":
		if operand.element != nil {
			return *operand.element
		}
	case "&":
		return maoType{kind: "pointer", element: &operand}
	case "!":
		return basicType("bool")
	}
	return operand
}

func (emitter *emitter) emitType(source maoType) ast.Expr {
	switch source.kind {
	case "basic":
		return ast.NewIdent(source.name)
	case "named":
		if packageName, member, ok := strings.Cut(source.name, "."); ok {
			return &ast.SelectorExpr{X: ast.NewIdent(packageName), Sel: ast.NewIdent(member)}
		}
		return ast.NewIdent(source.name)
	case "generic":
		arguments := make([]ast.Expr, len(source.args))
		for index, argument := range source.args {
			arguments[index] = emitter.emitType(argument)
		}
		return &ast.IndexListExpr{X: ast.NewIdent(source.name), Indices: arguments}
	case "table":
		emitter.needsRuntime = true
		return &ast.IndexListExpr{
			X:       &ast.SelectorExpr{X: ast.NewIdent("maort"), Sel: ast.NewIdent("Table")},
			Indices: []ast.Expr{emitter.emitType(*source.key), emitter.emitType(*source.value)},
		}
	case "optional":
		emitter.needsRuntime = true
		return &ast.IndexExpr{
			X:     &ast.SelectorExpr{X: ast.NewIdent("maort"), Sel: ast.NewIdent("Optional")},
			Index: emitter.emitType(*source.element),
		}
	case "slice":
		return &ast.ArrayType{Elt: emitter.emitType(*source.element)}
	case "pointer":
		return &ast.StarExpr{X: emitter.emitType(*source.element)}
	case "map":
		return &ast.MapType{
			Key: emitter.emitType(*source.key), Value: emitter.emitType(*source.value),
		}
	case "array":
		return &ast.ArrayType{
			Len: &ast.BasicLit{Kind: token.INT, Value: strconv.Itoa(source.length)},
			Elt: emitter.emitType(*source.element),
		}
	case "entry":
		emitter.needsRuntime = true
		return &ast.IndexListExpr{
			X:       &ast.SelectorExpr{X: ast.NewIdent("maort"), Sel: ast.NewIdent("Entry")},
			Indices: []ast.Expr{emitter.emitType(*source.key), emitter.emitType(*source.value)},
		}
	case "struct":
		fields := make([]*ast.Field, len(source.fields))
		for index, field := range source.fields {
			fields[index] = emitter.emitField(field)
		}
		return &ast.StructType{Fields: &ast.FieldList{List: fields}}
	case "interface":
		fields := make([]*ast.Field, len(source.fields))
		for index, field := range source.fields {
			fields[index] = emitter.emitField(field)
		}
		return &ast.InterfaceType{Methods: &ast.FieldList{List: fields}}
	case "function":
		var results *ast.FieldList
		if len(source.results) > 0 {
			results = emitter.emitFieldList(source.results)
		}
		return &ast.FuncType{
			Params: emitter.emitFieldList(source.params), Results: results,
		}
	case "channel":
		direction := ast.SEND | ast.RECV
		if source.name == "send" {
			direction = ast.SEND
		} else if source.name == "receive" {
			direction = ast.RECV
		}
		return &ast.ChanType{Dir: direction, Value: emitter.emitType(*source.element)}
	case "variadic":
		return &ast.Ellipsis{Elt: emitter.emitType(*source.element)}
	case "approximate":
		return &ast.UnaryExpr{Op: token.TILDE, X: emitter.emitType(*source.element)}
	case "union":
		result := emitter.emitType(source.args[0])
		for _, term := range source.args[1:] {
			result = &ast.BinaryExpr{X: result, Op: token.OR, Y: emitter.emitType(term)}
		}
		return result
	default:
		return ast.NewIdent("any")
	}
}
