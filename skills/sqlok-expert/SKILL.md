---
name: sqlok-expert
description: Guide agents working on SQLok from developer-facing API design through implementation, testing, performance, and documentation. Use for any SQLok project task involving query construction, ORM/Session behavior, mapping, identity, AST/compiler/executor changes, or public API docs.
---

# SQLok agent workflow

## Product direction

SQLAlchemy is the direct reference for SQLok's developer-facing API and workflow. A deviation requires a very strong, specific reason grounded in a real SQLok or Go constraint. Explain why the SQLAlchemy-shaped design does not work; convenience, novelty, or generic stylistic preference is not enough.

The intended experience is expressive query construction, automatic result mapping, coherent Session/Unit-of-Work behavior, and infrastructure hidden from ordinary application call sites. `LoadContext` and `Load` have been removed; do not reintroduce them. Do not make `CompositeKey` the ergonomic model for composite identities.

## Work top-down

Start with the application developer's call site and observable behavior. Define the public Go contract next; then implement only the supporting Session, result/mapping, identity, SST, compiler, executor, and dialect behavior needed to realize it.

Do not let existing internals dictate public ergonomics. `sst/dql.Select` is a low-level builder, not the ORM-facing query API. Do not invent public identifiers or signatures before the usage contract is designed. Keep undecided or unsupported behavior marked `WIP`.

Reference workflow from SQLAlchemy (Python syntax, not SQLok code):

```python
stmt = select(User).where(User.name == "Ana")
users = session.scalars(stmt).all()

session.add(user)
user.name = "Bia"
session.delete(other_user)
session.flush()
```

Preserve the operation model and call-site ergonomics in Go. Keep the current `go 1.24.0` minimum unless the operator approves raising it. Go 1.27 introduced generic concrete methods, but the module's `go` directive sets the language version enforced by the compiler. Sources: [Go toolchains](https://go.dev/doc/toolchain), [Go 1.27 release notes](https://go.dev/doc/go1.27).

## Sources of truth

- The active `tw-flow` task and focus own the current work scope. Keep focus on that task; do not jump into a sibling repository unless explicitly asked.
- This skill is the living agent playbook and feature-status ledger. Update it as implementation changes.
- `docs/research.md` records source-backed facts about SQLAlchemy and other targets/competitors. Keep local SQLok implementation status out of competitor research.
- `docs/vision.md` records what SQLok chooses to become. Record design decisions there when the public contract is settled.

## Status rules

- `Implemented` means the public behavior exists and relevant tests demonstrate it.
- `WIP` means the target behavior is absent, partial, or reachable only through a low-level or legacy API.
- An internal builder, fake-driver unit test, or adapter workaround does not by itself make a public ORM feature implemented.
- After each implementation slice, update this ledger and its tests/docs status. Do not mark a feature implemented based only on a plan or example.

## Current implementation ledger

### Implemented foundation

- SST has SELECT, INSERT, UPDATE, and DELETE roots; `sst/dql.Select` builds low-level SELECT AST statements.
- The compiler provides dialect rendering, bind layouts, prepared statement caching, and plan registries.
- `Mapper[T]` supports struct metadata, row scanning, and value extraction.
- `sqlok.Select(User{}).Where(sqlok.Eq("name", "Ana"))` builds a typed entity query. `All`, `One`, and `OneOrNone` execute through Session, map rows, reuse Identity Map pointers, and use Session-private prepared read plans. Values are bound; `OneOrNone` limits reads to two rows.
- `sqlok.Select(User{}).Columns("name").All(...)` returns mapped-column `SelectRow` values, and a single projected column supports `Scalars(...)` returning `[]any`. Projection columns are mapped database-column names and preserve requested order.
- `Session.Add` and `Session.Flush(ctx, tx)` support pending INSERTs and dirty UPDATEs. `Session.BindTransaction(tx)` binds reads to the caller-owned transaction and autoflushes pending or dirty entities before SELECT execution; `UnbindTransaction` is explicit. Generated-key support covers one numeric generated primary key.
- `LoadContext`, `Load`, and public `CompositeKey` were removed from core. Composite identity queries use one equality criterion per mapped key column.

### WIP — remaining SQLAlchemy-like workflow

- Public mapped-column predicates support equality, inequality, ordering, `IsNull`, and `IsNotNull`; NULL comparison values remain rejected, so callers use explicit NULL predicates. Typed field descriptors and richer predicate composition beyond these criteria are WIP.
- The Session transaction lifecycle remains caller-owned; it does not begin, commit, or roll back transactions automatically.
- Session Unit-of-Work DELETE and the target transaction lifecycle.
- Batch query and relation loading.
- Migrate the separately owned `sqlok-sqlite-modernc` E2E suite from its pinned legacy core API to public SELECT; do not edit that repository unless the active task explicitly includes it.
- Broader public API contract tests and migration documentation.

## Test and performance gates

- Run focused tests during development and `go test ./...` plus `go vet ./...` before completion, unless repository policy specifies a stronger gate.
- Core SELECT tests currently use a hand-written `database/sql` fake driver. That validates SQL shape, binds, mapping, and identity behavior, but not a real database driver. Real SQLite E2E migration remains a separate adapter task; do not edit that repository unless explicitly included.
- Treat performance as a non-regression requirement. Benchmark affected paths before and after changes under comparable conditions, preserve reusable plan/metadata caches, and investigate material regressions. Do not claim gains or zero regression without reproducible measurements.

Benchmarks captured on Go 1.27.0, Linux/amd64, AMD Ryzen 5 1600, five runs, using the core fake driver:

- Historical `LoadContext` prepared miss before removal: 10.039–10.761 µs/op, 1,754 B/op, 30 allocs/op.
- Current inline `Select(...).Where(...).All(...)`: 8.511–10.345 µs/op, 1,770 B/op, 31 allocs/op.
- Current inline `Select(...).Where(...).OneOrNone(...)`: 10.063–10.717 µs/op, 1,882 B/op, 30 allocs/op.
- `BenchmarkASTCompileCachedShape`: 7.399–7.717 ns/op, 0 B/op, 0 allocs/op.

Reproduce current full-call SELECT and compiler measurements with:

```bash
go test -run='^$' -bench='^(BenchmarkSessionSelectBuildAnd(All|OneOrNone)|BenchmarkASTCompileCachedShape)$' -benchmem -count=5 ./...
```

These fake-driver benchmarks measure core overhead and allocations, not real database latency. Keep real-driver E2E measurements separate.

## Focus and commits

- Work only within the active task's scope and keep its focus anchored. Use `tw-flow`; never raw `task`.
- Split unrelated work into separate atomic commits and stage only task-relevant files. Never stage `.` or `-A`.
- Commits and pushes require explicit operator confirmation. Do not close a task without an `OUTCOME` and the required confirmation.
