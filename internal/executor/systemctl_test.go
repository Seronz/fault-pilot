package executor_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/seronz/fault-pilot/internal/executor"
)

func TestSystemctl_Execute(t *testing.T) {
	runner := &fakeRunner{out: []byte("ok")}
	journal := &fakeJournal{}
	e := executor.NewSystemctlExecutor(
		[]string{"checkout"},
		executor.WithSystemctlRunner(runner),
		executor.WithSystemctlJournal(journal),
	)

	res, err := e.Execute(context.Background(), executor.Action{
		IncidentID: "inc-1", Type: "restart_service", Target: "checkout",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"systemctl", "restart", "checkout"}
	if !reflect.DeepEqual(runner.lastCall(), want) {
		t.Fatalf("command = %v, want %v", runner.lastCall(), want)
	}
	if res.Executor != "systemctl" {
		t.Fatalf("executor = %q", res.Executor)
	}
	if journal.started != 1 || journal.succeeded != 1 {
		t.Fatalf("journal before/after not recorded: %+v", journal)
	}
}

func TestSystemctl_TargetNotAllowed(t *testing.T) {
	runner := &fakeRunner{}
	e := executor.NewSystemctlExecutor([]string{"checkout"}, executor.WithSystemctlRunner(runner))

	_, err := e.Execute(context.Background(), executor.Action{
		IncidentID: "inc-1", Type: "restart_service", Target: "db",
	})
	if !errors.Is(err, executor.ErrServiceNotAllowed) {
		t.Fatalf("expected ErrServiceNotAllowed, got %v", err)
	}
	if runner.lastCall() != nil {
		t.Fatalf("runner must not be called when target not allowed")
	}
}

func TestSystemctl_PolicyDeny(t *testing.T) {
	runner := &fakeRunner{}
	policy := &fakePolicy{allow: false}
	e := executor.NewSystemctlExecutor([]string{"checkout"},
		executor.WithSystemctlRunner(runner),
		executor.WithSystemctlPolicy(policy),
	)

	_, err := e.Execute(context.Background(), executor.Action{
		IncidentID: "inc-1", Type: "restart_service", Target: "checkout",
	})
	if !errors.Is(err, executor.ErrDenied) {
		t.Fatalf("expected ErrDenied, got %v", err)
	}
	if runner.lastCall() != nil {
		t.Fatalf("runner must not run when policy denies")
	}
}
