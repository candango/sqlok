---
name: sqlok-sqlalchemy-api
description: Guide agents working on SQLok from developer-facing API design through implementation, testing, performance, and documentation. Use for any SQLok project task involving query construction, ORM/Session behavior, mapping, identity, AST/compiler/executor changes, or public API docs.
---

# SQLok agent workflow

## Product direction

SQLAlchemy is the direct reference for SQLok's developer-facing API and workflow. A deviation requires a very strong, specific reason grounded in a real SQLok or Go constraint. Explain why the SQLAlchemy-shaped design does not work; convenience, novelty, or generic stylistic preference is not enough.

The intended experience is expressive query construction, automatic result mapping, coherent Session/Unit-of-Work behavior, and infrastructure hidden from ordinary application call sites. `LoadContext` is legacy to be replaced by the public SELECT workflow, not a target API to preserve. Do not make `CompositeKey` the ergonomic model for composite identities.

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

### Implemented foundation (not the finished target workflow)

- SST has SELECT, INSERT, UPDATE, and DELETE roots; `sst/dql.Select` builds low-level SELECT AST statements.
- The compiler provides dialect rendering, bind layouts, prepared statement caching, and plan registries.
- `Mapper[T]` supports struct metadata, row scanning, and value extraction.
- `Session.Add` and `Session.Flush(ctx, tx)` support pending INSERTs and dirty UPDATEs. Flush receives a caller-owned transaction. Generated-key support covers one numeric generated primary key.
- The current core session tests use a hand-written `database/sql` fake driver. The separate `sqlok-sqlite-modernc` E2E suite uses real SQLite and currently exercises ORM lookups through `LoadContext`.

### WIP — target SQLAlchemy-like workflow

- Public `sqlok.Select(...)` for mapped entities and composable predicates.
- Session execution with typed results/scalars, automatic mapping, and identity-map reuse.
- Replacement/removal of `LoadContext` as the ORM lookup path; composite identity ergonomics without `CompositeKey` leakage.
- Session Unit-of-Work DELETE alongside INSERT and UPDATE.
- Session transaction lifecycle and ownership contract.
- Batch query and relation loading.
- Public contract tests, docs, and downstream migration of ORM E2E reads to the finalized public API.

## Test and performance gates

- Run focused tests during development and `go test ./...` plus `go vet ./...` before completion, unless repository policy specifies a stronger gate.
- Fake `database/sql` drivers are useful for precise core behavior, but do not substitute for real-driver E2E evidence when changing execution or mapping. Do not edit the separate SQLite adapter repository unless the active task explicitly includes it.
- Treat performance as a non-regression requirement. Benchmark affected paths before and after changes under comparable conditions, preserve reusable plan/metadata caches, and investigate material regressions. Do not claim gains or zero regression without reproducible measurements.

Baseline captured 2026-09-26 before SELECT API implementation, using Go 1.27.0 on Linux/amd64 (AMD Ryzen 5 1600), five runs:

- `BenchmarkSessionIdentityMapHit`: 82.19–86.25 ns/op, 0 B/op, 0 allocs/op.
- `BenchmarkSessionLoadPreparedMiss`: 10.039–10.761 µs/op, 1,754 B/op, 30 allocs/op.
- `BenchmarkASTCompileCachedShape`: 7.353–7.647 ns/op, 0 B/op, 0 allocs/op.

Reproduce with:

```bash
go test -run='^$' -bench='^(BenchmarkSessionLoadPreparedMiss|BenchmarkSessionIdentityMapHit|BenchmarkASTCompileCachedShape|BenchmarkMapperRows)$' -benchmem -count=5 ./...
```

These are adjacent existing-path baselines, not performance results for the new API. Add an equivalent query/result benchmark when its public contract exists.

## Focus and commits

- Work only within the active task's scope and keep its focus anchored. Use `tw-flow`; never raw `task`.
- Split unrelated work into separate atomic commits and stage only task-relevant files. Never stage `.` or `-A`.
- Commits and pushes require explicit operator confirmation. Do not close a task without an `OUTCOME` and the required confirmation.
