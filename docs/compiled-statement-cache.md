# Compiled Statement Cache

This document defines the proposed runtime compilation and cache strategy for
SQLok. It is a design document and a benchmark plan, not a claim that the
persistent cache or automatic bind-layout compiler already exists.

## Purpose

The typed SST/compiler path deliberately separates query construction from SQL
rendering. That separation gives SQLok a strong structural and safety boundary,
but it can cost more than a generated or handwritten query path when the same
statement shape is built repeatedly.

The proposed solution is a cold/warm execution model:

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

The design is inspired by systems such as MyFuses that parse/build a slow
representation once, store a generated artifact, and use a direct path after
the artifact is available. The analogy has an important boundary: MyFuses can
generate PHP source and include it later, while SQLok must not generate and
execute Go source at runtime. SQLok should cache a declarative SQL artifact:
a SQL template plus the metadata required to bind current values safely.

## What is being cached

The cache stores a **statement shape**, not a request, entity, transaction, or
result. A conceptual future artifact is:

```go
type CompiledStatement struct {
    Version    string
    Dialect    string
    ShapeKey   string
    SQL        string
    BindLayout []Binding
}
```

The public shape is immutable after compilation. Its behavior is:

- `SQL` contains placeholders, never interpolated runtime values;
- `BindLayout` maps explicit logical slots to placeholder positions;
- current execution values are supplied separately;
- binding order is deterministic and validated before execution;
- named slots use a shape-owned `ArgumentBuffer`, while legacy positional
  layouts may continue to receive `[]any`;
- the same shape can be reused with different values.

For example, these calls share one shape:

```text
UPDATE users SET name = ? WHERE users.id = ?
args: ["ana", 42]

UPDATE users SET name = ? WHERE users.id = ?
args: ["bob", 7]
```

The SQL template is reusable. The values are not cache data and must not be
part of the serialized artifact or cache key.

## Mutability contract

The cache depends on a strict boundary between construction state and execution
state. A builder may be mutable while a statement is being assembled, but the
artifact stored in the cache must be immutable after publication.

```text
Mutable construction:
  builder, model input, expressions, session state, transaction

Freeze/compile boundary:
  validate shape, render SQL, derive bind layout, calculate shape key

Immutable reusable artifact:
  SQL template, bind layout, dialect/version metadata, shape key

Per-execution mutable state:
  current values, args, rows affected, errors, transaction result
```

### Immutable after publication

These values must not change in place after a cache entry is published:

- statement root and AST topology;
- target tables, columns, operators, and clause ordering;
- SQL template and placeholder positions;
- bind layout and binding order;
- dialect and compiler artifact version;
- model descriptor version used to derive the shape;
- cache key and integrity metadata.

A structural change does not mutate an existing artifact. It produces a new
shape key and a new artifact. The implementation should expose read-only
accessors or defensive copies rather than exporting mutable slices that callers
can modify behind the cache's back.

### Mutable by design

These values belong to construction or execution scope and must not be shared
as part of an immutable artifact:

- fluent builder state before compilation;
- request values and current bind arguments;
- ORM Session identity-map, pending, dirty, and snapshot state;
- transaction and connection state;
- execution errors and result metadata;
- cache population, eviction, and hit counters.

A Session can reuse an immutable shape while supplying fresh values on every
flush. The Session itself remains outside the cache entry and should not be
shared across concurrent requests unless a future API explicitly defines that
ownership model.

### Concurrency consequence

Published statement shapes can be shared concurrently because they contain no
request-specific mutable data. Each execution must obtain its own argument
view, or use an explicitly owned/reusable argument buffer whose lifetime is
strictly bounded by that execution. A mutable builder must be frozen, copied,
or rebuilt before it is used by concurrent callers.

This is the same operational idea as a MyFuses generated artifact remaining
stable until its source/configuration fingerprint changes: source and runtime
state may change, but the published artifact is replaced rather than mutated.

## Relation to MyFuses

The MyFuses source review shows a three-stage application lifecycle:

```text
load:
  XML or cached data → structured arrays

build:
  structured arrays → application/circuit/action/verb objects

store:
  object tree → generated PHP and serialized data artifacts
```

The lifecycle is lazy by unit: `checkCircuit()` loads and builds a circuit when
it is first touched. A warm request includes the generated PHP artifact, while
some request/application work may still be reconstructed. The phase constants
and global phase state are request orchestration; they are not an explicit
artifact state machine.

SQLok does not have a direct `load → build` equivalent because Go code and the
fluent builder already create and hydrate the SST tree. The closer SQLok
lifecycle is:

```text
register/derive:
  Go builder or model descriptor → SST statement

store/publish:
  SST → immutable CompiledStatement → registry/cache

warm request:
  external PlanID → CompiledStatement → Bind(args) → Executor

invalidate:
  version/config/schema change → replace published artifact
```

Only artifact publication, warm artifact consumption, lazy first-use
preparation, and invalidation are meaningful parallels. The SQLok artifact
lifecycle (`Unseen → Published → Warm → Invalid → Rebuilding`, if adopted) is
our own design vocabulary, not a state machine inherited from MyFuses.

The SQLok artifact is intentionally less powerful than executable generated
source. It cannot execute arbitrary code. It only describes a previously
validated SQL shape and how current values reach its placeholders.

## Cache layers

The cache should be split by responsibility instead of putting all state in a
single global object.

```text
Model descriptor cache
  struct type → fields, tags, table name, column order, primary keys

Statement shape cache
  canonical statement shape → SQL template + bind layout

Session state
  request/unit-of-work → identity map, pending objects, dirty snapshots,
                          transaction state

Database prepared statements
  database/sql and driver scope → optional driver/database plan reuse
```

The first two caches can be shared at process or application scope. Session
state must remain scoped to the current request or unit of work. A Session must
not become the owner of a process-wide SQL cache, and session identity or
transaction state must never enter a compiled artifact.

## Query derivation strategies

The cache must not derive statement shapes directly from arbitrary runtime
values. It needs explicit derivation strategies that normalize different
public inputs into the same canonical result:

```text
input/source → statement shape + bind layout + runtime values
```

The shape and bind layout are immutable outputs of derivation. Runtime values
remain execution-scoped and mutable. Every strategy must validate its input
before publishing or looking up a cache entry.

### Core builder derivation

The Core builder is the most direct strategy. The caller explicitly constructs
an SST statement:

```text
Select/Insert/Update/Delete builder
  → statement root and child nodes
  → canonical shape fingerprint
  → bind layout
  → current args
```

The builder owns structural intent. The cache key is derived from the
canonicalized SST topology, target identifiers, clauses, operators, and binding
slots. Values supplied to bind parameters do not affect the shape key.

A mutable fluent builder must not be published directly as a cache artifact. It
must be validated and frozen or compiled into an immutable statement shape.

### Model/struct derivation

The future model API starts with a Go struct and a stable model descriptor:

```text
User struct + mapping metadata
  → table and column descriptor
  → operation-specific SST root
  → canonical shape and bind layout
  → values extracted from the current User
```

The model descriptor owns table name, column order, field mapping, primary-key
metadata, and descriptor version. The descriptor is cached separately from SQL
shapes. A changed tag, field mapping, or table configuration creates a new
descriptor version and invalidates dependent shapes.

Struct field order and map iteration order must not silently determine SQL
shape. The mapper must provide deterministic column ordering and reject
unknown or missing mapped fields according to the operation's contract.

### Session/Unit-of-Work derivation

A Session derives statements from entity lifecycle state during flush:

```text
Session state
  → operation plan: INSERT / UPDATE / DELETE
  → selected fields and values
  → SST statement root
  → shape cache lookup
  → current args inside the transaction
```

For UPDATE, the dirty-field set is structural. Updating `{name}` and updating
`{name, active}` are different shapes. The Session may reuse a shape across
entities with the same dirty-field set, but identity-map entries, snapshots,
transaction handles, and entity values never enter the cache key.

The Session is therefore a producer of derivation inputs, not the owner of the
compiled artifact.

### Raw SQL passthrough

Raw SQL is an explicit escape hatch rather than a typed derivation strategy:

```text
trusted SQL template + bound args → Executor
```

A raw statement may be executed directly and may optionally participate in an
application-managed cache when the caller supplies a stable, trusted shape
identity. SQLok must not infer a safe structural shape from arbitrary raw text,
and it must never interpolate runtime values into that text.

### Shared derivation rules

All strategies must obey the same rules:

- structural inputs determine the shape key;
- runtime values determine only the current args;
- parameter count, order, slot identity, and supported value contract are
  validated before execution;
- unknown, missing, stale, or incompatible bindings fail before the executor
  is called;
- deterministic ordering is mandatory for columns, fields, rows, and bindings;
- structural mutation creates a new shape instead of mutating a published one;
- descriptor, compiler, dialect, and schema versions participate in
  invalidation when they affect the result.

## Session placement

The future ORM path should use shared compiled artifacts below the Session
boundary while keeping mapping and lifecycle responsibilities separate:

```text
Mapper
  └── entity metadata, scan targets, primary keys, mapped values

Session
  ├── identity map
  ├── pending entities and dirty snapshots
  ├── load/query orchestration
  └── explicit transaction/unit-of-work coordination
        → statement shape cache or PlanRegistry
        → current ArgumentBuffer
        → database/sql Executor
```

A Session can benefit from the cache when repeated flushes produce the same
shape, such as updating the same set of fields across many entities. It must
still provide fresh values for every execution.

The first cache POC intentionally excludes Session. Otherwise reflection,
identity-map lookups, dirty tracking, flush planning, SQL compilation, and
possibly database I/O would be measured together. Session benchmarks belong to
a second layer after the compiler cache behavior is understood.

SQLok should also keep the Core path independent from Session. A developer who
constructs a statement directly should be able to use the shape cache without
adopting the ORM/session layer.

## Artifact granularity decision

The current implementation has one concrete reusable artifact:
`CompiledStatement`. `Session` tracks identity and pending state, but it does
not yet implement `Flush`, operation planning, or executor coordination. An
end-to-end statement-versus-flush benchmark would therefore require inventing
an API that does not exist.

The current benchmark boundary is the prepared statement artifact:

```text
statement shape
  → CompiledStatement
  → PlanRegistry lookup
  → Bind([]any) or BindBuffer(ArgumentBuffer)
  → Executor
```

The controlled workload uses `-benchmem -benchtime=1s -count=5` and
`benchstat`. On Go 1.27.0, linux/amd64, AMD Ryzen 5 1600, the representative
scale-1 results were:

| Path | Time | B/op | allocs/op |
|---|---:|---:|---:|
| Direct compile | 4.55 us | 1288 | 15 |
| Ad-hoc `CompileCached` hit | 11.17 us | 1528 | 32 |
| `PlanRegistry` hit | 67.9 ns | 0 | 0 |
| Reused named `ArgumentBuffer` | 68.8–71.4 ns | 0 | 0 |

The ad-hoc hit remains slower because it derives the shape key on every call.
Stable `PlanID` lookup avoids AST traversal. Named `BindBuffer` now performs
real logical-slot validation and SQL-order emission while retaining zero
allocations when its buffer is reused. Full scale results and methodology live
in `docs/compiler-hardening-review.md`; the benchmark implementation is
`compiler/cache_workload_bench_test.go`.

The suite still has no flush measurement because no concrete `Session.Flush`
implementation exists.

Decision for the current cache:

- cache one immutable `CompiledStatement` per statement shape;
- let a future flush compose and reuse statement artifacts;
- do not introduce a flush-level artifact or cache key until `Session.Flush`
  defines a real operation plan and execution boundary.

This keeps eviction and invalidation focused on statement shapes for now. The
artifact unit can be revisited when the ORM/session path has a concrete flush
implementation and an apples-to-apples benchmark.

## Cache cardinality and memory measurement

The current `StatementCache` was measured with realistic statement-shape
cardinality tests and synthetic published compiled artifacts. Runtime-value
variations of one statement shape produce one cache entry, while structurally
different statements produce one entry per shape.

The retained heap measurement uses `runtime.MemStats.HeapAlloc` after a garbage
collection and repeats populations of 1,000, 10,000, and 100,000 entries. On
the Ryzen 5 1600 test machine, the observed retained heap was approximately:

| Entries | Retained heap | Approx. bytes/entry |
|---:|---:|---:|
| 1,000 | 263,792–269,528 B | 264–270 B |
| 10,000 | 2,230,112–2,230,144 B | 223 B |
| 100,000 | 18,679,776–18,702,096 B | 187 B |

The population benchmark uses prebuilt artifacts and reports the cache storage
cost separately from AST compilation:

| Entries | B/op | allocs/op |
|---:|---:|---:|
| 1,000 | 455,560 | 23 |
| 10,000 | 3,673,520 | 89 |
| 100,000 | 29,389,448 | 561 |

These are directional measurements, not a production memory budget. They do
establish that an unbounded shape cache has linear retained-memory growth, so a
general bounded policy is now justified. The bound still needs an explicit
operational choice; it must not be inferred from the old, already-closed
pagination-fragmentation finding.

## Bounded cache policy

`NewBoundedStatementCache(maxEntries)` provides an explicit bounded variant of
`StatementCache`. It evicts the oldest inserted shape when the limit is
exceeded. Updating an existing key does not consume another entry, and
`Invalidate` and `Clear` remove published shapes without changing the public
binding contract.

The policy is insertion-order eviction rather than LRU deliberately: `Get`
remains a read-only operation, so the bounded cache does not add a write lock or
promotion allocation to every lookup. `NewStatementCache()` remains the
unbounded compatibility constructor until an application workload supplies a
safe default budget. `PlanRegistry` is an application-owned stable-plan index
and has separate lifecycle responsibilities.

Use the bounded constructor when the caller owns an explicit memory budget:

```go
cache, err := compiler.NewBoundedStatementCache(10_000)
if err != nil {
    return err
}

shape, err := compiler.Prepare(cache, stmt, renderingDialect)
if err != nil {
    return err
}
return executor.Exec(ctx, target, shape, currentArgs)
```

`maxEntries <= 0` returns `compiler.ErrInvalidCacheLimit`. The constructor
does not choose a default budget because the measured bytes-per-entry value is
workload- and shape-dependent.

## Shape identity

A cache key must represent the structure that affects rendered SQL and binding
layout. It should account for at least:

- statement root: SELECT, INSERT, UPDATE, or DELETE;
- statement topology and clause order;
- target table and selected or assigned columns;
- operators, joins, grouping, and ordering;
- INSERT row count and column order where relevant;
- dialect and placeholder strategy;
- compiler artifact version;
- model/reflection descriptor version when a model API is used;
- schema or migration fingerprint when schema changes affect the mapping.

Runtime values are excluded. Changing `42` to `7` in `id = ?` must reuse the
same shape. Adding a JOIN, changing the UPDATE assignment set, or changing the
number of INSERT rows must produce a different shape.

The key must be canonical. It must not depend on pointer addresses, map
iteration order, process-specific memory state, or request data. Public row-map
APIs therefore need deterministic column ordering before they reach the SST.

## Invalidation

A cached artifact is valid only while all inputs that affect its meaning remain
valid. Candidate invalidation sources are:

```text
compiler version    → rendering/traversal changes
 dialect version    → placeholder or syntax changes
model descriptor    → struct tags, field order, or table mapping changes
schema fingerprint  → migrations or database shape changes
shape key           → statement structure changes
```

Development mode should favor fast feedback:

- build on a cache miss;
- invalidate when source or descriptor versions change;
- expose cold-build and warm-hit diagnostics;
- provide a simple cache-clear operation.

Production should favor deterministic deployment:

- build or warm artifacts during deployment or an explicit warm-up command;
- prefer a read-only cache directory at request time;
- validate artifact version, dialect, shape key, and integrity before use;
- treat a missing or stale artifact as an explicit operational decision;
- write replacements atomically only when runtime rebuilding is explicitly
  enabled.

The database may maintain its own prepared-statement or query-plan cache. The
SQLok cache is an application-side cache for AST construction, SQL rendering,
and binding metadata; it is not a replacement for the database optimizer.

## Security boundaries

The cache must preserve the compiler's parameter-binding boundary:

- never interpolate runtime values into cached SQL;
- never serialize secrets, credentials, or request payloads;
- validate or constrain dynamic identifiers before they enter a shape;
- never execute generated Go code, plugins, or arbitrary cache contents;
- keep production artifacts owned and writable only by the deployment process;
- avoid logging bound values when reporting cache hits and misses;
- treat raw SQL as an explicit trusted escape hatch, not as a cache shortcut.

A raw expression may affect a shape key, but untrusted source text must not turn
cached compilation into an arbitrary execution mechanism.

## Minimal benchmark POC

The first POC measures application-side construction and compilation only. It
does not claim to measure database throughput, network latency, or driver query
plans.

The benchmark comparison is:

```text
BenchmarkStringQueryAssembly
  direct string baseline;

BenchmarkASTCompileEndToEnd
  build a fresh AST and compile it on every iteration;

BenchmarkASTCompileExistingStatement
  reuse an AST but traverse and render it on every iteration;

BenchmarkCompileCachedMiss
  compile and publish a shape on every cache miss;

BenchmarkCompileCachedHit
  reuse one statement, derive its shape key, reuse the cached SQL shape, and
  bind supplied values without re-rendering SQL. This still measures shape
  derivation on each call;

BenchmarkASTCompileCachedShape
  prepare one shape, select current arguments from a preallocated ring, and
  reuse its `CompiledStatement.Bind` path;

BenchmarkArgumentRingLookup
  calibrate the cost of selecting current arguments from that ring;

BenchmarkPlanRegistryHit
  look up one prepared shape by application-owned `PlanID`, select current
  arguments from the ring, and bind without shape-key derivation;

BenchmarkWorkloadCompileDirect / CompileCachedHit / PlanRegistryHit
  compare the three execution strategies as statement complexity scales;

BenchmarkWorkloadDeriveShapeKey
  isolate the structural fingerprint cost paid by the ad-hoc cache path;

BenchmarkWorkloadNamedArgumentBufferHit
  reuse named logical slots and an `ArgumentBuffer` across executions.
```

### Running and comparing benchmarks

`benchstat` is the comparison tool from `golang.org/x/perf`. It reads the
benchmark output produced by `go test` and compares repeated samples for
execution time, bytes allocated, and allocations. It is a development tool,
not a runtime or module dependency.

Install it outside the repository:

```bash
go install golang.org/x/perf/cmd/benchstat@latest
```

This writes the binary to `GOBIN` or `GOPATH/bin` and does not change
`go.mod`, `go.sum`, or the application binary.

Run the current compiler benchmarks with a time-based sample and repetitions:

```bash
go test -run '^$' \
  -bench 'Benchmark(ASTCompileExistingStatement|CompileCachedHit|ArgumentRingLookup|ASTCompileCachedShape|PlanRegistryHit)$' \
  -benchmem -benchtime=1s -count=10 ./compiler
```

Run the scalable workload and named-buffer benchmarks separately:

```bash
go test ./compiler -run '^$' \
  -bench '^BenchmarkWorkload' \
  -benchmem -benchtime=1s -count=10
```

The flags mean:

- `-run '^$'` skips ordinary tests and runs only benchmarks;
- `-bench` selects the benchmark names under comparison;
- `-benchmem` reports `B/op` and `allocs/op`;
- `-benchtime=1s` avoids timer-resolution and short-run GC distortion;
- `-count=10` provides enough samples for `benchstat` confidence intervals.

To compare a clean baseline with the current tree, both versions must be
measured with the same command. A temporary detached worktree keeps the
baseline separate and must be removed after the comparison:

```bash
set -eu
before=$(mktemp)
after=$(mktemp)
worktree=$(mktemp -d)
cleanup() {
  git worktree remove --force "$worktree" >/dev/null 2>&1 || true
  rm -f "$before" "$after"
}
trap cleanup EXIT

pattern='Benchmark(ASTCompileExistingStatement|CompileCachedHit|ArgumentRingLookup|ASTCompileCachedShape|PlanRegistryHit)$'
go test -run '^$' -bench "$pattern" -benchmem -benchtime=1s -count=10 ./compiler > "$after"
git worktree add --detach "$worktree" <baseline-commit>
(
  cd "$worktree"
  go test -run '^$' -bench "$pattern" -benchmem -benchtime=1s -count=10 ./internal/compiler > "$before"
)
benchstat "$before" "$after"
```

Replace `<baseline-commit>` with the commit being compared, such as
`b7f03d5`. That baseline predates the public package move, so its benchmark
package is `./internal/compiler`; use `./compiler` in both commands for a
baseline after that move. The command creates no persistent worktree after the
`trap` cleanup. Do not compare a single short run with a repeated time-based run.
Record the `benchstat` result and its methodology together; isolated figures
are useful for local iteration but are not reliable findings.

Interpret the output conservatively:

- `sec/op` is the time per benchmark operation;
- `B/op` is the allocated memory per operation;
- `allocs/op` is the number of allocations per operation;
- `~` means the measured time difference is not statistically significant;
- a ring-inclusive warm-path result includes the disclosed argument-selection
  cost and must not be compared directly with a hoisted-argument result.

The warm path is explicit. A legacy positional layout can continue to use
`[]any`; a named layout must address each logical slot explicitly:

```go
shape, err := compiler.CompileShape(stmt)
if err != nil {
    return err
}

args := shape.NewArgumentBuffer()
if err := args.Set("user_id", currentUserID); err != nil {
    return err
}
bound, err := shape.BindBuffer(args)
if err != nil {
    return err
}

execute(shape.SQL(), bound)
```

For a registry-backed plan, `compiler.PlanRegistry` stores the shape under an
application-owned external `PlanID` after the first-use compile:

```go
shape, err := compiler.Prepare(cache, stmt, renderingDialect)
if err != nil {
    return err
}
registry := compiler.NewPlanRegistry()
if err := registry.Put("users-by-id", shape); err != nil {
    return err
}

shape, ok := registry.Get("users-by-id")
if !ok {
    return errors.New("prepared plan not found")
}
return executor.Exec(ctx, target, shape, currentArgs)
```

The registry lookup does not derive a shape key or traverse a statement. It is
an explicit warm-path index, not a replacement for the canonical
`StatementCache`; both can be used together during cold preparation.

`CompileCached` remains a convenience for callers that provide a statement on
every call. It must derive the shape key each time to prevent collisions and
makes no warm-path performance promise. Code that already owns a stable
statement plan should retain the prepared `CompiledStatement` or publish it in
`PlanRegistry` and reuse it directly.

The benchmark must preserve dynamic values. The warm path may reuse the SQL
template, but it must create or populate current arguments for each iteration;
it must not reuse values from the warm-up execution.

The next benchmark layer should compare the same statement shape and values
across:

```text
direct database/sql or sqlc-like generated code
SQLok cold compilation
SQLok warm shape cache
SQLok ORM mapper + Session + flush + warm shape cache
```

These should remain separate measurements. Adding Session to the first test
would hide whether a result came from caching SQL compilation or from unrelated
identity-map and reflection behavior.

## Remaining implementation phases

The compiler artifact, bind layout, bounded cache, and `PlanRegistry` warm path
are implemented. Remaining work is driven by the public ORM experience:

1. Implement and cache stateless Mapper descriptors separately from SQL shapes.
2. Refactor Session to consume Mapper metadata for primary keys and mapped
   values instead of repeating reflection.
3. Prove database-backed `Session.Load` end to end: prepared plan, execution,
   row mapping, Identity Map registration, and pointer reuse.
4. Implement explicit Flush planning for pending and dirty entities.
5. Expose a cohesive model-oriented API that hides compiler/cache/registry/bind
   plumbing while retaining Core and direct SQL escape hatches.
6. Benchmark the complete ORM path against direct SQL, cold Core, and warm Core
   using equivalent statements and values.
7. Consider database prepared-statement reuse, persistent artifacts, and warm-up
   commands only after the real ORM consumer provides evidence for them.

## Non-goals

This design does not propose:

- caching request-specific SQL values;
- caching query results as a replacement for a data/cache layer;
- hiding transaction ownership inside a global cache;
- generating executable Go source at runtime;
- forcing Session or ORM behavior onto the Core statement API;
- promising that SQLok will outperform generated SQL without benchmarks.
