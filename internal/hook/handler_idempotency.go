package hook

import "regexp"

// idempotencyReviewer lets tests replace the real claude call.
var idempotencyReviewer = runClaudeReviewInRepo

// rpcHandlerRe matches the signature of a Monzo RPC handler, such as
// (ctx context.Context, req *fooproto.BarRequest) (*fooproto.BarResponse, error).
var rpcHandlerRe = regexp.MustCompile(`\(ctx context\.Context, req \*[\w.]+Request\) \(\*[\w.]+Response, error\)`)

// handlerFolderRe matches a changed file in a handler package. It finds a change to the body of a handler, because a
// diff hunk does not always show the signature, and a change to a helper of a handler, because helpers live there too.
var handlerFolderRe = regexp.MustCompile(`(?m)^(diff --git a/|NEW FILE )(\S*/)?handler/`)

// mayTouchRPCHandler is over-eager by design: a borderline hit costs one PASS.
func mayTouchRPCHandler(change string) bool {
	return rpcHandlerRe.MatchString(change) || handlerFolderRe.MatchString(change)
}

// reviewIdempotency returns the retry risks in the RPC handlers that the uncommitted Go changes in root add or change,
// or "" if there are none, or if the review can't run.
func reviewIdempotency(root string) string {
	// Tests don't run on a retry, and they make the change much larger.
	change, _, ok := uncommittedChange(root, "*.go", ":(exclude)*_test.go")
	if !ok || !mayTouchRPCHandler(change) {
		return ""
	}
	return runCommitReview("idempotency", idempotencyReviewer, idempotencyRubric, change, root)
}

const idempotencyRubric = `You are reviewing a commit to Monzo’s Go monorepo, given as a git diff. Judge ONLY whether the RPC handlers that the change adds or changes are safe to retry (idempotent). Ignore everything else, such as style, naming, tests and performance.

You are running in the root of the repository, and you can use Read, Grep and Glob. Use them to follow the handler into the helpers, DAO functions and downstream RPCs that the diff does not show. This is a focused review, not an audit: read only what you need to trace the changed handlers, use about 10 tool calls at most, then answer.

WHAT AN RPC HANDLER IS

- A method on the service’s Handler type, usually in the service’s handler/ package, with this shape:
  func (h *Handler) DoThing(ctx context.Context, req *<service>proto.DoThingRequest) (*<service>proto.DoThingResponse, error)
- The change touches a handler if it adds or changes a handler, or a function that a handler calls to do its work. A read-only handler (Read, List, Get, a dry run) has no side effects, so it is always safe: PASS.

WHY EVERY HANDLER MUST BE SAFE TO RETRY

- A Monzo handler can run more than one time for the same request. The client retries on an error or a timeout, even when the first attempt completed and only the response was lost. Consumers get events again, crons run again, and operators run a stuck request again.
- So every handler must give the same result when it runs again with the same request, at any point where the last attempt stopped.

HOW TO REVIEW

1. Find each handler that the change adds or changes. Read it from start to end, and follow the calls that have side effects.
2. Write down, in order, each step that changes something: a database write, a call to another service, an event, a lock, a call to an external party.
3. For each step, ask: "If the handler fails right after this step, and the client sends the same request again, what does the second attempt do?" Also ask what happens if two attempts run at the same time.
4. Flag a step only if the second attempt causes a dangerous duplicate, loses a side effect, or can never complete.

SIDE EFFECTS THAT ARE SAFE TO REPEAT (do NOT flag)

- Publishing an event again. Monzo consumers must accept duplicate events, and we prefer to publish again on a retry rather than risk losing the event.
- Writing the same content to the same database row again.
- Reads.
- A call to a downstream service with an idempotency key that comes from the request, or from stored state, so it is the same on every attempt (for example "settle:" + order ID).

DANGEROUS SIDE EFFECTS (flag these when a retry can repeat them)

- Moving money twice: a payment, transfer, ledger entry, settle, sweep, refund or fee, with no stable idempotency key.
- Contacting the customer twice: a push notification, email, SMS, in-app alert or letter.
- Creating a second record or entity where there must be one, for example an insert that uses a new random ID on each attempt.
- Repeating a change that is not the same each time: incrementing a counter, adding to a balance or a list, or a call to an external party with no idempotency.
- An idempotency key that changes between attempts, because it uses a random ID, a UUID or the current time. Such a key gives no protection.

OTHER RETRY RISKS (flag these too)

- Lost side effect: the handler saves the "done" state before the side effect. If the side effect then fails, the retry sees "done", skips it, and the side effect never happens.
- Stuck request: the handler moves a record to a new state, then fails. The retry rejects the new state (for example, with a precondition error), so the request can never complete.
- Race: two attempts at the same time can both pass a "has this already happened?" check, and both do the side effect. A distributed lock (distsync) or a conditional write prevents this.

So flag a handler only when you can name the step, the point of failure and what the retry does wrong. Say what to do, for example: pass an idempotency key that comes from the request, save the state after the side effect, accept the later states on a retry, or take a lock.

Be confident. If the risk depends on a downstream service that you could not check, or you are not sure, answer PASS.`
