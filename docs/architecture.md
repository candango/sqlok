# sqlok architecture

This document records the architecture decisions currently adopted by `sqlok`.
It describes the implemented SELECT SST slice and the construction boundaries
we have chosen so far. It does not claim that the long-term architecture in
[`vision.md`](vision.md) is already complete.

## Current pipeline

The current SELECT path is:

```text
Select statement → SST nodes → Visitor/compiler → SQL + args
```

The SST represents query intent and owns structural traversal through `Accept`.
Expression nodes expose their own `Expr()` representation, while the compiler
owns final SQL rendering, dialect syntax, and argument collection.

The older public builder in `internal/builder.go` is not yet fully connected to
this SST pipeline. Moving that builder toward SST construction remains a later
step.

## SQL Semantic Tree (SST)

`internal/sst` is the SQL Semantic Tree: a semantic intermediate representation
for SQL query construction and compilation. It is not a parser AST focused only
on grammatical shape. SST nodes carry SQL-domain meaning and behavior through
contracts such as `StatementNode`, `ClauseNode`, `ExpressionNode`,
`DeclarationNode`, `Err`, `Declaration()`, `Expr()`, and `Accept`.

The SST preserves query structure, gives composite nodes ownership of child
traversal, and provides the compiler with semantic callbacks. It does not
replace the compiler's responsibility for final SQL, dialect syntax, or bound
argument collection.

## Statement roots

A statement root represents one complete SQL operation. `dql.SelectStatement`
is the first statement root being developed.

`SelectStatement` owns the shape of the SELECT operation, including its
selected columns and, as the implementation grows, its source, joins, criteria,
ordering, and other clauses.

The statement is assembled through a fluent API. Fluent clause methods such as
`From`, `Where`, and `Join` configure the same statement and return
`*SelectStatement` so that the query reads as a chain:

```go
stmt := dql.Select(
    sst.NewColumnRef("users", "id"),
).From(sst.NewTableRef("users"))
```

The fluent chain determines the statement intent. It does not render or execute
SQL. Compilation remains the terminal operation at the compiler boundary:

```go
sql, args, err := compiler.Compile(stmt)
```

`Select` is therefore a fluent statement builder and an SST statement root at
the same time. The compiler is responsible for the final action of translating
that statement into SQL and bound arguments.

## Element construction

Elements such as `ColumnRef` and `TableRef` are structural SST nodes. Their
configuration happens through constructor options:

```go
column := sst.NewColumnRef(
    "users",
    "id",
    sst.WithColumnSchema("public"),
)

table := sst.NewTableRef(
    "users",
    sst.WithTableSchema("public"),
)
```

The option shape is type-specific:

```text
ColumnRefOption → configures ColumnRef
TableRefOption  → configures TableRef
```

Go does not support overloaded package functions, so the options use explicit
names such as `WithColumnSchema` and `WithTableSchema`. This preserves type
safety and makes the target element clear at the call site.

Options are applied during construction. Elements do not use fluent mutator
methods such as `column.WithSchema(...)`. Once constructed, an element is
treated as stable semantic data in the statement tree.

The constructors currently do not add special handling for `nil` options. A
`nil` option is an invalid programmer input, but it is not currently modeled as
an error returned by the SST constructors.

## SST contracts

The base contracts live in `internal/sst`:

- `Node` defines visitor dispatch through `Accept`.
- `DeclarationNode` is the shared contract for nodes that expose
  `Declaration() string`.
- `StatementNode` extends `DeclarationNode` with statement-level `Err()`.
- `ClauseNode` extends `DeclarationNode` for SQL clauses such as `FROM`.
- `ListNode[T]` represents an ordered list of semantic-tree nodes.
- `SelectStatementNode` represents a SELECT statement root and extends
  `StatementNode`.
- `ColumnRefNode` represents a qualified or unqualified column reference.
- `TableRefNode` represents a qualified or unqualified table reference.
- `ExpressionNode` is the base interface for expressions that render SQL
  through `Expr()`.
- `BindParamNode` is the specialized interface that extends `ExpressionNode`
  with `Value() any` for runtime argument collection; the base expression
  contract has no `Value()`.
- `Visitor` defines compiler/traversal operations for these nodes.

These interfaces describe behavior boundaries rather than marker-only types.
New abstractions should be introduced only when they provide real behavior or
serve multiple concrete consumers.

## Compiler boundary

`internal/compiler` implements the visitor and owns rendering:

```text
VisitStatement      → statement declaration
VisitClause         → clause declaration
VisitExpression     → expression rendering and argument collection
VisitColumnRef      → qualified column identifier
VisitTableRef       → qualified table identifier
VisitFromSource     → SELECT source traversal
VisitJoin           → JOIN rendering
VisitListSeparator  → comma-separated list formatting
```

Composite SST nodes own structural traversal through `Accept`. The compiler
renders the current node; `VisitExpression` recognizes `BindParamNode`,
collects its runtime value, and appends its expression representation.

The compiler returns:

```go
sql  string
args []any
```

A resolved `Dialect` is supplied through the compiler shape context. The core
compiler does not select or register a vendor dialect. Runtime values are
represented as bind parameters rather than concatenated into SQL text.
Identifier rendering and value binding remain separate responsibilities.

## Package responsibilities

Current package responsibilities are:

```text
internal/sst       SST contracts and shared concrete expression/reference nodes
internal/sst/dql   SELECT statement roots and source nodes
internal/compiler  SQL rendering and argument collection
internal/dialect   Dialect contract, default QuestionMarkDialect, shared behavior
internal/executor  Driver-agnostic execution of compiled statement plans
```

Vendor-specific dialect implementations and transport-driver integration stay
outside the core project. External adapters resolve a vendor dialect and pass it
to the compiler.

The current implementation keeps contracts and first concrete nodes together
in `internal/sst`. They can be split into focused packages later if the
boundary becomes stable and package-cycle pressure justifies it.

## Join naming and rendering

The SELECT builder preserves join intent through explicit operations:

```text
Join       → JOIN
InnerJoin  → INNER JOIN
CrossJoin  → CROSS JOIN
LeftJoin   → LEFT JOIN
RightJoin  → RIGHT JOIN
FullJoin   → FULL OUTER JOIN
```

Each operation creates the same `Join` node shape with a different `JoinType`.
The current `.FullJoin(...)` API is semantically equivalent to SQLAlchemy's
`join(..., full=True)`, although the explicit method may be less ergonomic.
That API shape remains a review point when dialect support and additional join
variants are developed.

`Join` may be created before its `On` condition is supplied. If another `Join`
is added, the previous join remains in the SST with `On == nil`, and the new
join becomes the pending join. The compiler and dialect layer decide whether
that syntax is valid for the target database. `CrossJoin` is the explicit
representation for intentional cartesian-product behavior.

## FromSourceNode and Join representation

`TableRef` remains the reusable schema/table identity used by `SELECT`,
`CREATE TABLE`, and `INSERT INTO`. It does not contain SELECT-only join
semantics.

`FromSourceNode` is the SELECT source boundary. It stores the source table and
may own the next attached `Join`. A `Join` represents an actual relationship
and preserves both sides:

```text
FromSourceNode
├── Table: TableRef
└── Join: Join
    ├── Left: FromSourceNode (back-reference)
    ├── JoinType: JOIN / INNER / CROSS / LEFT / RIGHT / FULL OUTER
    ├── Right: FromSourceNode (next traversal point)
    └── On: Node
```

This preserves the fluent construction shape:

```go
Select(...).From(users).Join(orders).On(condition)
```

The source chain is traversed in one direction: `Table → Join → Right`.
`Left` is retained for context and validation but is never followed during
forward traversal, preventing a cycle when the source owns its join link.

The builder's `pendingJoin` points to the most recently created `Join`. Calling
`On` completes that join; calling another `Join` advances to a new pending join
without treating the previous missing `On` as a builder-state error.

## Current SELECT sequence

The current slices include:

1. model selected expressions;
2. model the `ExpressionList` projection list and its separators;
3. model one primary SELECT source with `TableRef` and `FROM` declaration;
4. represent and traverse chained `Join` nodes, including missing `On`
   conditions;
5. delegate binary-expression traversal from composite nodes and collect bind
   arguments in the compiler.

The next slices are:

6. compile JOIN rendering and dialect-specific validation;
7. design disconnected-FROM diagnostics;
8. add WHERE criteria and additional clauses.

Accidental cartesian products from disconnected sources must not be silent. A
primary source plus explicit joins remains the normal construction path.

A `Join` with `On == nil` is retained in the SST because its syntactic validity
depends on the target dialect. Compiler/dialect validation decides whether that
form is allowed. A future `CrossJoin` will provide an explicit, portable
representation for intentional cartesian products.

## Expression and bind boundaries

The expression tree separates SQL rendering from runtime argument collection:

- `ExpressionNode` is the output-only interface: it renders SQL through
  `Expr()` and does not expose a runtime value. A literal expression belongs to
  this capability.
- `BindParamNode` extends `ExpressionNode` with `Value() any`. Its visitor
  writes the placeholder representation and appends the value to `args`.
- `RawExpr(sql)` remains an explicit trusted/raw SQL escape hatch and is not a
  substitute for binding user input.

A public comparison helper may accept a Go value for ergonomics, but it must
normalize that value to a concrete bind-parameter node implementing
`BindParamNode`. The SST must not silently turn request input into inline SQL.

## Runtime compilation and statement-shape cache

The typed SST/compiler path intentionally separates query construction from
SQL rendering. That separation makes the first execution more expensive than
a generated or handwritten query path: the application may need to resolve a
model, construct the SST, traverse the nodes, render SQL, and collect bound
arguments.

That cost does not need to be paid for every execution of the same statement
shape. The runtime can use a two-path model inspired by frameworks that parse
and build a slow representation once, persist a generated artifact, and use a
direct path after the artifact is available:

```text
Cold/build path:
  public DSL or model
    → model/reflection metadata
    → SST construction
    → compiler traversal
    → compiled statement shape
    → cache

Warm/execution path:
  statement shape lookup
    → bind current runtime values
    → optional database/sql prepared statement
    → execute
```

The analogy is deliberately limited. MyFuses can generate PHP source and let
the PHP runtime include that source on later requests. SQLok must not generate
and execute Go source at runtime. Its cache artifact should remain declarative:
a SQL template plus the metadata required to bind current values safely.

### Compiled statement artifact

The future cache boundary is a compiled statement shape, not a request result
and not a statement containing request-specific values. A conceptual shape is:

```go
type CompiledStatement struct {
    Version    string
    Dialect    string
    ShapeKey   string
    SQL        string
    BindLayout []Binding
}
```

`SQL` contains placeholders. `BindLayout` maps logical values from a builder or
model to placeholder positions. A binding may identify an expression slot, a
model field path, or a row/column position for a multi-row INSERT. The exact
public type remains open, but the boundary must preserve these properties:

- the SQL template contains no runtime values;
- the current call creates or supplies the `args` slice separately;
- binding order is deterministic and matches placeholder order;
- the artifact can be validated before execution;
- the same shape can be reused with different values.

For example, these two executions have one cacheable shape but different
runtime data:

```text
UPDATE users SET name = ? WHERE users.id = ?
args: ["ana", 42]

UPDATE users SET name = ? WHERE users.id = ?
args: ["bob", 7]
```

The values must never become part of the cache key or serialized artifact.

### Shape identity

A cache key must describe the structure that affects rendered SQL and binding
layout. It should account for at least:

- statement root: SELECT, INSERT, UPDATE, or DELETE;
- statement topology and clause order;
- target table and selected/assigned columns;
- operators, joins, grouping, ordering, and row count where relevant;
- dialect and placeholder strategy;
- compiler artifact version;
- model/reflection descriptor version when a model API is involved;
- schema or migration fingerprint when schema changes can invalidate mapping.

Runtime values are intentionally excluded. A query that changes only from
`id = 42` to `id = 7` should reuse the same shape. A query that adds a JOIN,
changes the INSERT column set, or changes the number of VALUES rows must use a
different shape.

The key should be canonical rather than derived from pointer addresses or Go
map iteration order. Public row-map APIs therefore need deterministic column
ordering before they reach the SST.

### Development and production paths

Development mode should optimize feedback:

- allow cache misses to build a shape immediately;
- invalidate shapes when source, model metadata, compiler, or dialect versions
  change;
- expose enough diagnostics to distinguish a cold build from a warm hit;
- keep the cache easy to clear during AST development.

Production should optimize repeat execution and predictable permissions:

- build or warm the cache during deployment or an explicit warm-up step;
- prefer a read-only cache directory at request time;
- validate artifact version, dialect, shape key, and integrity before use;
- treat a missing or stale artifact as an explicit operational decision rather
  than silently executing an unvalidated artifact;
- write replacements atomically when runtime rebuilding is explicitly enabled.

An in-memory cache is the first implementation target. A persistent cache can
follow once the shape and invalidation contracts are stable. The database may
also maintain its own prepared-statement or query-plan cache; SQLok's cache is
an application-side cache for AST construction, rendering, and binding
metadata, not a replacement for the database optimizer.

### Security boundaries

The cache must preserve the same safety boundary as the compiler:

- never interpolate runtime values into cached SQL;
- never serialize secrets, credentials, or request payloads into artifacts;
- validate or constrain dynamic identifiers before they enter a shape;
- do not execute generated Go code, plugins, or arbitrary cache contents;
- keep production artifacts owned and writable only by the deployment process;
- avoid logging bound values merely to report cache hits or misses.

Raw SQL remains an explicit trusted escape hatch. A raw expression may affect a
shape key, but it must not turn the cache into an execution path for untrusted
source text.

### Minimal benchmark POC

The current compiler benchmarks provide three useful measurements:

```text
BenchmarkASTCompileEndToEnd
  builds a fresh AST and compiles it on every iteration;

BenchmarkASTCompileExistingStatement
  reuses an AST but still traverses and renders it on every iteration;

BenchmarkASTCompileCachedShape
  builds the SQL shape once, then reuses the SQL template and binds current
  values on the warm path.
```

`BenchmarkASTCompileCachedShape` is intentionally a small upper-bound POC. Its
binding layout is explicit in the benchmark rather than automatically derived
from the SST. It demonstrates the value of avoiding repeated SQL rendering,
but it is not the production cache contract. A real implementation must derive
`BindLayout` from the statement tree and preserve dynamic values without
caching them.

The benchmark is not an end-to-end database benchmark. It measures application
CPU and allocation cost before `database/sql` and network latency are involved.
The next validation step is an apples-to-apples benchmark using the same
statement shape and values across direct SQL, generated code, SQLok cold
compilation, and SQLok warm execution.

### Implementation phases

1. Add an in-memory shape cache around the current compiler without changing
   the public API.
2. Introduce a real `CompiledStatement` and automatic bind-layout extraction.
3. Cache model/reflection descriptors separately from SQL shapes.
4. Add optional prepared-statement reuse through `database/sql`.
5. Add persistent, versioned, read-only production artifacts and an explicit
   warm-up command.
6. Re-run benchmarks and keep the cache only where the measured warm-path gain
   justifies its complexity.

## Related documents

- [`vision.md`](vision.md) records the project's purpose and long-term direction.
- [`research.md`](research.md) records external research and its implications.
- This document records the current internal architecture decisions.
