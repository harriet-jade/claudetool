package hook

import (
	"encoding/json"
	"fmt"
	"strings"
)

func init() {
	Register("blank-pr-body", handleBlankPRBody)
}

var prBodyValueFlags = []string{"--body", "-b"}

// prBodyFillFlags fill the PR description from a file, the commits or a template.
var prBodyFillFlags = []string{"--body-file", "-F", "--fill", "--fill-first", "--fill-verbose", "--template", "-T"}

// handleBlankPRBody blocks `gh pr create` and `gh pr edit` commands that set a
// PR description, because the user writes PR descriptions themselves.
//
// Use as a PreToolUse hook with matcher "Bash".
func handleBlankPRBody(in *Input) (*Output, error) {
	if in.ToolName != "Bash" {
		return nil, nil
	}
	var bash BashInput
	if err := json.Unmarshal(in.ToolInput, &bash); err != nil {
		return nil, nil
	}

	for _, seg := range splitSegments(bash.Command) {
		cmd, args := commandWord(strings.TrimSpace(seg))
		if cmd != "gh" || len(args) < 2 || unquote(args[0]) != "pr" {
			continue
		}
		if sub := unquote(args[1]); sub != "create" && sub != "new" && sub != "edit" {
			continue
		}
		if setsPRBody(args[2:]) {
			return nil, fmt.Errorf(
				"blocked: the user writes PR descriptions themselves, so leave the description " +
					"empty. Use `gh pr create --title \"...\" --body \"\"`. Don't use --body-file, " +
					"--fill or --template, and don't set --body on `gh pr edit`.")
		}
	}
	return nil, nil
}

func setsPRBody(args []string) bool {
	if hasAnyFlag(args, prBodyFillFlags...) {
		return true
	}
	for i, arg := range args {
		for _, flag := range prBodyValueFlags {
			if arg == flag && (i+1 >= len(args) || unquote(args[i+1]) != "") {
				return true
			}
			if value, ok := strings.CutPrefix(arg, flag+"="); ok && unquote(value) != "" {
				return true
			}
		}
	}
	return false
}
