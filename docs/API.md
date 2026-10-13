# Dokumentasi API — Fault Injector

Fault Injector adalah layanan HTTP di `lab/faultinjector` yang menyimulasikan
gangguan. Alamat default `:8081` (override lewat `FAULT_INJECTOR_ADDR`).

## Ringkasan Endpoint

| Method | Path | Deskripsi |
| ------ | ---- | --------- |
| `GET`  | `/health`  | Status kesehatan sesuai mode aktif. |
| `POST` | `/control` | Mengubah mode gangguan. |

---

## GET /health

Mengembalikan status kesehatan dan latensi yang disimulasikan.

**Respons** (`application/json`):

```json
{
  "status": "healthy",
  "latency_ms": 0
}
```

| Field | Tipe | Keterangan |
| ----- | ---- | ---------- |
| `status` | string | `healthy`, `unhealthy`, atau `error`. |
| `latency_ms` | int | Latensi yang disimulasikan (ms). |

**Kode status HTTP** bergantung mode:

| Mode | Kode | Catatan |
| ---- | ---- | ------- |
| `healthy` | `200` | Normal. |
| `latency` | `200` | Menunggu `latency_ms` sebelum merespons. |
| `error` | `200`/`500` | `500` dengan probabilitas `error_rate`. |
| `unhealthy` | `500` | Selalu error. |

Method selain `GET`/`HEAD` → `405 Method Not Allowed`.

---

## POST /control

Mengubah state gangguan.

**Request body** (`application/json`):

```json
{
  "mode": "latency",
  "latency_ms": 200,
  "error_rate": 0.0
}
```

| Field | Tipe | Wajib | Keterangan |
| ----- | ---- | ----- | ---------- |
| `mode` | string | ya | Salah satu dari `healthy`, `latency`, `error`, `unhealthy`. |
| `latency_ms` | int | tidak | ≥ 0. Dipakai saat mode `latency`. |
| `error_rate` | float | tidak | Antara `0` dan `1` (inklusif). Dipakai saat mode `error`. |

Field yang tidak dikenal akan **ditolak** (`DisallowUnknownFields`).
Ukuran body dibatasi 1 MiB.

**Respons sukses** (`200`) mengembalikan state baru:

```json
{
  "mode": "latency",
  "latency_ms": 200,
  "error_rate": 0
}
```

**Error:**

| Kode | Penyebab |
| ---- | -------- |
| `400` | JSON tidak valid, mode tidak dikenal, `latency_ms < 0`, atau `error_rate` di luar `[0,1]`. |
| `405` | Method bukan `POST`. |

---

## Contoh Penggunaan

```bash
# 1) Cek kondisi awal (200)
curl -s localhost:8081/health

# 2) Set mode unhealthy
curl -s -X POST -d '{"mode":"unhealthy"}' localhost:8081/control

# 3) Cek lagi -> 500
curl -s -w '\n%{http_code}\n' localhost:8081/health

# 4) Mode latency 200 ms
curl -s -X POST -d '{"mode":"latency","latency_ms":200}' localhost:8081/control

# 5) Mode error 30%
curl -s -X POST -d '{"mode":"error","error_rate":0.3}' localhost:8081/control

# 6) Kembali normal
curl -s -X POST -d '{"mode":"healthy"}' localhost:8081/control
```

## Catatan

- State dijaga `sync.RWMutex`, aman untuk request bersamaan.
- Log memakai `log/slog`.
- Pengujian ada di `lab/faultinjector/server_test.go`.
