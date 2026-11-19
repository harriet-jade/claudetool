package hook

import (
	"bytes"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPRCreateDir(t *testing.T) {
	// GIVEN - Commands run from /repo, and the folder each PR create runs in ("" for none).
	cases := map[string]string{
		`gh pr create --title "Add x" --body ""`: "/repo",
		`gh pr new --title x --body ""`:          "/repo",
		`git push && gh pr create --title x`:     "/repo",
		`cd sub && gh pr create --title x`:       "/repo/sub",
		`gh pr create --help`:                    "",
		`gh pr create --dry-run --title x`:       "",
		`gh pr view`:                             "",
		`gh pr edit --title x`:                   "",
		`echo "gh pr create"`:                    "",
	}
	for command, want := range cases {
		t.Run(command, func(t *testing.T) {
			// WHEN - The command is checked.
			got, _, ok := prCreateDir(command, "/repo")

			// THEN - Only a PR create is caught, in the folder it runs in.
			if ok != (want != "") || got != want {
				t.Errorf("prCreateDir(%q) = (%q, %v), want %q", command, got, ok, want)
			}
		})
	}
}

func TestPRBaseFlag(t *testing.T) {
	// GIVEN - PR create arguments, and the base branch that each sets.
	cases := map[string]string{
		`--title x --base develop`: "develop",
		`--base=develop`:           "develop",
		`-B "develop"`:             "develop",
		`--title x`:                "",
	}
	for args, want := range cases {
		t.Run(args, func(t *testing.T) {
			// WHEN - The base is read from the arguments.
			got := prBaseFlag(strings.Fields(args))

			// THEN - It is the value of --base or -B.
			if got != want {
				t.Errorf("prBaseFlag(%q) = %q, want %q", args, got, want)
			}
		})
	}
}

func TestOneChangePerPRBlocksOnceForEachHead(t *testing.T) {
	// GIVEN - A branch with a commit, and a reviewer that asks for a split.
	root := newBranchRepo(t)
	prompts := stubOneChangeReviewer(t, "REVISE\n1. Adds the feature\n   Files: feature.txt", nil)

	// WHEN - Claude tries to open the PR.
	code, stderr := runOneChangePerPR(t, root)

	// THEN - The PR is blocked, and the agent gets the reviewer's split.
	if code != 2 || !strings.Contains(stderr, "Adds the feature") {
		t.Fatalf("exit = %d, stderr = %q, want a block with the reviewer's split", code, stderr)
	}

	// WHEN - Claude tries again with the same commits.
	code, stderr = runOneChangePerPR(t, root)

	// THEN - The PR opens, and the reviewer does not run again.
	if code != 0 || len(*prompts) != 1 {
		t.Errorf("exit = %d, reviews = %d, stderr = %q, want exit 0 after 1 review", code, len(*prompts), stderr)
	}

	// WHEN - Claude adds a commit and tries again.
	writeRepoFile(t, root, "more.txt", "more\n")
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-q", "-m", "Add more")
	code, _ = runOneChangePerPR(t, root)

	// THEN - The new HEAD gets a new review.
	if code != 2 || len(*prompts) != 2 {
		t.Errorf("exit = %d, reviews = %d, want a block after a second review", code, len(*prompts))
	}
}

func TestOneChangePerPRAllowsOnePass(t *testing.T) {
	// GIVEN - A branch with a commit, and a reviewer that passes it.
	root := newBranchRepo(t)
	stubOneChangeReviewer(t, "PASS", nil)

	// WHEN - Claude tries to open the PR.
	code, stderr := runOneChangePerPR(t, root)

	// THEN - The PR opens.
	if code != 0 {
		t.Errorf("exit = %d, want 0; stderr: %s", code, stderr)
	}
}

func TestOneChangePerPRReviewsAgainAfterAnError(t *testing.T) {
	// GIVEN - A branch with a commit, and a reviewer that fails to run.
	root := newBranchRepo(t)
	prompts := stubOneChangeReviewer(t, "", errors.New("not logged in"))

	// WHEN - Claude tries to open the PR twice.
	code, _ := runOneChangePerPR(t, root)
	runOneChangePerPR(t, root)

	// THEN - The PR opens, and the second attempt gets a review again.
	if code != 0 || len(*prompts) != 2 {
		t.Errorf("exit = %d, reviews = %d, want exit 0 and 2 reviews", code, len(*prompts))
	}
}

func TestOneChangePerPRReviewsOnlyTheBranch(t *testing.T) {
	// GIVEN - A branch with a commit, and a merge of a later commit on master...
	root := newBranchRepo(t)
	gitRun(t, root, "checkout", "-q", "master")
	writeRepoFile(t, root, "from-master.txt", "master\n")
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-q", "-m", "Change on master")
	gitRun(t, root, "checkout", "-q", "feature")
	gitRun(t, root, "merge", "-q", "--no-edit", "master")
	// ...and a reviewer that passes it.
	prompts := stubOneChangeReviewer(t, "PASS", nil)

	// WHEN - Claude tries to open the PR.
	runOneChangePerPR(t, root)

	// THEN - The reviewer gets the branch's commit and file, but not the ones from master.
	if len(*prompts) != 1 {
		t.Fatalf("reviews = %d, want 1", len(*prompts))
	}
	prompt := (*prompts)[0]
	if !strings.Contains(prompt, "Add the feature") || !strings.Contains(prompt, "feature.txt") {
		t.Errorf("prompt does not have the branch's commit and file:\n%s", prompt)
	}
	if strings.Contains(prompt, "from-master.txt") || strings.Contains(prompt, "Change on master") {
		t.Errorf("prompt has the change from master:\n%s", prompt)
	}
}

// newBranchRepo creates a git repo with one commit on master and one commit on the branch feature, which is checked out.
func newBranchRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	gitRun(t, root, "init", "-q", "-b", "master")
	writeRepoFile(t, root, "README.md", "demo\n")
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-q", "-m", "First")
	gitRun(t, root, "checkout", "-q", "-b", "feature")
	writeRepoFile(t, root, "feature.txt", "feature\n")
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-q", "-m", "Add the feature")
	return root
}

func gitRun(t *testing.T, root string, args ...string) {
	t.Helper()
	args = append([]string{"-C", root, "-c", "user.name=test", "-c", "user.email=test@example.com"}, args...)
	if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// stubOneChangeReviewer makes the reviewer answer out and err without starting claude, and returns the prompts it gets.
func stubOneChangeReviewer(t *testing.T, out string, err error) *[]string {
	t.Helper()
	prompts := new([]string)
	original, originalLog := oneChangeReviewer, asyncLogOverride
	oneChangeReviewer = func(prompt, model, dir string) (string, error) {
		*prompts = append(*prompts, prompt)
		return out, err
	}
	asyncLogOverride = filepath.Join(t.TempDir(), "async.log")
	t.Cleanup(func() { oneChangeReviewer, asyncLogOverride = original, originalLog })
	return prompts
}

func runOneChangePerPR(t *testing.T, root string) (int, string) {
	t.Helper()
	toolInput, _ := json.Marshal(BashInput{Command: `gh pr create --title "Add the feature" --body ""`})
	input, _ := json.Marshal(Input{HookEventName: "PreToolUse", ToolName: "Bash", ToolInput: toolInput, CWD: root})
	var stdout, stderr bytes.Buffer
	code := Run([]string{"one-change-per-pr"}, bytes.NewReader(input), &stdout, &stderr)
	return code, stderr.String()
}
