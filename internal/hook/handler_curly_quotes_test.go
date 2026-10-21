package hook

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCurlApostrophes(t *testing.T) {
	// GIVEN - Strings with apostrophes, and the result each should have.
	cases := map[string]string{
		`"You're nearly done, don't worry"`: `"You’re nearly done, don’t worry"`,
		`"Monzo's app"`:                     `"Monzo’s app"`,
		`"name = 'x'"`:                      `"name = 'x'"`,
		`"say 'hello'"`:                     `"say 'hello'"`,
		"`It's raw`":                        "`It’s raw`",
	}
	for text, want := range cases {
		t.Run(text, func(t *testing.T) {
			// WHEN - The apostrophes are changed.
			got := curlApostrophes(text)

			// THEN - Only an apostrophe between two letters becomes ’.
			if got != want {
				t.Errorf("curlApostrophes(%s) = %s, want %s", text, got, want)
			}
		})
	}
}

func TestCurlFileOnlyChangesCustomerFacingStrings(t *testing.T) {
	// GIVEN - A Go file with customer copy, a comment, log and error messages...
	path := filepath.Join(t.TempDir(), "copy.go")
	src := `package copy

// We don't change comments.
func Title(err error) (string, error) {
	slog.Info(ctx, "Can't change logs")
	_ = terrors.Augment(err, "won't change terrors", nil)
	_ = fmt.Errorf("isn't changed: %w", err)
	_ = errors.New("doesn't change")
	old := "It's an unchanged line"
	return "You're all set", nil
}
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	// ...and every line has changed except the one that sets old.
	changed := func(line int) bool { return line != 9 }

	// WHEN - The file is processed.
	lines, err := curlFile(path, changed)
	if err != nil {
		t.Fatal(err)
	}

	// THEN - Only the customer copy on a changed line gets a curly apostrophe.
	got, _ := os.ReadFile(path)
	want := strings.Replace(src, `"You're all set"`, `"You’re all set"`, 1)
	if string(got) != want {
		t.Errorf("file =\n%s\nwant\n%s", got, want)
	}
	// ...and its line is reported.
	if len(lines) != 1 || lines[0] != 10 {
		t.Errorf("lines = %v, want [10]", lines)
	}
}

func TestCommitChecksCurlsAndRestagesStagedFile(t *testing.T) {
	// GIVEN - A git repo with a staged file that has customer copy...
	root := newGoRepo(t, "if Title() == \"\" { t.Fatal(\"empty\") }")
	writeRepoFile(t, root, "calc/calc.go", "package calc\n\nfunc Title() string {\n\treturn \"You're all set\"\n}\n")
	if out, err := exec.Command("git", "-C", root, "add", "-A").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	// ...and a constants review that passes.
	stubCommitReviewer(t, &constantsReviewer, "PASS")

	// WHEN - Claude tries to commit.
	toolInput, _ := json.Marshal(BashInput{Command: `git commit -m "change"`})
	input, _ := json.Marshal(Input{HookEventName: "PreToolUse", ToolName: "Bash", ToolInput: toolInput, CWD: root})
	var stdout, stderr bytes.Buffer
	code := Run([]string{"commit-checks"}, bytes.NewReader(input), &stdout, &stderr)

	// THEN - The commit is allowed, and Claude is told what changed...
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (allow); stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "calc/calc.go, lines 4") {
		t.Errorf("stdout = %q, want a note about calc/calc.go", stdout.String())
	}
	// ...and the curly apostrophe is staged, with nothing left unstaged.
	staged, _ := exec.Command("git", "-C", root, "diff", "--cached", "--", "calc/calc.go").Output()
	if !strings.Contains(string(staged), "You’re all set") {
		t.Errorf("staged diff does not contain the curly apostrophe:\n%s", staged)
	}
	if unstaged, _ := exec.Command("git", "-C", root, "diff", "--", "calc/calc.go").Output(); len(unstaged) > 0 {
		t.Errorf("file has unstaged changes:\n%s", unstaged)
	}
}
