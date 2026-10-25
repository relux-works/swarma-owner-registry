# Owner registry service design

Review draft, 2026-10-25, reconciled to merged S4 D38–D41.
The pinned public S4 record contracts are normative for this design. Content
approval, repository landing, consumer integration and activation are separate;
this text supplies no runtime or custody/transport/recovery qualification.

## 1. Scope and source contracts

One owner-administered registry on the separate trust node is the authoritative writer for record
storage, ordering and publication. It preserves original content signatures. The rollback witness shares
that node, in separate accounts, processes and streams; the trusted witness checkpoint stays off the
node. Agent accounts and the governed platform have no administrative access there.
The trust node uses a provider independent of the governed platform (R, Appendix C.3 WC1).

The service uses Go, standard library and `golang.org/x/sys`, plus the reviewed
public packages below. These public contract dependencies are not permission to
add other libraries. Repository location is decided in Q1.

Immutable inputs read for this design:

| Reference | Exact revision and relevant surface |
| --- | --- |
| F | [Shared formats](https://github.com/relux-works/waggle/blob/f76a400ebeb599b95a9bf9b2c59b522bef0392d8/spec/swarm-envelope-2.md), §§2–7: envelopes, signer contract, provenance, refusal order and consumer gates. |
| D | [D1–D41](https://github.com/relux-works/waggle/blob/f76a400ebeb599b95a9bf9b2c59b522bef0392d8/UNRESOLVED_QUESTIONS.md), especially D30 and D38–D41: separate freshness, required journal prior records, bounded freshness admission and explicit re-anchor. Design decisions are recorded in §10; the remaining composition API is a separate S5 dependency. |
| K | [Key service](https://github.com/relux-works/swarma-keymaker/blob/0ae0c4e93c3fae9db2bcadcfb5eeafbdcf0668c0/spec/keymaker.md), §§4.3, 5, 8–9: custody, pins, enrollment and purpose 5. |
| W | [Witness consumer](https://github.com/relux-works/swarma-keymaker/blob/5eb89870d25cd9c4dd9e71180d9449f18fba206f/spec/keymaker-witness.md), §§3.3–6, 8: operations, intent ordering, recovery and WC1. |
| B | [Broker](https://github.com/relux-works/swarma-credential-broker/blob/f2c01f52477b5885aefd5b6bd04d09a6a22813b2/spec/broker.md), §§6.4–6.6: local authority and the registry boundary. |
| T | [Time policy](https://github.com/relux-works/swarma-credential-broker/blob/f2c01f52477b5885aefd5b6bd04d09a6a22813b2/docs/time-policy.md) and [provider contract](https://github.com/relux-works/swarma-credential-broker/blob/f2c01f52477b5885aefd5b6bd04d09a6a22813b2/docs/time-confidence.md). |
| R | [Registrar](https://github.com/relux-works/swarma-registrar/blob/2219f70358fb7b5c7d66f229c80a8ba4a1fe0230/spec/registrar.md), §§4.2–4.5 and Appendix C.3. |
| P | Public [ccj](https://github.com/relux-works/swarma-registrar/tree/2219f70358fb7b5c7d66f229c80a8ba4a1fe0230/ccj), [enrollment](https://github.com/relux-works/swarma-registrar/tree/2219f70358fb7b5c7d66f229c80a8ba4a1fe0230/enrollment) and [registry](https://github.com/relux-works/swarma-registrar/tree/2219f70358fb7b5c7d66f229c80a8ba4a1fe0230/registry) packages. |
| WS | [Witness server proposal](https://github.com/relux-works/swarma-keymaker/blob/e7c5e1850c29d1f931de3faa24ad39d94da41ada/spec/witness-service.md), PR #20: exact server/wrapper, challenged reads, strict custodian and limits; spec only, not deployed or qualified. |
| V | S4 public [record](https://github.com/relux-works/waggle/tree/f76a400ebeb599b95a9bf9b2c59b522bef0392d8/record), [verifier](https://github.com/relux-works/waggle/tree/f76a400ebeb599b95a9bf9b2c59b522bef0392d8/verifier) and [fixtures](https://github.com/relux-works/waggle/tree/f76a400ebeb599b95a9bf9b2c59b522bef0392d8/vectors/swarm-envelope-2): pure admission, not I/O or activation. |

S4 at `f76a400ebeb599b95a9bf9b2c59b522bef0392d8` is merged and
adds freshness/1, D39 journal narrowing, D40 state/read bounds and D41 explicit
re-anchor to S3's journal/checkpoint contracts. F §§2.7, 2.9, 6–7 and D38–D41
are the public sources for the requirements below. Content approval and landing
do not activate this service or qualify consumers. The code module is pinned to
S4 `f76a400ebeb599b95a9bf9b2c59b522bef0392d8` by §11's dependency/profile
re-pin slice; new entry-point implementations and their activation stay gated.
S5 will supply the approved freshness-receipt composition path (§5.3); its API
is not part of the merged S4 surface.

Non-goals: message delivery, credential/account issuance, operator-act execution,
new verifier implementation, root recovery, contact records, registry exchange
or a registrar snapshot API. Broker PR #13 is merged in B, whose §6.5 retains
v0 local authority until separately reviewed migration. The registrar continues
to consume only R's pinned signed commit.

## 2. Trust boundary and key roles

Protected owner configuration pins keys, role scopes, owner, lineage, submitter ACLs, witness identity
and stream bindings. Neither requests nor repository metadata select roots.
Compare every present D30 role, including service subjects, commit signer and
publisher, before signed evidence at K1/O2/G7 or journal J2; unused new roles
need not be configured on inherited paths. Protected agent, device and service
collections must not overlap roles; unsigned key_class never selects a profile.

| Key role | Signs | Held where / permitted user | Rotation | Compromise permits |
| --- | --- | --- | --- | --- |
| Purpose-5 service root | Agent `attestation/1` only | External key-service custody, never registry storage; internal certification issuer only | Owner-authorized root/epoch transition, descendant reissue and retained revocations; K's activation gates apply | False agent attestations in that root scope, not grants, operator evidence or registry snapshots |
| Operator root | Device attestations and operator-domain snapshots | Owner's external protected issuer; offline/device/online custody is that issuer's gated choice, not a registry key | Explicit external pin transition preserving cumulative revocation history; D31 recovery has no v0 record | False device authority/snapshots, not purpose-5 attestations or grant-root signatures |
| Owner grant root | Grants, grant-domain snapshots and exact grant-service attestations with protected purpose/owner | Owner's external protected issuer; owner-authorized grant issuance only, custody choice stays external | Explicit pin/chain migration with old revocations retained; no automatic new-root trust | Grant-domain rights, false service identities and snapshots, not E1 or other roots' evidence |
| Grant-service subject | Parent-authorized `grant/1` only | External dedicated service signer, never the message signer; exact protected service collection | Grant-root re-attestation and cumulative key/attestation/parent revocation | Delegated rights within a current covering parent, never root, message, act or registry authority |
| Agent author key | F's agent message profile, never grants in v0 | External key-service custody, resolved active caller only; registry stores attestation/public key | New qualified binding/attestation, revoke old binding/key; no stale generation reuse | Agent provenance within surviving authority, not owner/operator rights |
| Operator device key | Published P-256 low-S act/approval and permitted delegated-grant profiles | Owner's device; qualified local signer/presence path only | Operator-root re-attestation and signed revocation; recovery remains separately gated | Device acts/delegated grants within current authority; no implicit A3, root or recovery powers |
| Registry-owner root | Registry-scoped `revocation/1`, checkpoints and closed S4 `freshness/1`, with agent scope pinned to the exact purpose-5 root | Separate signer process under its own account on the trust node (Q3); pre-approved record profiles only | Owner-approved protected pin transition, continuity preserved; new profiles/recovery require review | False scoped revocations/checkpoints/freshness and withholding, not other roots' signatures |
| Offline enrollment publisher | E1 Ed25519 `enrollment/1` only | Offline with the owner; explicit publisher operation, never service access | R's full-history re-sign and atomic pin advance; bodies/ids/tombstones retained | Forged enrollment envelopes; maintainer review and operator commit pin remain separate barriers |
| Registry maintainer | Git commits for R's allowlist | Separate online signer on trust node (Q6); export writer may request signatures, operator alone approves deployment pins | Owner changes allowlist and exact approved revision without resetting history | Forged commit signatures, not E1 content or deployment approval |
| Witness service | Its own receipt/head profiles | Separate online witness account/process on trust node; witness daemon only | Explicit owner-provisioned key epochs, old continuity and off-node checkpoints retained | False heads/receipts within its scope; co-location leaves full-node compromise shared |
| Witness consumer/bridge principal | Only its reviewed stream authentication/record profile, never content | Separate protected witness-side account; no credential available to registry writer | Owner rebinds credentials without stream reset; exact-candidate recovery gates persist | That consumer stream's claims, not other consumers' streams or content roots |
| Off-node checkpoint custodian | WS independently authenticated checkpoint confirmations/fresh reads; exact provider profile gated | Separate off-node administration, never registry/witness account access | Explicit owner-approved continuity-preserving policy; no reset from host backup | False latestness/rollback claims within its retained coverage; combined loss with all surviving evidence is outside the guarantee |
| Transport principals | TLS authentication only | Dedicated publisher/client or server process; private client keys never stored as records | Protected ACL/certificate update with explicit revocation and no root discovery | Allowed channel use, not content or witness authority |

Maintainer and registry-owner keys differ from each other and from every content
root, including E1. External issuer custody is not decided here: if an issuer
cannot provide fresh required signatures, its domain stays unavailable.

Recommend an independently generated Ed25519 registry-owner key; no assigned key-service purpose or
derivation is implied. Q3 is decided by the owner, 2026-10-24: the registry-owner
key is held by a separate signer process under its own account on the trust node.
This isolated closed-profile signer accepts only pre-approved record profiles,
not arbitrary preimages. No registry access to message/SSH signing is needed.
Process/account separation narrows accidental exposure; full-node compromise
still reaches the signer. No online delegation format is invented.

The signer requires private directories, owner/mode and no-link checks, no secret
logging/argv/environment/core dumps, restricted backups and qualified swap policy. Memory locking reports
its actual result; managed-runtime clearing does not prove non-extraction. Public pins and rotation
approval stay external.

The maintainer key signs enrollment-export commits. R still requires review, an out-of-band maintainer
allowlist and operator approval of that exact protected-branch revision. A service-signed commit alone is
not review or pin approval. Registrar and helper install matching revision, audience and pins. Git
signature verification and operator deployment approval are separate gates.

A full node compromise can alter availability, equivocate, steal online keys, sign false registry
revocations/checkpoints and compromise the co-located witness. Distinct uncompromised external roots
prevent forging their attestations, grants and E1 signatures. This does not prevent withholding a valid
record or lying about currentness; D30 alone gives no honest-publication or latestness proof. The
off-node checkpoint detects rollback only within its retained coverage.

## 3. Accept API and verification boundary

Propose a bounded authenticated submission operation, not a finalized HTTP schema: `Accept(expected_head,
request_id, record_type, exact_record_bytes, evidence)`. Protected configuration maps mutually
authenticated TLS client certificates to owner-controlled publisher identities and allowed types/scopes.
Administration uses a restricted local socket (Q4, decided 2026-10-24, content review).
Provisioning and rotation are explicit; no self-enrollment or bearer secret in URLs. An authorized
channel may carry only content its original signer can authorize. Read principals have separate ACLs;
public distribution requires an explicit metadata decision. Record types, key classes and signing
profiles are closed per format version; a new class requires a reviewed new format version, never a
configuration toggle.

| Intake class | Authority and v0 treatment |
| --- | --- |
| Agent/device/service `attestation/1` | Exact purpose-5/operator/grant-owner profiles respectively; service admission is specified below, never self-attestation or direct device-pin replacement. |
| General `grant/1` | Owner grant-root provenance or current parent-authorized device/service delegation; no agent issuers or broker-v0 domain relabeling. |
| Shared `revocation/1` | Exact issuer role/scope; complete cumulative snapshots. Registry may construct only its own authorized issuer scope. Other issuers retain their signatures. |
| `enrollment/1` | E1 signature and a complete prepared, reviewed signed enrollment projection; approval precedes acceptance of that projection. Never online self-enrollment. |
| Journal/checkpoint | Internally construct S3 records; no caller-selected history or sequence allocation. |
| `freshness/1` | Internal registry-owner-only S4 publication (§5.1); implementation waits for the explicit re-pin/qualification gates, no external intake or configurable new class. |
| `operator-act/1`, `message/1`, contact or unknown type | No standalone intake/effect profile here; refuse. A future signed authorization attachment needs its consumer-owned mapping. |

Before accepting new bytes, apply the following ordered gates. API-level names
below are proposed service diagnostics, not additions to F's refusal inventory.

1. Enforce TLS principal, scope, deadline, concurrency and byte budget before
   expensive decoding: `submitter_denied`, then `request_too_large`/`request_invalid`.
2. Check supported intake type and required service evidence:
   `record_type_unsupported`, then `verification_unavailable` if the pinned adapter/evidence is absent.
3. Run the appropriate published verifier. F's E1–E7, T1–T2, K1, E8 order,
   grant G1–G9 suborder and any future act profile keep their specified precedence.
   Return its typed code; never replace an invalid signature with a later conflict.
4. For enrollment, use P's `enrollment.Policy` and `registry.Loader` with the
   complete approved projection; preserve R's seven snapshot-wide phases. Its
   `Store.Commit` adapter enters §6's witnessed transaction only after validation,
   never writes before intent, and returns only after resolution. `Load` is not
   a pure dry run: no early or throwaway high-water installation is permitted.
   For this path that adapter performs stages 5–6 below before `Load` returns.
5. Under the sole writer's serialization, revalidate current evidence/time, then
   reject changed retained request arguments (`request_conflict`), then compare
   expected head (`head_conflict`), then capacity (`capacity_exhausted`), before
   reserving a sequence. This outer request journal is distinct from F's E8.
6. Commit the exact candidate through §4/§6. An uncertain durable outcome fences
   the service as `state_unavailable`; it is not a clean rejection or success.

Use V's actual `record.Parse`, `ContentID`, `SigningPreimage` and strict
signature math; raw math never admits a role. Construct `verifier.Context`
with checked immutable `Present` values, `Now`, trusted time and durable
watermark, exact content-id attestation lookup, current active generations,
issuer snapshots and id bindings. Explicitly pass `ServiceAuthority`,
`ServiceAttestations`, `CommitSigningKey`, `EnrollmentPublisher`, and protected
agent/device collections with all present roots. Call `VerifyAttestation`,
`VerifyServiceAttestation`, `CheckRevocation`, `Authorize` or `VerifyJournal`
for the corresponding profile; retain typed codes and empty refusal plans.
For S4 registry-domain currentness use the separate `VerifyFreshness` or explicit
`VerifyFreshnessReanchor` with §5.3's retained state/read facts; these do not
compose into the legacy APIs before S5. Intake consumes three issuer snapshots:
the registry's own, the operator root's and the grant root's; each retains the
legacy maximum age of 60 seconds. Under B an idle own snapshot exceeds that age,
so registry-domain attestations and every intake check needing it refuse until
the pending S5 composition in §5.3 is implemented and qualified. Grant/1, D33
service keys and device attestations also require their relevant external issuer
snapshots to stay current; a registry receipt cannot extend them (D32, §10 D42).
No function commits, executes effects, fetches or authenticates witness wire.
Revalidate at the serialized commit/publication boundary. Use P's CCJ-1, never
a second codec/verifier. Current-generation truth needs qualified external
evidence; K's purpose-5 preimage is aligned at the specification level (§5.5),
while runtime qualification remains a separate activation gate.

D33 service intake requires Ed25519, key_class=service, purpose=grant-service,
roles=[author], actions=[grant.sign], exact bounded owner binding, positive
validity <=86,400 seconds, and the pinned grant-owner issuer with explicit
protected grant-service purpose/owner. Verify its exact key-indexed attestation
and grant-owner revocation snapshot. Service key/attestation and every chain
issuer, subject and grant id remain subject to that revocation domain. A link
requires a covering parent addressed to the service with depth >=1 and all
narrowing/time checks. Attestation grants no parent or self-root authority;
agents remain forbidden issuers. Unknown classes stay closed per version.

Allocate one global positive safe-integer journal sequence per entry, including the head-change snapshot
reissue, exactly previous +1;
host enrollment sequences remain separate and may have gaps. One writer and one unresolved transition are
permitted. No multiwriter merge, sequence reset/wrap, or local lock as proof of old-instance exclusion.

Retain `(principal, request_id) -> exact operation, bytes, outcome` durably. Changed arguments under that
key refuse `request_conflict`; exact retries with current disclosure authorization return the retained
outcome without new entries. They do not renew authority or manufacture fresh acceptance of expired
evidence. For new requests, F's id binding distinguishes exact-byte retransmission from same id/different
bytes (`envelope_conflict`), including changed whitespace or signature order. Keep the earlier evidence;
never overwrite it to make ids agree. No request-id reclamation without a reviewed permanent-rejection
horizon. Retained-outcome lookup is separate from new-byte acceptance; it cannot execute an act or
release a new grant, signature or credential.

Separate budgets: ordinary intake permits one F envelope <=65,536 bytes;
attestations/revocations/acts and journal/checkpoint records <=8,192 bytes,
depth16, strings <=8,192, arrays <=64, signatures <=8; grant chain <=4 and
262,144 aggregate bytes. Keep the proposed 512 KiB total intake/evidence cap,
16 in-flight submissions and five-second monotonic shared budget. Enrollment
uses bounded pre-staged evidence, R's 65,536-byte files and 10,000-envelope/
16 MiB candidate cap; human review precedes the operation budget.

Publication-proof retrieval has a separate explicit **2 MiB aggregate received
bundle cap**, including all context records, witness/control evidence and
transport encoding. Keep the two independent pure-proof bounds side by side:

| Proof route | Per-call raw bound before context/carrier overhead |
| --- | --- |
| J: journal | <=64 entries + <=2 checkpoints, <=8,192 bytes each, depth16, <=540,672 raw bytes; the advancing anchor uses one entry place |
| F: freshness | <=64 publications + 1 checkpoint, <=8,192 bytes each, depth16, <=532,480 raw bytes; <=64 queried ids AND <=64 queried keys |

F's 532,480 = 65 × 8,192 bytes is 520 KiB, already 8 KiB over the
512 KiB intake route before its context snapshot or encoding. Route complete
publication proofs through the separate 2 MiB received-bundle route; do not
raise intake limits or truncate/split away authenticated completeness. J's
protected prior checkpoint, current snapshot, witness originals/control proof
and every encoded carrier also count in that outer route. Precharge all
per-record, pure-call, received-bundle and carrier bounds independently;
over-cap is `proof_limit` at the service, never a claim of reconciliation.
The library retains its own ordered J/F refusal codes (§§5.3, 6.1).

WS additionally requires the whole checkpoint wrapper including LF <=8,192
bytes, frames <=65,536, <=64 proof pages/128 entries, and one shared remaining
deadline. Many maximal J/F proofs do not fit that wrapper: preflight the full
encoded real-head proof, including D39 prior bytes, before intent; refuse unless
it fits or await a separately reviewed bounded adapter/transport. A 2 MiB route
is not a promise that all maxima fit WS. Idle freshness records do not become
witness append payloads; no ad hoc pagination.

Refusals leave records, sequences, pins and replay bindings unchanged unless an irreversible intent has
already occurred; that case is fenced and recoverable, never described as no effect. Bounded separate
refusal telemetry may change. Remote errors omit submitted payloads, filesystem details and signer
secrets.

## 4. Storage, signing and atomic publication

Proposed Git layout separates shared-record storage from R's existing input:

| Path | Meaning |
| --- | --- |
| `records/<record_type>/<content-id>.json` | Exact signed content bytes; closed validated type-to-path mapping, never an arbitrary caller path. |
| `journal/<seq>.json` | Exact unsigned S3 entries (§6). |
| `checkpoints/<seq>.json` | Exact signed S3 checkpoints, one per real journal entry. |
| Freshness history/store | Separate append-only durable exact S4 publications, outcomes and current pointer; local layout only, no new wire schema. |
| `enrollments/*.json` | One E1 envelope per file, all retained host history, exactly as R §4.4 reads it. |

The signed enrollment projection is a separate export ref, built by the same writer and externally
reviewed before import. It can contain only enrollment history; the shared journal need not wait for
review of an unrelated shared-record change. Registry history retains the exact approved Git object, and consumers pin that
object, never the general journal ref. New content cannot silently advance
deployment pins. Use 16-digit zero-padded decimal sequences, covering every positive safe
integer through 9007199254740991 without wrap (Q5); Git filenames/fanout are
our storage choice, not S3 wire rules. Filenames carry no enrollment authority.
Enrollment bytes are also stored under `records/`; the mirror must match exactly,
not be reserialized or re-signed. Other record types never enter `enrollments/`. No symlinks, special
files or caller-selected traversal. Reject rather than skip an invalid enrollment entry.

Shared-record authority is each original signature plus validated chain and
currentness; the S3 signed checkpoint authenticates ordering/references only.
Git preserves and moves bytes; Git commits alone do not authorize shared
grants or attestations. **Phase-0 exception:** R explicitly makes the approved pinned signed commit the
enrollment authority, with E1 as its additional integrity check. General Git
transport rules cannot replace that contract. Distinct maintainer signing plus
the enrollment mirror preserve
both models without changing R.

Content bytes, retained body/id history, ordering, tombstones, durable outcomes and witnessed continuity
are authoritative state. Current-record indexes, search results and caches are derived. They may be
rebuilt only from reconciled history; a missing authoritative file is never an empty result. R's approved
E1 rotation may replace envelope signatures while preserving body/id/sequence history; retain the
previous exact envelope revision in Git, not an id-conflict overwrite.

On every real head change, re-issue the registry's `revocation/1` with the
identical current cumulative set and strictly greater `issued_at`. Journal that
snapshot with `op=add`, `record_type=revocation/1` in the same transaction;
`checkpoint/1.snapshot` references the reissued snapshot id. This is part of the
candidate, not a separate intake request. The rule continues under S4;
F §2.6 supplies cumulative/unique issuance requirements, §2.7 supplies the
journal proof. The full three-entry control is
[P-JRN-publish-reissue](https://github.com/relux-works/waggle/blob/f76a400ebeb599b95a9bf9b2c59b522bef0392d8/vectors/swarm-envelope-2/1-positive/P-JRN-publish-reissue.json),
not the older two-entry P-JRN-reissue. Its six negatives and typed refusals
are mapped in §11. The transaction includes the exact prior checkpoint
through protected IDJournal (§§5.5, 6.1), not just the three entries and candidate.

Prepare an immutable complete candidate and signed commit privately, with a recoverable transaction
manifest outside public record schemas. That manifest binds base/candidate Git ids, all exact bytes,
outcomes, watermark and next head. It is local recovery state, not an invented identity-journal wire
format. No response, branch advertisement or snapshot references an unresolved candidate.

Use checked full writes, file sync, checked close, atomic rename/ref comparison
and directory sync; include Git objects, commit, refs and transaction evidence.
Do not assume `git commit` or rename alone guarantees power-loss durability.
The published ref is the last visibility step after witnessed resolution (§6);
external pins still need review/approval. Git/storage adapters must be qualified
on the chosen filesystem. Disable hooks, filters, ambient config and credential
helpers; never execute a repository-supplied program. Q6 (decided 2026-10-24,
content review) selects a protected, bounded system-Git adapter under the existing
dependency policy and a separate maintainer signer; operator pin approval stays external.

For real head changes, serialization couples record bytes, journal entry,
checkpoint, id binding, request outcome and watermark. Readers see one resolved
state. Failure at any
boundary stops publication and new acceptance; an indeterminate commit never
restores an older ref as if the candidate had not happened. No history rewriting
or garbage collection may discard acknowledged evidence or pending candidates.

## 5. Snapshots, freshness and consumers

### 5.1 Immutable base and S4 currentness

F's closed `revocation/1` is one issuer's signed complete cumulative set with
issued_at and effective entries. Preserve exact base bytes/id until a real
head change; registry signatures never replace operator/grant-domain signatures.
The global journal orders references, not each domain's cumulative truth.
Validate original issuer authority, all retained/future-effective entries and
continuity; never remove entries or move from later. Neither revocation/1 nor
checkpoint/1 gains a local snapshot-prev or publication-sequence field.

The merged S4 profile (F §2.9, D38) is a four-member
`{version,body,id,signatures}` envelope, version `swarm-envelope/2`, exact label
`swarma-registry freshness/1`, normal content-id/signing construction and exactly
one `author` signature by the protected registry-owner key. Its closed body is
`{type,registry,owner,publication_seq,prev,issued_at,snapshot,registry_seq,checkpoint_id}`.
Type is `freshness/1`; registry/owner are the exact protected root/owner.
Snapshot is the unchanged revocation/1 id, equal to checkpoint.snapshot;
registry_seq/checkpoint_id equal the current checkpoint's positive safe seq/id.
No other root, offline publisher or commit signer supplies freshness.

`publication_seq` is a positive safe integer, globally exactly previous+1,
including every head advance. `prev` names the previous freshness id for the
same head; it is null on the first publication after a head advance or at
publication genesis (publication_seq=1). Same-head continuations require that non-null
predecessor. `issued_at` strictly increases globally and never resets at a head
change. An exact retransmission preserves the accepted position and timestamp;
it is not a second publication or a renewal. The first publication has no
predecessor time, so the exact minimum timestamp `1970-01-01T00:00:00Z` is valid;
subsequent publications must advance beyond an actual minimum-time predecessor.

Only real head changes append journal entries/create checkpoints and use witness
intent/resolution plus custodian barriers. Every real head change includes §4's
snapshot reissue with the identical current cumulative set, strictly greater
issued_at, same-transaction add/revocation/1 entry and checkpoint snapshot
reference. A changed cumulative set completes that real transaction first.
Its next freshness resets prev to null while retaining global sequence/time.
Unchanged ticks preserve snapshot bytes/id and registry/witness positions;
there is no idle fallback to snapshot reissue.

Publish freshness every 20 seconds and promptly after a real head change:
4,320 scheduled freshness publications/day, plus immediate head-change
publications, with no unchanged-head journal/witness append. Twenty seconds
leaves 40 seconds nominal headroom under 60 seconds, including one missed
schedule before polling, transport and conservative uncertainty costs; this is
not measured availability. External issuer domains retain their own qualified
currentness and original signatures.

`VerifyFreshness` admits an aged unchanged snapshot and checkpoint only through
the full signed chain, exact three-way binding and fresh authenticated head read.
This does not relax the legacy methods' own-issued_at/checkpoint-age rules.
The registry publishes and also consumes its own snapshot in intake. An idle
base older than 60 seconds therefore needs pending S5 receipt composition for
registry-domain intake, as do Keymaker/broker old-base consumers (§§3, 5.3,
5.5–5.6); no younger Context or duplicate verifier supplies that authority.

### 5.2 Freshness durability, complete history and restore

Retain the registry's publication position as the exact S4 `FreshnessAnchor`
projection `{registry_id,owner,publication_seq,freshness_id,issued_at,snapshot,registry_seq,checkpoint_id}`,
with all sequences typed integers, positive registry_seq and scoped root/owner.
Explicit externally authorized publication genesis alone uses publication_seq=0,
freshness_id=null and issued_at=null while still pinning an authenticated
positive checkpoint and exact snapshot. It is not a seq0 witness, missing-state
bootstrap or permission to restart a restored lineage. Positive anchors retain
a real publication id/time and exact snapshot/checkpoint references.

`WitnessReadAt` is the protected timestamp of the exact authenticated
registry/head/checkpoint read, retained with its original evidence. It is not
checkpoint_time, a received self-assertion or a new JournalWitness member.
Retain the effective-time watermark as authoritative state too. Publication
bytes, anchor, exact snapshot/checkpoint, authenticated read evidence, watermark
and outcome become durable atomically before visibility/use; a normal F success
plan names all supplied publications, then checkpoint and snapshot ids, and the
monotonic watermark. Refusal, including revoked, returns no plan and advances
no anchor, counter, watermark or authority. A library plan itself writes nothing.

Separate the freshness transaction from real-head witnessed transactions.
Under the same publication serialization, revalidate the resolved head and
absence of intent, original domain authority and time; prepare exact freshness
bytes and a local recoverable manifest binding prior publication, current base,
head, next freshness position, outcome and watermark. Sync append-only history,
exact outcomes and candidate pointer; atomic pointer comparison/rename and
checked directory sync precede visibility. Readers see one durable complete
position; retries return exact retained bytes/outcome, never a second position.
No partial publication or late success; unknown durability fences publication.
A head race retries verification against the actual resolved head; freshness
cannot extend a superseded base or race a committed revocation.

Retain all freshness history, predecessor evidence and consumer high-water
outside disposable caches. Before use, require a qualified complete-history
check to an independently retained published freshness head. Off-node backups,
consumer-held last-seen positions and authenticated head comparison supply
surviving evidence; a restored local counter alone supplies none. Q12 (decided
2026-10-24, content review) requires complete chained freshness history and
off-node heads retained before use. Publisher/consumer integration must qualify that mechanism
and independent retention before B activation, without adding unchanged-head
witness/custodian writes. Missing coverage or an unverifiable gap fences; do not claim this mechanism exists.
Startup/restore reconciles both real registry/witness lineage and the independent
freshness floor/history, plus time continuity and old-instance exclusion.
Missing newer bytes or ambiguous visibility stays fenced; never initialize
freshness from restored bytes or reset its counter to match the old journal.

### 5.3 Consumer gates, bounded catch-up and composition

After the planned re-pin, call V's `VerifyFreshness(chain,checkpoint,ids,keys)`
with oldest-first exact raw publication bytes, exact raw current checkpoint,
protected `FreshnessAnchor`, `WitnessReadAt`, `SignedSnapshots` and the existing
six-field authenticated `JournalWitness`. Consumers pin registry root/owner/scope
out of band. Keep all original issuer, generation, grant and effect checks.
F §§2.9, 6 specify the F1–F9 refusal order; no service reordering is allowed:

| Stage | Required check and refusal boundary |
| --- | --- |
| F1–F3 | Missing proof, bounded closed wire, protected roles, every publication signature then checkpoint; freshness_unavailable / freshness_malformed / freshness_invalid / freshness_signature_invalid / checkpoint_invalid |
| F4–F7 | Full signed cumulative snapshot and trusted time/watermark; authenticated witness; exact snapshot/checkpoint/tip binding; current witnessed seq/head/id. Invalid snapshot/witness/binding refuse freshness_snapshot_invalid / freshness_witness_invalid / freshness_binding_mismatch; unavailable, rollback and fork remain distinct. |
| F8 | Tip and read currency before retained anchor validation, then global sequence, same-head predecessor/null-on-advance, strict time, head/snapshot continuity; freshness_unavailable / freshness_state_invalid / freshness_rollback / freshness_fork / freshness_gap / freshness_time_invalid / freshness_binding_mismatch |
| F9 | <=64 valid ids and <=64 valid keys, then effective matches only after the complete proof; freshness_request_invalid or revoked |

Let low=min(now,W) and high=max(now,W), with positive protected max_age<=60s.
Require snapshot.issued_at<=checkpoint.at<=tip.issued_at<=low and
high-tip.issued_at<=max_age. `WitnessReadAt` must belong to the exact freshly
authenticated projection, be >=tip.issued_at and <=low, and satisfy
high-WitnessReadAt<=max_age. Missing, stale, future or pre-tip reads refuse;
an unrelated fresh clock sample cannot replace that read. Refresh it at
admission/effect boundaries within the existing shared operation budget.
The library's inclusive age ceiling does not add grace to the consumer's
exclusive authority window or conservative uncertainty margin.

Persist exact admitted proof/checkpoint/snapshot bytes, final anchor and watermark
atomically before use. Same publication seq with different content/binding,
or same registry seq with another head/checkpoint, fences and signals. Freshness
never revives expired grants/attestations, clears revocations, proves completion
or authorizes restart. Archives mean verified at intake, not historical authority.

Without re-anchor the 64-publication bound covers at most 64 new intervals:
1,280 seconds, 21 min 20 s at a 20 s cadence, less if the retained anchor itself
uses a slot. This is a count limit, not a reconnect SLA. Every call's tip must
still be current and match the freshly witnessed head. Stale prefix tips cannot
be admitted in successive batches; a head change beyond the first 64 positions
may prevent catching up even earlier. Carrier/byte/time margins narrow the
bound further. Over-bound or missing history refuses on the normal path.

#### Explicit re-anchor

The separate merged `VerifyFreshnessReanchor(tip,checkpoint,ids,keys)` accepts
one raw signed current tip and one checkpoint. Its explicit call is the
recovery trigger; normal `VerifyFreshness` never falls back on refusal or treats
an invalid supplied chain as complete. Publication genesis stays on the normal
path. Consumers may choose this separate call in automatic long-outage recovery.

F1–F7, tip/read currency and F9 query/revocation matching stay unchanged.
F8R requires a retained positive publication with real id/time, not a genesis
anchor. Current registry_seq below retained refuses freshness_rollback;
equality requires the same checkpoint id (thus the same committed head), else
freshness_fork, and identical snapshot, else freshness_binding_mismatch.
An advanced registry_seq requires a different checkpoint id. Then require
strictly greater publication_seq, followed by strictly greater issued_at;
either non-advance refuses freshness_rollback. Witness unavailability closes
both normal and recovery admission.

Successful recovery adds `Plan.Reanchor` / `FreshnessReanchorPlan`, serialized
as `reanchor` with exactly
`{reanchored,skipped_publications,old_freshness_id,new_freshness_id,old_checkpoint_id,new_checkpoint_id}`.
Reanchored is true; skipped_publications is tip.publication_seq minus retained
publication_seq minus 1, including zero for explicitly adjacent recovery.
The ids bind both endpoint publications and checkpoint-bound heads. Normal
plans omit reanchor entirely; revoked and all refusals return a zero plan.
Atomically persist exact admitted bytes, new anchor, watermark and the skip
record with both endpoint ids before authority use. The registry retains full,
untruncated freshness history; consumers audit both endpoint heads against it
and independent witness/off-node evidence. No skipped signature/link or fork
consistency is claimed verified. Loss of fork detection inside the skipped span
is the accepted D41 recovery cost, not a reason to reset history or reopen it.

`rollback_safe` comes only from independently qualified cumulative publication
and rollback evidence. Never infer it from re-anchor comparisons, complete=true,
a higher registry_seq or a fresh signature. Retain all previously known key,
record and future-effective revocations without later from times; unseen
intermediate additions later omitted also require qualified history coverage.
Without that evidence stay unavailable, never widen authority on unknown history.

#### S5 receipt dependency

The legacy `VerifyAttestation`, `VerifyMessage`, `Authorize`, `VerifyOperator`,
`CheckRevocation` and `VerifyJournal` keep their own snapshot/checkpoint-age
checks. S4 freshness success is not a receipt those methods accept. Keep the
public `TestFreshnessCompositionBoundary` refusal and current-snapshot positive;
no weaker Context, fabricated younger bytes/time or second verifier composes it.

The approved next path is S5's freshness receipt, before Keymaker K3 activation
and the broker D12 adapter. Only `VerifyFreshness` or `VerifyFreshnessReanchor`
may issue it, bound to the exact snapshot, checkpoint, freshness and registry
root ids. Expiry is the exclusive bound
`min(tip.issued_at + max_age, WitnessReadAt + max_age)`, with no extension.
It exists only in process memory; audit records its ids, never the receipt.
Restart/persistence cannot restore receipt authority. S5's API/representation
and supported consumer composition still require their own implementation and
review; no receipt API fields or legacy age waiver are defined here.

For the registry's own intake, the planned S5 path verifies its own freshness
chain with `VerifyFreshness` and an authenticated witness read, holds one
`SignedSnapshots` Evidence for the current snapshot, and passes the receipt in
the intake Context. Keep that Evidence instance for the receipt's lifetime;
a rebuilt Evidence cannot reuse its receipt. After a real head change, rebuild
the Evidence for the reissued fresh snapshot and obtain the appropriate new
receipt before relying on aged-base composition. After restart no receipt exists
until `VerifyFreshness` runs again; witness unavailability refuses intake.

D32 separation limits the default S5 scope to the registry-owner domain.
Operator-root and grant-root snapshots still need their own age at most 60 seconds;
a valid registry receipt cannot extend them. Grant/1, D33 service-key and device
intake activation therefore also waits for the external-root currentness decision
D42 (§10), owned by the owner and the content review. No extension profile is assumed here.

A fresh witness read at an unchanged head cannot discover a withheld newer
freshness tail. Covered signed predecessor history, retained heads and
independent comparisons expose only covered gaps/forks. Co-compromise/partition
can hide views until evidence meets; tip age is bounded, latest publication
position and pre-publication availability are not proved. Distribute immutable
objects/current pointers over authenticated HTTPS; hints confer no authority.
Until qualified migration consumers keep existing authority and restore fences.

Distribution assumes qualified cumulative non-equivocating publication. Its
bound starts at first-containing issuance S, not submission: max(S+A,from).
D29 completion still needs acknowledgements with explicit consumer sets/durable
barriers, or proven expiry of all prior authority. Critical effects perform a
final online check with their authority owner. Intake acknowledgement is not
completion; emergency stop stays independent and restart needs current authority.

### 5.4 Capacity

Decided, 2026-10-24 (D36, content review; Q8): future pages under one complete
signed head. Page/head wire rules and completeness remain a later reviewed
slice; no paging implementation or silent domain repartition is authorized.
Until then preflight both <=64 entries AND <=8,192 whole signed bytes; even
64 entries may exceed the byte cap. Signal approaching capacity; refuse
`capacity_exhausted`, retain the last durable complete set and all entries,
fence affected publication/admission and signal blocked domain/head. Never
refresh the rejected pre-addition set as current, truncate or acknowledge a
lost change. Stale last-good evidence becomes unavailable normally. Merkle and
narrower-domain alternatives are deferred, not locally selectable policy.

### 5.5 Keymaker consumer and proof of publication

Keymaker submits original purpose-5 `attestation/1` through intake. The purpose-5
signing preimage is aligned at the specification level: [K's message profile,
lines 417–423](https://github.com/relux-works/swarma-keymaker/blob/0ae0c4e93c3fae9db2bcadcfb5eeafbdcf0668c0/spec/keymaker-msg.md#L417-L423)
against [F's signing construction, §2.2](https://github.com/relux-works/waggle/blob/f76a400ebeb599b95a9bf9b2c59b522bef0392d8/spec/swarm-envelope-2.md).
Runtime qualification remains a gate. It consumes activity
status and its domain's original revocation/1 plus checkpoint head and freshness.
Activity needs independently qualified current-generation evidence, not an old
attestation or registry cache assertion. Publishing identity is not minting grants.

Return, or ensure the consumer already durably holds, this complete bundle:

1. Exact original signed record bytes/content id and required issuer/authority evidence.
2. Exact unsigned E_n binding seq, op, record_type and that record id.
3. Exact signed C_(n+1) plus E_(n+1) linking E_n and journaling the reissued revocation/1.
4. Complete oldest-first suffix from a previously independently trusted projection,
   including its authenticated raw anchor, within the cap.
5. Exact signed registry-owner snapshot whose id equals checkpoint.snapshot.
6. Independently verified exact witness intent/resolution originals and both receipts,
   plus fresh challenged head/continuity proving that exact checkpoint is resolved.

Under the reissue rule, the anchor is E_(n-1); E_n is the entry for the published
record and E_(n+1) the reissued revocation/1, in one transaction. C_(n+1) covers
that head. Supply `entries=[E_(n-1), E_n, E_(n+1)]`, candidate C_(n+1),
conflict=nil, `IDJournalAvailable=true` and
`IDJournal[witness.checkpoint_id]=exact C_(n-1) bytes`. The pre-transition
witness names C_(n-1): registry_seq=n-1, registry_head=id(E_(n-1)),
checkpoint_id=id(C_(n-1)), with independently verified ancestry/predecessor.
Entry times are nondecreasing from the anchor; C_(n+1).at>=C_(n-1).at.
The prior checkpoint may be old; the candidate/reissued snapshot retain current
age checks. This uses three entry slots and one candidate checkpoint within
J's bounds; the protected prior bytes are additional context, not conflict
checkpoint evidence. Precharge them in the complete bundle and WS wrapper.
The exact control is
[P-JRN-publish-reissue at S4](https://github.com/relux-works/waggle/blob/f76a400ebeb599b95a9bf9b2c59b522bef0392d8/vectors/swarm-envelope-2/1-positive/P-JRN-publish-reissue.json). checkpoint.head is the entry id,
never the original record id. With an anchor at most63 new entries fit.
A second checkpoint is fully admitted current
fork evidence against the same snapshot context, never the previous/next slot.
Compare returned seq/id/type/op to authenticated E_n and independently verify
the original content profile. `VerifyJournal` returns only checkpoint id and
watermark: it neither performs membership comparisons nor stores receipts,
authenticates witness signatures or proves publication. A candidate ahead of a
prior witness fact still needs separate fresh evidence of matching resolution.
Only after that resolution and final checks may intake return publication success.

At publication time the consumer verifies and atomically persists the exact
verified checkpoint, original bundle, verified result, watermark and continuity
anchors before use. A stored proof cannot be re-verified arbitrarily later with
S3 high-level functions: both checkpoint and its exact snapshot require <=60 s
currentness. Stored means verified at intake, not historical re-verification;
the latter remains outside v0, with no historical verifier designed here. Archived signatures/hashes are not historical authority or witness
truth. Do not lower time or chain calls over expired intermediate checkpoints;
obtain current proof within bounds or await a separately qualified historical
path. Under B, S4's full registry freshness pipeline admits an aged unchanged base
with a current tip/read; it does not make those legacy methods accept it.
Keymaker therefore has a pending S5 receipt-composition dependency before K3
activation. Retained success is not renewed authority or revocation completion.

### 5.6 Broker consumer composition

B §6.5 retains broker v0 local authority/restore fences until reviewed migration.
The broker D12 adapter depends on the pending S5 receipt path in §5.3, separate
from S4 landing and this registry's publication implementation. It must combine
that bounded current registry evidence with all original grant/operator/domain,
assurance and critical-effect checks through a supported reviewed composition.
No registry receipt changes other issuers' signatures/currentness, substitutes
for a parent grant or weakens emergency-stop/restart policy. Neither S4 format
acceptance nor this design claims that adapter is implemented or activated.

## 6. S4 identity journal and witness adapter

### 6.1 Exact records and refusal order

An entry is exactly `{version,body,id}`, version=`swarm-envelope/2`, with no
signatures. Its body is exactly `{type,registry,owner,seq,prev,at,op,record_type,record}`:
type journal-entry/1; registry the protected registry-owner key id; owner exact
1–255 UTF-8 bytes; seq integer 1–9007199254740991; prev null iff seq=1,
otherwise preceding entry id; at exact `YYYY-MM-DDTHH:MM:SSZ`, real Gregorian
years 1970–9999, no offsets, fractions or leap seconds, nondecreasing; op exactly add/revoke; record_type exactly
attestation/1, grant/1, revocation/1 or enrollment/1; record original signed
content id (`sha256:` plus 64 lowercase hex digits). Hash `swarma-registry journal-entry/1` + LF + CCJ-1(body) for id.
Journal admission authenticates references only; it never admits enrollment contents.

A checkpoint is exactly `{version,body,id,signatures}` with the same version;
body exactly `{type,registry,owner,seq,head,at,snapshot}`, type checkpoint/1,
registry/owner/seq/at as above, head entry id at seq and snapshot the exact
current registry-owner revocation/1 id, not all issuer domains aggregated.
Use exact tag `swarma-registry checkpoint/1`, F's content-id/signing construction
and exactly one author signature by body.registry, with no issuer alias.
Entry time <=checkpoint.at; snapshot.issued_at <=checkpoint.at. Retain and
check previous checkpoint time separately to keep it nondecreasing; the anchor
only supplies prior ENTRY time. Each whole record <=8,192 bytes/depth16,
with shared string/array limits. At most64 entries, two checkpoints and540,672
decoded received bytes per call, independently of total history length.

| Step | Ordered check | Refusal |
| --- | --- | --- |
| J1 | Count/total/record bounds, duplicate-free depth16 parse, closed shapes/encoding | journal_malformed |
| J2 | Protected owner/root purpose/scope and all present D30/commit/publisher roles | journal_unavailable / journal_invalid |
| J3 | Candidate then optional checkpoint: pin/id/author signature, typed body/time, full current snapshot, exact id and time relation | checkpoint_invalid / time_unavailable / watermark_unavailable / journal_unavailable / checkpoint_snapshot_invalid |
| J4 | Already verified witness fact available, exactly typed and scoped | journal_unavailable / journal_witness_invalid |
| J5 | Same seq, different head OR checkpoint id fork, then rollback | journal_fork / journal_rollback |
| J5a | Advancing only: IDJournalAvailable=true and exact prior checkpoint under witness.checkpoint_id; authenticate id/root/scope/owner/seq/head without age, then candidate.at>=prior.at | journal_unavailable / checkpoint_invalid / journal_rollback |
| J6 | Advancing first entry must be the witnessed seq/id anchor before per-entry validation; then entry id/scope, seq/prev/nondecreasing time, final head/seq | journal_anchor_required / journal_entry_invalid / journal_chain_broken |

These rows are V's separate journal entry point, not prechecks on grants/acts.
Every refusal has an empty plan. Authenticate optional fork evidence fully;
corrupt unrelated bytes are not a fork. An exact witnessed seq/head/checkpoint
retransmission alone permits zero entries. **Require the authenticated prior
head entry as anchor on every advancing suffix**, checking its id/seq/root/owner
against protected retained evidence. Advance exactly +1, matching prev and
nondecreasing time through checkpoint head. An omitted anchor could hide a first
new entry older than the prior entry. No seq0 or missing-state bootstrap;
genesis requires independent protected authorization and ancestry validation.
D39 makes that anchor mandatory and requires the distinct exact prior checkpoint
through IDJournal before it; the six-field fact supplies neither timestamp.
An exact non-advancing checkpoint retransmission needs no prior checkpoint bytes.
The candidate and current snapshot keep their age checks; the authenticated
prior checkpoint is not subject to a current-age check.

### 6.2 WS server adapter and real transactions

Use WS's checkpoint-inclusion stream, original wrappers/receipts, pinned mTLS,
challenge-bound linearizable latest reads, full control/record/intent proofs and
strict synchronous off-node custodian per append acknowledgement and fresh-read
cross-check. WS's old idle snapshot-reissue recommendation is superseded by D38=B:
only real changes use inclusion, with no local wrapper-format change. These are required qualification dependencies, not deployed services.
All retries/pages/handshakes/custodian reads share the remaining five-second
maximum (or stricter consumer budget); timeout/unavailable custodian closes
authority, including on unchanged-head fresh reads. B avoids heartbeat appends,
not that dependency. Registrar WC2 transport acceptance remains pending its
own decision; this registry adapter does not depend on its outcome.

WS's eight-member projection is exactly `{registry_id,owner,registry_seq,
registry_head,checkpoint_id,checkpoint_time,candidate_digest,previous_checkpoint_id}`.
Independently validate originals, scope, signatures, full ancestry, challenged
head/control proofs, explicit genesis and exact candidate/commit evidence;
retain all eight plus originals/receipts. Copy checkpoint.at verbatim into
checkpoint_time, never translate or round it. Only THEN derive the closed S4
fact by omitting checkpoint_time and candidate_digest:
`{registry_id,owner,registry_seq,registry_head,checkpoint_id,previous_checkpoint_id}`.
Never pass eight fields to the six-field API, trust a received tuple or discard
the excluded facts. Positive safe registry_seq is distinct from witness sequence:
one real registry entry takes intent plus committed positions. Retain previous
checkpoint time independently of the six-field fact, with candidate inventory
and witness/control anchors; null predecessor needs external genesis authority.
Same registry seq and head with a different checkpoint id is a fork too; fence,
retain both verified originals and signal. A lower candidate is rollback.

The registry writer has **no witness append credential or append permission**.
The separate witness-side bridge validates complete recoverable candidate
inventory, not a writer digest, and appends only its provisioned registry stream.
It cannot write other consumer streams. Exact wrapper+predecessor retries return
original receipts. Reserve room for both positions; no second intent while one
is unresolved. The bridge independently verifies exact durable local commit
before matching resolution; the witness alone cannot prove durability/exclusion.

For each real accepted entry: quiesce publication and exclude old instances;
validate exact candidate and proof budget; acknowledge external intent before
any authoritative local commit; sync exact state/inventory/outcome/watermark;
acknowledge matching committed resolution; revalidate bytes/time/authority;
atomically publish the resolved head, then bound freshness and acknowledge.
One checkpoint per real entry, no batching or owner-root-change op in v0.
Freshness-only state follows §5.2, not this witnessed inventory; a real candidate
binds the freshness base/head transition and every publication dependency.
An intent or signed checkpoint alone is never publication success. Failed or
uncertain intent fences old authority even without local commit. Unknown
outcome requires fresh challenged discovery; unresolved intent requires approved
exact-candidate recovery, never automatic cancellation or silent downgrade.

## 7. Failure, backup and restore

| Condition | Required result |
| --- | --- |
| Invalid bytes/signature/authority | Exact refusal, no new authoritative state. |
| Disk, signature service, clock, witness or audit unavailable | Close dependent admission/publication; stale snapshots expire at consumers. |
| Intent outcome unknown | No local commit; fence and discover the actual latest head. |
| Local commit or sync failed after intent | Indeterminate state; no resolution or success, retain all candidate evidence. |
| Resolution acknowledged, response lost | Reconcile exact committed state; retry returns retained outcome once authorized. |
| New head conflicts with surviving checkpoint | Fence and alert; neither restored local bytes nor a fresh timestamp clears it. |

Keep encrypted off-node backups of exact record/journal history, Git objects,
configuration, freshness history/floors, exact publication proofs/results,
durable outcomes and transaction evidence, with retention covering
all acknowledged state. Recovery keys stay with the owner. Backups and independent
trusted checkpoint history have different jobs: a backup alone proves no freshness.

Before startup/restore, exclude old instances for the whole serving lifetime,
verify pins/custody, establish time continuity, validate all local state and request
a fresh challenge-bound authoritative witness head. Match exact state/head and
prove continuity from off-node evidence and reconcile the independent freshness
floor/history under §5.2, without treating an unchanged witness head as freshness.
Missing files, empty stream, lower head,
broken chain or pending intent fences. A restored lineage never becomes bootstrap.
Initial bootstrap requires explicit protected owner authorization and a pinned
initial state; no automatic “first use” from absent receipts or counters.

Recover an unresolved intent only with old instances excluded and independently
approved exact candidate bytes/history. A digest cannot reconstruct missing bytes.
If those bytes or independent authority are unavailable, stay fenced. Mode changes,
root replacement and stream repair need reviewed procedures, not a reset flag.
Registrar option A remains its current policy until option C is qualified; no
automatic fallback or claimed registry deployment changes that consumer contract.

Joint loss/rollback of registry and witness on the trust node is a residual
failure domain. A checkpoint on that node is not independent. The owner reconciles
against off-node history or explicitly invalidates old roots and reissues rights;
incomplete evidence leaves consumers fenced. No automatic restored-state restart.

## 8. Threats and residual risks

| Threat | Mitigation and limit |
| --- | --- |
| Forged signer, cross-role key or wrong scope | Pinned S4 strict signatures, D30 and protected pins; compromised original roots remain trusted failures. |
| Replay, equivocation, rollback or clone | Exact-byte binding, durable ordering, witness continuity and external checkpoints; single-node compromise defeats local independence. |
| Partial write, race or lost acknowledgement | Serialized intent/commit/resolution, durable outcomes and exclusion; real filesystem/crash qualification still required. |
| Withheld record or stale distribution | At-most-60-second age after issuance, final online checks where required; no bound before first honest signed publication. |
| Malicious submitter or repository | Closed schemas, bounded transport, no hooks or path interpretation; signed prose stays data, not instructions. |
| Resource exhaustion or missing dependencies | Finite capacity/deadlines and fail closed; deliberate denial of service remains possible. |
| Metadata disclosure | Read ACLs, minimal diagnostics, encrypted backup; hashes/signatures provide integrity, not confidentiality. |
| Split snapshot histories from a compromised issuer | Reviewed S4 predecessor/number continuity, durable consumer freshness/head floors, complete-history verification, published checkpoints and independent comparison with operator alerts; unchanged witness heads cannot expose hidden freshness; a co-located compromised witness or partition can hide forks until external views meet. No pre-publication deadline or honest-issuer proof follows from a fresh signature. |

## 9. Shared time and continuity

Import `timeconf` at B's exact revision through T; use its protected shared policy
and stricter-only service loader. Production embedding calls `CollectorEntry()`
first in `main`, and `StartSample` before any decision. No local collector, clock
fallback, environment-selected source or copied uncertainty implementation.

T requires sync age strictly below 2048 seconds, uncertainty at most 1.5 seconds
inclusive, and an exclusive authority end `min(sample+300s, reference+2048s)`.
Use returned conservative uncertainty, including skew ppm conversion; do not
reduce it to offset/root distance. Every acceptance, issuance and final publication
requires fresh confidence before the endpoint, not just a healthy startup sample.

Retain a durable effective-time watermark and previous decision bound outside
the rollback domain. F evaluates expiry at `max(now,W)` and starts at `min(now,W)`;
snapshot issuance cannot be future at that lower bound. New process lower bound
must be strictly after previous `P + deltaP`; wait monotonically and resample.
Waiting cannot recover missing history or erase a hold. Q11 (decided 2026-10-24,
content review) requires a durable watermark, zero backward tolerance and explicit
recovery after a continuity failure. Cross-process hold preservation and
dedicated-service authdata remain qualification gates, not supplied by the
provider import. Time failure never blocks the independent emergency stop.

## 10. Questions for review

Placement, root separation, E1 and registrar Git consumption remain settled.
Decided rows below record date/source; remaining choices retain options and
recommendations without reopening accepted S4 decisions. Activation is separate.

| Question | Options, recommendation or recorded decision |
| --- | --- |
| Q1: repository location | Decided, 2026-10-24: separate public service repository, this one; task board private. |
| Q2: format/profile alignment | S3 D33–D37 and D38=B accepted 2026-10-24; S4 merged at F's exact revision, recorded 2026-10-25 (F §§2.7, 2.9, 6–7; D39–D41). Closed freshness and both new verifier calls are specified; code re-pin, integration and activation remain separate. General aggregate profiles and purpose-5 runtime qualification remain gated; no signing-domain aliases. |
| Q3: custody | Decided, 2026-10-24, owner decision: registry-owner key held by a separate signer process under its own account on the trust node; only pre-approved record profiles (isolated closed-profile signer). Full-node compromise still reaches the signer. External issuers qualify their own custody/currentness. |
| Q4: API and proof budgets | Decided, 2026-10-24, content review: mTLS for publishers and a restricted local socket for administration. Keep the caps: intake at 512 KiB and full publication bundle at 2 MiB separately; WS wrapper fit must preflight before intent or refuse. Freeze framing and qualify representable proofs; a larger/paginated WS transport needs separate review. |
| Q5: filenames/history | Decided here, 2026-10-24: 16-digit decimal sequences cover safe integers; physical fanout may be flat or prefix directories, recommend bounded fanout after measurement. S3 does not prescribe Git layout; R's enrollments/*.json and E1 history exception stay exact. |
| Q6: Git dependency | Decided, 2026-10-24, content review: protected, bounded system-Git adapter under the existing dependency policy, with a separate maintainer signer and qualified durability; operator pin approval stays external. |
| Q7: revocation completion | Decided, 2026-10-24, content review, per owner decision: consumer acknowledgement with explicit consumer sets and durable barriers, or proven expiry of all prior authority; critical actions require a final online check. An intake acknowledgement is never completion. Landed B §6.5 still preserves broker v0 authority/restore fences until migration. Publication receipt/freshness is not completion. |
| Q8: capacity | Decided, 2026-10-24, content review D36: pages under one complete signed head; Merkle/narrower-domain alternatives deferred. Wire/completeness/retention qualification remains future work; current count AND byte refusal, fence, retention and operator signal stay mandatory. |
| Q9: unchanged refresh | Decided, 2026-10-24, content review D38=B: 20-second separate freshness publications; no unchanged journal/checkpoint/witness/custodian append, per-entry checkpoint for real changes. S4 now supplies the exact profile/currentness APIs (F §2.9, D38–D41); code re-pin and consumer qualification remain gated. No owner-root-change op; D31 recovery remains separate. |
| Q10: witness coordination | WS server is selected; independent witness-side bridge, exact eight-to-six adapter and mandatory advancing anchor, content review decision, 2026-10-24. Open with the owner: off-node custodian provider; bridge/candidate/exclusion implementation and strict custodian SLA remain unqualified; recommend independent validation, original evidence retention and no writer credentials. Registrar WC2 stays separate. |
| Q11: time activation | Decided, 2026-10-24, content review: durable watermark, zero backward tolerance and explicit recovery. Hold continuity and dedicated-service authdata remain qualification gates; missing independent history fences. |
| Q12: complete publication/restore | Decided, 2026-10-24, content review: complete chained freshness history and off-node heads retained before use, with §5.2 atomic local publication and fail-closed restore; S4 supplies the wire/anchor; independent storage and complete-history integration remain unqualified. Historical re-verification needs a separate future path; gossip alone cannot prove absent hidden publications. Co-location/partition and no pre-publication deadline remain explicit. |
| Q13: witness metadata disclosure | Open with the owner: approve what witness/custodian metadata may be disclosed and to which read principals before public distribution. Recommend minimal authenticated disclosure; this design makes no provider or disclosure choice. |
| Q14: D39 prior records | Decided, recorded 2026-10-25; F §§2.7, 6 and D39. Every advancing proof supplies both prior entry anchor and exact prior checkpoint via IDJournal, J5a then updated J6; no extra projection timestamp. |
| Q15: D40 state and bounds | Decided, recorded 2026-10-25; F §§2.9, 6–7 and D40. Explicit authorized publication genesis, exact FreshnessAnchor and WitnessReadAt, independent 64-publication/532,480-byte bounds; durable state before use. |
| Q16: D41 recovery/composition | Decided, recorded 2026-10-25; F §§2.9, 7 and D41. Explicit separate re-anchor with the accepted skipped-span fork-detection cost, full retained history and endpoint audit. S4 lands with its documented composition boundary; approved S5 receipt requirements are in §5.3, before K3/D12 activation. The receipt API remains pending S5, not an open choice of a weaker legacy Context. |
| Q17: D42 external-root currentness | Open design question, recorded 2026-10-25, owned by the owner and the content review, due before the broker D12 adapter. The current D32/S5 scope is unchanged. Three alternatives: (a) the root signs snapshot content only when it changes; the registry attests currency with its own freshness/1 and has no authority to alter that content. A compromised registry-owner key could withhold a newer root snapshot; the witness and full history mitigate this only if the root checks that its own submission was journaled. Freshness/1 at F (f76a400) binds a single snapshot, so this alternative requires a format change by the format owner. (b) each root maintains its own freshness/1 chain, signed by that root. This preserves D32 separation but requires an online freshness signer per root, separate from the content key. (c) the roots remain online and reissue snapshots within max_age; this is a custodian requirement. D42 remains an activation gate for grant, D33 service-key and device intake. No option is selected and no external-root extension profile is approved here. |

## 11. Implementation slices and evidence plan

Each slice starts with executable negative cases and nearby positive controls
bound to the approved contract. Names below are planned tests, not passing tests.
No production activation follows from mocks or two implementations agreeing.

| Order / gate | Negative tests first | Positive controls and scope |
| --- | --- | --- |
| 1. Pin contracts/activation | `TestUnpublishedProfileRefused`, `TestCrossRoleRootRefused`, `TestForeignPurpose5DomainRefused` | `TestPinnedS3ProfileAccepted`; accepted S3 and reviewed S4/custody/API gates before dependent activation. |
| 1a. S4 code re-pin before dependent slices | `TestS3PinCannotAdmitFreshness`, `TestS4ProfileWideningRefused`, `TestLegacyCurrentnessNotWeakened` | `TestPinnedS4ProfileAccepted`; explicit dependency/contracts/pins and fixture re-pin to F, preserving original refusal suites; separate review before implementation uses S4. |
| 2. Pure intake/service adapter | `TestWrongIssuerScopeRefused`, `TestInvalidSignatureBeforeConflict`, `TestDuplicateCCJMemberRefused`, `TestOversizeNoMutation`, `TestUnknownKeyClassRefused`, `TestServiceForbiddenRoleRefused`, `TestServiceMissingParentRefused`, `TestServiceDepthZeroRefused`, `TestServiceRevokedParentRefused`, `TestServiceRevokedAttestationRefused` | `TestAuthorizedSignedRecordAccepted`, `TestServiceDelegationAccepted`; exact S3 profiles and parent/revocation checks, no effects. |
| 3. Serialized durable storage/bounds | `TestChangedBytesSameIDRefused`, `TestConcurrentHeadConflict`, `TestSyncFailureFences`, `TestSequenceExhaustionNoWrap`, `TestPublicationProofCapRefused`, `TestWitnessWrapperCapRefused` | `TestExactRetrySameOutcome`, `TestCrashReconcilesExactCommit`, `TestBoundedCompleteProofAccepted`, `TestWrapperFitsBeforeIntent`; no writes/intent on preflight refusal. |
| 4. Enrollment projection | `TestInvalidMirrorFencesWholeLoad`, `TestUnapprovedCommitRefused`, `TestTombstoneHistoryCannotDisappear`, `TestReservedSigsWinsSnapshotWide` | `TestOfflineEnrollmentSignedCommitAccepted`; unchanged P/R and approved E1 rotation. |
| 5. Journal/witness proof | `TestWriterCannotAppendWitness`, `TestStaleChallengeRefused`, `TestUnresolvedIntentFences`, `TestRestoredCloneCannotServe`, `TestSameHeadCheckpointForkRefused`, `TestAdvancingAnchorMissingRefused`, `TestAnchorTimeRegressionRefused`, `TestCheckpointTimeRegressionRefused`, `TestEightFieldProjectionRefused`, `TestPublicationLinkLossRefused`, `TestIntentOnlyPublicationRefused`, `TestDelayedProofExpiredRefused` | `TestExactIntentCommitResolution`, `TestLostResolutionReceiptFreshHead`, `TestAnchoredSuffixAccepted`, `TestValidatedProjectionReduced`, `TestPublicationBundleVerifiedAtIssue`; real ACL/crash/custodian/exclusion qualification, archive is not a current receipt. |
| 6. Base snapshots/time/capacity | `TestHeadChangeStaleSnapshotRefused`, `TestHeadChangeOldSnapshotRefRefused`, `TestHeadChangeMissingRecordRefused`, `TestHeadChangeMissingReissueRefused`, `TestHeadChangeMissingPriorCheckpointRefused`, `TestHeadChangeWrongPriorCheckpointRefused`, `TestStaleSnapshotRefused`, `TestEqualIssueTimeDifferentBytesRefused`, `TestCumulativeRemovalRefused`, `TestGapRefused`, `TestSplitHeadSignalsAndRefuses`, `TestSnapshotCapacitySignalsWithoutDrop`, `TestTimeLossNoPublication` | `TestHeadChangeReissueAccepted` (P-JRN-publish-reissue at F), `TestFreshCompleteSnapshotAccepted`, `TestConsumerAgeBoundary`, `TestMatchingGossipHeadAccepted`; identical cumulative set reissued at greater issued_at, same-transaction add/revocation/1 entry and new checkpoint snapshot reference; issuer profiles, byte/count limits and provider qualification. |
| 7. S4 publisher/currentness after re-pin | `TestFreshnessMissingRefused`, `TestFreshnessStaleRefused`, `TestFreshnessWrongRootRefused`, `TestFreshnessWrongSnapshotRefused`, `TestFreshnessWrongHeadRefused`, `TestFreshnessSameSeqDifferentBindingRefused`, `TestFreshnessBackwardSeqRefused`, `TestFreshnessBackwardTimeRefused`, `TestFreshnessPredecessorMissingRefused`, `TestFreshnessHeadRaceRefused`, `TestFreshnessUnresolvedIntentRefused`, `TestSlowWitnessClosesFreshness`, `TestFreshnessHistoryOmissionRefused`, `TestRestoredFreshnessStateFences`, `TestOldS3RejectsAgedBase`, `TestFreshnessCannotReviveAuthority` | `TestFreshnessKeepsJournalHead`, `TestRealChangeUsesWitnessTransaction`, `TestAgedBaseAcceptedByVerifyFreshness`, `TestFreshnessAtomicRetry`; identical base bytes and unchanged registry/witness positions on healthy freshness; exact effects and boundaries verified, never a global old-verifier waiver. |
| 7a. S5 receipt composition before K3/D12 | `TestReceiptWrongBindingRefused`, `TestReceiptAtExpiryRefused`, `TestReceiptExtensionRefused`, `TestReceiptAfterRestartRefused`, `TestNonFreshnessReceiptIssuerRefused`, `TestLegacyOldBaseCompositionRefused` | `TestFreshnessReceiptCompositionAccepted`, `TestCurrentSnapshotLegacyCompositionAccepted`; only freshness calls issue in-memory exact-id-bound receipts, exclusive fixed expiry and ids-only audit; API pending S5. |
| 7b. Registry intake S5 and D42 activation | `TestQuietRegistryIntakeWithoutReceiptRefused`, `TestQuietRegistryIntakeRebuiltEvidenceRefused`, `TestRegistryIntakeWitnessUnavailableRefused`, `TestStaleGrantSnapshotWithRegistryReceiptRefused` | `TestQuietRegistryIntakeWithReceiptAccepted` (more than 60 seconds of quiet, same Evidence), `TestReissuedRegistryIntakeEvidenceAccepted`, `TestCurrentGrantSnapshotWithRegistryReceiptAccepted`; pending S5, unchanged external-root age and D42 gate, no effects on refusal. |
| 8. Restore and consumer activation | `TestRestoredBytesCannotBootstrap`, `TestJointLossFences`, `TestStopWorksWithoutRegistry`, `TestNoCompletionOnIntakeAck`, `TestStoredProofNotCurrentAuthority` | `TestOffNodeExactCandidateRecovery`, `TestReviewedConsumerMigration`, `TestKeymakerPublicationAndStatus`; owner barriers, independent retained proofs/freshness and consumer-specific activation. |

Slice 6 uses the actual three-entry
[P-JRN-publish-reissue](https://github.com/relux-works/waggle/blob/f76a400ebeb599b95a9bf9b2c59b522bef0392d8/vectors/swarm-envelope-2/1-positive/P-JRN-publish-reissue.json)
at F §2.7 for `TestHeadChangeReissueAccepted`, with prior checkpoint context. These
six exact fixture negatives each pair with that positive, not a two-entry proxy:

| Fixture suffix (N-JRN-publish-reissue-) | Planned negative | Expected refusal |
| --- | --- | --- |
| old-snapshot | `TestHeadChangeOldSnapshotRefRefused` | checkpoint_snapshot_invalid |
| missing-record | `TestHeadChangeMissingRecordRefused` | journal_chain_broken |
| missing-reissue | `TestHeadChangeMissingReissueRefused` | journal_chain_broken |
| missing-prior | `TestHeadChangeMissingPriorCheckpointRefused` | journal_unavailable |
| wrong-prior | `TestHeadChangeWrongPriorCheckpointRefused` | checkpoint_invalid |
| backward-time | `TestCheckpointTimeRegressionRefused` | journal_rollback |

The neighboring stale-base `TestHeadChangeStaleSnapshotRefused` still expects
journal_unavailable. Each pre-intent refusal has an empty plan and no publication,
witness intent or local mutation. Real head changes keep the journal/reissue
transaction; idle periods require the separate freshness path after qualification.

Additional planned S4 controls pair each failure class with named valid controls:

| Contract | Planned negatives / expected boundary | Named positive controls |
| --- | --- | --- |
| Explicit genesis and minimum time | `TestFreshnessMissingAnchorRefused` (freshness_unavailable), `TestFreshnessGenesisInvalidRefused` (freshness_state_invalid), `TestFreshnessZeroWitnessRefused` (freshness_witness_invalid), `TestFreshnessMinimumTimeNotIncreasingRefused` (freshness_time_invalid) | `TestFreshnessGenesisMinimumTimeAccepted`, `TestFreshnessMinimumTimeAdvanceAccepted` |
| Global positions across head changes | `TestFreshnessHeadChangeSequenceGapRefused` (freshness_gap), `TestFreshnessHeadChangeSequenceResetRefused` (freshness_rollback) | `TestFreshnessHeadAdvanceGlobalSequenceAccepted` |
| Null/non-null predecessor | `TestFreshnessSameHeadNullPrevRefused`, `TestFreshnessHeadAdvanceNonNullPrevRefused` (freshness_gap) | `TestFreshnessSameHeadPredecessorAccepted`, `TestFreshnessHeadAdvanceNullPredecessorAccepted` |
| Exact retransmission | `TestFreshnessSamePositionChangedContentRefused`, `TestFreshnessRetransmissionTimeChangedRefused` (freshness_fork), `TestFreshnessSameIDChangedBytesRefused` (envelope_conflict at the retaining adapter) | `TestFreshnessExactRetransmissionKeepsPosition` |
| Bound authenticated read | `TestFreshnessWitnessReadMissingRefused`, `TestFreshnessWitnessReadStaleRefused`, `TestFreshnessWitnessReadPreTipRefused`, `TestFreshnessWitnessReadFutureRefused` (freshness_unavailable) | `TestFreshWitnessReadAtTipAccepted`, `TestFreshWitnessReadAgeBoundaryAccepted` |
| Proof/query and route caps | `TestFreshness65PublicationsRefused` (freshness_malformed), `TestFreshness65QueriesRefused` (freshness_request_invalid), `TestFreshnessOversizeRecordRefused` (freshness_malformed), `TestFreshnessWrongRouteRefused` (proof_limit) | `TestFreshness64PublicationsAccepted`, `TestFreshness64QueriesAccepted`, `TestFreshnessBundleRouteAccepted` |
| Explicit re-anchor, never silent fallback | `TestFreshnessNoImplicitReanchor` (normal freshness_gap), `TestFreshnessReanchorGenesisRefused` (freshness_state_invalid), `TestFreshnessReanchorBackwardHeadRefused` (freshness_rollback), `TestFreshnessReanchorEqualHeadForkRefused` (freshness_fork), `TestFreshnessReanchorSnapshotMismatchRefused` (freshness_binding_mismatch), `TestFreshnessReanchorSequenceNotAdvancedRefused`, `TestFreshnessReanchorTimeNotAdvancedRefused` (freshness_rollback) | `TestFreshnessReanchorSameHeadAccepted`, `TestFreshnessReanchorAdvancedHeadAccepted`, `TestFreshnessAdjacentReanchorAccepted` (skip count zero) |
| Witness and cumulative safety after recovery | `TestFreshnessReanchorWitnessUnavailableRefused`, `TestReanchorCannotSupplyRollbackSafe`, `TestReanchorCumulativeRemovalRefused` (unavailable/invalid evidence, no use) | `TestReanchorQualifiedCumulativeEvidenceAccepted`, `TestFreshnessReanchorAuditPersisted` |
| Consumer composition | `TestLegacyOldBaseCompositionRefused` (legacy own-age refusal as in V's TestFreshnessCompositionBoundary), all slice 7a receipt and 7b intake refusals | `TestCurrentSnapshotLegacyCompositionAccepted`, `TestFreshnessReceiptCompositionAccepted`, `TestQuietRegistryIntakeWithReceiptAccepted`, `TestReissuedRegistryIntakeEvidenceAccepted`, `TestCurrentGrantSnapshotWithRegistryReceiptAccepted` after S5 |

All normal/re-anchor refusal and revoked controls must assert zero plans and
unchanged anchor/watermark. Acceptance controls must assert the exact normal
IDs/watermark and absence of reanchor, or the exact recovery endpoints/skip count,
and durable atomic persistence before use. These are future implementation tests,
not claims that documentation inspection executed or qualified those paths.

Content-review acceptance of this design precedes code. Later implementation evidence
must distinguish unit coverage, negative controls, crash/ACL qualification and
consumer activation; existing services and paused formats remain separate work.
