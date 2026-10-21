package hook

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Monzo copy uses the right single quotation mark for contractions and possessives.
const rightSingleQuote = '’'

// curlChangedFiles curls apostrophes in the changed Go files and returns a note for Claude.
func curlChangedFiles(root string, entries []statusEntry) string {
	var notes []string
	for _, entry := range entries {
		if !isChangedGoFile(entry) {
			continue
		}
		lines, err := curlFile(filepath.Join(root, entry.path), changedLines(root, entry))
		if err != nil || len(lines) == 0 {
			continue
		}
		note := fmt.Sprintf("- %s, lines %s", entry.path, joinLines(lines))
		switch {
		case entry.staged && entry.unstaged:
			// git add would also stage the parts the user left unstaged.
			note += " (not staged again, because the file also has unstaged changes)"
		case entry.staged:
			if _, err := gitOutput(root, "add", "--", entry.path); err != nil {
				note += " (could not stage it again: " + err.Error() + ")"
			}
		}
		notes = append(notes, note)
	}
	if len(notes) == 0 {
		return ""
	}
	return "Changed straight apostrophes to ’ in customer-facing strings:\n" + strings.Join(notes, "\n")
}

// changedLines reports which lines of the file differ from HEAD. A new file is changed in full.
func changedLines(root string, entry statusEntry) func(int) bool {
	all := func(int) bool { return true }
	if entry.untracked {
		return all
	}
	diff, err := gitOutput(root, "diff", "HEAD", "-U0", "--", entry.path)
	if err != nil {
		// A repo with no commits has no HEAD, so every file in it is new.
		return all
	}
	changed := map[int]bool{}
	for _, line := range strings.Split(diff, "\n") {
		// A hunk header is "@@ -a,b +c,d @@": the new lines are c to c+d-1, and d is 1 if left out.
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "@@" {
			continue
		}
		startText, countText, hasCount := strings.Cut(strings.TrimPrefix(fields[2], "+"), ",")
		start, _ := strconv.Atoi(startText)
		count := 1
		if hasCount {
			count, _ = strconv.Atoi(countText)
		}
		for n := start; n < start+count; n++ {
			changed[n] = true
		}
	}
	return func(n int) bool { return changed[n] }
}

// curlFile curls apostrophes in customer-facing strings on changed lines, and returns the lines it changed.
func curlFile(path string, changed func(int) bool) ([]int, error) {
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
		start, end := fset.Position(literal.Pos()), fset.Position(literal.End())
		if !anyLineChanged(start.Line, end.Line, changed) {
			continue
		}
		curled := curlApostrophes(literal.Value)
		if curled == literal.Value {
			continue
		}
		out = append(out[:start.Offset], append([]byte(curled), out[end.Offset:]...)...)
		lineSet[start.Line] = true
	}
	if len(lineSet) == 0 {
		return nil, nil
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, out, info.Mode().Perm()); err != nil {
		return nil, err
	}
	lines := make([]int, 0, len(lineSet))
	for line := range lineSet {
		lines = append(lines, line)
	}
	sort.Ints(lines)
	return lines, nil
}

// isLogOrErrorCall reports whether call logs or builds an error. Their messages
// are for engineers, not customers.
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

func anyLineChanged(first, last int, changed func(int) bool) bool {
	for line := first; line <= last; line++ {
		if changed(line) {
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
