# Fault Pilot

Fault Pilot adalah sistem otomasi penanganan gangguan (incident) yang bersifat
**fail closed** dan **tanpa dependensi Kubernetes**. Sistem ini mengumpulkan
event, mendeteksi/mengelompokkan insiden, lalu (pada tahap berikutnya) mengusulkan
dan mengeksekusi tindakan perbaikan secara terkendali.

> Bahasa dokumentasi: Indonesia.
> Versi bahasa: Go `1.24.5`.

## Status Komponen

| Kode | Komponen | Status |
| ---- | -------- | ------ |
| C01  | Bootstrap domain (`internal/domain`) | ✅ Selesai |
| C02  | Fault Injector (`lab/faultinjector`) | ✅ Selesai |
| C03  | Collector + Worker + Store | ✅ Selesai |
| C04  | Policy Engine (`internal/policy`) | ✅ Selesai |
| C05  | Journal + Audit (`internal/audit`) | ✅ Selesai |
| C06  | Executor + Verifier + Investigation | ✅ Selesai |
| C07  | Gemini CLI Adapter (`internal/gemini`) | ✅ Selesai |
| C08  | Final + Tests + Scenarios (`scenarios/`) | ✅ Selesai |
| C09  | Incident Connectors (`internal/connectors`) | ✅ Selesai |
| C10  | Prod Executors (systemctl/kubectl/ec2/ansible, lab kept) | ✅ Selesai |
| C11  | Observability (metrics/tracing/health) | ✅ Selesai |
| C12  | RBAC & Human Approval Gate (`internal/approval`) | ✅ Selesai |

## Struktur Direktori

```
.
├── cmd/
│   └── faultpilot/          # entry point aplikasi (menjalankan fault injector)
├── internal/
│   ├── domain/              # tipe data inti (Proposal, Event, Incident, Journal, ...)
│   ├── approval/            # RBAC + Human Approval Gate (approve:low/medium/high)
│   ├── connectors/          # pluggable incident sources (lab/prometheus/loki/pagerduty/cloudwatch)
│   ├── config/              # loader config.yaml (executor.type, allowlist)
│   ├── collector/           # penerima event: kanal terbatas + dedup
│   ├── worker/              # worker pool (batas konkurensi)
│   ├── store/               # penyimpanan SQLite (WAL): events, incidents, journal
│   ├── audit/               # ActionJournal + audit trail JSONL
│   ├── executor/            # executor aksi: lab, systemctl, kubectl, aws_ec2, ansible
│   ├── gemini/              # adapter proposal (CLI Gemini)
│   ├── investigation/       # membangun ringkasan insiden dari evidence
│   ├── observability/       # metrik, tracing (trace id), health/self-monitor
│   ├── policy/              # validasi proposal (allowlist + aturan state)
│   └── verifier/            # verifikasi hasil remediation (health check)
├── lab/
│   └── faultinjector/       # layanan lab penyimulasi gangguan (HTTP)
├── scenarios/               # skenario YAML (latency-spike, dependency-failure, retry-storm)
├── dashboards/              # dashboard Grafana (faultpilot.json)
├── docs/                    # dokumentasi tambahan
├── config/                  # config/rbac.yaml (roles + users)
├── config.yaml              # konfigurasi executor + observability + approval
├── go.mod
└── README.md
```

## Prasyarat

- Go `1.24.5` atau lebih baru.
- Tidak memerlukan Docker/kubernetes.

## Build & Menjalankan

```bash
# Build seluruh paket
go build ./...

# Menjalankan fault injector (default listen :8081)
go run ./cmd/faultpilot

# Mengganti alamat listen
FAULT_INJECTOR_ADDR=:18081 go run ./cmd/faultpilot
```

## Fault Injector (Endpoint Lab)

Fault Injector mensimulasikan kondisi service agar sistem bisa diuji.

| Method | Path | Keterangan |
| ------ | ---- | ---------- |
| `GET`  | `/health`  | Status kesehatan: `{"status","latency_ms"}` dengan HTTP `200`/`500`. |
| `POST` | `/control` | Mengubah mode gangguan. |

Mode yang didukung: `healthy`, `latency`, `error`, `unhealthy`.

Contoh:

```bash
# Kondisi normal (HTTP 200)
curl -s localhost:8081/health
# {"status":"healthy","latency_ms":0}

# Ubah mode menjadi unhealthy
curl -s -X POST -d '{"mode":"unhealthy"}' localhost:8081/control

# Sekarang /health membalas HTTP 500
curl -s -w '\n%{http_code}\n' localhost:8081/health
# {"status":"unhealthy","latency_ms":0}
# 500

# Menambah latensi 200 ms
curl -s -X POST -d '{"mode":"latency","latency_ms":200}' localhost:8081/control

# Error acak 30% untuk mode error
curl -s -X POST -d '{"mode":"error","error_rate":0.3}' localhost:8081/control
```

> Catatan lingkungan: pada host pengembangan, port `8081` dipakai oleh vhost
> nginx (`sre_lab_fkkmk`). Untuk mengujinya, jalankan dengan
> `FAULT_INJECTOR_ADDR=:18081`.

## Executor (Lab + Prod)

Executor menjalankan aksi remediation. Jenisnya diatur di `config.yaml`
(`executor.type`). Executor `lab` adalah default dan **tidak dihapus**: ia hanya
POST `/control` ke Fault Injector (aman, dummy) untuk pengujian.

| Type        | Perintah / Aksi                                  | Risk   |
| ----------- | ------------------------------------------------ | ------ |
| `lab`       | `POST /control` ke Fault Injector                | low    |
| `systemctl` | `systemctl restart <service>`                    | medium |
| `kubectl`   | `kubectl rollout restart deployment/<name> -n <ns>` | medium |
| `aws_ec2`   | EC2 `RebootInstances` (via interface `EC2API`)   | high   |
| `ansible`   | `ansible-playbook <runbook> -l <target>`         | high   |

Aturan keamanan untuk semua executor:
- Cek **Policy Engine** dulu (allowlist service/target/runbook); ditolak = tidak dieksekusi.
- Tulis ke **Journal + SQLite SEBELUM dan SESUDAH** eksekusi (audit trail).
- **Fail closed**, ada **timeout**, dan **tanpa shell** (memakai
  `exec.CommandContext` dengan argumen slice, bukan `bash -c`), sehingga tidak
  bisa disuntik perintah.
- `kubectl` mendukung `params.dry_run=true` → menambah `--dry-run=server`.

Contoh `config.yaml`:

```yaml
executor:
  type: systemctl
  allowlist:
    targets: [checkout]
    runbooks: [runbooks/restart.yml]
  fault_injector_url: http://localhost:8081/control
```

Contoh aksi (Go):

```go
exec, _ := executor.Build(executor.BuildConfig{
    Type:           "systemctl",
    AllowedTargets: []string{"checkout"},
})
_, err := exec.Execute(ctx, executor.Action{
    IncidentID: "inc-1", Type: "restart_service", Target: "checkout",
})
```

## Observability

Fault Pilot mengekspos metrik, kesehatan, dan tracing untuk dirinya sendiri
(tanpa dependensi eksternal — format teks Prometheus ditulis sendiri).

| Endpoint   | Keterangan |
| ---------- | ---------- |
| `GET /metrics` | Metrik Prometheus (counter/gauge/histogram). |
| `GET /healthz` | Deep check: SQLite (`Ping`), worker, freshness poll terakhir. `200`/`503`. |
| `GET /readyz`  | Kesiapan service. `200`/`503`. |

Metrik utama (prefix `faultpilot_`):
- `incidents_total{source}` — insiden masuk per sumber.
- `actions_total{executor,status}` — aksi remediation (success/failed/rejected).
- `policy_rejections_total{reason}` — penolakan policy beserta alasannya.
- `executor_duration_seconds{executor}` — histogram latensi eksekutor.
- `worker_queue_depth` — kedalaman antrean worker (real-time).
- `events_dropped_total` — event dibuang karena antrean penuh (fail closed).

Self-monitoring: `SelfMonitor` memberi peringatan **self-degraded** bila
kedalaman antrean > 90 atau error-rate aksi > 50% (fail closed, bukan sekadar
log). Setiap insiden terkait `trace_id` (lihat `observability.WithTraceID`).

Konfigurasi:

```yaml
observability:
  metrics_addr: ":9090"
  store_path: "faultpilot.db"
  last_poll_max: "2m"
```

Dashboard Grafana siap impor ada di [`dashboards/faultpilot.json`](dashboards/faultpilot.json).

## RBAC & Human Approval Gate

Aksi berisiko **tinggi tidak boleh dieksekusi otomatis**. Setiap aksi dengan risk
tertentu wajib disetujui manusia yang berwenang. Prinsipnya fail closed: user
tanpa role/permission, role tak dikenal, atau approval kadaluarsa => **DITOLAK**.

RBAC dibaca dari `config/rbac.yaml`:

```yaml
roles:
  viewer: [read]
  sre: [read, approve:low, approve:medium]
  sre_lead: [read, approve:low, approve:medium, approve:high]
users:
  ronz@company.com: sre_lead
  sre@company.com: sre
  viewer@company.com: viewer
```

Permission: `read`, `approve:low`, `approve:medium`, `approve:high`.

Alur approval (HTTP API, default `:8082`):

| Method | Path | Aksi | Permission |
| ------ | ---- | ---- | ---------- |
| `GET`  | `/approvals` | Daftar request (`?status=pending`) | `read` |
| `POST` | `/approvals` | Buat permohonan approval | `read` |
| `GET`  | `/approvals/{id}` | Lihat satu request | `read` |
| `POST` | `/approvals/{id}/approve` | Setujui | `approve:<risk>` |
| `POST` | `/approvals/{id}/reject` | Tolak | `approve:<risk>` |

Identitas diambil dari header `X-Actor` (mis. dari SSO). Contoh:

```bash
# SRE meminta approval aksi high-risk
curl -H 'X-Actor: sre@company.com' -X POST localhost:8082/approvals \
  -d '{"incident_id":"inc-1","action":"restart_simulated_service","target":"checkout","risk":"high"}'

# SRE lead menyetujui (four-eyes: approver != pemohon)
curl -H 'X-Actor: ronz@company.com' -X POST localhost:8082/approvals/<id>/approve -d '{"note":"lgtm"}'
```

Aturan:
- **Four-eyes**: pemohon tidak boleh menyetujui permohonannya sendiri.
- **Sekali pakai**: approval yang dipakai policy langsung ditandai `consumed`.
- **Kadaluarsa** (`ttl`, default 15 menit) => ditolak.
- Policy Engine memanggil gate sebelum mengklaim insiden: risk yang wajib
  approval (default `[high]`) tanpa approval => aksi tidak dijalankan.

Konfigurasi:

```yaml
approval:
  enabled: true
  addr: ":8082"
  rbac_path: "config/rbac.yaml"
  require_risks: [high]
  ttl: "15m"
```

## Pengujian

```bash
# Seluruh test
go test ./...

# Dengan deteksi race (disarankan untuk paket konkuren)
go test -race ./...

# Test konkurensi secara berulang untuk memastikan tidak flaky
go test -race -count=5 ./internal/collector/ ./internal/worker/
```

Daftar test penting:

| Test | Lokasi | Yang diuji |
| ---- | ------ | ---------- |
| T02  | `internal/policy`    | Proposal valid lolos. |
| T03  | `internal/policy`    | JSON tidak valid ditolak. |
| T04  | `internal/policy`    | Target di luar allowlist ditolak. |
| T05  | `internal/collector` | 100 event sama → 1 insiden (dedup). |
| T06  | `internal/collector` | 1000 event konkuren tetap terbatas (bounded). |
| T09  | `internal/policy`    | Dua worker pada insiden sama → tidak dobel. |
| T10  | `internal/audit`     | Proses mati setelah exec → state pulih, tidak dobel. |
| T11  | `internal/audit`     | Gagal tulis audit/DB → aksi baru ditahan. |
| T12  | `internal/collector` | Shutdown saat kanal penuh tidak menggantung. |
| T13  | `internal/investigation` | Evidence tidak berurutan tetap dirangkum. |
| T14  | `internal/verifier`  | Tidak recovered, tidak ada retry tak terhingga. |
| T16  | `internal/executor`  | Limit konkurensi per dependency untuk 2 insiden. |
| -    | `internal/executor`  | systemctl/kubectl/ec2/ansible: allowlist, dry-run, journal before/after, anti-injection. |
| -    | `internal/config`    | Load `config.yaml`; file hilang → default `lab`. |
| -    | `internal/worker`    | Konkurensi tidak melebihi batas maksimum. |
| -    | `internal/store`     | SQLite memakai WAL dan data tersimpan. |
| -    | `internal/observability` | Metrik/kontrol eksposisi, `/healthz`+`/readyz`, heartbeat, self-monitor, trace id, instrumentasi executor. |
| -    | `internal/approval`  | RBAC (role/permission), gate approval (four-eyes, sekali pakai, kadaluarsa), HTTP API `/approvals`. |
| -    | `internal/policy`    | Risk level + wajib approval untuk risk tertentu (fail closed). |
| -    | `lab/faultinjector`  | Perilaku tiap mode dan validasi `/control`. |

## Cara Kerja Singkat

1. **Collector** menerima event lewat `Push()`. Event duplikat dibuang, dan
   event disimpan pada kanal terbatas (default 100). Bila penuh → ditolak
   (fail closed), tidak memblokir pemanggil.
2. **Worker pool** (maksimum 5 konkuren) memproses event menjadi insiden.
3. **Store** (SQLite mode WAL) menyimpan `events` dan `incidents`.
4. **Policy Engine** memvalidasi proposal (allowlist aksi/target, insiden
   aktif, bukti milik insiden, anti-dobel/cooldown/limit). Proposal tidak valid
   tidak pernah dieksekusi.
5. **ActionJournal** mencatat aksi ke SQLite sebelum dan sesudah eksekusi,
   plus audit trail JSONL. Saat startup, `Reconcile` memulihkan aksi yang
   terputus dan mencegah eksekusi ganda.

Detail arsitektur ada di [`docs/ARSITEKTUR.md`](docs/ARSITEKTUR.md), [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md), [`docs/API.md`](docs/API.md), dan [`docs/JOURNAL.md`](docs/JOURNAL.md).

## Aturan Desain

- **Fail closed**: saat ambigu/penuh/gagal, sistem memilih berhenti/tolak,
  bukan memaksakan aksi.
- **Tanpa Kubernetes**: tidak ada dependensi ke API k8s.
- **Bounded**: antrean dan konkurensi selalu dibatasi.


## Audit log (contoh)
```json
{"action_id":"...","idempotency_key":"...","state":"executing","incident_id":"inc-1","action":"restart_simulated_service","target":"checkout","updated_at":"2026-10-14T...Z"}
```


## Fitur
- **Fail-closed**: policy menolak proposal tidak valid, executor deadline + hold journal saat gagal.
- **Bounded & dedup**: collector kanal 100, dedup by content, worker pool max 5.
- **Safe proposal**: allowlist aksi/target, anti-double running, cooldown, limit per incident.
- **Recoverable journal**: SQLite WAL + audit JSONL, reconcile state executing/pending -> unknown.
- **Idempotent execution**: ActionJournal pakai IdempotencyKey.
- **Audit trail**: setiap transisi state dicatat (fail-closed on write fail).
- **Gemini CLI adapter**: timeout 10s, max 4KB, backoff+jitter 429, sanitasi evidence.
- **Verifier**: health check before/after -> recovered/not_recovered/unknown.
- **Observability**: `/metrics` (Prometheus text), `/healthz`+`/readyz`, trace_id, self-degraded warning, dashboard Grafana.
- **RBAC + Human Approval**: aksi high-risk wajib disetujui `approve:high`; four-eyes, sekali pakai, kadaluarsa.
- **No K8s**: hanya HTTP + subprocess + SQLite.
