# swarma-owner-registry

An owner registry service for the trust node, written in Go. The accepted
[service design](design/owner-registry.md) defines the later record, publication
and recovery work.

The first two slices implement protected contract pins, activation checks and
a pure signed-record intake adapter. There is no network listener or storage
implementation. An admission verdict is not publication, deployment or
qualification evidence.

## Packages and protected configuration

`internal/contracts` freezes the reviewed S3 revision
`9b42338b8a57d12a21da4db4375b269deacdd6bf`, six service-supported record types
(`attestation/1`, `grant/1`, `revocation/1`, `enrollment/1`, `journal-entry/1`,
`checkpoint/1`), three key classes (`agent`, `operator-device`, `service`) and
the exact D33 grant-service profile. `message/1` and `operator-act/1` have no
standalone intake profile here. `freshness/1` needs a later reviewed revision.

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
`purpose5_runtime`, `custody`, `transport`, `time`, `witness`, `storage`,
`freshness_history` and `consumer_migration`. Every omitted gate is closed;
unknown names and non-booleans refuse. Only an explicit protected `true` opens
that gate. An open gate records an owner declaration, not measured qualification,
and cannot expand the compiled S3 record/profile registry.

The immutable `Config` exposes:

- `Pin`: a role's public pin by value.
- `CheckProfile`: type/class membership only; it does not validate combinations,
  signatures or activation dependencies.
- `CheckPurpose5`: exact requested root and purpose against the protected scope.
- `IsOpen` and `Require`: gate queries and typed closed-gate refusals.
- `VerifierContext`: a fresh immutable snapshot of static S3 pins only. Signed,
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
contact and future freshness records have no intake path in this slice.
The selected type fixes the byte limit before parsing and must match the body.

`Facts` supplies current time/watermark, audience, generations, exact signed
attestation collections, issuer snapshots with protected availability/age and
rollback metadata, and an available exact-byte id journal. Ordinary values use
CCJ types, including `int64` numbers. Nil means unavailable; nonnil empty
collections mean available and empty. All authority roots, owner and purpose
policy come from `Config`; dynamic facts cannot override them. The configured
service key retains its protected role even without a current attestation.

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
| Protected configuration, class and evidence preflight | `verification_unavailable`, `key_class_unsupported`, `request_too_large`, `grant_chain_malformed` |
| Published profile verifier | Unchanged typed codes, including `envelope_signature_invalid`, `key_binding_invalid`, `key_binding_unavailable`, `grant_root_unknown`, `grant_not_narrowing`, `revoked` and time/watermark/assurance refusals |
| Exact-byte binding | `id_journal_unavailable`, `envelope_conflict`; grant E8 also runs at its original point inside `Authorize` |

The adapter uses the pinned record reader's depth 16, string 8,192 and array
64 bounds. It precharges supplied raw evidence, keys, ordinary CCJ facts and
snapshot metadata against a 512 KiB decoded operation budget before copying
facts or verifying signatures. Chains permit at most four records and 262,144
aggregate grant bytes. Transport encoding, principal ACLs, deadline/concurrency
limits and qualified current-fact acquisition belong to the later service.

`Accepted` carries the content `ID`, `Type`, authority-domain `IssuerRoot` and
an owned copy of the exact `Bytes`. Refusals return nil. Calls leave caller
bytes, evidence and external state unchanged. A valid signature precedes any
conflict verdict for the submitted record. No function reads files, accesses
the network, journals, mutates replay state or executes effects. Callers must
check activation dependencies and revalidate evidence/time at the later
serialized publication boundary.

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
```

`go build` checks the three library packages; `go test` covers policy parsing,
closed gates, role separation, dependency pins and signed intake refusals and
positive controls; `go vet` checks those packages. Intake tests read public
fixtures from the exact downloaded module under `GOMODCACHE`, which must be
set explicitly. Run `heavy-slot --label registry -- go mod download all` to
populate that cache when needed.
Build/module artifacts go to the configured caches, tests report to stdout and
formatting updates source files. There is no executable binary or generated
service state. The scheduling wrapper serializes shared-host work; it is not a
runtime dependency of the library packages.
