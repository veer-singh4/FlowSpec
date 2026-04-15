package engine

import (
	"fmt"

	"flowspec/internal/parser"
)

// CloudConfig describes the target cloud provider and region.
type CloudConfig struct {
	Provider string `json:"provider"`
	Region   string `json:"region"`
}

// ModuleConfig describes a single module usage within an app.
type ModuleConfig struct {
	Module string            `json:"module"`
	Alias  string            `json:"alias"`
	Config map[string]string `json:"config"`
	Line   int               `json:"line"`
}

// ResourceConfig describes a single cloud resource within an app.
type ResourceConfig struct {
	Type   string            `json:"type"`
	Alias  string            `json:"alias"`
	Config map[string]string `json:"config"`
	Line   int               `json:"line"`
}

// AppBlock describes a single app declaration in a .ufs file.
type AppBlock struct {
	Name      string           `json:"name"`
	Cloud     *CloudConfig     `json:"cloud"`
	Database  string           `json:"database"`
	Modules   []ModuleConfig   `json:"modules"`
	Resources []ResourceConfig `json:"resources"`
	Line      int              `json:"line"`
}

// FlowSpec is the top-level parsed representation of a .ufs file.
type FlowSpec struct {
	Apps []AppBlock `json:"apps"`
}

// ParseDSL reads a .ufs file and returns a FlowSpec.
// Uses the native Go parser — no external Python dependency.
func ParseDSL(filePath string) (*FlowSpec, error) {
	parsed, err := parser.ParseFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}
	return convertSpec(parsed), nil
}

// convertSpec converts the parser AST into engine FlowSpec types.
func convertSpec(ps *parser.Spec) *FlowSpec {
	if ps == nil {
		return &FlowSpec{Apps: []AppBlock{}}
	}

	spec := &FlowSpec{Apps: make([]AppBlock, 0, len(ps.Apps))}
	for _, pa := range ps.Apps {
		app := AppBlock{
			Name:      pa.Name,
			Modules:   make([]ModuleConfig, 0, len(pa.Modules)),
			Resources: make([]ResourceConfig, 0, len(pa.Resources)),
			Line:      pa.Line,
		}

		if pa.Cloud != nil {
			app.Cloud = &CloudConfig{
				Provider: pa.Cloud.Provider,
				Region:   pa.Cloud.Region,
			}
		}

		for _, pm := range pa.Modules {
			app.Modules = append(app.Modules, ModuleConfig{
				Module: pm.Module,
				Alias:  pm.Alias,
				Config: pm.Config,
				Line:   pm.Line,
			})
		}

		for _, pr := range pa.Resources {
			app.Resources = append(app.Resources, ResourceConfig{
				Type:   pr.Type,
				Alias:  pr.Alias,
				Config: pr.Config,
				Line:   pr.Line,
			})
		}

		spec.Apps = append(spec.Apps, app)
	}

	return spec
}

// BuildSummary returns a human-readable summary of the spec for CLI output.
func BuildSummary(spec *FlowSpec) []string {
	if spec == nil {
		return []string{"No spec loaded"}
	}

	lines := []string{fmt.Sprintf("apps: %d", len(spec.Apps))}
	for _, app := range spec.Apps {
		provider := "none"
		region := "none"
		if app.Cloud != nil {
			provider = app.Cloud.Provider
			region = app.Cloud.Region
		}

		lines = append(lines,
			fmt.Sprintf("app=%s cloud=%s/%s modules=%d resources=%d",
				app.Name, provider, region, len(app.Modules), len(app.Resources)),
		)

		for _, m := range app.Modules {
			lines = append(lines, fmt.Sprintf("  use %s as %s", m.Module, m.Alias))
		}
		for _, r := range app.Resources {
			lines = append(lines, fmt.Sprintf("  resource %s as %s", r.Type, r.Alias))
		}
	}

	return lines
}
