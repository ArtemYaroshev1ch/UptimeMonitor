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
