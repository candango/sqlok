# sqlok

A Go library for PostgreSQL schema management and SQL query building.

## Overview

**sqlok** provides a fluent, type-safe API for building SQL queries and managing PostgreSQL schemas. It combines a query builder pattern with reflection-based schema introspection, making it easy to write database operations without manual SQL string concatenation.

## Features

- **Query Builder** - Fluent API for SELECT, INSERT, UPDATE, DELETE operations
- **Schema Management** - Table, Field, and ForeignKey definitions with constraint support
- **Parameterized Queries** - Safe against SQL injection via PostgreSQL placeholders (`$1`, `$2`, etc)
- **Reflection-Based Tags** - Define schema constraints using Go struct tags (`primary_key`, `unique`, etc)
- **CLI Interface** - Command-line tools for schema inspection and example generation
- **Type-Safe** - Leverage Go's type system for compile-time safety

## Installation

```bash
go get github.com/candango/sqlok
```

### Requirements

- Go 1.24 or higher
- PostgreSQL 12 or higher

## Quick Start

### Query Builder

```go
package main

import "github.com/candango/sqlok"

// SELECT query
sql, args := sqlok.Select("id", "name", "email").
  From("users").
  Where("id=$1", 1).
  Build()
// sql: "SELECT id, name, email FROM users WHERE id=$1"
// args: []any{1}

// INSERT query
sql, args := sqlok.NewInsertBuiler().
  InsertInto("users").
  Columns("name", "email").
  Values("John", "john@example.com").
  Returning("id").
  Build()
// sql: "INSERT INTO users (name, email) VALUES($1, $2) RETURNING id"
// args: []any{"John", "john@example.com"}

// Complex WHERE with AND/OR
sql, args := sqlok.Select("*").
  From("users").
  Where("age=$1", 18).
  And("status=$2", "active").
  Build()
```

### Schema Definition

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

```go
import "github.com/candango/sqlok"

loader := sqlok.NewPostgresLoader("postgresql://user:password@localhost/dbname", ctx)
if err := loader.Connect(); err != nil {
  log.Fatal(err)
}
defer loader.Disconnect()

if err := loader.Load(); err != nil {
  log.Fatal(err)
}

tables := loader.Tables()
```

## Architecture

### Core Packages

- **`builder.go`** (525 LOC) - Query builder implementations
  - `QueryBuilder` interface
  - `SelectBuilder`, `InsertBuilder`, `UpdateBuilder`, `DeleteBuilder`
  - Join and condition helpers (`And`, `Or`)

- **`sqlok.go`** (159 LOC) - Database connection and schema loading
  - `DatabaseLoader` interface
  - `PostgresLoader` implementation
  - Context management

- **`schema/`** - Schema definitions
  - `Table` - Represents a database table
  - `Field` - Represents a table column
  - `ForeignKey` - Represents foreign key constraints with reference options

- **`cli/`** - Command-line interface
  - `root.go` - Main CLI command
  - `database.go` - Database operations
  - `init.go` - Schema initialization
  - `example.go` - Example code generation

- **`mapper.go`** - Result mapping (in development)

- **`namefmt.go`** - Name formatting utilities

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
- Go 1.23
- Go 1.24
- Go 1.25

### Project Structure

```
.
 cmd/sqlok/          # CLI entry point
 internal/
   ├── builder.go      # Query builder (core)
   ├── sqlok.go        # DB connection
   ├── schema/         # Schema definitions
   ├── cli/            # CLI commands
   └── ...
 dummy/              # Example models and tests
 scripts/postgres/   # Database setup scripts
 makefile            # Build targets
```

## Dependencies

- **[pgx/v5](https://github.com/jackc/pgx)** - PostgreSQL driver
- **[cobra](https://github.com/spf13/cobra)** - CLI framework
- **[logrus](https://github.com/sirupsen/logrus)** - Structured logging
- **[testify](https://github.com/stretchr/testify)** - Testing utilities

## License

See [LICENSE](LICENSE) file.

## Contributing

Contributions are welcome! Please ensure tests pass before submitting pull requests.

```bash
make test
```

## Roadmap

- [ ] Complete `mapper.go` for result scanning
- [ ] Add UPDATE and DELETE builders
- [ ] Support for additional databases (MySQL, SQLite)
- [ ] Query optimization and performance analysis
- [ ] Extended documentation and examples
