package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCurlApostrophes(t *testing.T) {
	// GIVEN - Strings with apostrophes, and the result each should have.
	cases := map[string]string{
		`"You're nearly done, don't worry"`: `"You’re nearly done, don’t worry"`,
		`"Monzo's app"`:                     `"Monzo’s app"`,
		`"name = 'x'"`:                      `"name = 'x'"`,
		`"say 'hello'"`:                     `"say 'hello'"`,
		"`It's raw`":                        "`It’s raw`",
	}
	for text, want := range cases {
		t.Run(text, func(t *testing.T) {
			// WHEN - The apostrophes are changed.
			got := curlApostrophes(text)

			// THEN - Only an apostrophe between two letters becomes ’.
			if got != want {
				t.Errorf("curlApostrophes(%s) = %s, want %s", text, got, want)
			}
		})
	}
}

const copySource = `package copy

// We don't change comments.
func Title(err error) (string, error) {
	slog.Info(ctx, "Can't change logs")
	_ = terrors.Augment(err, "won't change terrors", nil)
	_ = fmt.Errorf("isn't changed: %w", err)
	_ = errors.New("doesn't change")
	return "You're all set", nil
}
`

func TestRunChangesOnlyCustomerFacingStrings(t *testing.T) {
	// GIVEN - A Go file with customer copy, a comment, log and error messages.
	path := writeFile(t, "copy.go", copySource)

	// WHEN - The tool runs on the file.
	var stdout, stderr bytes.Buffer
	code := run([]string{path}, &stdout, &stderr)

	// THEN - It succeeds...
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", code, stderr.String())
	}
	// ...only the customer copy gets a curly apostrophe...
	got, _ := os.ReadFile(path)
	want := strings.Replace(copySource, `"You're all set"`, `"You’re all set"`, 1)
	if string(got) != want {
		t.Errorf("file =\n%s\nwant\n%s", got, want)
	}
	// ...and its line is reported.
	if !strings.Contains(stdout.String(), path+": lines 9\n") {
		t.Errorf("stdout = %q, want a report of line 9", stdout.String())
	}
}

func TestRunDryRunDoesNotWrite(t *testing.T) {
	// GIVEN - A Go file with customer copy.
	path := writeFile(t, "copy.go", copySource)

	// WHEN - The tool runs with --dry-run.
	var stdout, stderr bytes.Buffer
	code := run([]string{"--dry-run", path}, &stdout, &stderr)

	// THEN - It reports the line, but the file does not change.
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "would change lines 9") {
		t.Errorf("stdout = %q, want a report of line 9", stdout.String())
	}
	if got, _ := os.ReadFile(path); string(got) != copySource {
		t.Errorf("file changed in a dry run:\n%s", got)
	}
}

func TestRunContinuesAfterABadFile(t *testing.T) {
	// GIVEN - A file that is not Go, and a good Go file.
	notGo := writeFile(t, "notes.txt", "It's text\n")
	good := writeFile(t, "copy.go", copySource)

	// WHEN - The tool runs on both.
	var stdout, stderr bytes.Buffer
	code := run([]string{notGo, good}, &stdout, &stderr)

	// THEN - It reports the bad file and exits 1...
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "notes.txt: not a Go file") {
		t.Errorf("stderr = %q, want an error for notes.txt", stderr.String())
	}
	// ...and still changes the good file.
	if got, _ := os.ReadFile(good); !strings.Contains(string(got), "You’re all set") {
		t.Errorf("good file was not changed:\n%s", got)
	}
}

func TestRunUsage(t *testing.T) {
	cases := map[string]struct {
		args     []string
		wantCode int
		// wantStdout is true if the usage must go to stdout, and false if it must go to stderr.
		wantStdout bool
	}{
		"help":     {args: []string{"--help"}, wantCode: 0, wantStdout: true},
		"no files": {args: nil, wantCode: 2},
		"bad flag": {args: []string{"--bad", "copy.go"}, wantCode: 2},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// WHEN - The tool runs with these arguments.
			var stdout, stderr bytes.Buffer
			code := run(tc.args, &stdout, &stderr)

			// THEN - It exits with the expected code...
			if code != tc.wantCode {
				t.Errorf("exit = %d, want %d", code, tc.wantCode)
			}
			// ...and shows the usage one time, on the expected stream.
			want, other := &stderr, &stdout
			if tc.wantStdout {
				want, other = &stdout, &stderr
			}
			if strings.Count(want.String(), "Usage:") != 1 {
				t.Errorf("want the usage one time, got:\n%s", want.String())
			}
			if strings.Contains(other.String(), "Usage:") {
				t.Errorf("usage is on the wrong stream:\n%s", other.String())
			}
		})
	}
}

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
