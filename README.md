# Overwatch Backend

The Go API and application composition root for Overwatch, a source-grounded
investigation workspace. It provides workspace-scoped source capture,
provenance-preserving observations, authored research records and connections,
bounded assistance, and the audit and retention machinery around them.

## What it supports

- Accounts, sessions, organisations, workspaces, memberships, and access
  boundaries.
- Source references and retained captures from URLs, text, PDFs, and images.
- Content-addressed artifact storage, capture history, source comparison, and
  provenance-linked PDF/OCR extraction.
- Exact observations tied to a source capture or derived extraction.
- Research records for people, accounts, organisations, and places.
- Qualified connections with supporting and opposing observations, review
  state, rationale, and append-only revisions.
- Reversible, human-confirmed identity-resolution proposals.
- Evidence review, bounded assistance proposals, grounded synthesis runs,
  investigation questions, reported events, working notes, briefs, and frozen
  handoffs.
- Retention review, source privacy/legal hold, explicit purge gates, artifact
  cleanup review, audit entries, journal lines, and an outbox dispatcher.
- Workspace-scoped causal Logs (`GET /v1/workspaces/{workspace}/logs`) with
  keyset pagination and correlation/causation provenance, kept separate from
  the governance audit trail.
- A workspace-scoped change projection (`GET /v1/workspaces/{workspace}/changes`)
  that compares the latest two completed runs per target and never calls an
  unmeasured subject gone. The paired `POST
  /v1/workspaces/{workspace}/changes/seen` stores a server-timestamped watermark
  per account and workspace.
- The existing operational surfaces for targets, tools, checks, runs, findings,
  and reports remain available alongside the investigation model.

AI and external processes are optional adapters. Local deterministic providers
remain available when no external binary is configured. Assistance and
synthesis receive bounded, selected material and return reviewable output;
they do not silently create observations, resolve identities, accept
relationships, or publish conclusions.

External assistance is denied by default at the workspace level. An admin must
explicitly opt a workspace in before a configured external provider can receive
retained material; local providers remain available, and policy changes are
persisted and emitted into the audit/event stream.

## Architecture

- Go HTTP server using the standard library `net/http` and `log/slog`.
- Explicit composition in `root/boot.go`; dependencies are constructed at the
  application boundary rather than resolved through a runtime container.
- Domain modules under `internal/<domain>` with application commands/queries,
  domain rules, and PostgreSQL adapters.
- PostgreSQL through `pgx`; SQL and forward-only, checksummed migrations are
  the source of truth for persistence.
- Shared infrastructure under `pkg/` for IDs, provenance, errors, HTTP,
  PostgreSQL, content-addressed blobs, bounded process execution, limits,
  mail, logging, and the outbox.
- Optional OCR, assistance, and synthesis executables are invoked through a
  bounded, shell-free process boundary with explicit time and output limits.

The server applies migrations during boot. A bad configuration, unreachable
database, edited applied migration, or unavailable required artifact root
fails before the HTTP listener is announced.

## Requirements

- The Go toolchain specified by `go.mod`.
- PostgreSQL for the running server and database-backed tests.
- Docker Compose if using the workspace infrastructure repository.
- `air` only if you want `make dev` hot reload:

```bash
go install github.com/air-verse/air@latest
```

## Local development

From this repository:

```bash
cp .env.example .env
mkdir -p var/artifacts
make run
```

The server listens on `http://localhost:7002` by default and exposes health
checks at `/healthz` and `/readyz`. The API is versioned under `/v1`.

For the complete workspace setup, run from the parent directory:

```bash
cp .env.example .env       # at the Overwatch workspace root
make up                    # PostgreSQL, test PostgreSQL, and Mailpit
make dev                   # backend and UI
```

The backend reads its required configuration from the environment. The local
example includes safe development values for PostgreSQL, mail, artifact
storage, execution limits, and the optional providers. Never commit a real
`.env` or production credentials.

Important settings include:

```text
PORT_SERVER=7002
DATABASE_URL=postgres://overwatch:overwatch@localhost:7020/overwatch?sslmode=disable
BASE_URL=http://localhost:7010
MAIL_ADDR=localhost:7025
ARTIFACT_ROOT=./var/artifacts
```

`OVERWATCH_TEST_DSN` is used only by database-dependent tests. Those tests
skip when it is unset; the rest of the Go suite can run without PostgreSQL.

## Commands

```bash
make run       # run once
make dev       # run with air reload
make build     # compile ./tmp/server
make test      # go test ./...
make test-db   # database-backed tests, when OVERWATCH_TEST_DSN is set
make check     # go vet and race-enabled tests
make tidy      # go mod tidy
```

Useful operational commands:

```bash
make env                       # show declared configuration values
make recent                    # latest journal lines
make chain C=<correlation-id>  # inspect one request's causation tree
```

## Project layout

```text
cmd/server/          server entrypoint
root/                composition root, routes, configuration, API wiring
internal/<domain>/   domain, application, and PostgreSQL implementation
pkg/                 shared infrastructure and safety boundaries
scripts/             operational SQL helpers
```

The frontend is maintained in the sibling `overwatch-ui` repository.
Workspace-level architecture, ports, environment policy, and the investigation
vision are documented in the workspace root.

## Safety and product boundaries

Source bytes, capture identity, extraction provenance, observations, authored
records, and connection assessments remain distinct. A model suggestion is a
review aid, not a fact. Identity resolution is explicit and reversible, and a
connection's review state describes the investigation's assessment rather
than certifying real-world truth.
