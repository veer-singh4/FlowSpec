package pulumi

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/veer-singh4/FlowSpec/internal/adapter"
	terraformadapter "github.com/veer-singh4/FlowSpec/internal/adapter/terraform"
	"github.com/veer-singh4/FlowSpec/internal/engine"
)

var _ adapter.IaCAdapter = (*Adapter)(nil)

// Adapter provides a Pulumi-compatible backend.
// For now it reuses the Terraform execution engine so the same .ufs spec works
// without backend-specific hardcoding in user code.
type Adapter struct {
	WorkDir     string
	tfCompatDir string
	tf          *terraformadapter.Adapter
}

// New creates a Pulumi adapter in compatibility mode.
func New(workDir string) *Adapter {
	tfCompatDir := filepath.Join(workDir, "terraform")
	return &Adapter{
		WorkDir:     workDir,
		tfCompatDir: tfCompatDir,
		tf:          terraformadapter.New(tfCompatDir),
	}
}

// Name returns the adapter name.
func (a *Adapter) Name() string {
	return "pulumi"
}

// Init initializes Pulumi compatibility workspace and delegates to Terraform engine.
func (a *Adapter) Init(config *engine.FlowSpec) error {
	if err := a.ensureDirs(); err != nil {
		return err
	}
	fmt.Println("ℹ Pulumi compatibility mode: executing via Terraform engine")
	return a.tf.Init(config)
}

// Plan previews changes using Pulumi compatibility mode.
func (a *Adapter) Plan(config *engine.FlowSpec) error {
	if err := a.ensureDirs(); err != nil {
		return err
	}
	fmt.Println("ℹ Pulumi compatibility mode: executing via Terraform engine")
	return a.tf.Plan(config)
}

// Apply executes changes using Pulumi compatibility mode.
func (a *Adapter) Apply(config *engine.FlowSpec) error {
	if err := a.ensureDirs(); err != nil {
		return err
	}
	fmt.Println("ℹ Pulumi compatibility mode: executing via Terraform engine")
	return a.tf.Apply(config)
}

// Destroy tears down resources using Pulumi compatibility mode.
func (a *Adapter) Destroy(config *engine.FlowSpec) error {
	if err := a.ensureDirs(); err != nil {
		return err
	}
	fmt.Println("ℹ Pulumi compatibility mode: executing via Terraform engine")
	return a.tf.Destroy(config)
}

func (a *Adapter) ensureDirs() error {
	if err := os.MkdirAll(a.WorkDir, 0o755); err != nil {
		return fmt.Errorf("failed to create pulumi workdir: %w", err)
	}
	if err := os.MkdirAll(a.tfCompatDir, 0o755); err != nil {
		return fmt.Errorf("failed to create pulumi terraform-compat workdir: %w", err)
	}
	return nil
}
