---
name: swarma-owner-registry
description: Explain and use Swarma Owner Registry's repository-local Go interfaces, protected pins, record admission, freshness receipts and adapter contracts. Use for this module, not live registry operations or owner activation.
---

# Swarma Owner Registry

Start with the [module README](../../../README.md) and the actual source for the requested boundary. This skill describes frozen revision `13eb35502d5fe137abaa1e1cdeb77a0f1db1de10`. Recheck source before applying it to another revision.

The module is `github.com/relux-works/swarma-owner-registry`. All implemented packages are under `internal/`: they are repository-local components, not a public SDK for unrelated modules. There is no module CLI, network listener, plugin API or production Git/witness publication adapter. Source availability establishes neither installation nor qualification.

- Read [admission and currency](references/admission.md) for protected configuration, pure intake, receipt-backed consumers and final-effect rechecks.
- Read [state and adapter seams](references/adapters.md) for local storage, enrollment orchestration, candidate construction, and how actual caller-supplied interfaces are wired and checked.
- Read [the inert fixture](references/fixture.md) for a source-backed accepted case beside its actual refusal. It is a static example, not a live registry request or a fixture installed in this repository.

Keep these boundaries explicit:

1. A protected owner string is a policy binding, not proof of a person's identity or presence. A public key identifier, a signature check, a content digest and an activation declaration establish only their implemented checks. They do not authenticate unspecified trust sources, independent history or durability.
2. Distinguish candidate admission, caller acknowledgement, local commit, external resolution and activation. Do not promote success at one boundary into success at the next.
3. Keep exact bytes, complete retained history, whole immutable evidence identity and independently protected restore/time floors. Missing evidence is not bootstrap; failed required checks do not permit a fallback.
4. **OWNER-SITTING ONLY:** owner-presence, key selection/provisioning, credentials, custody and signing flows belong to the owner in a separately authorized sitting. Agents may explain the source contracts and prepare documentation; never turn those flows into agent steps or infer permission from this skill. Do not access private/live state or issue registry writes.

For owner activation or qualified-time design questions, read [design §12](../../../design/owner-registry.md#12-owner-root-activation-record-activation1), [§9.1](../../../design/owner-registry.md#91-qualified-time-handoff-windows) and the [unresolved gates](../../../UNRESOLVED_QUESTIONS.md). These are inactive proposals and qualification obligations, not callable module APIs. No owner-activation signing profile or purpose allocation is implemented by those sections.

Report the selected interface, its exact outcome and the evidence still required outside it. This skill authorizes no installation, live qualification or deployment.
