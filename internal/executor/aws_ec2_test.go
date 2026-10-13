package executor_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/seronz/fault-pilot/internal/executor"
)

func TestEC2_RebootAllowedInstance(t *testing.T) {
	api := &fakeEC2{}
	journal := &fakeJournal{}
	e := executor.NewEC2Executor(api, []string{"i-123"},
		executor.WithEC2Journal(journal),
	)

	_, err := e.Execute(context.Background(), executor.Action{
		IncidentID: "inc-1", Type: "reboot_instance", Target: "i-123",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(api.ids, []string{"i-123"}) {
		t.Fatalf("rebooted ids = %v", api.ids)
	}
	if journal.started != 1 || journal.succeeded != 1 {
		t.Fatalf("journal not recorded: %+v", journal)
	}
}

func TestEC2_DeniedInstance(t *testing.T) {
	api := &fakeEC2{}
	e := executor.NewEC2Executor(api, []string{"i-123"})

	_, err := e.Execute(context.Background(), executor.Action{
		IncidentID: "inc-1", Type: "reboot_instance", Target: "i-999",
	})
	if !errors.Is(err, executor.ErrServiceNotAllowed) {
		t.Fatalf("expected ErrServiceNotAllowed, got %v", err)
	}
	if len(api.ids) != 0 {
		t.Fatalf("API must not be called for denied instance")
	}
}

func TestEC2_NoAPI(t *testing.T) {
	e := executor.NewEC2Executor(nil, []string{"i-123"})
	_, err := e.Execute(context.Background(), executor.Action{
		IncidentID: "inc-1", Type: "reboot_instance", Target: "i-123",
	})
	if !errors.Is(err, executor.ErrNoAPI) {
		t.Fatalf("expected ErrNoAPI, got %v", err)
	}
}
