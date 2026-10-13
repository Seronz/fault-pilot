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
| C04+ | Investigation, Policy, Executor, Verifier, Audit, Gemini | ⏳ Belum |

## Struktur Direktori

```
.
├── cmd/
│   └── faultpilot/          # entry point aplikasi (menjalankan fault injector)
├── internal/
│   ├── domain/              # tipe data inti (Proposal, Event, Incident, ActionResult)
│   ├── collector/           # penerima event: kanal terbatas + dedup
│   ├── worker/              # worker pool (batas konkurensi)
│   ├── store/               # penyimpanan SQLite (WAL): tabel events & incidents
│   ├── audit/               # (placeholder) logging audit
│   ├── executor/            # (placeholder) eksekusi aksi
│   ├── gemini/              # (placeholder) adapter LLM
│   ├── investigation/       # (placeholder) investigasi
│   ├── policy/              # (placeholder) kebijakan/evaluasi
│   └── verifier/            # (placeholder) verifikasi hasil
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
| T05  | `internal/collector` | 100 event sama → 1 insiden (dedup). |
| T06  | `internal/collector` | 1000 event konkuren tetap terbatas (bounded). |
| T12  | `internal/collector` | Shutdown saat kanal penuh tidak menggantung. |
| -    | `internal/worker`    | Konkurensi tidak melebihi batas maksimum. |
| -    | `internal/store`     | SQLite memakai WAL dan data tersimpan. |
| -    | `lab/faultinjector`  | Perilaku tiap mode dan validasi `/control`. |

## Cara Kerja Singkat

1. **Collector** menerima event lewat `Push()`. Event duplikat dibuang, dan
   event disimpan pada kanal terbatas (default 100). Bila penuh → ditolak
   (fail closed), tidak memblokir pemanggil.
2. **Worker pool** (maksimum 5 konkuren) memproses event menjadi insiden.
3. **Store** (SQLite mode WAL) menyimpan `events` dan `incidents`.

Detail arsitektur ada di [`docs/ARSITEKTUR.md`](docs/ARSITEKTUR.md) dan
[`docs/API.md`](docs/API.md).

## Aturan Desain

- **Fail closed**: saat ambigu/penuh/gagal, sistem memilih berhenti/tolak,
  bukan memaksakan aksi.
- **Tanpa Kubernetes**: tidak ada dependensi ke API k8s.
- **Bounded**: antrean dan konkurensi selalu dibatasi.
