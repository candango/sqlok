# sqlok architecture

This document records the architecture decisions currently adopted by `sqlok`.
It describes the implemented SELECT AST slice and the construction boundaries
we have chosen so far. It does not claim that the long-term architecture in
[`vision.md`](vision.md) is already complete.

## Current pipeline

The current SELECT path is:

```text
Select statement → AST nodes → Visitor/compiler → SQL + args
```

The AST represents query intent. The compiler owns SQL rendering and argument
collection. AST nodes do not render SQL themselves.

The older public builder in `internal/builder.go` is not yet fully connected to
this AST pipeline. Moving that builder toward AST construction remains a later
step.

## Statement roots

A statement root represents one complete SQL operation. `dql.Select` is the
first statement root being developed.

`Select` owns the shape of the SELECT operation, including its selected columns
and, as the implementation grows, its source, joins, criteria, ordering, and
other clauses.

The statement is assembled through a fluent API. Fluent clause methods such as
`From`, `Where`, and `Join` configure the same statement and return `*Select` so
that the query reads as a chain:

```go
stmt := dql.NewSelect(
    dql.NewSelectColumn(column),
).From(source)
```

The fluent chain determines the statement intent. It does not render or execute
SQL. Compilation remains the terminal operation at the compiler boundary:

```go
sql, args, err := compiler.Compile(stmt)
```

`Select` is therefore a fluent statement builder and an AST statement root at
the same time. The compiler is responsible for the final action of translating
that statement into SQL and bound arguments.

## Element construction

Elements such as `ColumnRef` and `TableRef` are structural AST nodes. Their
configuration happens through constructor options:

```go
column := elements.NewColumnRef(
    "users",
    "id",
    elements.WithColumnSchema("public"),
)

table := elements.NewTableRef(
    "users",
    elements.WithTableSchema("public"),
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
an error returned by the AST constructors.

## AST contracts

The base contracts live in `internal/sst`:

- `Node` defines visitor dispatch through `Accept`.
- `SelectNode` represents a SELECT statement root.
- `SelectColumnNode` represents one selected item.
- `ColumnRefNode` represents a qualified or unqualified column reference.
- `TableRefNode` represents a qualified or unqualified table reference.
- `Visitor` defines compiler/traversal operations for these nodes.

These interfaces describe behavior boundaries rather than marker-only types.
New abstractions should be introduced only when they provide real behavior or
serve multiple concrete consumers.

## Compiler boundary

`internal/compiler` implements the visitor and owns rendering:

```text
VisitSelect      → SELECT and statement clauses
VisitSelectColumn → selected expression
VisitColumnRef   → qualified column identifier
VisitTableRef    → qualified table identifier
```

The compiler returns:

```go
sql  string
args []any
```

Values must eventually be represented as bind parameters rather than
concatenated into SQL text. Identifier rendering and value binding remain
separate responsibilities.

## Package responsibilities

Current package responsibilities are:

```text
internal/sst      AST contracts and visitor interfaces
internal/dql      DQL statement roots and SELECT clause nodes
internal/elements Concrete shared AST elements
internal/compiler SQL rendering and argument collection
```

`internal/elements` currently keeps the first concrete nodes together while the
package is small. Once it becomes a grab bag, elements should be split into
focused files such as `column_ref.go`, `table_ref.go`, `literal.go`, and
`binary.go`.

## Join naming and rendering

The fluent API may expose both `Join` and `InnerJoin` for readability. They
represent the same inner-join semantics in the AST. The compiler must always
render the explicit SQL form:

```sql
INNER JOIN
```

This keeps generated SQL unambiguous in long queries. `Join` is a convenience
name at the construction boundary; it is not a distinct inner-join node.

## FromSourceNode and Join representation

`TableRef` remains the reusable schema/table identity used by `SELECT`,
`CREATE TABLE`, and `INSERT INTO`. It does not contain SELECT-only join
semantics.

`FromSourceNode` is the SELECT source boundary. It contains either a
`TableRef` or a composed `Join`. A `Join` represents an actual relationship
and preserves both sides for generic AST traversal:

```text
FromSourceNode
└── TableRef | Join

Join
├── Left: FromSourceNode
├── JoinType: INNER
├── Right: TableRef
└── On: ExpressionNode
```

This preserves the fluent construction shape:

```go
NewSelect(...).From(users).Join(orders, condition)
```

The source is a tree, not a flat list of disconnected tables. Traversal walks
`Left`, `Right`, and `On` for compilation, validation, column discovery, and
future disconnected-source diagnostics.

## Current SELECT sequence

The first three slices are implemented:

1. model selected columns;
2. model one primary SELECT source with `TableRef`;
3. compile the `FROM` clause.

The next slices are:

4. design explicit joins and disconnected-FROM diagnostics;
5. add WHERE criteria and bind parameters.

Accidental cartesian products must not become the silent default. A primary
source plus explicit joins is the normal direction. Intentional cross joins or
other disconnected source shapes must be represented explicitly or diagnosed.

## TODO: cache compiled statement shapes

The typed AST/compiler path may cost more during the first construction and
compilation than direct SQL-string assembly. That cost is acceptable when a
statement shape can be compiled once and reused for subsequent executions.

This TODO emerged while modeling the `JOIN ... ON` clause: keeping `ON` as a
structured or parameterized expression improves composition and safety, while
cached compilation can optimize the repeated execution path. The cache should
store the SQL template and parameter layout, not request-specific argument
values. Cache identity must account for the statement shape and SQL dialect.

## Related documents

- [`vision.md`](vision.md) records the project's purpose and long-term direction.
- [`research.md`](research.md) records external research and its implications.
- This document records the current internal architecture decisions.
