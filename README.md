# sqlok

A Go library for SQL query construction, schema management, and light
ORM-style behavior. The core uses Go's `database/sql`; PostgreSQL is the
current integration-test target.

## Overview

**sqlok** provides a fluent query-builder prototype, a structured SELECT AST
under development, session/identity-map behavior, and reflection-based schema
introspection. The public root package currently exposes the session API; the
legacy builder and schema loader remain under `internal/` while the public API
is being consolidated.

## Features

- **Query Builder** - Legacy fluent builder under `internal/`, being consolidated
- **SELECT AST** - Structured statement/compiler path under active development
- **Session API** - Identity-map and unit-of-work foundations in the root package
- **Schema Management** - Internal table, field, and foreign-key definitions
- **Parameterized Queries** - Builder support for PostgreSQL-style placeholders
- **CLI Interface** - Command-line tools for schema inspection and example generation
- **Type-Safe** - Leverage Go's type system for compile-time safety

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
API is being migrated toward the SELECT AST/compiler path before becoming part
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

- **`session.go`** - Public session and identity-map foundation

- **`schema/`** - Schema definitions
  - `Table` - Represents a database table
  - `Field` - Represents a table column
  - `ForeignKey` - Represents foreign key constraints with reference options

- **`cli/`** - Command-line interface
  - `root.go` - Main CLI command
  - `database.go` - Database operations
  - `init.go` - Schema initialization
  - `example.go` - Example code generation

- **Mapper** - Planned result mapping; no implementation exists yet

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
├── internal/
│   ├── builder.go      # Legacy query builder
│   ├── compiler/       # SQL compiler for the SELECT AST
│   ├── schema/         # Internal schema definitions
│   ├── sst/            # AST contracts and concrete nodes
│   ├── cli/            # CLI commands
│   └── sqlok.go        # Internal database loading
├── session.go          # Public session API
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

- [ ] Add result mapping from database rows to Go values
- [ ] Add UPDATE and DELETE builders
- [ ] Support for additional databases (MySQL, SQLite)
- [ ] Query optimization and performance analysis
- [ ] Extended documentation and examples
