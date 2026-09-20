# sqlok

A Go library for SQL query construction, schema management, and light
ORM-style behavior. The core uses Go's `database/sql`; PostgreSQL is the
current integration-test target.

## Overview

**sqlok** translates SQLAlchemy-style developer ergonomics into idiomatic Go:
expressive query construction, automatic result mapping, coherent Session
behavior, and infrastructure hidden from ordinary application call sites. It
is not a feature-for-feature Python port.

The implemented engine provides SQL Semantic Tree (SST) statement roots, a
dialect-aware compiler, immutable compiled plans, and driver-agnostic execution
on top of `database/sql`. The public root package currently exposes an early
Session and identity-map foundation. Result mapping, database-backed Session
loading, Unit-of-Work flushing, and the cohesive model-oriented facade are the
next ORM milestones.

## Features

- **SQL Semantic Tree** - SELECT, INSERT, UPDATE, and DELETE statement roots
- **Compiler** - Structural validation, bind layouts, shape identities, and SQL rendering
- **Compiled Plans** - Bounded statement cache and stable `PlanRegistry` warm path
- **Driver-Agnostic Execution** - `database/sql`-compatible executor boundary
- **Session Foundation** - Identity Map and pending-entity tracking in the root package
- **Schema Management** - Internal table, field, and foreign-key definitions
- **Legacy Query Builder** - Internal fluent string builder pending consolidation
- **CLI Interface** - Schema inspection and example-generation commands

## Installation

```bash
go get github.com/candango/sqlok
```

### Requirements

- Go 1.24 or higher
- PostgreSQL 12 or higher for integration tests; the core uses `database/sql`

## Quick Start

### Current public API

The root package currently exposes the session and identity-map foundation:

```go
package main

import (
  "database/sql"
  sqlok "github.com/candango/sqlok"
)

func track(db *sql.DB, user *User) error {
  session := sqlok.NewSession(db)
  return session.Add(user)
}
```

The legacy query builder and schema loader are repository-internal today. Their
API is being migrated toward the SELECT SST/compiler path before becoming part
of the stable public package.

### Schema Definition

Schema definitions currently live under `internal/schema` and are not yet part
of the stable public API. Repository-local code can use them as follows:

```go
import "github.com/candango/sqlok/internal/schema"

table := &schema.Table{
  TableName: "users",
  Schema:    "public",
  Fields: []*schema.Field{
    {FieldName: "id", Type: "BIGSERIAL", Primary: true},
    {FieldName: "name", Type: "VARCHAR(255)", Nullable: false},
    {FieldName: "email", Type: "VARCHAR(255)", Nullable: false},
  },
}
```

### Database Connection

The root API accepts an application-provided `*sql.DB`; it does not register a
specific driver or expose a PostgreSQL connection bootstrap. The repository's
schema loader is currently internal and uses `database/sql`.

## Architecture

### Core Packages

- **`internal/builder.go`** - Legacy query builder implementations
  - `QueryBuilder` interface
  - `SelectBuilder`, `InsertBuilder`, `UpdateBuilder`, `DeleteBuilder`
  - Join and condition helpers (`And`, `Or`)

- **`internal/sqlok.go`** - Internal database loading and schema inspection
  - `DatabaseLoader` interface
  - `Loader` implementation
  - Context management

- **`sst/`** - SQL Semantic Tree contracts and concrete nodes
  - Statements, clauses, expressions, references, and visitor traversal

- **`compiler/`** - Dialect-aware SQL rendering and statement-shape
  compilation

- **`dialect/`** - Core Dialect contract and default question-mark rendering;
  vendor adapters remain external

- **`executor/`** - Driver-agnostic execution of compiled plans

- **`session.go`** - Public session and identity-map foundation

- **`internal/schema/`** - Internal schema definitions
  - `Table` - Represents a database table
  - `Field` - Represents a table column
  - `ForeignKey` - Represents foreign key constraints with reference options

- **`cli/`** - Command-line interface
  - `root.go` - Main CLI command
  - `database.go` - Database operations
  - `init.go` - Schema initialization
  - `example.go` - Example code generation

- **Mapper** - Next ORM layer; no implementation exists yet. It will own
  struct metadata, primary-key metadata, column-to-field mapping, row scanning,
  and deterministic value extraction without owning Session state.

- **`internal/namefmt.go`** - Name formatting utilities

## Development

### Running Tests

```bash
make test
```

Tests use PostgreSQL with connection credentials from environment:
- Host: `localhost:5432`
- User: `sqlok`
- Password: Set via `PGSQL_SQLOK_PASSWORD` environment variable

### CI/CD Pipeline

GitHub Actions automatically tests against:
- Go 1.24
- Go 1.25
- Go 1.26

### Project Structure

```
.
├── cmd/sqlok/          # CLI entry point
├── compiler/           # SST compiler, shape cache, and prepared plans
├── dialect/            # Core rendering contract and default dialect
├── executor/           # database/sql-compatible execution boundary
├── sst/                # Statement roots, clauses, expressions, and visitors
├── internal/
│   ├── builder.go      # Legacy query builder
│   ├── schema/         # Internal schema definitions
│   ├── cli/            # CLI commands
│   └── sqlok.go        # Internal database loading
├── session.go          # Early public Session and Identity Map
├── dummy/              # Example models and tests
├── scripts/postgres/   # Database setup scripts
└── makefile            # Build targets
```

## Dependencies

- **[cobra](https://github.com/spf13/cobra)** - CLI framework
- **[logrus](https://github.com/sirupsen/logrus)** - Structured logging
- **[namsral/flag](https://github.com/namsral/flag)** - Flag parsing
- **[testify](https://github.com/stretchr/testify)** - Testing utilities

The core does not depend on a PostgreSQL driver; applications provide their
own `database/sql` driver.

## License

See [LICENSE](LICENSE) file.

## Contributing

Contributions are welcome! Please ensure tests pass before submitting pull requests.

```bash
make test
```

## Roadmap

1. Implement a stateless Mapper for metadata, row scanning, primary keys, and
   deterministic field/value extraction.
2. Refactor Session to consume Mapper metadata instead of performing its own
   reflection.
3. Complete database-backed `Session.Load`: prepared SELECT, row mapping, and
   Identity Map registration/reuse.
4. Implement explicit Unit-of-Work flushing for pending and dirty entities.
5. Deliver the cohesive model-oriented ORM API: expressive queries, automatic
   mapping, Session identity, and Flush without exposing engine plumbing.
6. Add vendor dialect adapters outside the driver-agnostic core as needed.
