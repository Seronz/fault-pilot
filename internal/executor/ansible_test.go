package executor_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/seronz/fault-pilot/internal/executor"
)

func TestAnsible_ExecuteWithExtraVars(t *testing.T) {
	runner := &fakeRunner{out: []byte("PLAY RECAP")}
	e := executor.NewAnsibleExecutor([]string{"runbooks/restart.yml"},
		executor.WithAnsibleRunner(runner),
	)

	_, err := e.Execute(context.Background(), executor.Action{
		IncidentID: "inc-1",
		Type:       "run_playbook",
		Target:     "runbooks/restart.yml",
		Params:     map[string]string{"extra_vars": "svc=checkout"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"ansible-playbook", "runbooks/restart.yml", "-l", "runbooks/restart.yml", "--extra-vars", "svc=checkout"}
	if !reflect.DeepEqual(runner.lastCall(), want) {
		t.Fatalf("command = %v, want %v", runner.lastCall(), want)
	}
}

func TestAnsible_RunbookNotAllowed(t *testing.T) {
	runner := &fakeRunner{}
	e := executor.NewAnsibleExecutor([]string{"runbooks/restart.yml"}, executor.WithAnsibleRunner(runner))

	_, err := e.Execute(context.Background(), executor.Action{
		IncidentID: "inc-1", Type: "run_playbook", Target: "runbooks/evil.yml",
	})
	if !errors.Is(err, executor.ErrRunbookNotAllowed) {
		t.Fatalf("expected ErrRunbookNotAllowed, got %v", err)
	}
	if runner.lastCall() != nil {
		t.Fatalf("runner must not run for disallowed runbook")
	}
}

// Target dengan karakter shell diperlakukan literal (tidak lolos allowlist).
func TestAnsible_NoShellInjection(t *testing.T) {
	runner := &fakeRunner{}
	e := executor.NewAnsibleExecutor([]string{"runbooks/restart.yml"}, executor.WithAnsibleRunner(runner))

	_, err := e.Execute(context.Background(), executor.Action{
		IncidentID: "inc-1", Type: "run_playbook", Target: "runbooks/restart.yml; rm -rf /",
	})
	if !errors.Is(err, executor.ErrRunbookNotAllowed) {
		t.Fatalf("expected ErrRunbookNotAllowed, got %v", err)
	}
	if runner.lastCall() != nil {
		t.Fatalf("runner must not run for injected target")
	}
}
