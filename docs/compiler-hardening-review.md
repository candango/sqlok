# Compiler and Cache Hardening: Review Findings and Direction

Record of a review session held on 2026-08-23 covering the SST compiler, the
compiled statement cache, target databases, and the MyFuses caching model that
inspired the cache design.

Every finding below was reproduced or measured against the working tree, not
inferred from reading alone. Where something is inference, it is labeled as
such. Statuses and measurements were reconciled with the current tree on
2026-09-20.

## 1. Findings

### 1.1 Cache key was supplied by the caller (resolved)

**Problem.** `StatementCache` accepted a `ShapeKey` chosen by the caller and
never checked it against the statement. Reusing one key across two different
statements returned the first statement's SQL while binding the second
statement's arguments, with no error.

**Evidence.**

```text
CompileCached(cache, "k", selectFromUsers)  -> "SELECT u.id FROM users"
CompileCached(cache, "k", selectFromOrders) -> "SELECT u.id FROM users"   (err == nil)
```

`docs/compiled-statement-cache.md` already required the key to be derived from
canonicalized SST topology, target identifiers, clauses, operators, and binding
slots, and required it not to depend on pointer addresses or map ordering. The
implementation was one layer behind its own specification.

**Solution.** Commit `aaabbf1` introduced `DeriveShapeKey`, which walks the
statement through a dedicated `shapeFingerprint` visitor and hashes the result.
The key parameter was removed from the public call, so the defect is no longer
expressible. The fingerprint uses length-prefixed tokens
(`fmt.Fprintf(&f.builder, "%d:%s", len(value), value)`), which prevents
concatenation collisions, and emits a bare `bind` token for bind parameters so
runtime values never reach the identity.

`ShapeContext` was added at the same time, carrying `Dialect` and
`CompilerVersion`, satisfying the shape identity requirement in the cache
document.

### 1.2 Identifiers were rendered without validation or quoting (resolved)

**Problem.** Schema, table, and column identifiers were joined into SQL with
`strings.Join` in `VisitColumnRef` and `VisitTableRef` with no validation.

**Evidence.**

```text
Compile(Select(...).From(NewTableRef("users; DROP TABLE users --")))
-> "SELECT u.id FROM users; DROP TABLE users --"   (err == nil)
```

Identifiers cannot be parameterized, so validation or dialect quoting is the
only available control. This is the SQL injection boundary named in
`docs/vision.md`.

**Solution.** Commit `9df65db` added identifier validation at the compiler
boundary. Dialect-specific quoting remains part of the dialect work described in
section 4.

### 1.3 SELECT with no projected columns produced invalid SQL (resolved)

**Problem.** `SelectStatement.Accept` did not guard its required children, while
`InsertStatement.Accept` and `UpdateStatement.Accept` did.

**Evidence.**

```text
Compile(Select().From(NewTableRef("users")))
-> "SELECT  FROM users"   (err == nil)
```

**Solution.** Commit `ea1ce40` rejects empty SELECT projections, restoring
symmetry with the other statement roots.

### 1.4 Ad-hoc cache hits are slower than direct compilation (known tradeoff)

**Finding.** `CompileCached` is an ad-hoc convenience path: it must derive the
shape key from the statement on every call before it can look up the cache. It
is therefore slower than direct compilation for the current AST sizes. This is
a measured API tradeoff, not a correctness defect.

**Current evidence.** On 2026-09-20 with Go 1.27.0, amd64 Ryzen 5 1600, using
`go test ./compiler -run '^$' -bench
'^BenchmarkWorkload(CompileDirect|CompileCachedHit|PlanRegistryHit)$'
-benchmem -benchtime=1s -count=5`, `CompileCachedHit` remained slower than
`CompileDirect` at every scale. `PlanRegistryHit` stayed in the warm tier with
zero allocations.

**Decision.** `Prepare` derives the shape once; `PlanRegistry` provides the
explicit identity needed by the warm path. `CompileCached` remains useful for
ad-hoc callers and makes no warm-path promise. The ORM layer, which is not yet
present in this repository, must choose between those paths.

### 1.5 Row limiting fragmentation and unbounded cache growth (resolved)

**Problem.** The original implementation included literal LIMIT/OFFSET values
in shape identity and exposed only an unbounded cache.

**Solution.** Dialect rendering now reserves runtime LIMIT/OFFSET slots, so
page values do not fragment the shape key. `NewBoundedStatementCache` provides
configurable insertion-order eviction; `Invalidate` removes stale eviction
entries and `Clear` resets the queue. `NewStatementCache` remains explicitly
unbounded for compatibility, so production callers must choose the bounded
constructor when request variety is unbounded.

**Evidence.** Cardinality tests keep runtime-value variations at one entry;
bounded-cache tests cover eviction, sustained queue bounds, invalidation,
reinsertion, empty invalidation, and Clear. Commits: `291a997`, `c566b29`, and
`74fdfec`.

### 1.6 Bind parameter placeholder was hard-coded (resolved)

**Problem.** The original SST path hard-coded `?`, which could not represent
PostgreSQL numbered placeholders.

**Solution.** Placeholder allocation belongs to the compiler dialect. The
compiler assigns each binding its SQL position, while dialects render the
placeholder format. The default/question-mark, MySQL, SQLite, and PostgreSQL
identities are part of shape context, so equivalent SQL text from different
dialects does not collide. Commit `291a997` completed this slice.

### 1.7 `OFFSET` without `LIMIT` is rejected (resolved)

The builder now records `ErrOffsetRequiresLimit` and rejects an offset-only
statement before compilation. Offset-before-limit construction remains
repairable before validation. This preserves the supported PostgreSQL,
MySQL, and SQLite baseline without inventing dialect-specific sentinel limits.

### 1.8 Fluent interface with no consumer (resolved)

The unused `sst.SelectBuilder` interface was removed and fluent methods now
return `*dql.SelectStatement`, preserving concrete builder ergonomics without
introducing a package cycle. The legacy `internal/builder.go` type remains a
separate concern. Commit `f1be7e7` completed this correction.

### 1.9 Parameter slots and logical bind identity (resolved)

**Problem.** Plan registration originally required `NewBindParam(nil)`, and
positional binding could not preserve logical source identity.

**Solution.** `ParameterSlot` declares a value-free positional slot, while
`NamedParameterSlot` declares an explicit logical source. `Binding.Source`
retains that identity, `ArgumentBuffer` validates unknown/missing/duplicate
slots, and `BindBuffer` emits values in SQL order without guessing from runtime
values. Legacy positional `Bind([]any)` remains available for unnamed layouts.

The E2E regression `TestNamedPreparedPlanRoundTripThroughCache` now covers
`Prepare` → bounded `StatementCache` → `PlanRegistry` → reusable
`ArgumentBuffer` → executor across multiple values. Commits: `17172d2` and
`d47b965`.

### 1.10 Named slots are intentionally outside `CompileCached` (documented)

`CompileCached` accepts positional `[]any` by design and therefore remains the
ad-hoc path for legacy layouts. Named slots use `Prepare`, `PlanRegistry`, and
`BindBuffer`, where the application-owned plan identity is already known. A
future ORM layer may add a higher-level adapter, but the compiler package does
not infer logical identity from runtime values.

### 1.11 Plan invalidation is an application-owned lifecycle (open)

`StatementCache.Invalidate` and `Clear` remove compiler cache entries, but they
do not silently delete plans published under `PlanRegistry`. Published plans
are immutable; callers must replace or stop publishing a plan when dialect,
compiler, schema, or application configuration changes. An automatic coupling
between the two registries remains a future lifecycle decision.

## 2. Measurements

Measured on 2026-09-20 with Go 1.27.0, linux/amd64, AMD Ryzen 5 1600, using:

```text
go test ./compiler -run '^$' -bench \
  '^BenchmarkWorkload(CompileDirect|CompileCachedHit|PlanRegistryHit)$' \
  -benchmem -benchtime=1s -count=5
```

The values below are `benchstat` summaries from five samples; five samples are
sufficient for repeatability checks but not for its 95% confidence interval.
Each cell is `ns/op`, `B/op`, and `allocs/op`.

| Scale | Direct compile | `CompileCached` hit | `PlanRegistry` hit |
|---|---:|---:|---:|
| 1 | 4.55 us / 1,288 / 15 | 11.17 us / 1,528 / 32 | 67.9 ns / 0 / 0 |
| 4 | 12.44 us / 3,152 / 41 | 33.02 us / 4,746 / 88 | 68.5 ns / 0 / 0 |
| 16 | 42.33 us / 13,232 / 131 | 122.7 us / 18,650 / 307 | 70.8 ns / 0 / 0 |
| 64 | 152.4 us / 51,760 / 473 | 498.9 us / 73,875 / 1,175 | 104.1 ns / 0 / 0 |

The result is stable: ad-hoc shape discovery is slower than direct compilation,
while explicit plan identity avoids AST traversal and remains allocation-free.
The workload benchmark is `compiler/cache_workload_bench_test.go`; its named
`ArgumentBuffer` path measured 68.8–71.4 ns/op with zero allocations across
five runs.

## 3. Target databases

`README.md` names PostgreSQL as the current integration-test target, with MySQL
and SQLite on the roadmap. `docs/execution-strategies.md` states that SQLok must
not require a concrete vendor driver, and `docs/vision.md` excludes `pgx`, MySQL
and SQLite drivers, and driver registration from the core.

Oracle and SQL Server are out of scope. They were never targets; they appeared
once in a research note citing SQLAlchemy dialect documentation.

| Database | Status |
|---|---|
| PostgreSQL | Current target; integration-test requirement and setup scripts |
| MySQL | Roadmap, not started |
| SQLite | Roadmap, not started |

Consequence for row limiting: `LIMIT n OFFSET m` is a genuine common
denominator across all three, and is what the compiler already emits, in the
correct order, including after `ORDER BY`. Verified output:

```text
SELECT u.id FROM users ORDER BY u.id ASC LIMIT 10 OFFSET 5
```

This supersedes the earlier decision that modeled ANSI row limiting as
`OFFSET`/`FETCH` with `LIMIT` deferred to dialect rendering. That decision was
driven by Oracle and SQL Server portability, and those are no longer in scope.

The placeholder divergence is now owned by the dialect layer, while offset-only
queries are rejected before rendering. The current dialect scope is therefore
placeholders, identifiers, and the supported LIMIT/OFFSET validation boundary;
vendor drivers remain outside this core.

## 4. What transfers from MyFuses

The cache design draws on MyFuses, a PHP front controller written 2006-2009,
rewritten for PHP 7.x, running on PHP 8, and still in production. This section
records what the model actually is, because the analogy was initially applied
too broadly.

### 4.1 The mechanism

The cold pass reads XML, hydrates an object tree, and emits PHP source. The warm
pass includes the generated file and executes native code; the XML parser and
the object tree are absent from the hot path.

The phases are distinct, and the naming matters:

```text
load   source (XML) or cached artifact  -> data array
build  data array                       -> object tree
store  object tree -> getParsedCode()   -> PHP on disk
```

`build` is hydration, not generation. Code emission happens in `store`.

### 4.2 The real insight: artifact granularity equals request granularity

Fusebox analyzed every circuit on every request, so an application with 100
circuits was impractical. MyFuses materialized only what the request path
reached. An application with 100 circuits paid for the 4 to 8 that participated.

Inspecting a production artifact directory confirms how far this goes: there is
**one generated file per fuseaction**, named `<circuit>.<fuseaction>.php`, 81
files totaling 6.2 MB. The generated file is fully flattened. The request graph
was resolved at build time and spliced inline:

```php
$myFusebox->thisPhase = "preProcess";
include(... "plugins/fusebuilderlive_wf/index.php");
/* do action="cSystem.SessionStart" */
$myFusebox->thisCircuit = "cSystem";
include(... "controller/system/actSessionStart.php");
...
$myFusebox->thisFuseaction = "DBClose";
include(... "controller/system/actDBClose.php");
```

At runtime there is no dispatch, no lookup, and no graph walk. The cost is
duplication: shared prologue and epilogue are inlined into every artifact, which
is why individual files reach 450 KB. Space was traded for time, and at that
ratio the trade paid.

Invalidation is `filemtime` of the source against the recorded last load time,
per unit, and is gated by an application `mode` parameter: `development`
rebuilds always, `production` rebuilds only on detected modification. The mode
is an ordinary configuration string, not a special abstraction.

### 4.3 What transfers, and what does not

Transfers:

- The cold/warm split with an immutable published artifact. `CompiledStatement`
  is the analogue of the generated PHP file, and the roughly 68–104 ns
  `PlanRegistry` warm path is the same phenomenon at smaller scale.
- Addressing by external identity instead of discovery by analysis. MyFuses
  resolves a circuit by name; it never inspects content to decide which artifact
  to load. `DeriveShapeKey` walking the tree to discover which entry applies is,
  in miniature, the Fusebox pattern.
- Lazy materialization. `Lifecycle::checkCircuit` loads a circuit only when it
  is first touched, because the working set is not knowable at boot. This
  favours prepare-on-first-use with a durable handle over a startup registry.
- Replacement on rebuild. `buildPlugins` calls `clearPlugins()` before
  rebuilding, so a rebuild replaces rather than accumulates.
- Explicit defaults before reading configuration, so the object is never in an
  undefined state.

Does not transfer:

- The `load` and `build` phases. They exist because the MyFuses source is XML on
  disk. In SQLok the source is Go code and the tree is already hydrated by the
  fluent builder. Only `store` has a direct analogue.
- Sub-statement caching. A circuit is a unit of business with a name and an
  independent lifecycle; a `WHERE` fragment is not. Caching fragments of a
  small tree likely costs more in complexity than it saves.

### 4.4 Limits of this reading

`FuseRequest`, `FuseQueue`, and `XfaVerb` were not read. The depth-first graph
resolution is inferred from the generated artifact, which is strong evidence —
the flattening is visibly present — but the resolution mechanism itself was not
inspected. No item in section 6 depends on that detail.

Separately, the production artifacts contain plugin configuration materialized
into generated PHP, including plaintext credentials for a wireframer plugin.
Generated-artifact directories should be excluded from version control and
treated as containing whatever configuration the build had access to.

## 5. Lifecycle

Corrected for SQLok, without the phases that only make sense for a
file-sourced framework:

```text
Go builder / model
  -> compile and freeze
  -> publish CompiledStatement
  -> PlanRegistry.Get
  -> Bind([]any) or BindBuffer(ArgumentBuffer)
  -> Executor
```

Invalidation and rebuild are a separate concern layered on top, not phases of
this sequence. `StatementCache` owns derived shape artifacts; `PlanRegistry`
owns application-published prepared plans and must be replaced explicitly when
its inputs become stale.

The artifact holds the SQL template, bind layout, dialect and compiler
identity, model or schema version, and fingerprint. It never holds runtime
values, a Session, a transaction, or a connection.

Two execution contracts are explicit:

1. **Prepared plan.** Known identity; prepared once, retained under `PlanID`,
   and reused through `PlanRegistry`. Promises warm-path performance.
2. **Ad-hoc query.** Shape derived on the spot through `CompileCached`. May use
   a bounded cache, but makes no warm-path promise.

## 6. Direction, in priority order

1. **Public ORM consumer.** The compiler, cache, registry, and executor are
   internal engine pieces; the repository still lacks the Session/mapper/API
   layer that chooses prepared versus ad-hoc execution.
2. **Prepared-plan lifecycle.** Define replacement/invalidation semantics for
   `PlanRegistry` when dialect, compiler, schema, or application configuration
   changes.
3. **Ad-hoc cache observability.** Add hit/miss counters only if the future ORM
   needs runtime promotion or operational visibility; do not infer repetition
   by paying the full AST derivation cost on every request.
4. **Named-buffer benchmark, complete.**
   `BenchmarkWorkloadNamedArgumentBufferHit` measures reusable named binding at
   approximately 70 ns/op with zero allocations on the current machine.

Schema fingerprinting remains excluded until SQLok has schema knowledge.

## 7. Deliberately open

These are decisions withheld for lack of evidence, not oversights:

- **Public prepared-query ergonomics:** whether repository accessors declare
  stable `PlanID` handles or the ORM discovers repetition and promotes shapes.
- **Registry invalidation:** whether application code replaces plans directly or
  a future version couples plan publication to schema/dialect/compiler epochs.
- **Artifact unit:** one statement, or one flush. A concrete Session/Flush API
  does not exist yet, so choosing a flush artifact now would invent scope.

## 8. Design goals this serves

Two goals were stated for SQLok: good performance, and an API ergonomic for the
programmer.

These conflict only if statement construction happens per request. With a
prepared plan held across requests, construction is a cold path and can afford
to be expressive, validating, and allocating, because it runs once per process
lifetime. This is the same trade MyFuses made: the slow pass is allowed to be
slow because there is only one of it.

On the SQLAlchemy comparison: much of SQLAlchemy's ergonomics rests on Python
operator overloading, which Go does not have and will not get. `sst.Eq(col,
val)` and `sst.And(...)` are the ceiling in that dimension, and pursuing the
literal syntax would produce poor Go. The dimension where Go can exceed the
reference is generics: typed column references and typed scan targets, moving
errors from runtime to compile time.

Worth knowing: SQLAlchemy's 1.4+ compiled cache derives its key from statement
structure, exactly as `DeriveShapeKey` does, and the project invested
substantially in making that derivation cheap through memoization on element
classes. The cost measured in section 2 is inherent to the approach rather than
an implementation defect, and the resolution is the same one reached here: avoid
deriving when the identity is already known.
