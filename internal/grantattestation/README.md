# Original agent attestation read component

This package is a local bounded read core, with no installed provider. Its tests
inject session and publication behavior around an unchanged public signed vector.
Those test sources are not authenticated protected-view implementations.

`Reader.Read(ctx, contentID)` returns exact original `attestation/1` bytes and
private publication/custody provenance. It checks the requested ID, original-wire
digest, separately pinned Ed25519 purpose-5 issuer, issuer signature, closed
agent profile, six binding fields, and the positive lifetime up to 86400 seconds.
It preserves envelope whitespace and signatures. Reading an expired original is
allowed; it does not make that original eligible for current issuance or use.
There is no latest-for-key query, signing, publication or general server.

The original operation deadline is mandatory and cannot exceed an explicitly
supplied finite resource budget. One operation runs per Reader; concurrent calls
refuse without queueing. The core allocates one 8193-byte buffer, consumes at most
8193 original-wire bytes, and parses only successful bodies up to 8192 bytes at
depth 16. It makes one source open, no retries and no timeout goroutines. Every
lease is closed, including a lease returned alongside an open error. Errors
release no original bytes or provenance and do not echo source diagnostics.
There is no reservation, grant accounting or effect operation in this package.

`AuthorizedSource` and `Lease` are proposed internal Go seams, not a new wire
format. A separately admitted connector must supply all of the following before
this component can serve any real protected record:

* An actual transport/session-authenticated reader principal and exact disclosure
  policy for record type, owner, subject and issuer domain. Publishing permission
  alone does not authorize a read. Neither request strings nor test callbacks
  establish reader authority.
* An admitted exact-record publication view, including original journal,
  checkpoint and resolved witness evidence; independently retained continuity,
  restore floor and old-instance exclusion; and update/read exclusion. Local
  store commit success and provenance identifiers alone establish none of these.
  Unpublished, unresolved, fenced, conflicting or rolled-back state must refuse.
* Actual storage/transport/frame/proof/error/total-transfer bounds and the
  original operation deadline enforced inside every blocking method, including
  open, read, check and close. The core cannot cancel a source that violates this
  contract. A slow-body fixture honors context; that is not native transport
  qualification. Resource timing here does not supply trusted evidence time.

No implementation of this protected connector is supplied. Leave the consumer
binding nil while these inputs are absent. Constructor success checks wiring
only. Do not install the test source or promote it to a protected provider.

For later composition, the existing local `Store.ReadRecord("attestation/1", id)`
provides exact retained bytes and digest checking under its own mutex, but its
generic local-file read cap is broader than this component's 8192-byte bound.
A separately scoped storage integration must preflight that narrower bound
before allocation and connect the existing store to the admitted publication
view under a qualified lock order. This change does not modify the shared store
or assert that separately sampling `Store.View` proves atomic publication or
rollback safety. Reuse the existing storage and publication mechanisms; do not
create a second authority store.

Only the exact wire may later be projected into the unchanged consumer
`Attestation(contentID string) ([]byte, error)` port. Publication provenance stays
internal. The consumer's pinned verifier still checks current-use eligibility,
generation, revocation, clock and assurance at its existing final serialized
boundary. This component neither supplies nor changes those ports or r8.2.
Keymaker publication handoff belongs to a separate source slice.

Rollback: this package is unreferenced by existing runtime code and has no state
migration. Drop its isolated candidate commit to remove it. A later composition
must keep provider installation disabled until qualified; rollback then removes
that binding and returns unavailable rather than selecting another record or
discarding retained safety history.

Checks: `go test ./internal/grantattestation` with the declared module toolchain
and unchanged dependency pins. The retained fixture is the exact public `PA2`
`fixture.attestations` original from the pinned envelope module. Tests create no
keys or signatures. They cover byte-for-byte retrieval, denial and same-ID
repair, publication/rollback/final-check refusal, wrong ID and newer-binding
substitution, changed original bytes at the same signed ID, bad issuer/signature,
disclosure binding, close failure, oversized source diagnostics, 8192/8193-byte
boundaries, slow reads, original deadlines, missing bindings and concurrency.
Synthetic controls prove component behavior only. Two independent reviews of
the exact final source head remain required before source acceptance.
