package hook

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestMayTouchRPCHandler(t *testing.T) {
	// GIVEN - Changes that may or may not touch an RPC handler.
	cases := map[string]bool{
		"+func (h *Handler) Pay(ctx context.Context, req *paymentsproto.PayRequest) (*paymentsproto.PayResponse, error) {": true,
		"diff --git a/service.payments/handler/pay.go b/service.payments/handler/pay.go":                                   true,
		"NEW FILE service.payments/handler/refund.go":                                                                      true,
		"NEW FILE handler/refund.go": true,
		"diff --git a/service.payments/dao/payment.go b/service.payments/dao/payment.go": false,
		"+func ReadPayment(ctx context.Context, id string) (*domain.Payment, error) {":   false,
		"NEW FILE service.payments/handlerutil/retry.go":                                 false,
	}
	for change, want := range cases {
		t.Run(change, func(t *testing.T) {
			// WHEN - The quick check runs.
			got := mayTouchRPCHandler(change)

			// THEN - Only a handler signature or a file in a handler folder starts a review.
			if got != want {
				t.Errorf("mayTouchRPCHandler(%q) = %v, want %v", change, got, want)
			}
		})
	}
}

func TestCommitChecksPassesOnIdempotencyAdvice(t *testing.T) {
	// GIVEN - A git repo with a passing test and a new file in a handler folder...
	root := newGoRepo(t, "if 1+1 != 2 { t.Fatal(\"maths is broken\") }")
	writeRepoFile(t, root, "payments/handler/pay.go", "package handler\n\nfunc Pay() {}\n")
	// ...a constants review that passes, and an idempotency review that finds a risk.
	stubCommitReviewer(t, &constantsReviewer, "PASS")
	prompt := stubCommitReviewer(t, &idempotencyReviewer, "REVISE\n- payments/handler/pay.go: a retry pays twice -> pass an idempotency key")

	// WHEN - Claude tries to commit.
	toolInput, _ := json.Marshal(BashInput{Command: `git commit -m "change"`})
	input, _ := json.Marshal(Input{HookEventName: "PreToolUse", ToolName: "Bash", ToolInput: toolInput, CWD: root})
	var stdout, stderr bytes.Buffer
	code := Run([]string{"commit-checks"}, bytes.NewReader(input), &stdout, &stderr)

	// THEN - The commit is allowed, and Claude gets the reviewer's advice...
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (allow); stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "a retry pays twice") {
		t.Errorf("stdout = %q, want the reviewer’s advice", stdout.String())
	}
	// ...and the reviewer was given the handler, but not the tests.
	if !strings.Contains(*prompt, "NEW FILE payments/handler/pay.go") {
		t.Errorf("prompt does not contain the handler:\n%s", *prompt)
	}
	if strings.Contains(*prompt, "calc_test.go") {
		t.Errorf("prompt contains a test file:\n%s", *prompt)
	}
}
