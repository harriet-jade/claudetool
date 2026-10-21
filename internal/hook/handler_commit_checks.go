package hook

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func init() {
	Register("commit-checks", handleCommitChecks)
}

// maxCheckOutputLines keeps one noisy failure from flooding Claude's context.
const maxCheckOutputLines = 60

// gitOptionsWithValue are git's global options that take the next token as
// their value, so `git -c key=val commit` still finds "commit".
var gitOptionsWithValue = map[string]bool{
	"-C": true, "-c": true, "--git-dir": true, "--work-tree": true,
	"--namespace": true, "--super-prefix": true, "--exec-path": true,
}

// handleCommitChecks runs go test, staticcheck and svcchecker on the changed Go
// packages before Claude runs `git commit`, and blocks the commit if any fail.
// `git commit --no-verify` skips the checks.
//
// Use as a PreToolUse hook with matcher "Bash".
func handleCommitChecks(in *Input) (*Output, error) {
	if in.ToolName != "Bash" {
		return nil, nil
	}
	var bash BashInput
	if err := json.Unmarshal(in.ToolInput, &bash); err != nil {
		return nil, nil
	}
	commitDir, ok := checkedGitCommitDir(bash.Command, in.CWD)
	if !ok {
		return nil, nil
	}

	root, err := gitOutput(commitDir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, nil
	}
	root = strings.TrimSpace(root)

	// Use every changed file, not only staged ones: in `git add x && git commit`
	// the hook runs before the add, so x isn't staged yet.
	status, err := gitOutput(root, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return nil, nil
	}
	packages, services := changedGoPackages(root, changedPaths(status))
	if len(packages) == 0 {
		return nil, nil
	}

	checks := [][]string{
		append([]string{"go", "test"}, packages...),
		append([]string{"staticcheck", "-tags", "integration", "-tests"}, packages...),
	}
	if len(services) > 0 {
		checks = append(checks, append([]string{"svcchecker"}, services...))
	}

	var failures []string
	for _, check := range checks {
		if _, err := exec.LookPath(check[0]); err != nil {
			continue
		}
		cmd := exec.Command(check[0], check[1:]...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err == nil {
			continue
		}
		failures = append(failures, fmt.Sprintf("$ %s\n%s", strings.Join(check, " "), lastLines(string(out), maxCheckOutputLines)))
	}
	if len(failures) == 0 {
		// The review is slow, so run it only after the checks pass.
		if feedback := reviewConstants(root); feedback != "" {
			return nil, fmt.Errorf("blocked: the constants review asked for changes, so the commit did not run. "+
				"Fix these, then commit again:\n\n%s", feedback)
		}
		return nil, nil
	}

	return nil, fmt.Errorf(
		"blocked: commit checks failed, so the commit did not run. Fix the problems below, "+
			"then commit again. If a failure was already there before your change, tell the "+
			"user, and commit with `git commit --no-verify` only if they agree.\n\n%s",
		strings.Join(failures, "\n\n"))
}

// checkedGitCommitDir finds a `git commit` without --no-verify in command and
// returns the folder it runs in, following any earlier `cd`.
func checkedGitCommitDir(command, cwd string) (string, bool) {
	dir := cwd
	for _, seg := range splitSegments(command) {
		cmd, args := commandWord(strings.TrimSpace(seg))
		switch cmd {
		case "cd":
			if len(args) > 0 {
				dir = resolveDir(dir, unquote(args[0]))
			}
		case "git":
			sub, rest := gitSubcommand(args)
			if sub == "commit" && !hasAnyFlag(rest, "--no-verify", "-n") {
				return dir, true
			}
		}
	}
	return "", false
}

// resolveDir returns the folder that `cd path` from dir changes to.
func resolveDir(dir, path string) string {
	switch {
	case path == "~" || strings.HasPrefix(path, "~/"):
		home, _ := os.UserHomeDir()
		return filepath.Join(home, strings.TrimPrefix(path, "~"))
	case filepath.IsAbs(path):
		return path
	default:
		return filepath.Join(dir, path)
	}
}

// gitSubcommand skips git's global options and returns the subcommand and the
// arguments after it.
func gitSubcommand(args []string) (string, []string) {
	for i := 0; i < len(args); i++ {
		arg := unquote(args[i])
		switch {
		case gitOptionsWithValue[arg]:
			i++
		case strings.HasPrefix(arg, "-"):
		default:
			return arg, args[i+1:]
		}
	}
	return "", nil
}

// changedPaths returns the paths in `git status --porcelain=v1` output,
// skipping deleted files.
func changedPaths(status string) []string {
	var paths []string
	for _, line := range strings.Split(status, "\n") {
		if len(line) < 4 || strings.Contains(line[:2], "D") {
			continue
		}
		path := line[3:]
		if _, renamedTo, ok := strings.Cut(path, " -> "); ok {
			path = renamedTo
		}
		paths = append(paths, path)
	}
	return paths
}

// changedGoPackages turns changed file paths into package patterns for go
// test and staticcheck, plus the service patterns for svcchecker.
func changedGoPackages(root string, paths []string) (packages, services []string) {
	packageSet := map[string]bool{}
	serviceSet := map[string]bool{}
	for _, path := range paths {
		if !strings.HasSuffix(path, ".go") || strings.HasPrefix(path, "vendor/") {
			continue
		}
		dir := filepath.Dir(path)
		if _, err := os.Stat(filepath.Join(root, dir)); err != nil {
			continue
		}
		top, _, _ := strings.Cut(dir, "/")
		pattern := "./" + dir
		if strings.Contains(top, ".") && top != "." {
			// Check a whole service (service.foo, cron.bar): one change can break its other tests.
			pattern = "./" + top + "/..."
		}
		packageSet[pattern] = true
		if strings.HasPrefix(top, "service.") {
			serviceSet[pattern] = true
		}
	}
	return sortedKeys(packageSet), sortedKeys(serviceSet)
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func lastLines(text string, n int) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) <= n {
		return strings.Join(lines, "\n")
	}
	return fmt.Sprintf("... (%d lines cut)\n%s", len(lines)-n, strings.Join(lines[len(lines)-n:], "\n"))
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}
