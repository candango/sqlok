# sqlok vision

## Purpose

`sqlok` is a Go library for SQL query construction and light ORM-style behavior.

The project should stay focused on:
- query construction
- structured query representation
- SQL compilation
- mapper behavior
- session / identity-map / unit-of-work behavior
- execution contracts based on Go's `database/sql`

## Core principle

`sqlok` core must be driver-agnostic.

The core must not:
- import a concrete database driver
- register a driver
- open driver-specific connections
- ship vendor-specific adapter behavior

The core may:
- accept application-provided `*sql.DB` / `*sql.Tx`
- build SQL statements
- compile SQL and parameters
- map rows to objects
- coordinate lightweight ORM/session behavior

If dedicated adapters are needed later, they must live outside this project.

## Architecture pipeline

The guiding architecture is:

```text
DSL → AST → Compiler → Dialect → SQL + params
```

### DSL

The DSL is the user-facing builder API.

Example shape:

```go
Select("id", "name").From("users").Where(Eq("id", 1)).Build()
```

The DSL should not concatenate final SQL directly forever. Its long-term role is to populate an internal AST.

### AST

The AST is the structured representation of a SQL statement before it becomes a string.

The top-level AST nodes are statement roots. A `SELECT` query is rooted at a
`Select` statement. `INSERT`, `UPDATE`, and `DELETE` have their own statement
roots as well.

```text
Select → root of a SELECT statement
Insert → root of an INSERT statement
Update → root of an UPDATE statement
Delete → root of a DELETE statement
```

Statement roots own the shape of the whole query operation. Smaller nodes hang
below them.

A `Select` root should represent query intent, such as:
- columns clause items, named `Columns` in `sqlok`
- relational sources
- joins
- where criteria
- literals / bind values
- ordering / grouping later

`Columns` is the chosen SELECT vocabulary for now. It follows the familiar SQL
and SQLAlchemy direction (`_raw_columns`, `selected_columns`) while remaining
ergonomic. In `sqlok`, `Columns` means the selected expressions in the SELECT
columns clause, not only physical table columns.

SELECT source handling should follow the SQLAlchemy 1.4+ safety direction:
advanced multi-source SELECT shapes may be allowed, but accidental cartesian
products must not be silent. The normal path should be a primary source plus
explicit joins. If the AST contains disconnected FROM elements, compilation or
validation should emit a diagnostic warning. Intentional cross joins or other
cartesian shapes must be represented explicitly so the query author's intent is
clear.

DML roots should represent their own operation-specific shape:
- `Insert`: target table, values, optional insert-from-select, returning
- `Update`: target table, values/set clauses, where criteria, returning
- `Delete`: target table, where criteria, returning

The AST is not responsible for rendering SQL.

#### Node categories

The initial mental model should distinguish statement roots from child nodes:

```text
Statement roots:
  Select
  Insert
  Update
  Delete

Child/query-shape nodes:
  Table
  Column
  Literal / Bind
  WhereCriteria
  Join
  Ordering
```

For the first implementation, keep this concrete and small. Do not start by
modeling every SQL feature or forcing abstract node families before the need is
clear.

### Compiler

The compiler walks the AST and produces SQL text plus bound parameters.

It owns traversal/rendering logic, not the AST nodes themselves.

Open design choice: `Compile()` may be exposed as an ergonomic method on
statement roots while still delegating the real work to the compiler boundary.
This would make the public API pleasant without requiring SQL rendering logic to
live inside the statement node.

Example shape:

```go
stmt := Select("id").From("users").Where(Eq("age", 18))
sql, args, err := stmt.Compile()
```

Alternative shape:

```go
stmt := Select("id").From("users").Where(Eq("age", 18))
sql, args, err := compiler.Compile(stmt)
```

Decision is still open between:
- ergonomic statement method: `stmt.Compile()`
- passive statement/AST plus explicit compiler: `compiler.Compile(stmt)`

### Dialect

The dialect owns database-specific SQL rules, such as:
- placeholders (`$1`, `?`, `:name`)
- identifier quoting
- vendor-specific syntax differences

Dialect support should be introduced only when needed. The first implementation can start with one default compiler and grow from there.

### SQL + params

The output of building/compiling is:

```go
sql  string
args []any
```

This keeps query generation separate from query execution.

Statement values must be represented as bind parameters, not concatenated into
SQL text. User-controlled values should flow into `args`, while the compiler and
dialect decide the placeholder syntax (`$1`, `?`, `:name`, etc.).

Security direction:
- never interpolate user values directly into SQL strings
- model bind/literal values as AST nodes before compilation
- keep identifier rendering separate from value binding
- let dialects own placeholder formatting and identifier quoting rules
- add tests that prove generated SQL uses placeholders and carries values in
  `args`

This is the main SQL injection boundary for `sqlok`: the AST and compiler must
make the safe path the default path.

## Research basis

This vision is informed by SQLAlchemy Core's statement/expression architecture,
but it records the `sqlok` direction rather than copying SQLAlchemy internals.

Source-backed notes and permalinks are kept in:

- [`docs/research.md`](research.md)

The main imported lessons are:
- keep the public builder as the DSL
- use statement roots such as `Select`, `Insert`, `Update`, and `Delete`
- prefer `Criteria` / `WhereCriteria` vocabulary for WHERE filtering
- keep AST nodes structural
- keep SQL rendering in a compiler/dialect boundary

## Proposed package shape

Preferred package direction:

```text
internal/
  ast/
    ast.go          # base AST contracts only: Node, Statement, Visitor shape

  dql/
    select.go       # SELECT statement root

  dml/
    insert.go       # INSERT statement root
    update.go       # UPDATE statement root
    delete.go       # DELETE statement root

  ddl/
    create.go       # future CREATE support
    alter.go        # future ALTER support
    drop.go         # future DROP support

  elements/
    column.go       # shared expression/source/criteria nodes
    literal.go
    criteria.go
    binary.go

  compiler/
    compiler.go     # Statement/AST → SQL + args
```

Package responsibility:
- `ast`: base contracts for tree/statement behavior only
- `dql`: query-language statement roots, starting with `Select`
- `dml`: data-manipulation statement roots: `Insert`, `Update`, `Delete`
- `ddl`: data-definition statement roots later, such as `Create`, `Alter`, `Drop`; package is reserved now, implementation comes in a later phase
- `elements`: shared nodes used by DQL/DML/DDL, such as columns, binds, and criteria
- `compiler`: SQL rendering and argument collection

The builder remains the public DSL and should eventually populate statement
roots instead of assembling final SQL directly.

DDL is part of the long-term package model, but it is not part of the current
end-to-end SELECT slice. Implement DDL only in a later phase.

Because the preferred package layout separates `dql`, `dml`, `ddl`, and
`elements` from `ast`, the Visitor design must avoid Go import cycles. This may
favor compiler-owned dispatch or a Visitor abstraction that does not require
`ast` to import concrete statement packages.

Open SELECT interface questions:
- `SelectNode` may live in the base tree package as a behavior interface.
- `SelectNode` should not be a marker-only interface.
- A useful first behavior is `Columns() []Node`.
- Do not introduce `Expression` yet if it only duplicates `Node` or creates
  package-cycle pressure.
- Revisit `Expression` when concrete nodes like Column, Literal, FunctionCall,
  or BinaryExpression exist and the behavior boundary is clearer.

## Visitor and traversal

AST traversal can be implemented in more than one way.

Two viable options:

1. Visitor pattern
   - nodes expose an `Accept(visitor)` method
   - compiler implements visitor methods
   - useful when multiple operations over the tree are expected

2. Compiler-owned dispatch
   - compiler receives nodes and uses type switches or internal dispatch
   - closer to simple Go style for an MVP
   - less boilerplate at the beginning

Decision for now:
- understand the Visitor pattern as the established AST pattern
- keep the first implementation small
- do not put `String()` / `ToSQL()` behavior on AST nodes
- keep SQL rendering in the compiler
- keep `Compile()` as an open API decision: a method is ergonomically attractive,
  but must delegate to compiler logic if adopted

## Core scope

Things that belong in `sqlok` core:
- DSL / builder API
- AST model
- compiler
- eventual dialect abstraction
- mapper
- identity map
- dirty checking
- unit of work
- `database/sql` execution contracts

Things that do not belong in core:
- `pgx`
- MySQL driver
- SQLite driver
- driver registration
- driver-specific connection bootstrap
- vendor-specific adapters

## Current direction

Current builder code started as string construction. The next architectural step is to introduce a minimal AST path and gradually move builder behavior toward:

```text
builder method calls → AST nodes → compiler output
```

The learning path should start with statement roots before attempting full SQL coverage:
- define `Select` as the root of a SELECT statement
- define minimal child nodes for projection(s), table source, and where criteria
- compile `Select` into SQL + args
- then define `Insert`, `Update`, and `Delete` as separate statement roots
- compile each root through the same compiler boundary

Then grow into:
- joins
- expression projections
- criteria composition
- subqueries
- dialect-specific compilation
