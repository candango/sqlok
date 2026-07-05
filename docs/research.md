# sqlok research

This document stores external research and source-backed references used to shape
`sqlok`. It records what other projects do; `docs/vision.md` records what
`sqlok` chooses to become.

## SQLAlchemy Core statement and AST model

Reference project: SQLAlchemy, commit
`d59159ca08cdf661f97d19d2966071a1b1d3df80`.

### Architecture lesson

SQLAlchemy Core follows a layered architecture equivalent to:

```text
Expression API → Expression Tree → Compiler/Dialect → SQL + bound parameters
```

For `sqlok`, the comparable shape is:

```text
Builder DSL → AST → Compiler/Dialect → SQL + args
```

The lesson is not to copy SQLAlchemy feature-for-feature. The useful part is the
separation between the user-facing construction API, statement/expression tree,
compiler/dialect layer, and final SQL output.

### SELECT returns a statement root

`select(...)` returns a `Select` object:

- Source: `select(...)` returns `Select(*entities)`
  https://github.com/sqlalchemy/sqlalchemy/blob/d59159ca08cdf661f97d19d2966071a1b1d3df80/lib/sqlalchemy/sql/_selectable_constructors.py#L574-L617

`Select` represents a `SELECT` statement and declares `__visit_name__ = "select"`:

- Source:
  https://github.com/sqlalchemy/sqlalchemy/blob/d59159ca08cdf661f97d19d2966071a1b1d3df80/lib/sqlalchemy/sql/selectable.py#L5449-L5488

Research conclusion:

```text
select(...) → Select statement root
```

The `Select` object is the top/root node for a SELECT query shape.

### INSERT, UPDATE, and DELETE are separate statement roots

SQLAlchemy has separate DML constructors:

```text
insert(table) → Insert
update(table) → Update
delete(table) → Delete
```

- Source:
  https://github.com/sqlalchemy/sqlalchemy/blob/d59159ca08cdf661f97d19d2966071a1b1d3df80/lib/sqlalchemy/sql/_dml_constructors.py#L20-L125

The statement classes live in `sqlalchemy/sql/dml.py`:

- `Insert`:
  https://github.com/sqlalchemy/sqlalchemy/blob/d59159ca08cdf661f97d19d2966071a1b1d3df80/lib/sqlalchemy/sql/dml.py#L1225-L1265
- `Update`:
  https://github.com/sqlalchemy/sqlalchemy/blob/d59159ca08cdf661f97d19d2966071a1b1d3df80/lib/sqlalchemy/sql/dml.py#L1633-L1670
- `Delete`:
  https://github.com/sqlalchemy/sqlalchemy/blob/d59159ca08cdf661f97d19d2966071a1b1d3df80/lib/sqlalchemy/sql/dml.py#L1842-L1875

Research conclusion:

```text
Select / Insert / Update / Delete are distinct statement roots.
```

They are not one generic bag of SQL fragments.

### SELECT columns vocabulary

SQLAlchemy uses column-oriented vocabulary for the selected part of a SELECT:

- `_raw_columns`: internal selected expressions/columns
- `selected_columns`: public accessor for selected columns

Sources:

- `_raw_columns` on `Select`:
  https://github.com/sqlalchemy/sqlalchemy/blob/d59159ca08cdf661f97d19d2966071a1b1d3df80/lib/sqlalchemy/sql/selectable.py#L5484-L5488
- `selected_columns` references:
  https://github.com/sqlalchemy/sqlalchemy/blob/d59159ca08cdf661f97d19d2966071a1b1d3df80/lib/sqlalchemy/sql/selectable.py#L6751-L6786

Research conclusion:

```text
Use Columns for the SELECT columns clause in sqlok, but define it broadly as
selected expressions, not only physical table columns.
```

### SELECT FROM behavior and cartesian products

SQLAlchemy allows multiple FROM elements in a SELECT. Calling `select_from()`
with separate tables, or otherwise allowing unrelated tables to enter the FROM
set, can produce SQL shaped like:

```sql
FROM users, orders
```

That form is a cartesian product unless the tables are connected by join or
WHERE criteria. SQLAlchemy 1.4+ includes FROM-linting that warns when a SELECT
contains unlinked FROM elements that imply an accidental cartesian product.

The preferred SQLAlchemy shape for related tables is an explicit join:

```python
select(user_table.c.id, address_table.c.email_address).join(
    address_table,
    user_table.c.id == address_table.c.user_id,
)
```

or an explicit joined FROM object:

```python
select(...).select_from(
    user_table.join(address_table, user_table.c.id == address_table.c.user_id)
)
```

Intentional cartesian products should be explicit, for example by joining on
`true()`, so the cartesian shape is deliberate rather than accidental.

Sources:

- SQLAlchemy 1.4 migration notes on built-in FROM linting and cartesian product
  warnings:
  https://docs.sqlalchemy.org/en/21/changelog/migration_14.html
- SQLAlchemy Core selectable documentation for `Select.select_from()`,
  `Select.join()`, and `Select.join_from()`:
  https://docs.sqlalchemy.org/en/21/core/selectable.html

Research conclusion:

```text
Do not make accidental cartesian products the easy path in sqlok. Model a
primary SELECT source first, add explicit joins later, and require any cross join
or multi-source cartesian behavior to be explicit.
```

### WHERE vocabulary: criteria, not Predicate

SQLAlchemy does not use `Predicate` as the primary statement-field vocabulary.
The relevant names are:

- `whereclause`: completed WHERE clause
- `_where_criteria`: internal collection of WHERE criteria
- `criterion`: each individual argument added by `.where(...)`
- `ColumnElement[bool]`: boolean SQL expression accepted as a WHERE item
- `BooleanClauseList`: combined boolean clause list, usually with AND/OR
- `BinaryExpression`: binary expression such as `left = right`

`Select` stores `_where_criteria` and exposes a `whereclause` property:

- Source:
  https://github.com/sqlalchemy/sqlalchemy/blob/d59159ca08cdf661f97d19d2966071a1b1d3df80/lib/sqlalchemy/sql/selectable.py#L5491-L5495
  https://github.com/sqlalchemy/sqlalchemy/blob/d59159ca08cdf661f97d19d2966071a1b1d3df80/lib/sqlalchemy/sql/selectable.py#L6473-L6507

DML UPDATE/DELETE share the same `_where_criteria` concept:

- Source:
  https://github.com/sqlalchemy/sqlalchemy/blob/d59159ca08cdf661f97d19d2966071a1b1d3df80/lib/sqlalchemy/sql/dml.py#L1510-L1554

`BooleanClauseList` is used to construct WHERE boolean clause lists:

- Source:
  https://github.com/sqlalchemy/sqlalchemy/blob/d59159ca08cdf661f97d19d2966071a1b1d3df80/lib/sqlalchemy/sql/elements.py#L3294-L3310
  https://github.com/sqlalchemy/sqlalchemy/blob/d59159ca08cdf661f97d19d2966071a1b1d3df80/lib/sqlalchemy/sql/elements.py#L3408-L3420

`BinaryExpression` represents `LEFT <operator> RIGHT`:

- Source:
  https://github.com/sqlalchemy/sqlalchemy/blob/d59159ca08cdf661f97d19d2966071a1b1d3df80/lib/sqlalchemy/sql/elements.py#L4165-L4180

Research conclusion:

```text
Prefer Criteria / WhereCriteria in sqlok vocabulary.
Predicate can remain a lower-level explanatory term for one boolean condition.
```

### Compiler dispatch

SQLAlchemy uses node visit names and compiler dispatch rather than putting SQL
rendering directly on the statement node.

`Visitable` generates `_compiler_dispatch()` from `__visit_name__` and calls the
matching `visit_<name>` method on the visitor/compiler:

- Source:
  https://github.com/sqlalchemy/sqlalchemy/blob/d59159ca08cdf661f97d19d2966071a1b1d3df80/lib/sqlalchemy/sql/visitors.py#L65-L128

`Compiled.process()` calls `obj._compiler_dispatch(self, **kwargs)`:

- Source:
  https://github.com/sqlalchemy/sqlalchemy/blob/d59159ca08cdf661f97d19d2966071a1b1d3df80/lib/sqlalchemy/sql/compiler.py#L971-L974

`SQLCompiler` compiles `ClauseElement` objects into SQL strings:

- Source:
  https://github.com/sqlalchemy/sqlalchemy/blob/d59159ca08cdf661f97d19d2966071a1b1d3df80/lib/sqlalchemy/sql/compiler.py#L1123-L1130

`SQLCompiler` has statement-specific methods:

- `visit_select`:
  https://github.com/sqlalchemy/sqlalchemy/blob/d59159ca08cdf661f97d19d2966071a1b1d3df80/lib/sqlalchemy/sql/compiler.py#L4973-L4998
- `visit_insert`:
  https://github.com/sqlalchemy/sqlalchemy/blob/d59159ca08cdf661f97d19d2966071a1b1d3df80/lib/sqlalchemy/sql/compiler.py#L6116-L6122
- `visit_update`:
  https://github.com/sqlalchemy/sqlalchemy/blob/d59159ca08cdf661f97d19d2966071a1b1d3df80/lib/sqlalchemy/sql/compiler.py#L6563-L6569
- `visit_delete`:
  https://github.com/sqlalchemy/sqlalchemy/blob/d59159ca08cdf661f97d19d2966071a1b1d3df80/lib/sqlalchemy/sql/compiler.py#L6734-L6740

Research conclusion:

```text
Keep AST nodes structural. Put SQL rendering in compiler/traversal code.
```

### Visitor manifestation: SQLAlchemy vs Go

SQLAlchemy does implement a visitor-like dispatch, but not through a classic
`Accept(visitor)` interface. The mechanism is:

```text
node.__visit_name__ = "select"
compiler.process(node)
  → node._compiler_dispatch(compiler)
  → compiler.visit_select(node)
```

The binding point is the compiler method named `visit_<name>`, derived from the
node's `__visit_name__`.

Research interpretation:
- Python does not have Go-style compile-time interface enforcement.
- Instead of relying only on informal duck typing, SQLAlchemy creates an
  explicit runtime dispatch protocol.
- The node declares its visit identity with `__visit_name__`.
- `Visitable` generates `_compiler_dispatch()`.
- The compiler must expose the corresponding `visit_<name>` method.

In Go, `sqlok` does not need to copy the dynamic string/name dispatch. We can
represent the same architectural idea with explicit interfaces and compile-time
substitutability.

Possible Go shape:

```go
type Node interface {
	Accept(Visitor) error
}

type Visitor interface {
	VisitSelect(Select) error
	VisitInsert(Insert) error
	VisitUpdate(Update) error
	VisitDelete(Delete) error
}
```

Or, if we choose compiler-owned dispatch for the first MVP, we can still keep an
explicit `Statement` interface and switch on concrete statement roots inside the
compiler.

Go conclusion:

```text
SQLAlchemy uses runtime visitor dispatch via __visit_name__ and visit_<name>.
sqlok can use explicit Go interfaces to enforce the expected behavior instead
of relying on runtime naming conventions.
```

This keeps the same separation of concerns while making the contract visible in
Go's type system.

### Compile ergonomics vs passive AST

SQLAlchemy statement objects are ergonomic: user code can construct a statement
and then call compilation behavior from that object-oriented surface, e.g.
conceptually `stmt.compile()`.

The relevant design question for `sqlok` is whether statement roots should expose
an ergonomic `Compile()` method or remain passive structures compiled only by an
external compiler function.

Two viable shapes:

```go
sql, args, err := stmt.Compile()
```

or:

```go
sql, args, err := compiler.Compile(stmt)
```

Research interpretation:
- `stmt.Compile()` is better ergonomically for users.
- `compiler.Compile(stmt)` preserves a stricter passive-AST boundary.
- A hybrid is possible: expose `stmt.Compile()` publicly, but implement it as a
  thin delegation to compiler logic.

Open conclusion for now:

```text
Do not decide yet. Track the trade-off between API ergonomics and passive AST
purity while keeping rendering logic out of the statement data model.
```

### Source-inspired package map

SQLAlchemy's relevant package layout:

```text
sqlalchemy/sql/
  _selectable_constructors.py  # select(...)
  selectable.py                # Select and selectable structures
  _dml_constructors.py         # insert(...), update(...), delete(...)
  dml.py                       # Insert, Update, Delete
  elements.py                  # expression and clause elements
  compiler.py                  # SQLCompiler
  visitors.py                  # compiler dispatch / traversal support
```

This supports a `sqlok` design where AST statement roots and the compiler are
separate packages/modules.
