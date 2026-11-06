# Inert accepted fixture and adjacent refusal

This is a static reading example from the frozen repository's tests. It submits nothing and provides no live signing, key or credential steps. No product/test command is needed to inspect it.

The actual example is [TestServiceDelegationAccepted](../../../../internal/intake/intake_test.go#L519) in `internal/intake/intake_test.go`. It calls the local test helper `load(t, "P-SVC-chain")`, which selects the public positive vector from the pinned envelope module, adapts its complete current facts and protected configuration, and then calls `check(t, s, "accept")`. `check` invokes `intake.Accept` and asserts exact ID/type/byte binding, nonempty issuer and unchanged caller bytes/facts. The accepted grant's issuer root is the protected grant-root pin. Mutating the returned byte copy leaves the next check unaffected.

The existing helper calls below illustrate that accepted sample and the neighboring type-mismatch refusal; they are contextual test excerpts, not a public API or a standalone program:

```go
s := load(t, "P-SVC-chain")
got := check(t, s, "accept")
```

The adjacent refusal test uses the same positive sample, then:

```go
s.kind = "attestation/1"
check(t, s, "envelope_type_mismatch")
```

The test's existing fixture-discovery code checks the selected module's exact path/version, nonempty directory and absence of a replacement. It builds a path within that selected module at test runtime. This repository contains no serialized copy at a local fixture path; do not invent one, search another identity's module cache or fetch/run the resolver merely to read this example. The pinned external vector's bytes were not acquired or executed for this documentation.

Read the immediately adjacent [TestUnsupportedIntakeTypeRefused](../../../../internal/intake/intake_test.go#L531). From the same positive sample it first checks acceptance, then selects unsupported kinds such as `enrollment/1` and expects nil with `record_type_unsupported`. Relabeling the same grant bytes as `attestation/1` expects `envelope_type_mismatch`. These are actual routing/type refusals, not a hypothetical signature failure.

[Accept](../../../../internal/intake/intake.go#L80) enforces that refusal order: selected byte limit, closed routing, bounded parse and body/type equality precede protected-context and profile verification. A successful test fixture asserts only the represented local admission contract. It proves neither human-owner identity, production currentness, cumulative history, live custody, publication nor activation.

If current facts are the question, the following [TestMissingEvidenceRefused](../../../../internal/intake/intake_test.go#L542) supplies real neighboring controls: missing configuration/request refuses `verification_unavailable`, absent time refuses `time_unavailable`, and unavailable exact-byte journal refuses `id_journal_unavailable`. Report these as source assertions, not tests run by this documentation task.
