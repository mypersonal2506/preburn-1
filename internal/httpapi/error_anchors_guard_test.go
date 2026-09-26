package httpapi_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

const (
	guardRepositoryRoot = "../.."
	errorsDocumentPath  = "docs/errors.md"
	httpapiPackageName  = "httpapi"
	httpapiImportPath   = "github.com/preburn/preburn/internal/httpapi"
	problemCodeMethod   = "ProblemCode"
	codeConstantPrefix  = "code"
	anchorHeadingPrefix = "## "
)

type errorCode struct {
	value    string
	position string
}

var (
	anchorRemovedCharacters = regexp.MustCompile(`[^a-z0-9 _-]`)
	codeArgumentIndexes     = map[string]int{"NewCodedError": 1, "NewCodedValidationProblem": 0}
)

func TestErrorAnchorsGuard(t *testing.T) {
	fileSet := token.NewFileSet()
	files := parseErrorSourceTree(t, fileSet)
	document, err := os.ReadFile(filepath.Join(guardRepositoryRoot, errorsDocumentPath))
	if err != nil {
		t.Fatalf("read %s: %v", errorsDocumentPath, err)
	}

	for _, problem := range guardErrorAnchors(fileSet, files, string(document)) {
		t.Error(problem)
	}
}

func TestErrorAnchorsGuardReportsViolations(t *testing.T) {
	httpapiSource := `package httpapi

const (
	codeNotFound = "not_found"
	problemTypeBase = "https://example.com"
)

func (coded *codedError) ProblemCode() string {
	return coded.code
}
`
	tests := []struct {
		name     string
		caller   string
		document string
		want     []string
	}{
		{
			name:     "every code documented",
			caller:   exampleCodedErrors(`"example_taken"`),
			document: "# Errors\n\n## not_found\n\nText.\n\n## example_taken\n",
		},
		{
			name:     "code without an anchor",
			caller:   exampleCodedErrors(`"example_taken"`),
			document: "## not_found\n",
			want:     []string{"internal/example/errors.go:10: error code example_taken has no anchor in docs/errors.md"},
		},
		{
			name:     "httpapi code without an anchor",
			caller:   exampleCodedErrors(`"example_taken"`),
			document: "## example_taken\n",
			want:     []string{"internal/httpapi/problems.go:4: error code not_found has no anchor in docs/errors.md"},
		},
		{
			name:     "anchor without a code",
			caller:   exampleCodedErrors(`"example_taken"`),
			document: "## not_found\n## example_taken\n## example_gone\n",
			want:     []string{"docs/errors.md documents example_gone, which no error code declares"},
		},
		{
			name:     "code that is not a string literal",
			caller:   exampleCodedErrors(`"example_" + "taken"`),
			document: "## not_found\n",
			want:     []string{"internal/example/errors.go:10: NewCodedError call passes a code that is not a string literal"},
		},
		{
			name:     "coded validation problem without an anchor",
			caller:   exampleCodedValidationProblem(`"example_invalid"`),
			document: "## not_found\n",
			want:     []string{"internal/example/errors.go:6: error code example_invalid has no anchor in docs/errors.md"},
		},
		{
			name:     "coded validation problem documented",
			caller:   exampleCodedValidationProblem(`"example_invalid"`),
			document: "## not_found\n## example_invalid\n",
		},
		{
			name:     "coded validation problem code that is not a string literal",
			caller:   exampleCodedValidationProblem(`exampleCode`),
			document: "## not_found\n",
			want:     []string{"internal/example/errors.go:6: NewCodedValidationProblem call passes a code that is not a string literal"},
		},
		{
			name: "coded error type outside httpapi",
			caller: `package example

type takenError struct{}

func (takenError) ProblemCode() string {
	return "example_taken"
}
`,
			document: "## not_found\n",
			want:     []string{"internal/example/errors.go:5: ProblemCode method outside package httpapi, declare the error with httpapi.NewCodedError"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fileSet := token.NewFileSet()
			files := []*ast.File{
				parseGuardSource(t, fileSet, "internal/httpapi/problems.go", httpapiSource),
				parseGuardSource(t, fileSet, "internal/example/errors.go", test.caller),
			}

			got := guardErrorAnchors(fileSet, files, test.document)

			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("problems mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func guardErrorAnchors(fileSet *token.FileSet, files []*ast.File, document string) []string {
	var problems []string
	var codes []errorCode
	for _, file := range files {
		fileCodes, fileProblems := declaredErrorCodes(fileSet, file)
		codes = append(codes, fileCodes...)
		problems = append(problems, fileProblems...)
	}
	anchors := documentAnchors(document)
	declared := map[string]bool{}
	for _, code := range codes {
		declared[code.value] = true
		if !slices.Contains(anchors, code.value) {
			problems = append(problems, fmt.Sprintf("%s: error code %s has no anchor in %s", code.position, code.value, errorsDocumentPath))
		}
	}
	for _, anchor := range anchors {
		if !declared[anchor] {
			problems = append(problems, fmt.Sprintf("%s documents %s, which no error code declares", errorsDocumentPath, anchor))
		}
	}
	return problems
}

func declaredErrorCodes(fileSet *token.FileSet, file *ast.File) ([]errorCode, []string) {
	insideHTTPAPI := file.Name.Name == httpapiPackageName
	importName, importsHTTPAPI := httpapiImportName(file)
	var codes []errorCode
	var problems []string
	ast.Inspect(file, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.ValueSpec:
			if insideHTTPAPI {
				codes = append(codes, codeConstants(fileSet, node)...)
			}
		case *ast.FuncDecl:
			if node.Recv != nil && node.Name.Name == problemCodeMethod && !insideHTTPAPI {
				problems = append(problems, fmt.Sprintf("%s: ProblemCode method outside package httpapi, declare the error with httpapi.NewCodedError", guardLocation(fileSet, node)))
			}
		case *ast.CallExpr:
			constructor, callsConstructor := codedErrorConstructor(node, insideHTTPAPI, importName, importsHTTPAPI)
			if !callsConstructor {
				return true
			}
			code, isLiteral := guardStringLiteral(node.Args[codeArgumentIndexes[constructor]])
			if !isLiteral {
				problems = append(problems, fmt.Sprintf("%s: %s call passes a code that is not a string literal", guardLocation(fileSet, node), constructor))
				return true
			}
			codes = append(codes, errorCode{value: code, position: guardLocation(fileSet, node)})
		}
		return true
	})
	return codes, problems
}

func codeConstants(fileSet *token.FileSet, specification *ast.ValueSpec) []errorCode {
	var codes []errorCode
	for index, identifier := range specification.Names {
		if !strings.HasPrefix(identifier.Name, codeConstantPrefix) || index >= len(specification.Values) {
			continue
		}
		if value, isLiteral := guardStringLiteral(specification.Values[index]); isLiteral {
			codes = append(codes, errorCode{value: value, position: guardLocation(fileSet, identifier)})
		}
	}
	return codes
}

func codedErrorConstructor(call *ast.CallExpr, insideHTTPAPI bool, importName string, importsHTTPAPI bool) (string, bool) {
	var name string
	switch function := call.Fun.(type) {
	case *ast.Ident:
		if !insideHTTPAPI {
			return "", false
		}
		name = function.Name
	case *ast.SelectorExpr:
		packageIdentifier, isIdentifier := function.X.(*ast.Ident)
		if !importsHTTPAPI || !isIdentifier || packageIdentifier.Name != importName {
			return "", false
		}
		name = function.Sel.Name
	default:
		return "", false
	}
	_, isConstructor := codeArgumentIndexes[name]
	return name, isConstructor
}

func documentAnchors(document string) []string {
	var anchors []string
	for line := range strings.Lines(document) {
		heading, isHeading := strings.CutPrefix(strings.TrimRight(line, "\n"), anchorHeadingPrefix)
		if !isHeading {
			continue
		}
		anchor := anchorRemovedCharacters.ReplaceAllString(strings.ToLower(strings.TrimSpace(heading)), "")
		anchors = append(anchors, strings.ReplaceAll(anchor, " ", "-"))
	}
	return anchors
}

func httpapiImportName(file *ast.File) (string, bool) {
	for _, importSpecification := range file.Imports {
		if importSpecification.Path.Value != strconv.Quote(httpapiImportPath) {
			continue
		}
		if importSpecification.Name != nil {
			return importSpecification.Name.Name, true
		}
		return httpapiPackageName, true
	}
	return "", false
}

func guardStringLiteral(expression ast.Expr) (string, bool) {
	literal, isLiteral := expression.(*ast.BasicLit)
	if !isLiteral || literal.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(literal.Value)
	return value, err == nil
}

func guardLocation(fileSet *token.FileSet, node ast.Node) string {
	position := fileSet.Position(node.Pos())
	return fmt.Sprintf("%s:%d", position.Filename, position.Line)
}

func parseErrorSourceTree(t *testing.T, fileSet *token.FileSet) []*ast.File {
	t.Helper()
	var files []*ast.File
	for _, directory := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(guardRepositoryRoot, directory), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() && entry.Name() == "testdata" {
				return filepath.SkipDir
			}
			if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			files = append(files, file)
			return nil
		})
		if err != nil {
			t.Fatalf("parse Go files under %s: %v", directory, err)
		}
	}
	return files
}

func parseGuardSource(t *testing.T, fileSet *token.FileSet, path string, source string) *ast.File {
	t.Helper()
	file, err := parser.ParseFile(fileSet, path, source, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return file
}

func exampleCodedErrors(code string) string {
	return `package example

import (
	"net/http"

	"github.com/preburn/preburn/internal/httpapi"
)

// ErrTaken is an example coded error.
var ErrTaken = httpapi.NewCodedError(http.StatusConflict, ` + code + `, "taken")
`
}

func exampleCodedValidationProblem(code string) string {
	return `package example

import "github.com/preburn/preburn/internal/httpapi"

func invalid(fieldErrors []httpapi.ProblemError) error {
	return httpapi.NewCodedValidationProblem(` + code + `, "invalid", fieldErrors...)
}
`
}
