# Tentang Fault Pilot

Fault Pilot adalah sistem otomasi penanganan gangguan (incident response
automation) berbasis fail-closed, tanpa dependensi Kubernetes.

## Tujuan
- Mengurangi waktu respon terhadap gangguan dengan memproses event secara terkontrol.
- Menjamin keamanan aksi (allowlist, cooldown, limit, anti-double execution).
- Menyediakan audit trail dan pemulihan saat proses mati (reconciliation).

## Arsitektur Inti
- **Collector**: menerima event, dedup, kanal terbatas.
- **Policy Engine**: validasi proposal sebelum eksekusi.
- **ActionJournal**: persist + audit trail JSONL, idempotensi & recovery.
- **Executor**: mengirim aksi hanya ke Fault Injector (HTTP), kontrol konkurensi per dependency.
- **Verifier**: verifikasi kesehatan sebelum/sesudah (health check).
- **Investigator**: merangkum insiden dari evidence nyata.

## Cara Menjalankan
```bash
go build ./...
go test ./...
go run ./cmd/faultpilot  # default :8081; FAULT_INJECTOR_ADDR untuk override
```

## Lisensi
Proyek latihan/internal untuk penelitian dan pembelajaran.
