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

Runtime values are represented as bind parameters rather than concatenated
into SQL text. Identifier rendering and value binding remain separate
responsibilities.

## Package responsibilities

Current package responsibilities are:

```text
internal/sst       SST contracts and shared concrete expression/reference nodes
internal/sst/dql   SELECT statement roots and source nodes
internal/compiler  SQL rendering and argument collection
```

The current implementation keeps contracts and first concrete nodes together
in `internal/sst`. They can be split into focused packages later if the
boundary becomes stable and package-cycle pressure justifies it.

## Join naming and rendering

The current SELECT slice has one concrete join node: `Join`. The public
`Join()` operation has inner-join semantics and renders the portable SQL token
`JOIN`; a separate `InnerJoin()` API is not needed. `CrossJoin` remains a future
explicit join type/API variant.

`Join` may be created before its `On` condition is supplied. If another `Join`
is added, the previous join remains in the SST with `On == nil`, and the new
join becomes the pending join. The compiler and dialect layer decide whether
that syntax is valid for the target database. An explicit `CrossJoin` remains a
future, clearer representation for portable cartesian-product intent.

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
    ├── JoinType: JOIN (inner join semantics)
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

## TODO: cache compiled statement shapes

The typed SST/compiler path may cost more during the first construction and
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
