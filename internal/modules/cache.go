package modules

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Cache manages the local module cache directory.
type Cache struct {
	Dir string // e.g. ".flow/modules"
}

// NewCache creates a cache manager for the given directory.
func NewCache(dir string) *Cache {
	return &Cache{Dir: dir}
}

// Init ensures the cache directory exists.
func (c *Cache) Init() error {
	return os.MkdirAll(c.Dir, 0o755)
}

// CachedModule represents a module stored in the local cache.
type CachedModule struct {
	Source   string `json:"source"`
	Version string `json:"version"`
	Path    string `json:"path"`
}

// List returns all modules currently in the cache.
func (c *Cache) List() ([]CachedModule, error) {
	var modules []CachedModule

	if _, err := os.Stat(c.Dir); os.IsNotExist(err) {
		return modules, nil
	}

	// Walk: namespace/name/system/version
	namespaces, _ := os.ReadDir(c.Dir)
	for _, ns := range namespaces {
		if !ns.IsDir() {
			continue
		}
		names, _ := os.ReadDir(filepath.Join(c.Dir, ns.Name()))
		for _, name := range names {
			if !name.IsDir() {
				continue
			}
			systems, _ := os.ReadDir(filepath.Join(c.Dir, ns.Name(), name.Name()))
			for _, sys := range systems {
				if !sys.IsDir() {
					continue
				}
				versions, _ := os.ReadDir(filepath.Join(c.Dir, ns.Name(), name.Name(), sys.Name()))
				for _, ver := range versions {
					if !ver.IsDir() {
						continue
					}
					modules = append(modules, CachedModule{
						Source:  fmt.Sprintf("%s/%s/%s", ns.Name(), name.Name(), sys.Name()),
						Version: ver.Name(),
						Path:    filepath.Join(c.Dir, ns.Name(), name.Name(), sys.Name(), ver.Name()),
					})
				}
			}
		}
	}

	sort.Slice(modules, func(i, j int) bool {
		return modules[i].Source < modules[j].Source
	})

	return modules, nil
}

// Clean removes all cached modules.
func (c *Cache) Clean() error {
	if _, err := os.Stat(c.Dir); os.IsNotExist(err) {
		return nil
	}
	return os.RemoveAll(c.Dir)
}

// Has checks if a specific module version is already cached.
func (c *Cache) Has(coords *RegistryCoords, version string) bool {
	if coords == nil || version == "" {
		return false
	}
	path := filepath.Join(c.Dir, coords.Namespace, coords.Name, coords.System, version)
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// ModulePath returns the local path where a module version would be cached.
func (c *Cache) ModulePath(coords *RegistryCoords, version string) string {
	return filepath.Join(c.Dir, coords.Namespace, coords.Name, coords.System, version)
}

// UserMappings allows users to override or extend module mappings.
type UserMappings struct {
	Modules map[string]UserModuleEntry `json:"modules"`
}

// UserModuleEntry is a user-defined module mapping.
type UserModuleEntry struct {
	AWS   string `json:"aws,omitempty"`
	Azure string `json:"azure,omitempty"`
	GCP   string `json:"gcp,omitempty"`
}

// LoadUserMappings loads user-defined module mappings from .flow/modules.json.
func LoadUserMappings(path string) (*UserMappings, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &UserMappings{Modules: map[string]UserModuleEntry{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}

	var m UserMappings
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", path, err)
	}
	if m.Modules == nil {
		m.Modules = map[string]UserModuleEntry{}
	}
	return &m, nil
}

// ResolveWithUser looks up a module name using user mappings first, then defaults.
func ResolveWithUser(moduleName, provider string, userMappings *UserMappings) *RegistryCoords {
	moduleName = strings.TrimSpace(strings.ToLower(moduleName))
	provider = strings.TrimSpace(strings.ToLower(provider))

	// Check user mappings first
	if userMappings != nil {
		if entry, ok := userMappings.Modules[moduleName]; ok {
			var source string
			switch provider {
			case "aws":
				source = entry.AWS
			case "azure":
				source = entry.Azure
			case "gcp":
				source = entry.GCP
			}
			if source != "" {
				coords := parseSource(source)
				if coords != nil {
					return coords
				}
			}
		}
	}

	// Fall back to defaults
	return Resolve(moduleName, provider)
}

// parseSource parses "namespace/name/system" into RegistryCoords.
func parseSource(source string) *RegistryCoords {
	parts := strings.Split(source, "/")
	if len(parts) != 3 {
		return nil
	}
	return &RegistryCoords{
		Namespace: parts[0],
		Name:      parts[1],
		System:    parts[2],
	}
}
