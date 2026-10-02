# Tooling

This document describes the development tools of the project.

## Principles

`go.mod` pins the Go tools in a `tool` block, and every caller runs them through `go tool`. The Makefile, the pre-commit hooks, and CI use the same version.

`make` holds every command. Pre-commit and CI call `make` targets and do not repeat the commands.

Generated code stays out of git. `make generate` makes it again from the source.

## Go tools

The `tool` block in `go.mod`:

| Tool            | Module                                                   | Use                              |
| --------------- | -------------------------------------------------------- | -------------------------------- |
| `sqlc`          | `github.com/sqlc-dev/sqlc/cmd/sqlc`                      | Generate Go code from SQL.       |
| `goose`         | `github.com/pressly/goose/v3/cmd/goose`                  | Make new migration files.        |
| `golangci-lint` | `github.com/golangci/golangci-lint/v2/cmd/golangci-lint` | Lint and format the Go code.     |
| `gotestsum`     | `gotest.tools/gotestsum`                                 | Run the tests with short output. |

Tools outside Go:

- `pre-commit`
- Docker with the Compose plugin

The k6 load test (spec decision 15) runs in the `grafana/k6` container.

## Make

`make` with no target prints the list of targets. Each target has a `## ` comment, and the `help` target reads these comments.

| Target           | Action                                                                     |
| ---------------- | -------------------------------------------------------------------------- |
| `help`           | List the targets.                                                          |
| `generate`       | Run `sqlc generate`.                                                       |
| `build`          | Build the binary into `./bin`.                                             |
| `run`            | Run the service on the host.                                               |
| `check`          | Run all the CI checks: `go mod tidy -diff`, `go vet`, the tests, the lint. |
| `fmt`            | Format the Go code with `golangci-lint fmt`.                               |
| `tidy`           | Run `go mod tidy`.                                                         |
| `migrate`        | Apply the pending migrations.                                              |
| `migrate-status` | Show the applied migrations.                                               |
| `psql`           | Open `psql` on the dev database.                                           |
| `up`             | Build and start the database and the service with Docker Compose.          |
| `down`           | Stop the stack. The volumes stay.                                          |
| `logs`           | Follow the service logs.                                                   |
| `load`           | Run the k6 load test against the local stack.                              |
| `load-wallets`   | Run the k6 load test on random wallets against the local stack.            |
| `clean`          | Remove the build output and the generated files.                           |

`check` is read-only.

## Database: sqlc and goose

### Layout

```
db/
  db.go          embeds the migrations into the binary
  migrations/    goose SQL migrations: 00001_wallets.sql, ...
  queries/       sqlc query files
internal/postgres/internal/sqlc/   sqlc output, *_sqlc.go, gitignored
sqlc.yaml
```

### goose

- Migrations are plain SQL files with `-- +goose Up` and `-- +goose Down` sections.
- `db.go` embeds the files with `//go:embed`. The binary applies them with the `migrate up|down|status` subcommand, which calls the goose library. Thus the container needs no source tree and no goose CLI.

### sqlc

`sqlc.yaml`:

- `engine: postgresql`, `sql_package: pgx/v5`
- `schema: db/migrations`, so sqlc reads the schema from the goose files.
- `output_files_suffix: _sqlc`.

## Pre-commit

The hooks run in two stages. Fast checks run on each commit. The full check runs on push.

```yaml
default_install_hook_types: [pre-commit, pre-push]
default_stages: [pre-commit]
```

| Hook                                                                                                                             | Stage      | Source                        |
| -------------------------------------------------------------------------------------------------------------------------------- | ---------- | ----------------------------- |
| `check-yaml`, `end-of-file-fixer`, `trailing-whitespace`, `check-added-large-files`, `check-merge-conflict`, `mixed-line-ending` | pre-commit | `pre-commit/pre-commit-hooks` |
| `squawk` on `db/migrations`                                                                                                      | pre-commit | `sbdchd/squawk`, as in `yaa`  |
| `golangci-lint fmt`                                                                                                              | pre-commit | local, `make fmt`             |
| `go mod tidy`                                                                                                                    | pre-commit | local, `make tidy`            |
| `make check`                                                                                                                     | pre-push   | local, `make check`           |

The local hooks use `language: system`, so they run the tool versions from `go.mod`.

Squawk finds unsafe migrations, for example a lock on a large table. `.squawk.toml` turns off the rules that do not apply to a small service: `ban-drop-table`, `require-lock-timeout`, `require-statement-timeout`.

## GitHub CI

Two workflows run on each pull request. Both have `permissions: contents: read` and a `concurrency` group that cancels the old run on a new push.

### `ci.yml`: pre-commit

- Runs `pre-commit/action` on all files.
- Skips the local hooks with `SKIP: golangci-lint-fmt,go-mod-tidy,make-check`. The `go.yml` workflow covers them.

### `go.yml`: make check

- Skips changes to `docs/**` and `**.md` only.
- Starts a `postgres:18-alpine` service container with a health check. The integration tests and the migrations need a real server.
- Gives the tests `TEST_DATABASE_URL`.
- Uses `actions/setup-go` with `go-version-file: go.mod` and the module cache.
- Runs `make generate`, because the sqlc output is not in git.
- Runs `make check`.

## Local environment

- `config.env.example` is in git. `make up` copies it to `config.env` if the file does not exist. `config.env` is gitignored.
- `docker-compose.yml` has four services:
  - `db`: `postgres:18-alpine`, with a health check.
  - `migrate`: runs `wallet migrate up` one time, after `db` is healthy.
  - `app`: starts after `migrate` exits with code 0. Its health check runs `wallet healthcheck` (spec decision 17), so `make up` waits until the app is ready.
  - `k6`: runs `load/wallet.js` against `app`. `make load-wallets` runs `load/wallets.js` in the same service. It has the profile `load`, so `make up` does not start it.
- `migrate` and `app` use one image from the `Dockerfile`. `pull_policy: build` makes Compose build the image on each `up`. The BuildKit cache makes a build with no changes fast.
- The `Makefile` runs Compose with `--env-file config.env`, so `docker-compose.yml` can read `HTTP_PORT`. `app` publishes `HTTP_PORT` on the same port of the host. A `docker compose` call without `--env-file` stops with an error.
- The host gets Postgres on port `5432`, the same port as the CI service container.
- `.setenv` holds `TEST_DATABASE_URL` for the integration tests on the host. It is gitignored.

## Linter configuration

`.golangci.yml`:

- `linters.default: standard`, plus `exhaustive` with `default-signifies-exhaustive: false`. The `operationType` switch then must name every value.
- Formatters: `gofmt`, `goimports`.
- Generated files: `exclusions.generated: lax`.
