// Package worker menyediakan worker pool sederhana dengan batas jumlah
// pekerjaan yang berjalan bersamaan (concurrency).
//
// Pool dipakai untuk membatasi berapa banyak insiden yang diproses sekaligus,
// sehingga beban ke database/kanal tetap terkendali.
package worker

import (
	"context"
	"sync"
)

// Pool adalah worker pool dengan batas konkurensi tetap.
//
// Semantik penting:
//   - Jumlah goroutine yang menjalankan job tidak pernah melebihi max.
//   - Submit akan menunggu (block) bila semua slot sedang terpakai, kecuali
//     context sudah dibatalkan atau pool sudah ditutup.
//   - Fail closed: setelah Close, semua Submit baru akan ditolak (false).
type Pool struct {
	max    int            // batas maksimum job yang berjalan bersamaan
	slots  chan struct{}  // semaphore pengatur slot konkurensi
	wg     sync.WaitGroup // menunggu job yang sedang berjalan selesai
	mu     sync.Mutex     // melindungi field closed
	closed bool           // menandai pool sudah ditutup
}

// New membuat Pool baru dengan batas konkurensi max.
// Bila max <= 0, nilai default 1 dipakai.
func New(max int) *Pool {
	if max <= 0 {
		max = 1
	}
	return &Pool{
		max:   max,
		slots: make(chan struct{}, max),
	}
}

// Max mengembalikan batas konkurensi pool.
func (p *Pool) Max() int { return p.max }

// Submit memasukkan sebuah job untuk dijalankan.
//
// Fungsi mengembalikan true bila job diterima, dan false bila:
//   - context sudah dibatalkan (ctx.Done), atau
//   - pool sudah ditutup.
//
// Submit boleh dipanggil dari banyak goroutine. Bila semua slot terpakai,
// pemanggil akan menunggu sampai ada slot yang bebas.
func (p *Pool) Submit(ctx context.Context, job func()) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	if job == nil {
		return false
	}

	// Ambil satu slot; batal bila context selesai lebih dulu.
	select {
	case <-ctx.Done():
		return false
	case p.slots <- struct{}{}:
	}

	// Cek status tertutup di bawah mutex agar tidak ada job yang lolos
	// setelah Close mulai menunggu (menghindari race pada WaitGroup).
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		<-p.slots
		return false
	}
	p.wg.Add(1)
	p.mu.Unlock()

	go func() {
		defer p.wg.Done()
		defer func() { <-p.slots }() // lepaskan slot saat job selesai
		job()
	}()
	return true
}

// Close menandai pool tertutup lalu menunggu semua job yang sedang berjalan
// selesai. Submit baru setelah Close akan selalu ditolak.
//
// Close aman dipanggil lebih dari sekali.
func (p *Pool) Close() {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	p.wg.Wait()
}
