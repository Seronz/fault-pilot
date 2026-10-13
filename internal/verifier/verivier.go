// Package verifier memverifikasi hasil remediation dengan memeriksa health
// sebelum dan sesudah aksi.
package verifier

import (
	"context"
	"errors"
	"net/http"
	"time"
)

var (
	ErrVerifierFail = errors.New("verifier: request failed")
)

// Verifier memverifikasi pemulihan berdasarkan status health.
type Verifier struct {
	client *http.Client
	url    string
}

// Option konfigurasi verifier.
type Option func(*Verifier)

// WithHTTPClient mengganti http client.
func WithHTTPClient(c *http.Client) Option {
	return func(v *Verifier) {
		if c != nil {
			v.client = c
		}
	}
}

// WithHealthURL mengatur URL health endpoint.
func WithHealthURL(url string) Option { return func(v *Verifier) { v.url = url } }

// New membuat Verifier baru.
func New(opts ...Option) *Verifier {
	v := &Verifier{
		client: &http.Client{Timeout: 5 * time.Second},
		url:    "http://localhost:8081/health",
	}
	for _, opt := range opts {
		opt(v)
	}
	return v
}

type healthResp struct {
	Status    string `json:"status"`
	LatencyMS int    `json:"latency_ms"`
}

// Result hasil verifikasi.
type Result string

const (
	ResultRecovered    Result = "recovered"
	ResultNotRecovered Result = "not_recovered"
	ResultUnknown      Result = "unknown"
)

// CheckHealth memeriksa status health saat ini.
func (v *Verifier) CheckHealth(ctx context.Context) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.url, nil)
	if err != nil {
		return ResultUnknown, ErrVerifierFail
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return ResultUnknown, ErrVerifierFail
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return ResultRecovered, nil
	}
	if resp.StatusCode >= 500 {
		return ResultNotRecovered, nil
	}
	return ResultUnknown, nil
}

// Verify memeriksa health sebelum dan sesudah. Untuk pengujian sederhana,
// ia mengevaluasi kondisi post terhadap ambang tertentu.
func (v *Verifier) Verify(ctx context.Context, before, after Result) Result {
	if after == ResultRecovered {
		return ResultRecovered
	}
	if after == ResultNotRecovered {
		return ResultNotRecovered
	}
	if before == after {
		return ResultNotRecovered
	}
	return ResultUnknown
}
