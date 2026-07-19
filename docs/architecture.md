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

## Current SELECT sequence

The implementation sequence currently adopted is:

1. model selected columns;
2. model one primary SELECT source with `TableRef`;
3. compile the `FROM` clause;
4. design explicit joins and disconnected-FROM diagnostics;
5. add WHERE criteria and bind parameters.

Accidental cartesian products must not become the silent default. A primary
source plus explicit joins is the normal direction. Intentional cross joins or
other disconnected source shapes must be represented explicitly or diagnosed.

## Related documents

- [`vision.md`](vision.md) records the project's purpose and long-term direction.
- [`research.md`](research.md) records external research and its implications.
- This document records the current internal architecture decisions.
