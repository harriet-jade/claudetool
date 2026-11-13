# claudetool

My Claude Code guardrails. It is one Go binary. Claude Code runs it as a hook
before and after the agent's tool calls, in every repo I work in. It blocks
shell patterns I don't want, checks code before a commit, and runs background
reviewers that push back on code I would push back on in review.

This is a fork of [laurenkt/claudetool](https://github.com/laurenkt/claudetool).
I treat it as my own codebase. I don't send changes upstream.

## How it fits together

```
~/src/github.com/harriet-jade/claudetool   source (this repo)
        │  go install
        ▼
~/bin/claudetool                           the binary
        │  referenced by
        ▼
~/.claude/settings.json                    global hooks, so they apply in every repo
        │  Claude Code runs the hooks around each tool call
        ▼
Claude agents in wearedev, worktrees and other repos
```

Claude Code gives each hook the tool call as JSON on stdin (tool name, input,
working directory). The hook's exit code decides what happens:

| Exit | Effect |
|---|---|
| `0` | Continue. Any JSON on stdout can add context for the agent. |
| `2` | Before a tool: the tool does not run. The text on stderr goes to the agent as the reason. |

Hooks with `asyncRewake: true` run in the background. The agent does not wait
for them. They wake the agent only when they exit 2.

## Build and update

```sh
go -C ~/src/github.com/harriet-jade/claudetool test ./...
go -C ~/src/github.com/harriet-jade/claudetool install . ./cmd/...
```

The binaries go to `GOBIN` (`~/bin`). Hooks use the new binary at once, also in
running sessions.

## Manual tools

These are separate binaries in `cmd/`. No hook runs them. I run them myself.

### `curly-apostrophes`

```sh
curly-apostrophes [--dry-run] FILE...
```

It changes straight apostrophes to `’` in the customer-facing strings of the
Go files that you give it, and edits the files in place. In each string
literal, a `'` with a letter on both sides becomes `’` (`don't` → `don’t`,
`Monzo's` → `Monzo’s`). It changes all the strings in the file. It does not
change comments, generated files, or messages passed to `slog`, `terrors`,
`errors.New` or `fmt.Errorf`. It cannot know if a string is really for
customers, so read the result before you commit it. `--dry-run` shows the lines
that would change, and does not write the files. `curly-apostrophes --help`
shows the full usage, the output format and the exit codes.

## Rules and enforcements

### 1. Before each Bash command — guards

These block a command and tell the agent the better form.

| Handler | Blocks | Use instead |
|---|---|---|
| `no-git-c` | `git -C <path>` | plain `git ...` |
| `no-shell-loops` | `for` / `while` / `until` loops | one command per call, or the Read/Grep tools |
| `no-inline-python` | `python -c '...'` | `jq`, or the Read/Grep tools |
| `no-inline-file` | heredocs and `$(cat/printf/echo ...)` | write a file, then `-F file` / `--body-file file` |
| `no-exit-code-check` | `$?` checks | run the command and read its output |
| `no-shared-pr-body` | the shared `/tmp/pr-body.md` path | a unique file |
| `blank-pr-body` | `gh pr create/new/edit` that sets a description (`--body "..."`, `--body-file`, `--fill*`, `--template`) | `--body ""` — I write PR descriptions myself |
| `use-linear-mcp` | `gh`/`curl`/`wget`/`linear-cli` calls to Linear, and WebFetch on `linear.app` | the Linear MCP tools |

### 2. Before `git commit` — `commit-checks`

Runs when a Bash command contains `git commit` without `--no-verify`. It follows
a `cd` earlier in the same command. It works on every changed file in the repo
(staged, unstaged and new), because in `git add x && git commit` the hook runs
before the `add`. Steps 1 to 3 run only when Go files changed. Steps 4 and 5
run on every commit.

In order:

1. **Checks — these block the commit on failure:**
   - `go test` on the changed packages. A change in a top-level folder with a
     dot in its name (`service.foo`) tests the whole folder.
   - `staticcheck -tags integration -tests` on the same packages.
   - `svcchecker` on changed `service.*` folders.
2. **Constants review — advice only.** A headless Claude reviews the diff with
   read-only access (Read, Grep, Glob) to the repo. Its feedback goes to the
   agent, and the commit still runs. Rules:
   - Put a constant in `proto/constants.go` only if other services use it, or
     are meant to. Otherwise, put it at the top of the file that uses it.
   - A value should be a constant when several places must stay the same, when
     it is a setting that could be config (such as a concurrency of 10), or when
     the raw value means nothing (`3.14` → `Pi`).
   - A value should not be a constant when it is already clear
     (`3 * time.Minute` does not need `ThreeMinutes`). If it has a role, name the
     role: `CreateHandlerTimeout = 3 * time.Minute`.
3. **Idempotency review — advice only.** It runs at the same time as the
   constants review, when the change adds or changes an RPC handler signature or
   a file in a `handler/` folder. A headless Claude with the same read-only
   access follows each changed handler from start to end. At each side effect,
   it asks what a retry with the same request does if the handler stops at that
   point. It speaks only when it is confident. It flags:
   - money that moves twice, or a customer who gets the same alert twice;
   - a duplicate record, or an idempotency key that changes between attempts;
   - a "done" state saved before the side effect, a state that a retry rejects
     so the request can never complete, and two attempts that race.

   An event that is published again, or the same row that is written again, is
   safe, so it does not flag them. Test files are not sent to it.
4. **Comments review — advice only.** It runs at the same time as the other
   reviews, when the change adds a comment in a `.proto`, Go, JS, TS, Python,
   shell or SQL file. Generated `*.pb.go` files are skipped. A headless Claude
   with the same read-only access looks only at the comments that the change
   adds or changes. Comments that were there before are out of scope. For each
   comment, it finds a principle whose conditions definitely apply. Most
   comments match no principle, and it ignores them. It reports a comment only
   when the comment does not obey the rule of its principle. Each report gives
   the comment, the principle, the problem and the fix. The principles:
   - **RPC comments** — a comment above an `rpc` in a `.proto` file tells the
     caller how to use the RPC and what they must know. It does not describe
     how the RPC works inside, unless the caller must know it. It can be
     several lines long, if each line has a purpose.
   - **No doc comments on short accessor methods** — a method whose body is
     one statement or a short guard, and whose name already says what it
     returns, has no doc comment. If the name is not clear, rename the method.
5. **Writing review — advice only.** It runs at the same time as the other
   reviews, when the change adds text to a document (`.md`, `.mdx`,
   `.markdown`, `.rst`) or adds a comment in a file that the comments review
   reads. Vendored documents are skipped. A headless Claude with no tools
   checks the new text against the guidelines in
   [Write like Monzo](https://www.notion.so/monzo/WriteLikeMonzo-32969fe426d28073bce0dc6edfb0e5b9),
   with only the parts that apply to writing for staff. It reads comments,
   READMEs, runbooks and other documents. It does not read text for customers,
   error messages, code, or the commit message. It reports a text only when it
   is a very clear violation, and the default is no feedback. It gives one
   answer for the whole commit, by file, with the text, the guideline, the
   problem and a new text. It does not report missing contractions, because my
   writing rules do not use them.

`git commit --no-verify` skips all of this. The block message tells the agent to
ask me first.

### 3. After each edit

**`semgrep`** — runs on each `.go` file the agent writes or edits, and reports
findings to the agent at once. For an edit, it reports only the findings on the
lines that the edit changed. For a Write, it reports all findings in the file:

| Rule | Asks for |
|---|---|
| `go-swallowed-error` | return a logged error, or add a comment that says why it is safe to drop |
| `use-clock-now` | `clock.Now(ctx)`, not `time.Now()` |
| `use-slog-withparam` | `slog.WithParam` for a single param |
| `use-params-not-inline-map` | params through the context, not an inline `map[string]string` in slog/terrors calls |
| `use-newmoneywithcurrency-for-zero` | `decimal.NewMoneyWithCurrency` for zero money |
| `use-timelocation` | `timelocation`, not `dates.Location*` |
| `use-sliceutil-flatmap` | `sliceutil.FlatMapReturningFirstError`, not a hand-written loop |
| `use-sliceutil-map` | `sliceutil.Map`, not a hand-written loop |
| `no-slogparams-in-slog-call` | no `slog.Params(ctx)` in slog calls — only in terrors |
| `dao-setup-only-in-testmain` | DAO setup in `TestMain` only (tests) |
| `terror-test-matching` | `test.AssertTerrorMatches`, not `terrors.Is` in an assertion (tests) |

**Background reviewers** — each runs a headless Claude (Sonnet) on the changed
text. They have no tools, so they see only the new text of the edit (or the
whole file for a Write). They wake the agent only when they find a problem:

| Reviewer | Flags |
|---|---|
| `change-detector-tests` | tests that mirror the implementation instead of checking behaviour |
| `rpc-wrapper` | functions that only wrap one RPC call — inline them |

The reviewers are advice. The agent decides what to act on.

### 4. After a push — `ci-watch`

After `git push`, `gh pr create` or `gh pr ready`, it watches the PR's CI in the
background for up to 30 minutes. It wakes the agent only if a check fails, while
the agent still has the context.

## The hook config

The claudetool entries in `~/.claude/settings.json`:

```json
{
  "hooks": {
    "PreToolUse": [
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "/Users/hturner/bin/claudetool hook no-git-c no-shell-loops no-inline-python no-inline-file no-exit-code-check no-shared-pr-body blank-pr-body"}]},
      {"matcher": "Bash|WebFetch", "hooks": [{"type": "command", "command": "/Users/hturner/bin/claudetool hook use-linear-mcp"}]},
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "/Users/hturner/bin/claudetool hook commit-checks", "timeout": 900}]}
    ],
    "PostToolUse": [
      {"matcher": "Write|Edit|MultiEdit", "hooks": [
        {"type": "command", "command": "/Users/hturner/bin/claudetool hook semgrep"},
        {"type": "command", "command": "/Users/hturner/bin/claudetool hook change-detector-tests", "asyncRewake": true},
        {"type": "command", "command": "/Users/hturner/bin/claudetool hook rpc-wrapper", "asyncRewake": true}
      ]},
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "/Users/hturner/bin/claudetool hook ci-watch", "asyncRewake": true, "timeout": 1800}]}
    ]
  }
}
```

The absolute path matters: hooks don't always get the shell's `PATH`. Several
handlers in one command run in order, and the first block stops the chain. A
background reviewer must be its own entry, because `asyncRewake` is set per hook.

## Debugging

- `tail -f ~/.claude/claudetool-async.log` shows each reviewer run: `DISPATCH`,
  then `PASS`, `REVISE` or `ERROR`. A reviewer that fails to run never wakes the
  agent, so an `ERROR` here is the only sign.
- `~/.claude/ci-watch/` holds a pidfile for each branch that `ci-watch` is
  watching.
- To try a handler by hand, pipe hook JSON into it:
  `claudetool hook no-shell-loops < input.json`. Exit 2 means blocked.

## Changing the rules

- **A semgrep rule:** add it to the YAML in `internal/hook/handler_semgrep_rules.go`.
- **A reviewer rule:** edit the `rubric` text in that reviewer's file, for
  example `handler_rpcwrapper.go`. The commit-time reviewers keep theirs in
  `handler_constants.go`, `handler_idempotency.go`, `handler_comments.go` and
  `handler_writing.go`.
- **A comment principle:** add a section under `PRINCIPLES` in
  `handler_comments.go`. Give it a title, an "Applies when:" line that says when
  it fires, a "Rule:" line that says what the comment must do, and examples.
- **A new guard:** add `internal/hook/handler_<name>.go` and register it:

  ```go
  func init() { Register("your-name", handleYourName) }

  func handleYourName(in *Input) (*Output, error) {
      // return nil, nil to allow
      // return nil, fmt.Errorf("reason") to block (exit 2)
      // return &Output{...}, nil for structured output
  }
  ```

- **A new background reviewer:** configure an `asyncReview` (file suffixes, a
  cheap `precheck`, and a `rubric`) and register its `handler()`. See
  `handler_rpcwrapper.go`.

Then do all the steps in [Finishing a change](#finishing-a-change).

## Finishing a change

A change is not complete until you do all of these steps. Do them in this
order. Do not stop after a step to ask if you must continue.

1. **Test:** run `go test ./...`. If a test fails because of your change, fix
   it. If a failure was there before your change, tell me the test and the
   cause.
2. **Document:** if the change alters what a handler does, update this README.
3. **Install:** run `go install . ./cmd/...`. Hooks use the new binary at once.
4. **Add the hook:** if you added a handler, add it to `~/.claude/settings.json`.
5. **Commit:** commit to `master`. Commit only the files that your change
   touched. If there are other uncommitted changes, do not commit them, and tell
   me about them.
6. **Push:** run `git push origin master`. `origin` is my fork. Never push to
   `upstream`.

## Handlers I don't use

These are in the binary but not in my config: `no-cd`, `strip-redundant-cd`,
`auto-approve-readonly` (auto mode decides approvals), `no-command-substitution`
(wearedev's `bash-guard.sh` covers it), `use-ripgrep`, `no-prod`,
`no-diff-master`, `go-augment-style`, `go-fix`, `go-makeslice`, `go-named-func`,
`go-cmp-or`, `backend101`, `redirect-writes`, `dump`, and the `statusline`
command (I use the Monzo status line plugin).
