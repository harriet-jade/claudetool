package hook

import "strings"

func init() {
	run := semgrepHandler(semgrepRules)
	Register("semgrep", func(in *Input) (*Output, error) {
		// Starting semgrep takes a few seconds, so skip files the Go rules can't match.
		if !strings.HasSuffix(extractFilePath(in), ".go") {
			return nil, nil
		}
		return run(in)
	})
}

const semgrepRules = `rules:
  - id: go-swallowed-error
    patterns:
      - pattern: |
          if err != nil {
              $LOG.$METHOD(...)
          }
      - metavariable-regex:
          metavariable: $LOG
          regex: ^(slog|log|fmt)$
      - pattern-not-regex: "//.*\n"
    languages: [go]
    severity: WARNING
    message: >-
      Error is logged but not propagated. Either return the error
      or add a comment explaining why it is safe to swallow.

  - id: use-clock-now
    patterns:
      - pattern: time.Now()
    message: Use clock.Now(ctx) instead of time.Now() — it enables time mocking in tests.
    languages: [go]
    severity: ERROR

  - id: use-slog-withparam
    patterns:
      - pattern: "slog.WithParams($CTX, map[string]string{$KEY: $VAL})"
    message: Use slog.WithParam($CTX, $KEY, $VAL) for single parameters instead of the map form.
    languages: [go]
    severity: ERROR

  - id: use-params-not-inline-map
    patterns:
      - pattern-either:
          - patterns:
              - pattern: slog.$FUNC(..., map[string]string{...})
              - metavariable-regex:
                  metavariable: $FUNC
                  regex: ^(Trace|Debug|Info|Warn|Error|Critical)$
          - pattern: terrors.$FUNC(..., map[string]string{...})
    message: Pass params via context, not an inline map[string]string. Add them earlier with ctx = slog.WithParams(ctx, ...), then pass slog.Params(ctx) to slog and terrors calls.
    languages: [go]
    severity: ERROR

  - id: use-newmoneywithcurrency-for-zero
    patterns:
      - pattern: decimal.NewMoneyFromInt64MinorUnits(0, $CURRENCY)
    message: Use decimal.NewMoneyWithCurrency($CURRENCY) instead of NewMoneyFromInt64MinorUnits(0, $CURRENCY) to construct a zero-amount Money.
    languages: [go]
    severity: ERROR

  - id: use-timelocation
    patterns:
      - pattern-either:
          - pattern: dates.LocationEuropeLondon()
          - pattern: dates.LocationAmericaNewYork()
          - pattern: dates.LocationAsiaTokyo()
          - pattern: dates.LocationEuropeBerlin()
    message: Use timelocation instead of dates for time zone operations. The timelocation library only blocks start-up of services that actually need timezones.
    languages: [go]
    severity: ERROR

  - id: use-sliceutil-flatmap
    patterns:
      - pattern-either:
          - pattern: |
              var $ACC []$T
              for _, $ITEM := range $SRC {
                  $PARTIAL, $ERR := $CALL
                  if $ERR != nil {
                      return nil, $ERR
                  }
                  $ACC = append($ACC, $PARTIAL...)
              }
              return $ACC, nil
          - pattern: |
              $ACC := []$T{}
              for _, $ITEM := range $SRC {
                  $PARTIAL, $ERR := $CALL
                  if $ERR != nil {
                      return nil, $ERR
                  }
                  $ACC = append($ACC, $PARTIAL...)
              }
              return $ACC, nil
          - pattern: |
              $ACC := make([]$T, 0)
              for _, $ITEM := range $SRC {
                  $PARTIAL, $ERR := $CALL
                  if $ERR != nil {
                      return nil, $ERR
                  }
                  $ACC = append($ACC, $PARTIAL...)
              }
              return $ACC, nil
    message: Use sliceutil.FlatMapReturningFirstError instead of manually looping over a slice and appending — it flattens the per-item slices and returns the first error.
    languages: [go]
    severity: ERROR

  - id: no-slogparams-in-slog-call
    patterns:
      - pattern: slog.$FUNC(..., slog.Params($CTX))
      - metavariable-regex:
          metavariable: $FUNC
          regex: ^(Trace|Debug|Info|Warn|Error|Critical)$
    message: Don't pass slog.Params(ctx) into slog logging calls — ctx params are included automatically. Only pass slog.Params(ctx) to terrors (e.g. terrors.Augment).
    languages: [go]
    severity: ERROR

  - id: use-sliceutil-map
    patterns:
      - pattern-either:
          - pattern: |
              var $ACC []$T
              for _, $ITEM := range $SRC {
                  $ACC = append($ACC, $EXPR)
              }
              return $ACC
          - pattern: |
              $ACC := []$T{}
              for _, $ITEM := range $SRC {
                  $ACC = append($ACC, $EXPR)
              }
              return $ACC
          - pattern: |
              $ACC := make([]$T, 0)
              for _, $ITEM := range $SRC {
                  $ACC = append($ACC, $EXPR)
              }
              return $ACC
    message: Use sliceutil.Map(src, func(item T) R { return ... }) instead of manually looping and appending a single mapped value.
    languages: [go]
    severity: ERROR

  - id: dao-setup-only-in-testmain
    patterns:
      - pattern-either:
          - pattern: dao.Keyspace = gocassa.NewMockKeySpace()
          - pattern: dao.Init()
      - pattern-not-inside: |
          func TestMain(...) {
            ...
          }
    message: Initialise the DAO once in TestMain, not in individual test functions. Set dao.Keyspace = gocassa.NewMockKeySpace() and call dao.Init() inside func TestMain(m *testing.M).
    languages: [go]
    severity: ERROR
    paths:
      include:
        - "*_test.go"

  - id: terror-test-matching
    patterns:
      - pattern: $PKG.$FUNC($T, terrors.Is(...), ...)
    message: You're better off using test.AssertTerrorMatches; test failures will show you the expected and actual values, rather than just failing.
    languages: [go]
    severity: ERROR
    paths:
      include:
        - "*_test.go"
      exclude:
        # libraries/test imports bedrock, so bedrock cannot use test.AssertTerrorMatches without an import cycle
        - "/bedrock/**/*.go"
`
