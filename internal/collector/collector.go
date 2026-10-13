// Package collector menerima event masuk, membuang duplikat, lalu
// mendistribusikannya ke worker pool untuk diproses menjadi insiden.
//
// Prinsip desain:
//   - Fail closed: Push tidak pernah memblokir pemanggil. Bila kanal penuh
//     atau collector sudah ditutup, event ditolak (return false).
//   - Bounded: jumlah event yang menunggu dibatasi oleh kapasitas kanal
//     (default 100) sehingga pemakaian memori terkendali.
//   - Dedup: event dengan isi sama hanya diterima sekali.
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

// Collector menerima event dan memprosesnya secara asinkron.
type Collector struct {
	events  chan domain.Event // kanal terbatas berisi event yang belum diproses
	pool    *worker.Pool      // worker pool pembatas konkurensi
	process Processor         // callback pemrosesan event
	log     *slog.Logger

	mu   sync.Mutex          // melindungi peta dedup
	seen map[string]struct{} // kunci dedup yang sudah diterima
	seq  atomic.Uint64       // penghitung untuk membangkitkan ID event

	ctx    context.Context // dibatalkan saat Close untuk membatalkan pekerjaan
	cancel context.CancelFunc
	done   chan struct{}  // ditutup saat Close
	once   sync.Once      // memastikan Close hanya dijalankan sekali
	wg     sync.WaitGroup // menunggu goroutine consumer berhenti

	accepted atomic.Uint64 // jumlah event yang diterima
	dropped  atomic.Uint64 // jumlah event yang ditolak karena kanal penuh
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
		seen:    make(map[string]struct{}),
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
//   - event merupakan duplikat dari yang sudah pernah diterima, atau
//   - kanal sedang penuh, atau
//   - collector sudah ditutup.
//
// Push tidak pernah memblokir pemanggil.
func (c *Collector) Push(e domain.Event) bool {
	// Bangkitkan ID event bila belum ada, dengan format "ev-000001".
	if e.ID == "" {
		e.ID = fmt.Sprintf("ev-%06d", c.seq.Add(1))
	}
	key := dedupKey(e)

	// Tandai sudah diterima lebih dulu agar request duplikat yang datang
	// bersamaan ikut terbuang.
	c.mu.Lock()
	if _, ok := c.seen[key]; ok {
		c.mu.Unlock()
		return false
	}
	c.seen[key] = struct{}{}
	c.mu.Unlock()

	// Tolak lebih awal bila collector sudah ditutup.
	select {
	case <-c.done:
		c.forget(key)
		return false
	default:
	}

	// Kirim tanpa memblokir; bila penuh, kembalikan tanda dedup agar event
	// bisa dicoba lagi di masa depan.
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
			c.pool.Submit(c.ctx, func() {
				if err := c.process(c.ctx, ev); err != nil {
					c.log.Error("process event failed", "event_id", ev.ID, "err", err)
				}
			})
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

// Accepted mengembalikan total event yang diterima.
func (c *Collector) Accepted() uint64 { return c.accepted.Load() }

// Dropped mengembalikan total event yang ditolak karena kanal penuh.
func (c *Collector) Dropped() uint64 { return c.dropped.Load() }

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
