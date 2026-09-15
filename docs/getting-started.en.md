# Getting Started

This guide covers the shortest supported development path. For the broader project overview and operational notes, see the [Chinese README](../README.md).

## Prerequisites

- Go 1.26 or newer, matching the version declared in `go.mod`
- Docker with Docker Compose
- GNU Make

The application requires PostgreSQL and Redis. Redis is a required session dependency, not an optional cache.

## Run Locally

```bash
make up
make migrate
make dev
```

Open `http://127.0.0.1:8080` after the application starts.

`make up` starts PostgreSQL and Redis. `make migrate` waits for PostgreSQL; migrations and seeds run during application startup because there is no separate migration command. Redis must also be available when the application starts. `make dev` starts the development process with Air reload support.

Database settings can be overridden with `PGHOST`, `PGPORT`, `PGUSER`, `PGPASSWORD`, and `PGDATABASE`. Their defaults match `docker-compose.yml` and `config.yaml`.

## Useful Checks

```bash
go test ./path/to/directly/affected/package
go build ./...
go vet ./...
```

Use focused package tests while developing. `make` lists the repository's other targets. `make clean-data` removes the local database and Redis volumes; it is the destructive local target.

The backend uses Jet templates, native JavaScript, and HTMX. There is no separate frontend bundle to build. Some templates and component assets are embedded in the Go binary, while admin templates and `/static` assets are still served from the filesystem.
