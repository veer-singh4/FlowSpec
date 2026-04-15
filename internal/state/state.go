package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"flowspec/internal/engine"
)

type ResourceRecord struct {
	ID       string `json:"id"`
	App      string `json:"app"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Region   string `json:"region"`
}

type State struct {
	Resources []ResourceRecord `json:"resources"`
}

func Load(path string) (*State, error) {
	bytes, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &State{Resources: []ResourceRecord{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read state: %w", err)
	}

	if strings.TrimSpace(string(bytes)) == "" {
		return &State{Resources: []ResourceRecord{}}, nil
	}

	var st State
	if err := json.Unmarshal(bytes, &st); err != nil {
		return nil, fmt.Errorf("failed to decode state: %w", err)
	}
	if st.Resources == nil {
		st.Resources = []ResourceRecord{}
	}
	return &st, nil
}

func Save(path string, st *State) error {
	if st == nil {
		st = &State{Resources: []ResourceRecord{}}
	}
	if st.Resources == nil {
		st.Resources = []ResourceRecord{}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create state dir: %w", err)
	}

	payload, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode state: %w", err)
	}
	payload = append(payload, '\n')

	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return fmt.Errorf("failed to write state: %w", err)
	}
	return nil
}

func DesiredFromSpec(spec *engine.FlowSpec) []ResourceRecord {
	if spec == nil {
		return []ResourceRecord{}
	}

	out := []ResourceRecord{}
	seen := map[string]bool{}

	for _, app := range spec.Apps {
		provider, region := cloudOf(app)

		for _, res := range app.Resources {
			id := resourceID(app.Name, res.Type, res.Alias)
			if seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, ResourceRecord{
				ID:       id,
				App:      app.Name,
				Type:     res.Type,
				Name:     res.Alias,
				Provider: provider,
				Region:   region,
			})
		}

		for _, mod := range app.Modules {
			id := moduleID(app.Name, mod.Alias)
			if seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, ResourceRecord{
				ID:       id,
				App:      app.Name,
				Type:     "module",
				Name:     mod.Alias,
				Provider: provider,
				Region:   region,
			})
		}
	}

	return out
}

func FilterSpecForCreate(spec *engine.FlowSpec, st *State) (*engine.FlowSpec, []ResourceRecord) {
	if spec == nil {
		return &engine.FlowSpec{Apps: []engine.AppBlock{}}, []ResourceRecord{}
	}
	if st == nil {
		st = &State{}
	}

	newSpec := &engine.FlowSpec{Apps: []engine.AppBlock{}}
	newResources := []ResourceRecord{}
	seen := map[string]bool{}

	for _, app := range spec.Apps {
		provider, region := cloudOf(app)
		newApp := engine.AppBlock{
			Name:      app.Name,
			Cloud:     app.Cloud,
			Modules:   []engine.ModuleConfig{},
			Resources: []engine.ResourceConfig{},
			Line:      app.Line,
		}

		for _, res := range app.Resources {
			id := resourceID(app.Name, res.Type, res.Alias)
			if st.hasResource(id) || seen[id] {
				continue
			}
			seen[id] = true
			newApp.Resources = append(newApp.Resources, res)
			newResources = append(newResources, ResourceRecord{
				ID:       id,
				App:      app.Name,
				Type:     res.Type,
				Name:     res.Alias,
				Provider: provider,
				Region:   region,
			})
		}

		for _, mod := range app.Modules {
			id := moduleID(app.Name, mod.Alias)
			if st.hasResource(id) || seen[id] {
				continue
			}
			seen[id] = true
			newApp.Modules = append(newApp.Modules, mod)
			newResources = append(newResources, ResourceRecord{
				ID:       id,
				App:      app.Name,
				Type:     "module",
				Name:     mod.Alias,
				Provider: provider,
				Region:   region,
			})
		}

		if len(newApp.Modules) > 0 || len(newApp.Resources) > 0 {
			newSpec.Apps = append(newSpec.Apps, newApp)
		}
	}

	return newSpec, newResources
}

func (st *State) Merge(newResources []ResourceRecord) {
	if st.Resources == nil {
		st.Resources = []ResourceRecord{}
	}
	for _, rec := range newResources {
		if !st.hasResource(rec.ID) {
			st.Resources = append(st.Resources, rec)
		}
	}
}

func (st *State) hasResource(id string) bool {
	for _, rec := range st.Resources {
		if rec.ID == id {
			return true
		}
	}
	return false
}

func cloudOf(app engine.AppBlock) (provider, region string) {
	if app.Cloud == nil {
		return "", ""
	}
	return app.Cloud.Provider, app.Cloud.Region
}

func moduleID(app, alias string) string {
	return fmt.Sprintf("%s:module:%s", app, alias)
}

func resourceID(app, resType, alias string) string {
	return fmt.Sprintf("%s:resource:%s:%s", app, resType, alias)
}
