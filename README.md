# swarma-owner-registry

An owner registry service for the trust node, written in Go. The accepted
[service design](design/owner-registry.md) defines the later record, publication
and recovery work.

Slice 1 implements protected contract pins and activation checks only. There is
no service, network listener, intake or storage implementation yet. Configuration
acceptance is not record admission, deployment or qualification evidence.

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
Signed identity, lifetime and revocation verification remain later work; a
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

## Tools

Use Go 1.26.10 (module language version 1.26.0) and `gofmt`. Dependencies are
limited to the reviewed public `waggle` and its exact registrar dependency;
`go.mod`, `go.sum` and the dependency policy test retain those versions. Configuration parsing and the
reviewed record package use one registrar CCJ implementation. No other modules are required.

Set `GOFLAGS=-trimpath`, keep `GOCACHE` and `GOMODCACHE` in a disposable workspace
cache directory, and put the selected Go toolchain on `PATH`. On a shared host,
run all multi-package commands through the installed scheduling wrapper:

```sh
heavy-slot --label registry -- go build ./...
heavy-slot --label registry -- go test ./...
heavy-slot --label registry -- go vet ./...
gofmt -w internal/contracts/*.go internal/pins/*.go
```

`go build` checks both library packages; `go test` covers strict policy parsing,
closed gates, all role pairs and dependency pins; `go vet` checks both packages.
Build/module artifacts go to the configured caches, tests report to stdout and
formatting updates source files. There is no executable binary or generated
service state. The scheduling wrapper serializes shared-host work; it is not a
runtime dependency of either package.
