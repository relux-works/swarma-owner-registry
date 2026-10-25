# swarma-owner-registry

An owner registry service for the trust node, written in Go. The accepted
[service design](design/owner-registry.md) defines the later record, publication
and recovery work.

The first two slices implement protected contract pins, activation checks and
a pure signed-record intake adapter. A separate enrollment adapter orchestrates
complete approved exports through a caller-qualified transaction. A local
transaction store provides checked durability and recovery. There is no network listener or Git/
witness publication adapter. An admission or local commit verdict is not
publication, deployment or qualification evidence.

## Packages and protected configuration

`internal/contracts` freezes the reviewed S5 revision
`5f4e7435d75697385ff6ce15a7501ac6f31c8ea5`, six service-supported record types
(`attestation/1`, `grant/1`, `revocation/1`, `enrollment/1`, `journal-entry/1`,
`checkpoint/1`), three key classes (`agent`, `operator-device`, `service`) and
the exact D33 grant-service profile. `message/1` and `operator-act/1` have no
standalone intake profile here. `freshness/1` remains an internal registry publication in S5; it has no intake path.

`internal/pins.Parse` accepts caller-protected CCJ-1 JSON, at most 16 KiB and
eight container levels. It uses the pinned `record.Parse` and registrar `ccj`
reader; it refuses duplicate names, unknown members and invalid shapes. The
caller supplies trusted configuration bytes, never request or repository data.
No configuration file is opened by this package.

The top-level members are `revision`, `owner`, `object_format`, `roles`, `record_types`,
`key_classes`, `service_profile` and optional `activation`. The revision and
complete type/class sets must match the compiled contract; arrays cannot add,
duplicate or omit entries. Owner is a nonempty UTF-8 string of at most 255 bytes.
`object_format` is required and exactly `sha1` or `sha256`; omission, other
values and nonstrings refuse as `config_malformed`. There is no default or
inference from a revision. The registrar deployment and this service must share
the same protected Git object format. `Config.ObjectFormat()` returns that
immutable value, or an empty string for unavailable configuration.

Each required role is a closed `{key,purpose,scope}` object:

| Role | Local purpose label | Exact scope |
| --- | --- | --- |
| `registry_owner` | `registry-revocation` | The purpose-5 root key id |
| `commit_signer` | `git-commit` | Owner |
| `enrollment_publisher` | `enrollment/1` | Owner |
| `purpose5_root` | `purpose-5` | `purpose-5` |
| `operator_root` | `operator` | Owner |
| `grant_root` | `grant` | Owner |
| `grant_service` | `grant-service` | Owner |

Keys are exact canonical public key ids checked by the reviewed key validator.
All seven roles must differ pairwise. Purpose-5, enrollment publisher and
service subject use Ed25519. These local policy labels introduce no protocol
signing domains; the purpose-5 verifier fact uses integer purpose `5`.

`service_profile` is exactly
`{"key_class":"service","purpose":"grant-service","roles":["author"],"actions":["grant.sign"],"max_lifetime_seconds":86400}`.
Its issuer is the pinned grant root and its binding is the protected owner.
The intake adapter verifies signed identity, lifetime and current revocation; a
service identity alone supplies no parent grant or action authority.

`activation` is an object of optional booleans named `enrollment_intake`, `s4_freshness`,
`s4_verify_freshness`, `s4_reanchor`, `purpose5_runtime`, `custody`, `transport`, `time`, `witness`, `storage`,
`freshness_history` and `consumer_migration`. Every omitted gate is closed;
unknown names and non-booleans refuse. Only an explicit protected `true` opens
that gate. An open gate records an owner declaration, not measured qualification,
and cannot expand the compiled record/profile registry. The three S4 gates
respectively declare publication, freshness verification and re-anchor readiness;
opening one leaves the others closed and implements none of those later slices.

S5 receipt fields exist in the dependency, but this intake supplies no receipt,
requirement flag or margin. Receipt composition remains separate adapter work;
no new activation gate is declared by this re-pin.

The immutable `Config` exposes:

- `Pin`: a role's public pin by value.
- `CheckProfile`: type/class membership only; it does not validate combinations,
  signatures or activation dependencies.
- `CheckPurpose5`: exact requested root and purpose against the protected scope.
- `IsOpen` and `Require`: gate queries and typed closed-gate refusals.
- `VerifierContext`: a fresh immutable snapshot of static S5 pins only. Signed,
  time, generation and witness evidence remain unavailable. Consumers must
  enforce their dependent gates and supply qualified evidence separately.

Nil and zero configurations remain closed. Every parse refusal returns nil
configuration and a typed `*pins.Error` containing only a stable non-secret
code; caller bytes and external state remain unchanged.

## Pure record admission

`internal/intake.Accept(kind, exactBytes, config, facts)` returns either
`*Accepted` or a typed `*intake.Error` containing only its refusal code. Its
closed types are `attestation/1` (agent, operator-device and D33 service),
`grant/1` and `revocation/1`. Enrollment, journal/checkpoint, message, act,
contact and registry-internal freshness records have no intake path in this slice.
The selected type fixes the byte limit before parsing and must match the body.

`Facts` supplies current time/watermark, audience, generations, exact signed
attestation collections, issuer snapshots with protected availability/age and
rollback metadata, and an available exact-byte id journal. Ordinary values use
CCJ types, including `int64` numbers. Nil means unavailable; nonnil empty
collections mean available and empty. All authority roots, owner and purpose
policy come from `Config`; dynamic facts cannot override them. The configured
service key retains its protected role even without a current attestation.
Only that exact configured subject may be admitted as a service or appear in
`Facts.Services`; another subject refuses `key_binding_invalid` before profile
verification, including when used as delegation evidence.

A grant additionally requires leaf-first `Parents` and the caller's protected
concrete authorization `Request` (`key`, `action`, `target`), plus assurance
and other evidence required by the published verifier. Admission uses
`Authorize` for the whole chain, including all narrowing and revocation checks.
It neither invents a request nor returns the verifier's effect plan. Identity
admission uses `VerifyAttestation` or `VerifyServiceAttestation`. Snapshot
admission uses `CheckRevocation` with the submitted bytes and caller-provided
metadata for that exact issuer. It verifies current signed snapshot structure,
not cumulative continuity against historical snapshots.

| Order | Refusal codes / behavior |
| --- | --- |
| Selected-type byte limit, before parsing | `request_too_large`; 65,536 bytes for grants, 8,192 for attestations/snapshots |
| Closed routing and bounded CCJ shape | `record_type_unsupported`, pinned `envelope_malformed`, `request_invalid`, `envelope_type_mismatch` |
| Protected configuration, class and evidence preflight | `verification_unavailable`, `key_class_unsupported`, `request_too_large`, `grant_chain_malformed`; unpinned service subjects/evidence refuse `key_binding_invalid` |
| Published profile verifier | Unchanged typed codes, including `envelope_signature_invalid`, `key_binding_invalid`, `key_binding_unavailable`, `grant_root_unknown`, `grant_not_narrowing`, `revoked` and time/watermark/assurance refusals |
| Exact-byte binding | `id_journal_unavailable`, `envelope_conflict`; grant E8 also runs at its original point inside `Authorize` |

The adapter uses the pinned record reader's depth 16, string 8,192 and array
64 wire bounds. Its local decoded-accounting policy also applies those bounds
to ordinary `Facts`, charges their canonical encoded size, reserves 128 bytes
per snapshot's metadata, and permits 65,536 bytes per `Retained` entry. These
are local accounting choices, not additional protocol wire limits. Raw evidence,
keys and those charges must fit a 512 KiB decoded operation budget before the
immutable `Present` snapshots and signature work. Ordinary facts are encoded
before charging, so that encoding may allocate before over-budget refusal.
Chains permit at most four records and 262,144 aggregate grant bytes. Transport
encoding, principal ACLs, deadline/concurrency limits and qualified current-fact
acquisition belong to the later service.

`Accepted` carries the content `ID`, `Type`, authority-domain `IssuerRoot` and
an owned copy of the exact `Bytes`. Refusals return nil. Calls leave caller
bytes, evidence and external state unchanged. A valid signature precedes any
conflict verdict for the submitted record. No function reads files, accesses
the network, journals, mutates replay state or executes effects. Callers must
check activation dependencies and revalidate evidence/time at the later
serialized publication boundary.

## Enrollment projection orchestration

`internal/enrollment` uses the pinned registrar `enrollment.Policy` and
`registry.Loader` for complete approved signed exports. `New(Trust)` constructs
the policy from the protected enrollment-publisher pin, canonical audience and
independently trusted host classification. The caller supplies a protected
`registry.Verifier` that binds every entry and its type/bytes to the exact
reviewed signed commit under the protected commit-signer pin/maintainer allowlist,
and verifies ancestry. This package never fetches or
follows a branch, verifies a Git signature itself, or admits self-enrollment.
Every Loader receives the protected `object_format` pin; cross-format commit
IDs refuse as `registry_unavailable` before any transaction operation.

The `enrollment_intake` gate defaults closed. Opening it is an owner declaration;
`New` also refuses an absent transaction. There is no production `Transaction`
implementation in this repository, so this adapter cannot be activated yet.
The later witness and Git adapters need their own qualification.

`Projection` supplies the full separate enrollment export ref's entries and a
content-ID map of the exact enrollment bytes under `records/`. Only regular
`enrollments/*.json` entries are permitted. Public Loader validation preserves
all seven snapshot-wide phases, including reserved `sigs` before any signature
failure and conflict before rollback. After those phases, its Store bridge
checks complete, byte-identical mirrors before any transaction operation.
It performs no reserialization or signing. An invalid entry refuses the whole
load; no lower valid subset is installed.

`Trust.Retained` supplies protected, independently reconciled `State`, including
high-water and every earlier exact `Projection` in `Exports`. First use is
explicit; missing rollback evidence is not bootstrap. Loader enforces immutable
body/ID/sequence history and irreversible tombstones. Approved publisher rotation
may change envelope signatures while preserving that history. Each resolved
candidate appends its exact export revision; earlier envelope bytes remain in
its complete transaction inventory, never overwritten by content ID alone.
Archives are retained in full; sizing and durable retention require qualification.

The bridge calls the injected transaction in order:

1. `Preflight(Candidate)` validates the complete recoverable inventory/budget,
   excludes old instances and previews the exact local durable state.
2. `Intent(Binding)` obtains its external durable acknowledgement.
3. `Commit(Binding)` durably commits through `internal/store` and reports the
   exact previewed state binding.
4. `Resolve(Binding)` verifies that commit, obtains matching resolution,
   revalidates authority/time/bytes and publishes the resolved head.

The candidate ID binds the before/after high-water, full retained exports and
exact mirror bytes. It is a local inventory digest, separate from protocol
witness digests. Both the candidate and previewed state IDs must match through
commit and resolution; uncertainty, a mismatched binding or any error fences.
The interface contract requires real durability and witness evidence from its
qualified implementation; comparing report fields cannot authenticate them.

`Load` returns a public read-only snapshot only after exact resolved success.
Every failed load returns nil, sets a sticky `Fenced` state and prevents later
transaction calls. The caller must stop using previously returned snapshots
when fenced. `LastResolved` returns copied recovery evidence only: after a failed
transaction it cannot prove that durable state was unchanged. Recovery needs a
new instance with externally reconciled state; there is no reset method.
Inputs and transaction candidates are copied. Typed errors expose only codes:
`enrollment_intake_closed`, `transaction_unavailable`, `state_unavailable` for
subsequent fenced calls, and the public Loader's unchanged enrollment/registry
refusals. Mirror/path/transaction failures use `registry_unavailable`.

Tests use build-selected public registrar fixtures and a test-only transaction
double. They check ordering, exact bindings, whole-load refusal, history,
rotation/archive retention and sticky fencing. They do not qualify Git approval,
local crash durability, witness transport or recovery. No in-memory successful
transaction implementation is shipped in production.

## Local transaction storage

`internal/store` is a Linux local-filesystem implementation of `Transactions`.
It uses standard-library syscalls and the existing pinned packages; there are no
new dependencies. `Create` explicitly bootstraps an existing empty private
(mode 0700) directory. `Open` never bootstraps missing state. A lifetime directory
lock excludes cooperating local opens, and a mutex serializes all API readers
and the single writer; neither proves old-instance exclusion or clone safety.

`Preview` validates a request and returns its candidate state ID without writing
or reserving a sequence. `Commit` repeats the checks under serialization and
returns an owned `Outcome`. `Open(root, expectedStateID)` and
`Reconcile(expectedStateID)` require the exact independently protected latest
candidate ID. Missing evidence, forks, unsafe paths and mismatched expected IDs
stay fenced; disk evidence alone cannot detect complete loss or rollback.

A later witness adapter must serialize **Preview → external intent → Commit**
across the whole sequence, independently retain the complete approved candidate
inventory **and** expected ID before intent, and independently authenticate the
latest witness authority. The inventory includes every exact blob, delta,
record, journal/checkpoint, retained request/outcome and predecessor needed to
recover the candidate. A digest cannot reconstruct erased bytes. Preview exports
an identity only; there is no approved-inventory installation/export API in this
slice. Building and qualifying that cross-system inventory belongs to the later
adapter. A local successful commit supplies no witness or publication authority.

`Request` carries protected principal/request ID, operation, expected/current head,
watermark, exact record bytes, opaque journal/checkpoint bytes and opaque outcome.
Record signatures, authority, journal construction, time qualification and external
resolution remain the caller's responsibilities. A new transaction allocates one
positive sequence through 9007199254740991. Under the lock, changed retained
arguments refuse `request_conflict`, a different expected head refuses
`head_conflict`, then exhausted sequence or byte capacity refuses
`capacity_exhausted`, before reservation. An exact retry returns its original
outcome after later heads and restarts, appends nothing and renews no authority.
Same content ID/different bytes refuses `envelope_conflict`, preserving originals.

The on-disk format is local recovery state, not a public record schema:

| Path | Contents |
| --- | --- |
| `anchor.json` | Immutable explicitly created empty baseline with local `Format: "delta-1"` marker |
| `transactions/<state-id>.json` | Append-only delta: base ID, next sequence/head/watermark, this request's path-to-blob digests and retained request/outcome references |
| `blobs/<digest>.blob` | Immutable exact byte payloads and local request/outcome reference encodings; received bundles and result bytes are independent blobs, deduplicated by exact-byte digest |
| `records/<type-dir>/<content-id>.json` | Exact caller bytes; six compiled kinds map `/1` to `-1`, for example `grant-1`; IDs are `sha256:` plus 64 lowercase hex digits |
| `journal/<seq>.json`, `checkpoints/<seq>.json` | Exact opaque bytes at 16-digit zero-padded positive decimal positions |
| `CURRENT` | The single locally resolved state ID, replaced last |
| `.tmp-*` in those directories | Retained pending fragments; never authoritative reader inputs |

Each state ID hashes its complete canonical local delta, including the base ID.
Recovery verifies and folds the chain from the anchor; later deltas never repeat
old payloads or full images. A request reference preserves all arguments,
including nil versus empty byte slices, while each payload is stored once by
its own digest. Materialized record/journal/checkpoint paths are additional exact
copies for their existing read layout. Only blobs referenced by the verified
delta fold are admitted, including a pending candidate when recovery verifies
and resolves its manifest. Any extra blob fences Open and Reconcile even if its
digest matches; its bytes stay intact. Unknown manifests or staging owners also
fence. No history, blob or pending fragment is rewritten or garbage-collected.
The anchor's `delta-1` marker is bound into the predecessor chain. Missing or
unknown markers fence, including empty older roots without a `blobs/` directory.
There is no automatic conversion of unversioned or older full-image formats;
incompatible evidence stays intact.

A commit audits all authoritative bytes and namespace entries, prepares only its
delta, then writes the durable manifest, immutable blobs, exact materialized
files and finally `CURRENT`. Type directories are created on first use with
checked mkdir and parent-directory barriers. Each file uses a checked full
write, file fsync, checked close, same-directory rename and directory fsync.
Component-relative no-follow opens reject links, special files and traversal.
Any durability error or uncertainty fences readers and writers as
`state_unavailable`. Reconciliation validates the entire lineage before any
recovery write, reconstructs missing paths only from verified blobs, refuses
changed materialized bytes, and re-establishes file/directory durability before
exposure. Missing referenced blobs never become empty reads.

Memory holds digest/path/request-position indexes, not historical manifest,
bundle or outcome bytes. With N retained paths/requests/manifests/blobs, index
space is O(N); payload working space is O(L), where L is the 16 MiB maximum
local blob. Recovery decodes one bounded request/delta at a time and folds
indexes in place. A delta is at most 64 KiB, and each request is independently
bounded by the limits below. This is a bounded-payload algorithm, not constant
memory independent of history. The 2,000-small-commit test measures additional
live heap during Open against a 64 MiB workload ceiling; it is not a universal
heap guarantee. Every API audit still reads/hashes retained authoritative bytes,
so audit work grows with history; the change qualifies linear storage growth,
not constant-time throughput or physical-filesystem durability.

`CreateWithOptions` and `OpenWithOptions` accept `Options{MaxBytes: ...}`. Zero
selects the default **1 GiB local logical regular-file byte budget**. Preflight
charges all existing regular bytes, including retained fragments, plus every
new blob/materialized path, the new delta and a complete
replacement `CURRENT`. Counting both old and staging `CURRENT` is conservative:
success leaves one pointer-sized reserve. The check precedes intent, mkdir or
any write; refusal is `capacity_exhausted` with no tree change. Exact retries
need no additional capacity. Directory/inode metadata, filesystem allocation
rounding and external copies/backups are outside this logical byte budget; real
ENOSPC still fences as a durability failure. Opening with a smaller budget does
not discard existing evidence or invalidate exact retries; it blocks new growth.

The following are **local storage API limits, not protocol wire limits**:

| Local field / operation | Limit |
| --- | --- |
| Principal, request ID, operation | Nonempty UTF-8, at most 255 bytes each |
| Records in one request | 1–64 |
| Result | At most 65,536 bytes |
| Aggregate record bytes | At most 2 MiB |
| Each query string | UTF-8, at most 8,192 bytes |
| Record bytes | 65,536 for grant/enrollment; 8,192 for other compiled kinds |
| Opaque journal/checkpoint bytes | Nonempty, at most 8,192 each |
| Local serialized blob / delta | 16 MiB / 64 KiB |
| Total retained regular bytes | Configurable; default 1 GiB, charged as above |

`Preflight` additionally enforces the design's separate full received bundle
2 MiB bound, J's 64 entries/two checkpoints/540,672 bytes, F's 64 publications/
one checkpoint/532,480 bytes and 64 IDs/keys, per-proof-record 8,192-byte/depth-16
bounds, and complete WS wrapper 8,192 bytes including LF. The caller supplies the
entire received carrier. These budget/shape checks authenticate no proof or
wrapper, implement no WS transport and never truncate a complete proof to fit.

## Pure journal construction

`internal/journal` builds unsigned `journal-entry/1` records and signed
`checkpoint/1` candidates using the pinned record codec, content IDs and signing
preimages. `BuildEntry` receives a closed body and separately retained prior
entry time. `BuildCheckpoint` receives a closed body, prior checkpoint time and
an owner signer. Equal times are permitted; backward times are refused. Hash
links, scopes, signatures, snapshot admission and the ordered journal decisions
are checked by the pinned `VerifyJournal`, exposed through `journal.Verify`.
Journal admission authenticates references, not the referenced content's rights.

`NewSigner` wraps an external `SignatureService` behind the closed `Signer`
interface. Only registry-owner `revocation/1`, `checkpoint/1` and `freshness/1`
bodies enter that service. Shapes, key/owner bindings and complete output bounds
are checked first; returned signatures are checked against the original pinned
preimage. Failures expose stable codes only. Production must supply and qualify
the separate custody service; this package provides no private-key store,
process launcher, key generation or custody activation. Published fixed seeds
are used only in tests.

`BuildPublication` accepts the oldest-first anchor/new-record/reissue entries,
the prepared checkpoint body, exact prior checkpoint, supplied reissued snapshot
and protected snapshot availability/age/rollback facts. Its validated witness
comes from `ReduceProjection`. It returns owned exact entries/candidate/snapshot,
the exact prior checkpoint lookup, explicit lookup availability and the immutable
verifier context needed to repeat admission. Every refusal returns an empty
prepared value and plan; pinned verifier refusal codes are preserved. It also
requires this application's three-entry layout and the snapshot-add binding,
and refuses reuse of the prior checkpoint's snapshot ID. The supplied snapshot
must already be the publisher's qualified cumulative reissue: comparing complete
old/new cumulative history and enforcing unique issuance belongs to the snapshot
publisher, not reference-only journal admission.

`ReduceProjection` takes all eight projection fields and already authenticated
originals: protected scope, exact checkpoint, approved candidate digest and
validated predecessor. It binds `checkpoint_time` to checkpoint `at` verbatim,
retains all eight fields and the original checkpoint, then exposes the six-field
fact. A received six-field tuple is never accepted. Retained projections reject
same-position head/checkpoint forks and lower positions. Its zero value is
unavailable. Missing retained state is usable only after independent genesis
qualification; this reducer performs no network authentication, ancestry proof
fetch, challenged latest read or witness resolution. Those remain adapter work.

All records use the pinned duplicate-free depth-16 parser and the 8,192-byte
record bound. `Verify` keeps the journal limit of 64 entries, two checkpoints and
540,672 received bytes. Publication preparation additionally precharges its
snapshot, prior-checkpoint and projection context against that same byte budget,
including the complete generated candidate, before signing; this is a stricter
local construction budget, with no truncation or pagination. Prepared signatures
are private candidates, not publication success. No store, witness, bridge,
commit, custodian or exclusion transaction is implemented here.

## Tools

Use Go 1.26.10 (module language version 1.26.0) and `gofmt`. Dependencies are
limited to the reviewed public `waggle` at `v0.0.0-20261025015329-5f4e7435d756`
and registrar at `v0.0.0-20261025040811-264660e5dabb`. Module selection uses this
registrar pin over waggle's older requirement. Its CCJ packages are byte-identical
to that requirement; configuration parsing and the reviewed record package use
one implementation. `go.mod`, `go.sum` and the dependency policy test retain
those versions. No other modules are required.

Set `GOFLAGS=-trimpath`, keep `GOCACHE` and `GOMODCACHE` in a disposable workspace
cache directory, and put the selected Go toolchain on `PATH`. On a shared host,
run Go commands through the installed scheduling wrapper:

```sh
heavy-slot --label registry -- go build ./internal/enrollment ./internal/intake ./internal/pins ./internal/contracts
heavy-slot --label registry -- go test ./internal/enrollment ./internal/intake ./internal/pins ./internal/contracts
heavy-slot --label registry -- go vet ./internal/enrollment ./internal/intake ./internal/pins ./internal/contracts
heavy-slot --label registry -- gofmt -w internal/enrollment/*.go internal/contracts/*.go internal/pins/*.go internal/intake/*.go
heavy-slot --label registry -- go build ./internal/store
heavy-slot --label registry -- go test ./internal/store -timeout=2h
heavy-slot --label registry -- go vet ./internal/store
heavy-slot --label registry -- gofmt -w internal/store/*.go
heavy-slot --label registry -- go build ./internal/journal
heavy-slot --label registry -- go test ./internal/journal
heavy-slot --label registry -- go vet ./internal/journal
heavy-slot --label registry -- gofmt -w internal/journal/*.go
```

`go build` checks the selected library packages; the core `go test` command covers policy parsing,
closed gates, role separation, dependency pins and signed intake refusals and
positive controls; `go vet` checks those packages. Intake, enrollment and journal tests read public
fixtures from the build-selected module directory returned by
`go list -m -json github.com/relux-works/waggle` or
`go list -m -json github.com/relux-works/swarma-registrar`, checking the exact module path,
reviewed version and absence of replacements. Standard Go configuration,
including a `GOENV`-only `GOMODCACHE`, is supported; missing fixtures fail closed.
Run `heavy-slot --label registry -- go mod download all` to populate the selected
cache when needed.
Build/module artifacts go to the configured caches, tests report to stdout and
formatting updates source files. The build produces no executable binary; only
storage API calls or temporary storage tests create local state. The scheduling
wrapper bounds shared-host work; it is not a
runtime dependency of the library packages.

Storage tests use only disposable temporary directories. They cover conflict
precedence, exact retries, unsafe paths, no-write preflight refusals, each checked
I/O failure, partial writes, bootstrap mkdir/anchor failures, first-commit boundaries and
new type-directory barriers. The bounded power model tracks fsynced inode
contents independently from parent-synced names, including lost parents. Size
and memory tests use 1,000 retained 64 KiB bundles and 2,000 small commits. They
perform real fsync and whole-history audits; the explicit storage-test timeout
allows for this work on shared filesystems. Package-private fault hooks are unavailable
to production callers. The power-loss model and local locks do not qualify a
filesystem, witness, Git adapter, service startup or external publication. Build
and vet produce no persistent output; place redirected test logs and coverage
reports in a disposable workspace scratch directory.

## Hosted checks

The Linux workflow runs on `ubuntu-26.04` with Go `1.26.10`, local toolchain
selection and read-only repository permissions. Both actions are pinned to exact
commits, checkout credentials are not retained, and full history is fetched for
the pull request's added-line check. Pull requests and pushes to `main` run one
40-minute job: module verification, build, vet, formatting, public hygiene, an
explicit dependency-policy test and the verbose full suite. The full-suite
command has a 35-minute per-package timeout to cover the storage tests. There is
no privileged-test exemption; the workflow runs the existing tests unchanged.
Hosted checks supplement the separate review gate and do not qualify deployment.

The workflow commands can also be run locally through the shared scheduling
wrapper, with Go on `PATH` and `GOTOOLCHAIN=local GOMAXPROCS=2 GOFLAGS=-trimpath`:

```sh
heavy-slot --label registry -- go mod verify
heavy-slot --label registry -- go build ./...
heavy-slot --label registry -- go vet ./...
heavy-slot --label registry -- bash -e -c 'out="$(gofmt -l .)"; test -z "$out"'
python3 .scripts/check-hygiene.py --base <base-commit>
heavy-slot --label registry -- go test ./internal/contracts -run '^TestDependencyPolicy$' -count=1 -v
heavy-slot --label registry -- go test ./... -count=1 -v -timeout=35m
```

`.scripts/check-hygiene.py` uses Python 3 and Git to reject attribution metadata
in all tracked content and the pull request's added lines. It checks link targets
without following links, emits paths rather than offending content, and fails
closed when Git or a tracked file is unavailable. The optional base must be an
exact commit ID; an initial push's zero base is covered by the full tracked scan.
Formatting preserves the formatter's exit status, including syntax errors in
files excluded from the current platform's build. Checks report to stdout/stderr;
local logs belong in a disposable workspace scratch directory, not versioned
source. The workflow writes no reports or deployment artifacts.
