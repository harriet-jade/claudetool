package hook

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestMayNeedConstantReview(t *testing.T) {
	// GIVEN - Changed code that may or may not need a constants review.
	cases := map[string]bool{
		"const ThreeMinutes = 3 * time.Minute": true,
		"area := 3.14 * r * r":                 true,
		"rsp.Items = rsp.Items[:50]":           true,
		"workers := 10":                        true,
		"if len(items) == 0 {":                 false,
		"i += 1":                               false,
		"return v2.Client{}":                   false,
		"name := firstName + lastName":         false,
	}
	for text, want := range cases {
		t.Run(text, func(t *testing.T) {
			// WHEN - The quick check runs.
			got := mayNeedConstantReview(text)

			// THEN - Only a const or a number other than 0 or 1 starts a review.
			if got != want {
				t.Errorf("mayNeedConstantReview(%q) = %v, want %v", text, got, want)
			}
		})
	}
}

func TestCommitChecksPassesOnConstantsAdvice(t *testing.T) {
	// GIVEN - A git repo whose passing test file adds a constant...
	root := newGoRepo(t, "if ThreeMinutes == 0 { t.Fatal(\"wrong\") }")
	writeRepoFile(t, root, "calc/calc.go", "package calc\n\nimport \"time\"\n\nconst ThreeMinutes = 3 * time.Minute\n")
	// ...and a constants reviewer that asks for a change.
	prompt := stubCommitReviewer(t, &constantsReviewer, "REVISE\n- calc/calc.go: ThreeMinutes only repeats its value -> inline it")

	// WHEN - Claude tries to commit.
	toolInput, _ := json.Marshal(BashInput{Command: `git commit -m "change"`})
	input, _ := json.Marshal(Input{HookEventName: "PreToolUse", ToolName: "Bash", ToolInput: toolInput, CWD: root})
	var stdout, stderr bytes.Buffer
	code := Run([]string{"commit-checks"}, bytes.NewReader(input), &stdout, &stderr)

	// THEN - The commit is allowed, and Claude gets the reviewer's advice...
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (allow); stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "ThreeMinutes only repeats its value") {
		t.Errorf("stdout = %q, want the reviewer's advice", stdout.String())
	}
	// ...and the reviewer was given the new file.
	if !strings.Contains(*prompt, "NEW FILE calc/calc.go") {
		t.Errorf("prompt does not contain the new file:\n%s", *prompt)
	}
}

// stubCommitReviewer makes a commit-time review answer verdict without starting claude, and returns the prompt it gets.
func stubCommitReviewer(t *testing.T, reviewer *func(prompt, model, dir string) (string, error), verdict string) *string {
	t.Helper()
	prompt := new(string)
	original, originalLog := *reviewer, asyncLogOverride
	*reviewer = func(p, model, dir string) (string, error) {
		*prompt = p
		return verdict, nil
	}
	asyncLogOverride = filepath.Join(t.TempDir(), "async.log")
	t.Cleanup(func() { *reviewer, asyncLogOverride = original, originalLog })
	return prompt
}
