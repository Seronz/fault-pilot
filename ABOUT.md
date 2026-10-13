# About Fault Pilot

**Apa itu ini**
Fault Pilot itu engine auto-remediation fail-closed buat simulasi handling incident — dibuat buat ngulik bareng, nggak butuh k8s.

**Kenapa bikin**
Biar bisa latian safe automation: validasi proposal, cegah double execution, dan default-nya fail-closed (mending nolak ketimbang ngawur eksekusi).

**Gimana cara kerjanya**
- Event masuk → Collector dedup & masuk channel bounded (100).
- Policy validasi allowlist + incident/evidence; ActionJournal catat state before/after + audit JSONL.
- Executor (HTTP-only, per-dependency semaphore) + Verifier (health before/after) nentuin recovery, plus idempotency & reconciliation.

**Tech stack**
Go 1.24.5 • SQLite (WAL) • bounded worker pool • CLI Gemini adapter • fail-closed design
