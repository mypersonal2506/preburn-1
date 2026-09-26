package logging_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
)

const (
	repositoryRoot     = "../.."
	eventTypeName      = "Event"
	loggingPackageName = "logging"
	loggingImportPath  = "github.com/preburn/preburn/internal/logging"
	snakeCaseSegment   = `[a-z][a-z0-9]*(?:_[a-z0-9]+)*`
)

type eventConstant struct {
	identifier *ast.Ident
	value      string
	valid      bool
}

var (
	eventsAwaitingCallers = []string{}
	attributeKeyPattern   = regexp.MustCompile(`^` + snakeCaseSegment + `$`)
	eventValuePattern     = regexp.MustCompile(`^` + snakeCaseSegment + `(?:\.` + snakeCaseSegment + `)+$`)
)

func TestEventsGuard(t *testing.T) {
	fileSet := token.NewFileSet()
	eventsFile, otherFiles := parseSourceTree(t, fileSet)

	for _, problem := range guardEvents(fileSet, eventsFile, otherFiles, eventsAwaitingCallers) {
		t.Error(problem)
	}
}

func TestEventsGuardReportsViolations(t *testing.T) {
	startedRegistry := exampleRegistry(`	ExampleStarted Event = "example.started"
`)
	startedAndStoppedRegistry := exampleRegistry(`	ExampleStarted Event = "example.started"
	ExampleStopped Event = "example.stopped"
`)
	tests := []struct {
		name            string
		events          string
		caller          string
		awaitingCallers []string
		want            []string
	}{
		{
			name:   "registered events and snake_case keys",
			events: startedRegistry,
			caller: exampleCaller(`	event := logging.ExampleStarted
	logger.Error(ctx, event, slog.String("customer_id", "cus_1"), slog.Int("retry_count", 2))
`),
		},
		{
			name:   "string literal events",
			events: startedRegistry,
			caller: exampleCaller(`	logger.Info(ctx, "example.started")
	logger.Warn(ctx, logging.Event("example.started"))
	logger.Error(ctx, logging.ExampleStarted+".failed")
	logger.Debug(ctx, ("example.started"))
`),
			want: []string{
				"internal/example/example.go:12: logger call passes a string literal as the event",
				"internal/example/example.go:13: logger call passes a string literal as the event",
				"internal/example/example.go:14: logger call passes a string literal as the event",
				"internal/example/example.go:15: logger call passes a string literal as the event",
			},
		},
		{
			name:   "attribute keys not in snake_case",
			events: startedRegistry,
			caller: exampleCaller(`	logger.Info(ctx, logging.ExampleStarted, slog.String("customerID", "cus_1"))
	logger.Info(ctx, logging.ExampleStarted, slog.Group("stripe-account"))
`),
			want: []string{
				`internal/example/example.go:12: attribute key "customerID" is not snake_case`,
				`internal/example/example.go:13: attribute key "stripe-account" is not snake_case`,
			},
		},
		{
			name:   "reference through an aliased import",
			events: startedRegistry,
			caller: `package example

import (
	"context"

	events "github.com/preburn/preburn/internal/logging"
)

func run(ctx context.Context, logger *events.Logger) {
	logger.Info(ctx, events.ExampleStarted)
}
`,
		},
		{
			name:   "reference inside package logging",
			events: startedRegistry,
			caller: `package logging

func startedEvent() Event {
	return ExampleStarted
}
`,
		},
		{
			name:   "same name from another package",
			events: startedRegistry,
			caller: `package example

import "github.com/preburn/preburn/internal/other"

func started() string {
	return other.ExampleStarted
}
`,
			want: []string{
				"internal/logging/events.go:6: event ExampleStarted is never referenced outside events.go, reference it or add it to eventsAwaitingCallers",
			},
		},
		{
			name:   "unreferenced event",
			events: startedAndStoppedRegistry,
			caller: exampleCaller(""),
			want: []string{
				"internal/logging/events.go:7: event ExampleStopped is never referenced outside events.go, reference it or add it to eventsAwaitingCallers",
			},
		},
		{
			name:            "unreferenced event awaiting callers",
			events:          startedAndStoppedRegistry,
			caller:          exampleCaller(""),
			awaitingCallers: []string{"ExampleStopped"},
		},
		{
			name:            "referenced event awaiting callers",
			events:          startedRegistry,
			caller:          exampleCaller(""),
			awaitingCallers: []string{"ExampleStarted"},
			want: []string{
				"internal/logging/events.go:6: event ExampleStarted is referenced, remove it from eventsAwaitingCallers",
			},
		},
		{
			name:            "unknown event awaiting callers",
			events:          startedRegistry,
			caller:          exampleCaller(""),
			awaitingCallers: []string{"ExampleMissing"},
			want: []string{
				"eventsAwaitingCallers lists ExampleMissing, which is not an event in events.go",
			},
		},
		{
			name: "values not lowercase dotted",
			events: exampleRegistry(`	ExampleStarted Event = "Example.Started"
	ExampleStopped Event = "example_stopped"
`),
			caller:          exampleCaller(""),
			awaitingCallers: []string{"ExampleStopped"},
			want: []string{
				`internal/logging/events.go:6: event ExampleStarted value "Example.Started" is not lowercase dotted snake_case`,
				`internal/logging/events.go:7: event ExampleStopped value "example_stopped" is not lowercase dotted snake_case`,
			},
		},
		{
			name: "duplicate values",
			events: exampleRegistry(`	ExampleStarted Event = "example.started"
	ExampleBegun   Event = "example.started"
`),
			caller:          exampleCaller(""),
			awaitingCallers: []string{"ExampleBegun"},
			want: []string{
				`internal/logging/events.go:7: events ExampleStarted and ExampleBegun share the value "example.started"`,
			},
		},
		{
			name: "declarations without type Event or a string literal",
			events: exampleRegistry(`	ExampleStarted       = "example.started"
	ExampleStopped Event = prefix + "stopped"
`),
			caller:          exampleCaller(""),
			awaitingCallers: []string{"ExampleStopped"},
			want: []string{
				"internal/logging/events.go:6: event ExampleStarted must have type Event and a string literal value",
				"internal/logging/events.go:7: event ExampleStopped must have type Event and a string literal value",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fileSet := token.NewFileSet()
			eventsFile := parseSource(t, fileSet, "internal/logging/events.go", test.events)
			callerFile := parseSource(t, fileSet, "internal/example/example.go", test.caller)

			got := guardEvents(fileSet, eventsFile, []*ast.File{callerFile}, test.awaitingCallers)

			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("problems mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func guardEvents(fileSet *token.FileSet, eventsFile *ast.File, otherFiles []*ast.File, awaitingCallers []string) []string {
	var problems []string
	referenced := map[string]bool{}
	for _, file := range otherFiles {
		problems = append(problems, callProblems(fileSet, file)...)
		for _, name := range eventReferences(file) {
			referenced[name] = true
		}
	}
	constants := eventConstants(eventsFile)
	problems = append(problems, valueProblems(fileSet, constants)...)
	return append(problems, referenceProblems(fileSet, constants, referenced, awaitingCallers)...)
}

func callProblems(fileSet *token.FileSet, file *ast.File) []string {
	var problems []string
	ast.Inspect(file, func(node ast.Node) bool {
		call, isCall := node.(*ast.CallExpr)
		if !isCall {
			return true
		}
		selector, isSelector := call.Fun.(*ast.SelectorExpr)
		if !isSelector {
			return true
		}
		if isLoggerMethod(selector.Sel.Name) && len(call.Args) >= 2 && isLiteralEvent(call.Args[1]) {
			problems = append(problems, fmt.Sprintf("%s: logger call passes a string literal as the event", location(fileSet, call)))
		}
		if isSlogAttributeConstructor(selector) && len(call.Args) >= 1 {
			key, isLiteral := stringLiteral(call.Args[0])
			if isLiteral && !attributeKeyPattern.MatchString(key) {
				problems = append(problems, fmt.Sprintf("%s: attribute key %q is not snake_case", location(fileSet, call), key))
			}
		}
		return true
	})
	return problems
}

func eventReferences(file *ast.File) []string {
	importName, importsLogging := loggingImportName(file)
	insideLogging := file.Name.Name == loggingPackageName
	var names []string
	ast.Inspect(file, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.Ident:
			if insideLogging {
				names = append(names, node.Name)
			}
		case *ast.SelectorExpr:
			packageIdentifier, isIdentifier := node.X.(*ast.Ident)
			if importsLogging && isIdentifier && packageIdentifier.Name == importName {
				names = append(names, node.Sel.Name)
			}
		}
		return true
	})
	return names
}

func valueProblems(fileSet *token.FileSet, constants []eventConstant) []string {
	var problems []string
	namesByValue := map[string]string{}
	for _, constant := range constants {
		position := location(fileSet, constant.identifier)
		name := constant.identifier.Name
		if !constant.valid {
			problems = append(problems, fmt.Sprintf("%s: event %s must have type Event and a string literal value", position, name))
			continue
		}
		if !eventValuePattern.MatchString(constant.value) {
			problems = append(problems, fmt.Sprintf("%s: event %s value %q is not lowercase dotted snake_case", position, name, constant.value))
		}
		if firstName, duplicate := namesByValue[constant.value]; duplicate {
			problems = append(problems, fmt.Sprintf("%s: events %s and %s share the value %q", position, firstName, name, constant.value))
		}
		namesByValue[constant.value] = name
	}
	return problems
}

func referenceProblems(fileSet *token.FileSet, constants []eventConstant, referenced map[string]bool, awaitingCallers []string) []string {
	var problems []string
	declared := map[string]bool{}
	for _, constant := range constants {
		position := location(fileSet, constant.identifier)
		name := constant.identifier.Name
		declared[name] = true
		awaiting := slices.Contains(awaitingCallers, name)
		if !referenced[name] && !awaiting {
			problems = append(problems, fmt.Sprintf("%s: event %s is never referenced outside events.go, reference it or add it to eventsAwaitingCallers", position, name))
		}
		if referenced[name] && awaiting {
			problems = append(problems, fmt.Sprintf("%s: event %s is referenced, remove it from eventsAwaitingCallers", position, name))
		}
	}
	for _, name := range awaitingCallers {
		if !declared[name] {
			problems = append(problems, fmt.Sprintf("eventsAwaitingCallers lists %s, which is not an event in events.go", name))
		}
	}
	return problems
}

func eventConstants(file *ast.File) []eventConstant {
	var constants []eventConstant
	for _, declaration := range file.Decls {
		general, isGeneral := declaration.(*ast.GenDecl)
		if !isGeneral || general.Tok != token.CONST {
			continue
		}
		for _, specification := range general.Specs {
			valueSpecification := specification.(*ast.ValueSpec)
			for index, identifier := range valueSpecification.Names {
				value, valid := eventValue(valueSpecification, index)
				constants = append(constants, eventConstant{identifier: identifier, value: value, valid: valid})
			}
		}
	}
	return constants
}

func eventValue(specification *ast.ValueSpec, index int) (string, bool) {
	if !isEventType(specification.Type) || len(specification.Values) != len(specification.Names) {
		return "", false
	}
	return stringLiteral(specification.Values[index])
}

func isLiteralEvent(expression ast.Expr) bool {
	switch expression := expression.(type) {
	case *ast.BasicLit:
		return expression.Kind == token.STRING
	case *ast.ParenExpr:
		return isLiteralEvent(expression.X)
	case *ast.BinaryExpr:
		return isLiteralEvent(expression.X) || isLiteralEvent(expression.Y)
	case *ast.CallExpr:
		return len(expression.Args) == 1 && isEventType(expression.Fun) && isLiteralEvent(expression.Args[0])
	}
	return false
}

func isEventType(expression ast.Expr) bool {
	switch expression := expression.(type) {
	case *ast.Ident:
		return expression.Name == eventTypeName
	case *ast.SelectorExpr:
		return expression.Sel.Name == eventTypeName
	}
	return false
}

func isLoggerMethod(name string) bool {
	switch name {
	case "Debug", "Info", "Warn", "Error":
		return true
	}
	return false
}

func isSlogAttributeConstructor(selector *ast.SelectorExpr) bool {
	packageIdentifier, isIdentifier := selector.X.(*ast.Ident)
	if !isIdentifier || packageIdentifier.Name != "slog" {
		return false
	}
	switch selector.Sel.Name {
	case "Any", "Bool", "Duration", "Float64", "Group", "Int", "Int64", "String", "Time", "Uint64":
		return true
	}
	return false
}

func loggingImportName(file *ast.File) (string, bool) {
	for _, importSpecification := range file.Imports {
		if importSpecification.Path.Value != strconv.Quote(loggingImportPath) {
			continue
		}
		if importSpecification.Name != nil {
			return importSpecification.Name.Name, true
		}
		return loggingPackageName, true
	}
	return "", false
}

func stringLiteral(expression ast.Expr) (string, bool) {
	literal, isLiteral := expression.(*ast.BasicLit)
	if !isLiteral || literal.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(literal.Value)
	return value, err == nil
}

func location(fileSet *token.FileSet, node ast.Node) string {
	position := fileSet.Position(node.Pos())
	return fmt.Sprintf("%s:%d", position.Filename, position.Line)
}

func parseSourceTree(t *testing.T, fileSet *token.FileSet) (*ast.File, []*ast.File) {
	t.Helper()
	eventsPath := filepath.Join(repositoryRoot, "internal", "logging", "events.go")
	var eventsFile *ast.File
	var otherFiles []*ast.File
	for _, directory := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(repositoryRoot, directory), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() && entry.Name() == "testdata" {
				return filepath.SkipDir
			}
			if entry.IsDir() || filepath.Ext(path) != ".go" {
				return nil
			}
			file, err := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			if path == eventsPath {
				eventsFile = file
			} else {
				otherFiles = append(otherFiles, file)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("parse Go files under %s: %v", directory, err)
		}
	}
	if eventsFile == nil {
		t.Fatalf("%s not found", eventsPath)
	}
	return eventsFile, otherFiles
}

func parseSource(t *testing.T, fileSet *token.FileSet, path string, source string) *ast.File {
	t.Helper()
	file, err := parser.ParseFile(fileSet, path, source, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return file
}

func exampleRegistry(constants string) string {
	return "package logging\n\ntype Event string\n\nconst (\n" + constants + ")\n"
}

func exampleCaller(body string) string {
	return `package example

import (
	"context"
	"log/slog"

	"github.com/preburn/preburn/internal/logging"
)

func run(ctx context.Context, logger *logging.Logger) {
	logger.Info(ctx, logging.ExampleStarted)
` + body + "}\n"
}
