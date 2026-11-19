package hook

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func init() {
	Register("one-change-per-pr", handleOneChangePerPR)
}

// oneChangeReviewer lets tests replace the real claude call.
var oneChangeReviewer = runClaudeReviewInRepo

// maxOneChangeDiffLines keeps a large PR from making the prompt too large. The reviewer can read the files to see
// the rest.
const maxOneChangeDiffLines = 3000

// oneChangeDiffExcludes are generated and vendored files. They stay in the diff stat, but their content does not
// help the reviewer to find the logical changes.
var oneChangeDiffExcludes = []string{
	":(exclude)vendor/**", ":(exclude)*.pb.go", ":(exclude)go.sum",
	":(exclude)package-lock.json", ":(exclude)yarn.lock", ":(exclude)pnpm-lock.yaml",
}

// handleOneChangePerPR blocks `gh pr create` when a reviewer finds that the branch has more than one logical change,
// and tells the agent how to split it. It reviews each HEAD only once. If the agent does not agree with the review, it
// can run the same command again, and the PR opens.
//
// Use as a PreToolUse hook with matcher "Bash".
func handleOneChangePerPR(in *Input) (*Output, error) {
	if in.ToolName != "Bash" {
		return nil, nil
	}
	var bash BashInput
	if err := json.Unmarshal(in.ToolInput, &bash); err != nil {
		return nil, nil
	}
	dir, args, ok := prCreateDir(bash.Command, in.CWD)
	if !ok {
		return nil, nil
	}

	root, err := gitOutput(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, nil
	}
	root = strings.TrimSpace(root)
	head, err := gitOutput(root, "rev-parse", "HEAD")
	if err != nil {
		return nil, nil
	}
	head = strings.TrimSpace(head)
	marker, ok := reviewedMarker(root, head)
	if !ok {
		return nil, nil
	}
	if _, err := os.Stat(marker); err == nil {
		return nil, nil
	}

	base, ok := prMergeBase(root, prBaseFlag(args))
	if !ok {
		return nil, nil
	}
	commits, err := gitOutput(root, "log", "--no-merges", "--reverse", "--format=commit %h%n%B", base+"..HEAD")
	if err != nil || strings.TrimSpace(commits) == "" {
		return nil, nil
	}
	stat, err := gitOutput(root, "diff", "--stat=200", base, "HEAD")
	if err != nil {
		return nil, nil
	}
	diff, err := gitOutput(root, append([]string{"diff", base, "HEAD", "--"}, oneChangeDiffExcludes...)...)
	if err != nil {
		return nil, nil
	}

	prompt := buildOneChangePrompt(bash.Command, commits, stat, firstLines(diff, maxOneChangeDiffLines))
	verdict, feedback, ok := runReview("one-change-per-pr", oneChangeReviewer, SlowAccurate.model(), prompt, root)
	if !ok {
		return nil, nil
	}
	// Record the review only when it ran, so that an error does not let the next attempt through without a review.
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err == nil {
		_ = os.WriteFile(marker, nil, 0o644) // If the write fails, the next attempt gets a new review. That is safe.
	}
	if verdict != "REVISE" {
		return nil, nil
	}
	return nil, fmt.Errorf(
		"blocked: the PR was not created. A reviewer found more than one logical change on this branch. "+
			"A PR must have one logical change.\n\n%s\n\n"+
			"Split the branch: make a new branch from the base for each change, and move the commits or files "+
			"of each change to its branch (for example with `git cherry-pick` or `git checkout <branch> -- <file>`). "+
			"Then open a PR for each branch, in the order above. Interactive rebase is not available.\n\n"+
			"If you are sure that the reviewer is wrong, run the same command again. The review runs only once "+
			"for each HEAD, so the PR then opens. Do not do this only to save time.",
		feedback)
}

// prCreateDir finds a `gh pr create` in command and returns the folder it runs in, following any earlier `cd`, and
// the arguments after `create`.
func prCreateDir(command, cwd string) (string, []string, bool) {
	dir := cwd
	for _, seg := range splitSegments(command) {
		cmd, args := commandWord(strings.TrimSpace(seg))
		switch {
		case cmd == "cd":
			if len(args) > 0 {
				dir = resolveDir(dir, unquote(args[0]))
			}
		case cmd == "gh" && len(args) >= 2 && unquote(args[0]) == "pr":
			if sub := unquote(args[1]); sub != "create" && sub != "new" {
				continue
			}
			if hasAnyFlag(args[2:], "--help", "-h", "--dry-run") {
				return "", nil, false
			}
			return dir, args[2:], true
		}
	}
	return "", nil, false
}

// prBaseFlag returns the value of --base or -B in the `gh pr create` arguments, or "" if it is not set.
func prBaseFlag(args []string) string {
	for i, arg := range args {
		arg = unquote(arg)
		for _, flag := range []string{"--base", "-B"} {
			if arg == flag && i+1 < len(args) {
				return unquote(args[i+1])
			}
			if value, ok := strings.CutPrefix(arg, flag+"="); ok {
				return unquote(value)
			}
		}
	}
	return ""
}

// prMergeBase returns the commit where HEAD left the PR's base branch. With no base, it uses the default branch of
// origin. It uses the remote branch when there is one, because the local branch can be out of date.
func prMergeBase(root, base string) (string, bool) {
	var candidates []string
	if base != "" {
		candidates = []string{"origin/" + base, base}
	} else {
		if ref, err := gitOutput(root, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
			candidates = append(candidates, strings.TrimSpace(ref))
		}
		candidates = append(candidates, "origin/master", "origin/main", "master", "main")
	}
	for _, ref := range candidates {
		if _, err := gitOutput(root, "rev-parse", "-q", "--verify", ref+"^{commit}"); err != nil {
			continue
		}
		mergeBase, err := gitOutput(root, "merge-base", ref, "HEAD")
		if err != nil {
			return "", false
		}
		return strings.TrimSpace(mergeBase), true
	}
	return "", false
}

// reviewedMarker returns the file that records that head had a review. It is in the git folder of the worktree, so
// it goes when the worktree goes.
func reviewedMarker(root, head string) (string, bool) {
	path, err := gitOutput(root, "rev-parse", "--git-path", "claudetool/one-change-per-pr/"+head)
	if err != nil {
		return "", false
	}
	path = strings.TrimSpace(path)
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	return path, true
}

func firstLines(text string, n int) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) <= n {
		return strings.Join(lines, "\n")
	}
	return fmt.Sprintf("%s\n... (%d lines cut: read the files to see the rest)", strings.Join(lines[:n], "\n"), len(lines)-n)
}

func buildOneChangePrompt(command, commits, stat, diff string) string {
	return fmt.Sprintf(`%s

OUTPUT PROTOCOL
If the PR is one logical change, or if you are not sure, respond with exactly:
PASS

Otherwise respond with:
REVISE
1. <one sentence that says what the first change does>
   Files: <the files of this change>
2. <the same for each other change, in the order to merge them>
Why: <one sentence that says why the changes are independent>
(nothing after this)

--- BEGIN COMMAND THAT OPENS THE PR ---
%s
--- END COMMAND ---

--- BEGIN COMMITS (oldest first) ---
%s
--- END COMMITS ---

--- BEGIN DIFF STAT ---
%s
--- END DIFF STAT ---

--- BEGIN DIFF (generated and vendored files are not included) ---
%s
--- END DIFF ---
`, oneChangeRubric, command, strings.TrimSpace(commits), strings.TrimSpace(stat), diff)
}

const oneChangeRubric = `You are reviewing a branch before it becomes a pull request (PR). Judge ONLY one thing: does the PR contain one logical change, or several logical changes that must be separate PRs? Ignore the quality of the code.

The PR description is empty by design. Use the PR title in the command, the commit messages and the diff to find what the change is for.

You are running in the root of the repository, and you can use Read, Grep and Glob. Use them only when the diff does not show if two parts depend on each other. Do a few quick searches at most.

THE BAR IS HIGH. Most PRs are one logical change, and the normal answer is PASS. A request to split a PR costs the author a lot of work, so ask for it only when the split is clearly correct. A large PR, a PR with many files, or a PR in several folders or services can still be one logical change. Size is never a reason to split.

A PR IS ONE LOGICAL CHANGE WHEN you can say what it does in one sentence, and each part of the diff is necessary for that sentence. All of these are part of one change:
- the code, its tests, its docs, generated code, config, and migrations that it needs;
- a refactor, rename or move that makes the change possible, even a large one;
- a dependency upgrade, and the code changes that the upgrade needs;
- one mechanical change across many files, such as a rename or an API migration;
- changes in several services that together make one feature or fix;
- small fixes and cleanups in or near the code that the change touches.

ASK FOR A SPLIT ONLY WHEN ALL OF THESE ARE TRUE:
1. The PR has two or more parts, and you can say what each part does in its own sentence.
2. The parts are independent: each part works and makes sense if it merges alone, and no part needs another part.
3. Each part is substantial: a reviewer would want to review it separately. A few lines of unrelated cleanup is not substantial.
4. A reviewer who reads the PR title would be surprised to find one of the parts.

Examples that need a split:
- a new feature in one service, and a fix for an unrelated bug in a different service;
- a behaviour change, and a large refactor of an area that the change does not use;
- two features that do not share code.

Examples that do not need a split:
- a new RPC, with its proto change, handler, tests, and the client that calls it;
- a fix, and the refactor that the fix needs;
- a feature, and a small typo fix in a file that the feature also changes.

If you are not sure, answer PASS.`
