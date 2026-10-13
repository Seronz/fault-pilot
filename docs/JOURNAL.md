# ActionJournal & Audit Trail

C05 menambahkan **ActionJournal** untuk mencatat siklus hidup aksi, serta
**audit trail JSONL** untuk pencatatan yang tidak dapat diubah.

## 1. Tujuan

- **Idempotensi**: aksi dengan `IdempotencyKey` yang sama tidak dieksekusi dua kali.
- **Recoverability**: bila proses mati saat aksi sedang berjalan (`executing`),
  state dipulihkan menjadi `unknown` saat startup (tidak mengeksekusi ulang).
- **Auditability**: setiap perubahan state dicatat ke JSONL (satu baris per perubahan).
- **Fail closed**: bila gagal menulis (DB atau audit), journal memasuki state
  `held` dan menolak aksi baru.

## 2. State Aksi

| State | Keterangan |
| ----- | ---------- |
| `pending` | Aksi telah dicatat ke jurnal, belum mulai dieksekusi. |
| `executing` | Aksi sedang berjalan (ditulis SEBELUM eksekusi). |
| `succeeded` | Eksekusi berhasil. |
| `failed` | Eksekusi gagal. |
| `unknown` | Hasil tidak diketahui (proses mati saat running). |

Alur normal: `pending → executing → succeeded/failed`.

## 3. Tabel SQLite `journal`

Tabel `journal` disimpan bersama DB utama (SQLite, mode WAL).

```sql
CREATE TABLE IF NOT EXISTS journal (
    action_id       TEXT PRIMARY KEY,        -- UUID unik
    idempotency_key TEXT NOT NULL UNIQUE,    -- kunci anti-eksekusi ganda
    incident_id     TEXT NOT NULL DEFAULT '',
    action          TEXT NOT NULL DEFAULT '',
    target          TEXT NOT NULL DEFAULT '',
    state           TEXT NOT NULL,
    message         TEXT NOT NULL DEFAULT '',
    updated_at      INTEGER NOT NULL          -- UnixNano (UTC)
);
```

`SaveJournalEntry` menggunakan `INSERT ... ON CONFLICT(action_id) DO UPDATE`
untuk melakukan upsert saat transisi state.

## 4. ActionJournal (`internal/audit/journal.go`)

Method utama:

| Method | Keterangan |
| ------ | ---------- |
| `Start(ctx, Action)` | Mencatat aksi. Menulis `pending` lalu `executing` (sebelum eksekusi). Bila `IdempotencyKey` sudah ada → `ErrAlreadyExists` (tidak dieksekusi ulang). |
| `Succeed(ctx, actionID, msg)` | Transisi → `succeeded`. |
| `Fail(ctx, actionID, msg)` | Transisi → `failed`. |
| `Unknown(ctx, actionID, msg)` | Transisi → `unknown`. |
| `Reconcile(ctx)` | Dipanggil saat startup: semua entri `pending`/`executing` dipulihkan menjadi `unknown`. |
| `Held()` | True bila journal menahan aksi baru akibat kegagalan tulis (fail closed). |

## 5. Audit Trail JSONL (`internal/audit/jsonl.go`)

Setiap perubahan state ditulis sebagai satu baris JSON (`JournalEntry`). Audit
ditulis setelah berhasil menyimpan ke SQLite. Bila penulisan audit gagal,
journal masuk mode `held`.

Contoh baris:

```json
{"action_id":"7fa930e0-...","idempotency_key":"key-1","incident_id":"inc-1","action":"restart_simulated_service","target":"checkout","state":"executing","updated_at":"2026-10-14T...Z"}
```

## 6. Alur Pemulihan (T10)

1. Proses menulis state `executing` ke SQLite lalu "mati".
2. Pada startup, proses baru membuka DB yang sama dan memanggil `Reconcile()`.
3. `Reconcile()` mengubah semua entri `pending`/`executing` menjadi `unknown`.
4. Percobaan eksekusi ulang dengan `IdempotencyKey` yang sama menghasilkan
   `ErrAlreadyExists` — **tidak ada eksekusi ganda**.

## 7. Penahanan Aksi Baru (T11)

Bila `SaveJournalEntry` (DB) gagal atau `Append` (audit) gagal, `Journal`:
- Memanggil `fail()` → `held = true`.
- Mengembalikan error.
- Semua `Start()` berikutnya mengembalikan `ErrHeld` (aksi baru ditahan).

Ini menjamin fail closed: sistem tidak melanjutkan eksekusi bila catatan
audit/persistensi tidak dapat dipercaya.