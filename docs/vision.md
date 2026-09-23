# sqlok vision

## Purpose

`sqlok` is a Go library for SQL query construction and light ORM-style behavior.

SQLAlchemy-grade developer ergonomics, translated into idiomatic Go, is a
non-negotiable product goal. Compiler, cache, Mapper, and Session work is not
complete merely because its internal API functions; ordinary application code
must reach it through one coherent, discoverable public workflow.

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

## Developer ergonomics target

The project borrows SQLAlchemy's strongest developer-experience lesson: normal
application code should express domain intent without manually coordinating
compiler, cache, registry, bind layout, row scanning, or Identity Map details.
Those engine boundaries stay explicit internally and remain available to Core
users, but the ORM facade hides them.

Target application shape:

```go
user, err := db.Users().Get(ctx, userID)
active, err := db.Users().Where("active = ?", true).List(ctx)

session := db.Session(tx)
if err := session.Add(user); err != nil {
    return err
}
return session.Flush(ctx)
```

This syntax is directional, not an implemented API contract. The required
experience is:

- one discoverable model-oriented entry point;
- composable query construction rather than handwritten string assembly;
- automatic row-to-entity mapping;
- Identity Map reuse inside a Session;
- Add and Flush semantics for Unit-of-Work behavior;
- compiled-plan reuse with no cache or `PlanID` plumbing in application code;
- direct Core and raw `database/sql` escape hatches when the ORM is not the
  right tool.

Go ergonomics take precedence over Python imitation. SQLok will not reproduce
operator overloading or runtime class machinery; concrete fluent return types,
generics, ordinary errors, and explicit transaction ownership provide the Go
version of the same productive workflow.

### Position alongside sqlc

SQLok does not attempt to replace sqlc on statically known, SQL-first queries.
The two tools occupy complementary execution paths:

```text
Fixed, performance-critical SQL    → sqlc or handwritten database/sql
Runtime-composable query structure → SQLok Core
Entity identity and lifecycle      → SQLok Mapper + Session / Unit of Work
```

SQLok's advantage is developer productivity when query shape or entity
lifecycle is dynamic: composable statements, automatic row mapping, Identity
Map reuse, dirty tracking, and Flush behind one model-oriented API. It must not
claim an execution-speed advantage over generated static query methods.

Applications must remain free to use sqlc for fixed hot paths and SQLok for the
rest through the same application-owned `database/sql` transaction boundary.
Future typed model/column descriptors or optional generated bindings should be
evaluated to recover more compile-time safety without making code generation a
requirement for the ORM.

### DSL

The DSL is the user-facing builder API.

Example shape:

```go
Select("id", "name").From("users").Where(Eq("id", 1)).Build()
```

The DSL should not concatenate final SQL directly forever. Its long-term role is to populate an internal AST.

For SELECT construction, the public entry point is `Select(...)`. It returns a
`SelectStatement`, which is the concrete builder and AST root:

```go
stmt := Select(id, name).
    From(users).
    Join(orders).
    On(userID == orderUserID)
```

`Select` is the SQL-facing constructor; `SelectStatement` is the concrete
statement type. The SST `SelectStatementNode` remains the behavior contract
implemented by `SelectStatement`.

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

A `SelectStatement` root should represent query intent, such as:
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

The AST owns query structure and traversal order through `Accept`. Expression
nodes expose their own `Expr()` representation, while the compiler owns final
SQL rendering, dialect syntax, and argument collection.

#### Node categories

The initial mental model should distinguish statement roots from child nodes:

```text
Statement roots:
  SelectStatement
  Insert
  Update
  Delete

Child/query-shape nodes:
  Table
  Column
  ExpressionNode / BindParamNode
  WhereCriteria
  Join
  Ordering
```

For the first implementation, keep this concrete and small. Do not start by
modeling every SQL feature or forcing abstract node families before the need is
clear.

### Compiler

The compiler receives AST visitor callbacks and produces SQL text plus bound
parameters. Composite AST nodes own structural traversal through `Accept`; the
compiler owns rendering, dialect syntax, and argument collection. A
`BinaryExpression` delegates to its left operand, emits its operator through the
visitor, and then delegates to its right operand.

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

The compiler already consumes a `dialect.Dialect`. Core ships only the default
question-mark implementation; vendor-specific identities, placeholder rules,
and adapters remain outside this driver-agnostic project until a concrete
consumer requires them.

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

## Mapper and Session boundary

The ORM interpretation is intentionally split into a stateless mapping layer
and a stateful Unit of Work.

The Mapper owns structural knowledge about a Go entity:

- recursive struct and embedded-field metadata;
- `sqlok` tags, column names, and primary-key field paths;
- column-to-field scan targets for one database row;
- deterministic extraction of mapped column/value pairs.

The Mapper does not own a database connection, Identity Map, pending queue,
snapshots, dirty state, transaction, statement cache, or flush lifecycle. It
may expose deterministic mapped values that another layer can fingerprint, but
it does not decide whether an entity is dirty.

The Session owns execution-scoped ORM state and coordination:

- Identity Map registration and reuse;
- pending entities;
- snapshots or field fingerprints used for dirty checking;
- load/query orchestration through prepared plans and an Executor;
- flush planning and explicit transaction coordination.

Session must consume Mapper metadata rather than repeat reflection for primary
keys or field traversal. A Session may use shared `StatementCache` and
`PlanRegistry` instances, but it does not own process-wide compiled artifacts.

The implemented ORM slice is deliberately narrow:

```text
LoadContext[T]
  → Identity Map lookup
  → prepared SELECT on miss
  → Executor.Query
  → Mapper.Scan
  → Identity Map registration and snapshot
  → return the same pointer on later loads

Session.Flush(ctx, tx)
  → INSERT pending entities
  → UPDATE dirty tracked entities
  → refresh snapshots after successful statements
```

The caller owns `tx`; Session never begins, commits, or rolls back it.

## Research basis

This vision is informed by SQLAlchemy Core's statement/expression architecture,
but it records the `sqlok` direction rather than copying SQLAlchemy internals.

Source-backed notes and permalinks are kept in:

- [`docs/research.md`](research.md)

The main imported lessons are:
- keep the public builder as the DSL
- use statement roots such as `Select`, `Insert`, `Update`, and `Delete`
- prefer `Criteria` / `WhereCriteria` vocabulary for WHERE filtering
- keep AST nodes structural and responsible for child traversal
- keep final SQL rendering in a compiler/dialect boundary

This is an architectural translation into Go, not a feature-parity project:

| Source concept | SQLok interpretation |
|---|---|
| SQL expression tree | SST statement roots and structural child nodes |
| SQL compiler and dialects | `compiler` plus the minimal `dialect.Dialect` contract |
| Structural compilation cache | canonical `ShapeKey` plus bounded `StatementCache` |
| Known compiled queries | immutable `CompiledStatement` published by `PlanID` |
| Mapper | stateless Go struct metadata, row scanning, and value extraction |
| Session / Identity Map / Unit of Work | explicit Session state over Mapper and Core execution |
| Engine / Connection | application-owned `database/sql` handles through `Executor` |
| Declarative models | future Go structs, tags, and generic APIs rather than runtime class machinery |

The project deliberately does not copy Python operator overloading, automatic
driver ownership, implicit transaction boundaries, or a mandatory ORM entry
point. It does aim to match the productive application workflow: expressive
queries, automatic mapping, coherent Session behavior, and infrastructure that
stays out of ordinary call sites. Relationship loading, cascades, events, and
broad SQL feature parity are later scope; they do not gate the first working
Mapper/Session slice.

## Current package boundaries

The repository currently uses these top-level engine packages:

```text
sst/        statement roots, clauses, expressions, and visitor contracts
compiler/   validation, rendering, shape identity, caches, and prepared plans
dialect/    rendering contract and default question-mark implementation
executor/   database/sql-compatible execution boundary
mapper.go   public struct metadata, scanning, and values
session.go  public Identity Map, Load, snapshots, and Flush
```

The legacy string builder and schema loader remain under `internal/`. DDL
remains future scope.

The package boundary is behavioral:

- SST owns query structure and traversal order;
- compiler owns validation, SQL rendering, binding layout, and shape identity;
- dialect owns variable rendering rules;
- executor owns the call into an application-provided database handle;
- Mapper owns entity metadata and row/value translation;
- Session owns entity identity and Unit-of-Work lifecycle.

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
- use the Visitor pattern as the established AST pattern
- keep the first implementation small
- let composite nodes delegate child `Accept` calls in SQL order
- let dedicated visitor methods handle bind parameters and parameter slots;
  placeholder allocation remains compiler/dialect behavior
- do not put final `String()` / `ToSQL()` behavior on AST nodes; `Expr()` is the
  expression-level representation used by the current AST contract
- keep final SQL rendering in the compiler
- keep `Compile()` as an open API decision: a method is ergonomically attractive,
  but must delegate to compiler logic if adopted

## Core scope

Things that belong in `sqlok` core:
- DSL / builder API
- AST model
- compiler
- dialect abstraction and default rendering behavior
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

The engine now has SELECT, INSERT, UPDATE, and DELETE SST roots; visitor-based
compilation; bind and named parameter slots; dialect-owned placeholders;
canonical shape keys; bounded statement caching; stable prepared-plan lookup;
and a driver-agnostic Executor. The prepared named-binding path is validated
end to end and remains allocation-free in the current benchmark.

The missing bridge is ORM mapping and lifecycle, not another cache layer. Work
should proceed in this order:

1. implement a stateless Mapper and its metadata/scan tests;
2. refactor Session primary-key handling to consume Mapper metadata;
3. implement database-backed `Session.Load` through prepared SELECT execution;
4. prove Identity Map reuse with an end-to-end test;
5. implement explicit Flush planning for pending and dirty entities;
6. consolidate the public DSL/ORM API without making Session mandatory for
   Core users.

Relationships, eager/lazy loading, automatic prepared-query promotion, schema
fingerprinting, and DDL remain later decisions. They must not be mixed into the
first Mapper/Session vertical slice.
