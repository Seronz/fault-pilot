// Package collector menerima event masuk, membuang duplikat, lalu
// mendistribusikannya ke worker pool untuk diproses menjadi insiden.
//
// Prinsip desain:
//   - Fail closed: Push tidak pernah memblokir pemanggil. Bila kanal penuh
//     atau collector sudah ditutup, event ditolak (return false).
//   - Bounded: jumlah event yang menunggu dibatasi oleh kapasitas kanal
//     (default 100) sehingga pemakaian memori terkendali.
//   - Dedup: event dengan isi sama hanya diproses sekali. Event yang GAGAL
//     diproses boleh dicoba lagi lewat Push (lihat EventState).
package collector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/seronz/fault-pilot/internal/domain"
	"github.com/seronz/fault-pilot/internal/worker"
)

const (
	// DefaultBufferSize adalah kapasitas kanal event (batas antrean).
	DefaultBufferSize = 100
	// DefaultMaxConcurrent adalah jumlah maksimum insiden yang diproses
	// secara bersamaan.
	DefaultMaxConcurrent = 5
)

// Processor adalah fungsi yang memproses satu event. Implementasinya biasanya
// menulis event ke store dan membuat/meng-update insiden.
type Processor func(ctx context.Context, e domain.Event) error

// EventState adalah status daur hidup sebuah event di peta dedup.
type EventState string

const (
	EventPending EventState = "pending" // diterima, menunggu diproses
	EventRunning EventState = "running" // sedang diproses worker
	EventDone    EventState = "done"    // selesai diproses dengan sukses
	EventFailed  EventState = "failed"  // gagal diproses; boleh dicoba ulang
)

// Collector menerima event dan memprosesnya secara asinkron.
type Collector struct {
	events  chan domain.Event // kanal terbatas berisi event yang belum diproses
	pool    *worker.Pool      // worker pool pembatas konkurensi
	process Processor         // callback pemrosesan event
	log     *slog.Logger

	mu   sync.Mutex            // melindungi peta dedup
	seen map[string]EventState // kunci dedup -> status daur hidup event
	seq  atomic.Uint64         // penghitung untuk membangkitkan ID event

	ctx    context.Context // dibatalkan saat Close untuk membatalkan pekerjaan
	cancel context.CancelFunc
	done   chan struct{}  // ditutup saat Close
	once   sync.Once      // memastikan Close hanya dijalankan sekali
	wg     sync.WaitGroup // menunggu goroutine consumer berhenti

	accepted   atomic.Uint64 // jumlah event yang diterima ke antrean
	dropped    atomic.Uint64 // jumlah event yang ditolak karena kanal penuh
	duplicates atomic.Uint64 // jumlah event yang ditolak karena duplikat
	retried    atomic.Uint64 // jumlah event gagal yang dicoba ulang
	completed  atomic.Uint64 // jumlah event yang selesai diproses
	failed     atomic.Uint64 // jumlah event yang gagal diproses
}

// New membuat Collector baru dan langsung menjalankan goroutine consumer.
//
// Parameter:
//   - size: kapasitas kanal event (<=0 memakai DefaultBufferSize).
//   - maxConcurrent: batas proses bersamaan (<=0 memakai DefaultMaxConcurrent).
//   - process: callback pemrosesan event (nil diabaikan/di-no-op).
//   - logger: logger slog (nil memakai slog.Default()).
func New(size, maxConcurrent int, process Processor, logger *slog.Logger) *Collector {
	if size <= 0 {
		size = DefaultBufferSize
	}
	if maxConcurrent <= 0 {
		maxConcurrent = DefaultMaxConcurrent
	}
	if process == nil {
		process = func(context.Context, domain.Event) error { return nil }
	}
	if logger == nil {
		logger = slog.Default()
	}

	ctx, cancel := context.WithCancel(context.Background())
	c := &Collector{
		events:  make(chan domain.Event, size),
		pool:    worker.New(maxConcurrent),
		process: process,
		log:     logger,
		seen:    make(map[string]EventState),
		ctx:     ctx,
		cancel:  cancel,
		done:    make(chan struct{}),
	}
	c.wg.Add(1)
	go c.run()
	return c
}

// Push mencoba memasukkan satu event ke antrean.
//
// Fungsi mengembalikan true bila event diterima, dan false bila:
//   - event duplikat dari event yang berstatus pending/running/done, atau
//   - kanal sedang penuh, atau
//   - collector sudah ditutup.
//
// Event yang berstatus EventFailed (gagal diproses sebelumnya) akan diterima
// kembali sehingga bisa dicoba ulang. Push tidak pernah memblokir pemanggil.
func (c *Collector) Push(e domain.Event) bool {
	// Bangkitkan ID event bila belum ada, dengan format "ev-000001".
	if e.ID == "" {
		e.ID = fmt.Sprintf("ev-%06d", c.seq.Add(1))
	}
	key := dedupKey(e)

	// Kelola status dedup lebih dulu agar request duplikat yang datang
	// bersamaan ikut terbuang. Event yang sebelumnya GAGAL boleh dicoba lagi;
	// selain itu (pending/running/done) dianggap duplikat.
	c.mu.Lock()
	if st, ok := c.seen[key]; ok {
		if st != EventFailed {
			c.mu.Unlock()
			c.duplicates.Add(1)
			return false
		}
		c.retried.Add(1)
	}
	c.seen[key] = EventPending
	c.mu.Unlock()

	// Tolak lebih awal bila collector sudah ditutup.
	select {
	case <-c.done:
		c.forget(key)
		return false
	default:
	}

	// Kirim tanpa memblokir; bila penuh, buang kunci dedup agar event bisa
	// dicoba lagi di masa depan.
	select {
	case c.events <- e:
		c.accepted.Add(1)
		return true
	default:
		c.forget(key)
		c.dropped.Add(1)
		return false
	}
}

// run adalah goroutine consumer yang membaca event dari kanal lalu
// menyerahkannya ke worker pool.
func (c *Collector) run() {
	defer c.wg.Done()
	for {
		select {
		case <-c.done:
			return
		case e := <-c.events:
			ev := e
			key := dedupKey(ev)
			ok := c.pool.Submit(c.ctx, func() {
				c.setState(key, EventRunning)
				if err := c.process(c.ctx, ev); err != nil {
					c.setState(key, EventFailed)
					c.failed.Add(1)
					c.log.Error("process event failed", "event_id", ev.ID, "err", err)
					return
				}
				c.setState(key, EventDone)
				c.completed.Add(1)
			})
			if !ok {
				// Submit ditolak (context dibatalkan / pool ditutup): event
				// tidak diproses. Tandai gagal agar tidak dianggap selesai dan
				// tetap bisa dicoba ulang.
				c.setState(key, EventFailed)
				c.failed.Add(1)
				c.log.Error("submit event failed", "event_id", ev.ID)
			}
		}
	}
}

// Close menghentikan penerimaan event baru dan menunggu semua pekerjaan
// yang sedang berjalan selesai. Setelah Close, Push selalu mengembalikan false.
//
// Close aman dipanggil lebih dari sekali.
func (c *Collector) Close() error {
	c.once.Do(func() {
		// Batalkan context lebih dulu agar Submit yang sedang menunggu slot
		// ikut berhenti, lalu sinyalkan consumer untuk keluar.
		c.cancel()
		close(c.done)
	})
	c.wg.Wait()
	c.pool.Close()
	return nil
}

// Len mengembalikan jumlah event yang sedang menunggu di kanal.
func (c *Collector) Len() int { return len(c.events) }

// Cap mengembalikan kapasitas kanal event.
func (c *Collector) Cap() int { return cap(c.events) }

// Healthy memeriksa apakah worker pool masih responsif (tidak deadlocked).
// Dipakai untuk health check (/healthz).
func (c *Collector) Healthy(ctx context.Context) error {
	select {
	case <-c.done:
		return errors.New("collector closed")
	default:
	}
	done := make(chan struct{})
	if !c.pool.Submit(ctx, func() { close(done) }) {
		return errors.New("worker pool busy or closed")
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Accepted mengembalikan total event yang diterima ke antrean.
func (c *Collector) Accepted() uint64 { return c.accepted.Load() }

// Dropped mengembalikan total event yang ditolak karena kanal penuh.
func (c *Collector) Dropped() uint64 { return c.dropped.Load() }

// Duplicates mengembalikan total event yang ditolak karena duplikat.
func (c *Collector) Duplicates() uint64 { return c.duplicates.Load() }

// Retried mengembalikan total event gagal yang dicoba ulang.
func (c *Collector) Retried() uint64 { return c.retried.Load() }

// Completed mengembalikan total event yang selesai diproses dengan sukses.
func (c *Collector) Completed() uint64 { return c.completed.Load() }

// Failed mengembalikan total event yang gagal diproses; event seperti ini
// masih boleh dicoba ulang lewat Push.
func (c *Collector) Failed() uint64 { return c.failed.Load() }

// Status mengembalikan status daur hidup terakhir untuk event dengan isi sama.
// ok bernilai false bila event belum pernah diterima.
func (c *Collector) Status(e domain.Event) (st EventState, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st, ok = c.seen[dedupKey(e)]
	return st, ok
}

// setState memperbarui status dedup sebuah key. Bila key sudah dibuang, tidak
// ada yang dilakukan agar status event lama tidak hidup kembali.
func (c *Collector) setState(key string, st EventState) {
	c.mu.Lock()
	if _, ok := c.seen[key]; ok {
		c.seen[key] = st
	}
	c.mu.Unlock()
}

// forget menghapus kunci dedup, dipakai saat event gagal dikirim ke kanal.
func (c *Collector) forget(key string) {
	c.mu.Lock()
	delete(c.seen, key)
	c.mu.Unlock()
}

// dedupKey membangun kunci dedup dari isi event.
// ID dan Timestamp sengaja diabaikan agar event dengan isi sama dianggap duplikat.
func dedupKey(e domain.Event) string {
	return e.Type + "\x00" + e.Service + "\x00" + e.Message
}
