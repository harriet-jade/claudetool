// Command curly-apostrophes changes straight apostrophes to curly apostrophes in the customer-facing strings of Go
// files. Run it with --help for the full usage.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Monzo copy uses the right single quotation mark for contractions and possessives.
const rightSingleQuote = '’'

const usage = `curly-apostrophes changes straight apostrophes (') to curly apostrophes (’)
in the customer-facing strings of Go files. It edits the files in place.

Usage:
  curly-apostrophes [--dry-run] FILE...

Options:
  --dry-run   Show the lines that would change, but do not write the files.
  -h, --help  Show this help.

What it changes:
  In each string literal, a ' that has a letter on both sides becomes ’.
  "don't" becomes "don’t", and "Monzo's" becomes "Monzo’s".
  It changes all the string literals in the file, not only changed lines.

What it does not change:
  - Comments.
  - Strings inside calls to slog.*, terrors.*, errors.New and fmt.Errorf,
    because those messages are for engineers.
  - Generated files (files with a "Code generated ... DO NOT EDIT." line).
  - Quotes that do not have a letter on both sides, such as 'x' or 'hello'.
  - Possessives that end in s, such as customers' accounts.

It cannot know if a string is really for customers. It also changes other
strings, such as SQL, JSON keys and test data, so read the result before
you commit it.

Output:
  One line for each file that changed: "PATH: lines N, N".
  Then a summary line. Errors go to stderr.

Exit codes:
  0  All files were processed (with or without changes).
  1  A file could not be read, parsed or written. The other files are
     still processed.
  2  The command was used incorrectly (for example, no files).

Examples:
  curly-apostrophes handler/copy.go
  curly-apostrophes --dry-run $(git diff --name-only -- '*.go')
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("curly-apostrophes", flag.ContinueOnError)
	flags.SetOutput(stderr)
	// The usage is printed below, so that help goes to stdout and a bad flag goes to stderr.
	flags.Usage = func() {}
	dryRun := flags.Bool("dry-run", false, "")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			fmt.Fprint(stdout, usage)
			return 0
		}
		fmt.Fprint(stderr, "\n"+usage)
		return 2
	}
	paths := flags.Args()
	if len(paths) == 0 {
		fmt.Fprint(stderr, "curly-apostrophes: give at least one file\n\n"+usage)
		return 2
	}

	verb := "lines"
	if *dryRun {
		verb = "would change lines"
	}
	code, changed := 0, 0
	for _, path := range paths {
		lines, err := curlFile(path, !*dryRun)
		if err != nil {
			fmt.Fprintf(stderr, "curly-apostrophes: %s: %v\n", path, err)
			code = 1
			continue
		}
		if len(lines) > 0 {
			changed++
			fmt.Fprintf(stdout, "%s: %s %s\n", path, verb, joinLines(lines))
		}
	}
	summary := "Changed"
	if *dryRun {
		summary = "Would change"
	}
	fmt.Fprintf(stdout, "%s %d of %d files.\n", summary, changed, len(paths))
	return code
}

// curlFile curls the apostrophes in the customer-facing strings of a Go file. It writes the file only if write is
// true. It returns the lines where a changed string starts.
func curlFile(path string, write bool) ([]int, error) {
	if !strings.HasSuffix(path, ".go") {
		return nil, fmt.Errorf("not a Go file")
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	if ast.IsGenerated(file) {
		return nil, nil
	}

	var skipped [][2]token.Pos
	var literals []*ast.BasicLit
	ast.Inspect(file, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.CallExpr:
			if isLogOrErrorCall(n) {
				skipped = append(skipped, [2]token.Pos{n.Lparen, n.Rparen})
			}
		case *ast.BasicLit:
			if n.Kind == token.STRING {
				literals = append(literals, n)
			}
		}
		return true
	})

	out := []byte(string(src))
	lineSet := map[int]bool{}
	// Replace from the end of the file, so earlier offsets stay correct.
	for i := len(literals) - 1; i >= 0; i-- {
		literal := literals[i]
		if insideAny(literal.Pos(), skipped) {
			continue
		}
		curled := curlApostrophes(literal.Value)
		if curled == literal.Value {
			continue
		}
		start, end := fset.Position(literal.Pos()), fset.Position(literal.End())
		out = append(out[:start.Offset], append([]byte(curled), out[end.Offset:]...)...)
		lineSet[start.Line] = true
	}
	if len(lineSet) == 0 {
		return nil, nil
	}

	if write {
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, out, info.Mode().Perm()); err != nil {
			return nil, err
		}
	}
	lines := make([]int, 0, len(lineSet))
	for line := range lineSet {
		lines = append(lines, line)
	}
	sort.Ints(lines)
	return lines, nil
}

// isLogOrErrorCall reports whether call logs or builds an error. Their messages are for engineers, not customers.
func isLogOrErrorCall(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	if !ok {
		return false
	}
	switch pkg.Name {
	case "slog", "terrors":
		return true
	case "errors":
		return selector.Sel.Name == "New"
	case "fmt":
		return selector.Sel.Name == "Errorf"
	}
	return false
}

func insideAny(pos token.Pos, ranges [][2]token.Pos) bool {
	for _, r := range ranges {
		if pos >= r[0] && pos <= r[1] {
			return true
		}
	}
	return false
}

// curlApostrophes changes each ' between two letters to ’, as in don't and Monzo's.
func curlApostrophes(text string) string {
	runes := []rune(text)
	for i := 1; i < len(runes)-1; i++ {
		if runes[i] == '\'' && unicode.IsLetter(runes[i-1]) && unicode.IsLetter(runes[i+1]) {
			runes[i] = rightSingleQuote
		}
	}
	return string(runes)
}

func joinLines(lines []int) string {
	parts := make([]string, len(lines))
	for i, line := range lines {
		parts[i] = strconv.Itoa(line)
	}
	return strings.Join(parts, ", ")
}
