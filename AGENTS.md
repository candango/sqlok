# Agent instructions

## SELECT API direction

- Use SQLAlchemy as the direct reference for SQLok's public query API and user experience: query construction, composition, result access, and ORM integration should feel similar.
- `LoadContext` and `Load` were removed; do not reintroduce them. Use the SQLAlchemy-like SELECT API for ORM reads.
- Preserve SQLAlchemy's API shape and semantics wherever applicable; do not dilute this direction with generic caveats.
- Deviate from SQLAlchemy only for a very strong, specific reason grounded in a real SQLok or Go constraint. Explain why the SQLAlchemy-shaped design does not work; convenience, novelty, or generic stylistic preference is not enough.

## API-first, top-down development

- Start from the developer-facing SQLAlchemy-like usage and expected behavior, then define the public Go API, Session/result/mapping behavior, and only then implement the supporting AST/compiler/executor layers.
- Do not let existing bottom-up internals dictate public ergonomics. `sst/dql.Select` is a low-level builder; the root `sqlok.Select` facade is the ORM read API.
- Use `skills/sqlok-expert/SKILL.md` as the SQLok agent playbook and implementation ledger. Mark unsupported behavior `WIP`; mark it implemented only when the public behavior is backed by code and relevant tests. Update the skill as implementation progresses.
- Treat performance as a non-regression requirement: measure relevant existing benchmarks before and after changes, preserve hot-path plan/metadata caching, and investigate material regressions before accepting the implementation. Do not claim performance gains or zero regression without reproducible measurements.

## SELECT AST guide mode

When guiding the SELECT AST work:
- Use GUIDE mode by default.
- Be concise, but not cryptic.
- Aim for 5-10 lines; go beyond 15 only when the answer truly needs it.
- No lectures, recaps, status dumps, or workflow exposition unless requested.
- Answer the exact question first.
- Give one useful step at a time.
- Explain just enough for the user to act.
- If code is requested, provide only the minimum code for the current step.
- Do not dump large code blocks.
- End with a small next action only when useful.
