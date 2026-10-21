package hook

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// constantsReviewer lets tests replace the real claude call.
var constantsReviewer = runClaudeReviewInRepo

// Skips 0 and 1: they are rarely unexplained values.
var unexplainedNumberRe = regexp.MustCompile(`\b(\d*\.\d+|[2-9]|\d{2,})\b`)

// mayNeedConstantReview is over-eager by design: a borderline hit costs one PASS.
func mayNeedConstantReview(text string) bool {
	return strings.Contains(text, "const") || unexplainedNumberRe.MatchString(text)
}

// reviewConstants returns what to fix in how the uncommitted Go changes in root use constants, or "" if nothing, or if the review can't run.
func reviewConstants(root string) string {
	// Ignore the error: a repo with no commits has no HEAD, and all its files are untracked.
	diff, _ := gitOutput(root, "diff", "HEAD", "--", "*.go")
	untracked, err := gitOutput(root, "ls-files", "--others", "--exclude-standard", "--", "*.go")
	if err != nil {
		return ""
	}

	var change, added strings.Builder
	change.WriteString(diff)
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			added.WriteString(line + "\n")
		}
	}
	for _, path := range strings.Fields(untracked) {
		content, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			continue
		}
		fmt.Fprintf(&change, "\nNEW FILE %s\n%s\n", path, content)
		added.Write(content)
	}
	if !mayNeedConstantReview(added.String()) {
		return ""
	}

	logAsync("constants", "DISPATCH", root)
	out, err := constantsReviewer(buildCommitReviewPrompt(constantsRubric, change.String()), MediumBalanced.model(), root)
	if err != nil {
		logAsync("constants", "ERROR: "+err.Error(), root)
		return ""
	}
	verdict, feedback := parseVerdict(out)
	logAsync("constants", verdict, root)
	if verdict != "REVISE" {
		return ""
	}
	return feedback
}

func buildCommitReviewPrompt(rubric, change string) string {
	return fmt.Sprintf(`%s

OUTPUT PROTOCOL
If the change below is fine by the rubric above, respond with exactly:
PASS

Otherwise respond with:
REVISE
- <one short bullet per issue: file, what is wrong -> what to do>
(nothing after the list)

--- BEGIN CHANGE (git diff, then any new files) ---
%s
--- END CHANGE ---
`, rubric, change)
}

const constantsRubric = `You are reviewing a commit to a Go codebase, given as a git diff. Judge ONLY how the change uses constants: where they are declared, and whether each value should or should not be a named constant. Ignore everything else, and judge only the added or changed lines.

You are running in the root of the repository, and you can use Read, Grep and Glob. Use them only to answer what the diff can't, such as how many places use a constant, or whether other services use a constant in proto/constants.go. This is a fast review, not an in-depth one: do a few quick searches at most, then answer.

WHERE A CONSTANT GOES

- Declare a constant in proto/constants.go only if it is visible outside the service: other services use it, or are meant to use it (for example, a value that is part of the service's API). Otherwise, declare it at the top of the file that uses it.

A VALUE SHOULD BE A NAMED CONSTANT WHEN

- Several places use it and must stay the same: if one changes and the others do not, something breaks.
- It is a setting that could be config but is not yet, such as a concurrency limit of 10 or a batch size.
- The raw value means nothing on its own. A literal 3.14 is bad; the constant Pi is good. A literal 86400 is bad; a name that says what it is, is good.

A VALUE SHOULD NOT BE A NAMED CONSTANT WHEN

- It is already clear by itself. "return 3 * time.Minute" does not need a constant called ThreeMinutes: that name only repeats the value. Error codes and log messages are already descriptive, so keep them inline.
- But if the value has a role, a constant that names the role is good: CreateHandlerTimeout = 3 * time.Minute is correct, because the name says why the value exists.

So flag:

- a constant whose name only repeats its value (ThreeMinutes, Ten, DefaultTwo) -> inline the value, or rename it after its role;
- a constant used once, with no setting-like role and a value that is clear by itself -> inline it;
- an unexplained literal (a number whose meaning is not clear from the code around it) -> give it a named constant;
- a constant in proto/constants.go that only this service uses -> move it to the top of the file that uses it.

If you are not sure, answer PASS.`
