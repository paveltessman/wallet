---
name: implement
description: Build a feature in reviewed steps. Plan first, split the work by commits, then write one commit at a time and stop for review. Use when the user asks to implement a feature, a part of the spec, or a change described in text, and wants to review each step before it lands.
---

# implement

Three phases. The user reviews between every one of them. You never commit.

```
1. Plan      you read, you ask, you propose commits   -> user approves
2. Build     you write one commit, you verify, stop   -> user reviews and commits
3. Resume    you read HEAD, you build the next one    -> repeat until done
```

## Phase 1: plan

The user names the work as text, or points at a document such as an issue or a decision in `docs/spec.md`.

Read before you plan:

- The document the user named, and `docs/spec.md` above it.
- `docs/tooling.md`. It sets the layout, the tools, and the `make` targets.
- The code the change touches. Read whole files, not excerpts.
- The nearest thing that already works.
- The tests of that nearest thing. They state the conventions.

Then write the plan in the chat. It holds:

1. **What the change adds**, in four or five lines. Name the packages it touches.
2. **The decisions you made**, with the reason. A design document leaves gaps. Fill them, state that you filled them, and let the user correct you before any code exists.
3. **The commits.** One heading each, with the files and the tests. Say what makes each one green on its own.
4. **What stays out**, and why.

Split the commits so that each one passes on its own.

Ask a question only where two readings lead to different work. Anything else is a decision you state in the plan. If the work conflicts with a decision in `docs/spec.md`, stop and ask. Do not change the decision silently.

**Stop. Wait for the user to approve the plan.**

## Phase 2: build one commit

Work the current commit and nothing else. Do not start the next one.

Before you stop, every one of these passes:

- `make generate`, if the commit touches `db/queries/` or `db/migrations/`.
- `make check`. It runs `go mod tidy -diff`, `go vet`, the tests, and the lint.

The integration tests need `TEST_DATABASE_URL`. Load it from `.setenv`, and start the database with `make up` if it is not running. If you cannot run them, say so in the report. Do not report them as green.

Then report in the chat any things to look at: a decision worth a second opinion, a trade-off, an assumption and so on.

Keep your report concise and short.

In your report, suggest a commit message in the style of `git log`, for example `docs: decide tooling`. Add a footer with you as the co-author.

**Stop. Do not run `git commit`. The user commits.**

## Phase 3: resume

The user commits by hand, and can introduce changes while doing it. Before the next commit:

- Read `git log --oneline -3` and `git show --stat HEAD`.
- Take this as the current state. Never silently revert it. If the change is bad, stop and tell the user about it.

## Rules

- **Never commit, never push.** The user does both.
- **One commit per turn.** Green at the end of each.
- **Match the code around you.** Comment density, naming, error wording, test style. The nearest working file is the specification.
- **Write the comments and the chat in ASD-STE100.** The user rule set applies. Code and identifiers do not.
- **Say when a test is weak.** A test that passes because a mock returns the zero value proves nothing. Scope it to the part under test.

## Notes

- Tests sit next to the code as `<file>_test.go`.
- Integration tests use a real PostgreSQL and read `TEST_DATABASE_URL`. They insert the wallets directly, because the API does not create them (decision 4).
- golangci-lint formats the code with `gofmt` and `goimports`. `exhaustive` requires every `operationType` value in a switch.
- Migrations are goose SQL files in `db/migrations/`. squawk checks them on commit. sqlc reads the schema from the same files.
- The sqlc output in `internal/postgres/internal/sqlc/` is generated and gitignored. Never edit it by hand. Change `db/queries/` and run `make generate`.
- Every error response uses the Problem Details body and the `code` and `retryable` values from decisions 9 and 10. A new error case needs a row in the table of decision 10 first.
- Settings come from `config.env`. Add a new variable to `config.env.example` and to the table of decision 16.
- A new tool goes into the `tool` block of `go.mod` and runs through `go tool`. A new command goes into a `make` target with a `## ` comment.
