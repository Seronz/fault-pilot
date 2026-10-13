# Arsitektur Fault Pilot

Dokumen ini menjelaskan arsitektur internal, alur data, dan keputusan desain
pada komponen yang sudah diimplementasikan (C01–C03).

## 1. Gambaran Umum

Fault Pilot dirancang sebagai pipeline penanganan insiden:

```
        Push(event)
            │
            ▼
   ┌─────────────────┐   dedup + kanal terbatas (100)
   │    Collector    │───────────────────────────────┐
   └─────────────────┘                               │
                                                     ▼
                                        ┌───────────────────────┐
                                        │      Worker Pool       │
                                        │  maks 5 konkuren       │
                                        └───────────────────────┘
                                                     │
                                                     ▼
                                        ┌───────────────────────┐
                                        │   Store (SQLite WAL)   │
                                        │  tabel events/incidents│
                                        └───────────────────────┘
```

Fault Injector (`lab/faultinjector`) berdiri di sisi lain: ia adalah *target*
yang meniru gangguan untuk menguji sistem.

## 2. Komponen

### 2.1 `internal/domain` (C01)

Tipe data inti tanpa dependensi infrastruktur:

| Tipe | Kegunaan |
| ---- | -------- |
| `Proposal` | Usulan tindakan perbaikan dari tahap investigasi. |
| `Event` | Satu kejadian/telemetri yang dikumpulkan. |
| `Incident` | Kumpulan bukti yang jadi satu masalah. |
| `ActionResult` | Hasil eksekusi sebuah proposal. |

Semua tipe memiliki tag JSON agar stabil saat serialisasi.

### 2.2 `internal/collector` (C03)

Tanggung jawab: menerima event, membuang duplikat, meneruskan ke worker pool.

- **Kanal terbatas** berkapasitas `DefaultBufferSize = 100`.
- **`Push(e) bool`** bersifat non-blocking:
  - `true` bila diterima,
  - `false` bila duplikat, kanal penuh, atau collector sudah ditutup.
- **ID event** dibangkitkan otomatis bila kosong dengan format `ev-%06d`.
- **Dedup** memakai kunci `Type + "\x00" + Service + "\x00" + Message`
  (ID dan timestamp diabaikan). Saat pengiriman gagal karena kanal penuh,
  kunci dedup dibatalkan kembali agar event bisa dicoba lagi.
- **`Close()`** membatalkan context, menghentikan consumer, lalu menunggu
  pekerjaan yang berjalan selesai. Aman dipanggil berkali-kali.

### 2.3 `internal/worker` (C03)

Worker pool generik dengan batas konkurensi.

- `New(max)` membuat pool; `max <= 0` menjadi `1`.
- `Submit(ctx, job) bool` menerima job; menunggu slot bila penuh, dan
  mengembalikan `false` bila context dibatalkan atau pool ditutup.
- `Close()` menandai pool tertutup dan menunggu job aktif selesai.
- Penjagaan `closed` dilakukan di bawah `sync.Mutex` agar tidak ada job yang
  lolos setelah `Close` mulai menunggu (menghindari race pada `WaitGroup`).

### 2.4 `internal/store` (C03)

Penyimpanan SQLite dengan mode **WAL**.

- Koneksi dibatasi `SetMaxOpenConns(1)` agar tidak terkena `database is locked`.
- PRAGMA yang dipakai: `journal_mode=WAL`, `busy_timeout=5000`, `foreign_keys=ON`.
- Operasi: `AppendEvent`, `CreateIncident`, `CountEvents`, `CountIncidents`,
  `JournalMode`, `Close`.

Skema:

```sql
CREATE TABLE events (
    id        TEXT PRIMARY KEY,
    timestamp INTEGER NOT NULL,   -- UnixNano (UTC)
    type      TEXT NOT NULL,
    service   TEXT NOT NULL,
    message   TEXT NOT NULL
);

CREATE TABLE incidents (
    id           TEXT PRIMARY KEY,
    status       TEXT NOT NULL,
    evidence_ids TEXT NOT NULL DEFAULT '[]',  -- array JSON
    created_at   INTEGER NOT NULL             -- UnixNano (UTC)
);
```

Penulisan memakai `INSERT OR IGNORE` sehingga idempoten terhadap ID duplikat.

### 2.5 `lab/faultinjector` (C02)

Layanan HTTP penyimulasi gangguan pada port `8081` (bisa dioverride lewat
`FAULT_INJECTOR_ADDR`).

- State (`Mode`, `LatencyMS`, `ErrorRate`) dilindungi `sync.RWMutex`.
- `/health` mengembalikan `{"status","latency_ms"}` dengan kode `200`/`500`.
- `/control` memvalidasi payload; payload tidak valid → `400`.

Detail API ada di [`API.md`](API.md).

## 3. Alur Data (End-to-End)

1. Pemanggil memanggil `Collector.Push(event)`.
2. Collector membangkitkan ID bila perlu, mengecek dedup, lalu mengirim ke
   kanal secara non-blocking.
3. Goroutine consumer mengambil event dan menyerahkannya ke `Worker.Pool`.
4. Worker (maks 5 sekaligus) memproses event — misalnya menyimpan ke store dan
   membuat insiden.
5. Saat shutdown, `Collector.Close()` membatalkan context dan menunggu semua
   pekerjaan selesai.

## 4. Keputusan Desain

| Keputusan | Alasan |
| --------- | ------ |
| Fail closed | Saat penuh/gagal, event ditolak daripada memblokir atau membanjiri memori. |
| Kanal bounded (100) | Membatasi pemakaian memori dan memberi backpressure. |
| Dedup berbasis isi | 100 event identik tidak menghasilkan 100 insiden. |
| Konkurensi maksimum 5 | Melindungi store (SQLite) dan sumber daya lain. |
| SQLite WAL | Pembacaan/penulisan lebih mulus, satu penulis tetap aman. |
| Tanpa Kubernetes | Menjaga dependensi minimal dan portabilitas. |

## 5. Referensi Pengujian

- `go test ./...` — seluruh test.
- `go test -race ./...` — deteksi data race.
- T05, T06, T12 berada di `internal/collector/collector_test.go`.
