package hook

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSemgrepSwallowedErrorBlocks(t *testing.T) {
	if _, err := exec.LookPath("semgrep"); err != nil {
		t.Skip("semgrep not installed")
	}

	// Write a Go file with a swallowed error
	f, err := os.CreateTemp(t.TempDir(), "bad-*.go")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`package main

import "log/slog"

func doStuff() {
	err := something()
	if err != nil {
		slog.Warn("something failed")
	}
}

func something() error { return nil }
`)
	f.Close()

	input := makeToolInput("PostToolUse", "Write", WriteInput{
		FilePath: f.Name(),
		Content:  "ignored",
	})

	var stdout, stderr bytes.Buffer
	code := Run([]string{"semgrep"}, strings.NewReader(input), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, stderr.String())
	}

	var out Output
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal output: %v\nstdout: %s", err, stdout.String())
	}
	if out.Decision != "block" {
		t.Errorf("decision = %q, want block", out.Decision)
	}
	if !strings.Contains(out.Reason, "swallowed") {
		t.Errorf("reason = %q, want mention of swallowed error", out.Reason)
	}
}

func TestSemgrepClockNowBlocks(t *testing.T) {
	if _, err := exec.LookPath("semgrep"); err != nil {
		t.Skip("semgrep not installed")
	}

	f, err := os.CreateTemp(t.TempDir(), "clock-*.go")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`package main

import "time"

func today() time.Time {
	return time.Now()
}
`)
	f.Close()

	input := makeToolInput("PostToolUse", "Write", WriteInput{
		FilePath: f.Name(),
		Content:  "ignored",
	})

	var stdout, stderr bytes.Buffer
	code := Run([]string{"semgrep"}, strings.NewReader(input), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, stderr.String())
	}

	var out Output
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal output: %v\nstdout: %s", err, stdout.String())
	}
	if out.Decision != "block" {
		t.Errorf("decision = %q, want block", out.Decision)
	}
	if !strings.Contains(out.Reason, "[use-clock-now]") {
		t.Errorf("reason = %q, want mention of use-clock-now", out.Reason)
	}
}

func TestSemgrepSwallowedErrorAllowsCommented(t *testing.T) {
	if _, err := exec.LookPath("semgrep"); err != nil {
		t.Skip("semgrep not installed")
	}

	// A comment above the log call justifies the swallowed error
	f, err := os.CreateTemp(t.TempDir(), "commented-*.go")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`package main

import "log/slog"

func doStuff() {
	err := something()
	if err != nil {
		// best-effort: failure here doesn't affect the caller
		slog.Warn("something failed")
	}
}

func something() error { return nil }
`)
	f.Close()

	input := makeToolInput("PostToolUse", "Write", WriteInput{
		FilePath: f.Name(),
		Content:  "ignored",
	})

	code, stderr := runHandler(t, "semgrep", input)
	if code != 0 {
		t.Errorf("exit code = %d, want 0 (commented swallow is OK); stderr: %s", code, stderr)
	}
}

func TestSemgrepSwallowedErrorAllowsPropagated(t *testing.T) {
	if _, err := exec.LookPath("semgrep"); err != nil {
		t.Skip("semgrep not installed")
	}

	// Write a Go file that properly returns the error
	f, err := os.CreateTemp(t.TempDir(), "good-*.go")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`package main

func doStuff() error {
	err := something()
	if err != nil {
		return err
	}
	return nil
}

func something() error { return nil }
`)
	f.Close()

	input := makeToolInput("PostToolUse", "Write", WriteInput{
		FilePath: f.Name(),
		Content:  "ignored",
	})

	code, stderr := runHandler(t, "semgrep", input)
	if code != 0 {
		t.Errorf("exit code = %d, want 0 (clean file); stderr: %s", code, stderr)
	}
}

func TestSemgrepSwallowedErrorAllowsReturnedWithLoggerCall(t *testing.T) {
	if _, err := exec.LookPath("semgrep"); err != nil {
		t.Skip("semgrep not installed")
	}

	// slog.Params and fmt.Errorf are nested in a return, so the error is not swallowed.
	f, err := os.CreateTemp(t.TempDir(), "returned-*.go")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/monzo/terrors"
)

func augmented(ctx context.Context) (int, error) {
	err := something()
	if err != nil {
		return 0, terrors.Augment(err, "do something", slog.Params(ctx))
	}
	return 1, nil
}

func wrapped() error {
	err := something()
	if err != nil {
		return fmt.Errorf("do something: %w", err)
	}
	return nil
}

func something() error { return nil }
`)
	f.Close()

	input := makeToolInput("PostToolUse", "Write", WriteInput{
		FilePath: f.Name(),
		Content:  "ignored",
	})

	var stdout, stderr bytes.Buffer
	code := Run([]string{"semgrep"}, strings.NewReader(input), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want no findings", stdout.String())
	}
}

const semgrepEditFixture = `package main

import "time"

func today() time.Time {
	return time.Now()
}

func doStuff() {
	err := something()
	if err != nil {
		slog.Warn("something failed")
	}
}

func something() error { return nil }
`

func TestSemgrepEditIgnoresUnchangedLines(t *testing.T) {
	if _, err := exec.LookPath("semgrep"); err != nil {
		t.Skip("semgrep not installed")
	}

	// GIVEN - A file with findings on lines that the edit does not change
	path := filepath.Join(t.TempDir(), "edit.go")
	if err := os.WriteFile(path, []byte(semgrepEditFixture), 0o644); err != nil {
		t.Fatal(err)
	}

	// WHEN - The hook runs for an edit to a clean line
	input := makeToolInput("PostToolUse", "Edit", EditInput{
		FilePath:  path,
		OldString: "func something() error { return errors.New(\"x\") }",
		NewString: "func something() error { return nil }",
	})

	var stdout, stderr bytes.Buffer
	code := Run([]string{"semgrep"}, strings.NewReader(input), &stdout, &stderr)

	// THEN - It reports no findings
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want no findings", stdout.String())
	}
}

func TestSemgrepEditReportsMatchThatOverlapsChange(t *testing.T) {
	if _, err := exec.LookPath("semgrep"); err != nil {
		t.Skip("semgrep not installed")
	}

	// GIVEN - A file where an edit put a log call in an if block
	// ...but the match starts on the if line, above the changed line
	path := filepath.Join(t.TempDir(), "edit.go")
	if err := os.WriteFile(path, []byte(semgrepEditFixture), 0o644); err != nil {
		t.Fatal(err)
	}

	// WHEN - The hook runs for that edit
	input := makeToolInput("PostToolUse", "Edit", EditInput{
		FilePath:  path,
		OldString: "\t\treturn err",
		NewString: "\t\tslog.Warn(\"something failed\")",
	})

	var stdout, stderr bytes.Buffer
	code := Run([]string{"semgrep"}, strings.NewReader(input), &stdout, &stderr)

	// THEN - It reports the swallowed error
	// ...but not the time.Now() on an unchanged line
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, stderr.String())
	}

	var out Output
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal output: %v\nstdout: %s", err, stdout.String())
	}
	if !strings.Contains(out.Reason, "[go-swallowed-error]") {
		t.Errorf("reason = %q, want go-swallowed-error", out.Reason)
	}
	if strings.Contains(out.Reason, "[use-clock-now]") {
		t.Errorf("reason = %q, want no use-clock-now (line not changed)", out.Reason)
	}
}

func TestChangedLineRanges(t *testing.T) {
	// GIVEN - A file where the line "b" occurs two times
	path := filepath.Join(t.TempDir(), "f.go")
	if err := os.WriteFile(path, []byte("a\nb\nc\nd\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		tool      string
		toolInput any
		want      []lineRange
		wholeFile bool
	}{
		{"write", "Write", WriteInput{FilePath: path}, nil, true},
		{"multi-line edit", "Edit", EditInput{FilePath: path, NewString: "c\nd\n"}, []lineRange{{3, 4}}, false},
		{"every match", "Edit", EditInput{FilePath: path, NewString: "b"}, []lineRange{{2, 2}, {5, 5}}, false},
		{"deletion", "Edit", EditInput{FilePath: path, NewString: ""}, nil, false},
		{"multi edit", "MultiEdit", MultiEditInput{FilePath: path, Edits: []EditInput{
			{NewString: "a"}, {NewString: "d"},
		}}, []lineRange{{1, 1}, {4, 4}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// WHEN - We get the changed lines for the tool call
			ti, _ := json.Marshal(tt.toolInput)
			got, wholeFile := changedLineRanges(&Input{ToolName: tt.tool, ToolInput: ti}, path)

			// THEN - They are the lines that the new text is on
			if !reflect.DeepEqual(got, tt.want) || wholeFile != tt.wholeFile {
				t.Errorf("got (%v, %v), want (%v, %v)", got, wholeFile, tt.want, tt.wholeFile)
			}
		})
	}
}
