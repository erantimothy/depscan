# depscan

`depscan` is a Go dependency scanner for repositories containing one or more
`go.mod` files. It reports raw dependencies together with repository-level
statistics, module summaries, shared dependencies, version conflicts,
ecosystem information, and a dependency graph.

The scanner skips `.git` and `vendor` directories and processes modules with a
bounded worker pool.

## Requirements

- Go 1.26 or newer
- `jq` is only required by the example shell script

## Build and test

```bash
go build -o depscan ./cmd/depscan
go test ./...
go vet ./...
```

## CLI usage

The CLI scans a local repository without starting an HTTP server:

```bash
./depscan scan /path/to/repository
```

By default, the JSON result is written to stdout. Export it to a file with
`--output`; parent directories are created automatically:

```bash
./depscan scan ~/projects/agent-manager \
  --output output/agent-manager-scan.json
```

These equivalent forms are supported:

```bash
./depscan scan --root /path/to/repository
./depscan scan /path/to/repository --output output/scan.json
```

The result contains an `id`, `rootPath`, `modules`, `statistics`,
`moduleSummaries`, `sharedDependencies`, `versionConflicts`, `ecosystems`, and
`graph` fields.

Running the binary without a command starts the HTTP service:

```bash
./depscan
```

## HTTP API

The server listens on `:8080` by default.

Create a local scan:

```bash
curl -sS -X POST http://localhost:8080/scans \
  -H 'Content-Type: application/json' \
  -d '{"rootPath":"/path/to/repository"}' | jq .
```

Create a scan from a public GitHub repository (the repository is downloaded
to a temporary directory):

```bash
curl -sS -X POST http://localhost:8080/scans/remote \
  -H 'Content-Type: application/json' \
  -d '{"owner":"golang","repo":"go","ref":"master"}' | jq .
```

Available endpoints:

| Endpoint | Purpose |
| --- | --- |
| `GET /healthz` | Health check |
| `POST /scans` | Scan a local path |
| `POST /scans/remote` | Download and scan a GitHub repository |
| `GET /scans` | List stored scans |
| `GET /scans/{id}` | Get the complete scan result |
| `GET /scans/{id}/summary` | Get the compact summary |
| `GET /scans/{id}/modules` | List modules |
| `GET /scans/{id}/modules/{moduleID}` | Get one module |
| `GET /scans/{id}/conflicts` | List version conflicts |
| `GET /scans/{id}/duplicates` | List shared dependencies |
| `GET /scans/{id}/graph` | Get the dependency graph |
| `GET /scans/{id}/changes?base={baseID}` | Compare two scans |

The included [`test.sh`](test.sh) exercises the API and saves the complete
scan result under `output/scan-{id}.json`.

## Configuration

Configuration is supplied through environment variables:

| Variable | Default | Description |
| --- | --- | --- |
| `DEPSCAN_HTTP_ADDR` | `:8080` | HTTP listen address |
| `DEPSCAN_MAX_SCAN_WORKERS` | `8` | Maximum concurrent module parsers |
| `DEPSCAN_SHUTDOWN_TIMEOUT` | `10s` | Graceful shutdown timeout |
| `DEPSCAN_LOG_LEVEL` | `info` | Log level |

Example:

```bash
DEPSCAN_HTTP_ADDR=:9090 DEPSCAN_MAX_SCAN_WORKERS=4 ./depscan
```

## Limitations

The core scanner currently analyzes Go modules only. License, vulnerability,
release-age, and latest-version enrichment are intentionally not part of the
core scan yet.
