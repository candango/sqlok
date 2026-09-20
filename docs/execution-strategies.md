# Execution Strategies

This document defines how SQLok statements reach the database and where the
Core, cache, ORM, and application-owned driver boundaries belong.

## Driver boundary

SQLok core must remain driver-agnostic. The application owns the concrete
`database/sql` connection and transaction objects. SQLok consumes the standard
execution contract instead of importing a vendor driver or exposing a driver-
specific API.

A minimal compatible executor is:

```go
type Executor interface {
    ExecContext(context.Context, string, ...any) (sql.Result, error)
    QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}
```

The standard library types already satisfy this contract:

```text
*sql.DB   → database-level execution
*sql.Tx   → transaction-scoped execution
*sql.Conn → connection-scoped execution
```

The current core implementation lives in `executor`. Its `Executor` contract
is satisfied by standard `database/sql` handles. The behavior boundary
is the important part: SQLok needs execution capability, not ownership of a
particular driver.

SQLok must not require callers to pass a concrete PostgreSQL, MySQL, SQLite,
or other vendor driver. Applications remain free to choose and configure the
driver behind `database/sql`.

## Three execution tiers

SQLok should support three deliberate paths:

| Path | Purpose | Runtime characteristics |
|---|---|---|
| Direct SQL or sqlc-like generated code | Maximum control and performance-critical queries | SQL and argument layout are already known; minimal SQLok overhead |
| SQLok Core | CRUD, composable statements, and ordinary transactions | DSL/SST/compiler with optional compiled-shape cache |
| SQLok ORM | Struct mapping, Session, identity map, and Unit of Work | Highest convenience; reflection and lifecycle coordination are optional costs |

The ORM is not the mandatory entry point. A performance-critical operation may
bypass the ORM and use direct SQL or generated code while the rest of the
application uses SQLok Core or the ORM layer.

Developer ergonomics is a primary acceptance criterion, not optional polish.
The public ORM path must not require callers to manage `StatementCache`,
`PlanRegistry`, `CompiledStatement`, bind layouts, `ArgumentBuffer`, reflection
metadata, or Identity Map registration. Those are engine responsibilities.
Application code should operate in terms of models, queries, Sessions, and
explicit transaction scopes.

## Core execution

The Core path compiles a statement shape once and delegates each execution to
an application-owned executor:

```go
plan, err := compiler.CompileShape(stmt)
if err != nil {
    return err
}

_, err = executor.Exec(ctx, tx, plan, currentArgs)
return err
```

`executor.Exec` and `executor.Query` support both binding contracts. Unnamed
legacy layouts use positional `[]any`; named reusable layouts use a
shape-owned `ArgumentBuffer`, which validates logical slot identity and emits
values in SQL order before the database call.

Stable repeated operations should prepare once and execute by `PlanID`:

```go
plan, err := compiler.Prepare(cache, stmt, renderingDialect)
if err != nil {
    return err
}
if err := registry.Put("users.get", plan); err != nil {
    return err
}

plan, ok := registry.Get("users.get")
if !ok {
    return errors.New("prepared plan not found")
}
args := plan.NewArgumentBuffer()
if err := args.Set("user_id", userID); err != nil {
    return err
}
_, err = executor.Query(ctx, tx, plan, args)
return err
```

The higher-level ORM should hide cache and registry mechanics from application
code; this explicit form documents the engine boundary.

The Core path owns statement structure, validation, SQL rendering, and bound
argument ordering. The executor owns connection selection and the actual
operation against the database.

For reads, the same boundary applies:

```go
rows, err := tx.QueryContext(ctx, sqlText, args...)
```

SQLok should not hide the returned `*sql.Rows` behind an ORM mapper when the
caller intentionally chose the Core path.

## Direct SQL escape hatch

Some operations will be too vendor-specific, too performance-sensitive, or too
unusual for a generic SST builder. Those operations should be able to use the
same application-owned `Executor` directly:

```go
rows, err := tx.QueryContext(
    ctx,
    `SELECT id, payload FROM events
     WHERE tenant_id = $1
     ORDER BY created_at DESC
     LIMIT $2`,
    tenantID,
    limit,
)
```

This is an explicit escape hatch, not a reason for SQLok core to import or
expose a concrete driver. Runtime values remain bound arguments. Raw SQL
identifiers and syntax remain the caller's responsibility and should be
reviewed as trusted input.

A future convenience helper may accept a SQLok raw statement, but it must
preserve the same separation:

```text
trusted SQL template + bound args → Executor
```

It must not interpolate request values into a raw string.

## Transaction ownership

The application should own the transaction boundary. SQLok can help build and
flush statements, but it should not silently begin, commit, or roll back a
transaction behind the caller's back.

The explicit Core flow is:

```go
tx, err := db.BeginTx(ctx, nil)
if err != nil {
    return err
}

defer tx.Rollback()

sqlText, args, err := compiler.CompileWithDialect(stmt, renderingDialect)
if err != nil {
    return err
}
if _, err := tx.ExecContext(ctx, sqlText, args...); err != nil {
    return err
}

return tx.Commit()
```

The deferred rollback is harmless after a successful commit and preserves the
failure path when an intermediate operation returns an error.

A future Session/Unit of Work may coordinate several statements inside a
transaction, but the transaction's ownership and lifecycle must remain
explicit in the API contract.

## ORM execution

The ORM layer sits above Core. Session coordinates the operation; Mapper
translates between database rows/values and Go entities:

```text
Read:
  ORM API → Session → prepared SELECT → Executor.Query
          → Mapper.Scan → Identity Map → entity

Write:
  ORM API → Session.Flush → Mapper metadata/values
          → SST/DML → compiled plan → Executor backed by *sql.Tx
```

Mapper is stateless with respect to execution. It owns:

- recursive struct and embedded-field metadata;
- column names, tags, primary-key paths, and scan targets;
- row-to-entity mapping;
- deterministic extraction of mapped field/value pairs.

Mapper does not own Identity Map entries, snapshots, pending entities,
transactions, caches, or flush decisions.

Session owns ORM state and lifecycle:

- identity-map lookups and pointer reuse;
- pending new entities;
- snapshots or field fingerprints for dirty checking;
- database-backed load/query orchestration;
- flush planning;
- coordinating multiple CRUD statements in one explicit unit of work.

A Session is not responsible for owning the global compiled-statement cache.
The cache can be shared across Sessions, while identity and transaction state
remain scoped to the current Session/request.

The first ORM acceptance path is:

```text
Session.Load(id)
  → return tracked pointer on Identity Map hit
  → on miss, execute one prepared SELECT
  → map one row through Mapper
  → register and snapshot the entity
  → return that same pointer on subsequent loads
```

Only after this path passes end to end should Flush add INSERT handling for
pending entities and UPDATE handling for dirty persistent entities.

The Session path is valuable for domain productivity and consistency. It is
not the default recommendation for the hottest performance-sensitive query.
When performance is the primary constraint, direct SQL or generated code is the
first path to evaluate; the ORM is the first layer that may be removed from
that operation.

## Performance model

The benchmark comparison must keep application overhead separate from database
latency:

```text
Direct SQL/sqlc-like:
  prepared SQL + current args → Executor

SQLok Core cold:
  build AST + compile + current args → Executor

SQLok Core warm:
  PlanRegistry + ArgumentBuffer + current values → Executor

SQLok ORM:
  map struct + Session/flush + cached shape + current args → Executor
```

The direct SQL baseline is useful but optimistic when it only measures a
preassembled string. A complete benchmark should eventually include equivalent
statement shapes and values, driver execution, and a controlled database or
query benchmark environment.

The current compiler POC isolates the application-side construction and
compilation cost. It intentionally excludes Session, network, driver, and
server-side query planning so the cache's local benefit can be measured before
those variables are introduced.

## Design rule

SQLok's execution policy is therefore:

```text
SQLok Core → Compile → executor.ExecContext(...)
SQLok ORM  → Session.Flush → SQLok Core → tx.ExecContext(...)
SQL critical → sqlc or handwritten SQL → tx.ExecContext(...)
```

No single abstraction must win every workload. SQLok should make the safe,
composable Core path pleasant, make the ORM path optional, and leave a clear,
standard-library-compatible escape hatch for operations where performance or
vendor-specific SQL is more important than generic abstraction.
