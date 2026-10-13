package policy_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/seronz/fault-pilot/internal/domain"
	"github.com/seronz/fault-pilot/internal/gemini"
	"github.com/seronz/fault-pilot/internal/policy"
	"github.com/seronz/fault-pilot/internal/store"
)

// pastikan store.Store memenuhi policy.Store
var _ policy.Store = (*store.Store)(nil)

type fakeStore struct {
	incidents map[string]domain.Incident
	events    map[string]struct{}
	fail      bool
}

func (f *fakeStore) Incident(ctx context.Context, id string) (domain.Incident, bool, error) {
	if f.fail {
		return domain.Incident{}, false, errors.New("boom")
	}
	inc, ok := f.incidents[id]
	return inc, ok, nil
}

func (f *fakeStore) EventExists(ctx context.Context, id string) (bool, error) {
	if f.fail {
		return false, errors.New("boom")
	}
	_, ok := f.events[id]
	return ok, nil
}

func testStore() *fakeStore {
	return &fakeStore{
		incidents: map[string]domain.Incident{
			"inc-1": {ID: "inc-1", Status: policy.ActiveStatus, EvidenceIDs: []string{"ev-1"}},
		},
		events: map[string]struct{}{"ev-1": {}},
	}
}

func validProposal() domain.Proposal {
	return domain.Proposal{
		IncidentID:  "inc-1",
		EvidenceIDs: []string{"ev-1"},
		Action:      policy.ActionRestartSimulatedService,
		Target:      policy.TargetCheckout,
		Reason:      "test",
	}
}

// T02: proposal valid lolos.
func TestT02_ValidProposalPasses(t *testing.T) {
	eng := policy.New(testStore())
	ok, reason := eng.Validate(context.Background(), validProposal())
	if !ok {
		t.Fatalf("proposal valid harus lolos, ditolak: %s", reason)
	}
}

// T03: JSON tidak valid ditolak.
func TestT03_InvalidJSONRejected(t *testing.T) {
	eng := policy.New(testStore())
	ok, reason := eng.ValidateJSON(context.Background(), []byte(`{"incident_id":`))
	if ok {
		t.Fatal("JSON tidak valid harus ditolak")
	}
	if reason == "" {
		t.Fatal("harus ada alasan penolakan")
	}
}

// T04: target di luar allowlist ditolak.
func TestT04_TargetOutsideAllowlistRejected(t *testing.T) {
	eng := policy.New(testStore())
	p := validProposal()
	p.Target = "payments"
	if ok, _ := eng.Validate(context.Background(), p); ok {
		t.Fatal("target di luar allowlist harus ditolak")
	}
}

// T09: dua worker pada insiden sama tidak boleh dua-duanya lolos.
func TestT09_TwoWorkersSameIncidentNoDouble(t *testing.T) {
	eng := policy.New(testStore())
	p := validProposal()

	var accepted int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if ok, _ := eng.Validate(context.Background(), p); ok {
				atomic.AddInt32(&accepted, 1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if accepted != 1 {
		t.Fatalf("harus tepat 1 yang lolos, dapat %d", accepted)
	}
}

func TestRejectMissingEvidence(t *testing.T) {
	eng := policy.New(testStore())
	p := validProposal()
	p.EvidenceIDs = nil
	if ok, _ := eng.Validate(context.Background(), p); ok {
		t.Fatal("bukti hilang harus ditolak")
	}
}

func TestRejectForeignEvidence(t *testing.T) {
	eng := policy.New(testStore())
	p := validProposal()
	p.EvidenceIDs = []string{"ev-999"}
	if ok, _ := eng.Validate(context.Background(), p); ok {
		t.Fatal("bukti bukan milik insiden harus ditolak")
	}
}

func TestRejectInactiveIncident(t *testing.T) {
	st := testStore()
	st.incidents["inc-1"] = domain.Incident{ID: "inc-1", Status: "resolved", EvidenceIDs: []string{"ev-1"}}
	eng := policy.New(st)
	if ok, _ := eng.Validate(context.Background(), validProposal()); ok {
		t.Fatal("insiden tidak aktif harus ditolak")
	}
}

func TestRejectUnknownAction(t *testing.T) {
	eng := policy.New(testStore())
	p := validProposal()
	p.Action = "delete_everything"
	if ok, _ := eng.Validate(context.Background(), p); ok {
		t.Fatal("aksi di luar allowlist harus ditolak")
	}
}

func TestFailClosedOnStoreError(t *testing.T) {
	eng := policy.New(&fakeStore{fail: true})
	if ok, _ := eng.Validate(context.Background(), validProposal()); ok {
		t.Fatal("error store harus fail closed (tolak)")
	}
}

func TestCooldownAfterDone(t *testing.T) {
	now := time.Now()
	eng := policy.NewWithConfig(testStore(), policy.Config{
		Cooldown:              time.Minute,
		MaxActionsPerIncident: 5,
		Now:                   func() time.Time { return now },
	})
	p := validProposal()

	if ok, reason := eng.Validate(context.Background(), p); !ok {
		t.Fatalf("aksi pertama harus lolos: %s", reason)
	}
	eng.Done("inc-1")

	if ok, _ := eng.Validate(context.Background(), p); ok {
		t.Fatal("dalam cooldown harus ditolak")
	}
}

func TestActionLimit(t *testing.T) {
	now := time.Now()
	eng := policy.NewWithConfig(testStore(), policy.Config{
		Cooldown:              0,
		MaxActionsPerIncident: 1,
		Now:                   func() time.Time { return now },
	})
	p := validProposal()

	if ok, _ := eng.Validate(context.Background(), p); !ok {
		t.Fatal("aksi pertama harus lolos")
	}
	eng.Done("inc-1")

	if ok, _ := eng.Validate(context.Background(), p); ok {
		t.Fatal("melewati limit harus ditolak")
	}
}

// Integrasi: policy.Store terpenuhi oleh store SQLite asli.
func TestIntegrationWithRealStore(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "policy.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	if err := st.AppendEvent(ctx, domain.Event{ID: "ev-1", Timestamp: time.Now()}); err != nil {
		t.Fatalf("append event: %v", err)
	}
	if err := st.CreateIncident(ctx, domain.Incident{ID: "inc-1", Status: policy.ActiveStatus, EvidenceIDs: []string{"ev-1"}}); err != nil {
		t.Fatalf("create incident: %v", err)
	}

	eng := policy.New(st)
	if ok, reason := eng.Validate(ctx, validProposal()); !ok {
		t.Fatalf("proposal valid harus lolos: %s", reason)
	}
}

// Integrasi: proposal dari FakeAdapter lolos policy.
func TestFakeAdapterProposalPassesPolicy(t *testing.T) {
	eng := policy.New(testStore())
	inc := domain.Incident{ID: "inc-1", Status: policy.ActiveStatus, EvidenceIDs: []string{"ev-1"}}

	p, err := gemini.FakeAdapter{}.Propose(context.Background(), inc)
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	if ok, reason := eng.Validate(context.Background(), p); !ok {
		t.Fatalf("proposal FakeAdapter harus lolos: %s", reason)
	}
}
