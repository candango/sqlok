# sqlok progress

## Current vision

`sqlok` should be a core library for SQL query building and light ORM/session behavior.

Architecture direction agreed so far:
- core must speak only through `database/sql`
- core must not bundle or depend on a specific database driver
- real database validation is still required, but belongs to integration testing
- if a dedicated adapter layer is ever needed, it should live in a separate project
- scope here is SQL queries, mapper/session behavior, and light ORM concerns; driver-specific features are out of core scope

## What we achieved so far

### Builder consolidation
- created a dedicated initiative for builder work: `Builder Consolidation`
- linked the focused task to GitHub issue `#6` (`Define the sql builder`)
- fixed the `SELECT ... OFFSET` rendering bug
- cleaned up builder constructor naming from `Builer` to `Builder`
- consolidated builder-facing API updates already present in the worktree
- updated tests and example usage to match the current builder API
- validated the builder changes with the test suite
- committed and pushed the builder consolidation work

### CI / dependency work
- updated GitHub Actions Go matrix to drop `1.23` and add `1.26`
- aligned CI minimum with `go.mod` (`go 1.24.0`)
- bumped `github.com/sirupsen/logrus` from `v1.9.3` to `v1.9.4`
- validated the dependency bump with the test suite
- committed and pushed the logrus bump

### Architecture understanding clarified
- `sqlok` should remain driver-agnostic in core
- testing against real databases is still desired:
  - PostgreSQL
  - MySQL/MariaDB
  - SQLite
- real drivers can be used in test-only integration code without making them part of the core contract
- created GitHub issue `#10` (`Stabilize driver-agnostic core architecture`)
- created local initiative `Core Stabilization`
- removed `pgx` coupling from the current core worktree by switching loader behavior to application-provided `*sql.DB`
- neutralized CLI/example flows that previously opened driver connections from inside core
- `pgx` has been removed from the active `go.mod` / `go.sum` worktree state

## Dirty worktree snapshot

Current uncommitted paths:

### Modified
- `README.md`
- `internal/cli/database.go`
- `internal/schema/schema.go`
- `internal/sqlok.go`

### Untracked
- `dummy/`
- `internal/mapper.go`
- `internal/mapper_test.go`
- `internal/namefmt.go`
- `internal/namefmt_test.go`
- `internal/schema/schema_test.go`
- `internal/session.go`

## TODOs from current dirty state

### High priority
- define how to remove driver coupling from `internal/sqlok.go`
- keep core on top of `database/sql` only
- decide the first shape of integration testing for PostgreSQL / MySQL/MariaDB / SQLite

### Files that need triage
- `internal/sqlok.go`
  - inspect current `pgx` / driver coupling
  - identify what must move to pure `database/sql`
- `internal/cli/database.go`
  - decide whether this remains a CLI utility or needs separation from core concerns
- `internal/schema/schema.go`
  - review current schema changes and confirm whether they belong to the same architectural move
- `README.md`
  - update docs once the driver-agnostic direction is finalized

### New / untracked work to classify
- `internal/mapper.go`
- `internal/mapper_test.go`
- `internal/session.go`
- `internal/namefmt.go`
- `internal/namefmt_test.go`
- `internal/schema/schema_test.go`
- `dummy/`

Questions to answer during triage:
- is this part of the core roadmap?
- is this experimental or production-bound?
- does this depend on driver-specific behavior?
- should this be committed together, split, or discarded?

## Recommended next move

Before starting the next implementation wave:
1. triage the dirty files into coherent chunks
2. isolate driver-coupled code paths
3. define the integration test entrypoints for real databases
4. only then start the driver-removal refactor from core
