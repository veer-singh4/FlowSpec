package engine

import (
	"fmt"

	"github.com/veer-singh4/FlowSpec/internal/parser"
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
	Params    map[string]string  `json:"params"`
	Line      int               `json:"line"`
}

// FlowSpec is the top-level parsed representation of a .ufs file.
type FlowSpec struct {
	Apps   []AppBlock        `json:"apps"`
	Params map[string]string `json:"params"`
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
	if ps == nil {
		return &FlowSpec{Apps: []AppBlock{}, Params: map[string]string{}}
	}
 
	spec := &FlowSpec{
		Apps:   make([]AppBlock, 0, len(ps.Apps)),
		Params: ps.Params,
	}
	for _, pa := range ps.Apps {
		app := AppBlock{
			Name:      pa.Name,
			Modules:   make([]ModuleConfig, 0, len(pa.Modules)),
			Resources: make([]ResourceConfig, 0, len(pa.Resources)),
			Params:    pa.Params,
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

	// Resolve variables after everything is loaded
	ResolveVariables(spec)

	return spec
}

// ResolveVariables interpolates ${param.name} in all configs.
func ResolveVariables(spec *FlowSpec) {
	for i := range spec.Apps {
		app := &spec.Apps[i]

		// Combine global params and app-local params
		allParams := make(map[string]string)
		for k, v := range spec.Params {
			allParams[k] = v
		}
		for k, v := range app.Params {
			allParams[k] = v
		}

		// Interpolate in cloud config
		if app.Cloud != nil {
			app.Cloud.Provider = interpolate(app.Cloud.Provider, allParams)
			app.Cloud.Region = interpolate(app.Cloud.Region, allParams)
		}

		// Interpolate in modules
		for j := range app.Modules {
			m := &app.Modules[j]
			for k, v := range m.Config {
				m.Config[k] = interpolate(v, allParams)
			}
		}

		// Interpolate in resources
		for j := range app.Resources {
			r := &app.Resources[j]
			for k, v := range r.Config {
				r.Config[k] = interpolate(v, allParams)
			}
		}
	}
}

func interpolate(val string, params map[string]string) string {
	// Simple implementation: find ${param.NAME} and replace with params[NAME]
	// Optimized for MVP.
	for k, v := range params {
		placeholder := fmt.Sprintf("${param.%s}", k)
		val = strings.ReplaceAll(val, placeholder, v)
	}
	return val
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
