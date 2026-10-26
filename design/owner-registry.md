# Owner registry service design

Review draft, 2026-10-25, reconciled to merged S4 D38–D41, S5 receipts and
the merged qualified-time contract at T. Time adapter/backend qualification and
activation remain separate gates.
The pinned public S4 record and S5 receipt contracts are normative for this
design. Content approval, repository landing, consumer integration and activation are separate;
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
| D | [D1–D41](https://github.com/relux-works/waggle/blob/f76a400ebeb599b95a9bf9b2c59b522bef0392d8/UNRESOLVED_QUESTIONS.md), especially D30 and D38–D41: separate freshness, required journal prior records, bounded freshness admission and explicit re-anchor. Design decisions are recorded in §10; merged receipt composition is pinned separately as S5 below. |
| K | [Key service](https://github.com/relux-works/swarma-keymaker/blob/0ae0c4e93c3fae9db2bcadcfb5eeafbdcf0668c0/spec/keymaker.md), §§4.3, 5, 8–9: custody, pins, enrollment and purpose 5. |
| W | [Witness consumer](https://github.com/relux-works/swarma-keymaker/blob/5eb89870d25cd9c4dd9e71180d9449f18fba206f/spec/keymaker-witness.md), §§3.3–6, 8: operations, intent ordering, recovery and WC1. |
| B | [Broker](https://github.com/relux-works/swarma-credential-broker/blob/f2c01f52477b5885aefd5b6bd04d09a6a22813b2/spec/broker.md), §§6.4–6.6: local authority and the registry boundary. |
| T | [Time policy](https://github.com/relux-works/swarma-credential-broker/blob/7294b7aea68a8e0757437075efa3a6e318a18e51/docs/time-policy.md), [qualified consumer contract](https://github.com/relux-works/swarma-credential-broker/blob/7294b7aea68a8e0757437075efa3a6e318a18e51/docs/time-contract.md), [handoff contract](https://github.com/relux-works/swarma-credential-broker/blob/7294b7aea68a8e0757437075efa3a6e318a18e51/docs/time-handoff.md) and [window-profile schema](https://github.com/relux-works/swarma-credential-broker/blob/7294b7aea68a8e0757437075efa3a6e318a18e51/docs/time-window-profile.md), exact revision `7294b7aea68a8e0757437075efa3a6e318a18e51` (`7294b7a`). Published documentation contracts; receipt authentication, independent continuity and bounded consumption implementation remain activation gates. |
| R | [Registrar](https://github.com/relux-works/swarma-registrar/blob/2219f70358fb7b5c7d66f229c80a8ba4a1fe0230/spec/registrar.md), §§4.2–4.5 and Appendix C.3. |
| P | Public [ccj](https://github.com/relux-works/swarma-registrar/tree/2219f70358fb7b5c7d66f229c80a8ba4a1fe0230/ccj), [enrollment](https://github.com/relux-works/swarma-registrar/tree/2219f70358fb7b5c7d66f229c80a8ba4a1fe0230/enrollment) and [registry](https://github.com/relux-works/swarma-registrar/tree/2219f70358fb7b5c7d66f229c80a8ba4a1fe0230/registry) packages. |
| WS | [Witness server proposal](https://github.com/relux-works/swarma-keymaker/blob/e7c5e1850c29d1f931de3faa24ad39d94da41ada/spec/witness-service.md), PR #20: exact server/wrapper, challenged reads, strict custodian and limits; spec only, not deployed or qualified. |
| WC | Witness libraries at `c18d70040d11a6a03078df1e61c1bf3aa0dae85a`: [policy/binding.go](https://github.com/relux-works/swarma-witness/blob/c18d70040d11a6a03078df1e61c1bf3aa0dae85a/policy/binding.go), [spec/policy-v1.md](https://github.com/relux-works/swarma-witness/blob/c18d70040d11a6a03078df1e61c1bf3aa0dae85a/spec/policy-v1.md), [spec/wire-v1.md](https://github.com/relux-works/swarma-witness/blob/c18d70040d11a6a03078df1e61c1bf3aa0dae85a/spec/wire-v1.md) and [UNRESOLVED_QUESTIONS.md](https://github.com/relux-works/swarma-witness/blob/c18d70040d11a6a03078df1e61c1bf3aa0dae85a/UNRESOLVED_QUESTIONS.md). Pure contracts; serving remains closed (`spec/policy-v1.md:19–31`). |
| OR | Registry baseline for bridge citations: [design/owner-registry.md](https://github.com/relux-works/swarma-owner-registry/blob/018c21624b0c8a2adbbf823c4e2059bc3cb264a2/design/owner-registry.md) at `018c21624b0c8a2adbbf823c4e2059bc3cb264a2`. |
| V | S4 public [record](https://github.com/relux-works/waggle/tree/f76a400ebeb599b95a9bf9b2c59b522bef0392d8/record), [verifier](https://github.com/relux-works/waggle/tree/f76a400ebeb599b95a9bf9b2c59b522bef0392d8/verifier) and [fixtures](https://github.com/relux-works/waggle/tree/f76a400ebeb599b95a9bf9b2c59b522bef0392d8/vectors/swarm-envelope-2): pure admission, not I/O or activation. |
| S5 | [Receipt contract](https://github.com/relux-works/waggle/blob/5f4e7435d75697385ff6ce15a7501ac6f31c8ea5/spec/swarm-envelope-2.md), §§2.10, 6–7; [D41 closed / D42 open](https://github.com/relux-works/waggle/blob/5f4e7435d75697385ff6ce15a7501ac6f31c8ea5/UNRESOLVED_QUESTIONS.md); [verifier API classification](https://github.com/relux-works/waggle/blob/5f4e7435d75697385ff6ce15a7501ac6f31c8ea5/verifier/README.md), [receipt](https://github.com/relux-works/waggle/blob/5f4e7435d75697385ff6ce15a7501ac6f31c8ea5/verifier/receipt.go) and [service validation](https://github.com/relux-works/waggle/blob/5f4e7435d75697385ff6ce15a7501ac6f31c8ea5/verifier/service.go). Merged on 2026-10-25; design source, not the current module pin. |

S4 at `f76a400ebeb599b95a9bf9b2c59b522bef0392d8` is merged and
adds freshness/1, D39 journal narrowing, D40 state/read bounds and D41 explicit
re-anchor to S3's journal/checkpoint contracts. F §§2.7, 2.9, 6–7 and D38–D41
are the public sources for the requirements below. Content approval and landing
do not activate this service or qualify consumers. The code module is now pinned
to S5 `5f4e7435d75697385ff6ce15a7501ac6f31c8ea5` by the dependency/profile
re-pin (§11, row 1b). Receipt composition in §5.3 still requires its separate
intake adapter and consumer qualification; the re-pin supplies neither receipt
consumption nor an activation gate.

Non-goals: message delivery, credential/account issuance, operator-act execution,
new verifier implementation, root recovery, contact records, registry exchange
or a registrar snapshot API. Broker PR #13 is merged in B, whose §6.5 retains
v0 local authority until separately reviewed migration. The registrar continues
to consume only R's pinned signed commit.

Bridge source citations below use `OR`, `WS` and `WC` with file:line ranges
at those exact revisions. They distinguish proposed consumer requirements from
implemented witness-local checks; they do not qualify an adapter or reopen
serving (OR:design/owner-registry.md:749–764,964–978;
WC:spec/policy-v1.md:3–6,27–31).

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

The registry bridge must use the **Bridge** role, never Observer, for its bound
checkpoint stream. Provision its exact principal and transport pin both for
append and explicitly in that stream's readers; append membership grants no
read membership. An Observer's explicit read permission does not make it a
registry bridge. S2 checks only witness-local principal/pin/role membership:
separately qualify writer credential exclusion, the bridge's lack of content
authority, and custodian account/administrative isolation before activation.
G07/G10 remain closed; this design grants no serving or disclosure approval
(OR:design/owner-registry.md:74–76,782–788;
WC:policy/binding.go:25–51,100–121; WC:spec/policy-v1.md:19–31;
WS:spec/witness-service.md:100–117).

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
For registry-domain currentness, successful `VerifyFreshness` or explicit
`VerifyFreshnessReanchor` supplies `Result.FreshnessReceipt` under merged S5.
After atomically persisting its admitted anchor, watermark and any re-anchor
audit, §5.3's adapter supplies `Context.FreshnessReceipt`,
`Context.RequireFreshnessReceipt` and stricter-only
`Context.FreshnessMarginSeconds` with the
same whole `SignedSnapshots` Evidence and genuine current facts. Intake consumes
registry-, operator- and grant-root snapshots: only the receipt-bound registry
snapshot can replace its own-age check. The current receipt-free intake retains
the legacy 60-second own-age checks and rebuilds Evidence per admission; it
still refuses an aged idle registry snapshot until the row 7b retaining adapter
is implemented and qualified. Entry-point classes and the
separate new-snapshot submission path are specified in §5.3. Grant/1, D33
service-key and device intake wait for the format owner's D42(a) multi-snapshot
freshness change and its pin (§5.3, §10 Q17, row 7c). Until then, their operator-
and grant-root snapshots retain their own 60-second age checks; a registry
receipt cannot extend them (D32/S5,
[S5:spec/swarm-envelope-2.md:998–1007](https://github.com/relux-works/waggle/blob/5f4e7435d75697385ff6ce15a7501ac6f31c8ea5/spec/swarm-envelope-2.md#L998-L1007);
[design baseline:190–195](https://github.com/relux-works/swarma-owner-registry/blob/fa6e1ef2e02138a7c935672ec2436709dbc96cf6/design/owner-registry.md#L190-L195)).
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
base older than 60 seconds therefore needs the merged S5 receipt composition for
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
The consumer-local completion measurement, original uncertainty and retained
read evidence must satisfy §6.2 before §9's conservative projection. WC1's
archive does not protect registry FreshnessAnchor/history or C/V/F use state;
their independent protection remains Q18 (§6.2), not an archive inference
(OR:design/owner-registry.md:386–420,924–946,1188–1209;
WS:spec/witness-service.md:475–504; WC:UNRESOLVED_QUESTIONS.md:21–25).
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

With the reviewed S5 module pin in place, call `VerifyFreshness(chain,checkpoint,ids,keys)`
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

#### Merged S5 receipt composition

S5 §§2.10 and 6 at the pinned `5f4e7435d75697385ff6ce15a7501ac6f31c8ea5`
provide the API before Keymaker K3 activation and the broker D12 adapter.
Row 1b now pins that API; receipt composition remains separate adapter work.
Retain `TestFreshnessCompositionBoundary` and its current-snapshot control for
legacy/no-receipt behavior. Never substitute younger bytes/time, a weakened
Context or another verifier for receipt composition.

Only successful full `VerifyFreshness` or `VerifyFreshnessReanchor` returns
`Result.FreshnessReceipt`, an opaque immutable original pointer, alongside the
candidate Plan. Refusals return a zero Plan and nil receipt. It binds the exact
registry root/owner, snapshot bytes/id, checkpoint, freshness tip and Evidence
premise. It has no constructor, accepted `Present` input or persistence/JSON
format; zero, copied and reflection-created values are invalid. Original-pointer
sharing is supported. `receipt.Audit()` returns detached origin ids and recovery
endpoints; its bool establishes origin only, never current validity or authority.
It grants neither Commit nor effects. No receipt survives restart.

Expiry is fixed at mint, with protected `max_age` at most 60 seconds:
`E = min(tip.issued_at, issuing WitnessReadAt) + max_age`.
Use requires `max(now, W) < E - Context.FreshnessMarginSeconds` (exclusive).
`FreshnessMarginSeconds` is an integer 0–60, default 0; it only tightens.
Newer reads never extend E. An F success at its inclusive age boundary can mint
an already unusable receipt. Existing trusted-time, watermark, non-future,
snapshot signature/schema/completeness, cumulative/rollback-safe qualification
and effective-revocation checks remain mandatory.

For receipt consumers and validate-only calls, attach the original pointer as
`Context.FreshnessReceipt`. `Context.RequireFreshnessReceipt=true` refuses absence;
nil plus false deliberately selects legacy own-age behavior. A supplied receipt
always selects receipt mode even if legacy own-age would pass. A receipt refusal
never retries without the receipt. R runs after the entry point's existing
trusted-time/watermark gate and before validity/admission continuation; earlier
envelope/signed-evidence failures retain precedence. Preserve these typed codes
through K1/G7/O2, with zero identity/Unavailable and exact errors where applicable:

| Order | Receipt check | Refusal |
| --- | --- | --- |
| R1 | Required presence, authentic original pointer, margin 0–60 | `freshness_receipt_unavailable` for required absence; `freshness_receipt_invalid` for invalid provenance or margin |
| R2 | All required current binding/read presence first, then syntax, then exact whole-Evidence/root/owner/snapshot/witness/admitted-anchor/read consistency | Missing: `freshness_receipt_unavailable`; malformed/future read: `freshness_receipt_invalid`; foreign binding, earlier read or rebuilt Evidence: `freshness_receipt_binding_mismatch` |
| R3 | Exclusive fixed expiry `max(now,W) < E-margin` | `freshness_receipt_expired` |

The row 7b intake adapter must retain **one entire immutable `SignedSnapshots`
Evidence for the receipt's lifetime**, not just the registry row's bytes/id.
First verify the full normal/re-anchor producer proof. Atomically persist its
Plan's **admitted tip `FreshnessAnchor`**, watermark and any re-anchor audit
before use; never pass the old mint anchor to a receipt consumer. Then supply
the receipt and that admitted anchor/watermark with the same whole Evidence,
the current authenticated `JournalWitness`, `WitnessReadAt`, and genuine
`Now`/time-confidence facts. The read must be well-formed, non-future at
`min(now,W)` and no earlier than the issuing read, with exact bound witness
registry/owner/checkpoint/sequence/head. A new immutable `Verifier` with updated
genuine clock facts and the same Evidence is allowed; this neither remints nor
extends the receipt. Repeat current checks at the serialized effect boundary.

Rebuild Evidence and re-run F to mint a new receipt after any snapshot or
cumulative/rollback-safe premise change, including **other rows of the whole
Evidence map** and changes later returning to the same ids/booleans. Re-anchor
and restart also require new full F admission and a newly minted receipt. `complete=true` alone never proves cumulative history. After restart
there is no receipt until successful full F verification; witness unavailability
refuses receipt-backed intake. Today's `internal/intake` calls `Present` to
rebuild snapshots on every admission. Retaining/reusing the Evidence instance
is planned row 7b work, not behavior supplied by attaching a field today.

Choose the operation's evidence path up front. A newly submitted `revocation/1`,
including a replacement registry snapshot or an operator-/grant-root snapshot,
is validated through the existing **no-receipt own-age submission path** using
its exact submitted bytes and issuer metadata. A new receipt is obtained only
after that snapshot's committed head is freshness-verified. An old receipt
cannot authenticate replacement Evidence. Do not attach a registry receipt to
`CheckRevocation` for an operator or grant root: its explicit root must equal
the receipt root. Neither submission nor authority paths may retry without a
receipt after a receipt refusal.

The S5 entry-point classes and our intake mapping are:

| Class | Entry points | Registry intake mapping / age boundary |
| --- | --- | --- |
| May extend only the bound registry-root snapshot's own-age | `VerifyAttestation` (agent), `VerifyMessage`, `Authorize`, `CheckRevocation`, `VerifyEnvelope` where applicable | Agent attestation intake calls `VerifyAttestation`; grant-chain intake calls `Authorize` and keeps all other-domain ages; receipt-backed current registry-root checks call `CheckRevocation`. Newly submitted snapshots instead use the no-receipt path above. `VerifyMessage` and `VerifyEnvelope` have no direct current intake call. |
| Validate-only, R1–R3 required in receipt mode | `VerifyAttestation(device=true)`, `VerifyOperator`, `VerifyServiceAttestation` | Device intake calls `VerifyAttestation(device=true)`; service intake calls `VerifyServiceAttestation`, after its trusted-time gate and before service validity. Operator-/grant-root snapshot ages remain mandatory; service is grant-domain, not a registry-age extension. `VerifyOperator` has no direct current intake call. |
| Outside receipt mode by design | `VerifyJournal`, `VerifyFreshness`, `VerifyFreshnessReanchor` | The planned journal/freshness adapters run their full independent contracts and ignore all receipt-consumption fields, even invalid ones. Journal snapshot own-age remains; the F producers mint only on full success. None is a current record-intake call. |

S5's remaining exports perform no receipt-consuming admission: `New` stores
Context; `Evidence.IsAvailable` and `OperatorRecordCode` keep their context-free
helper contracts. `Present` and receipt `MarshalJSON`/`UnmarshalJSON` reject
receipt construction/persistence, while `Audit` reports origin only. The class
split accounts for every verification entry point; the `VerifyAttestation`
device flag intentionally selects two different classes.

D32 separation limits the default S5 scope to the registry-owner domain.
Operator-root and grant-root snapshots still need their own age at most 60 seconds;
a valid registry receipt cannot extend them. Grant/1, D33 service-key and device
intake activation therefore waits for the format owner's **D42(a) multi-snapshot
freshness change and its pin** (§10 Q17). It must provide a multi-snapshot
freshness format binding the registry-journaled external-root snapshots. The
format owner has announced versioned checkpoint/2 and freshness/2 records;
checkpoint/1 and freshness/1 stay unchanged, with no silent downgrade. The
proposal is not yet published. Our publisher must cover those snapshots, and
S5 consumers may accept a registry receipt for those roots only
through the format owner's specified rule. D42 remains the open format item;
this design invents no binding schema or consumer age-waiver rule. Until that
change is merged and pinned, current D32/S5 scope and external-root own-age
checks remain unchanged. The registry-owner retaining adapter (row 7b) can start
now; external-root currency is separate row 7c
([design baseline:617–621](https://github.com/relux-works/swarma-owner-registry/blob/fa6e1ef2e02138a7c935672ec2436709dbc96cf6/design/owner-registry.md#L617-L621),
[S5 D42:205–228](https://github.com/relux-works/waggle/blob/5f4e7435d75697385ff6ce15a7501ac6f31c8ea5/UNRESOLVED_QUESTIONS.md#L205-L228),
[S5 scope:998–1007](https://github.com/relux-works/waggle/blob/5f4e7435d75697385ff6ce15a7501ac6f31c8ea5/spec/swarm-envelope-2.md#L998-L1007)).

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
Keymaker therefore depends on the merged S5 receipt path and separately
qualified consumer integration before K3 activation. Retained success is not
renewed authority or revocation completion.

### 5.6 Broker consumer composition

B §6.5 retains broker v0 local authority/restore fences until reviewed migration.
The broker D12 adapter depends on the merged S5 receipt path in §5.3 and
separately qualified integration, not just S4/S5 landing or this registry's
publication implementation. It must combine that bounded current registry evidence with all original grant/operator/domain,
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
own decision; this registry adapter does not depend on its outcome
(OR:design/owner-registry.md:749–758;
WS:spec/witness-service.md:218–243,318–337,483–504;
WC:UNRESOLVED_QUESTIONS.md:25,28,38).

**Open consumer-local read contract (slice 5b).** `WitnessReadAt` must come from
the protected adapter's qualified read-completion measurement of the **exact
authenticated complete read**. Retain the original measurement, uncertainty,
clock/boot and qualification identity and conversion provenance; apply §9's
conservative projection and §5's original and converted age predicates. Neither
WC1's closed head/receipt fields nor TLS time, checkpoint_time, server audit
times, an old retry receipt or an unrelated clock sample supply this fact.
Missing or unqualified completion evidence refuses `freshness_unavailable`
without advancing freshness authority; there is no substitute time source
(OR:design/owner-registry.md:386–394,438–444,760–764,924–946;
WS:spec/witness-service.md:267–282,598–604;
WC:spec/wire-v1.md:88–100; WC:UNRESOLVED_QUESTIONS.md:24,30).

The adapter must own and retain the request/challenge, service/stream and trusted
record/control anchors; exact authenticated signed head and original receipt;
all required record, control and immediate-intent proof pages; the eight-field
projection and original checkpoint/candidate inventory; exact C completion/use
binding; and the associated completion measurement/uncertainty above. Verify
scope, challenge, signatures, complete chains, pairing and exact head before
producing a bounded owned verified-read result. Equal anchors still require
explicit current intent evidence. Missing pages, conflicting anchors, absent
commit binding or exceeded proof limits fence authority: no truncated prefix,
hash-only currentness or automatic offline fallback. A fresh read is not a lease;
V must recheck the exact C/head under its final serialization
(OR:design/owner-registry.md:766–788,1078–1080,1188–1198;
WS:spec/witness-service.md:218–243,284–309;
WC:spec/wire-v1.md:88–100,113–117; WC:UNRESOLVED_QUESTIONS.md:30,33–34).

**Independent evidence from both witnesses (unqualified bridge design).** Call
the first witness W1 and the confirming custodian W2. Verify each one's original
evidence against separately protected service/provider identities, signer pins,
epochs and historical authorization. These custody references use WC1 at
`29df43b7e8b26d1bce7101c5fc73e8c9b8050b99` for design inspection, without
changing the module pin. Retain exact W1 wrappers, original receipts
and complete record/control/intent proofs, W2 original confirmations and fresh
read evidence, request challenges and protected complete-read measurements.
A W1 report or relay of W2 state is never custodian evidence by itself. If W1
carries W2's original signed evidence, independently verify that original, its
exact transition/view and a challenge bound to the bridge's fresh request.
Use a bridge-generated unpredictable challenge or a separately accepted,
purpose-separated cryptographic derivation; a challenge chosen solely by W1
cannot establish freshness independently of W1. Record and current-control
proofs need an accepted coherent-view and race/refusal rule. A direct W2 fetch
costs another budgeted exchange. These delivery/challenge/composition bindings
are still unspecified by WC1's opaque evidence interface; define them before
bridge activation, without adding ad hoc members to closed WS responses
([WS consumer verification:218–243](https://github.com/relux-works/swarma-keymaker/blob/e7c5e1850c29d1f931de3faa24ad39d94da41ada/spec/witness-service.md#L218-L243),
[WS independent custody:483–504](https://github.com/relux-works/swarma-keymaker/blob/e7c5e1850c29d1f931de3faa24ad39d94da41ada/spec/witness-service.md#L483-L504),
[WC1 evidence contract:18–35](https://github.com/relux-works/swarma-witness/blob/29df43b7e8b26d1bce7101c5fc73e8c9b8050b99/custodian/contract.go#L18-L35)).

A second process or replicated store is insufficient. The proposed custodian
qualification must establish all of the following; this list does not supply a
wire protocol or accepting implementation:

- Separate administration and independently protected signing keys/epochs,
  including authorized historical pins; no shared or derived W1 keys or shared
  cloud, administrative, recovery or API credentials. Pinning a transport peer
  alone does not establish independent custody
  ([WS roles:100–106](https://github.com/relux-works/swarma-keymaker/blob/e7c5e1850c29d1f931de3faa24ad39d94da41ada/spec/witness-service.md#L100-L106),
  [WS failure domain:483–493](https://github.com/relux-works/swarma-keymaker/blob/e7c5e1850c29d1f931de3faa24ad39d94da41ada/spec/witness-service.md#L483-L493)).
- A recovery domain and retained originals/anchors outside W1's restore and
  administrative control; independent verification of authorized submissions
  against W2's retained predecessor, not replication, a writer digest or W1's
  signature alone. Original-evidence delivery, identity/epoch codecs and lost-key
  continuity are open WC1 provider work; Transition currently carries tuples and
  a transaction string, not a complete verification bundle
  ([WS retention/restore:475–514](https://github.com/relux-works/swarma-keymaker/blob/e7c5e1850c29d1f931de3faa24ad39d94da41ada/spec/witness-service.md#L475-L514),
  [WC1 provider shape:11–35](https://github.com/relux-works/swarma-witness/blob/29df43b7e8b26d1bce7101c5fc73e8c9b8050b99/custodian/contract.go#L11-L35)).
- Atomic durable exact compare-previous confirmation over the complete custody
  tuple and original transaction, plus fresh challenged authoritative record and
  separate current-control reads. Same-position differences refuse; exact repeats
  retain the original confirmation. Genesis and migration require external
  authorization and protected continuity, never empty-store first use or a reset
  after restore. Exact provider envelopes, limits and recovery remain open
  ([WS append/genesis:183–214](https://github.com/relux-works/swarma-keymaker/blob/e7c5e1850c29d1f931de3faa24ad39d94da41ada/spec/witness-service.md#L183-L214),
  [WS custody/restore:483–514](https://github.com/relux-works/swarma-keymaker/blob/e7c5e1850c29d1f931de3faa24ad39d94da41ada/spec/witness-service.md#L483-L514),
  [WC1 open boundaries:165–175](https://github.com/relux-works/swarma-witness/blob/29df43b7e8b26d1bce7101c5fc73e8c9b8050b99/spec/storage-v1.md#L165-L175)).
- G18 fencing for each W1/W2 and registry writer/bridge/publisher instance through
  actual write, signature and release, including prepared replies after pause,
  partition or revocation. Confirmation CAS, heartbeat and flock cannot substitute.
  Qualify a finite acyclic dependency graph ending in independently trusted
  nonrollback custody; W1 confirming W2 while W2 confirms W1 supplies no such base
  ([WC1 G18:26](https://github.com/relux-works/swarma-witness/blob/29df43b7e8b26d1bce7101c5fc73e8c9b8050b99/UNRESOLVED_QUESTIONS.md#L26),
  [WS trusted dependency:490–493](https://github.com/relux-works/swarma-keymaker/blob/e7c5e1850c29d1f931de3faa24ad39d94da41ada/spec/witness-service.md#L490-L493)).

Verify every custody tuple field and independent record/control anchor before
deriving the closed JournalWitness projection below. W2 does not attest registry
content semantics or C's physical durability for the bridge, nor does either
witness's tuple cover later V/F or Q18 state without its separately approved
namespace/protocol ([WS durability boundary:495–514](https://github.com/relux-works/swarma-keymaker/blob/e7c5e1850c29d1f931de3faa24ad39d94da41ada/spec/witness-service.md#L495-L514),
[Q18](#914-versioned-use-transition-and-frozen-content-decision)).

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
retain both verified originals and signal. A lower candidate is rollback
(OR:design/owner-registry.md:766–780;
WS:spec/witness-service.md:334–367,380–411;
WC:spec/wire-v1.md:113–117; WC:UNRESOLVED_QUESTIONS.md:32–34).

Pre-intent genesis and historical-checkpoint verification remain external WC1
G31 qualification dependencies. Checkpoint wrapper decoding is not verification;
never fabricate JournalWitness facts to satisfy a pre-intent verification
dependency (OR:design/owner-registry.md:766–780;
WC:UNRESOLVED_QUESTIONS.md:39; WC:spec/wire-v1.md:113–117).

The registry writer has **no witness append credential or append permission**.
The separate witness-side bridge validates complete recoverable candidate
inventory, not a writer digest, and appends only its provisioned registry stream.
It cannot write other consumer streams. Reserve room for both positions;
no second intent while one is unresolved. The bridge independently verifies
exact durable local commit before matching resolution; the witness alone
cannot prove durability/exclusion
(OR:design/owner-registry.md:782–788;
WS:spec/witness-service.md:334–349,369–389;
WC:policy/binding.go:100–107; WC:UNRESOLVED_QUESTIONS.md:33).

Append retry means resending the byte-for-byte identical **complete wrapper**
against its original predecessor: return the original receipt, without another
position, replacement receipt or re-signing. Changed occupied bytes refuse
`position_conflict`; a wrong predecessor refuses `head_conflict` (checkpoint
fork detection retains its prior precedence). Current authorization, complete
durability barriers and the separately durable retry-lookup audit still apply;
failure cannot be bypassed to release a receipt. That old exact-retry receipt is
historical outcome evidence, **never a fresh resolution read**
(OR:design/owner-registry.md:782–788,819–822;
WS:spec/witness-service.md:181–214,610–616;
WC:UNRESOLVED_QUESTIONS.md:21–23,27).

For each real accepted entry, first quiesce publication, exclude older instances
and validate the exact candidate, both reserved witness positions and complete
proof budget. Then perform this single ordered composition; all C/V windows
remain inactive pending §9.1/Q18 qualification
(OR:design/owner-registry.md:790–822,1078–1080,1188–1210;
WS:spec/witness-service.md:197–214,340–404,506–514;
WC:UNRESOLVED_QUESTIONS.md:26–28,33–34):

1. Obtain the acknowledged **exact witness intent**, retaining its original
   wrapper/predecessor/receipt before any authoritative registry commit.
2. Obtain a **fresh registry C handoff** and atomically commit the frozen content,
   completion metadata, exact outcome/watermark and C-use transition together.
   C remains private; it grants no resolved visibility.
3. Have the independent bridge verify that **exact durable C commit**, including
   complete content inventory, completion/use revision, original intent and
   independent monotonic completion authority. A writer digest is insufficient.
4. Obtain the acknowledged **matching witness resolution plus a fresh, complete,
   challenged read** proving the exact resolution and current intent/head/control
   state. A successful retry lookup alone cannot discharge the read obligation.
5. Obtain a **fresh registry V handoff**, revalidate exact C, resolution, head,
   authority and predecessor under final serialization, and atomically commit
   visibility and V-use. Subsequent freshness follows its separate F window.

The registry and WC1 share **no H, reserve or use CAS**. Each owns its qualified
window and clock/deadline domain; no cross-host absolute monotonic comparison
is implied. Every remote call, including time collection, bridge verification,
append, audit/custodian work, proof pages, reconnects and retries, is capped by
the registry operation's **remaining original** at-most-five-second or stricter
budget. No phase renews it, and no final lock spans a remote call. WC1's relative
timeout fields alone cannot enforce an unseen whole registry attempt; qualify
caller-wide propagation/cancellation before activation. An expired caller cannot
receive a late V success; unknown outcomes stay fenced for exact recovery
(OR:design/owner-registry.md:796–822,1063–1074,1172–1186,1292–1298;
WS:spec/witness-service.md:239–243,504,591–593;
WC:spec/policy-v1.md:100–106; WC:UNRESOLVED_QUESTIONS.md:27–28).

Under WC1 G18, the owner decided (2026-10-25) that after a pause, network
partition or revocation an old writer, bridge or publisher instance or clone
cannot write, sign or release anything; heartbeat, flock and CAS are not fences,
including for §7 and §9.1.5
([existing exclusion:918–939](https://github.com/relux-works/swarma-owner-registry/blob/e2b06e6b0d08a50b68dde8c015e6e6a5bbdbb208/design/owner-registry.md#L918-L939),
[restore/use:1406–1413](https://github.com/relux-works/swarma-owner-registry/blob/e2b06e6b0d08a50b68dde8c015e6e6a5bbdbb208/design/owner-registry.md#L1406-L1413),
[WC1 G18:26](https://github.com/relux-works/swarma-witness/blob/29df43b7e8b26d1bce7101c5fc73e8c9b8050b99/UNRESOLVED_QUESTIONS.md#L26)).

Keep sole-writer serialization, publication quiescence and old-instance exclusion
across the intent/commit/resolution sequence, but perform qualified-time collection
and witness/custodian RPCs outside final store, journal and publication locks;
qualify the outer exclusion and dependency graph so they cannot indirectly block
emergency stop or introduce a provider callback cycle. After the preceding remote
phase, obtain a fresh operation-bound, durably acknowledged receipt before each
final local commit or publication window, reserve that window's complete bounded
handoff and local durability allowance within the receipt interval and uncertainty
cap, then enter once and revalidate the exact candidate/head, native operation
binding, challenge, boot, interval, authority and original monotonic deadline using
only bounded local work. Do not carry a cached passed verdict or reuse a receipt
across a witness call, a changed candidate, a retry or another final window. Release
final local locks before any further provider or witness call; all collection,
witness/custodian traffic, local durability and handoffs share the original
five-second decision budget or its stricter remaining deadline, without renewal.
Unknown latency, an unqualified handoff or insufficient remaining budget refuses.
This separation does not waive acknowledged intent before authoritative commit,
independently verified durable commit before matching resolution, resolution before
publication, or fencing and exact-candidate recovery after an uncertain outcome
(OR:design/owner-registry.md:796–814,1055–1080,1188–1209;
WS:spec/witness-service.md:369–378,495–504;
WC:UNRESOLVED_QUESTIONS.md:23,27–28).

One checkpoint per real entry, no batching or owner-root-change op in v0.
Freshness-only state follows §5.2, not this witnessed inventory; a real candidate
binds the freshness base/head transition and every publication dependency.
An intent or signed checkpoint alone is never publication success. Failed or
uncertain intent fences old authority even without local commit. Unknown
outcome requires fresh challenged discovery; unresolved intent requires approved
exact-candidate recovery, never automatic cancellation or silent downgrade
(OR:design/owner-registry.md:816–822;
WS:spec/witness-service.md:334–337,369–405,506–514;
WC:UNRESOLVED_QUESTIONS.md:27,38).

For G14/G15, the owner decided (2026-10-25) that archive and audit custody are
separate from the record custodian
([existing archive scope:952–968](https://github.com/relux-works/swarma-owner-registry/blob/e2b06e6b0d08a50b68dde8c015e6e6a5bbdbb208/design/owner-registry.md#L952-L968),
[WC1 G14/G15:22–23](https://github.com/relux-works/swarma-witness/blob/29df43b7e8b26d1bce7101c5fc73e8c9b8050b99/UNRESOLVED_QUESTIONS.md#L22-L23)).

**Proposed archive boundary; no implementation or activation.** WC1 archive
retention covers WC1 records, controls, original receipts and audit state only.
Registry FreshnessAnchor and full publication history, C/V/F completion/use
metadata and visibility outcomes require their own independently retained,
monotonic protection under Q18. A WC1 tuple or archive acknowledgment does not
prove that no later registry use/publication existed. The following are proposed
adapter capabilities, not existing WC1 wire methods
(OR:design/owner-registry.md:408–420,1188–1209;
WS:spec/witness-service.md:475–504,610–616;
WC:UNRESOLVED_QUESTIONS.md:21–23,30).

| Capability | Bridge boundary and required evidence |
| --- | --- |
| Retain | Bridge may submit exact wrappers, original receipts, head/proof/control and custody/transaction evidence to an independently authorized archive before relying on its retention acknowledgment; authenticate scope, bytes and durable coverage. It grants no registry freshness/use protection (OR:design/owner-registry.md:408–420,766–788,1188–1209; WS:spec/witness-service.md:475–504,610–616; WC:UNRESOLVED_QUESTIONS.md:21–23). |
| Retrieve | Bridge may retrieve exact historical originals during preparation/reconciliation or authorized offline work, subject to separate archive ACLs. Verify lengths, digests, signatures, scope, ancestry and anchors; retrieved history or an old head cannot substitute for the fresh challenged complete read (OR:design/owner-registry.md:766–788,819–822; WS:spec/witness-service.md:218–243,475–481; WC:UNRESOLVED_QUESTIONS.md:22,30). |
| VerifyOffline | Offline-only, separately authorized bounded continuity verification preserving the old trusted anchor. Never an automatic online `proof_limit` fallback or permission to serve an incomplete proof (OR:design/owner-registry.md:408–420,819–822; WS:spec/witness-service.md:235–243,506–514; WC:UNRESOLVED_QUESTIONS.md:22,30). |
| AdvanceAnchor | Offline-only explicit recovery operation: after complete continuity verification **and a new fresh challenged complete-to-head read**, atomically compare/advance the exact protected consumer anchor. Recover a lost acknowledgment as that exact transition; no reset, witness admin/append power or automatic jump. This exceptional offline operation adds no per-append owner approval to ordinary online verified advancement (OR:design/owner-registry.md:408–420,766–788,819–822; WS:spec/witness-service.md:235–243,506–514; WC:UNRESOLVED_QUESTIONS.md:22,27,30). |

**One custodian decision:** WC1 G17 and registry Q10 share the same custody
choice. For placement, the owner decided (2026-10-26) on a second witness on a
separate VPS. The prototype uses the same provider, but a separate project and
account with no shared API token. Platform deployment uses a different provider
and account. The prototype retains common-provider outage and administrative
failure risk; separate accounts do not remove that shared failure domain. This
records the new direction for the existing independent-custody requirement,
not a claim that the pinned WC1 text already selects a provider
([WC1 G17:25](https://github.com/relux-works/swarma-witness/blob/29df43b7e8b26d1bce7101c5fc73e8c9b8050b99/UNRESOLVED_QUESTIONS.md#L25),
[WS strict restore:483–523](https://github.com/relux-works/swarma-keymaker/blob/e7c5e1850c29d1f931de3faa24ad39d94da41ada/spec/witness-service.md#L483-L523)).

For custody, the owner decided (2026-10-26) on **option (a)**: WC1 plus one
synchronous independent G17 custodian for all consumers, with separately enforced
old-instance fencing. The weaker option was rejected. Exact predecessor
confirmation and fresh authoritative reads must fit the remaining original
operation budget; no on-node, periodic-only or absent-provider fallback is
permitted. Custodian confirmation is not an instance fence
([WC1 G17/G18:25–26](https://github.com/relux-works/swarma-witness/blob/29df43b7e8b26d1bce7101c5fc73e8c9b8050b99/UNRESOLVED_QUESTIONS.md#L25-L26),
[WS confirmation:483–504](https://github.com/relux-works/swarma-keymaker/blob/e7c5e1850c29d1f931de3faa24ad39d94da41ada/spec/witness-service.md#L483-L504)).

For activation, the owner decided (2026-10-26) that this is placement direction,
not a qualified provider, identity or pin set; deployment on the trust node is a
separate owner step. Concrete provider/SLA selection, typed confirmation/read and
original-evidence protocol, independent keys and epochs, recovery domain and
qualification remain open. Q18's option-(i) format boundary stays decided; its
residual completion/visibility/use authority remains tied to this custodian.
Registry namespaces, acknowledgments, retention, migration and recovery still
need explicit approval. Archive/audit custody remains separate. All activation,
archive/timing/custody qualification and G07/G10 serving gates stay closed
([Q18](#914-versioned-use-transition-and-frozen-content-decision),
[WC1 custody/archive/recovery gates:18–28](https://github.com/relux-works/swarma-witness/blob/29df43b7e8b26d1bce7101c5fc73e8c9b8050b99/UNRESOLVED_QUESTIONS.md#L18-L28),
[WS recovery:506–523](https://github.com/relux-works/swarma-keymaker/blob/e7c5e1850c29d1f931de3faa24ad39d94da41ada/spec/witness-service.md#L506-L523)).

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

Under WC1 G19, the owner decided (2026-10-25) on evidence-bound recovery only,
with old instances fenced; on ambiguity, hold and never recreate
([existing recovery:999–1019](https://github.com/relux-works/swarma-owner-registry/blob/e2b06e6b0d08a50b68dde8c015e6e6a5bbdbb208/design/owner-registry.md#L999-L1019),
[WC1 G19:27](https://github.com/relux-works/swarma-witness/blob/29df43b7e8b26d1bce7101c5fc73e8c9b8050b99/UNRESOLVED_QUESTIONS.md#L27)).

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

Import `github.com/relux-works/swarma-credential-broker/timeconf` at T's exact
reviewed revision and use its protected shared policy and stricter-only service
loader. Production embedding calls `CollectorEntry()` first in `main`.
`StartSample` collects fresh evidence but alone does not implement the qualified
consumer contract's receipt, durable continuity or bounded consumption API.
Those remain integration and activation gates. No local collector, clock
fallback, environment-selected source or copied uncertainty implementation is
permitted.

Retain the complete qualified provider receipt, including observed time `O`,
conservative uncertainty `δ`, interval `[L,U]=[O−δ,O+δ]`, exclusive authority `A`,
current boot, sample id, one-use challenge, generation and protected
authority/policy/adapter/scope bindings. Independently derive the enrolled native
profile's `operation_digest` from the original operation identity and canonical
argument digest; never copy the receipt's digest as the expected binding or
borrow the executor's Scope codec. A changed operation or retry requires new
bound evidence, not a new continuity history. The native profile and owning
codec require joint review before activation (T, native operation binding).
Shared ceilings remain sync age strictly below 2048 seconds, uncertainty at
most 1.5 seconds inclusive, and authority no later than
`min(O+300 seconds, reference+2048 seconds)`. Use the returned uncertainty,
including skew and the qualified remaining operation delay. Every acceptance,
issuance and final publication requires new bound evidence with `U < A`;
uncertainty can only tighten the consumer's validity and freshness predicates.

Distinguish the provider-owned consumed-time upper bound `W` from this registry's
admitted-plan watermark and retained decision upper-bound floor `J`.
`continuity.prior_high_water` is the provider floor before this consumption;
`continuity.high_water` is its newly consumed upper bound, and `journal_floor`
imports the registry's stricter previous decision upper bound, not the executor's
report-floor formula. Each new consumption requires `L > max(W,J)` against the
preceding checkpoint, then the provider durably commits `W=U` and its operation
association before returning its receipt, even if the registry later refuses or
never commits an operation. The provider owns every consumed bound, in-flight
observation and hold, including failed operations; admitted registry state alone
cannot reconstruct that history.

The registry also retains its required own durable decision floor `J`, at least
the previous decision's effective upper bound, and boot identity outside rollback
before acknowledging its decision (T, durable continuity). Neither history
replaces or resets the other. The registry's admitted-plan watermark still
follows F's min/max rules and atomic publication requirements. Do not map newly
consumed `continuity.high_water` directly to a point-time input's prior
`Watermark`. Snapshot and freshness inputs remain local integration projections,
not authenticated qualified receipts or complete receipt bindings.

The adapter preserves original nanosecond facts and checks their predicates.
Any coarser representation rounds lower interval bounds downward, upper bounds
and retained upper-bound floors upward, and exclusive endpoints downward; the
converted predicates must also pass. Conversion may add refusals but never turn
an original refusal into acceptance. Reject overflow, unrepresentable values or
an unusable converted interval. `O` alone cannot replace `[L,U]`. The complete
interval-aware mapping is a gate before any activation (§11, row 6a).

Registry projection. For every issuance and self-check the registry derives,
from one bound interval and its own retained state: `issued_at = floor(L)`;
verifier `Now = ceil(U)`; verifier `Watermark = max(W_prior, floor(L))`, where
`W_prior` is this registry's prior admitted-plan watermark, never the provider's
`prior_high_water` or `high_water`. F's `low = min(Now, Watermark)` and
`high = max(Now, Watermark)` then cover
`[max(W_prior, floor(L)), ceil(U)]`: the no-future check uses the low end and the
age check uses the high end. The decision also requires `ceil(U) < floor(A)`
and `floor(L) > J` with `J` rounded up, in addition to the nanosecond predicates
`U < A` and `L > max(W, J)`. After an acknowledged decision the registry retains
`J = ceil(U)` and admitted-plan watermark `max(W_prior, floor(L))`. `issued_at`
must also be strictly greater than the previous accepted `issued_at`; equality
refuses. With `δ ≥ 1` second a valid sequence never reaches that refusal, but
the check stays. A `W_prior` greater than `ceil(U)` refuses rather than moving
time backward.

A new process must recover the consumed bound, hold and any in-flight marker;
its new lower bound must be strictly after every retained previous `P+δP`.
A reboot preserves those obligations, requires current-boot evidence and never
compares elapsed values across boots. Waiting and resampling are allowed only
outside consumer exclusions; missing history, uncertain writes and holds cannot
be repaired by waiting or by a healthy report. Q11 retains zero backward
tolerance and explicit recovery. Dedicated-service authdata, independent store
and witness restore exclusions, full receipt validation and bounded delivery
remain qualification gates. Use §6.2's final local-window ordering and original
five-second budget: collection and RPCs stay outside final local locks, and each
window requires a new operation-bound acknowledged receipt and qualified bounded
handoff, without deadline renewal. Time failure never blocks the independent
emergency stop, directly or through a lock holder waiting on time.

### 9.1 Qualified-time handoff windows

Status: **consumer design; all three windows inactive on Linux and macOS**.
QT-2c uses a keyless retained-original anchor and private consumer commitment
within one trusted process on one host, option (d). A caller digest, receipt
decoder or copied expected tuple cannot establish that anchor. Crossing this
trust boundary requires another reviewed profile. No secret or signing purpose
is selected for handoff authentication. Existing signed wire formats stay closed.

All current-code citations in §9.1 refer to **8b14f65d504b35d10595d0b7c4d79d5c78b0e63e**;
line numbers are at that revision, not at this document's eventual merge head.
Identifiers and fields introduced below are proposed consumer requirements,
not runtime broker API members. T's published documentation schema at `7294b7a`
maps to the native C/V/F requirements below; it adds no wire fields or activation.
Native stricter requirements remain authoritative: the retained pre-lock c is
mandatory, exact H remains a stored QTUse commitment, and the original operation
deadline remains at most five seconds or stricter.

| Schema group at T | C: candidate commit | V: resolved visibility | F: freshness Publish / Retry |
| --- | --- | --- | --- |
| 1. Identity and dependency graph | §9.1.3 exact request, content/base/checkpoint/snapshot/head and C slot; §9.1.4 frozen content/completion split. | Same original operation/content plus authenticated resolution, pointer predecessor and distinct V slot (§9.1.3). | Protected schedule/request and action, frozen bytes/anchor/history, independent logical slot and publication_seq (§9.1.3). All windows retain native codecs and non-circular bindings. |
| 2. Time meanings | §9.1.1 original b/D and s/p/m; native required c maps only to p. | Same original D after resolution; new s/p/m, not C's captures (§9.1.1). | Publish/revalidate retain their original D and action-specific allowances (§9.1.1). Native c cannot use the schema's optional p=s fallback. |
| 3. Uncertainty coverage | §9.1 and §9.1.1 preserve raw/coarse predicates and original C reserve. | New V settlement/reserve within the same original D, without spending C's reserve again (§9.1.1). | Separate publish/revalidate reserve and native watermark/J projection (§9.1.1); no renewal or second use of a spent interval. |
| 4. Preparation authenticity | §9.1.2 staged signed content and §9.1.7 protected originals/witness before locks. | New preparation after matching resolution, with exact immutable content and live custody (§9.1.2, §9.1.7). | Frozen signed publication before final locks and complete protected read evidence (§9.1.2, §9.1.7). All require bounded owned originals; hashes alone confer no authority. |
| 5. Final admission, use and recovery | §9.1.2 exact predecessor/unused v admission before m; atomic C act/use (§9.1.4). | Same order against C completion/resolution and prior pointer, with separate V act/use (§9.1.2–§9.1.4). | Same order against exact StateID and resolved head; Retry changes floors/use, not publication position (§9.1.2–§9.1.5). Native QTUse requires stored H, exact CAS and independent recovery for all windows. |
| 6. Enforcement and qualification per platform | §9.1.1–§9.1.2 C workload/residual bound; §9.1.6 Linux/macOS full durable endpoint. | V marker/pointer and use durability, independent visibility authority and native parity (§9.1.2, §9.1.4, §9.1.6). | Publish/Retry persistence endpoints and their distinct workloads on both platforms (§9.1.1–§9.1.2, §9.1.6). All stay handoff_unqualified; stop remains independent. |
| 7. Paired controls and composition | §9.1.8 admission-order, race, duplicate, frozen-intent and crash pairs; §9.1.1 shared original D. | New handoff/read/use after C and resolution, with no shared reserve or checked candidate across lock release (§9.1.1–§9.1.2). | §9.1.5 retransmission/Retry distinction and §9.1.8 paired controls; no extra position or use grant from recovery. Every control remains PENDING. |

#### 9.1.1 Admission, original time and per-window budgets

Finish provider collection, durable settlement acknowledgment, protected
original reads, independent witness/ancestry validation, signing-service work
and bounded immutable copying before final locks. Retain the original operation
start `b`, exclusive deadline `D`, boot/domain, caller UTC deadline identity and
qualified conversion rule. Set `D <= b + 5 seconds`, intersected with every
stricter original deadline. A scheduled refresh has an independently authorized
schedule occurrence and original start too; its 20-second cadence is not its
execution budget. Retries, witness waits, a new receipt and a new window never
restart this operation deadline. Overflow in `b+5s` refuses.

**Whole-attempt preflight, including retry/reconnect.** Before starting, and
again before any retry or reconnect, admit work only if the remaining original
budget covers that exchange and every still-required barrier/local act. Account
for connection setup, all populated-stream and current-control checks, exact
confirmation, independent audit acknowledgment, complete proof pages,
time/settlement/original-reader/signer work, bridge/authority verification,
durable local commit and each remaining window's f, epsilon and reserve
obligations. Unknown bounds do not fit by assumption. A retry receives no new
allowance; uncertain outcomes stay fenced and exact recovery cannot authorize
late success for the expired attempt. No remote work moves under final locks.
This refines the existing original-D composition, not its deadline
([original budget rule](https://github.com/relux-works/swarma-owner-registry/blob/e5f2a38bcc2d34997f367144f8d15777f4cf0c18/design/owner-registry.md#L901-L916),
[WS pages/deadlines:235–243](https://github.com/relux-works/swarma-keymaker/blob/e7c5e1850c29d1f931de3faa24ad39d94da41ada/spec/witness-service.md#L235-L243),
[WC1 G20:28](https://github.com/relux-works/swarma-witness/blob/29df43b7e8b26d1bce7101c5fc73e8c9b8050b99/UNRESOLVED_QUESTIONS.md#L28)).

**Illustrative accounting only: every count, latency and allocation below is an
unmeasured assumption or arithmetic derived from those assumptions.** The
five-second ceiling is the existing contract, not a measured performance claim.
Model a cold exchange as TCP setup plus TLS setup plus request/reply, assumed
network cost `3*r` for RTT r. Assume extra transfer/flight delays are negligible
for these tables only; real transfer costs must be added. WC1 currently
specifies one request per connection without resumption; W2 and audit transport
profiles are not yet specified, so the model assumes the same cold cost for them,
not an available connection-reuse saving
([WC1 TLS policy:13](https://github.com/relux-works/swarma-witness/blob/29df43b7e8b26d1bce7101c5fc73e8c9b8050b99/spec/tls-profile.md#L13),
[WS TLS:157–160](https://github.com/relux-works/swarma-keymaker/blob/e7c5e1850c29d1f931de3faa24ad39d94da41ada/spec/witness-service.md#L157-L160)).

Let S be populated streams, p required proof-page requests, `g=3+p` primary
requests for one intent/resolution/latest plus pages, and `gF=1+p` for a separate
F latest plus pages. Assume each primary request checks S record heads and one
current-control head at W2, and performs one audit-tail check and one audit
confirmation at the separate audit authority. C+V adds two record confirmations;
unchanged-head F adds none. Thus, without double-counting nested network flights:

| Assumed workload | W1 exchanges | W2 exchanges | Separate audit exchanges | Derived total exchanges |
| --- | --- | --- | --- | --- |
| C+V | g | g(S+1)+2 | 2g | `N = g(S+4)+2` |
| Separate F | gF | gF(S+1) | 2gF | `N_F = gF(S+4)` |

The stream/control growth and confirmation/audit ordering come from
[WC1 preflight:71–134](https://github.com/relux-works/swarma-witness/blob/29df43b7e8b26d1bce7101c5fc73e8c9b8050b99/service/barrier.go#L71-L134),
[per-request admission:169–179](https://github.com/relux-works/swarma-witness/blob/29df43b7e8b26d1bce7101c5fc73e8c9b8050b99/service/barrier.go#L169-L179),
[confirmation then audit:247–296](https://github.com/relux-works/swarma-witness/blob/29df43b7e8b26d1bce7101c5fc73e8c9b8050b99/service/barrier.go#L247-L296)
and [WS read audit:610–616](https://github.com/relux-works/swarma-keymaker/blob/e7c5e1850c29d1f931de3faa24ad39d94da41ada/spec/witness-service.md#L610-L616).
This is a target-service model, not evidence of an accepting WC1 read service:
[successful latest/proof construction remains closed:133–155](https://github.com/relux-works/swarma-witness/blob/29df43b7e8b26d1bce7101c5fc73e8c9b8050b99/service/append.go#L133-L155).

For the assumed small case `S=1, p=2`, `g=5, gF=3`, giving `N=27, N_F=15`.
Assume C+V non-network work totals 1.600s, allocated as follows; these are caps to
qualify, not observed timings or generally available allowances:

| Assumed C+V allocation | Assumed seconds |
| --- | --- |
| Other time/original/signer/authority remote work | 0.400 |
| W1/W2/audit processing and local durability | 0.400 |
| C remaining durable-act f | 0.300 |
| V remaining durable-act f | 0.150 |
| Registry preparation/bridge/pre-lock work | 0.200 |
| C and V epsilon, 0.050 each | 0.100 |
| Planning slack | 0.050 |
| Total | 1.600 |

Assume separate F non-network work of 0.900s. For uniform r on all relevant links,
the derived totals are `3*N*r+1.600s` and `3*N_F*r+0.900s`:

| Assumed RTT on each link | Derived C+V seconds | Derived separate F seconds | Comparison with the existing exclusive five-second ceiling, under these assumptions only |
| --- | --- | --- | --- |
| 1–5 ms | 1.681–2.005 | 0.945–1.125 | Nominal arithmetic margin; no tail guarantee. |
| 20 ms | 3.220 | 1.800 | Fits this assumed small workload only. |
| 40 ms | 4.840 | 2.700 | C+V leaves only 0.160s nominal margin. |
| 80 ms | 8.080 | 4.500 | C+V cannot fit this assumed workload. |

Assumed window examples are `s->m<=50ms`; C has `f=300ms, epsilon=50ms,
T=450ms, R=500ms`; V has `f=150ms, epsilon=50ms, T=300ms, R=350ms`.
Assuming source delta 400ms and a qualified coverage rule, resulting deltas would
be 900ms and 750ms. These are unqualified examples of the existing per-window
predicates below. Reserve covers interval uncertainty once, not extra wall time
added again. Other services need their own qualified windows; F's history-size
cost and its own f/T/epsilon/reserve remain unqualified
([existing window obligations](https://github.com/relux-works/swarma-owner-registry/blob/e5f2a38bcc2d34997f367144f8d15777f4cf0c18/design/owner-registry.md#L1198-L1218)).

Add any extra initial read, independent W2 fetch, Q18 protection, original
retrieval, retry/reconnect, proof page, recovery or exclusion call not covered by
the assumed allocations; none is free. DNS, loss, transfer size, TLS extra flights,
contention and correlated tails can exhaust the budget even when the example
fits. Either sum disjoint network/processing costs as above or measure inclusive
W1 calls; never add their nested W2/audit time twice. Preflight must use remaining
mandatory work, not a mean per-call allowance. Q19 qualification must measure and
enforce actual setup/exchange tails, stream/page counts, nested audit/custodian
work, storage size/history/device endpoints, time/original/signer costs,
cancellation and late-release prevention. Neither percentile latency nor these
arithmetic examples qualify f or open any gate; Q21 owns the unresolved head-check
envelope ([WS added round trips:495–504](https://github.com/relux-works/swarma-keymaker/blob/e7c5e1850c29d1f931de3faa24ad39d94da41ada/spec/witness-service.md#L495-L504),
[WC1 whole-attempt gap:28](https://github.com/relux-works/swarma-witness/blob/29df43b7e8b26d1bce7101c5fc73e8c9b8050b99/UNRESOLVED_QUESTIONS.md#L28),
[Q19](#10-questions-for-review)).

Exactly one qualified suspend-aware elapsed-clock read occurs after admission
under **all** final locks, immediately before pure `VerifyWindow`. On Linux
select `CLOCK_BOOTTIME`, which includes suspend, as documented by
[clock_gettime(2)](https://man7.org/linux/man-pages/man2/clock_gettime.2.html).
The verifier receives the captured integer; it has no clock, callback, provider,
RPC, descriptor, randomness, lazy loading, I/O or mutation. Preparation captures
are outside this one final read. Define `s` as the original bound observation in
that clock domain, `p` as the last qualified pre-lock capture, and `m` as the
sole final read, following T's distinct meanings. Our retained native `c` maps
exactly to `p`, never to `s` or `m`; c stays required even though the shared
schema permits p to be absent and then use s. That fallback is not permitted by
this native profile. Check same boot/domain and, in checked integer nanoseconds:

```
m >= p >= s
f > 0; epsilon >= 0; T > 0
m - s + f + epsilon <= T
m + f + epsilon < D
```

`T` covers observation through the full durable endpoint, not just checker CPU.
`f` is the enforced remaining bound from `m` through that endpoint; `epsilon`
covers qualified mapping, conversion, capture and rate error not already charged
elsewhere. Reject negative elapsed values, underflow, overflow or equality at D.
The original `D`, `T` and reserve may only tighten. Never advance O, shrink the
original delta or restart s. The receipt must already include the whole reserved
delay in its uncertainty: write `delta = delta_source + R`, with R covering the
qualified T/error/rate rule once. Total delta stays <=1.5 seconds and within any
stricter policy. Verification checks coverage against that retained reservation;
it does not add R again, refresh it or spend it in two acts. Each distinct local
window needs new settlement/challenge/H and its own reserve within the same D.
All §9 raw and conservative converted predicates remain mandatory, including
`L>max(prior provider W, imported J)`, any stricter locked J, `U<A`, the admitted
watermark rule and strictly advancing new issuance. Provider W never becomes
the registry's prior admitted watermark.

| Window profile | T, f, epsilon and reserve required before selection | Why these bounds belong to this window |
| --- | --- | --- |
| `registry-candidate-commit/1` (C) | `T_C`, `f_C`, `epsilon_C`, `R_C` fixed by a qualified content-size/file-count/history/device profile. Require `delta_source+R_C<=1.5s`, coverage of `m-s+f_C+epsilon_C`, and the original D. No numeric f is qualified today. | f includes pure checks, exact predecessor/use CAS, manifest and blob writes, materialization, CURRENT rename and namespace durability, and committed completion/use state. History audit and capacity preflight must finish before m under exclusions or be included explicitly in f. |
| `registry-resolved-visibility/1` (V) | Distinct `T_V`, `f_V`, `epsilon_V`, `R_V`, collected **after** matching resolution; constrained by remaining original D and its own delta cap. No inherited C reserve or new five seconds. Values remain unqualified. | f covers final resolved-head/authority/use checks and the complete durable visibility marker/ref-pointer plus associated use commit. Witness/custodian work already spent time before entry; it is not hidden in a fresh timer. |
| `registry-freshness/1` (F) | `T_F`, `f_F`, `epsilon_F`, `R_F` selected for the exact action (`publish` or `revalidate`) and bounded retained-history workload, under its original D/delta cap. Both action-specific bounds require qualification. | Publish includes exact bytes/history/anchor/outcome and pointer/use durability; Retry includes new time floors and use state with the original publication unchanged. Existing history copying and persistence cost cannot be treated as constant just because the input record is bounded. |

**Protected provider-name map, version 1 (design only).** The closed table below
is part of the protected enrolled consumer profile. Apply the same exact mapping
when preparing independently expected bindings and when encoding the handoff;
compare the encoded field/value pair against that protected expectation again
at admission. Native records, identity bytes/domains and use-state names remain
unchanged. This is an explicit allowlist, not general slash replacement or
normalization; no request, receipt or unregistered version can extend it.

| Exact native name | Provider field | Exact provider value |
| --- | --- | --- |
| `registry-candidate-commit/1` | `Window.Profile` | `registry-candidate-commit:1` |
| `registry-resolved-visibility/1` | `Window.Profile` | `registry-resolved-visibility:1` |
| `registry-freshness/1` | `Window.Profile` | `registry-freshness:1` |
| `registry-window-intent/1` | Never a provider field; native owning identity codec (§9.1.3), whose exact bytes feed the enrolled operation digest. | None; retain the native name in its own codec. |
| `QTUse/1` | `TransitionVersion` | `QTUse:1` |

The field is part of each mapping: a window name cannot become TransitionVersion
and QTUse cannot become Window.Profile. Unknown names/versions and swapped fields
refuse protected consumer binding even when their spelling fits the alphabet.
Literal `/` in either provider field refuses `encoding`; alphabet conformance
alone grants no preparation, use or activation authority. Broker source pin for
this representation constraint: `b38aeda82fb563503960836c771bcc94e8103810`,
[timeconf/handoff_linux.go:137–159](https://github.com/relux-works/swarma-credential-broker/blob/b38aeda82fb563503960836c771bcc94e8103810/timeconf/handoff_linux.go#L137-L159)
and [timeconf/receipt.go:103](https://github.com/relux-works/swarma-credential-broker/blob/b38aeda82fb563503960836c771bcc94e8103810/timeconf/receipt.go#L103).
The published template requires explicit native-name mapping
([docs/time-window-profile.md:25–32](https://github.com/relux-works/swarma-credential-broker/blob/b38aeda82fb563503960836c771bcc94e8103810/docs/time-window-profile.md#L25-L32));
expected window/transition equality is checked at
[timeconf/window_linux.go:39–40](https://github.com/relux-works/swarma-credential-broker/blob/b38aeda82fb563503960836c771bcc94e8103810/timeconf/window_linux.go#L39-L40).
This code citation does not change T's documentation pin or qualify a registry
adapter; Q18–Q20 remain open.

These are explicit unqualified parameters, not permissive defaults. No universal
250/1000/2000 ms allowance is assigned. The installed profile must publish finite
values, units, maximum workload, rate/error calculation, enforcement/fence,
qualification digest and target OS/filesystem/device. Missing any one yields
`ActivationRefusal{handoff_unqualified}`. A percentile, timer, context, mutex,
post-sync read or suppression of a late reply does not enforce f: a pause after
m can let the act become durable late. Qualify an actual read-to-durability bound
or a commit primitive honoring original D; otherwise no act is admitted.
Emergency stop must remain independently reachable, including while outer
operation exclusions or failed storage calls are retained.

#### 9.1.2 Final locks, acts and current implementation gaps

The proposed **publication exclusion** serializes C, V and F against real-head
changes; the proposed **authority exclusion** freezes the relevant authority
generation/read set. Those are logical requirements with no current concrete
lock name. Qualify their implementation and lock order before an adapter:
publication exclusion, authority exclusion, then the applicable local mutex
below and its persistence transaction admission. A lifetime/outer sole-writer
fence and publication quiescence may span remote phases (§6.2); final mutexes
must not. The dependency graph must not make the outer exclusion a time/RPC
wait on the emergency-stop path. Do not reacquire a public Store method while
already holding its non-reentrant mutex.

For **each of C, V and F**, initial persistence/use admission and selection of
the exact protected predecessor revision and unused use revision `v`, including
the current head/authority generation, occur under all final locks **before m**.
After m and the pure check, the exact-revision CAS
`unused(v) -> burned(v+1,H,act,outcome)` follows in the same atomic commit as
the act (§9.1.4); initial admission after m is forbidden even if nonwaiting and
budgeted in f. No checked candidate survives release of the final locks.

| Window | Exact current anchors and proposed final held/released state | Durable endpoint and release |
| --- | --- | --- |
| C: real-head candidate commit | `Store.mu` (`internal/store/store.go:100–109`) is acquired/deferred at `:462–463`; `s.check`, candidate and capacity checks follow at `:464–478`. The directory's lifetime nonblocking exclusive flock is acquired in `internal/store/fs_linux.go:67–77`; it is the outer local-instance fence, retained across remote phases, not a final-window mutex. Hold Store.mu and the proposed publication/authority exclusions; require the expected local state ID as well as head/sequence, native intent and unused revision. Preview at `internal/store/store.go:427–452` reserves nothing. Publisher.mu is not part of C; no provider, signer RPC or witness call under final locks. | Completion of the **whole store transaction and QTUse state**, through checked CURRENT namespace sync and exact committed state. Current write path is `internal/store/store.go:479–506`; it has no QTUse transition. Release final locks after commit or fenced refusal, before witness resolution RPC. A local commit is still private, not resolved publication. |
| V: later resolved-head visibility | After remote matching resolution, acquire the proposed publication/authority exclusions and a new visibility persistence CAS gate against exact local C completion, witness resolution and prior public pointer. Store.mu may protect the local read set only through an explicitly reviewed internal integration; View alone releases it (`internal/store/store.go:508–515`). No visibility mutex/marker implementation currently exists: Store rejects an unchanged head (`internal/store/store.go:327–329`), and its interface disclaims external publication (`:87–95`). Publisher.mu need not be acquired; if freshness state also changes, define the common transaction/order first. | Durable atomic publication marker or public ref/pointer **and V-use state**, after matching resolution. Required file/namespace barriers and persistence acknowledgment are part of the endpoint; a pointer reservation or network acknowledgment is not it. Release final gates after this local commit or fenced refusal, before any later provider/witness activity. Never reuse C's handoff. |
| F: Publish and Retry | `Publisher.mu` is declared at `internal/freshness/freshness.go:184–195` and held at `:315–316` through return. Add the common publication/authority exclusions and actual persistence CAS admission. Recheck resolved head and no pending intent under them, not merely supplied witness bytes; the current witness input is caller-authenticated (`:162–169`). Store.mu is not acquired by this package. Release all final gates before collection/RPC. | For Publish, exact new bytes, full history/anchor, outcome, admitted watermark, decision floor, boot, pointer and F-use commit together. Retry commits new time/use floors without another publication_seq or changed bytes/outcome (`:404–412`). State update/ack checks are `:428–450`; the persistence contract `:147–153` has **no implementation**. |

Current snapshot and journal builders create private candidates, not publication
authority (`internal/snapshot/snapshot.go:80–88`,
`internal/journal/publication.go:28–35,135–153`). C owns their authoritative
commit. Signer invocation is not thereby at-most-once: that stronger property
needs its own durable signer-intent/idempotency contract.

Current F invokes `Signer.Sign` under Publisher.mu
(`internal/freshness/freshness.go:382–399`). The selected separate signer process
(Q3) cannot be treated as bounded local pure work. Stage signing outside final
locks and freeze exact signed bytes before the final handoff; revalidate those
unchanged bytes inside. This needs a reviewed staged API, not a claim that today's
Publish already implements F. Original issuance still uses its own §9 projection;
later commit/publication revalidation neither resigns nor changes issued_at to
the later interval's floor(L). Final collection binds the already frozen artifact,
avoiding an operation digest that depends on its own receipt or H. If a selected
broker codec cannot express this staging/binding without a cycle, keep it closed
and resolve the profile with the codec owner before implementation.

No residual f is bounded by the current disk path. `Store.check` audits current
inventory (`internal/store/store.go:417–423`,
`internal/store/recovery.go:160–183`); `disk.atomic` does a checked write, Sync,
close, Renameat and directory sync (`internal/store/fs_linux.go:236–317`), and
`syncDir` does directory Sync/close (`:170–191`). Blocking storage, scheduling,
faults, suspend and growing history have no deadline argument or enforced bound;
Commit itself takes only Request (`internal/store/store.go:70–95,455`). V lacks
its act primitive; F lacks its backend. All three remain **handoff_unqualified**.

#### 9.1.3 Native operation and window binding

Define a proposed owning canonical identity codec `registry-window-intent/1`.
Publish its closed schema, byte/length bounds and test vectors jointly with the
broker template before use. Hash exact canonical owning-codec bytes using the
enrolled native-operation domain. No executor Scope is reused. Encode the exact
registry key and owner UTF-8 bytes without normalization; owner length can be
255 bytes (§6.1), so never copy/truncate them into provider enrollment IDs.
The protected enrollment mapping binds that full native digest to consumer,
authority/policy/adapter, machine, purpose and continuity scope.

Expected values come from protected original intent and the current **locked**
read set. The receipt is only compared to them. Neither the receipt nor a request
can supply its own expected authority, revisions, unused state or schedule slot.
The identity has no H, resulting use-state digest containing H, or other circular
post-state reference. Bind the known content candidate and expected predecessor;
the resulting composite completion ID is computed only after H is available.

| Identity field group required from the future template | C | V | F |
| --- | --- | --- | --- |
| Registry, exact owner, consumer enrollment/continuity scope | Exact protected native values for every window; no lossy provider-ID conversion. | Same identity as the committed content; new phase. | Same registry/owner; independent freshness namespace. |
| Original operation | Authenticated principal, original request ID/generation and canonical argument digest; an exact retry retains that identity. | Original C operation plus exact matching resolution identity; not a newly invented request. | Protected schedule occurrence or original publication/revalidation request identity and arguments; method is `publish` or `revalidate`. |
| Content and current read set | Exact immutable content-candidate inventory/commit, prior base, checkpoint, snapshot, journal head, next registry_seq, original issuance provenance. | Same committed content/base/checkpoint/snapshot/head, authenticated resolution and previous visibility pointer. | Frozen publication bytes/ID for publish or exact previous bytes for Retry; base, checkpoint, snapshot, resolved head and current freshness anchor/history identity. |
| Revisions | Exact predecessor content/state ID, authority generation, visibility revision, use predecessor and current time floors. | Exact C completion ID and local/public pointer predecessor, authority generation and use predecessor. | Exact `StateID(Before)`, real-head/visibility revision, authority generation, use predecessor and current watermark/J/boot. |
| Purpose, phase, logical window sequence | Authorized record/snapshot/checkpoint signing purposes; phase C and its fixed operation-owned slot. | Existing authorized purpose of the bound signed records and local visibility action; no new signing purpose. Phase V has its distinct operation-owned slot, linked to C. | Existing authorized freshness signing purpose for publish; Retry revalidates that same purpose without a new key/purpose. Phase F plus action and its allocated logical decision slot. |

`registry_seq`, `publication_seq`, logical window sequence and use revision are
distinct values. Only real changes advance registry_seq. F publish advances
publication_seq, Retry does not. A replacement H cannot choose a new logical
sequence or operation ID to evade a burned slot. The operation controller fixes
allowed windows before preparation, allocates a next genuinely authorized
revalidation slot under protected serialization, and retains the prior slots.
If a profile cannot bound this allocation/history within its capacity contract,
refuse rather than discard burn history. The current F state contains history,
watermark/J/boot but no such ledger (`internal/freshness/freshness.go:98–105`).

#### 9.1.4 Versioned use transition and frozen content decision

For Q18, the owner decided (2026-10-25) on **option (i)**: version the local
transaction into immutable witnessed **content-candidate inventory** and later
atomic **completion/use metadata**, bound by a non-circular composite root.
This decides the format boundary, not the exact schema or qualification, and
is not permission to change today's inventory.
Option (ii), revising the inventory and witness protocol, would need an explicit
new cross-service contract and re-review. Option (iii), a separately protected
ledger, requires the owner to approve custody, restore authority and coupling;
this profile does not select or create it.

The reason is ordering: §6.2 freezes the candidate before witness intent, but C
must collect its handoff after acknowledged intent and V after resolution. H
therefore cannot be inserted into that already witnessed inventory. Collecting
it early across a witness call, changing candidate_digest, hiding H in RequestID,
Operation or Result, or appending an unrecognized sidecar is forbidden. Current
local image/manifest formats are `delta-1`
(`internal/store/store.go:117–134,150–152`) and recovery enforces exact inventory
(`internal/store/recovery.go:150–183,291–295`); they are not this proposed split.

Proposed local **QTUse/1** is carried by a newly versioned owning transaction;
neither closed journal/checkpoint/revocation/freshness records nor witness intents
gain a member. Its unique semantic key is:

```
(registry, exact owner, native operation identity, phase, logical window sequence)
```

H is a stored commitment **value**, never a component of that key. Retain schema
version, exact predecessor composite revision, original operation/run/boot/start/D,
profile/qualification and receipt/original-closure commitments, H, bound content
candidate and resulting act/pointer identity, authority generation, time floors,
status and exact outcome linkage. All three windows use the same key rule; their
namespaces/phases and owning transactions stay distinct.

Under the final serialization and after the one read/check, atomically compare
the exact predecessor revision **and** the independently authenticated unused
slot. Commit `unused(v) -> burned(v+1,H,act,outcome)` with the act in the same
atomic durable transition. A WindowCheck is only a proposed CAS; two pure checks
of v grant no independent uses. Missing state is not unused. Every H, including
a different commitment, refuses for an already burned key. An uncertain attempt
stays held/burned until exact recovery; it never returns to unused. Keep durable
tombstones/outcomes across cleanup, restart, reboot and capacity pressure.

For option (i), the owning composite commit must atomically select content,
completion and QTUse in one committed root. The metadata binds the immutable
content digest; the root authenticates both, using a versioned, non-circular
encoding. The bridge must independently read/verify actual content durability,
the exact associated committed metadata/use revision, original intent, act and
outcome, against an independently retained monotonic completion authority before
resolution. It cannot accept the writer's digest alone. Recovery verifies this
whole root/lineage and independent expected revision, plus visibility, freshness
history, provider continuity and old-instance fencing. An intact content snapshot
beside absent, restored or conflicting metadata fences; neither local hashing nor
the content witness alone proves that no later use/completion existed.

The independent completion/visibility rollback authority and its protected
retention/acknowledgment protocol remain open, tied to the custodian in the
Q10/WC1 G17 second-witness direction, not qualified by that placement decision. Any remote protection
step must occur outside final locks, yet must not leave the act/use root eligible
for rollback or require an impossible post-commit admission renewal. If that
cannot be demonstrated with the selected atomic protocol, option (i) remains
unimplemented and the owner must choose a revised protocol or protected ledger.
Current Reconcile requires an external expectedStateID
(`internal/store/recovery.go:201–207,291–292`); F likewise requires independent
reconciliation (`internal/freshness/freshness.go:120–153`). Those inputs do not
already authenticate proposed QTUse. Until schema, bridge authentication,
rollback protection, genesis/migration and recovery are approved, **C, V and F
consume adapters stay closed**. Never synthesize an unused entry for old state.

#### 9.1.5 Crash, retry and refusal mapping

| Cut or race | Required behavior; no new-use grant from recovery |
| --- | --- |
| Act without burn | Not an accepted composite state. Fence missing/mismatched QTUse even if all content bytes survive; never reconstruct unused from an old manifest. |
| Burn without act | Retain burned/held status and exact candidate; no second attempt with another H. Recover only the authorized exact outcome/protocol. At-most-once permission can lose availability; no generic exactly-once promise. |
| Partial or uncertain write/sync/rename/directory sync | Retain evidence, fence and reconcile exact composite state with independent authority. A late recovered commit can retain a burn but cannot create automatic late visibility. |
| Lost acknowledgment or duplicate after restart | Recover exact retained bytes/outcome/status; no new act or ConsumeIfUnused. Current Store returns a retained outcome at `internal/store/store.go:312–318,471–473`; distinguish this path explicitly from a new successful use. Old prepared run/window objects are discarded. |
| Freshness retransmission versus Retry | Pure retrieval of stored bytes/status makes no new time-authority claim and burns no new slot. Current identical-input Publish rechecks an original interval without persistence (`internal/freshness/freshness.go:329–341,404–412`); that cannot be promoted to fresh qualified authority. Any currentness decision or actual renewed exposure needs a fresh F handoff/use/floor transition. Retry preserves bytes, original outcome and publication_seq while consuming its own new authorized revalidation slot; no renewal of the old H. A transport release needing time authority requires a separately bounded profile, not implicit reuse. |
| Head A prepared, locked head B | Refuse before burn/act on any mismatching head/revision/base/authority. Release final locks, then recollect only if original D permits; never overwrite B or restart D. |
| Restored use history or two instances | Independent completion, publication/freshness and provider continuity reconciliation plus live-instance fencing are required. An intact provider W cannot detect restored consumer-use history. Lifetime flock is only local cooperation (`internal/store/store.go:98–99`), not a clone fence. |
| Reboot or suspend across D | Discard old-boot handoffs, preserve all burns/floors and reconcile independently before new evidence. Suspend counts against D. No cross-boot elapsed comparison or deadline reconstruction from wall time. |

The future broker's fixed `HandoffRefusal` reasons preserve precise causes:
`boot`, `deadline_binding`, `deadline`, `reserve`, `overflow`, `interval`,
`authority`, `operation`, `window`, `replay`, `run`, `continuity`,
`unauthenticated` and `original_missing` as applicable. Check failure yields a
zero candidate and no writes. These are proposed shared typed reasons, not
current registry public codes. Map handoff/time qualification failures outward
to existing `time_unavailable`; a mismatching real head maps `head_conflict`,
conflicting native retry maps `request_conflict`, unknown store durability maps
`state_unavailable`, and F uses `freshness_state_unavailable` or
`freshness_persistence_unavailable` for those corresponding failures. Existing
store codes are at `internal/store/store.go:25–42`; F's time/persistence refusals
are at `internal/freshness/freshness.go:341–343,404–409,432–433`. Preserve the
actual narrower verifier refusal where applicable. No broad new wire code is
needed. Inactive construction returns the proposed shared typed
`ActivationRefusal{handoff_unqualified}` (or `handoff_unselected` for an
unselected profile); it cannot silently fall back to point time or a raw receipt.

#### 9.1.6 Mandatory macOS profile

The same C/V/F logical windows require native parity; Linux activation is not
evidence of it. Select one fixed `mach_continuous_time()` read after all native
equivalent final exclusions. Apple's pinned
[Mach header](https://github.com/apple-oss-distributions/xnu/blob/xnu-11215.1.10/osfmk/mach/mach_time.h#L60-L64)
defines advancement during sleep. Preload the timebase; convert ticks with checked
rational arithmetic, current elapsed rounded upward and deadline downward.
No `mach_absolute_time`, sleep-excluding clock or wall-time substitution. Capture
and validate `kern.bootsessionuuid` outside final locks through protected native
sysctl; the pinned [XNU declaration](https://github.com/apple-oss-distributions/xnu/blob/xnu-11215.1.10/bsd/kern/kern_sysctl.c#L2869-L2871)
is read-only. Boot UUID alone does not fence a restored machine image.

Use a separately reviewed continuous-clock domain/platform mapping; do not label
Mach ticks as Linux BOOTTIME or silently widen an existing closed receipt codec.
Keep original UTC identity, boot, start, D, T, f, epsilon and reserve, with the
same inequalities and endpoint. The broker's native platform representation and
mapping qualification remain gaps.

| Native window | Required act and durability |
| --- | --- |
| C | Exact content/completion/use root transaction, checked full writes and file flushes, same-filesystem atomic root rename, checked close and separately qualified namespace durability for every created/renamed dependency. |
| V | Durable resolved visibility marker/ref-pointer via same-filesystem atomic rename, with V-use in the same transaction and its directory/persistence barriers. |
| F | Publish: exact history/anchor/outcome/time/use and atomic publication pointer. Retry: atomic new floors/use retaining exact prior publication. Both require the actual persistence commit and all relevant namespace barriers. |

Require checked `fcntl(F_FULLFSYNC)` for written durable files, plus independently
qualified directory/rename ordering. Apple's [fcntl manual](https://developer.apple.com/library/archive/documentation/System/Conceptual/ManPages_iPhoneOS/man2/fcntl.2.html)
describes the device-flush request; it neither bounds completion nor guarantees
that every device honors it. No silent fsync-only fallback. Apple's
[rename manual](https://developer.apple.com/library/archive/documentation/System/Conceptual/ManPages_iPhoneOS/man2/rename.2.html)
requires the same filesystem and describes replacement-name continuity; namespace
atomicity is not the full durable multi-file/use commit. Qualify the exact native
OS/filesystem/device, full flush and directory barriers, locks, independent
restore/instance fence, crash recovery, suspend and post-read f. The current
store path is explicitly Linux-specific (`internal/store/fs_linux.go:1–13,43–77,236–317`);
no Darwin store/clock/use adapter or native qualification is supplied here.
**handoff_unqualified on both platforms** remains the disposition.

#### 9.1.7 Broker interfaces required before implementation

The protected **original reader** must return exact receipt/settlement/source
bytes, full bounded original manifest/closure and authenticated settlement
association, protected descriptor/object identities, validator version and
enrollment/scope/operation/challenge/generation bindings. It must establish live
provenance and immutable retention, reject holds/steps or missing originals, and
prevent unobserved authority/original invalidation through the admitted window.
It does not accept arbitrary saved receipts as live collection. Its factory must
retain private provenance; a public constructor from bytes/hash is insufficient.

The independent **witness interface** must establish the current checkpoint,
exact receipt/record association, ancestry and authoritative acknowledgment under
original D, with explicit store/witness restore exclusions. A witness-head digest
is not an acknowledgment. Fetch, reconciliation and unbounded history traversal
stay outside final locks. Returned bounded owned evidence must remain valid under
the installed invalidation/fencing model. Neither interface replaces the
registry's independent use/publication rollback protection.

The published template maps the native identity groups in §9.1.3, phase/action,
logical sequence and exact predecessor revisions, original boot/domain/start/D
and UTC/mapping identity, distinct s/p/m admission provenance with native c=p,
T/f/epsilon/reserve and qualification identity, exact receipt/closure/validator
references, authenticated
anchor, private run and native-required stored H. The sole m is captured locally
after admission, not a receipt-supplied assertion or future value required in
the preparation commitment. Preserve current receipt and closed consumer wire
codecs;
resolve any unsupported field or platform domain with the broker owner rather
than invent an extension. Production reader, witness, preparation and admission
implementations remain inactive and return typed activation refusals.

#### 9.1.8 Paired controls — all PENDING

These are design acceptance witnesses, **not executed tests or qualification**.
Every negative asserts its named refusal/fence, no forbidden act or duplicate
position, and the exact permitted burned/held state; the nearby positive checks
the same candidate with just the violated condition repaired. Arithmetic fixtures
do not qualify real scheduling/storage. Run native pause/crash controls only
against the selected actual mechanism in isolated fixtures.

| Planned control pair | Negative witness | Valid control | Status |
| --- | --- | --- | --- |
| `TestWindowStaleCapture` | Same old c, but delay final-lock admission beyond original D; actual fresh m refuses deadline. A checker using only c misses it. | Same inputs, timely actual m and qualified residual endpoint. | PENDING |
| `TestWindowUseAdmissionBeforeCapture` | Capture m before initial persistence/use admission or before selecting the exact predecessor and unused v; refuse window entry with no burn/act, even for a nonwaiting later admission budgeted in f. | Under every final lock, admit the exact predecessor and unused v before the sole m, then check and atomically CAS act/use. Moving admission/selection after m must fail the order assertion. | PENDING |
| `TestWindowDoubleCheck` | Two checks from one unused predecessor both produce candidates; a second durable consume must fail revision/replay with one act total. | One check/CAS against unused v commits act/use once. | PENDING |
| `TestWindowTimelyAndLateAct` | Pause immediately after m through D or suspend across it; the actual selected primitive must prevent late durable act. A late reply suppressed after an already late act fails this control. | Same actual primitive commits complete act/use before D. Unknown f makes both unqualified. | PENDING |
| `TestWindowDeadlineEquality` | `m+f+epsilon == D` refuses deadline; elapsed/reserve equality alone cannot override it. | Same workload with strict endpoint `<D` and elapsed `<=T`. | PENDING |
| `TestWindowLockedHeadChanged` | Prepare head A, then admit locked B; head/revision refusal before use or publication. | Matching A with exact same locked read set. | PENDING |
| `TestWindowBurnedDifferentH` | Burn key with H1, present H2/new challenge for same key; replay refuses and retains H1/outcome. | Independently authorized next logical slot with fresh H and exact predecessor. | PENDING |
| `TestWindowLostCommitAck` | Lose ack after act/use durable commit; restart returns exact stored outcome, no consume or act again. | Original acknowledged commit returns the identical bytes/outcome. | PENDING |
| `TestWindowFreshnessRevalidation` | Reuse old H for currentness, or allocate a new publication position on Retry; replay/contract refusal with unchanged bytes/position. | Fresh F evidence and new authorized revalidation slot atomically advance floors/use only; retrieval/status retransmission grants no new time authority. | PENDING |
| `TestWindowUseMetadataMissing` | Intact content but absent/mismatched use/completion metadata fences; missing is not unused. | Intact atomic composite and independent expected revision recover exact state. | PENDING |
| `TestWindowPostIntentInventoryChanged` | Add later H into frozen content inventory; candidate mismatch refuses, no altered witness intent. | Frozen content unchanged; approved versioned completion binds H in the atomic composite. | PENDING |
| `TestWindowUseRestoreDespiteProviderW` | Restore old use ledger while provider W stays current, or start competing instance; independent consumer authority fences. | Current consumer/provider histories, exact revisions and sole live instance reconcile. | PENDING |
| `TestWindowOriginalOrBudgetSubstitution` | Swap receipt/original, owner bytes, operation/purpose/generation, boot, deadline or reserve; intended authentication/binding/overflow refusal, no writes. | Exact protected closure/intent and original qualified budget pass. | PENDING |
| `TestWindowProviderNameLiteralSlash` | Copy a native slash name into either provider field; `encoding` refuses, with no prepared authority, burn or act. | All four mapped provider values in map version 1 fit the broker alphabet; exact field/value pairs pass name validation only. | PENDING |
| `TestWindowProviderNameWrongVersion` | Use an unregistered `:2` in either field, including when preparation and encoding both propose it; protected mapping refuses before burn/act. | Exact version-1 mapping at both preparation and encoding passes name binding. | PENDING |
| `TestWindowProviderNameUnregistered` | Supply an unregistered native or provider name, or place `registry-window-intent/1` in a provider field; refuse mapping (literal slash also fails encoding), no burn/act. | Native intent codec remains outside provider fields; only the table's enrolled names pass mapping. | PENDING |
| `TestWindowProviderNameSwappedField` | Put `QTUse:1` in Window.Profile or a window value in TransitionVersion, or map differently at preparation and encoding; refuse protected binding despite valid alphabet, no burn/act. | Each exact table row supplies its declared field at preparation, encoding and admission; no native record/identity bytes change. | PENDING |

These name controls target the protected map above and the broker's pinned
alphabet/field checks at `b38aeda82fb563503960836c771bcc94e8103810`:
[timeconf/receipt.go:103](https://github.com/relux-works/swarma-credential-broker/blob/b38aeda82fb563503960836c771bcc94e8103810/timeconf/receipt.go#L103),
[timeconf/handoff_linux.go:137–159](https://github.com/relux-works/swarma-credential-broker/blob/b38aeda82fb563503960836c771bcc94e8103810/timeconf/handoff_linux.go#L137-L159)
and [timeconf/window_linux.go:39–40](https://github.com/relux-works/swarma-credential-broker/blob/b38aeda82fb563503960836c771bcc94e8103810/timeconf/window_linux.go#L39-L40).
They are pending consumer tests, not evidence that the generic provider alphabet
enforces this registry allowlist or that any adapter is activated.

Future gates must demonstrate detection with bounded plants: stale-c-only
admission, omitted unused-revision CAS, H-in-key, late-act acceptance, missing-use
acceptance and restored-use acceptance. Record the specific named failing test;
a setup failure, unapplied plant or survivor is not detection. All remain PENDING.

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
| Q10: witness coordination / WC1 G17 | Decided direction: the owner decided (2026-10-26) on a second witness on a separate VPS: prototype on the same provider in a separate project/account without a shared API token, retaining common-provider outage/administrative risk; platform on a different provider/account. The owner decided (2026-10-26) on option (a), WC1 plus one synchronous independent G17 custodian for all consumers with separately enforced old-instance fencing; the weaker option was rejected. The owner decided (2026-10-26) that placement is not provider/identity/pin qualification and trust-node deployment is a separate owner step. Concrete provider/SLA, typed confirmation/read/original-evidence protocol, independent keys/epochs, recovery domain and qualification remain open; all gates stay closed. Exact predecessor confirmation and fresh reads fit the original budget; no periodic, local or absent-provider fallback. Selected WS stream, independent bridge, eight-to-six adapter and advancing anchor remain unqualified. Q18 residual authority stays tied to this custodian; its option-(i) boundary stays decided. Registrar WC2 stays separate ([placement and source anchors in §6.2](#62-ws-server-adapter-and-real-transactions)). |
| Q11: time activation | Decided, 2026-10-24, content review: durable watermark, zero backward tolerance and explicit recovery. Hold continuity and dedicated-service authdata remain qualification gates; missing independent history fences. |
| Q12: complete publication/restore | Decided, 2026-10-24, content review: complete chained freshness history and off-node heads retained before use, with §5.2 atomic local publication and fail-closed restore; S4 supplies the wire/anchor; independent storage and complete-history integration remain unqualified. Historical re-verification needs a separate future path; gossip alone cannot prove absent hidden publications. Co-location/partition and no pre-publication deadline remain explicit. |
| Q13: witness metadata disclosure / WC1 G07 | Decided rule: the owner decided (2026-10-25) on closed-by-default disclosure of witness and custodian metadata only to an explicitly approved circle of readers; otherwise isolation applies, without filtering the signed chain. The approved reader set or isolation design, implementation and qualification remain open; the gate stays closed ([existing Q13:1564](https://github.com/relux-works/swarma-owner-registry/blob/e2b06e6b0d08a50b68dde8c015e6e6a5bbdbb208/design/owner-registry.md#L1564), [WC1 G07:15](https://github.com/relux-works/swarma-witness/blob/29df43b7e8b26d1bce7101c5fc73e8c9b8050b99/UNRESOLVED_QUESTIONS.md#L15)). |
| Q14: D39 prior records | Decided, recorded 2026-10-25; F §§2.7, 6 and D39. Every advancing proof supplies both prior entry anchor and exact prior checkpoint via IDJournal, J5a then updated J6; no extra projection timestamp. |
| Q15: D40 state and bounds | Decided, recorded 2026-10-25; F §§2.9, 6–7 and D40. Explicit authorized publication genesis, exact FreshnessAnchor and WitnessReadAt, independent 64-publication/532,480-byte bounds; durable state before use. |
| Q16: D41 recovery/composition | Decided, 2026-10-25: S5 merged at `5f4e7435d75697385ff6ce15a7501ac6f31c8ea5`, S5 §§2.10, 6–7 and D41 (closed). Explicit separate re-anchor keeps the accepted skipped-span fork-detection cost, full retained history and endpoint audit. Merged receipt composition in §5.3 uses the admitted anchor and whole Evidence, with no receipt-failure fallback. Code re-pin, adapter qualification and K3/D12 activation remain separate gates; no weaker legacy Context is authorized. |
| Q17: D42 external-root currentness | Decided: the owner decided (2026-10-25) on **option (a)**. Operator and grant roots sign snapshot content only when it changes; the registry attests currency with its own multi-snapshot freshness format (§5.3) and cannot alter that content. Options (b), a per-root online freshness signer, and (c), reissue every 60 seconds, were rejected: (a) avoids periodic root signing while retaining distinct content authority. Residual risk: a compromised registry-owner key could withhold a newer root snapshot, hiding revocation. The witness and full retained history mitigate this only if the root's **Signer application checks its own submission was journaled**. That check is the root's obligation; the registry must supply journal inclusion evidence for the exact submission (intake receipt plus authenticated witness head/inclusion proof), not merely an intake acknowledgment. Freshness/1 at F (`f76a400`) binds a single snapshot, so (a) requires the format owner (waggle) to merge a multi-snapshot freshness change and us to pin it. Until that change is merged and pinned, current D32/S5 scope is unchanged: operator-/grant-root snapshots retain their own 60-second age checks, registry receipts cannot extend them, and grant/1, D33 service-key and device intake remain gated on that pin. Publisher coverage and receipt consumption must follow the format owner's specified rule; none is invented here ([prior design:1551](https://github.com/relux-works/swarma-owner-registry/blob/fa6e1ef2e02138a7c935672ec2436709dbc96cf6/design/owner-registry.md#L1551), [F single snapshot:783–803](https://github.com/relux-works/waggle/blob/f76a400ebeb599b95a9bf9b2c59b522bef0392d8/spec/swarm-envelope-2.md#L783-L803), [S5 D42:205–228](https://github.com/relux-works/waggle/blob/5f4e7435d75697385ff6ce15a7501ac6f31c8ea5/UNRESOLVED_QUESTIONS.md#L205-L228), [S5 scope:998–1007](https://github.com/relux-works/waggle/blob/5f4e7435d75697385ff6ce15a7501ac6f31c8ea5/spec/swarm-envelope-2.md#L998-L1007)). |
| Q18: QT-2c local content/use format | Decided format boundary: the owner decided (2026-10-25) on §9.1.4 **option (i)**, versioned separation of immutable witnessed content from later atomic local completion/use metadata, bound by a non-circular composite root. The independent completion/visibility rollback authority and protected retention/acknowledgment protocol remain open, tied to the Q10/WC1 G17 second-witness direction, whose concrete provider/SLA, typed protocol, keys/epochs, recovery domain and qualification remain open. Exact versioned schema, capacity, genesis/migration, bridge authentication and recovery remain design and qualification work; the decision approves none of those details. No choice is implemented; all C/V/F consume-adapter activation gates stay closed. |
| Q19: QT-2c durable endpoint timing | Durable act plus use, one post-admission suspend-aware read, original <=5-second-or-stricter D and same-process keyless trust are settled requirements. Open with platform and storage owners: actual enforced f or deadline-honoring act for each C/V/F workload, finite T/epsilon/reserve qualification, final common exclusion/order and emergency-stop independence. Unknown bounds keep all windows handoff_unqualified. |
| Q20: QT-2c provider/template and native parity | Open jointly with broker/platform owners: protected original reader and independent witness contracts, profile/identity codec and frozen-artifact signing staging, original deadline/rate mapping, Linux and mandatory macOS clock/boot representation, native full-flush/rename durability and instance/restore fencing. A template must map the required fields without widening closed wire codecs; no accepting implementation is authorized by this design. |
| Q21: per-request head-check envelope / WC1 | Open with the owner and WC1: which retained record heads and current-control state must be checked on each request, with what finite stream-count/exchange bound and proof of equivalent rollback protection? S3 checks every populated stream plus current control, so cost grows with S ([WC1 preflight:71–134](https://github.com/relux-works/swarma-witness/blob/29df43b7e8b26d1bce7101c5fc73e8c9b8050b99/service/barrier.go#L71-L134)). Under §9.1.1 assumptions p=2, 5ms on every link and 1.600s non-network work, S=41 yields N=227 and 5.005s, already beyond the original five-second ceiling; an assumed S=201 yields 17.005s. These are derived unmeasured examples, not observed consumer inventories or latency. Placement alone does not bound the envelope. Do not omit heads, batch checks or weaken fresh/control coverage without an accepted protocol and qualification; the gate stays closed ([budget model](#911-admission-original-time-and-per-window-budgets), [WC1 G20:28](https://github.com/relux-works/swarma-witness/blob/29df43b7e8b26d1bce7101c5fc73e8c9b8050b99/UNRESOLVED_QUESTIONS.md#L28)). |
| Q22: owner-activation domain and purpose | Open: Proposed §12 domain `swarma-owner activation/1`, profile `owner-activation/1` and Ed25519 external root signature require complete domain/profile and verifier review. Purpose 7 is free in Keymaker but not reserved. Keymaker never derives, imports, holds or signs with the activation key; no Keymaker purpose is required unless a later registry-wide allocation decides otherwise. No purpose/path/API is allocated. Signing stays closed. See [§12](#12-owner-root-activation-record-activation1). |
| Q23: owner activation root and pins | Open: Select the actual external owner-activation root identity, algorithm/profile acceptance, protected owner/audience/receiver role pins, historical authorization, rotation/lost-key continuity and revocation. The activation key is distinct from the operator-root key and every other excluded role; no existing registry/device/station/service root is an alias or gains this role. Same-key cross-role reuse must refuse. No key or pin is selected; signing stays closed. See [§12](#12-owner-root-activation-record-activation1). |
| Q24: activation reference closure and limits | Open: Jointly publish the complete native qualified tuple and accepted artifact/review/intent/occurrence mappings through the existing qualification, policy, list/marker and phased cutover records. Both role UID/generation references and the explicit protected caller-to-worker mapping must be published; current broker identity/property/row coverage is insufficient. Set finite record/reference/identifier limits and canonical identifier mapping; no parallel tuple/cutover schema or opaque fallback. No body may be instantiated while these are missing. See [§12](#12-owner-root-activation-record-activation1). |
| Q25: activation validity time | Open: Select and qualify the authoritative UTC/elapsed/boot/continuity source, conservative whole-second conversion, finite start/end and original component deadline intersections. No guessed TTL, wall-clock fallback or renewed occurrence; unknown time closes forward authority. See [§12](#12-owner-root-activation-record-activation1). |
| Q26: activation withdrawal/currentness | Open: Publish explicit owner withdrawal and authoritative invalidation transport, detection/acknowledgment/retention and independent restore floors. Where registry root-state publication is used, retain Q17 D42(a) format/pin, publisher and consumer gates; otherwise qualify the external authority/transport. Planned expiry/removal closes NEW starts through the dispatcher-owned or protected start gate, with retained closure evidence and the earliest original stop bound; material continues under broker policy until worker stop. No new-lease interceptor, admission closure or immediate policy replacement is required for that case. Keep original lease lifetime, policy-generation invalidation, release and token debt separate. Genuine security revocation remains closed: the operator manually stops the worker and replaces broker policy, with an explicit detection-to-policy-replacement-commit issuance window, not a fence. A first-run canary claims no execution-isolation security-revocation conformance. See [§12](#12-owner-root-activation-record-activation1). |
| Q27: activation replay ownership | Open: Assign the authoritative replay/consumption store owner and cross-consumer reservation/commit/outcome/recovery protocol. Preserve immutable original intent/body/occurrence, external restore/clone floors and G18 exclusion without replenishing worker-lineage start debit. No atomic cross-service commit or automatic missing-state genesis is claimed. See [§12](#12-owner-root-activation-record-activation1). |
| Q28: activation consumer intake and outcomes | Open: Publish the actual activation-record verifier and its protected intake: dispatcher or protected operator installation path. Publish the authenticated accepted-state projection into the receiver-owned start gate; user-manager/launcher only authenticate its publisher and enforce their existing interlock, never activation signatures or owner intent. Keymaker signs only its protected dispatcher-request scope, not activation; broker v0 retains own policy/lease checks. Neither gains an owner-intent input. Retain original outcomes, currentness/custody, selected committed-start and native qualification gates. No blanket record-verification list or new downstream member/method is authorized. See [§12](#12-owner-root-activation-record-activation1). |

## 11. Implementation slices and evidence plan

Each slice starts with executable negative cases and nearby positive controls
bound to the approved contract. Names below are planned tests, not passing tests.
No production activation follows from mocks or two implementations agreeing.

| Order / gate | Negative tests first | Positive controls and scope |
| --- | --- | --- |
| 1. Pin contracts/activation | `TestUnpublishedProfileRefused`, `TestCrossRoleRootRefused`, `TestForeignPurpose5DomainRefused` | `TestPinnedS3ProfileAccepted`; accepted S3 and reviewed S4/custody/API gates before dependent activation. |
| 1a. S4 code re-pin before dependent slices | `TestS3PinCannotAdmitFreshness`, `TestS4ProfileWideningRefused`, `TestLegacyCurrentnessNotWeakened` | `TestPinnedS4ProfileAccepted`; explicit dependency/contracts/pins and fixture re-pin to F, preserving original refusal suites; separate review before implementation uses S4. |
| 1b. S5 code re-pin before receipt adapter (7a/7b) | `TestS4PinCannotExposeReceipt`, `TestS5ProfileWideningRefused`, `TestS5RepinPreservesLegacyRefusals` | `TestPinnedS5ReceiptProfileAccepted`; separately update module/contracts/pins and fixtures to S5 `5f4e7435d75697385ff6ce15a7501ac6f31c8ea5`, preserving prior refusal suites. Until reviewed re-pin, code stays at S4 `f76a400ebeb599b95a9bf9b2c59b522bef0392d8`. |
| 2. Pure intake/service adapter | `TestWrongIssuerScopeRefused`, `TestInvalidSignatureBeforeConflict`, `TestDuplicateCCJMemberRefused`, `TestOversizeNoMutation`, `TestUnknownKeyClassRefused`, `TestServiceForbiddenRoleRefused`, `TestServiceMissingParentRefused`, `TestServiceDepthZeroRefused`, `TestServiceRevokedParentRefused`, `TestServiceRevokedAttestationRefused` | `TestAuthorizedSignedRecordAccepted`, `TestServiceDelegationAccepted`; exact S3 profiles and parent/revocation checks, no effects. |
| 3. Serialized durable storage/bounds | `TestChangedBytesSameIDRefused`, `TestConcurrentHeadConflict`, `TestSyncFailureFences`, `TestSequenceExhaustionNoWrap`, `TestPublicationProofCapRefused`, `TestWitnessWrapperCapRefused` | `TestExactRetrySameOutcome`, `TestCrashReconcilesExactCommit`, `TestBoundedCompleteProofAccepted`, `TestWrapperFitsBeforeIntent`; no writes/intent on preflight refusal. |
| 4. Enrollment projection | `TestInvalidMirrorFencesWholeLoad`, `TestUnapprovedCommitRefused`, `TestTombstoneHistoryCannotDisappear`, `TestReservedSigsWinsSnapshotWide` | `TestOfflineEnrollmentSignedCommitAccepted`; unchanged P/R and approved E1 rotation. |
| 5. Journal/witness proof | `TestWriterCannotAppendWitness`, `TestStaleChallengeRefused`, `TestUnresolvedIntentFences`, `TestRestoredCloneCannotServe`, `TestSameHeadCheckpointForkRefused`, `TestAdvancingAnchorMissingRefused`, `TestAnchorTimeRegressionRefused`, `TestCheckpointTimeRegressionRefused`, `TestEightFieldProjectionRefused`, `TestPublicationLinkLossRefused`, `TestIntentOnlyPublicationRefused`, `TestDelayedProofExpiredRefused` | `TestExactIntentCommitResolution`, `TestLostResolutionReceiptFreshHead`, `TestAnchoredSuffixAccepted`, `TestValidatedProjectionReduced`, `TestPublicationBundleVerifiedAtIssue`; real ACL/crash/custodian/exclusion qualification, archive is not a current receipt. |
| 5b. Bridge composition (PENDING) | `TestBridgeRetryReceiptNotFreshRead`, `TestBridgeTransportTimeNotReadAt`, `TestBridgeObserverRoleRefused`, `TestBridgeRemoteUnderFinalLockRefused`, `TestBridgePhaseBudgetRenewalRefused` | Five paired controls below for §6.2; no code or serving activation. C/V/F, Q18–Q20 and WC1 G07/G10 remain gated (OR:design/owner-registry.md:760–764,796–814,1368–1370; WS:spec/witness-service.md:181–243; WC:spec/policy-v1.md:19–31; WC:UNRESOLVED_QUESTIONS.md:28). |
| 6. Base snapshots/time/capacity | `TestHeadChangeStaleSnapshotRefused`, `TestHeadChangeOldSnapshotRefRefused`, `TestHeadChangeMissingRecordRefused`, `TestHeadChangeMissingReissueRefused`, `TestHeadChangeMissingPriorCheckpointRefused`, `TestHeadChangeWrongPriorCheckpointRefused`, `TestStaleSnapshotRefused`, `TestEqualIssueTimeDifferentBytesRefused`, `TestCumulativeRemovalRefused`, `TestGapRefused`, `TestSplitHeadSignalsAndRefuses`, `TestSnapshotCapacitySignalsWithoutDrop`, `TestTimeLossNoPublication` | `TestHeadChangeReissueAccepted` (P-JRN-publish-reissue at F), `TestFreshCompleteSnapshotAccepted`, `TestConsumerAgeBoundary`, `TestMatchingGossipHeadAccepted`; identical cumulative set reissued at greater issued_at, same-transaction add/revocation/1 entry and new checkpoint snapshot reference; issuer profiles, byte/count limits and provider qualification. |
| 6a. Interval-aware time adapter before any activation | `TestPointTimeViewCannotAccept` (`O=100`, `δ=1`, `A=101` refuses because `U=A`), `TestConsumedHighWaterNotPriorWatermark`, `TestCoarseRoundingNeverAccepts` | `TestIntervalReceiptAccepted`; complete bound receipt with `L`, `U`, exclusive `A`, boot and sample id, native operation/challenge binding, distinct provider history and consumer watermark, conservative rounding with original and converted predicates (§9). This slice implements the pure interval projection for slices 6/7. The timeconf adapter, complete receipt validation and operation/challenge binding remain planned qualification gates. |
| 6b. QT-2c schemas/use and frozen-content decision (PENDING) | `TestWindowDoubleCheck`, `TestWindowBurnedDifferentH`, `TestWindowUseMetadataMissing`, `TestWindowPostIntentInventoryChanged`, `TestWindowUseRestoreDespiteProviderW` | Exact unused-revision composite commit and independently reconciled content/completion/use history (§9.1.4, §9.1.8); Q18 decided (i); schema, authority and qualification pending; native mapping to T's published template first. No code/codec/adapter activation. |
| 6c. C/V/F final-window integration (PENDING) | `TestWindowUseAdmissionBeforeCapture`, `TestWindowLockedHeadChanged`, `TestWindowLostCommitAck`, `TestWindowFreshnessRevalidation`, `TestWindowOriginalOrBudgetSubstitution` | Exact matching locked candidate, recovered original outcome, fresh Retry floors without another publication position; §9.1.2–§9.1.5 paired controls. All named current locks plus approved common exclusions; staged signing/reader/witness work outside them. |
| 6d. Linux and macOS durable timing qualification (PENDING) | `TestWindowStaleCapture`, `TestWindowTimelyAndLateAct`, `TestWindowDeadlineEquality` | Actual on-time durable act/use with one native suspend-aware admission read and qualified f for each window/OS, plus applied narrowing controls (§9.1.8). Synthetic arithmetic alone cannot close Q19/Q20 or handoff_unqualified. |
| 7. S4 publisher/currentness after re-pin | `TestFreshnessMissingRefused`, `TestFreshnessStaleRefused`, `TestFreshnessWrongRootRefused`, `TestFreshnessWrongSnapshotRefused`, `TestFreshnessWrongHeadRefused`, `TestFreshnessSameSeqDifferentBindingRefused`, `TestFreshnessBackwardSeqRefused`, `TestFreshnessBackwardTimeRefused`, `TestFreshnessPredecessorMissingRefused`, `TestFreshnessHeadRaceRefused`, `TestFreshnessUnresolvedIntentRefused`, `TestSlowWitnessClosesFreshness`, `TestFreshnessHistoryOmissionRefused`, `TestRestoredFreshnessStateFences`, `TestOldS3RejectsAgedBase`, `TestFreshnessCannotReviveAuthority` | `TestFreshnessKeepsJournalHead`, `TestRealChangeUsesWitnessTransaction`, `TestAgedBaseAcceptedByVerifyFreshness`, `TestFreshnessAtomicRetry`; identical base bytes and unchanged registry/witness positions on healthy freshness; exact effects and boundaries verified, never a global old-verifier waiver. |
| 7a. S5 receipt composition before K3/D12 | `TestReceiptWrongBindingRefused`, `TestReceiptAtExpiryRefused`, `TestReceiptExtensionRefused`, `TestReceiptAfterRestartRefused`, `TestNonFreshnessReceiptIssuerRefused`, `TestLegacyOldBaseCompositionRefused` | `TestFreshnessReceiptCompositionAccepted`, `TestCurrentSnapshotLegacyCompositionAccepted`; merged S5 producers return `Result.FreshnessReceipt`, used via Context after admitted-plan persistence; exclusive fixed expiry, R1–R3 and origin-only audit; row 1b re-pin and adapter qualification remain gates. |
| 7b. Registry-owner intake with a retaining adapter | `TestQuietRegistryIntakeWithoutReceiptRefused`, `TestQuietRegistryIntakeRebuiltEvidenceRefused`, `TestRegistryIntakeWitnessUnavailableRefused` | `TestQuietRegistryIntakeWithReceiptAccepted` (more than 60 seconds of quiet, same Evidence), `TestReissuedRegistryIntakeEvidenceAccepted`; can start now, with admitted-anchor persistence acknowledgment from slice 7a required before receipt use. Retain the same immutable whole Evidence for the receipt's lifetime across genuine clock updates; preserve domain-specific submission and no effects on refusal. This registry-owner slice does not wait for D42(a) external-root currency ([design baseline:570–600](https://github.com/relux-works/swarma-owner-registry/blob/fa6e1ef2e02138a7c935672ec2436709dbc96cf6/design/owner-registry.md#L570-L600), [S5:955–965](https://github.com/relux-works/waggle/blob/5f4e7435d75697385ff6ce15a7501ac6f31c8ea5/spec/swarm-envelope-2.md#L955-L965)). |
| 7c. External-root currency under D42(a) (PENDING format change/pin) | `TestStaleGrantSnapshotWithRegistryReceiptRefused` (current S5 registry-only receipt cannot waive external-root age), `TestUnjournaledRootSnapshotWithRegistryReceiptRefused` (even with a current registry receipt) | `TestCurrentGrantSnapshotWithRegistryReceiptAccepted`; new pending pair: `TestJournaledRootSnapshotWithRegistryReceiptAccepted`, under the future format owner's specified rule. Wait for the multi-snapshot freshness change, its pin, publisher coverage and consumer rule; until then retain current operator-/grant-root own-age gates. No effects on refusal; no invented external-root receipt extension ([design baseline:617–621](https://github.com/relux-works/swarma-owner-registry/blob/fa6e1ef2e02138a7c935672ec2436709dbc96cf6/design/owner-registry.md#L617-L621), [S5 D42:205–228](https://github.com/relux-works/waggle/blob/5f4e7435d75697385ff6ce15a7501ac6f31c8ea5/UNRESOLVED_QUESTIONS.md#L205-L228), [S5 scope:998–1007](https://github.com/relux-works/waggle/blob/5f4e7435d75697385ff6ce15a7501ac6f31c8ea5/spec/swarm-envelope-2.md#L998-L1007)). |
| 8. Restore and consumer activation | `TestRestoredBytesCannotBootstrap`, `TestJointLossFences`, `TestStopWorksWithoutRegistry`, `TestNoCompletionOnIntakeAck`, `TestStoredProofNotCurrentAuthority` | `TestOffNodeExactCandidateRecovery`, `TestReviewedConsumerMigration`, `TestKeymakerPublicationAndStatus`; owner barriers, independent retained proofs/freshness and consumer-specific activation. |

Slice 5b requires the following paired controls; **all are PENDING**, with no
claim of executable coverage. Each negative must refuse before any further
visibility/use or freshness advancement; preserve already durable C/intent and its fence
for exact recovery. A forbidden RPC is prevented before dispatch. Applied
narrowing controls must fail the named negative rather than a setup error
(OR:design/owner-registry.md:796–822,1068–1080,1183–1198;
WS:spec/witness-service.md:197–214,506–514;
WC:UNRESOLVED_QUESTIONS.md:27–28).

| Planned negative | Nearby positive / acceptance bound | Source pair at pinned revisions |
| --- | --- | --- |
| `TestBridgeRetryReceiptNotFreshRead`: supply only an old exact-retry receipt as fresh resolution evidence; refuse. | `TestBridgeRetryThenFreshCompleteRead`: retain that original outcome, then authenticate a new challenged complete matching-resolution read before a new V handoff. | OR:design/owner-registry.md:760–764,782–814; WS:spec/witness-service.md:191–214,218–243,610–616. |
| `TestBridgeTransportTimeNotReadAt`: substitute TLS, checkpoint or server audit time for protected read completion; refuse `freshness_unavailable`. | `TestBridgeQualifiedCompleteReadAt`: original completion measurement and uncertainty bound to the exact complete read satisfy original and conservative projected age predicates. | OR:design/owner-registry.md:386–394,438–444,760–764; WS:spec/witness-service.md:267–282,598–604; WC:UNRESOLVED_QUESTIONS.md:24. |
| `TestBridgeObserverRoleRefused`: even an explicitly authorized Observer reader cannot act as registry Bridge; deny append/bridge admission. | `TestBridgeExactPrincipalPinAndReader`: bound Bridge with exact principal/pin and explicit readers membership passes local ACL checks only; separately omit reader membership and refuse the read. | OR:design/owner-registry.md:74–76,782–788; WC:policy/binding.go:25–51,100–121; WC:spec/policy-v1.md:19–31. |
| `TestBridgeRemoteUnderFinalLockRefused`: attempt provider, witness or custodian RPC while any final lock is held; prevent dispatch and refuse admission. | `TestBridgeRemoteThenFreshLocalWindow`: complete remote phase outside final locks, then obtain fresh phase handoff and perform bounded local atomic act/use under locks. | OR:design/owner-registry.md:796–814,1063–1080; WS:spec/witness-service.md:206–214,495–504; WC:UNRESOLVED_QUESTIONS.md:23,28. |
| `TestBridgePhaseBudgetRenewalRefused`: expire original D after intent, then supply a newly reset phase allowance; refuse late C/V authority. | `TestBridgeOriginalRemainingBudget`: all phases/pages/retries fit the unchanged original deadline and each distinct fresh local window is admitted within its remaining budget. | OR:design/owner-registry.md:796–814,976–978,1172–1186; WS:spec/witness-service.md:239–243,591–593; WC:spec/policy-v1.md:100–106; WC:UNRESOLVED_QUESTIONS.md:28. |

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
| Consumer composition | `TestLegacyOldBaseCompositionRefused` (legacy own-age refusal as in V's TestFreshnessCompositionBoundary), all slice 7a receipt, 7b registry-owner intake and 7c external-root refusals | `TestCurrentSnapshotLegacyCompositionAccepted`, `TestFreshnessReceiptCompositionAccepted`, `TestQuietRegistryIntakeWithReceiptAccepted`, `TestReissuedRegistryIntakeEvidenceAccepted`, `TestCurrentGrantSnapshotWithRegistryReceiptAccepted`; retain S5 re-pin/adapter qualification, with new external-root currency additionally requiring row 7c's format change/pin |

Additional planned S5 adapter controls (after row 1b; no execution claimed):

| Contract | Planned negative / exact boundary | Named positive controls |
| --- | --- | --- |
| Atomic admitted-anchor transition | `TestReceiptMintAnchorRefused` (`freshness_receipt_binding_mismatch` for the old producer anchor on an advancing proof); no use before atomic admitted-plan persistence | `TestReceiptAdmittedAnchorAccepted` with exact persisted tip anchor/watermark and recovery audit where applicable |
| Same Evidence across genuine clocks and exclusive expiry | `TestReceiptClockUpdateAtExpiryRefused` (`freshness_receipt_expired` at E-margin); existing `TestReceiptExtensionRefused` | `TestReceiptClockUpdateSameEvidenceAccepted` with a new immutable Verifier and genuine later clock before E-margin |
| Rebuild and whole-map premise changes | Existing `TestQuietRegistryIntakeRebuiltEvidenceRefused` with byte-identical snapshots; `TestReceiptOtherSnapshotPremiseChangedRefused` (both `freshness_receipt_binding_mismatch`) | `TestQuietRegistryIntakeWithReceiptAccepted` with original Evidence; `TestRemintedWholeEvidenceAccepted` only after new full F admission |
| Upfront snapshot-submission path | `TestReplacementSnapshotWithOldReceiptRefused`, `TestNonRegistrySnapshotWithRegistryReceiptRefused` (`freshness_receipt_binding_mismatch`); `TestStaleSubmittedSnapshotRefused` retains own-age refusal, never retries a refused receipt as legacy | `TestReplacementSnapshotOwnAgeSubmissionAccepted`, `TestNonRegistrySnapshotOwnAgeSubmissionAccepted` use new/current submitted bytes with no receipt; `TestCommittedSubmissionFreshnessReceiptAccepted` obtains a new receipt only after committed-head F verification |
| Validate-only service, no fallback | `TestServiceInvalidReceiptNoFallbackRefused` (`freshness_receipt_binding_mismatch` despite current grant snapshot), `TestServiceRequiredReceiptMissingRefused` (`freshness_receipt_unavailable`); unchanged grant own-age refusal | `TestCurrentServiceWithValidReceiptAccepted`; `TestCurrentServiceWithoutReceiptAccepted` only when optional legacy mode was chosen up front; neither extends grant age |

All normal/re-anchor refusal and revoked controls must assert zero plans and
unchanged anchor/watermark. Acceptance controls must assert the exact normal
IDs/watermark and absence of reanchor, or the exact recovery endpoints/skip count,
and durable atomic persistence before use. These are future implementation tests,
not claims that documentation inspection executed or qualified those paths.

Content-review acceptance of this design precedes code. Later implementation evidence
must distinguish unit coverage, negative controls, crash/ACL qualification and
consumer activation; existing services and paused formats remain separate work.

## 12. Owner-root activation record (activation/1)

Status: **inactive design proposal**. Nothing is signed, installed, qualified or
activated by this section. It proposes the missing owner authorization for one
trusted, nonfederated Linux worker and one physical launch with owned-run cancel.
It grants no delegation, account creation/recreation, automatic restart, general
deployment or macOS activation. Accepted component evidence and exact-content
owner approval remain prerequisites, not consequences of a signature
([dispatcher scope][activation-dispatch-scope],
[qualification remains unavailable][activation-broker-status]).

### 12.1 Signing authority, domain and canonical bytes

Only the owner's **external protected root**, explicitly pinned for the
owner-activation role, may authorize this profile through the owner's isolated
closed-profile Signer path. That path must construct and validate this exact
profile; it cannot expose arbitrary-preimage signing. It is not the online
registry-owner Signer from Q3. The activation key is distinct from the operator
root key, not an alias for that role. Neither that operator root, the device
operator-act key, purpose-5 certification root, grant-service subject nor station
purpose-6 key can substitute.
No existing root gains this new role implicitly; the actual root identity,
algorithm/profile acceptance, pins and historical pin transitions remain Q23
([existing root separation and Signer limits][activation-roles],
[device operator verification][activation-device],
[station-caller exclusions][activation-station]).

Propose the distinct ASCII domain **`swarma-owner activation/1`**, with the
closed signing profile **`owner-activation/1`**. Purpose **7** is free in the
Keymaker inventory but is not reserved for this external root. Keymaker never
derives, imports, holds or signs with the activation key. No Keymaker purpose is
required unless a later registry-wide allocation decides otherwise. Q22 retains
complete purpose/domain inventory and consumer review; no derivation path, native
method, key provisioning or purpose-7 capability is allocated here. Existing
purposes 1–5 and the proposed station purpose 6 retain their own roles. The new
literal prefix differs from registration/enrollment, revocation/checkpoint/
freshness/attestation, broker grant, dispatcher request and witness/custody
profiles; root-role pinning is an additional boundary, not replaced by the prefix.
No profile alias or cross-domain signature reuse is permitted
([purpose inventory][activation-purpose],
[station purpose and profiles][activation-station],
[dispatcher preimage][activation-dispatch-bytes],
[broker grant preimage][activation-broker-grant],
[registry root profiles][activation-roles]).

Use registrar CCJ-1 and its body-envelope construction, not native socket JSON
or the device operator-act envelope. The proposed root signature algorithm is
Ed25519, subject to Q22/Q23 acceptance; P-256 device signatures cannot be used as
an automatic substitute. The envelope has exactly `body`, `id`, `sig`:

```text
preimage = ASCII("swarma-owner activation/1") || 0x0a || CCJ-1(body)
id       = "sha256:" || lowercase_hex(SHA-256(preimage))
sig      = canonical_base64url_no_pad(Ed25519_signature(preimage))
```

There is no final LF, recursive member stripping, hash-instead-of-body signing,
transport frame or caller-selected domain. Neither id nor sig enters the body
preimage; `intent_id` below is a separate original-operation identifier. Reuse
CCJ-1's precise UTF-8, duplicate/escaped-alias rejection, closed nested objects,
integer-only safe range, depth limit, scalar key ordering, escapes and lack of
Unicode normalization. Reuse strict public-key/signature and canonical base64url
checks beyond shape validation. These are proposed bytes for this profile, not
an assertion that current registrar or Keymaker APIs accept the new domain.
Finite record/reference/identifier limits must be published under Q24; absent
limits or an incomplete nested grammar close parsing/signing, rather than
borrowing a transport cap ([CCJ-1 and envelope rules][activation-ccj],
[external-domain API boundary][activation-dispatch-bytes]).

### 12.2 Closed body and existing-record references

The proposed body has exactly **`v`, `kind`, `scope`, `artifacts`,
`qualified_tuple`, `validity`, `intent_id`**. All are required; no extensions,
optional wildcard, inline artifact body, private path or discovery URL is allowed.
`Digest` below is exactly 64 lowercase hex digits identifying SHA-256 of the
complete retained existing artifact bytes. Resolve it under that artifact owner's
protected source and typed verifier; do not reserialize the artifact using this
profile, replace its owner-defined ID, or treat digest equality as authority.
These are references to existing records, not replacement record formats
([broker record identity][activation-broker-record],
[broker publication validation][activation-broker-publication],
[dispatcher typed-reference boundary][activation-dispatch-forms]).

| Member | Proposed closed type and required meaning |
| --- | --- |
| `v` | Integer exactly `1`. |
| `kind` | String exactly `"activation/1"`; no operation branch or implicit withdrawal command. |
| `scope` | Exactly `{audience,policy}`. `audience` is the exact protected installation audience string. `policy` is the Digest of the operator-controlled dispatcher policy defining the intended receiver, purpose-6 caller association and caller UID/generation, separately pinned executor worker UID/generation, explicit protected caller-to-worker mapping, plan pins, one-start scope and owned cancel. Never copy that policy into this body or infer audience/receiver from a hostname. Every consumer must match its protected intended role/receiver in that same scope ([dispatcher policy][activation-dispatch-forms], [two-role requirement][activation-two-roles], [protected audience][activation-device]). |
| `artifacts` | Exactly `{qualified_list,activation,policy,cutover}`, each a Digest. They reference the broker's complete `qualified-list/1`, existing `qualification-activation/1` marker, complete protected broker static configuration/policy, and the operator's durable phased cutover record, respectively. This `policy` is the broker configuration, distinct from `scope.policy`. Preserve all source/build/binary/schema/configuration/plan/catalog/profile/adapter/review and qualification evidence through those owners' accepted reference closures; no convenient version-only projection or research-head-for-installed-binary substitution ([broker policy][activation-broker-policy], [list/marker][activation-broker-marker], [cutover][activation-cutover]). |
| `qualified_tuple` | Digest of the complete existing `qualification/1` record selected by the referenced whole qualified list. Its identity, case/approval/artifact closure and current observations must cover the exact native installed tuple, including the purpose-6 caller and executor worker as distinct roles with their own UID/generation references and the protected mapping reference, every other cross-service identity/generation, OS/kernel/architecture/boot, service-manager/property/build/exec profile, routing/account/receiver/key/epoch and custody/time/backend/exclusion fact needed by the selected scope. It is not a new tuple/manifest schema ([record][activation-broker-record], [current observer limits][activation-broker-observers], [consumer native tuple][activation-dispatch-gates], [two-role retention][activation-bootstrap]). |
| `validity` | Exactly `{occurrence,start,end}`. `occurrence` is the retained original authorization occurrence identifier, an exact nonempty string; it is not allocated afresh on retry. `start` and `end` are safe JSON integers denoting whole UTC seconds, with `start < end`, interpreted only through qualified time/elapsed/boot continuity. Admit only when the entire qualified interval lies in `[start,end)`, intersected with every component's original, possibly shorter deadline. Q25 selects the actual source, conversion and continuity contract; no default TTL or "until replaced" is authorized ([original-time requirements][activation-time], [retained deadlines][activation-replay]). |
| `intent_id` | Exact nonempty original owner-intent identifier retained by the operator's original installation/cutover operation. Bind it immutably to this body, its id and occurrence. It is not a native signer request ID, the envelope id or a new execution/result receipt. Q24 must settle its existing-operation mapping and finite encoding limits; this field supplies no new ID allocator ([cutover retention][activation-cutover], [original operation/effect retention][activation-replay]). |

The six semantic requirements map as follows: scope/audience to `scope`;
immutable artifacts to `artifacts`; complete qualified tuple to `qualified_tuple`;
finite original validity to `validity`; revocation/currentness bindings to those
same exact policy, qualification and cutover references plus independently
protected root/history facts; original human intent to `intent_id`. There is no
caller-supplied `current`, `approved`, `enabled`, revocation verdict or substitute
root-state blob in this body. Authoritative withdrawal/currentness is an admission
input under §12.4, not a Boolean that the signature makes true
([policy/currentness ownership][activation-dispatch-current],
[broker marker scope][activation-broker-publication]).

The caller and executor worker are distinct roles. Neither role can be derived
from one station UID. Even same-account equality must be selected explicitly in
protected policy. Both `scope.policy` and `qualified_tuple` must retain both
UID/generation references and that mapping through their existing typed closure;
this section adds no guessed native policy member. B4 pins both roles. Broker
v0 setup allowlists the exact executor worker UID, and its peer-scoped `status`
positive control must come from that worker and return the expected policy
generation and account. Caller-UID evidence cannot substitute; when distinct,
the caller is among the foreign-UID empty/denied controls. A lease request is
never a setup probe. Setup remains separately bounded and qualified, with
admission closed and no active lease during policy replacement
([roles][activation-two-roles], [setup/status][activation-worker-status],
[B4/B5 retained mapping][activation-bootstrap]).

**Q24 remains a signing gate.** The broker record currently names its own measured
identity and artifacts; it does not yet publish the complete cross-service tuple
or all accepted-review/intent bindings required here. Its applied-property
observer and required live rows remain unavailable. The cutover record is a
phased prose contract, not an already published root-signing/result envelope.
The owning services must publish and jointly accept the complete typed reference
closure and mappings in their existing records before a body can be instantiated.
Do not create an auxiliary tuple/approval/cutover schema, use an opaque manifest,
or fill missing installed evidence with source commit pins. No usable signing
example, installed digest, key or pin set is supplied here
([current qualification status][activation-broker-status],
[observer limits][activation-broker-observers], [cutover][activation-cutover]).

### 12.3 Ordered verification and consuming obligations

The actual verifier of the root-signed record is the dispatcher, or the
protected operator installation path when establishing protected state. Only
that verifier applies the nine steps below to the signature, body, complete
references and currentness. It closes NEW starts at the dispatcher-owned or
protected start gate when permission expires or is withdrawn. Missing limits,
protected inputs, closure evidence or qualified integration keeps activation
unavailable; unknown closure proves no stopped execution or settled debt.
Downstream services consume their own accepted facts, not this record. The
user-manager authenticates the publisher of its receiver-owned gate state;
Keymaker uses its protected caller association and operator-installed scope;
broker v0 continues its own policy checks. None gains an owner-intent input.

| Order | Required check; refuse before proceeding on failure |
| --- | --- |
| 1 | Enforce the selected finite byte/count/depth limits while reading; parse the envelope/body and every required nested type with the closed-member and CCJ-1 integer rules. Never repair an unknown field or incomplete schema. |
| 2 | Select only the fixed activation domain/profile from protected consumer policy, reconstruct the exact preimage and verify its id. A caller cannot supply another prefix, frame, body digest or signing plan. |
| 3 | Resolve the external owner root from protected pins and independently retained historical authorization for this owner/profile/occurrence. Received keys, claims or artifact digests choose no root. Known historical authorization does not bypass later withdrawal. |
| 4 | Verify the exact preimage signature using that permitted root/profile, including strict Ed25519/key/encoding checks. Device, station, registry and foreign-domain signatures refuse. |
| 5 | Match installation audience, intended receiver/consumer, operation scope, independently pinned caller and executor worker UID/generation references, their explicit protected mapping and original intent. No wildcard, adoption or delegation follows. |
| 6 | Resolve every referenced artifact and its complete typed closure through its owner; independently reverify whole bytes/digests, all list entries/records/approvals/case artifacts, exact current observations, policy/plan provenance, accepted revisions and cutover phase. A marker, digest or broker record alone cannot establish the other owners' facts. |
| 7 | Check the finite validity interval against qualified time/elapsed/boot continuity and all original shorter component deadlines. Unknown time or equality at expiry closes forward authority; no renewal through another receipt/window. |
| 8 | Recheck authoritative root/key/policy/qualification/generation/configuration/version/custody/time/restore currentness and explicit owner withdrawal, using independently protected monotonic histories and exclusions. Missing or stale revocation/currentness evidence refuses. |
| 9 | Under the owning durable consumption transaction, revalidate the exact admitted revisions and reserve/consume the original occurrence once before its forward effect. Retain the immutable intent/body binding, outcome/unknown state, tombstones, original debit and cleanup/stop obligations. Verification alone consumes nothing and issues no effect. |

This order composes the existing closed-byte/root/authority boundaries, whole-list
publication checks and retained currentness/replay requirements; the new mapping
and implementation still need independent consumer acceptance
([CCJ-1][activation-ccj], [protected root/history facts][activation-device],
[whole-list checks][activation-broker-publication],
[protected currentness][activation-dispatch-current],
[original replay/effects][activation-replay]).

Repeated checks by the dispatcher or protected installation verifier may not
create repeated activation or launch effects for one occurrence. Same
intent/occurrence and identical body recover only the original outcome; a
changed body conflicts. Restart, byte-identical
reinstall, restored clone, a new signature or a newly named occurrence cannot
replenish the launch debit for the original worker/policy lineage. Keep retained
consumption, pending effects and tombstones across restart/restore/clone and
reconcile external surviving floors with enforceable old-instance fencing before
forward reuse. Missing history is unknown, never unused. Q27 must assign the
replay store owner and cross-consumer atomicity/recovery protocol; no atomic
cross-service commit or accepting store is claimed. Current activation authority
is needed for unperformed enablement work; existing broker lease activity follows
§12.4. Original observation/reconciliation and reduce-only stop use their own
retained authority ([dispatcher replay][activation-replay],
[restore and instance fences][activation-restore],
[proposed independent custody/restore][activation-custodian-restore],
[lifetime exclusion remains unqualified][activation-custodian-exclusion]).

These are obligations on the owning services, not implemented registry calls:

| Consumer / call site obligation | Required retained boundary |
| --- | --- |
| Dispatcher, or protected operator installation path: actual activation-record verifier | Verify the root signature, closed body, complete owner-reverified references and currentness before installing accepted protected start state or permitting NEW starts. Bind the original intent to the exact policy, caller association and UID/generation, independently pinned worker UID/generation, explicit mapping, tuple and one-start ledger. Reconcile the original outcome; do not delegate verification to downstream own-fact consumers ([prepare/currentness][activation-dispatch-current], [replay][activation-replay]). |
| User-manager and launcher: receiver-owned start-gate consumers, not activation-record verifiers | They never verify `activation/1`, signatures or owner intent and gain no input or member for them. Authenticate the publisher of accepted protected gate state, then enforce the existing generation/claim/controller interlock and selected start contract. Root receipt and verified privilege drop precede C; C before Rv or expiry retains the preparatory child and every resumer as original stop work. Qualify the residual interval and independent stop within the earliest original bound; C must precede Rv and expiry; otherwise refuse. The selected committed-start contract is unchanged; no strict-native-release alternative is selected ([protected publisher][activation-manager-publisher], [selected start][activation-manager-start]). |
| Keymaker: no activation-record verifier | It signs only `dispatcher-request/1` under the protected caller association (an operator-installed B0–B5 scope). The signature authenticates exact request bytes and never authorizes a start. Owner activation gates NEW starts at the dispatcher; Keymaker gains no owner-intent input, field, method or code. Do not send this body to `sign_station_caller` or make purpose 6 sign it. B4 is dispatcher policy installation; B5 activates the operator's purpose-6 scope, not owner activation. Preserve own-fact association/currentness/custody checks and the accepted reduce-only owned-run cancel rules ([station profiles][activation-station], [B4/B5][activation-bootstrap], [cancel/currentness][activation-station-cancel]). |
| Broker v0: own-fact policy and lease checks only | No activation-record verifier or owner-intent input, field, RPC or command is added. Kernel-peer, account/profile, policy-generation, qualification and material-delivery checks remain its own. It does not observe owner withdrawal or enforce this permission's expiry. Planned removal needs no unpublished interceptor, new-lease closure or immediate policy replacement; credential material continues under broker policy until worker stop, with separate lease/token debt ([lease lifetime][activation-broker-lifetime], [policy replacement][activation-broker-reload]). |

Q28 must publish the dispatcher's or protected installation path's actual
verification/intake and authenticated publication of accepted protected start
state. The user-manager/launcher only consume that gate state through their
existing interlock after authenticating its publisher. Keymaker consumes its own
association and operator-installed signing scope; broker v0 consumes its own
policy and lease facts. No blanket record delivery or verification by downstream
services is authorized. Retain each owner's original installation/outcome and
native qualification obligations; no new launcher member, broker install API,
generic signing endpoint or parallel result schema is introduced. The accepted
projection/intake, selected start boundary and qualification remain Q28 gates,
not permission to execute an integration plan
([dispatcher gates][activation-dispatch-gates],
[protected publisher][activation-manager-publisher],
[selected start][activation-manager-start]).

### 12.4 Withdrawal, currentness, order and closed gates

Planned expiry or withdrawal of the owner's enable-only permission closes NEW
starts at the dispatcher-owned or protected start gate. Work already committed
under the selected start contract stops by its earliest ORIGINAL qualified stop
bound, including the bounded residual interval and every resumer. Credential
material continues under the broker's own policy until worker stop; lease and
token debts remain separately tracked. This case requires no unpublished broker
interceptor, new-lease admission closure or immediate policy replacement. The
owning start-gate publisher retains actual closure evidence; unknown completion
is unconfirmed and does not prove worker stop or settled effects.
The actual verifier and the services enforcing their own facts close affected
scope on authoritative invalidation or unknown currentness of the signing root/key,
protected policy or accepted qualification; changed configuration, binary/version,
caller/worker/controller generation or association; invalid plan/catalog/routing/
account/receiver/key/master epoch; and lost custody/time continuity, restore floor,
replay history or instance exclusion. These remain the owning verifier and
own-fact interlock/currentness obligations, not activation inputs for downstream
services. Each owner checks its own facts at preparation and its selected effect
or release boundary. A new generation of either role requires a new protected
association, policy and verified executor-worker allowlist entry; equal
UID/public bytes never adopt an old generation automatically. A UID-only broker entry proves no generation
([new-generation setup][activation-manager-setup],
[recreate/currentness][activation-recreate]). Q26 must publish
the authoritative withdrawal/revocation delivery, detection bounds, continuity
and acknowledgment rules; a signed intent cannot attest its own continuing
validity ([currentness/invalidation][activation-dispatch-current],
[receiver invalidation][activation-manager-invalidation],
[restore fences][activation-restore]).

Where registry publication carries the external root state, preserve D42(a):
that external root signs changed content, while registry freshness proves only
the currency allowed by the format owner's accepted multi-snapshot profile.
The root Signer application must check that its exact submission was journaled;
intake acknowledgment alone is insufficient. Current single-snapshot freshness
cannot waive external-root own-age checks or prove this activation remains
unwithdrawn. Q17's format change/pin, publisher coverage and consumer rule remain
gates; this section widens no existing registry wire/intake profile. If root
currentness is delivered outside that path, Q26 must identify and qualify its
independent authority/transport/restore protocol instead; none is guessed here
([existing D42(a) boundaries][activation-currency],
[root Signer inclusion obligation][activation-root-inclusion]).

Expiry, refusal, withdrawal or changed tuple closes forward enablement while
preserving every original unknown effect, consumed launch entitlement, claim,
lease/token/custody/output disposition and stop/cleanup debt. Under unchanged
broker policy, an existing lease retains its original `authorized_until`; renewal
may replace material under the existing v0 checks but never extends that deadline.
Successful policy-generation replacement is a separate action that invalidates
old-generation leases and connections earlier; no lease-survival-across-reload
guarantee is made ([lease methods/lifetime][activation-broker-lifetime],
[retained renewal deadline][activation-broker-renewal],
[policy-generation invalidation][activation-broker-reload]).

Admission closure, worker stop, material delivery, `lease.release` or lease expiry,
and vendor-token invalidation are separate facts. A failed `lease.release`,
including an old-generation refusal, retains unresolved debt and is not an
acknowledgement. Neither local closure nor a lease receipt proves immediate
vendor-token revocation. Authorized original observation/reconciliation and
reduce-only stop retain their own authority; no new occurrence, signature, reboot
or retry clears debt ([old-generation refusal][activation-broker-release],
[retained effects/debit][activation-replay]).

Planned permission-window expiry preserves that earliest original stop bound;
it is not a security trigger or security-revocation guarantee. Genuine tuple
security revocation remains a separate closed Q26/Q28 gate. A first-run canary
using this record carries an explicit limitation: on a real security event the
operator manually stops the worker and replaces the broker policy. The
**detection-to-policy-replacement-commit issuance window** is a named limitation,
not a fence; material may still be issued between detection and the replacement
commit. The record and canary claim no conformance to the platform's
execution-isolation security-revocation requirement. No new-lease interceptor is
selected as a substitute response. Publish and qualify authoritative detection,
response bounds and complete stop/effect endpoints before claiming that property.
This may only tighten existing deadlines; publication, acknowledgement or retry
cannot restart an original stop or security-response obligation
([receiver stop/deadline rules][activation-manager-stop],
[policy-replacement boundary][activation-broker-reload]).

Order is: separately accepted artifacts and native qualified tuple; exact-content
review and owner-root Signer authorization; dispatcher or protected installation
verification and authenticated protected-state publication; downstream own-fact
interlocks and each owner's retained installation/activation outcome;
only then any canary. Qualification that itself needs live approval retains its
separate explicit scope; this record supplies no live-test authorization. A
coordinating party may transport the reviewed request and response but does not
sign for the owner. Signature delivery is not execution acknowledgment, a cutover result
or proof of canary success. Do not pre-fill future canary/load results into the
phase being authorized; each later phase retains its own evidence. Lost or
one-sided acknowledgments remain closed and reconcile the original operation
([phased cutover][activation-cutover],
[qualification approval/publication boundary][activation-broker-status],
[dispatcher installation and replay][activation-dispatch-current]).

WC1 now publishes an independent custodian protocol v1 proposal: it remains
a proposal, with no accepted custody wire envelope or qualified provider. Its
separate keys/pins, original-evidence confirmation, current-control and lifetime
exclusion obligations apply only through accepted consumer mappings; it supplies
no implicit registry enrollment, activation root or replay-store owner
([proposal scope and wire boundary][activation-custodian-scope],
[restore][activation-custodian-restore],
[exclusion][activation-custodian-exclusion]).

All signing, installation, serving, native timing/custody, consumer intake and
canary gates remain closed. Q22–Q28 in §10 are publication/acceptance dependencies,
not discretionary warnings. No owner decision accepting this new profile,
actual key, validity interval or deployment is asserted by this proposal.

### 12.5 Paired documentary controls — all PENDING

These are named design counterexamples, not executed tests, implementation
coverage or qualification. Every refusal/unknown case keeps the original intent,
debit, effects and stop obligations; it grants no new enablement authority.
Existing broker lease activity follows the separate §12.4 rules.

| Named case | Nearby valid case, conditional on every gate | Concrete refusal / retained boundary |
| --- | --- | --- |
| `ActivationVerifierOwnership` | Only the dispatcher or protected operator installation verifies the root record; downstream services use their own facts. User-manager authenticates the protected gate publisher, Keymaker signs its protected request scope, and broker v0 uses its own policy. | A blanket obligation for every consumer to receive or verify the record is invalid even when accompanied by no-new-method wording. Passing owner signatures/intent to user-manager/launcher or Keymaker is forbidden ([publisher][activation-manager-publisher], [station scope][activation-bootstrap]). |
| `ActivationRootRoleSeparation` | A dedicated external owner-activation key has explicit protected role pins and independently verified historical authorization. | Reuse the same key in the operator-root or any other excluded role, or alias such a key as the activation key: refuse even if its cryptographic signature is valid. Domain separation cannot replace key/role separation ([roles][activation-roles]). |
| `OwnerScopeCurrent` | Pinned external owner root signs the exact activation domain/body for the intended installation/receiver and current referenced tuple/policies, with finite qualified validity and fresh authoritative withdrawal checks. | A cryptographically valid device/purpose-6/registry signature, wrong receiver, revoked root, unavailable purpose/schema or unknown revocation source refuses before activation. Digest/marker or signature success alone cannot authorize. |
| `CallerWorkerMapping` | The referenced protected policy and tuple pin the caller association/UID/generation and independently the executor worker UID/generation, with an explicit mapping; exact worker-UID status and the foreign-UID control satisfy qualified setup. | Substitute caller UID/status evidence for a distinct worker, infer both roles from one UID, omit either generation or adopt a recreated equal UID: refuse. Same-account use needs an explicit selected mapping and independent role checks ([two roles][activation-two-roles], [status][activation-worker-status], [B4][activation-bootstrap]). |
| `ChangedTupleAndClone` | Independently reverified unchanged native identity/generations, complete record/list/marker closure and surviving external currentness/replay/exclusion evidence recover only the original outcome/debit. | Changed binary/policy/configuration/generation, unknown boot continuity, retired/recreated equal UID, restored clone or restored byte-identical success refuses forward reuse; require owning requalification/reapproval/reconciliation, never a new occurrence or replenished start. |
| `OwnerPermissionLeaseSeparation` | Planned permission expiry/withdrawal closes NEW starts at the dispatcher-owned or protected start gate. Already-committed work retains its earliest original qualified stop bound; credential material continues under broker policy until worker stop, with separate lease/token debts. An unchanged-policy lease retains its original `authorized_until`; renewal never extends it. | Requiring an unpublished new-lease interceptor, NEW-lease admission closure or immediate policy replacement for planned expiry/removal is a contract violation. Equating permission expiry with lease/token expiry or settled stop debt also refuses this distinction ([lifetime][activation-broker-lifetime], [renewal][activation-broker-renewal]). |
| `SecurityRevocationWindow` | On a genuine tuple security event, the operator manually stops the worker and replaces broker policy; explicitly retain the detection-to-policy-replacement-commit issuance window and first-run limitation. The canary claims no execution-isolation security-revocation conformance. | Claiming a fence, immediate revocation or conformance before qualified detection/stop/effect endpoints, or substituting a new-lease closure requirement for the stated manual response, fails the limitation. Do not reintroduce that closure requirement through planned expiry ([policy replacement][activation-broker-reload]). |
| `PolicyGenerationLeaseSeparation` | Successful policy-generation replacement separately invalidates old-generation leases/connections before their original deadline; an authenticated successful release has its own receipt. | Do not infer lease survival across reload, treat an old-generation `lease.release` refusal as an acknowledgement, discard its unresolved debt, or infer vendor-token revocation from closure/expiry/release. Pair the unchanged-generation lease above with this earlier invalidation case ([reload][activation-broker-reload], [release guard][activation-broker-release]). |
| `ActivationOutcome` | Each owner durably retains its own authenticated installation/activation result bound to the same original root intent, with all current gates satisfied before a canary. | Lost reply, partial/one-sided opening, `enabled` flag, broker marker or delivered signature presented as full activation stays unknown/closed; reconcile the original outcome without duplicate effects or invented result receipts. |

Source boundaries for these pairs are the
[whole qualification publication check][activation-broker-publication],
[dispatcher scope/currentness/replay][activation-dispatch-current],
[original effect retention][activation-replay] and
[receiver start/stop gate][activation-manager-start]. All remain **PENDING**.

[activation-roles]: https://github.com/relux-works/swarma-owner-registry/blob/79d92d6521fd984e46b35238f848b7ff954a3ef0/design/owner-registry.md#L62-L110
[activation-ccj]: https://github.com/relux-works/swarma-registrar/blob/2219f70358fb7b5c7d66f229c80a8ba4a1fe0230/spec/registrar.md#L56-L109
[activation-purpose]: https://github.com/relux-works/swarma-keymaker/blob/14532907e28cbfda6d049955e5e2b7de0bf42554/spec/keymaker.md#L691-L707
[activation-station]: https://github.com/relux-works/swarma-keymaker/blob/14532907e28cbfda6d049955e5e2b7de0bf42554/spec/keymaker-station-caller.md#L8-L105
[activation-device]: https://github.com/relux-works/swarma-keymaker/blob/14532907e28cbfda6d049955e5e2b7de0bf42554/spec/keymaker-operator-record.md#L18-L108
[activation-dispatch-scope]: https://github.com/relux-works/swarma-dispatcher/blob/5558b30ca214745390d82fa3ea92aa85feb6dad5/spec/m0-launch.md#L11-L41
[activation-dispatch-forms]: https://github.com/relux-works/swarma-dispatcher/blob/5558b30ca214745390d82fa3ea92aa85feb6dad5/spec/m0-launch.md#L57-L89
[activation-dispatch-bytes]: https://github.com/relux-works/swarma-dispatcher/blob/5558b30ca214745390d82fa3ea92aa85feb6dad5/spec/m0-launch.md#L91-L115
[activation-dispatch-current]: https://github.com/relux-works/swarma-dispatcher/blob/5558b30ca214745390d82fa3ea92aa85feb6dad5/spec/m0-launch.md#L155-L179
[activation-dispatch-gates]: https://github.com/relux-works/swarma-dispatcher/blob/5558b30ca214745390d82fa3ea92aa85feb6dad5/spec/m0-launch.md#L363-L374
[activation-replay]: https://github.com/relux-works/swarma-dispatcher/blob/5558b30ca214745390d82fa3ea92aa85feb6dad5/spec/m0-launch.md#L225-L258
[activation-manager-start]: https://github.com/relux-works/swarma-user-manager/blob/5db5b8bbeba8af7ed6962c87586ef48a0cd5b0b3/docs/design/m0-start-invalidation.md#L87-L116
[activation-broker-record]: https://github.com/relux-works/swarma-credential-broker/blob/8b45a165fa528395da5f8b098ac722427c90dce5/internal/qualification/record.go#L19-L73
[activation-broker-policy]: https://github.com/relux-works/swarma-credential-broker/blob/8b45a165fa528395da5f8b098ac722427c90dce5/internal/policy/policy.go#L16-L42
[activation-broker-publication]: https://github.com/relux-works/swarma-credential-broker/blob/8b45a165fa528395da5f8b098ac722427c90dce5/internal/qualification/storage_linux.go#L482-L513
[activation-broker-status]: https://github.com/relux-works/swarma-credential-broker/blob/8b45a165fa528395da5f8b098ac722427c90dce5/docs/qualification.md#L1-L55
[activation-broker-observers]: https://github.com/relux-works/swarma-credential-broker/blob/8b45a165fa528395da5f8b098ac722427c90dce5/docs/qualification.md#L75-L90
[activation-broker-grant]: https://github.com/relux-works/swarma-credential-broker/blob/8b45a165fa528395da5f8b098ac722427c90dce5/spec/broker.md#L324-L329
[activation-cutover]: https://github.com/relux-works/swarma-credential-broker/blob/8b45a165fa528395da5f8b098ac722427c90dce5/spec/v0-codex-refresher.md#L184-L241
[activation-time]: https://github.com/relux-works/swarma-owner-registry/blob/79d92d6521fd984e46b35238f848b7ff954a3ef0/design/owner-registry.md#L1251-L1261
[activation-restore]: https://github.com/relux-works/swarma-owner-registry/blob/79d92d6521fd984e46b35238f848b7ff954a3ef0/design/owner-registry.md#L1080-L1122
[activation-currency]: https://github.com/relux-works/swarma-owner-registry/blob/79d92d6521fd984e46b35238f848b7ff954a3ef0/design/owner-registry.md#L611-L633
[activation-root-inclusion]: https://github.com/relux-works/swarma-owner-registry/blob/79d92d6521fd984e46b35238f848b7ff954a3ef0/design/owner-registry.md#L1770

[activation-broker-marker]: https://github.com/relux-works/swarma-credential-broker/blob/8b45a165fa528395da5f8b098ac722427c90dce5/internal/qualification/storage_linux.go#L323-L327
[activation-two-roles]: https://github.com/relux-works/swarma-keymaker/blob/14532907e28cbfda6d049955e5e2b7de0bf42554/spec/keymaker-station-caller.md#L223-L250
[activation-worker-status]: https://github.com/relux-works/swarma-keymaker/blob/14532907e28cbfda6d049955e5e2b7de0bf42554/spec/keymaker-station-caller.md#L275-L310
[activation-bootstrap]: https://github.com/relux-works/swarma-keymaker/blob/14532907e28cbfda6d049955e5e2b7de0bf42554/spec/keymaker-station-caller.md#L312-L363
[activation-recreate]: https://github.com/relux-works/swarma-keymaker/blob/14532907e28cbfda6d049955e5e2b7de0bf42554/spec/keymaker-station-caller.md#L547-L562
[activation-manager-setup]: https://github.com/relux-works/swarma-user-manager/blob/5db5b8bbeba8af7ed6962c87586ef48a0cd5b0b3/docs/design/first-run-launch-stop.md#L30-L40
[activation-custodian-scope]: https://github.com/relux-works/swarma-witness/blob/e90b8a2ec1f560167cae93b6078c7ccd0d1fce21/spec/custodian-v1.md#L1-L91
[activation-custodian-restore]: https://github.com/relux-works/swarma-witness/blob/e90b8a2ec1f560167cae93b6078c7ccd0d1fce21/spec/custodian-v1.md#L190-L239
[activation-custodian-exclusion]: https://github.com/relux-works/swarma-witness/blob/e90b8a2ec1f560167cae93b6078c7ccd0d1fce21/spec/custodian-v1.md#L249-L277

[activation-broker-lifetime]: https://github.com/relux-works/swarma-credential-broker/blob/8b45a165fa528395da5f8b098ac722427c90dce5/spec/v0-codex-refresher.md#L617-L630
[activation-broker-renewal]: https://github.com/relux-works/swarma-credential-broker/blob/8b45a165fa528395da5f8b098ac722427c90dce5/internal/lease/manager.go#L648-L679
[activation-broker-reload]: https://github.com/relux-works/swarma-credential-broker/blob/8b45a165fa528395da5f8b098ac722427c90dce5/spec/v0-codex-refresher.md#L96-L104
[activation-broker-release]: https://github.com/relux-works/swarma-credential-broker/blob/8b45a165fa528395da5f8b098ac722427c90dce5/internal/lease/manager.go#L511-L525

[activation-manager-publisher]: https://github.com/relux-works/swarma-user-manager/blob/5db5b8bbeba8af7ed6962c87586ef48a0cd5b0b3/docs/design/m0-start-invalidation.md#L40-L61
[activation-manager-invalidation]: https://github.com/relux-works/swarma-user-manager/blob/5db5b8bbeba8af7ed6962c87586ef48a0cd5b0b3/docs/design/m0-start-invalidation.md#L63-L83
[activation-manager-stop]: https://github.com/relux-works/swarma-user-manager/blob/5db5b8bbeba8af7ed6962c87586ef48a0cd5b0b3/docs/design/m0-start-invalidation.md#L233-L262
[activation-station-cancel]: https://github.com/relux-works/swarma-keymaker/blob/14532907e28cbfda6d049955e5e2b7de0bf42554/spec/keymaker-station-caller.md#L531-L562
