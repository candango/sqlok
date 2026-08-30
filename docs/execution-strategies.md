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

The current core implementation lives in `internal/executor`. Its `Executor`
contract is satisfied by standard `database/sql` handles. The behavior boundary
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

`executor.Exec` binds the current values through `CompiledStatement.Bind` before
calling `ExecContext`. `executor.Query` provides the equivalent read path.

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

sqlText, args, err := stmt.Compile()
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

The ORM layer sits above Core:

```text
SQLok ORM
  → struct mapper
  → Session / Unit of Work
  → flush
  → SQLok Core
  → Executor backed by *sql.Tx
```

A Session is responsible for ORM state such as:

- identity-map lookups;
- pending new entities;
- dirty-field or snapshot tracking;
- flush planning;
- coordinating multiple CRUD statements in one unit of work.

A Session is not responsible for owning the global compiled-statement cache.
The cache can be shared across Sessions, while identity and transaction state
remain scoped to the current Session/request.

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
  cached shape + current args → Executor

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
