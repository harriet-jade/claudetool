package hook

import (
	"regexp"
	"strings"
)

func init() {
	Register("constants", asyncReview{
		name:         "constants",
		fileSuffixes: []string{".go"},
		tier:         MediumBalanced, // sonnet by default; override with -tier
		precheck:     mayNeedConstantReview,
		rubric:       constantsRubric,
		summary:      "Async review flagged a problem with constants:",
	}.handler())
}

// Matches a number other than 0 or 1, which are rarely unexplained values.
var unexplainedNumberRe = regexp.MustCompile(`\b(\d*\.\d+|[2-9]|\d{2,})\b`)

// mayNeedConstantReview is over-eager by design: a borderline hit costs one PASS.
func mayNeedConstantReview(text string) bool {
	return strings.Contains(text, "const") || unexplainedNumberRe.MatchString(text)
}

const constantsRubric = `You are reviewing a change to a Go file. Judge ONLY how the change uses constants: where they are declared, and whether each value should or should not be a named constant. Ignore everything else.

WHERE A CONSTANT GOES

- Declare a constant in proto/constants.go only if it is visible outside the service: other services use it, or are meant to use it (for example, a value that is part of the service's API). Otherwise, declare it at the top of the file that uses it.
- You can see only this change, not the rest of the codebase. Flag a constant in proto/constants.go only when nothing suggests outside use — for example, a name that clearly describes an internal detail such as a retry count or a page size.

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

Only flag what is clear from the change itself. If you are not sure, answer PASS.`
