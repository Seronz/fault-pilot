# About Fault Pilot

**What is**
Fault Pilot is a fail-closed auto-remediation engine for simulating incident response — built for SWE tinkering, no k8s.

**Why**
To practice safe automation: validate proposals, prevent double execution, and fail closed instead of blindly firing actions.

**How it works**
- Event → Collector dedups (content-based) with bounded channel (100).
- Policy checks allowlist + incident/evidence; ActionJournal tracks state before/after + JSONL audit.
- Executor (HTTP-only, per-dependency semaphore) + Verifier (health before/after) drive remediation with idempotency + reconciliation.

**Tech stack**
Go 1.24.5 • SQLite (WAL) • bounded worker pool • CLI Gemini adapter • fail-closed design
