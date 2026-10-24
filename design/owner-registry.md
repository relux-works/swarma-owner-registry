# Owner registry service design

Review draft, 2026-10-24, reconciled to accepted S3 D33–D37 and D38=B.
This is a service design, not implementation or activation evidence. S3 record
contracts are normative; S4 freshness formats/APIs, custody, transport and
recovery qualification remain gates. Content formats belong to their owners.

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
| F | [Shared formats](https://github.com/relux-works/waggle/blob/9b42338b8a57d12a21da4db4375b269deacdd6bf/spec/swarm-envelope-2.md), §§2–7: envelopes, signer contract, provenance, refusal order and consumer gates. |
| D | [D1–D38](https://github.com/relux-works/waggle/blob/9b42338b8a57d12a21da4db4375b269deacdd6bf/UNRESOLVED_QUESTIONS.md), especially D30 and D32–D38. D33–D37 and B are accepted by content review on 2026-10-24; the pinned text still presents them for review and recommends A, superseded here by that decision. |
| K | [Key service](https://github.com/relux-works/swarma-keymaker/blob/5eb89870d25cd9c4dd9e71180d9449f18fba206f/spec/keymaker.md), §§4.3, 5, 8–9: custody, pins, enrollment and purpose 5. |
| W | [Witness consumer](https://github.com/relux-works/swarma-keymaker/blob/5eb89870d25cd9c4dd9e71180d9449f18fba206f/spec/keymaker-witness.md), §§3.3–6, 8: operations, intent ordering, recovery and WC1. |
| B | [Broker](https://github.com/relux-works/swarma-credential-broker/blob/f2c01f52477b5885aefd5b6bd04d09a6a22813b2/spec/broker.md), §§6.4–6.6: local authority and the registry boundary. |
| T | [Time policy](https://github.com/relux-works/swarma-credential-broker/blob/f2c01f52477b5885aefd5b6bd04d09a6a22813b2/docs/time-policy.md) and [provider contract](https://github.com/relux-works/swarma-credential-broker/blob/f2c01f52477b5885aefd5b6bd04d09a6a22813b2/docs/time-confidence.md). |
| R | [Registrar](https://github.com/relux-works/swarma-registrar/blob/2219f70358fb7b5c7d66f229c80a8ba4a1fe0230/spec/registrar.md), §§4.2–4.5 and Appendix C.3. |
| P | Public [ccj](https://github.com/relux-works/swarma-registrar/tree/2219f70358fb7b5c7d66f229c80a8ba4a1fe0230/ccj), [enrollment](https://github.com/relux-works/swarma-registrar/tree/2219f70358fb7b5c7d66f229c80a8ba4a1fe0230/enrollment) and [registry](https://github.com/relux-works/swarma-registrar/tree/2219f70358fb7b5c7d66f229c80a8ba4a1fe0230/registry) packages. |
| WS | [Witness server proposal](https://github.com/relux-works/swarma-keymaker/blob/e7c5e1850c29d1f931de3faa24ad39d94da41ada/spec/witness-service.md), PR #20: exact server/wrapper, challenged reads, strict custodian and limits; spec only, not deployed or qualified. |
| V | S3 public [record](https://github.com/relux-works/waggle/tree/9b42338b8a57d12a21da4db4375b269deacdd6bf/record), [verifier](https://github.com/relux-works/waggle/tree/9b42338b8a57d12a21da4db4375b269deacdd6bf/verifier) and [fixtures](https://github.com/relux-works/waggle/tree/9b42338b8a57d12a21da4db4375b269deacdd6bf/vectors/swarm-envelope-2): pure admission, not I/O or activation. |

S3 defines §6's journal/checkpoint types, exact tags and APIs. Publication,
content acceptance, repository landing and consumer activation are distinct;
the acceptance recorded above does not qualify storage or deployed consumers.
D38=B supersedes the pinned recommendation, but does not register freshness/1:
its exact format and freshness-aware verifier are pending S4; invent no fields.

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
| Registry-owner root | Registry-scoped `revocation/1` and checkpoints; future freshness/1 only after S4, with agent scope pinned to the exact purpose-5 root | Online restricted signer on trust node, or external option in Q3; registry-built reviewed profiles only | Owner-approved protected pin transition, continuity preserved; new profiles/recovery require review | False scoped revocations/checkpoints, future freshness and withholding, not other roots' signatures |
| Offline enrollment publisher | E1 Ed25519 `enrollment/1` only | Offline with the owner; explicit publisher operation, never service access | R's full-history re-sign and atomic pin advance; bodies/ids/tombstones retained | Forged enrollment envelopes; maintainer review and operator commit pin remain separate barriers |
| Registry maintainer | Git commits for R's allowlist | Proposed separate online signer on trust node; export writer may request signatures, operator alone approves deployment pins | Owner changes allowlist and exact approved revision without resetting history | Forged commit signatures, not E1 content or deployment approval |
| Witness service | Its own receipt/head profiles | Separate online witness account/process on trust node; witness daemon only | Explicit owner-provisioned key epochs, old continuity and off-node checkpoints retained | False heads/receipts within its scope; co-location leaves full-node compromise shared |
| Witness consumer/bridge principal | Only its reviewed stream authentication/record profile, never content | Separate protected witness-side account; no credential available to registry writer | Owner rebinds credentials without stream reset; exact-candidate recovery gates persist | That consumer stream's claims, not other consumers' streams or content roots |
| Off-node checkpoint custodian | WS independently authenticated checkpoint confirmations/fresh reads; exact provider profile gated | Separate off-node administration, never registry/witness account access | Explicit owner-approved continuity-preserving policy; no reset from host backup | False latestness/rollback claims within its retained coverage; combined loss with all surviving evidence is outside the guarantee |
| Transport principals | TLS authentication only | Dedicated publisher/client or server process; private client keys never stored as records | Protected ACL/certificate update with explicit revocation and no root discovery | Allowed channel use, not content or witness authority |

Maintainer and registry-owner keys differ from each other and from every content
root, including E1. External issuer custody is not decided here: if an issuer
cannot provide fresh required signatures, its domain stays unavailable.

Recommend an independently generated Ed25519 registry-owner key; no assigned key-service purpose or
derivation is implied. The online signing component lives in the registry account's boundary on the trust
node, or in an explicitly chosen external custody provider. It accepts only locally constructed reviewed
profiles, not arbitrary preimages. No registry access to message/SSH signing is needed.

The owner chooses protection (Q3): a protected software file is simple but exposes the key to
service-memory compromise; a separate signer process/account with a closed signing API narrows accidental
exposure but not full-node compromise; hardware or an independently administered remote signer can
prevent extraction but adds latency, availability and authorization-policy dependencies. An offline key cannot autonomously sustain freshness publications.
No online delegation format is invented to hide that tradeoff.

Any software option requires private directories, owner/mode and no-link checks, no secret
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
| Future `freshness/1` | Internal registry-owner publication only after S4; no external intake or configurable new class. |
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
No function commits, executes effects, fetches or authenticates witness wire.
Revalidate at the serialized commit/publication boundary. Use P's CCJ-1, never
a second codec/verifier. Current-generation truth needs qualified external
evidence; K's purpose-5 preimage alignment remains a separate activation gate,
not an alias accepted because S3 exists.

D33 service intake requires Ed25519, key_class=service, purpose=grant-service,
roles=[author], actions=[grant.sign], exact bounded owner binding, positive
validity <=86,400 seconds, and the pinned grant-owner issuer with explicit
protected grant-service purpose/owner. Verify its exact key-indexed attestation
and grant-owner revocation snapshot. Service key/attestation and every chain
issuer, subject and grant id remain subject to that revocation domain. A link
requires a covering parent addressed to the service with depth >=1 and all
narrowing/time checks. Attestation grants no parent or self-root authority;
agents remain forbidden issuers. Unknown classes stay closed per version.

Allocate one global positive safe-integer journal sequence per accepted mutation, exactly previous +1;
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
transport encoding. S3 permits <=64 entries plus <=2 checkpoints, <=540,672
received decoded bytes before the context snapshot/other evidence; that is
not the 512 KiB intake budget. Precharge both per-record and aggregate limits,
including encoded carriers. Over-cap is `proof_limit`, never truncation or
an assertion of complete reconciliation. WS additionally requires the whole
checkpoint wrapper including LF <=8,192 bytes, frames <=65,536, <=64 proof
pages/128 entries, and one shared remaining deadline. Many valid S3 proofs do
not fit that wrapper: preflight the complete encoded wrapper before intent;
refuse unless it fits, or await a separately reviewed bounded adapter/transport.
No claim that the maximal S3 proof fits WS, and no ad hoc pagination.

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
helpers; never execute a repository-supplied program. Git dependency is Q6.

For real head changes, serialization couples record bytes, journal entry,
checkpoint, id binding, request outcome and watermark. Readers see one resolved
state. Failure at any
boundary stops publication and new acceptance; an indeterminate commit never
restores an older ref as if the candidate had not happened. No history rewriting
or garbage collection may discard acknowledged evidence or pending candidates.

## 5. Snapshots, freshness and consumers

### 5.1 Immutable base and B currentness (pending S4)

F's closed `revocation/1` is one issuer's signed complete cumulative set with
issued_at and effective entries. Preserve exact base bytes/id until a real
change; registry signatures never replace operator/grant-domain signatures.
The global journal orders references, not each domain's cumulative truth.
Validate original issuer authority, all retained/future-effective entries and
continuity; never remove entries or move from later. Neither revocation/1 nor
checkpoint/1 gains a local snapshot-prev or publication-sequence field.

Decided, 2026-10-24 (D38=B, content review; Q9): publish separate registry-owner-
signed freshness/1 for the exact unchanged base and current resolved checkpoint
head. It has its own monotonic sequence and strictly increasing issued_at;
its closed container, tag, genesis rules and API are **pending S4**.
The pending S4 chain requires a predecessor, strictly increasing sequence and
issued_at, and a caller-supplied last-seen freshness fact; rollback, fork and
gap refuse. This design invents no fields or signing preimage and admits no
freshness yet.
Only real head changes append journal entries/create checkpoints and use witness
intent/resolution plus custodian barriers. A changed cumulative set completes
that real transaction first; unchanged ticks preserve snapshot bytes/id and
registry/witness positions. No silent fallback to snapshot reissue.

Publish freshness every 20 seconds and promptly after a real head change:
4,320 freshness publications/day with no unchanged-head journal/witness append.
Thirty seconds gives 2,880/day; twenty leaves 40 seconds nominal headroom under
60 seconds, including one missed scheduling opportunity before polling,
transport and conservative uncertainty costs. This is not measured availability;
a 60-second cadence has no useful margin. External issuer domains still need
their own qualified currentness; registry freshness cannot re-sign their content.

Both old base issued_at AND old checkpoint.at fail today's S3 high-level
currentness checks. B needs a new reviewed path admitting immutable base and
resolved witnessed binding before S4 freshness currentness; the pending separate
B entry point verifies the base without its current-age rule, then freshness/1.
Changing only the snapshot check is insufficient. Never weaken `VerifyJournal`/`CheckRevocation`
globally, lower Now/watermark or manufacture younger context. Integration and
consumer activation wait for that path; low-level math supplies no authority.

### 5.2 Freshness durability, complete history and restore

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
surviving evidence; a restored local counter alone supplies none. S4/Q12 must
qualify the complete-publication mechanism and independent retention before B
activation, without adding unchanged-head witness/custodian writes. Missing
coverage or an unverifiable gap fences; do not claim this mechanism exists.
Startup/restore reconciles both real registry/witness lineage and the independent
freshness floor/history, plus time continuity and old-instance exclusion.
Missing newer bytes or ambiguous visibility stays fenced; never initialize
freshness from restored bytes or reset its counter to match the old journal.

### 5.3 Consumer gates and limits

Consumers pin registry root/owner/scope out of band and require exact signed
base and checkpoint/head binding, original issuer evidence, complete cumulative
revocations, monotonic retained freshness sequence/issued_at and registry head,
all required predecessors and fresh authenticated witness head/continuity.
Use trusted time/watermark and max_age <=60 seconds with the consumer's stricter
exclusive-window/conservative-delta margin; no extra skew grace. Missing, stale,
unavailable, wrong-root, mismatched, backward or incomplete evidence refuses.
Same freshness sequence with different binding/content, or same registry
sequence with different head OR checkpoint id, fences and signals the operator.
Persist exact verified bytes, anchors and watermark atomically before use.
Freshness never revives expired grants/attestations, clears revocations, moves
from later, proves revocation completion or authorizes restart.

A fresh witness read at an unchanged registry head does not reveal withheld
newer freshness. Signed predecessor history, retained heads, complete-history
checks and authenticated consumer/witness/off-node comparison expose only
covered gaps/forks; signatures, age and unchanged witness position alone do
not prove complete publication or issuer honesty. Co-compromise and partition
can hide views until independent evidence meets; no pre-publication deadline.
Distribute immutable objects and an authenticated current pointer over HTTPS;
push hints confer no authority. Older objects are archives, never fallback to
an empty set or an expired cached authorization. Until migration each consumer
keeps its existing authority and restore fence.

The distribution bound assumes a qualified complete, cumulative, non-equivocating
publication sequence. It starts at first-containing issuance S, not submission:
`max(S + A, from)` for maximum age A. D29 completion still needs a durable
consumer barrier/acknowledgement set or proven authority expiry (Q7); critical
effects check their authority owner immediately before effect. Emergency stop
works independently; restart requires separate current authority.

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

Keymaker submits original purpose-5 `attestation/1` through intake; alignment
of K's signing preimage with F remains an explicit gate. It consumes activity
status and its domain's original revocation/1 plus checkpoint head and freshness.
Activity needs independently qualified current-generation evidence, not an old
attestation or registry cache assertion. Publishing identity is not minting grants.

Return, or ensure the consumer already durably holds, this complete bundle:

1. Exact original signed record bytes/content id and required issuer/authority evidence.
2. Exact unsigned E_n binding seq, op, record_type and that record id.
3. Exact signed C_n, preferably, or C_(n+1) plus E_(n+1) linking E_n.
4. Complete oldest-first suffix from a previously independently trusted projection,
   including its authenticated raw anchor, within the cap.
5. Exact signed registry-owner snapshot whose id equals checkpoint.snapshot.
6. Independently verified exact witness intent/resolution originals and both receipts,
   plus fresh challenged head/continuity proving that exact checkpoint is resolved.

checkpoint.head is the entry id, never the original record id. From anchor n-1,
C_n needs two entries and C_(n+1) three; both use ONE candidate checkpoint and
conflict=nil. A nearer authenticated anchor may need fewer entries; with an
anchor at most63 new entries fit. A second checkpoint is fully admitted current
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
path. Under B an aged unchanged base also needs the new S4-aware path. Neither
historical nor freshness verification is exported by the pinned S3 API. Retained
success is not renewed authority or revocation completion.

## 6. S3 identity journal and witness adapter

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
| J6 | Oldest-first entry id/scope, seq/prev/nondecreasing time, final head/seq | journal_entry_invalid / journal_chain_broken |

These rows are V's separate journal entry point, not prechecks on grants/acts.
Every refusal has an empty plan. Authenticate optional fork evidence fully;
corrupt unrelated bytes are not a fork. An exact witnessed seq/head/checkpoint
retransmission alone permits zero entries. **Require the authenticated prior
head entry as anchor on every advancing suffix**, checking its id/seq/root/owner
against protected retained evidence. Advance exactly +1, matching prev and
nondecreasing time through checkpoint head. An omitted anchor could hide a first
new entry older than the prior entry. No seq0 or missing-state bootstrap;
genesis requires independent protected authorization and ancestry validation.
S4 makes that advancing anchor mandatory too; its update is pending, while
S3 already treats same-head/sequence, different checkpoint id as a fork.

### 6.2 WS server adapter and real transactions

Use WS's checkpoint-inclusion stream, original wrappers/receipts, pinned mTLS,
challenge-bound linearizable latest reads, full control/record/intent proofs and
strict synchronous off-node custodian per append acknowledgement and fresh-read
cross-check. WS's old snapshot-reissue recommendation is superseded by D38=B:
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
checkpoint_time, never translate or round it. Only THEN derive the closed S3
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
| Forged signer, cross-role key or wrong scope | Pinned S3 strict signatures, D30 and protected pins; compromised original roots remain trusted failures. |
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
Waiting cannot recover missing history or erase a hold. Propose zero backward
tolerance and operator recovery after a continuity failure. Cross-process hold
preservation and dedicated-service authdata qualification are Q11, not supplied
by the provider import. Time failure never blocks the independent emergency stop.

## 10. Questions for review

Placement, root separation, E1 and registrar Git consumption remain settled.
Decided rows below record date/source; remaining choices retain options and
recommendations without reopening accepted S3 or D38=B. Activation is separate.

| Question | Options, recommendation or recorded decision |
| --- | --- |
| Q1: repository location | Decided, 2026-10-24: separate public service repository, this one; task board private. |
| Q2: format/profile alignment | S3 journal/checkpoint/roles accepted, 2026-10-24, content review D33–D37; D38=B accepted the same date. S4 owns freshness format and new verifier; wait for reviewed APIs rather than weaken old currentness. General aggregate profiles and K purpose-5 alignment remain gated; recommend reusing original issuer revocation/1 payloads, never signing-domain aliases. |
| Q3: custody | Software file, isolated signer, hardware or external signer. Recommend isolated closed-profile registry signer as baseline; owner decides extraction/availability/recovery tradeoffs. External issuers qualify their own custody/currentness. |
| Q4: API and proof budgets | mTLS endpoint or owner-local socket; recommend mTLS publishers and restricted local administration. This design caps intake at 512 KiB and full publication bundle at 2 MiB separately; WS wrapper fit must preflight before intent or refuse. Freeze framing and qualify representable proofs; a larger/paginated WS transport needs separate review. |
| Q5: filenames/history | Decided here, 2026-10-24: 16-digit decimal sequences cover safe integers; physical fanout may be flat or prefix directories, recommend bounded fanout after measurement. S3 does not prescribe Git layout; R's enrollments/*.json and E1 history exception stay exact. |
| Q6: Git dependency | Fixed system Git or new Go dependency. Recommend protected bounded system adapter under existing dependency policy, qualified durability and separate maintainer signer; operator pin approval external. |
| Q7: revocation completion | Durable consumer acknowledgement barrier or proven expiry of all authority, plus consumer-selected final online checks. Recommend explicit consumer set/barriers; landed B §6.5 still preserves broker v0 authority/restore fences until migration. Publication receipt/freshness is not completion. |
| Q8: capacity | Decided, 2026-10-24, content review D36: pages under one complete signed head; Merkle/narrower-domain alternatives deferred. Wire/completeness/retention qualification remains future work; current count AND byte refusal, fence, retention and operator signal stay mandatory. |
| Q9: unchanged refresh | Decided, 2026-10-24, content review D38=B: 20-second separate freshness publications; no unchanged journal/checkpoint/witness/custodian append, per-entry checkpoint for real changes. S4 gates exact format/new currentness API. No owner-root-change op; D31 recovery remains separate. |
| Q10: witness coordination | WS server is selected; independent witness-side bridge, exact eight-to-six adapter and mandatory advancing anchor, content review decision, 2026-10-24. Bridge/candidate/exclusion implementation and strict custodian provider/SLA remain unqualified; recommend independent validation, original evidence retention and no writer credentials. Registrar WC2 stays separate. |
| Q11: time activation | Provider alone or qualified durable watermark/hold continuity and dedicated-service authdata. Recommend latter, zero backward tolerance and explicit recovery; missing independent history fences. |
| Q12: complete publication/restore | Independent observer, authenticated consumer comparison or jointly qualified publication-history service. Recommend S4-reviewed complete chained freshness history and off-node retained heads before use, with §5.2 atomic local publication and fail-closed restore; no invented wire or available service claim. Historical re-verification needs a separate future path; gossip alone cannot prove absent hidden publications. Co-location/partition and no pre-publication deadline remain explicit. |

## 11. Implementation slices and evidence plan

Each slice starts with executable negative cases and nearby positive controls
bound to the approved contract. Names below are planned tests, not passing tests.
No production activation follows from mocks or two implementations agreeing.

| Order / gate | Negative tests first | Positive controls and scope |
| --- | --- | --- |
| 1. Pin contracts/activation | `TestUnpublishedProfileRefused`, `TestCrossRoleRootRefused`, `TestForeignPurpose5DomainRefused` | `TestPinnedS3ProfileAccepted`; accepted S3 and reviewed S4/custody/API gates before dependent activation. |
| 2. Pure intake/service adapter | `TestWrongIssuerScopeRefused`, `TestInvalidSignatureBeforeConflict`, `TestDuplicateCCJMemberRefused`, `TestOversizeNoMutation`, `TestUnknownKeyClassRefused`, `TestServiceForbiddenRoleRefused`, `TestServiceMissingParentRefused`, `TestServiceDepthZeroRefused`, `TestServiceRevokedParentRefused`, `TestServiceRevokedAttestationRefused` | `TestAuthorizedSignedRecordAccepted`, `TestServiceDelegationAccepted`; exact S3 profiles and parent/revocation checks, no effects. |
| 3. Serialized durable storage/bounds | `TestChangedBytesSameIDRefused`, `TestConcurrentHeadConflict`, `TestSyncFailureFences`, `TestSequenceExhaustionNoWrap`, `TestPublicationProofCapRefused`, `TestWitnessWrapperCapRefused` | `TestExactRetrySameOutcome`, `TestCrashReconcilesExactCommit`, `TestBoundedCompleteProofAccepted`, `TestWrapperFitsBeforeIntent`; no writes/intent on preflight refusal. |
| 4. Enrollment projection | `TestInvalidMirrorFencesWholeLoad`, `TestUnapprovedCommitRefused`, `TestTombstoneHistoryCannotDisappear`, `TestReservedSigsWinsSnapshotWide` | `TestOfflineEnrollmentSignedCommitAccepted`; unchanged P/R and approved E1 rotation. |
| 5. Journal/witness proof | `TestWriterCannotAppendWitness`, `TestStaleChallengeRefused`, `TestUnresolvedIntentFences`, `TestRestoredCloneCannotServe`, `TestSameHeadCheckpointForkRefused`, `TestAdvancingAnchorMissingRefused`, `TestAnchorTimeRegressionRefused`, `TestCheckpointTimeRegressionRefused`, `TestEightFieldProjectionRefused`, `TestPublicationLinkLossRefused`, `TestIntentOnlyPublicationRefused`, `TestDelayedProofExpiredRefused` | `TestExactIntentCommitResolution`, `TestLostResolutionReceiptFreshHead`, `TestAnchoredSuffixAccepted`, `TestValidatedProjectionReduced`, `TestPublicationBundleVerifiedAtIssue`; real ACL/crash/custodian/exclusion qualification, archive is not a current receipt. |
| 6. Base snapshots/time/capacity | `TestStaleSnapshotRefused`, `TestEqualIssueTimeDifferentBytesRefused`, `TestCumulativeRemovalRefused`, `TestGapRefused`, `TestSplitHeadSignalsAndRefuses`, `TestSnapshotCapacitySignalsWithoutDrop`, `TestTimeLossNoPublication` | `TestFreshCompleteSnapshotAccepted`, `TestConsumerAgeBoundary`, `TestMatchingGossipHeadAccepted`; issuer profiles, byte/count limits and provider qualification. |
| 7. B freshness only after S4 | `TestFreshnessMissingRefused`, `TestFreshnessStaleRefused`, `TestFreshnessWrongRootRefused`, `TestFreshnessWrongSnapshotRefused`, `TestFreshnessWrongHeadRefused`, `TestFreshnessSameSeqDifferentBindingRefused`, `TestFreshnessBackwardSeqRefused`, `TestFreshnessBackwardTimeRefused`, `TestFreshnessPredecessorMissingRefused`, `TestFreshnessHeadRaceRefused`, `TestFreshnessUnresolvedIntentRefused`, `TestSlowWitnessClosesFreshness`, `TestFreshnessHistoryOmissionRefused`, `TestRestoredFreshnessStateFences`, `TestOldS3RejectsAgedBase`, `TestFreshnessCannotReviveAuthority` | `TestFreshnessKeepsJournalHead`, `TestRealChangeUsesWitnessTransaction`, `TestAgedBaseAcceptedOnlyByReviewedBPath`, `TestFreshnessAtomicRetry`; identical base bytes and unchanged registry/witness positions on healthy freshness; exact effects and boundaries verified, never a global old-verifier waiver. |
| 8. Restore and consumer activation | `TestRestoredBytesCannotBootstrap`, `TestJointLossFences`, `TestStopWorksWithoutRegistry`, `TestNoCompletionOnIntakeAck`, `TestStoredProofNotCurrentAuthority` | `TestOffNodeExactCandidateRecovery`, `TestReviewedConsumerMigration`, `TestKeymakerPublicationAndStatus`; owner barriers, independent retained proofs/freshness and consumer-specific activation. |

Content-review acceptance of this design precedes code. Later implementation evidence
must distinguish unit coverage, negative controls, crash/ACL qualification and
consumer activation; existing services and paused formats remain separate work.
