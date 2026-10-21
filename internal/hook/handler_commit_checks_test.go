package hook

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCheckedGitCommitDir(t *testing.T) {
	home, _ := os.UserHomeDir()
	// GIVEN - Commands run from /repo, and the folder each checked git commit runs in ("" for none).
	cases := map[string]string{
		`git commit -m "fix"`:                    "/repo",
		`git add a.go && git commit -m "fix"`:    "/repo",
		`git -c user.name=x commit -m "fix"`:     "/repo",
		`/usr/bin/git commit`:                    "/repo",
		`cd /other && git commit -m "fix"`:       "/other",
		`cd sub && git commit -m "fix"`:          "/repo/sub",
		`cd ~/src && git commit -m "fix"`:        filepath.Join(home, "src"),
		`git commit --no-verify -m "fix"`:        "",
		`git commit -n -m "fix"`:                 "",
		`git log --grep commit`:                  "",
		`git status`:                             "",
		`echo "git commit"`:                      "",
		`cd /other && git commit --no-verify -m`: "",
	}
	for command, want := range cases {
		t.Run(command, func(t *testing.T) {
			// WHEN - The command is checked.
			got, ok := checkedGitCommitDir(command, "/repo")

			// THEN - Only a git commit without --no-verify is caught, in the folder it runs in.
			if ok != (want != "") || got != want {
				t.Errorf("checkedGitCommitDir(%q) = (%q, %v), want %q", command, got, ok, want)
			}
		})
	}
}

func TestChangedGoPackages(t *testing.T) {
	// GIVEN - A repo with a service, a library and a vendored package...
	root := t.TempDir()
	for _, dir := range []string{"service.foo/handler", "libraries/money", "vendor/x"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// ...and git status output that changes files in each, plus a deleted file and a non-Go file.
	status := strings.Join([]string{
		" M service.foo/handler/read.go",
		"A  service.foo/handler/write.go",
		"?? libraries/money/round.go",
		"R  libraries/money/old.go -> libraries/money/new.go",
		" D service.bar/gone.go",
		" M vendor/x/x.go",
		" M README.md",
	}, "\n")

	// WHEN - The changed packages are worked out.
	packages, services := changedGoPackages(root, changedPaths(status))

	// THEN - The service is checked as a whole, the library by package, and vendor is skipped.
	if want := []string{"./libraries/money", "./service.foo/..."}; !reflect.DeepEqual(packages, want) {
		t.Errorf("packages = %v, want %v", packages, want)
	}
	// ...and only the service goes to svcchecker.
	if want := []string{"./service.foo/..."}; !reflect.DeepEqual(services, want) {
		t.Errorf("services = %v, want %v", services, want)
	}
}

func TestCommitChecksBlocksFailingTest(t *testing.T) {
	// GIVEN - A git repo with a changed package whose test fails.
	root := newGoRepo(t, "if 1+1 != 3 { t.Fatal(\"maths is broken\") }")

	// WHEN - Claude tries to commit.
	code, stderr := runCommitChecks(t, root, `git commit -m "change"`)

	// THEN - The commit is blocked, and the go test output is shown.
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (block); stderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, "$ go test") || !strings.Contains(stderr, "maths is broken") {
		t.Errorf("stderr = %q, want go test failure output", stderr)
	}
}

func TestCommitChecksAllowsPassingTest(t *testing.T) {
	// GIVEN - A git repo with a changed package whose test passes.
	root := newGoRepo(t, "if 1+1 != 2 { t.Fatal(\"maths is broken\") }")

	// WHEN - Claude tries to commit.
	code, stderr := runCommitChecks(t, root, `git commit -m "change"`)

	// THEN - The commit is allowed.
	if code != 0 {
		t.Errorf("exit = %d, want 0 (allow); stderr: %s", code, stderr)
	}
}

func TestCommitChecksSkipsNoVerify(t *testing.T) {
	// GIVEN - A git repo with a changed package whose test fails.
	root := newGoRepo(t, "t.Fatal(\"always fails\")")

	// WHEN - Claude commits with --no-verify.
	code, stderr := runCommitChecks(t, root, `git commit --no-verify -m "change"`)

	// THEN - The checks don't run, so the commit is allowed.
	if code != 0 {
		t.Errorf("exit = %d, want 0 (allow); stderr: %s", code, stderr)
	}
}

// newGoRepo creates a git repo with one uncommitted package whose test body is testBody.
func newGoRepo(t *testing.T, testBody string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	files := map[string]string{
		"go.mod":            "module example.com/demo\n\ngo 1.25\n",
		"calc/calc.go":      "package calc\n",
		"calc/calc_test.go": "package calc\n\nimport \"testing\"\n\nfunc TestCalc(t *testing.T) {\n\t" + testBody + "\n}\n",
	}
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	return root
}

func runCommitChecks(t *testing.T, cwd, command string) (int, string) {
	t.Helper()
	toolInput, _ := json.Marshal(BashInput{Command: command})
	input, _ := json.Marshal(Input{
		HookEventName: "PreToolUse",
		ToolName:      "Bash",
		ToolInput:     toolInput,
		CWD:           cwd,
	})
	var stdout, stderr bytes.Buffer
	code := Run([]string{"commit-checks"}, bytes.NewReader(input), &stdout, &stderr)
	return code, stderr.String()
}
