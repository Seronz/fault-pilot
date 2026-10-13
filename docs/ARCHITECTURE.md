# Architecture

Agent vs Policy vs Executor

- Agent (Investigator + Gemini adapter): mengamati evidence, menyusun ringkasan insiden dan mengusulkan proposal. Gemini CLI adapter hanya propose, treat log as data.
- Policy Engine: fail-closed validator (allowlist: restart_simulated_service -> checkout), cek active incident, evidence ownership/existence, duplicate running, cooldown, limits. Reject invalid JSON, unknown target, missing evidence.
- Executor: menjalankan aksi. Default `lab` hanya POST `/control` ke Fault Injector (dummy, tetap dipertahankan). Executor prod: `systemctl` (`systemctl restart <service>`), `kubectl` (`kubectl rollout restart deployment/<name> -n <ns>`, dukung dry-run), `aws_ec2` (RebootInstances via interface `EC2API`, mockable), `ansible` (`ansible-playbook <runbook> -l <target>`, runbook wajib di allowlist). Semua executor fail-closed, timeout, tanpa shell, cek policy dulu, dan catat Journal+SQLite sebelum/sesudah. Verifier cek health sebelum/sesudah (recovered/not_recovered/unknown). ActionJournal rekam before/after + audit JSONL, idempotency, reconciliation on restart.

## Executors
- Jenis dipilih lewat `config.yaml` -> `executor.type` (`lab` bila kosong).
- Risiko: `lab` low, `systemctl`/`kubectl` medium, `aws_ec2`/`ansible` high.
- Argumen dibangun sebagai slice (`exec.CommandContext`), bukan `bash -c` -> tidak bisa command injection.
- `policy.Validate` dipanggil sebelum eksekusi; bila ditolak, executor tidak dijalankan.

## Flow
Collector -> Investigator -> Gemini -> Policy -> Executor -> Verifier
Store(SQLite WAL) + ActionJournal maintain state.

## Observability
- `internal/observability` menyediakan registry metrik format teks Prometheus (tanpa dependensi eksternal): `Counter`, `CounterVec`, `Gauge`, `GaugeFunc`, `HistogramVec`.
- Metrics Fault Pilot: `incidents_total{source}`, `actions_total{executor,status}`, `policy_rejections_total{reason}`, `executor_duration_seconds{executor}`, `worker_queue_depth`, `events_dropped_total`.
- `policy.Config.Observer` diisi `Metrics.PolicyRejection(reason)` sehingga setiap penolakan tercatat beralasan.
- `InstrumentExecutor` adalah decorator `executor.Executor` yang mengukur durasi dan status (success/failed/rejected) tanpa mengubah executor.
- Server HTTP: `GET /metrics`, `GET /healthz` (deep check SQLite/worker/freshness poll), `GET /readyz`.
- `Heartbeat` + `Checker` untuk health; `SelfMonitor` memberi peringatan `self-degraded` bila antrean > 90 atau error-rate aksi > 50%.
- Tracing ringan: `trace_id` pada context (`observability.WithTraceID` / `TraceIDFrom` / `EnsureTraceID`) dan `LoggerWithTrace`.

## Audit log example
{"action_id":"...","idempotency_key":"...","state":"executing","incident_id":"inc-1","action":"restart_simulated_service","target":"checkout","updated_at":"...Z"}

## Test coverage (T01–T16)
T02 valid pass; T03 invalid JSON reject; T04 target outside allowlist reject; T05 100 same events→1 incident; T06 1000 concurrent bounded; T09 same incident no double; T10 crash recover no double exec; T11 hold on audit/db failure; T12 shutdown when full; T13 out-of-order evidence; T14 not_recovered no infinite retry; T16 same dependency concurrency limit. Others covered in tests.
