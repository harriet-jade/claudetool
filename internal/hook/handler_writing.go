package hook

import (
	"fmt"
	"strings"
)

// writingReviewer lets tests replace the real claude call. The reviewer gets no tools, because the diff holds all the
// text that it reviews.
var writingReviewer = func(prompt, model, _ string) (string, error) {
	return runClaudeReview(prompt, model)
}

// writingDocPathspecs are the documents that the writing review reads in full. In code files, it reads only the
// comments, from commentPathspecs.
var writingDocPathspecs = []string{
	"*.md", "*.mdx", "*.markdown", "*.rst",
	// Vendored documents were not written in this repo.
	":(exclude)vendor/*", ":(exclude)*/vendor/*",
	":(exclude)node_modules/*", ":(exclude)*/node_modules/*",
}

// reviewWriting returns advice on the writing in the comments and documents that the uncommitted changes in root
// add, or "" if there is none, or if the review can't run.
func reviewWriting(root string) string {
	docs, docsAdded, ok := uncommittedChange(root, writingDocPathspecs...)
	if !ok {
		return ""
	}
	code, codeAdded, ok := uncommittedChange(root, commentPathspecs...)
	if !ok {
		return ""
	}

	var change strings.Builder
	if strings.TrimSpace(docsAdded) != "" {
		change.WriteString(docs + "\n")
	}
	if containsComment(codeAdded) {
		change.WriteString(code + "\n")
	}
	if change.Len() == 0 {
		return ""
	}
	return runCommitReview("writing", writingReviewer, buildWritingReviewPrompt(change.String()), root)
}

func buildWritingReviewPrompt(change string) string {
	return fmt.Sprintf(`%s

OUTPUT PROTOCOL
If no text in scope is a very clear violation of a guideline, respond with exactly:
PASS

Otherwise respond with REVISE, then one section for each file that has text to improve. Give all the feedback for the commit in this one answer:
REVISE
### <file>
- "<the text, quoted exactly; for a long text, its first words>"
  Guideline: <the number and title of the guideline>
  Problem: <why the text is hard to read or understand>
  Suggestion: <the new text>
(nothing after the last section)

--- BEGIN CHANGE (git diff, then any new files) ---
%s
--- END CHANGE ---
`, writingRubric, change)
}

// writingRubric comes from Monzo's "Write like Monzo" guidance:
// https://www.notion.so/monzo/WriteLikeMonzo-32969fe426d28073bce0dc6edfb0e5b9
// The guidance is for all Monzo writing. This rubric keeps the parts that apply to writing for staff.
const writingRubric = `You are reviewing the writing in a commit, given as a git diff, then the full text of each new file. This writing is for engineers and other staff, not for customers: code comments, READMEs, runbooks and other documents. Judge ONLY how the text is written. Ignore the code, and do not check if the text is correct about the code.

The diff gives you all the text that you need. Do not use tools.

WHICH TEXT IS IN SCOPE

- Review only the text that this change adds or changes: the lines that start with "+" in the diff, and all of a new file. A line that starts with a space or with "-" was there before this change. Do not review it.
- In a code file, review only the comments. Do not review strings, log messages or error messages.
- In a document, review the prose: paragraphs, headings, list items and the text in tables.
- Do not review:
  - text for customers, such as app copy, help articles, emails and notifications. It has different rules.
  - error messages, also when a document quotes one. They have different rules.
  - code, commands, file paths, config, log output, and all text in code blocks or backticks.
  - names of functions, fields, services, teams, tools and products.
  - generated text, licence text, and text copied from another source.

THE BAR FOR FEEDBACK

Your default answer is PASS. Report a text only when it is a very clear violation of a guideline below: a reader must stop, read it again, or guess what it means, and the fix is obvious. If the writing is close enough, do not report it. Do not report a text because you would write it differently. If a text has only small problems, it passes. Most commits get PASS. If you are not sure, answer PASS.

The readers are engineers. A technical term that a Monzo engineer knows (RPC, proto, idempotency, retry, Cassandra, Kafka) is not jargon. Short, clipped text is normal in a code comment.

Do not report:
- text that does not use contractions. The authors follow a style guide that does not use them.
- British or American spelling.
- the length of a sentence, unless the sentence is so long (more than about 35 words) that it is hard to follow.
- one formal word by itself. Report formal words only when the text sounds like a corporate document.
- formatting choices, such as bullets or paragraphs, unless a long block of text is hard to scan.
- text that is missing. Review only the text that is there.

THE GUIDELINES

1. Start with the reader
Put the most important information first: what the reader must know or do. A runbook says what the alert means and what to do before it gives background. A README says what the thing is and how to use it before it gives history. Keep it easy to scan: use headings, short paragraphs and lists.
Clear violation: a runbook step that the reader must do first is at the end of a long paragraph of background.

2. Be clear, simple and human
Use plain English and everyday words. Write like you speak: natural, not corporate. Keep sentences short, about 15 to 20 words. If it would not sound normal out loud, rewrite it.
Clear violation: "Utilisation of the aforementioned endpoint is contingent upon prior authentication." -> "Log in before you call this endpoint."

3. Be kind and inclusive
Use language that everyone can understand. Do not use idioms or cultural references that can exclude people, such as sports or film references. Do not blame a person in a runbook or incident note: say what happened. Do not joke about customers, money problems, fraud or incidents.
Clear violation: "If the job dies, it’s a Hail Mary: just YOLO a restart."

4. Make complex things simple
If a term is not one that most engineers know, such as a team’s own name for a process or an acronym that is not common, explain it the first time. Do not assume knowledge that a new engineer on the team does not have. Break a long procedure into numbered steps.
Clear violation: a runbook that gives six actions in one paragraph, with no steps.

5. Be transparent and take responsibility
Use the active voice: say who or what does the action. Do not hide behind vague or passive language. Be direct, also when the news is bad: say what does not work.
Clear violation: "Reconciliation may be performed at some point if required." -> "The reconciliation cron runs each night. Run it by hand if a payment is missing."

6. Keep it natural, not corporate
Use normal words, not formal ones: need, not require; give, not provide; help, not assist; use, not utilise; but, not however; start, not commence. Do not use clichés, filler phrases or marketing words, such as "leverage", "seamless", "robust", "best-in-class", "please note that" and "it is worth noting that". Do not try to sound professional by being complicated.
Clear violation: "This service leverages a robust, best-in-class pipeline to seamlessly facilitate payouts." -> "This service sends payouts."

7. Make every word count
Remove words that do nothing. Do not say the same thing twice. Use specific words, not vague ones such as "various", "some things", "stuff" or "soon".
Clear violation: a paragraph that says the same thing three times in different words.

Before you report a text, ask: is it clear at a glance? Does it sound like a real person? Could it be much simpler? Report it only if the answer shows a very clear problem.`
