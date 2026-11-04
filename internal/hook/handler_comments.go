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

// reviewComments returns the comments in the uncommitted changes in root that fail a comment principle, or "" if there
// are none, or if the review can't run.
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
If no comment in scope fails a principle, respond with exactly:
PASS
This is also the answer when no comment matches a principle, and when each comment that matches a principle obeys it.

Otherwise respond with REVISE, then one section for each comment that fails:
REVISE
### <file>: <the first words of the comment>
Principle: <the title of the principle>
Problem: <why the comment does not obey the principle>
Fix: <how to change the comment, with the new text if you can write it>
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

1. Make a list of the comments that are in scope.
2. For each comment, find the principle below whose conditions apply to it. A principle applies only when its conditions are definitely true for that comment. Most comments match no principle. If no principle definitely applies, do nothing with that comment: do not report it, and do not judge it by your own rules.
3. For each comment that matches a principle, use the rule of that principle to check the comment. If the comment obeys the rule, do not report it.
4. Report each comment that does not obey the rule of its principle. Say why it fails, and how to change it.

The diff usually shows enough of the code around each comment. You can use Read, Grep and Glob in the root of the repository if it does not, but use them only for a few quick looks.

Only the principles below are rules. Do not report a comment for a reason that is not in a principle. If you are not sure that a principle applies, or that a comment fails it, do not report the comment.

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

This comment is bad. It describes how the handler works inside (function names, a table, a batch size), and the caller does not need to know these things. It also does not tell the caller about the dry run, commit=true or allow_resend=true. Fix: describe what the RPC does for the caller, and the request fields that change its behaviour.`
