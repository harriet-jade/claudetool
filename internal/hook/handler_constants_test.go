package hook

import "testing"

func TestMayNeedConstantReview(t *testing.T) {
	// GIVEN - Changed code that may or may not need a constants review.
	cases := map[string]bool{
		"const ThreeMinutes = 3 * time.Minute": true,
		"area := 3.14 * r * r":                 true,
		"rsp.Items = rsp.Items[:50]":           true,
		"workers := 10":                        true,
		"if len(items) == 0 {":                 false,
		"i += 1":                               false,
		"return v2.Client{}":                   false,
		"name := firstName + lastName":         false,
	}
	for text, want := range cases {
		t.Run(text, func(t *testing.T) {
			// WHEN - The quick check runs.
			got := mayNeedConstantReview(text)

			// THEN - Only a const or a number other than 0 or 1 starts a review.
			if got != want {
				t.Errorf("mayNeedConstantReview(%q) = %v, want %v", text, got, want)
			}
		})
	}
}

func TestConstantsReviewsTestFiles(t *testing.T) {
	// GIVEN - The constants reviewer, with a stub that records whether it ran.
	called := false
	review := asyncReview{
		name:         "constants",
		fileSuffixes: []string{".go"},
		precheck:     mayNeedConstantReview,
		rubric:       constantsRubric,
		review: func(prompt, model string) (string, error) {
			called = true
			return "PASS", nil
		},
	}

	// WHEN - A test file with a constant is written.
	invoke(t, review, "Write", WriteInput{FilePath: "/src/x_test.go", Content: "const ThreeMinutes = 3 * time.Minute"})

	// THEN - The reviewer runs on it.
	if !called {
		t.Error("reviewer should run on _test.go files")
	}
}
