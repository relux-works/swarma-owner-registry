# swarma-owner-registry

An owner registry service for the trust node, written in Go. The accepted
[service design](design/owner-registry.md) defines the later record, publication
and recovery work.

The first two slices implement protected contract pins, activation checks and
a pure signed-record intake adapter. A separate local transaction store now
provides checked durability and recovery. There is no network listener or Git/
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

The top-level members are `revision`, `owner`, `roles`, `record_types`,
`key_classes`, `service_profile` and optional `activation`. The revision and
complete type/class sets must match the compiled contract; arrays cannot add,
duplicate or omit entries. Owner is a nonempty UTF-8 string of at most 255 bytes.
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

`activation` is an object of optional booleans named `s4_freshness`,
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

## Tools

Use Go 1.26.10 (module language version 1.26.0) and `gofmt`. Dependencies are
limited to the reviewed public `waggle` and its exact registrar dependency;
`go.mod`, `go.sum` and the dependency policy test retain those versions. Configuration parsing and the
reviewed record package use one registrar CCJ implementation. No other modules are required.

Set `GOFLAGS=-trimpath`, keep `GOCACHE` and `GOMODCACHE` in a disposable workspace
cache directory, and put the selected Go toolchain on `PATH`. On a shared host,
run Go commands through the installed scheduling wrapper:

```sh
heavy-slot --label registry -- go build ./internal/intake ./internal/pins ./internal/contracts
heavy-slot --label registry -- go test ./internal/intake ./internal/pins ./internal/contracts
heavy-slot --label registry -- go vet ./internal/intake ./internal/pins ./internal/contracts
heavy-slot --label registry -- gofmt -w internal/contracts/*.go internal/pins/*.go internal/intake/*.go
heavy-slot --label registry -- go build ./internal/store
heavy-slot --label registry -- go test ./internal/store -timeout=2h
heavy-slot --label registry -- go vet ./internal/store
heavy-slot --label registry -- gofmt -w internal/store/*.go
```

`go build` checks the selected library packages; the core `go test` command covers policy parsing,
closed gates, role separation, dependency pins and signed intake refusals and
positive controls; `go vet` checks those packages. Intake tests read public
fixtures from the build-selected module directory returned by
`go list -m -json github.com/relux-works/waggle`, checking the exact module path,
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
