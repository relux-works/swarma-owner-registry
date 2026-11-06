# State and actual adapter seams

There is no plugin API, loader, manifest, registration function or plugin flag in this frozen module. Its extension points are repository-local Go interfaces and function fields. For an explicitly requested adapter change, write an implementation of the selected existing contract and wire it at the named constructor/input field; there is no separate registration step. The following is source guidance, not permission to implement or activate a live adapter.

## Enrollment transaction

Read [enrollment.go](../../../../internal/enrollment/enrollment.go). `enrollment.New(Trust)` requires the protected enrollment gate, a caller-supplied `Trust.Verifier`, `Trust.Transaction`, policy inputs and independently reconciled retained history. The verifier is the pinned registrar interface: this adapter relies on it for exact approved signed-commit binding and ancestry. It does not fetch a branch or verify Git signatures itself. No production enrollment transaction implementation ships here.

`Projection` contains the complete export revision, regular immediate `enrollments/*.json` entries and exact record-byte mirrors. These are contract paths, not a supplied local export directory. An invalid entry refuses the whole load. Exact archived exports must remain available; missing state is not first use.

The actual `Transaction` method signatures are:

```go
Preflight(Candidate) (Binding, error)
Intent(Binding) error
Commit(Binding) (Binding, error)
Resolve(Binding) (Resolution, error)
```

Implement these as methods of the caller's transaction type. `Preflight` must exclude old instances, bound and retain the complete recoverable candidate inventory and preview the local state before external intent. `Intent` obtains the durable external acknowledgement. `Commit` durably retains the exact inventory and reports the same candidate/state binding. `Resolve` independently verifies commit and final authority/time/bytes before matching resolved visibility. Digest equality is a binding check, not authentication or durability evidence.

Wire the implementation through `enrollment.Trust.Transaction` passed to `enrollment.New`; supply the protected verifier through `Trust.Verifier`. `Adapter.Load` orders preflight, intent, commit, resolve through its Store bridge only after loader/mirror validation. Any failed load returns nil and permanently fences the instance. Stop using old snapshots when `Fenced()` is true. `LastResolved()` is copied recovery evidence, not proof of unchanged disk after uncertainty.

For an adapter's scoped offline tests, follow the existing transaction double and assertions in [enrollment_test.go](../../../../internal/enrollment/enrollment_test.go): `TestTransactionFailuresFence` covers each stage, uncertain resolution and wrong bindings; `TestEnrollmentGateClosedByDefault` covers closed gates and missing/typed-nil dependencies. Assert that later calls never run after a failure and a repaired double cannot clear the fence. These controls do not qualify real Git, witness transport or durability.

## Local store

Read [store.go](../../../../internal/store/store.go), [recovery.go](../../../../internal/store/recovery.go) and [fs_linux.go](../../../../internal/store/fs_linux.go). `store.Transactions` is a Linux local-storage interface, distinct from the enrollment transaction. `Create` bootstraps an existing empty private directory explicitly; `Open` and `Reconcile` require the independently protected expected state ID. Do not turn missing state into bootstrap.

`Preview(Request)` returns a checked local candidate identity without writes or sequence reservation. `Commit(Request)` repeats checks under the store lock, retaining exact request/outcome bytes. An exact retry returns the original outcome without renewal. `CreateWithOptions` and `OpenWithOptions` accept `Options.MaxBytes`; zero selects the default 1 GiB logical regular-file budget. This excludes filesystem allocation/inode costs. Capacity preflight charges retained and new bytes before writes; uncertainty fences with `state_unavailable`.

This store checks local recovery lineage, no-follow paths and cooperating local exclusion. It authenticates no signed record, human owner, remote witness or independent restore floor. Its on-disk format is local recovery state, not a public registry protocol. Wrapping it in an enrollment transaction still requires complete inventory retention and external intent/resolution. Do not equate `store.Commit` with publication.

## Candidate production and time

Read [snapshot.go](../../../../internal/snapshot/snapshot.go), [journal publication](../../../../internal/journal/publication.go), [currency publication](../../../../internal/journal/currency.go) and [freshness.go](../../../../internal/freshness/freshness.go).

`snapshot.Issue`, `Build` and `Prepare` produce private snapshot/publication candidates. They compare caller-supplied complete cumulative history; they do not discover withheld history, serialize external transitions or qualify genesis. Both 64 entries and 8,192 whole signed bytes are hard snapshot bounds. `journal.BuildPublication` and `BuildCurrencyPublication` retain exact proof originals; a prepared candidate is not a publication receipt. The latter's `CurrencyPublication.Capacity` callback checks complete archive and caller quotas before custody.

`freshness.New(Trust)` requires reconciled complete state and the independent `Recovery.StateID`, with explicit genesis authorization where needed. Supply an actual persistence adapter through `freshness.Trust.Persistence`. Its `Persistence.Commit(Update) (string, error)` must compare the prior digest, atomically retain the whole next inventory/outcome and acknowledge the exact durable state ID. Version 2 additionally requires `CurrencyPersistence.Preflight(before string, maximumAfterBytes int) error` before signing. There is no production implementation here.

`Publisher.Publish` returns bytes only after a matching persistence acknowledgement; uncertainty fences. `Publisher.Retry` preserves original publication bytes, position and outcome while rechecking new time evidence and retaining new floors. It does not create a renewed publication. Inspect [publisher_test.go](../../../../internal/freshness/publisher_test.go) for persistence failure/digest and retry controls and [currency_test.go](../../../../internal/freshness/currency_test.go) for /2 capacity/recovery cases when testing an explicitly requested adapter. Test doubles establish local contract behavior, not qualified storage or witness facts.

[qtime.Interval.Project](../../../../internal/qtime/interval.go) checks supplied intervals and conservatively rounds them; it performs no clock collection, boot/sample authentication or continuity recovery. Keep provider floors separate from the registry watermark and retain the decision floor outside rollback. A local digest or true qualification boolean cannot replace independent evidence.

## Custody and signing — OWNER-SITTING ONLY

The source contracts are [journal.Signer and SignatureService](../../../../internal/journal/signer.go), `journal.NewSigner` and [NewCurrencySigner](../../../../internal/journal/currency.go). The former constructor supports only registry-owned `revocation/1`, `checkpoint/1` and `freshness/1`; the currency constructor supports `revocation/1`, `checkpoint/2` and `freshness/2` and refuses /1 downgrade. These are closed body/profile gates over a supplied transport, not a generic signing API or an owner-activation signer.

`SignatureService.Sign(preimage []byte) ([]byte, error)` is wired through the constructor's service argument, and the returned signature is checked against the original pinned preimage. The module supplies no key store, credential provisioning, custody launcher or custody qualification. Owner presence, keys, credentials, custody setup and all signing flows remain owner-only sitting work. This section gives agents no signing or provisioning steps.
