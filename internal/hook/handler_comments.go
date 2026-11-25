package hook

import (
	"fmt"
	"strings"
)

// commentsReviewer lets tests replace the real claude call.
var commentsReviewer = runClaudeReviewInRepo

var commentPathspecs = []string{
	"*.proto",
	"*.go",
	"*.js", "*.jsx", "*.mjs", "*.cjs",
	"*.ts", "*.tsx", "*.mts", "*.cts",
	"*.py", "*.pyi",
	"*.sh", "*.bash", "*.zsh",
	"*.sql",
	// Generated files copy the proto comments. The proto file is the source.
	":(exclude)*.pb.go",
}

// containsComment is over-eager by design: a borderline hit, such as a `//` in a string, costs one PASS.
func containsComment(text string) bool {
	// `--` must start the line, because `cmd --flag` is not a comment.
	leading := []string{"//", "/*", "#", "--", `"""`, "'''"}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		for _, marker := range leading {
			if strings.HasPrefix(trimmed, marker) {
				return true
			}
		}
		// The space before the marker skips the `://` in a URL.
		if strings.Contains(line, " //") || strings.Contains(line, " #") {
			return true
		}
	}
	return false
}

// reviewComments returns the comments in the uncommitted changes in root that are not allowed, or "" if there are none,
// or if the review can't run. A comment is allowed only if a principle permits it.
func reviewComments(root string) string {
	change, added, ok := uncommittedChange(root, commentPathspecs...)
	if !ok || !containsComment(added) {
		return ""
	}
	return runCommitReview("comments", commentsReviewer, buildCommentsReviewPrompt(change), root)
}

func buildCommentsReviewPrompt(change string) string {
	return fmt.Sprintf(`%s

OUTPUT PROTOCOL
If each comment in scope is allowed, respond with exactly:
PASS
This is the answer only when there are no comments in scope, or when each comment in scope is a directive, or matches a principle and obeys its rule.

Otherwise respond with REVISE, then one section for each comment that is not allowed:
REVISE
### <file>: <the first words of the comment>
Principle: <the title of the principle>
Problem: <why the comment is not allowed>
Fix: <"Remove the comment." under "No comments". Under another principle, how to change the comment, with the new text if you can write it>
(nothing after the last section)

--- BEGIN CHANGE (git diff, then any new files) ---
%s
--- END CHANGE ---
`, commentsRubric, change)
}

const commentsRubric = `You are reviewing the comments in a commit, given as a git diff, then the full text of each new file. Judge ONLY the comments. Ignore the code.

WHICH COMMENTS ARE IN SCOPE

- Review only the comments that this change adds or changes: comments on the lines that start with "+" in the diff, and all comments in a new file.
- A comment on an unchanged line (a diff line that starts with a space) or on a removed line (a line that starts with "-") was there before this change. It is out of scope. Do not review it, also if it does not obey a principle.

HOW TO REVIEW

Comments are NOT allowed. A comment can stay only when one of the principles below applies to it, and the comment obeys the rule of that principle. The last principle, "No comments", applies to each other comment, and it says that the comment must be removed.

1. Make a list of the comments that are in scope.
2. For each comment, find the principle below whose conditions apply to it. A principle applies only when its conditions are definitely true for that comment. If no other principle definitely applies, "No comments" applies.
3. Use the rule of that principle to check the comment. If the comment obeys the rule, do not report it.
4. Report each comment that does not obey the rule of its principle. Under "No comments", this is each comment. Report it also when it is accurate, short or useful. Do not suggest a better comment, and do not suggest that the agent can keep it.

The diff usually shows enough of the code around each comment. You can use Read, Grep and Glob in the root of the repository if it does not, but use them only for a few quick looks.

DIRECTIVES ARE NOT COMMENTS

Some lines use the syntax of a comment, but they are instructions to a tool. They are not in scope. Do not report them. For example: a shebang ("#!/bin/bash"), "//go:build", "//go:generate", "//go:embed", "// +build", "//nolint", "# noqa", "# type: ignore", "# shellcheck disable=", "// eslint-disable", "// @ts-ignore", "// @ts-expect-error", "// prettier-ignore", and "// Code generated ... DO NOT EDIT.". If you are not sure that a line is a directive, it is a comment.

PRINCIPLES

### RPC comments

Applies when: the comment is directly above an "rpc" definition in a .proto file.

Rule: The comment is for the people who call the RPC. It must tell them how to use the RPC, and the important things that they must know before they call it. For example: what the RPC does, what it does by default, the request fields that change what it does, and when it is not safe to call it again. Do not describe how the RPC works inside, unless the caller must know this to use the RPC correctly. The comment can be several lines long, if each line has a purpose. It can use full sentences: it does not need short, clipped ones.

Example of a good comment:

    // ProcessInformationRequest downloads a Phase 1 information request file from TGSS via the EDITRAN proxy,
    // it processes the file by resolving debtors and their accounts, then it builds the Phase 2 response file and
    // sends it back to TGSS via the EDITRAN proxy. By default the handler performs a dry run, use commit=true to
    // perform the actual upload to TGSS. If you’re resending a file that was already sent, you must pass
    // allow_resend=true to override the default idempotency protection.
    rpc ProcessInformationRequest(ProcessInformationRequestRequest) returns (ProcessInformationRequestResponse) {
        option (handler).path = "/process/information-request";
        option (handler).method = "POST";
        option (humans_who_can_rpc) = CONTRIBUTORS_WITH_APPROVAL;
    }

This comment is good. It says what the RPC does from the point of view of the caller, it says that the default is a dry run and which field changes this, and it says how to send a file again. It is five lines long, and each line has a purpose.

Example of a bad comment, on the same RPC:

    // ProcessInformationRequest calls downloadFile, then for each row it calls resolveDebtor and
    // writes the result to the information_requests table in batches of 50.

This comment is bad. It describes how the handler works inside (function names, a table, a batch size), and the caller does not need to know these things. It also does not tell the caller about the dry run, commit=true or allow_resend=true. Fix: describe what the RPC does for the caller, and the request fields that change its behaviour.

### No comments

Applies when: no other principle above applies to the comment. This includes doc comments on functions, types and fields, comments in a function body, docstrings, and comments in SQL and shell scripts.

Rule: The comment is not allowed. Remove it. If the code is not clear without the comment, make the code clearer: for example, use better names, or move the code into a function whose name says what it does. This is a firm rule, not a preference. The agent must obey it.

Example of a bad comment:

    // SignaturesOverridden reports whether an engineer accepted the order’s signatures although they failed validation.
    func (o *Order) SignaturesOverridden() bool {
        return !o.SignatureOverriddenAt.IsZero()
    }

This comment is not allowed, because no other principle applies to it. Fix: remove the comment.`
