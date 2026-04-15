package adapter

import "github.com/veer-singh4/FlowSpec/internal/engine"

// IaCAdapter defines the contract for infrastructure backend adapters.
// Each adapter translates FlowSpec into a specific IaC backend (Terraform, Pulumi, etc.).
type IaCAdapter interface {
	// Init performs backend-specific initialization.
	Init(config *engine.FlowSpec) error

	// Plan generates and shows a preview of changes.
	Plan(config *engine.FlowSpec) error

	// Apply executes the planned changes.
	Apply(config *engine.FlowSpec) error

	// Destroy tears down all managed resources.
	Destroy(config *engine.FlowSpec) error

	// Name returns the adapter backend name (e.g. "terraform", "pulumi").
	Name() string
}
