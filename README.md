# Uptime Monitor

[![CI](https://github.com/artemyaroshev1ch/uptime-monitor/actions/workflows/ci.yml/badge.svg)](https://github.com/artemyaroshev1ch/uptime-monitor/actions/workflows/ci.yml)
[![Go](https://img.shields.io/github/go-mod/go-version/artemyaroshev1ch/uptime-monitor)](go.mod)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Self-hosted uptime monitoring for websites with HTTP/HTTPS diagnostics,
incidents and notifications, built to be operated: metrics, health probes,
structured logs and graceful shutdown.

> **Status: early development (Phase 1, database).** The API process with
> health probes and `/version`, PostgreSQL and migrations exist so far. Items
> marked *planned* are not implemented yet; see [Roadmap](#roadmap).

## Features

- Liveness `GET /healthz`, readiness `GET /readyz` (checks PostgreSQL) and
  build information `GET /version`.
- PostgreSQL schema migrations embedded in a separate `migrate` binary.
- JSON logs in production, text logs in development.
- Graceful shutdown on `SIGTERM`/Ctrl+C.
- `-healthcheck` flag for container health checks without curl.
- *Planned:* users and JWT auth, HTTP/HTTPS monitors with per-phase timings
  (DNS, connect, TLS, TTFB), incidents, Telegram and webhook notifications,
  TLS certificate expiry warnings, Prometheus metrics.

## Quick start

Requirements: Go 1.27+, GNU Make, a POSIX shell (on Windows: Git for Windows,
see [Windows](#windows)) and Docker with Compose v2.

```sh
cp .env.example .env
make up           # PostgreSQL in Docker, waits until healthy
make migrate-up   # apply migrations
make run-api
```

In another terminal:

```sh
curl http://localhost:8080/healthz
# {"status":"ok"}
curl http://localhost:8080/readyz
# {"status":"ok","checks":{"database":"ok"}}
curl http://localhost:8080/version
# {"version":"0.0.0-dev","commit":"...","built_at":"..."}
```

*Planned:* the whole system with `docker compose up --build`.

## Configuration

All configuration comes from environment variables. `make run-api` also reads
`.env` if it exists.

| Variable           | Description                                      | Default |
|--------------------|--------------------------------------------------|---------|
| `APP_ENV`          | `dev` (text logs) or `prod` (JSON logs)          | `dev`   |
| `LOG_LEVEL`        | `debug`, `info`, `warn` or `error`               | `info`  |
| `HTTP_ADDR`        | public REST API listen address                   | `:8080` |
| `DATABASE_URL`     | PostgreSQL URL (**required**, contains password) | —       |
| `DB_MAX_CONNS`     | connection pool size of one `api` process        | `10`    |
| `SHUTDOWN_TIMEOUT` | time for in-flight requests to finish on SIGTERM | `25s`   |

Invalid values stop the process at startup with an error that lists every
problem.

## Operations

| Endpoint / flag    | Purpose                                                           |
|--------------------|-------------------------------------------------------------------|
| `GET /healthz`     | liveness: the process serves HTTP; does not check dependencies    |
| `GET /readyz`      | readiness: 200 if PostgreSQL answers within 2 s, otherwise 503    |
| `GET /version`     | `{"version","commit","built_at"}` of the running build            |
| `api -healthcheck` | probes `/healthz` on `HTTP_ADDR`, exit code 0 = healthy, 1 = not  |
| `api -version`     | prints version, commit, build time and Go version                 |
| `migrate up`       | applies all pending migrations; run before starting a new version |
| `migrate down`     | rolls back exactly one migration                                  |
| `migrate status`   | lists migrations and whether they are applied                     |

- **Logs:** one record per line on stdout with `time`, `level`, `msg`,
  `service` and `version`.
- **Shutdown:** on `SIGTERM` the API stops accepting connections and waits up
  to `SHUTDOWN_TIMEOUT` for in-flight requests.
- **Database outage:** `/readyz` returns 503 and the API keeps running; it
  serves again as soon as the database is back. `/healthz` stays 200.
- *Planned:* ops port with `/metrics` (catalog in `docs/metrics.md`) and the
  rollback procedure.

### Migrations

Migrations live in `migrations/NNNN_name.sql` (goose format) and are embedded
into the `migrate` binary. A PostgreSQL advisory lock makes concurrent
`migrate up` runs wait for each other.

```sh
make migrate-create name=add_monitors   # new empty migration
make migrate-up                         # apply all pending
make migrate-down                       # roll back one
make migrate-status                     # what is applied
```

Every migration must work with the **previous** release of the application,
because the old version keeps running while the new one is rolled out and
after a rollback (expand/contract):

- Allowed: new tables, new nullable columns or columns with a default, new
  indexes (`CREATE INDEX CONCURRENTLY` on large tables).
- In two releases: renaming or dropping a column, tightening a constraint.
  First release: add the new shape and write to both; next release, once the
  old code is gone: remove the old shape.
- `-- +goose Down` must undo `Up` exactly; the integration test applies all
  migrations, rolls them back and applies them again.

## Security

*Planned:* SSRF protection for monitored URLs and the `ALLOW_PRIVATE_TARGETS`
development switch.

## Development

```sh
make help   # list all targets
make test   # unit tests (-race when cgo is available)
make test-integration  # plus tests against PostgreSQL in Docker (testcontainers)
make lint   # golangci-lint (installed into bin/tools on first run)
make fmt    # gofmt + goimports
make cover  # coverage report in coverage.html
```

```
cmd/api/                  API binary
cmd/migrate/              migration binary
internal/config/          environment configuration and validation
internal/httpapi/         router, handlers, JSON responses
internal/observability/   build information (metrics later)
internal/platform/        logger, HTTP server, self health check, PostgreSQL
migrations/               SQL migrations (embedded)
docker-compose.yml        local PostgreSQL
docs/decisions.md         design decisions
```

### Windows

The project builds natively on Windows without WSL:

- Install Git for Windows and GNU Make. `make` must find `sh.exe` in PATH
  (`C:\Program Files\Git\bin`); the Makefile stops with a clear error if it
  does not and adds Git's `usr\bin` tools to PATH itself.
- `.gitattributes` keeps every text file in LF regardless of `core.autocrlf`.
- The race detector needs a C compiler; without one, `make test` runs without
  `-race`. CI always runs with `-race`.
- Listening on `:8080` triggers a Windows Firewall prompt on first run. Set
  `HTTP_ADDR=127.0.0.1:8080` in `.env` to stay on loopback.
- Docker Desktop must be running for `make up` and `make test-integration`.
- `DATABASE_URL` uses `127.0.0.1`, not `localhost`: `localhost` resolves to
  `::1` first, where the container does not listen, and each connection waits
  about 2 s before falling back to IPv4.

Background for these choices is in [docs/decisions.md](docs/decisions.md).

## Related

Deployment, monitoring stack, SLOs, alerts and runbooks live in the
`uptime-infra` repository (*planned*).

## Roadmap

- [x] Phase 0: skeleton, config, logging, `/healthz`, `/version`, CI
- [x] Phase 1: PostgreSQL, migrations, `/readyz`, integration tests
- [ ] Phase 2: users and authentication
- [ ] Phase 3: monitors CRUD
- [ ] Phase 4: checks, scheduler, statistics
- [ ] Phase 5: incidents
- [ ] Phase 6: notifications and certificate warnings
- [ ] Phase 7: observability
- [ ] Phase 8: Docker
- [ ] Phase 9: CI/CD
- [ ] Phase 10: documentation and v1.0.0

## License

[MIT](LICENSE)
