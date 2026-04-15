package pulumi

import (
	"fmt"

	"github.com/veer-singh4/FlowSpec/internal/adapter"
	"github.com/veer-singh4/FlowSpec/internal/engine"
)

var _ adapter.IaCAdapter = (*Adapter)(nil)

// Adapter is a placeholder Pulumi backend adapter.
// In a future phase this will generate Pulumi YAML or TypeScript from FlowSpec.
type Adapter struct {
	WorkDir string
}

// New creates a Pulumi adapter stub.
func New(workDir string) *Adapter {
	return &Adapter{WorkDir: workDir}
}

// Name returns the adapter name.
func (a *Adapter) Name() string {
	return "pulumi"
}

// Init is not yet implemented for Pulumi.
func (a *Adapter) Init(_ *engine.FlowSpec) error {
	return fmt.Errorf("pulumi backend is not yet implemented — coming soon")
}

// Plan is not yet implemented for Pulumi.
func (a *Adapter) Plan(_ *engine.FlowSpec) error {
	return fmt.Errorf("pulumi backend is not yet implemented — coming soon")
}

// Apply is not yet implemented for Pulumi.
func (a *Adapter) Apply(_ *engine.FlowSpec) error {
	return fmt.Errorf("pulumi backend is not yet implemented — coming soon")
}

// Destroy is not yet implemented for Pulumi.
func (a *Adapter) Destroy(_ *engine.FlowSpec) error {
	return fmt.Errorf("pulumi backend is not yet implemented — coming soon")
}
