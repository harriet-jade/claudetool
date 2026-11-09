package hook

import (
	"strings"
	"testing"
)

func TestCommitChecksPassesOnWritingAdvice(t *testing.T) {
	// GIVEN - A git repo with no Go code, and a new README...
	root := newEmptyRepo(t)
	writeRepoFile(t, root, "README.md", "# Payouts\n\nThis service leverages a robust pipeline to seamlessly facilitate payouts.\n")
	// ...and a writing review that finds a problem.
	prompt := stubCommitReviewer(t, &writingReviewer, "REVISE\n### README.md\n- \"This service leverages\"\n  Guideline: 6. Keep it natural, not corporate")

	// WHEN - Claude tries to commit.
	code, stdout, stderr := runCommitChecksWithOutput(t, root, `git commit -m "change"`)

	// THEN - The commit is allowed, and Claude gets the reviewer's advice...
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (allow); stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "Keep it natural, not corporate") {
		t.Errorf("stdout = %q, want the reviewer’s advice", stdout)
	}
	// ...and the reviewer was given the README and the guidelines.
	if !strings.Contains(*prompt, "NEW FILE README.md") {
		t.Errorf("prompt does not contain the README:\n%s", *prompt)
	}
	if !strings.Contains(*prompt, "THE BAR FOR FEEDBACK") {
		t.Errorf("prompt does not contain the guidelines:\n%s", *prompt)
	}
}

func TestReviewWritingSendsCodeComments(t *testing.T) {
	// GIVEN - A git repo with a new Go file that has a comment.
	root := newEmptyRepo(t)
	writeRepoFile(t, root, "calc/calc.go", "package calc\n\n// Add returns the sum.\nfunc Add(a, b int) int { return a + b }\n")
	prompt := stubCommitReviewer(t, &writingReviewer, "PASS")

	// WHEN - The writing review runs.
	reviewWriting(root)

	// THEN - The reviewer is given the Go file.
	if !strings.Contains(*prompt, "NEW FILE calc/calc.go") {
		t.Errorf("prompt does not contain the Go file:\n%s", *prompt)
	}
}

func TestReviewWritingSkipsChangesWithNoWriting(t *testing.T) {
	// GIVEN - Changes that add no comment and no document of this repo.
	cases := map[string]string{
		"code without comments": "calc/calc.go",
		"a vendored document":   "vendor/x/README.md",
		"a nested node module":  "web/node_modules/x/README.md",
	}
	for name, path := range cases {
		t.Run(name, func(t *testing.T) {
			root := newEmptyRepo(t)
			writeRepoFile(t, root, path, "package calc\n\nfunc Add(a, b int) int { return a + b }\n")
			prompt := stubCommitReviewer(t, &writingReviewer, "REVISE\n- bad")

			// WHEN - The writing review runs.
			feedback := reviewWriting(root)

			// THEN - The reviewer does not run.
			if feedback != "" || *prompt != "" {
				t.Errorf("feedback = %q, prompt = %q, want no review", feedback, *prompt)
			}
		})
	}
}
