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
| C07+ | Gemini nyata + sinkronisasi lanjut | ⏳ Belum |

## Struktur Direktori

```
.
├── cmd/
│   └── faultpilot/          # entry point aplikasi (menjalankan fault injector)
├── internal/
│   ├── domain/              # tipe data inti (Proposal, Event, Incident, Journal, ...)
│   ├── collector/           # penerima event: kanal terbatas + dedup
│   ├── worker/              # worker pool (batas konkurensi)
│   ├── store/               # penyimpanan SQLite (WAL): events, incidents, journal
│   ├── audit/               # ActionJournal + audit trail JSONL
│   ├── executor/            # eksekusi aksi ke Fault Injector (HTTP)
│   ├── gemini/              # adapter proposal (FakeAdapter)
│   ├── investigation/       # membangun ringkasan insiden dari evidence
│   ├── policy/              # validasi proposal (allowlist + aturan state)
│   └── verifier/            # verifikasi hasil remediation (health check)
├── lab/
│   └── faultinjector/       # layanan lab penyimulasi gangguan (HTTP)
├── docs/                    # dokumentasi tambahan
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
| -    | `internal/worker`    | Konkurensi tidak melebihi batas maksimum. |
| -    | `internal/store`     | SQLite memakai WAL dan data tersimpan. |
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
