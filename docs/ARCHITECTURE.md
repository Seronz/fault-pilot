# Architecture

Agent vs Policy vs Executor

- Agent (Investigator + Gemini adapter): mengamati evidence, menyusun ringkasan insiden dan mengusulkan proposal. Gemini CLI adapter hanya propose, treat log as data.
- Policy Engine: fail-closed validator (allowlist: restart_simulated_service -> checkout), cek active incident, evidence ownership/existence, duplicate running, cooldown, limits. Reject invalid JSON, unknown target, missing evidence.
- Executor: menjalankan aksi hanya ke Fault Injector (HTTP POST /control), concurrency per dependency, deadline, never touches host. Verifier cek health sebelum/sesudah (recovered/not_recovered/unknown). ActionJournal rekam before/after + audit JSONL, idempotency, reconciliation on restart.

## Flow
Collector -> Investigator -> Gemini -> Policy -> Executor -> Verifier
Store(SQLite WAL) + ActionJournal maintain state.

## Audit log example
{"action_id":"...","idempotency_key":"...","state":"executing","incident_id":"inc-1","action":"restart_simulated_service","target":"checkout","updated_at":"...Z"}

## Test coverage (T01–T16)
T02 valid pass; T03 invalid JSON reject; T04 target outside allowlist reject; T05 100 same events→1 incident; T06 1000 concurrent bounded; T09 same incident no double; T10 crash recover no double exec; T11 hold on audit/db failure; T12 shutdown when full; T13 out-of-order evidence; T14 not_recovered no infinite retry; T16 same dependency concurrency limit. Others covered in tests.
