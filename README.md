# sqlok

A Go library for SQL query construction, schema management, and light
ORM-style behavior. The core uses Go's `database/sql`; PostgreSQL is the
current integration-test target.

## Overview

**sqlok** uses SQLAlchemy as the direct reference for developer ergonomics,
expressed through Go's types and APIs: composable queries, automatic result
mapping, coherent Session behavior, and infrastructure hidden from ordinary
application call sites. Deviations require a concrete SQLok or Go constraint.

The implemented engine provides SQL Semantic Tree (SST) statement roots, a
dialect-aware compiler, immutable compiled plans, and driver-agnostic execution
on top of `database/sql`. The public root package exposes a stateless Mapper
and a Session Unit of Work with typed SELECT queries, automatic result mapping,
Identity Map reuse, and explicit transactional flushing.

## Features

- **SQL Semantic Tree** - SELECT, INSERT, UPDATE, and DELETE statement roots
- **Compiler** - Structural validation, bind layouts, shape identities, and SQL rendering
- **Compiled Plans** - Bounded statement cache and stable `PlanRegistry` warm path
- **Driver-Agnostic Execution** - `database/sql`-compatible executor boundary
- **Mapper and Session** - Typed SELECT queries, struct mapping, Identity Map reuse, INSERT/UPDATE flushing, and numeric generated keys via `LastInsertId`
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

### SELECT and Session

Build a typed entity query with `sqlok.Select`, then execute it through the
Session. `Eq`, `Ne`, `Gt`, `Gte`, `Lt`, and `Lte` bind non-NULL values, while
`IsNull` and `IsNotNull` render NULL predicates without binds. Repeated `Where`
calls compose with AND, and comparison criteria reject nil rather than emitting
incorrect `= NULL` SQL. `All` returns mapped entities, while `OneOrNone` returns
nil for no match and reports an error if more than one row matches.

```go
package main

import (
  "context"
  "database/sql"

  sqlok "github.com/candango/sqlok"
)

type User struct {
  ID   int    `sqlok:"column=id,pk"`
  Name string `sqlok:"column=name"`
}

func (*User) TableName() string { return "users" }

func rename(ctx context.Context, db *sql.DB, id int, name string) error {
  session := sqlok.NewSession(db)
  user, err := sqlok.Select(User{}).
    Where(sqlok.Eq("id", id)).
    OneOrNone(ctx, session)
  if err != nil {
    return err
  }
  if user == nil {
    return sql.ErrNoRows
  }

  tx, err := db.BeginTx(ctx, nil)
  if err != nil {
    return err
  }
  defer tx.Rollback()

  user.Name = name
  if err := session.Flush(ctx, tx); err != nil {
    return err
  }
  return tx.Commit()
}
```

`Select(User{})` uses the value only as a Go type witness. Results are mapped
and tracked in the Session Identity Map. For mapped-column projections, use
`Columns(...).All(...)` for `SelectRow` values or `Columns(one).Scalars(...)`
for a single scalar column:

```go
rows, err := sqlok.Select(User{}).
  Columns("id", "name").
  Where(sqlok.IsNotNull("name")).
  All(ctx, session)
values, err := sqlok.Select(User{}).
  Columns("name").
  Scalars(ctx, session)
```

`SelectRow` exposes `Columns`, `Values`, and `Value(column)`. Composite-key
queries use ordinary criteria, for example `Where(Eq("tenant_id", 7),
Eq("user_id", 11))`; NULL queries use `Where(IsNull("deleted_at"))`; no
`CompositeKey` value is exposed. Session writes remain caller-transactional:
`Flush` never begins, commits, or rolls back a transaction. SELECT currently
uses the Session's `*sql.DB` and does not autoflush pending/dirty entities;
transaction-bound reads and autoflush remain WIP.

The legacy query builder and schema loader remain repository-internal.

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

- **`mapper.go`** - Public stateless struct metadata, scanning, and values
- **`session.go`** - Public Session Unit of Work, Identity Map, and explicit flushing

- **`internal/schema/`** - Internal schema definitions
  - `Table` - Represents a database table
  - `Field` - Represents a table column
  - `ForeignKey` - Represents foreign key constraints with reference options

- **`cli/`** - Command-line interface
  - `root.go` - Main CLI command
  - `database.go` - Database operations
  - `init.go` - Schema initialization
  - `example.go` - Example code generation

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
├── mapper.go           # Public Mapper metadata and row mapping
├── session.go          # Public Session, Identity Map, Load, and Flush
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

1. Extend the cohesive model-oriented ORM API with richer query ergonomics.
2. Add relationship loading, cascades, and lifecycle features only when their
   behavior has a concrete application consumer.
3. Add vendor dialect adapters outside the driver-agnostic core as needed.
