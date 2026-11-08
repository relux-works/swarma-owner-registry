# Current agent local read core

This package is a finite local projection component. It is buildable but **not
a protected CurrentAgent provider**. A zero Reader always refuses, and there is
no exported admission constructor, injected source callback, persistence format
or connector. Successful tests construct private synthetic typed premises.

The returned Agent keeps exactly the existing LinuxRegistryAgent members. One
later composition must copy them to the existing CurrentAgent(key) consumer
port. It must preserve the full independently installed MXID, key, UID and
registration mapping; a response cannot appoint an identity. The core compares
all binding members and the exact reconciled head/lineage. It deliberately
cannot order opaque revisions, prove ancestry, advance a floor or bootstrap an
empty state. No lexicographic generation/epoch comparison is used.

The core bounds every retained string to 255 UTF-8 bytes, keys to the existing
canonical Ed25519 grammar, UID to 0..4294967294 and registration number to
1..9007199254740991. These are component ceilings, not selected deployment or
transport policy. A read performs no I/O, retries or goroutine creation. Lock
contention refuses immediately. Errors disclose no partial identity. The
original finite local deadline is checked twice and never renewed. Local time
enforces only that operation budget; it is never source time qualification.

Source owners must supply these inputs before a constructor can be implemented:

* The authoritative D1 transition writer and exact active-generation read
  contract, including registration, retirement, recreation and publication ack.
* Authenticated origin and read principal from the admitted transport/session;
  exact subject/owner disclosure policy, independently installed sender mapping,
  framing/proof/total-byte, deadline, invalidation and concurrency bounds.
* Authoritative revision/lineage order, complete retained transition/tombstone
  history, independent surviving high-water custody, restart reconciliation,
  old-instance exclusion and explicit authorized recovery. A local store read
  or equal checksum is insufficient. Missing state is never genesis.
* The consumer's final-call/update serialization contract. This package's local
  mutex ends at return; it cannot fence a remote writer or hold a consumer effect
  boundary. Even a successful synthetic sample cannot authorize a later effect.

Private handles currently check only process-local binding/refencing. Their
presence is not authenticated evidence, continuity, durable high water or a
qualified freshness claim. No production function creates them. The future
connector must qualify the actual sources before it can create any admission;
adding booleans, an exported fixture constructor or a callback is insufficient.

No attestation read, snapshot, assurance, root/key operation, new wire, store
schema, dependency change, server, deployment or live grant is included. The
existing consumer contract and error precedence require a separate integration
review; this local generic error is not a new platform refusal code.

For local checks run `go test ./internal/grantcurrentagent` within a bounded
offline sandbox. Run owning-module checks against its unchanged dependency pins.
Two independent exact-head reviews remain required before any later PR route.

Rollback before integration is to omit this isolated package from composition.
For a composed candidate, remove its connector wiring and restore unavailable
CurrentAgent reads first, then revert the package-only commit through normal
review. There is no on-disk state or migration to undo. Rollback must not revive
a retired binding, reset a source floor or weaken another evidence gate.
