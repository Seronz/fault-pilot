package executor_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/seronz/fault-pilot/internal/executor"
)

func TestKubectl_ExecuteWithNamespace(t *testing.T) {
	runner := &fakeRunner{out: []byte("deployment restarted")}
	e := executor.NewKubectlExecutor([]string{"checkout"}, executor.WithKubectlRunner(runner))

	_, err := e.Execute(context.Background(), executor.Action{
		IncidentID: "inc-1",
		Type:       "restart_deployment",
		Target:     "checkout",
		Params:     map[string]string{"namespace": "prod"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"kubectl", "rollout", "restart", "deployment/checkout", "-n", "prod"}
	if !reflect.DeepEqual(runner.lastCall(), want) {
		t.Fatalf("command = %v, want %v", runner.lastCall(), want)
	}
}

func TestKubectl_DryRun(t *testing.T) {
	runner := &fakeRunner{}
	e := executor.NewKubectlExecutor([]string{"checkout"}, executor.WithKubectlRunner(runner))

	res, err := e.Execute(context.Background(), executor.Action{
		IncidentID: "inc-1",
		Type:       "restart_deployment",
		Target:     "checkout",
		Params:     map[string]string{"dry_run": "true"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"kubectl", "rollout", "restart", "deployment/checkout", "-n", "default", "--dry-run=server"}
	if !reflect.DeepEqual(runner.lastCall(), want) {
		t.Fatalf("command = %v, want %v", runner.lastCall(), want)
	}
	if !res.DryRun {
		t.Fatalf("expected DryRun result")
	}
}
