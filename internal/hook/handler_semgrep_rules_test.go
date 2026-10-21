package hook

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
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
