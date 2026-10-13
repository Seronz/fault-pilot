package executor

import (
	"context"
	"os/exec"
)

// CommandRunner menjalankan program eksternal tanpa shell.
//
// Argumen selalu berupa slice sehingga tidak ada interpolasi shell; ini
// mencegah command injection (`;`, `|`, `$(...)` diperlakukan sebagai argumen
// literal). Implementasi nyata memakai exec.CommandContext.
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// execRunner implementasi CommandRunner memakai exec.CommandContext.
type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}
