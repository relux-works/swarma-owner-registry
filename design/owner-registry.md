# Owner registry service design

Review draft, 2026-10-24. This document proposes a service, not an activated protocol. Content formats
belong to their format owners. Unpublished interfaces, custody choices and recovery procedures below are
gates before implementation or deployment; this document supplies neither code nor qualification
evidence.

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
| F | [Shared formats](https://github.com/relux-works/waggle/blob/67df75bec015ea463f685e4daf4975ecda18816a/spec/swarm-envelope-2.md), §§2–7: envelopes, signer contract, provenance, refusal order and consumer gates. |
| D | [D1–D32](https://github.com/relux-works/waggle/blob/67df75bec015ea463f685e4daf4975ecda18816a/UNRESOLVED_QUESTIONS.md), especially distinct roots D30 and registry boundary D32. |
| K | [Key service](https://github.com/relux-works/swarma-keymaker/blob/5eb89870d25cd9c4dd9e71180d9449f18fba206f/spec/keymaker.md), §§4.3, 5, 8–9: custody, pins, enrollment and purpose 5. |
| W | [Witness consumer](https://github.com/relux-works/swarma-keymaker/blob/5eb89870d25cd9c4dd9e71180d9449f18fba206f/spec/keymaker-witness.md), §§3.3–6, 8: operations, intent ordering, recovery and WC1. |
| B | [Broker](https://github.com/relux-works/swarma-credential-broker/blob/3924ab6e811b5512ad8763a392c2c88abcdd1b2c/spec/broker.md), §§6.4–6.6: local authority and the registry boundary. |
| T | [Time policy](https://github.com/relux-works/swarma-credential-broker/blob/3924ab6e811b5512ad8763a392c2c88abcdd1b2c/docs/time-policy.md) and [provider contract](https://github.com/relux-works/swarma-credential-broker/blob/3924ab6e811b5512ad8763a392c2c88abcdd1b2c/docs/time-confidence.md). |
| R | [Registrar](https://github.com/relux-works/swarma-registrar/blob/2219f70358fb7b5c7d66f229c80a8ba4a1fe0230/spec/registrar.md), §§4.2–4.5 and Appendix C.3. |
| P | Public [ccj](https://github.com/relux-works/swarma-registrar/tree/2219f70358fb7b5c7d66f229c80a8ba4a1fe0230/ccj), [enrollment](https://github.com/relux-works/swarma-registrar/tree/2219f70358fb7b5c7d66f229c80a8ba4a1fe0230/enrollment) and [registry](https://github.com/relux-works/swarma-registrar/tree/2219f70358fb7b5c7d66f229c80a8ba4a1fe0230/registry) packages. |
| BP | [Broker PR #13](https://github.com/relux-works/swarma-credential-broker/pull/13), inspected [head](https://github.com/relux-works/swarma-credential-broker/blob/344bd876fc4124f8361223a882e216f408377813/spec/broker.md#65-registry): **open and unmerged**. Its trust-node wording is a proposal, not the broker's landed contract. |

A supplementary format-owner working draft supplies the journal/checkpoint
shape in §6. It is non-normative, has no published commit, and may change.
Its publication and review are prerequisites, not new tag assignments here.

Non-goals: message signing or delivery, credential issuance, account creation, operator-act execution,
production verifier implementation, root recovery, contact records, exchange between owner registries, or
a registrar snapshot API. Broker v0 local authority remains unchanged; BP is unmerged and provides no
migration. The registrar continues to consume only R's pinned signed commit.

## 2. Trust boundary and key roles

Protected owner configuration pins keys, role scopes, owner, lineage, submitter ACLs, witness identity
and stream bindings. Neither a request nor repository metadata can select its own root. D30's four roots
are pairwise distinct, checked before signed evidence intake at the verifier's K1/O2/G7 positions.

| Key role | Signs | Held where / permitted user | Rotation | Compromise permits |
| --- | --- | --- | --- | --- |
| Purpose-5 service root | Agent `attestation/1` only | External key-service custody, never registry storage; internal certification issuer only | Owner-authorized root/epoch transition, descendant reissue and retained revocations; K's activation gates apply | False agent attestations in that root scope, not grants, operator evidence or registry snapshots |
| Operator root | Device attestations and operator-domain snapshots | Owner's external protected issuer; offline/device/online custody is that issuer's gated choice, not a registry key | Explicit external pin transition preserving cumulative revocation history; D31 recovery has no v0 record | False device authority/snapshots, not purpose-5 attestations or grant-root signatures |
| Owner grant root | Grants and grant-domain snapshots | Owner's external protected issuer; owner-authorized grant issuance only, custody choice stays external | Explicit pin/chain migration with old revocations retained; no automatic new-root trust | Grant-domain rights and false snapshots, not E1 or other roots' evidence |
| Agent author key | F's agent message profile, never grants in v0 | External key-service custody, resolved active caller only; registry stores attestation/public key | New qualified binding/attestation, revoke old binding/key; no stale generation reuse | Agent provenance within surviving authority, not owner/operator rights |
| Operator device key | Published P-256 low-S act/approval and permitted delegated-grant profiles | Owner's device; qualified local signer/presence path only | Operator-root re-attestation and signed revocation; recovery remains separately gated | Device acts/delegated grants within current authority; no implicit A3, root or recovery powers |
| Registry-owner root | Registry-scoped `revocation/1`, snapshots and checkpoints only; agent scope pins the exact purpose-5 root | Online restricted signer on trust node, or external option in Q3; registry-built reviewed profiles only | Owner-approved protected pin transition, continuity preserved; new profiles/recovery require review | False scoped revocations/checkpoints and withholding, not other roots' signatures |
| Offline enrollment publisher | E1 Ed25519 `enrollment/1` only | Offline with the owner; explicit publisher operation, never service access | R's full-history re-sign and atomic pin advance; bodies/ids/tombstones retained | Forged enrollment envelopes; maintainer review and operator commit pin remain separate barriers |
| Registry maintainer | Git commits for R's allowlist | Proposed separate online signer on trust node; export writer may request signatures, operator alone approves deployment pins | Owner changes allowlist and exact approved revision without resetting history | Forged commit signatures, not E1 content or deployment approval |
| Witness service | Its own receipt/head profiles | Separate online witness account/process on trust node; witness daemon only | Explicit owner-provisioned key epochs, old continuity and off-node checkpoints retained | False heads/receipts within its scope; co-location leaves full-node compromise shared |
| Witness consumer/bridge principal | Only its reviewed stream authentication/record profile, never content | Separate protected witness-side account; no credential available to registry writer | Owner rebinds credentials without stream reset; exact-candidate recovery gates persist | That consumer stream's claims, not other consumers' streams or content roots |
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
prevent extraction but adds latency, availability and authorization-policy dependencies. An offline key
cannot autonomously sustain fresh snapshots. No online delegation format is invented to hide that
tradeoff.

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
| Agent/device `attestation/1` | Purpose-5/operator-root evidence respectively; no claimed root role, self-attestation or direct device-pin replacement. |
| General `grant/1` | Owner grant-root provenance and any published permitted delegation chain; no agent-issued grants or broker-v0 domain relabeling. |
| Shared `revocation/1` | Exact issuer role/scope; complete cumulative snapshots. Registry may construct only its own authorized issuer scope. Other issuers retain their signatures. |
| `enrollment/1` | E1 signature and a complete prepared, reviewed signed enrollment projection; approval precedes acceptance of that projection. Never online self-enrollment. |
| Journal/checkpoint | Internal construction after the format slice; no caller-supplied history or sequence allocation. |
| `operator-act/1`, `message/1`, contact or unknown type | No standalone intake/effect profile here; refuse. A future signed authorization attachment needs its consumer-owned mapping. |

Before accepting new bytes, apply the following ordered gates. API-level names
below are proposed service diagnostics, not additions to F's refusal inventory.

1. Enforce TLS principal, scope, deadline, concurrency and byte budget before
   expensive decoding: `submitter_denied`, then `request_too_large`/`request_invalid`.
2. Check supported intake type and required service evidence:
   `record_type_unsupported`, then `verification_unavailable` if S1 is absent.
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

The S1 library is in progress. Required adapter interface: immutable exact input bytes plus protected
role/type policy; explicit time sample and durable watermark; attestation lookup by content id, device
lookup by key, current generation/binding; issuer-scoped snapshots with continuity; and exact-byte
id-journal availability. It must return a typed verdict and candidate persistence plan without I/O,
ambient time, private keys or effects. Service-owned atomic commit and final revalidation remain
mandatory. These are requirements, not claims about exported S1 names. Use public `ccj.Parse`/`Marshal`;
do not write a second CCJ or record verifier. Current-generation truth comes from qualified key-service
evidence, not an old attestation. The registry cannot silently infer currentness from its own cache.

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

Admission limits: F envelopes 65,536 bytes; attestations/revocations and acts 8,192 bytes; depth 16,
strings 8,192 bytes, arrays 64, signatures 8, grant chain 4 and aggregate chain 262,144 bytes. A single
submission carries one record; propose a 512 KiB total request/evidence cap, 16 in-flight submissions and
a five-second monotonic budget, including retries. Enrollment projection evidence references bounded
pre-staged objects, never an unbounded inline upload or fetch. Human review occurs before submission,
outside that operation budget. Enrollments retain R's per-file 65,536-byte and 10,000-envelope/16 MiB
candidate caps. Independent bounded reads/proofs cannot bypass these limits. Budgets and API mapping are
Q4.

Refusals leave records, sequences, pins and replay bindings unchanged unless an irreversible intent has
already occurred; that case is fenced and recoverable, never described as no effect. Bounded separate
refusal telemetry may change. Remote errors omit submitted payloads, filesystem details and signer
secrets.

## 4. Storage, signing and atomic publication

Proposed Git layout separates shared-record storage from R's existing input:

| Path | Meaning |
| --- | --- |
| `records/<record_type>/<content-id>.json` | Exact signed content bytes; closed validated type-to-path mapping, never an arbitrary caller path. |
| `journal/<seq>.json` | Reserved for format-owner draft journal entries (§6). |
| `checkpoints/<seq>.json` | Reserved for draft signed checkpoints; not yet a published wire contract. |
| `enrollments/*.json` | One E1 envelope per file, all retained host history, exactly as R §4.4 reads it. |

The signed enrollment projection is a separate export ref, built by the same writer and externally
reviewed before import. It can contain only enrollment history; the shared journal need not wait for
review of a new heartbeat. Registry history retains the exact approved Git object, and consumers pin that
object, never the general journal ref. New content cannot silently advance deployment pins. The draft
suggests twelve-digit sequence padding; Q5 resolves its range before coding. Filenames carry no
enrollment authority. Enrollment bytes are also stored under `records/`; the mirror must match exactly,
not be reserialized or re-signed. Other record types never enter `enrollments/`. No symlinks, special
files or caller-selected traversal. Reject rather than skip an invalid enrollment entry.

Shared-record authority is each original signature plus validated chain and freshness, later the signed
identity journal/checkpoint. Git preserves and moves bytes; Git commits alone do not authorize shared
grants or attestations. **Phase-0 exception:** R explicitly makes the approved pinned signed commit the
enrollment authority, with E1 as its additional integrity check. The draft phrase “Git commits prove
nothing” cannot replace that contract. Distinct maintainer signing plus the enrollment mirror preserve
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

Serialization couples record bytes, journal entry, checkpoint, id binding,
request outcome and watermark. Readers see one resolved state. Failure at any
boundary stops publication and new acceptance; an indeterminate commit never
restores an older ref as if the candidate had not happened. No history rewriting
or garbage collection may discard acknowledged evidence or pending candidates.

## 5. Signed snapshots and consumer behavior

The immediately known snapshot is F's `revocation/1`: issuer, strictly ordered
issuance time, `complete=true`, and cumulative key/record entries with effective
times. Retain original signed attestations/grants separately. Device and grant
snapshots keep their own issuer roots; the registry cannot refresh those signatures.
An aggregate distribution manifest, its signed profile and coverage semantics
belong to the format owner (Q2); this design assigns no schema or signing tag.

Propose reissuing the registry's own complete snapshot every 20 seconds even
without changes, and immediately on accepted revocation. Every different snapshot
has strictly increasing whole-second `issued_at`; same-time retransmission must
be identical. Coalesce within a second without acknowledging unpublished changes.
Other issuers need their own qualified refresh path. Twenty seconds leaves some
transport margin under 60 seconds; it is no delivery or completion guarantee.

Consumers pin root/scope out of band and require configured age at most 60 seconds,
trusted time, increasing issuer continuity and completeness. Store high-water and
known revocations durably; refuse lower issuance/sequence, equal position with
different content, removals, later effective times, or an unproved journal gap.
An advancing head needs bounded contiguous proof from a retained checkpoint;
incomplete proof fences rather than accepting an apparently fresh suffix.
The future publication profile must chain each domain's snapshots by previous
snapshot hash and monotonic number. F's closed `revocation/1` has neither member:
do not add them locally. Q2/Q12 require reviewed metadata binding exact snapshot
ids to the chain and published head. Consumers persist last-seen head and refuse
with an operator signal on rollback, gaps or equivocation. Comparing heads through
consumer gossip or the witness can expose split views; transport is not authority.

Distribute immutable objects and an authenticated current pointer over HTTPS,
with bounded polling/proofs; push hints may trigger reads but supply no authority.
Signed old objects remain archival data, not current authorization. Unknown,
missing, stale, unreachable or unverifiable required evidence refuses. Expiry
does not permit an empty revocation set or fallback to an older usable cache.
Before migration, consumers retain their existing pinned/local authority.

The bound assumes a qualified complete, cumulative, non-equivocating publication
sequence; a fresh signature/time alone proves neither completeness nor issuer
honesty. For an effective revocation, the bound begins at first-containing
issuance S, not submission: `S + A`, or `max(S + A, from)` for future entries.
An intake receipt does not mean “revocation complete.” D29 needs a specified
durable barrier/acknowledgement set or proven expiry of all admitted authority;
critical effects additionally check the authority owner immediately before effect.
Concrete completion/online-check integration is Q7. Emergency stop remains
independently available when registry, witness or time is unavailable; restart
requires separate current authority, never a stop receipt.

F's 64-entry/8,192-byte bound cannot be evaded by dropping retained entries.
Until the format owner decides Q8, preflight exact encoded candidate size/count;
signal approaching capacity to the operator, refuse an unrepresentable addition
as `capacity_exhausted`, fence affected publication/admission, and signal the
blocked domain/head with bounded diagnostics. Never acknowledge the lost update
or refresh the pre-rejection set as if it were current. Keep all retained entries.
Pages sharing a signed head, a Merkle root in the snapshot, or narrower domains
are format-owner options, not locally invented sharding or an adopted policy.

## 6. Draft identity journal and WC1 checkpoint interface

The format-owner working draft proposes an unsigned `journal-entry/1` body with
`type, registry, owner, seq, prev, at, op, record_type, record`: registry root id,
exact owner, +1 sequence, predecessor entry id, nondecreasing acceptance time,
`add`/`revoke`, original type and original signed record id. `prev` is null only
at explicit bootstrap sequence 1. Content bytes live separately and are immutable.
Authenticity comes from a signed checkpoint over the head, not a signature on
every journal entry. This is a draft shape, not a registered type/tag or parser.

Its proposed `checkpoint/1` body has `type, registry, owner, seq, head, at, snapshot`:
the exact head id and sequence, nondecreasing checkpoint time not before the head,
and the current registry-issued revocation snapshot id, signed by the registry
root as author. Same sequence with different head is equivocation: fence and alert.
Time is not latestness proof. Draft labels/preimages/limits require the next
format slice; journal wire implementation stays blocked until publication.
There is no owner-root-change op in v0: D31 recovery needs a future separate type.

Recommend one checkpoint per accepted journal entry before success, initially
without batching (Q9). Each reissued signed snapshot is a new stored record and
an `add` entry, advancing the journal sequence even when its entries are unchanged.
Its checkpoint therefore has a new path; no same-sequence checkpoint overwrite
or new journal operation is needed. Witness coordination also
covers acceptance state, retained outcomes/watermark and publication dependencies,
not only a content head which could hide a changed local transaction state.
Publish the signed head id/number/time for comparison with retained witness
evidence and the off-node trusted checkpoint. An independently observing verifier
outside the node can cross-check it; the co-located witness alone cannot prove
honesty after full-node compromise. Consumer-held heads and gossip add detection
only when conflicting views meet; Q12 gates their reviewed profile and transport.

Registry needs from the witness integration: compare-head durable retention;
challenge-bound linearizable latest-head reads including unresolved intent;
continuity proof from an independently retained checkpoint; exact receipts binding
stream, head id, sequence, candidate digest and checkpoint time; finite shared
deadlines (default at most five seconds). Witness framing, authentication,
counter-signatures and server internals belong to the witness owner, not this doc.
Do not reuse the key-service purpose-3 record or registrar record for a new stream.

The registry writer has **no witness append credential or append permission**.
A separate witness-side consumer/bridge account validates the protected registry
stream and exact candidate, then coordinates intent/resolution under its own ACL;
it cannot write key-service or registrar consumer streams. This required boundary
is Q10, not an implemented proxy API. A writer-provided digest alone is insufficient
to resolve a transition; independent validation and old-instance exclusion matter.

Required ordering: quiesce publication; validate candidate; obtain acknowledged
external intent before any authoritative local commit; durably commit exact state;
obtain acknowledged matching resolution; revalidate bytes/time/authority; publish
and acknowledge. A failed intent fences old authority even with no local commit.
Private candidate staging grants no authority and cannot change the served ref.
Unknown append outcome requires a fresh latest read; an actual unresolved intent
requires privileged exact-candidate recovery. Never auto-cancel or downgrade.

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
configuration, durable outcomes and transaction evidence, with retention covering
all acknowledged state. Recovery keys stay with the owner. Backups and independent
trusted checkpoint history have different jobs: a backup alone proves no freshness.

Before startup/restore, exclude old instances for the whole serving lifetime,
verify pins/custody, establish time continuity, validate all local state and request
a fresh challenge-bound authoritative witness head. Match exact state/head and
prove continuity from off-node evidence. Missing files, empty stream, lower head,
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
| Forged signer, cross-role key or wrong scope | S1 strict signatures, D30 and protected pins; compromised original roots remain trusted failures. |
| Replay, equivocation, rollback or clone | Exact-byte binding, durable ordering, witness continuity and external checkpoints; single-node compromise defeats local independence. |
| Partial write, race or lost acknowledgement | Serialized intent/commit/resolution, durable outcomes and exclusion; real filesystem/crash qualification still required. |
| Withheld record or stale distribution | At-most-60-second age after issuance, final online checks where required; no bound before first honest signed publication. |
| Malicious submitter or repository | Closed schemas, bounded transport, no hooks or path interpretation; signed prose stays data, not instructions. |
| Resource exhaustion or missing dependencies | Finite capacity/deadlines and fail closed; deliberate denial of service remains possible. |
| Metadata disclosure | Read ACLs, minimal diagnostics, encrypted backup; hashes/signatures provide integrity, not confidentiality. |
| Split snapshot histories from a compromised issuer | Previous-hash/number continuity, consumer-held heads, published checkpoints and independent witness/head comparison with operator alerts; a co-located compromised witness or partition can hide forks until external views meet. No pre-publication deadline or honest-issuer proof follows from a fresh signature. |

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

Placement, repository location, single writer, root separation, E1 and registrar Git consumption are settled.
Q1 records the repository decision; Q2–Q12 remain open. Their recommendations do
not silently amend any existing specification.

| Question | Options and recommendation |
| --- | --- |
| Q1: repository location | Decided, 2026-10-24: a separate public service repository, this one; the task board stays private. |
| Q2: format/profile alignment | Publish signed journal/checkpoint and aggregate snapshot profiles, or reuse existing issuer snapshot payloads initially. Recommend reusing existing `revocation/1` payloads first; usable registry publication still waits for reviewed chained metadata and witness reconciliation, and journal activation waits for the format slice; D32 currently assigns no general snapshot tag. K's purpose-5 preimage also differs from F: align through its owner, never accept both domains as aliases. |
| Q3: custody and refresh | Protected software file, isolated signer, hardware, or external signer. Recommend an isolated closed-profile registry signer as the software baseline; the owner chooses extraction/availability tradeoffs and recovery custody. External issuer owners must also confirm their custody/refresh path before their domains activate. |
| Q4: API and admission budget | mTLS service endpoint or owner-local socket; proposed limits above or measured stricter ones. Recommend mTLS for authenticated publishers plus restricted local administration; freeze schema, error precedence and resource tests before API code. |
| Q5: draft filenames and history | Twelve-digit fixed sequence width or a representation covering safe integers. Recommend format-owner clarification covering the full range, no truncation/wrap, while preserving R's exact enrollment path and E1 rotation exception. |
| Q6: Git dependency and approval | Fixed system Git executable or a new Go Git dependency. Recommend a protected bounded system Git adapter to retain the stated dependency policy, with qualified durability and distinct maintainer signing; operator approval remains external. |
| Q7: revocation completion | Durable acknowledgement set or complete authority-expiry proof, with consumer-selected online critical-effect checks. Recommend explicit barriers and a published consumer set before claiming completion. BP is unmerged; broker migration must preserve its local restore fence and authority until reviewed activation. |
| Q8: cumulative snapshot capacity | Pages with a shared signed head, a Merkle root in the snapshot, or a narrower domain. The format owner decides the normative policy. Recommend its review before activation; meanwhile refuse/fence at the current size/count limit, signal the operator and never drop entries. |
| Q9: checkpoint cadence and heartbeat | Per-entry or batched checkpoints, and heartbeat intervals below the 60-second maximum age. Recommend per-entry checkpoints and 20-second snapshot reissue, recording every reissue as a new `add` so its checkpoint path remains immutable; qualify sustained sync/signing load. No owner-root-change op in v0; D31 recovery stays separate. |
| Q10: witness coordination and exclusion | A separate authorized bridge or witness-owned observation/coordinator; distributed exclusion needs its own qualification. Recommend witness-owned coordination with no registry append capability, exact-candidate checks and off-node checkpoint retention. The witness owner publishes the server interface; local locks alone are insufficient. |
| Q11: time activation | Provider import alone or qualified durable watermark/hold continuity plus dedicated-service authdata. Recommend the latter, zero backward tolerance and explicit recovery; missing independent history fences startup. |
| Q12: publication continuity | Consumer head gossip, witness-mediated comparison or an independent off-node head observer. Recommend a reviewed chained-publication profile plus independently retained/compared heads; preserve settled witness co-location, make full-node and partition limits explicit, and do not reuse paused registry-to-registry exchange. |

## 11. Implementation slices and evidence plan

Each slice starts with executable negative cases and nearby positive controls
bound to the approved contract. Names below are planned tests, not passing tests.
No production activation follows from mocks or two implementations agreeing.

| Order / gate | Negative tests first | Positive controls and scope |
| --- | --- | --- |
| 1. Publish dependencies | `TestUnpublishedProfileRefused`, `TestCrossRoleRootRefused`, `TestForeignPurpose5DomainRefused` | `TestPinnedS1ProfileAccepted`; reviewed S1, format/custody/API decisions before dependent code. |
| 2. Pure intake adapter | `TestWrongIssuerScopeRefused`, `TestInvalidSignatureBeforeConflict`, `TestDuplicateCCJMemberRefused`, `TestOversizeNoMutation`, `TestUnknownKeyClassRefused` | `TestAuthorizedSignedRecordAccepted`; original bytes and S1 refusal precedence, no effect execution. |
| 3. Serialized durable storage | `TestChangedBytesSameIDRefused`, `TestConcurrentHeadConflict`, `TestSyncFailureFences`, `TestSequenceExhaustionNoWrap` | `TestExactRetrySameOutcome`, `TestCrashReconcilesExactCommit`; isolated crash fixtures at every durability boundary. |
| 4. Enrollment projection | `TestInvalidMirrorFencesWholeLoad`, `TestUnapprovedCommitRefused`, `TestTombstoneHistoryCannotDisappear`, `TestReservedSigsWinsSnapshotWide` | `TestOfflineEnrollmentSignedCommitAccepted`; reuse P and R unchanged, approved E1 rotation retained. |
| 5. Witness integration | `TestWriterCannotAppendWitness`, `TestStaleChallengeRefused`, `TestUnresolvedIntentFences`, `TestRestoredCloneCannotServe` | `TestExactIntentCommitResolution`, `TestLostResolutionReceiptFreshHead`; real ACL/durability/exclusion qualification, not mock-only evidence. |
| 6. Snapshot and clock publication | `TestStaleSnapshotRefused`, `TestEqualIssueTimeDifferentBytesRefused`, `TestCumulativeRemovalRefused`, `TestGapRefused`, `TestSplitHeadSignalsAndRefuses`, `TestSnapshotCapacitySignalsWithoutDrop`, `TestTimeLossNoPublication` | `TestFreshCompleteSnapshotAccepted`, `TestHeartbeatAdvancesJournal`, `TestConsumerAgeBoundary`, `TestMatchingGossipHeadAccepted`; qualified provider and reviewed capacity/continuity profiles. |
| 7. Restore and consumer activation | `TestRestoredBytesCannotBootstrap`, `TestJointLossFences`, `TestStopWorksWithoutRegistry`, `TestNoCompletionOnIntakeAck` | `TestOffNodeExactCandidateRecovery`, `TestReviewedConsumerMigration`; consumer-owned barriers, emergency stop and independent recovery evidence. |

Content-review acceptance of this design precedes code. Later implementation evidence
must distinguish unit coverage, negative controls, crash/ACL qualification and
consumer activation; existing services and paused formats remain separate work.
