# Generated keys in Session.Flush

`Session.Flush` supports generated primary keys for pending entities when the
executor returns a usable `sql.Result.LastInsertId` value.

## Contract

- The entity must have exactly one primary-key field.
- The primary-key field must be an integer or unsigned integer type, including
  pointer forms.
- The entity is inserted without its missing primary-key value.
- SQLok reads `LastInsertId`, assigns the value to the entity, registers the
  entity in the Session Identity Map, and records the post-insert snapshot.
- The caller still owns the transaction. `Flush` never commits or rolls back.
- If the driver cannot provide `LastInsertId`, `Flush` returns
  `ErrGeneratedKeyUnsupported` and keeps the pending entity in Session state.

Composite generated keys, non-numeric generated keys, and dialect-specific
`RETURNING` strategies are not inferred by this contract. They require a
separate dialect capability and explicit design. Applications using a driver
that does not support `LastInsertId` must provide their own insert/key path until
that capability exists.

After a successful `Flush`, a caller that rolls the transaction back must
discard the Session before retrying, because the Session has already updated
its identity and snapshot state.
