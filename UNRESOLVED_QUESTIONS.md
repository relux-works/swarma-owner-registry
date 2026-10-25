# Unresolved questions

The authoritative decisions and options are in
[design §10](design/owner-registry.md#10-questions-for-review).
This index adds no activation authority.

| Question | Required decision / evidence | Gate |
| --- | --- | --- |
| Q10 / WC1 G17 (one decision) | Decided rule: the owner decided (2026-10-25) on one synchronous independent custodian for both specifications, exact predecessor confirmation and fresh reads within the operation budget, with no periodic or local fallback. Provider selection, provider/SLA, protocol and qualification remain open; Q18 stays separate ([§10 Q10](design/owner-registry.md#10-questions-for-review); [WC1 G17:25](https://github.com/relux-works/swarma-witness/blob/29df43b7e8b26d1bce7101c5fc73e8c9b8050b99/UNRESOLVED_QUESTIONS.md#L25)). | Witness coordination/publication gate stays closed. |
| Q13 / WC1 G07 | Decided rule: the owner decided (2026-10-25) on closed-by-default witness/custodian metadata disclosure only to an explicitly approved circle of readers; otherwise isolation, without filtering the signed chain. Actual reader-set or isolation-design approval and qualification remain open ([§10 Q13](design/owner-registry.md#10-questions-for-review); [WC1 G07:15](https://github.com/relux-works/swarma-witness/blob/29df43b7e8b26d1bce7101c5fc73e8c9b8050b99/UNRESOLVED_QUESTIONS.md#L15)). | Disclosure gate stays closed. |
| Q17 / D42 | Decided: the owner decided (2026-10-25) on (a), external roots sign content only on change and registry freshness attests currency; the root's Signer application checks its own journal inclusion. Options (b) and (c) rejected. Remaining dependency: the format owner's multi-snapshot freshness change, its pin, publisher coverage and specified consumer rule ([design §10 Q17](design/owner-registry.md#10-questions-for-review); [S5 D42:205–228](https://github.com/relux-works/waggle/blob/5f4e7435d75697385ff6ce15a7501ac6f31c8ea5/UNRESOLVED_QUESTIONS.md#L205-L228)). | External-root currency/intake (row 7c); current D32/S5 age checks remain unchanged until the change is merged and pinned. Registry-owner retaining adapter (7b) can start now. |
| Q18 | Approve versioned immutable content versus atomic completion/use metadata (recommended), or an explicit alternate protocol; select independent completion/visibility rollback authority, bridge authentication, capacity, genesis/migration and recovery. A separate protected ledger needs an explicit owner decision. | All QT-2c consume adapters. |
| Q19 | Qualify actual read-to-durable-act/use enforcement for C/V/F, finite workload-specific T/f/epsilon/reserve, common final exclusions/order and emergency-stop independence. Original deadline and suspend-aware read requirements are settled. | Linux and macOS timing admission; unknown bounds remain handoff_unqualified. |
| Q20 | Review the native mapping to the broker template published at 7294b7a; publish and qualify protected original-reader/witness interfaces, owning identity codec and staged frozen-artifact signing, clock/deadline mapping and native macOS full-flush/rename/restore equivalence. | Codec/adapter implementation and native parity. |

All executable QT-2c controls in
[§9.1.8](design/owner-registry.md#918-paired-controls--all-pending) remain PENDING.
The current change supplies design only.
