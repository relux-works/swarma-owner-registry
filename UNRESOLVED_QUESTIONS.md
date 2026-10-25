# Unresolved questions

The authoritative decisions and options are in
[design §10](design/owner-registry.md#10-questions-for-review).
This index adds no activation authority.

| Question | Required decision / evidence | Gate |
| --- | --- | --- |
| Q10 / WC1 G17 (one decision) | Select independent custodian provider, failure domain, exact-tuple/fresh-read protocol, continuity/recovery and worst-case SLA; qualify bridge/exclusion and durability. Q18 remains separate; no weaker custodian fallback. | Witness coordination and publication; [§6.2 source pairs](design/owner-registry.md#62-ws-server-adapter-and-real-transactions): OR:design/owner-registry.md:1360,1368 at 018c21624b0c8a2adbbf823c4e2059bc3cb264a2; WS:spec/witness-service.md:483–523 at e7c5e1850c29d1f931de3faa24ad39d94da41ada; WC:UNRESOLVED_QUESTIONS.md:25 at c18d70040d11a6a03078df1e61c1bf3aa0dae85a. |
| Q13 | Owner approval of witness/custodian metadata disclosure and read principals. | Public distribution. |
| Q17 / D42 | Decided: the owner decided (2026-10-25) on (a), external roots sign content only on change and registry freshness attests currency; the root's Signer application checks its own journal inclusion. Options (b) and (c) rejected. Remaining dependency: the format owner's multi-snapshot freshness change, its pin, publisher coverage and specified consumer rule ([design §10 Q17](design/owner-registry.md#10-questions-for-review); [S5 D42:205–228](https://github.com/relux-works/waggle/blob/5f4e7435d75697385ff6ce15a7501ac6f31c8ea5/UNRESOLVED_QUESTIONS.md#L205-L228)). | External-root currency/intake (row 7c); current D32/S5 age checks remain unchanged until the change is merged and pinned. Registry-owner retaining adapter (7b) can start now. |
| Q18 | Approve versioned immutable content versus atomic completion/use metadata (recommended), or an explicit alternate protocol; select independent completion/visibility rollback authority, bridge authentication, capacity, genesis/migration and recovery. A separate protected ledger needs an explicit owner decision. | All QT-2c consume adapters. |
| Q19 | Qualify actual read-to-durable-act/use enforcement for C/V/F, finite workload-specific T/f/epsilon/reserve, common final exclusions/order and emergency-stop independence. Original deadline and suspend-aware read requirements are settled. | Linux and macOS timing admission; unknown bounds remain handoff_unqualified. |
| Q20 | Review the native mapping to the broker template published at 7294b7a; publish and qualify protected original-reader/witness interfaces, owning identity codec and staged frozen-artifact signing, clock/deadline mapping and native macOS full-flush/rename/restore equivalence. | Codec/adapter implementation and native parity. |

All executable QT-2c controls in
[§9.1.8](design/owner-registry.md#918-paired-controls--all-pending) remain PENDING.
The current change supplies design only.
