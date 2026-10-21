package hook

import (
	"strings"
	"testing"
)

func TestBlankPRBodyBlocks(t *testing.T) {
	// GIVEN - gh commands that would give a PR a description.
	commands := []string{
		`gh pr create --title "Fix" --body "Fixes the bug"`,
		`gh pr create --title "Fix" -b "Fixes the bug"`,
		`gh pr create --title "Fix" --body="Fixes the bug"`,
		`gh pr create --title "Fix" --body-file /tmp/body.md`,
		`gh pr create --title "Fix" -F -`,
		`gh pr create --fill`,
		`gh pr create --fill-first`,
		`gh pr create --title "Fix" --template pull_request_template.md`,
		`git push -u origin fix && gh pr create --title "Fix" --body "Fixes the bug"`,
		`gh pr edit 123 --body "Now with a description"`,
		`gh pr new --title "Fix" --body "Fixes the bug"`,
	}
	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			// WHEN - The hook checks the command.
			input := makeToolInput("PreToolUse", "Bash", BashInput{Command: command})
			code, stderr := runHandler(t, "blank-pr-body", input)

			// THEN - The command is blocked, and Claude is told to use an empty --body.
			if code != 2 {
				t.Errorf("exit = %d, want 2 (block)", code)
			}
			if !strings.Contains(stderr, `--body ""`) {
				t.Errorf("stderr = %q, want --body \"\" guidance", stderr)
			}
		})
	}
}

func TestBlankPRBodyAllows(t *testing.T) {
	// GIVEN - gh commands that leave the PR description empty or don't touch it.
	commands := []string{
		`gh pr create --title "Fix" --body ""`,
		`gh pr create --title "Fix" --body ''`,
		`gh pr create --title "Fix" --body=""`,
		`gh pr create --title "Fix" -b ""`,
		`gh pr create --title "Fix" --base main --body "" --draft`,
		`gh pr edit 123 --add-label bug`,
		`gh pr view 123`,
		`gh issue create --title "Bug" --body "Steps to reproduce"`,
	}
	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			// WHEN - The hook checks the command.
			input := makeToolInput("PreToolUse", "Bash", BashInput{Command: command})
			code, stderr := runHandler(t, "blank-pr-body", input)

			// THEN - The command is allowed.
			if code != 0 {
				t.Errorf("exit = %d, want 0 (allow); stderr: %s", code, stderr)
			}
		})
	}
}
