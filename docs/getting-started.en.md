# Getting Started

This guide covers the shortest supported development path. For the broader project overview and operational notes, see the [Chinese README](../README.md).

## Prerequisites

- Go 1.26 or newer, matching the version declared in `go.mod`
- PostgreSQL 18 or newer running locally
- Redis 7 or newer running locally
- GNU Make

The application requires PostgreSQL and Redis. Redis is a required session dependency, not an optional cache.

## Run Locally

```bash
make migrate
make dev
```

Open `http://127.0.0.1:8080` after the application starts.

`make migrate` checks the local PostgreSQL service with `pg_isready`, maps the `PG*` connection variables to `GOWP_DATABASE_*`, and runs `go run ./cmd -migrate-only`. The command requires a database role with migration privileges. Whether Air automatically runs migrations after each restart is controlled by `database.run_migrations`: set it to `true` for a development database whose application role has DDL privileges; set it to `false` and run `make migrate` manually when using a restricted application role. Redis must also be available when the application starts. `make dev` starts the development process with Air reload support.

Database settings can be overridden with `PGHOST`, `PGPORT`, `PGUSER`, `PGPASSWORD`, and `PGDATABASE`.

## Useful Checks

```bash
go test ./path/to/directly/affected/package
go build ./...
go vet ./...
```

Use focused package tests while developing. `make` lists the repository's other targets. Remove local test databases through PostgreSQL administration when cleanup is needed.

The backend uses Jet templates, native JavaScript, and HTMX. There is no separate frontend bundle to build. Some templates and component assets are embedded in the Go binary, while admin templates and `/static` assets are still served from the filesystem.
