# Dependency Scanner Improvement Ideas

## Goal

Turn the scanner from a raw dependency inventory into a dependency
intelligence service that is useful for: - Humans - CI/CD pipelines -
Vulnerability scanners - LLMs working on large Go codebases

## Current strengths

-   Multi-module awareness
-   Exact versions
-   Direct vs indirect dependencies
-   Stable hierarchical structure (`Scan -> Modules -> Dependencies`)
-   Suitable foundation for SBOM generation

## Recommended enhancements

### 1. Repository statistics

Include aggregate metrics:

``` json
{
  "moduleCount": 4,
  "directDependencies": 29,
  "indirectDependencies": 71,
  "uniqueDependencies": 84
}
```

### 2. Version conflict detection

Report packages used at different versions across modules.

### 3. Duplicate dependency report

List shared dependencies and which modules use them.

### 4. Ecosystem summary

Summarize major technology groups (Kubernetes, Hashicorp, GORM, etc.).

### 5. Dependency graph

Provide parent/child relationships to explain *why* a dependency exists.

### 6. Module summaries

Per module include: - purpose (optional) - direct count - indirect
count - top-level frameworks - notable external systems (DB, Kubernetes,
Vault, OAuth)

### 7. Optional enrichments

-   Licenses
-   Vulnerabilities (OSV/GHSA/NVD)
-   Release age
-   Latest available version
-   Maintenance status

Keep these optional so the core scan stays fast.

## Making the output LLM-friendly

Large dependency lists waste context. Add compact summaries.

### Recommended top-level schema

``` text
Scan
├── Metadata
├── Statistics
├── Module Summaries
├── Shared Dependencies
├── Version Conflicts
├── Ecosystem Summary
├── Dependency Graph
└── Raw Dependencies
```

### Module summary example

``` yaml
module: agent-manager-service
go: 1.25.7

frameworks:
  - GORM
  - Vault
  - Kubernetes
  - MCP SDK

external_systems:
  - PostgreSQL
  - Vault

direct: 18
indirect: 56
```

An AI can usually answer architectural questions from this without
reading 70+ dependencies.

### Shared dependency table

``` text
github.com/google/uuid
  agent-manager-service
  cli
  e2e

golang.org/x/oauth2
  service -> v0.35.0
  cli -> v0.36.0
```

### AI-oriented tags

Tag dependencies by capability:

-   database
-   auth
-   kubernetes
-   websocket
-   messaging
-   telemetry
-   testing
-   codegen
-   configuration
-   security
-   cloud

This enables semantic search without scanning every package name.

### Dependency reasoning

Store "why" a package exists.

Example:

``` yaml
gorm.io/gorm:
  reason: ORM
  category: database
```

### Stable identifiers

Assign IDs to modules and dependencies so future scans can diff
efficiently.

### Change summaries

Between scans emit: - added - removed - upgraded - downgraded

Ideal for AI review and PR summaries.

## Token-saving strategy

Instead of sending every dependency to an LLM:

1.  Send repository statistics.
2.  Send module summaries.
3.  Send ecosystem summary.
4.  Send conflicts/shared dependencies.
5.  Only expand raw dependency lists on demand.

This often reduces prompt size dramatically while preserving
architectural understanding.

## Suggested API

-   `/scan`
-   `/summary`
-   `/modules`
-   `/module/{id}`
-   `/conflicts`
-   `/duplicates`
-   `/graph`
-   `/changes`
-   `/sbom`

## Overall vision

Treat the scanner as a dependency knowledge engine rather than a JSON
exporter. The raw inventory is the source of truth, while higher-level
summaries, graphs, and analyses make it valuable to humans and AI
systems alike.
