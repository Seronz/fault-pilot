package executor_test

import (
	"testing"

	"github.com/seronz/fault-pilot/internal/executor"
)

func TestBuild_AllTypes(t *testing.T) {
	cases := []struct{ typ, want string }{
		{"", "lab"},
		{"lab", "lab"},
		{"systemctl", "systemctl"},
		{"kubectl", "kubectl"},
		{"aws_ec2", "aws_ec2"},
		{"ansible", "ansible"},
	}
	for _, c := range cases {
		e, err := executor.Build(executor.BuildConfig{Type: c.typ})
		if err != nil {
			t.Fatalf("Build(%q) error: %v", c.typ, err)
		}
		if e.Type() != c.want {
			t.Fatalf("Build(%q).Type() = %q, want %q", c.typ, e.Type(), c.want)
		}
	}
}

func TestBuild_UnknownType(t *testing.T) {
	if _, err := executor.Build(executor.BuildConfig{Type: "bogus"}); err == nil {
		t.Fatalf("expected error for unknown type")
	}
}
