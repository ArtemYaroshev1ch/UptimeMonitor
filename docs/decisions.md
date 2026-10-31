# Decisions

Lightweight decision records: context → decision → alternatives.
Newest entries go at the bottom.

## 001. LF line endings for all text files

*2026-10-24, Phase 0*

**Context.** Development happens on native Windows, where Git for Windows sets
`core.autocrlf=true` by default and checks text files out with CRLF. Shell
scripts and make recipes break on CRLF (`$'\r': command not found`), gofmt
reports CRLF files as unformatted, and Docker/compose/`.env` parsers expect LF.

**Decision.** `.gitattributes` sets `* text=auto eol=lf`: the repository and
every working tree use LF, whatever `core.autocrlf` says. `Makefile`, `*.mk`
and `*.sh` are additionally forced to `text eol=lf`; `*.bat`/`*.cmd` use CRLF.

**Alternatives.** Ask every contributor to set `core.autocrlf=input`: easy to
forget and not enforced. LF only for Makefile and `*.sh`: Go files would still
get CRLF on Windows and fail formatting checks.

## 002. Makefile runs on native Windows through sh from Git for Windows

*2026-10-24, Phase 0*

**Context.** The project must build on Linux (CI), macOS and native Windows
without WSL. GNU Make for Windows uses `sh.exe` when it finds one in PATH and
silently falls back to `cmd.exe` otherwise. Even with sh, make starts simple
recipe lines (without shell metacharacters) directly, so tools such as `echo`,
`date`, `rm` or `awk` must be in PATH, and they live in Git's `usr\bin`, which
is not in PATH outside Git Bash.

**Decision.**
- Recipes are POSIX sh. The Makefile stops with a clear error if make uses
  `cmd.exe`, and on Windows prepends Git's `usr\bin` (found with `cygpath`) to
  PATH, so it works from PowerShell, cmd and Git Bash alike.
- Executable names use `go env GOEXE` (`.exe` on Windows).
- `run-api` builds `bin/api` and starts it instead of `go run`: the executable
  path stays the same, so Windows Firewall asks once, and Ctrl+C reaches the
  server process directly.
- `-race` needs cgo and a C compiler. It is enabled when `go env CGO_ENABLED`
  is `1`; on a Windows machine without gcc tests run without it, and CI
  (Linux) always runs them with `-race`.
- Scripts are invoked as `sh script.sh` or `bash script.sh` rather than relying
  on the executable bit, which Windows checkouts do not keep.

**Alternatives.** Require WSL: excluded by the environment. PowerShell scripts
or a Go-based task runner (mage, task): a second toolchain to learn, while
GNU Make is the de-facto standard in Go projects and CI.

## 003. golangci-lint is installed by `go install` into `bin/tools`

*2026-10-24, Phase 0*

**Context.** `make lint` must work on a fresh machine, and the linter has to
support the Go version from `go.mod` (here Go 1.27).

**Decision.** The Makefile pins `GOLANGCI_LINT_VERSION` and builds the linter
with the local Go toolchain into `bin/tools/golangci-lint-<version>`. The
version in the file name makes a version bump reinstall it. CI uses the
official `golangci-lint-action` with the same version for speed and caching.

**Alternatives.** Prebuilt release binaries via the install script: faster,
but it means piping a downloaded script into sh and handling OS/arch on
Windows. `go tool` (a `tool` directive in `go.mod`): adds the linter's large
dependency tree to the project's module graph and Dependabot updates.

## 004. Repository structure and configuration grow phase by phase

*2026-10-24, Phase 0*

**Context.** The target layout and the full list of environment variables are
known up front, but most packages and variables belong to later phases.

**Decision.** A directory, a Makefile target or a variable in `.env.example`
appears in the phase that implements it. Git does not track empty directories,
and placeholder packages or unused variables would document things that do
not exist yet.

**Alternatives.** Create the whole tree with `doc.go` stubs and every variable
on day one: the structure is visible earlier, but the repository describes
features that are not there.

## 005. Build metadata is linked into package main

*2026-10-24, Phase 0*

**Context.** `/version`, `-version` and `uptime_build_info` report version,
commit and build time.

**Decision.** Each binary declares `version`, `commit` and `builtAt` in package
`main`, set with `-ldflags "-X main.version=..."`; the same flags work for all
three binaries. The values are passed on as `observability.BuildInfo`, so no
other package reads globals. The version is SemVer without the `v` of the git
tag (`v1.2.0` → `1.2.0`, matching the image tag), `0.0.0-dev` before the first
tag. `-version` prints with `fmt` to stdout because it is command output, not
a log record.

**Alternatives.** Variables in an internal package: a long `-X` path for no
benefit. Only `debug.ReadBuildInfo()` VCS stamps: not available for `go run`
and Docker builds without `.git`.

## 006. Configuration parsing with caarlos0/env

*2026-10-24, Phase 0*

**Context.** Configuration comes only from environment variables and must be
validated at startup.

**Decision.** `caarlos0/env` parses typed fields (durations, `slog.Level`) and
defaults from struct tags; semantic checks live in `validate()` methods that
report all problems at once via `errors.Join`. Tests pass a map through
`env.Options.Environment` instead of changing the process environment. Parse
errors from the library name the struct field (`ShutdownTimeout`) rather than
the variable (`SHUTDOWN_TIMEOUT`); the rejected value is in the message, which
is enough to find the mistake.

**Alternatives.** Hand-written `os.Getenv` parsing: more code for every type.
Translating field names to variable names via reflection: extra code for a
small gain.

## 007. The API starts without a database; readiness reports it

*2026-10-30, Phase 1*

**Context.** The API needs PostgreSQL, but the database may be unavailable at
startup (it starts in parallel, a failover is in progress) or later.

**Decision.** The pgx pool is created without connecting (`MinConns = 0`), so
the process starts and serves `/healthz` regardless of the database.
`GET /readyz` pings the database with a 2 s timeout and answers 503 while it is
unreachable: the instance leaves the load balancer but is not restarted.
The response names the failed check without the error text (the port is
public); the error goes to the log at `warn`. `/healthz` never touches the
database (see `handler.Healthz`).

**Alternatives.** Ping at startup and exit on failure: during a database
outage every replica crash-loops, and restarts cannot fix the database.
Retry at startup until the database appears: the process is up but cannot
explain why it is not serving, which is what `/readyz` does.

## 008. Migrations: goose Provider, embedded SQL, advisory lock

*2026-10-30, Phase 1*

**Context.** Migrations run as a separate step (`/app/migrate up`) before a
new version starts. Two deploys may overlap, and the binary must not depend on
files next to it.

**Decision.**
- SQL files `migrations/NNNN_name.sql` are embedded with `embed`, so each
  binary carries exactly the schema of its commit.
- goose's `Provider` API instead of package-level functions: no global state
  (dialect, table name, logger), and every call takes a `context.Context`.
- A PostgreSQL session advisory lock (`lock.NewPostgresSessionLocker`)
  serializes concurrent runs; an integration test runs two `up`s at once.
- Commands: `up` (all pending), `down` (exactly one step, safe to repeat),
  `status`. Rolling back several steps is a deliberate repetition of `down`.
- Numbering is sequential (`0001`, `0002`, …), created by
  `make migrate-create name=...`; with one developer, timestamp prefixes would
  only make ordering harder to read.

**Alternatives.** goose CLI in the image: a second binary and its own config.
golang-migrate: similar features, but goose is the pinned choice (PROMPT.md §4).
Timestamp-based versions: avoid merge conflicts in big teams, not needed here.

## 009. Connection pool metrics come with the metrics registry (Phase 7)

*2026-10-30, Phase 1*

**Context.** Rule 8 says an external call is not finished without its metrics,
and Phase 1 adds the database. The plan (PROMPT.md §14) places the metrics
registry, the ops server (`:9090`) and the `pgxpool` collector in Phase 7.

**Decision.** Phase 1 adds no metrics. `uptime_db_pool_*` is implemented in
Phase 7 together with the registry and `/metrics`, where it can be scraped and
tested with `promlint`. Until then `/readyz` and the logs show the database
state.

**Alternatives.** A collector now without a `/metrics` endpoint: code that
cannot be observed and would be reworked when the registry appears. Pulling
the ops server forward into Phase 1: mixes two phases in one review.

## 010. Integration tests: testcontainers, build tag, one container per test

*2026-10-30, Phase 1*

**Context.** Repository code and migrations must be tested against a real
PostgreSQL, both locally (Windows with Docker Desktop) and in CI (Linux).

**Decision.** Tests that need Docker carry the build tag `integration` and run
with `make test-integration`; `make test` stays fast and Docker-free.
`pgtest.Start` starts an empty `postgres` container per test with the same
image tag as `docker-compose.yml`. golangci-lint runs with the tag, so this
code is linted too. CI has a separate `integration` job.

**Alternatives.** A shared container per package with a database per test:
faster, worth doing once the number of tests makes the start-up time (~3 s)
noticeable. An external database from compose: tests would depend on manual
setup and leftover state.

## 011. Access tokens: HS256 JWT via golang-jwt, no refresh tokens

*2026-10-31, Phase 2*

**Context.** The API needs stateless authentication that works with N API
replicas. The stack (PROMPT.md section 4) fixes `golang-jwt/jwt/v5`.

**Decision.** One HS256-signed access token per login, lifetime
`ACCESS_TOKEN_TTL` (12 h), claims `sub` (user ID), `iss`, `iat`, `exp`. The
parser accepts only HS256 and requires `exp`, so tokens with `alg: none`, an
other algorithm or no expiry are rejected. `JWT_SECRET` must be at least 32
bytes. A valid token of a deleted user gets 401, like any invalid token.

**Alternatives.** Hand-written HMAC JWT on the standard library: little code,
but parsing untrusted tokens is exactly where subtle bugs hide. Refresh tokens
with rotation and logout: deferred to the stretch list (PROMPT.md section 17);
until then a token cannot be revoked before it expires, short of rotating
`JWT_SECRET` (which logs everyone out).

## 012. Emails are stored lowercase

*2026-10-31, Phase 2*

**Context.** `users.email` is `UNIQUE`, but case-sensitive: `Alice@x.com` and
`alice@x.com` would be two accounts, and a user who typed a different case at
login would get "invalid credentials".

**Decision.** The service trims and lowercases emails before registration and
login. The unique index then enforces one account per address; a duplicate is
reported as 409 from the unique violation (no racy "SELECT first").

**Alternatives.** A `citext` column or a unique index on `lower(email)`: same
effect in the database, but needs a migration and still requires lowercasing
for lookups by index. Per RFC 5321 the local part is case-sensitive, but no
real provider treats it that way.

## 013. Auth rate limit: in memory, per client IP, optional proxy header

*2026-10-31, Phase 2*

**Context.** `/api/v1/auth/*` is limited to 10 requests per minute per IP.
The API has no shared store (no Redis), and in production it runs behind a
reverse proxy, where the TCP peer is the proxy.

**Decision.** A token bucket per IP (`golang.org/x/time/rate`, burst 10,
refill one per 6 s), shared by register and login. Buckets idle for a minute
are dropped inline, at most once a minute, so there is no cleanup goroutine.
The client IP is the TCP peer unless `TRUST_PROXY_HEADERS=true` (a variable
added to the PROMPT.md section 12 list); then it is the **last**
`X-Forwarded-For` entry, the one appended by our own proxy. Earlier entries
come from the client and are ignored.

**Known limitations.** Each API replica counts separately, so with N replicas
the effective limit is N x 10. Every IPv6 address is a separate client,
though one user may own a whole /64. Both are acceptable for brute-force
protection at this scale; a shared limiter (PostgreSQL or Redis) would fix
the first.

**Alternatives.** chi `middleware.RealIP`: trusts headers from anyone, so a
client could pick a new "IP" per request and bypass the limit.

## 014. Request decoding: unknown fields rejected, oversized body is a 400

*2026-10-31, Phase 2*

**Context.** The error codes are a closed list (PROMPT.md section 9); 413 is
not on it. The body limit is 1 MiB.

**Decision.** JSON bodies are decoded strictly: unknown fields, trailing data
and wrong types are `validation_failed` with the field in `details`. A body
over 1 MiB is also `validation_failed` (`{"field":"body","issue":"must be at
most 1048576 bytes"}`), status 400.

**Alternatives.** 413 with a new code `payload_too_large`: more precise HTTP,
but extends the public error contract for a case no legitimate client hits.
Ignoring unknown fields: a typo such as `"pasword"` would yield a confusing
"password is required".

## 015. Request ID from the client is reused if it is safe

*2026-10-31, Phase 2*

**Context.** Every log line of a request needs `request_id`, and services deep
in the call stack log with only a `context.Context`.

**Decision.** The request ID middleware reuses an incoming `X-Request-ID` (a
proxy may have set it, which links proxy and API logs) if it is at most 64
characters of `[A-Za-z0-9._-]`; otherwise it generates 128 random bits. The ID
is returned in `X-Request-ID` and stored in the context; a wrapping
`slog.Handler` (`logger.WithAttrs`) adds it, and `user_id` after
authentication, to every record logged with that context.

**Alternatives.** chi `middleware.RequestID`: does not reuse the incoming
header and does not reach slog. A per-request `*slog.Logger` in the context:
every function would have to fetch the logger from the context instead of
using its injected one.
