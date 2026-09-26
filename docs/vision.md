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

SQLAlchemy is the direct reference for SQLok's developer-facing query and ORM
workflow. Application code should express model selection, composable criteria,
result access, and Session Unit-of-Work operations without coordinating
compiler plans, bind buffers, row scanning, or Identity Map details.

The first functional SELECT slice is:

```go
session := sqlok.NewSession(db)

users, err := sqlok.Select(User{}).
    Where(sqlok.Eq("active", true)).
    All(ctx, session)

user, err := sqlok.Select(User{}).
    Where(sqlok.Eq("id", userID)).
    OneOrNone(ctx, session)
```

`User{}` is a Go type witness; its field values are not read. `Eq` takes a
mapped database-column name and binds the value. `All`, `One`, and `OneOrNone`
return typed mapped entities, and the Session reuses tracked pointers through
its Identity Map. `OneOrNone` limits execution to two rows to detect
non-uniqueness. Typed field descriptors and richer expression operators remain
WIP; this first slice is not the final ergonomics contract.

The required experience is:

- one discoverable `sqlok.Select(...)` model-oriented entry point;
- composable criteria rather than handwritten SQL strings;
- automatic row-to-entity mapping and Identity Map reuse;
- Session execution with reusable prepared plans and no compiler/cache plumbing
  in application code;
- Session Unit-of-Work writes, with the current caller-owned `Flush(ctx, tx)`
  contract clearly documented;
- direct Core and raw `database/sql` escape hatches when the ORM is not the
  right tool.

The module currently targets Go 1.24.0. Its typed query carries the result type
and exposes execution methods that receive a Session; raising the Go floor for
generic concrete methods requires an explicit compatibility decision. Any other
deviation from SQLAlchemy needs a strong, specific SQLok or Go reason.

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

The ORM-facing root constructor is `sqlok.Select(model)`. It returns a typed
query builder that composes mapped-entity criteria and executes through a
Session:

```go
query := sqlok.Select(User{}).
    Where(sqlok.Eq("name", "Ana"))
users, err := query.All(ctx, session)
```

The low-level `sst/dql.Select(columns...)` remains the AST builder used under
the facade and by Core-oriented callers. It returns a concrete
`dql.SelectStatement`, which implements the SST `SelectStatementNode` contract.
Keep that AST boundary structural; ordinary ORM callers should not construct
SST nodes or bind parameters directly.

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

The implemented ORM slice is:

```text
Select(T{}).Where(Eq(column, value)).All/One/OneOrNone(ctx, Session)
  → typed entity SELECT AST
  → Session-private prepared read-plan reuse
  → Executor.Query
  → Mapper.Scan
  → Identity Map reuse/registration and snapshot

Session.Flush(ctx, tx)
  → INSERT pending entities
  → UPDATE dirty tracked entities
  → refresh snapshots after successful statements
```

`LoadContext` and `Load` have been removed; primary-key and composite-key
lookups use normal SELECT criteria. The caller still owns `tx`; Session does
not begin, commit, or roll back it.

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

This table maps observed SQLAlchemy concepts to SQLok's architecture; the
product target remains SQLAlchemy-like developer ergonomics:

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

SQLok follows SQLAlchemy's productive application workflow. The current Go
1.24 floor uses explicit `Eq(column, value)` criteria and typed query methods
instead of overloaded field operators or generic methods on Session. These are
specific language-version adaptations, not reasons to weaken the SQLAlchemy
reference. Relationship loading, cascades, and events remain WIP.

## Current package boundaries

The repository currently uses these top-level engine packages:

```text
sst/        statement roots, clauses, expressions, and visitor contracts
compiler/   validation, rendering, shape identity, caches, and prepared plans
dialect/    rendering contract and default question-mark implementation
executor/   database/sql-compatible execution boundary
mapper.go   public struct metadata, scanning, and values
session.go  public Session Unit of Work, Identity Map, snapshots, and Flush
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

The first typed SELECT vertical slice now composes mapped-entity equality
criteria, executes through Session with cached prepared plans, maps rows, and
reuses Identity Map pointers. `LoadContext` and `Load` were removed in favor of
that public query path.

Continue top-down from the application contract:

1. add richer typed field expressions and comparison operators;
2. define projected/scalar result shapes beyond mapped entities;
3. align Session Unit-of-Work DELETE and transaction lifecycle with the target
   developer workflow;
4. add batch query and relation-loading behavior;
5. update the separately owned SQLite adapter E2E suite after the core API is
   stable.

Relationships, eager/lazy loading, schema fingerprinting, and DDL remain WIP.
