package executor

import "fmt"

// BuildConfig merangkum konfigurasi untuk membangun Executor dari config.yaml.
type BuildConfig struct {
	Type            string   // "lab" | "systemctl" | "kubectl" | "aws_ec2" | "ansible"
	AllowedTargets  []string // allowlist service/target
	AllowedRunbooks []string // allowlist runbook (ansible)
	FIURL           string   // endpoint /control untuk lab
	Policy          Policy
	Journal         Journal
	EC2             EC2API
}

// Build membuat Executor sesuai Type (fail closed bila tidak dikenal).
func Build(cfg BuildConfig) (Executor, error) {
	switch cfg.Type {
	case "", "lab":
		opts := []LabOption{WithLabPolicy(cfg.Policy), WithLabJournal(cfg.Journal)}
		if cfg.FIURL != "" {
			opts = append(opts, WithFaultInjectorURL(cfg.FIURL))
		}
		return New(opts...), nil
	case "systemctl":
		return NewSystemctlExecutor(cfg.AllowedTargets,
			WithSystemctlPolicy(cfg.Policy), WithSystemctlJournal(cfg.Journal)), nil
	case "kubectl":
		return NewKubectlExecutor(cfg.AllowedTargets,
			WithKubectlPolicy(cfg.Policy), WithKubectlJournal(cfg.Journal)), nil
	case "aws_ec2":
		return NewEC2Executor(cfg.EC2, cfg.AllowedTargets,
			WithEC2Policy(cfg.Policy), WithEC2Journal(cfg.Journal)), nil
	case "ansible":
		return NewAnsibleExecutor(cfg.AllowedRunbooks,
			WithAnsiblePolicy(cfg.Policy), WithAnsibleJournal(cfg.Journal)), nil
	default:
		return nil, fmt.Errorf("executor: unknown type %q", cfg.Type)
	}
}

// RiskForType mengembalikan level risiko untuk sebuah tipe executor. Tipe yang
// tidak dikenal dianggap "high" (fail closed).
func RiskForType(typ string) string {
	switch typ {
	case "", "lab":
		return "low"
	case "systemctl", "kubectl":
		return "medium"
	case "aws_ec2", "ansible":
		return "high"
	default:
		return "high"
	}
}
