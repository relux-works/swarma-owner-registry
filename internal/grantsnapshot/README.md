# Grant snapshot read-side preparation

This package verifies original `swarm-envelope/2` `revocation/1` records and
analyzes one signer's complete historical originals. It is a source library,
not a protected provider or an implementation of `Snapshot(root)`.

`Observe` accepts only the two installed origins: agent revocation and the
appointed grant publisher. It verifies the closed schema, exact content ID and
original author signature with the existing pinned codec and crypto. The
service and grant roots remain distinct protected policy pins. This package
cannot authenticate their appointment or the agent root's service scope.

Static signed observations remain available at any age, including future issue
times, refusal and repeated entries. They are not prefiltered by freshness or
publisher ordering. Invalid or over-limit records produce no observation.
Original wire and entry accessors return owned copies. No bytes are re-signed,
normalized, silently truncated or translated to a different wire profile.

`Analyze` takes a complete historical inventory for one origin. It preserves
the cumulative union and earliest effective times, detects conflicting exact
originals at equal issue times, and refuses lower nonfuture marks and clearing
or postponing old entries. Only observations not later than the supplied `now`
contribute to its proposed mark. The read's own-age calculation requires
`issued_at <= min(now, watermark)` and age at `max(now, watermark) <= 60s`.
Time comparisons retain nanoseconds; wire timestamps remain exact UTC seconds.
No TTL, local wall clock or arrival time proves currentness.

The analysis is ephemeral. It never writes a journal, creates a sidecar,
initializes an empty authority, acknowledges publication or reserves an effect.
The existing receiver must durably absorb valid signed observations on refusal,
with union entries before nonfuture marks and before any reservation/effect.
The source cannot infer continuity from that receiver's journal or a checksum.
A nil historical inventory refuses. An explicit empty slice is a hypothetical
bootstrap input, not authentication of empty history. A complete directory
restore still requires an independent surviving floor, old-instance exclusion
and explicit recovery policy. Publisher rotation remains an explicit gate.

Both the 8192-byte signed-record and 64-entry bounds apply independently.
The 2 MiB historical-input bound limits one library analysis call; it is not a
transport allowance, deployed scan policy, durable inventory size or permission
to discard older history. Capacity exhaustion refuses and requires source
custodian handling. Do not maintain only the latest snapshot to fit the bound.

## Integration prerequisites

A later composition must supply all of the following before returning an
available, rollback-safe result through the existing consumer port:

- A real protected reader with transport-authenticated identity and exact
  record/domain disclosure authorization. No request string appoints a reader.
- Original publisher appointment, fixed room, poster and receiver membership;
  agent-revocation root, service scope and authenticated source retention.
- Complete cumulative source history, independent restore floor and surviving
  custody; resolved witness/publication evidence and old-instance exclusion.
- Qualified time and publication watermark with an admitted validity interval,
  capacity policy and update-versus-final-admission serialization contract.
- Bounded transport, event wrapper, total transfer, error bodies, retries,
  concurrency and original absolute deadlines enforced by the actual connector.

These inputs are absent. A local callback, successful analysis, valid signature,
store read or publication receipt alone cannot supply them. No protected-result
constructor or successful provider callback is exposed here. Root revocations
remain a separate protected set with the receiver's existing precedence. The
package creates no assurance evidence or A2 claim.

Keep the existing consumer interfaces, store and receiver journal unchanged.
The consumer's snapshot sampling and historical observation path retain their
existing refusal precedence. One later composition must bind this library to
that exact path and the admitted protected source, without remote work under
the receiver journal lock or recursive lock acquisition. Registry freshness
receipts do not waive snapshot own-age checks. No platform dependency is upgraded.

## Local verification and rollback

Run `go test -mod=readonly ./internal/grantsnapshot` with the module's pinned Go
version and dependencies. Public signed test snapshots are copied from the
pinned envelope module's vector suite; no signing key is created or used by the
tests. Tests include signature and schema refusal, exact size/count boundaries,
historical/future observations, signer separation, cumulative clearing,
postponement, forks, rollback and nanosecond freshness boundaries. They do not
qualify crash durability, protected transport, witness custody or live readiness.

The change is additive and has no runtime registration. To roll it back before
integration, remove `internal/grantsnapshot`; no durable state or configuration
migration is involved. Any later integration needs its own reviewed rollback
and recovery plan. Two distinct independent reviews of the exact candidate
remain required before source publication under the applicable review route.
