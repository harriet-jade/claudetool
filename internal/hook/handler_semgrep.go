package hook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// semgrepHandler returns a Handler that runs semgrep with the given YAML rule
// against files written/edited by Claude. Use as a PostToolUse hook.
func semgrepHandler(ruleYAML string) Handler {
	return func(in *Input) (*Output, error) {
		filePath := extractFilePath(in)
		if filePath == "" {
			return nil, nil
		}

		changed, wholeFile := changedLineRanges(in, filePath)
		if !wholeFile && len(changed) == 0 {
			return nil, nil
		}

		results, err := runSemgrep(ruleYAML, filePath)
		if err != nil {
			// semgrep not installed or failed; don't block
			return nil, nil
		}
		// Findings in code the agent did not touch are noise, so drop them.
		if !wholeFile {
			results = filterResults(results, changed)
		}
		if len(results) == 0 {
			return nil, nil
		}

		return &Output{
			Decision: "block",
			Reason:   fmt.Sprintf("semgrep found issues in %s:\n%s", filePath, formatSemgrepResults(results)),
		}, nil
	}
}

func extractFilePath(in *Input) string {
	if len(in.ToolInput) == 0 {
		return ""
	}
	var obj struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(in.ToolInput, &obj); err != nil {
		return ""
	}
	return obj.FilePath
}

// lineRange is an inclusive range of 1-based line numbers.
type lineRange struct {
	start, end int
}

func (r lineRange) overlaps(o lineRange) bool {
	return r.start <= o.end && o.start <= r.end
}

// changedLineRanges returns the lines of filePath that an Edit or MultiEdit
// wrote. wholeFile is true when the whole file counts as changed.
func changedLineRanges(in *Input, filePath string) (ranges []lineRange, wholeFile bool) {
	var newStrings []string
	switch in.ToolName {
	case "Edit":
		var e EditInput
		if err := json.Unmarshal(in.ToolInput, &e); err != nil {
			return nil, true
		}
		newStrings = []string{e.NewString}
	case "MultiEdit":
		var m MultiEditInput
		if err := json.Unmarshal(in.ToolInput, &m); err != nil {
			return nil, true
		}
		for _, e := range m.Edits {
			newStrings = append(newStrings, e.NewString)
		}
	default:
		// A Write replaces the whole file.
		return nil, true
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, true
	}
	content := string(data)

	for _, s := range newStrings {
		// A deletion leaves no new lines to check.
		if s == "" {
			continue
		}
		height := strings.Count(strings.TrimSuffix(s, "\n"), "\n")
		// Mark every match, because we cannot tell which one the edit wrote.
		for offset := 0; ; {
			i := strings.Index(content[offset:], s)
			if i < 0 {
				break
			}
			start := strings.Count(content[:offset+i], "\n") + 1
			ranges = append(ranges, lineRange{start, start + height})
			offset += i + len(s)
		}
	}
	return ranges, false
}

type semgrepResult struct {
	CheckID string `json:"check_id"`
	Path    string `json:"path"`
	Start   struct {
		Line int `json:"line"`
	} `json:"start"`
	End struct {
		Line int `json:"line"`
	} `json:"end"`
	Extra struct {
		Message string `json:"message"`
	} `json:"extra"`
}

// filterResults keeps the results whose lines overlap a changed range.
func filterResults(results []semgrepResult, changed []lineRange) []semgrepResult {
	var kept []semgrepResult
	for _, r := range results {
		span := lineRange{r.Start.Line, r.End.Line}
		for _, c := range changed {
			// Not the start line alone: an if block can start above the changed line.
			if span.overlaps(c) {
				kept = append(kept, r)
				break
			}
		}
	}
	return kept
}

func runSemgrep(ruleYAML, filePath string) ([]semgrepResult, error) {
	f, err := os.CreateTemp("", "semgrep-rule-*.yml")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())

	if _, err := f.WriteString(ruleYAML); err != nil {
		f.Close()
		return nil, err
	}
	f.Close()

	cmd := exec.Command("semgrep", "scan", "--quiet", "--json", "--config", f.Name(), filePath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return parseSemgrepOutput(stdout.Bytes())
}

func parseSemgrepOutput(data []byte) ([]semgrepResult, error) {
	var result struct {
		Results []semgrepResult `json:"results"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result.Results, nil
}

func formatSemgrepResults(results []semgrepResult) string {
	var b strings.Builder
	for _, r := range results {
		// semgrep prefixes each rule id with the dotted path of the temp config file.
		ruleID := r.CheckID[strings.LastIndex(r.CheckID, ".")+1:]
		fmt.Fprintf(&b, "  %s:%d: [%s] %s\n", r.Path, r.Start.Line, ruleID, r.Extra.Message)
	}
	return b.String()
}
