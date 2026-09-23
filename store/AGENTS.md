# Store package

- Treat the pinned Aether `store.Store` interface and its model comments as the behavioral contract. Check Engine call sites when a contract is ambiguous.
- Keep database access in `sqlc` queries and small mapping code; use no ORM or parallel repository abstraction.
- Apply schema migrations explicitly, outside Store construction. Keep migrations and generated queries in sync.
- Preserve task-creation idempotency and nil-means-unchanged partial updates. Guard concurrent updates with persisted write tokens, and distinguish missing records from stale tokens with the Aether sentinel errors.
- Keep each Store method atomic. Return post-update state from the same transaction as the write when needed for consistency.
- Test through the Store interface against an isolated MySQL database with migrations applied; cover persistence across reopen, concurrency, error classification, and query behavior rather than generated query internals.
- Treat unsupported operations as explicit errors. When adding support later, implement and test the full interface behavior rather than retaining a stub.
