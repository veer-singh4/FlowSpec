package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	githubactionsadapter "github.com/veer-singh4/FlowSpec/internal/adapter/githubactions"
	terraformadapter "github.com/veer-singh4/FlowSpec/internal/adapter/terraform"
	pulumiadapter "github.com/veer-singh4/FlowSpec/internal/adapter/pulumi"
	"github.com/veer-singh4/FlowSpec/internal/adapter"
	"github.com/veer-singh4/FlowSpec/internal/engine"
	"github.com/veer-singh4/FlowSpec/internal/modules"
	flowstate "github.com/veer-singh4/FlowSpec/internal/state"
)

const (
	flowDir      = ".flow"
	terraformDir = ".flow/terraform"
	pulumiDir    = ".flow/pulumi"
	githubDir    = ".flow/github-actions"
	stateFile    = ".flow/state.json"
	modulesDir   = ".flow/modules"
	modulesJSON  = ".flow/modules.json"
	ufsTrackFile = ".ufstrack"
	version      = "1.2.0"
)

// Run is the main CLI entrypoint.
func Run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return errors.New("missing command")
	}

	switch args[0] {
	case "init":
		return handleInit(args[1:])
	case "plan":
		return handlePlan(args[1:])
	case "deploy":
		return handleDeploy(args[1:])
	case "destroy":
		return handleDestroy(args[1:])
	case "status":
		return handleStatus(args[1:])
	case "modules":
		return handleModules(args[1:])
	case "version", "-v", "--version":
		return handleVersion()
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		printUsage()
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func printUsage() {
	fmt.Println("UniFlow CLI v" + version)
	fmt.Println()
	fmt.Println("Write infrastructure once. Deploy anywhere.")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  flow init [--backend terraform|pulumi|github-actions]   Initialize .flow workspace")
	fmt.Println("  flow plan    <file.ufs>                  Preview infrastructure changes")
	fmt.Println("  flow deploy  <file.ufs>                  Apply infrastructure changes")
	fmt.Println("  flow destroy                             Destroy all managed resources")
	fmt.Println("  flow status                              Show current state")
	fmt.Println("  flow modules list                        List cached modules")
	fmt.Println("  flow modules update <file.ufs>           Re-download modules for spec")
	fmt.Println("  flow modules clean                       Remove cached modules")
	fmt.Println("  flow modules mappings                    Show available module mappings")
	fmt.Println("  flow version                             Print CLI version")
	fmt.Println()
	fmt.Println("Supported file extensions: .ufs (recommended), .ufl, .flow, .fs")
}

func handleVersion() error {
	fmt.Printf("UniFlow CLI v%s\n", version)
	fmt.Println("Backend: Terraform (default), Pulumi (native), GitHub Actions (YAML generator)")
	fmt.Println("Parser: Native Go (no Python dependency)")
	return nil
}

// ----- init -----

func handleInit(args []string) error {
	backend := "terraform"

	// Parse --backend flag
	for i, arg := range args {
		if arg == "--backend" && i+1 < len(args) {
			backend = strings.ToLower(args[i+1])
		}
	}

	if backend == "github" || backend == "github_actions" || backend == "github-actions" {
		backend = "github-actions"
	}

	if backend != "terraform" && backend != "pulumi" && backend != "github-actions" {
		return fmt.Errorf("unsupported backend: %s (use 'terraform', 'pulumi', or 'github-actions')", backend)
	}

	if err := ensureFlowSetup(); err != nil {
		return err
	}

	// Save config
	cfg := DefaultConfig()
	cfg.Backend = backend
	if err := SaveConfig(cfg); err != nil {
		return err
	}

	// Create modules directory
	if err := os.MkdirAll(modulesDir, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", modulesDir, err)
	}

	fmt.Printf("✓ Initialized UniFlow workspace at %s\n", filepath.Clean(flowDir))
	fmt.Printf("  Backend:      %s\n", backend)
	fmt.Printf("  Module cache: %s\n", modulesDir)
	fmt.Printf("  Config:       %s\n", filepath.Join(flowDir, configFileName))
	return nil
}

// ----- plan -----

func handlePlan(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: flow plan <file.ufs>")
	}
	if err := validateSpecFile(args[0]); err != nil {
		return err
	}
	if err := ensureFlowSetup(); err != nil {
		return err
	}

	cfg, err := LoadConfig()
	if err != nil {
		return err
	}

	// Parse with native Go parser
	fmt.Printf("⟳ Parsing %s ...\n", args[0])
	
	// Auto-load .ufsparam files from current directory
	params, _ := filepath.Glob("*.ufsparam")
	
	spec, err := engine.ParseDSL(args[0])
	if err != nil {
		return err
	}

	// Load global params from .ufsparam files
	for _, pf := range params {
		fmt.Printf("  • Loading parameters from %s\n", pf)
		ps, err := engine.ParseDSL(pf)
		if err == nil {
			for k, v := range ps.Params {
				spec.Params[k] = v
			}
		}
	}

	// Resolve variables after loading all params
	engine.ResolveVariables(spec)

	// Resolve and download modules
	if err := resolveModules(spec, cfg); err != nil {
		return err
	}

	// Load state
	st, err := flowstate.Load(stateFile)
	if err != nil {
		return err
	}

	desired := flowstate.DesiredFromSpec(spec)
	filteredSpec, newResources := flowstate.FilterSpecForCreate(spec, st)

	fmt.Println()
	fmt.Printf("✓ Plan parsed from %s\n", args[0])
	for _, line := range engine.BuildSummary(spec) {
		fmt.Printf("  %s\n", line)
	}
	fmt.Printf("  desired resources:  %d\n", len(desired))
	fmt.Printf("  new resources:      %d\n", len(newResources))
	fmt.Printf("  existing (skipped): %d\n", len(desired)-len(newResources))
	fmt.Println()

	if len(newResources) == 0 {
		adpt := getAdapter(cfg)
		if err := writeUFSTrack("plan", args[0], cfg, adpt.Name(), "no_changes", spec, desired, newResources); err != nil {
			fmt.Printf("⚠ failed to update %s: %v\n", ufsTrackFile, err)
		}
		fmt.Println("✓ No new resources to create")
		return nil
	}

	adpt := getAdapter(cfg)
	if err := adpt.Plan(filteredSpec); err != nil {
		return err
	}
	if err := writeUFSTrack("plan", args[0], cfg, adpt.Name(), "success", spec, desired, newResources); err != nil {
		fmt.Printf("⚠ failed to update %s: %v\n", ufsTrackFile, err)
	}

	fmt.Println()
	fmt.Println("✓ State-aware plan finished via", adpt.Name())
	return nil
}

// ----- deploy -----

func handleDeploy(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: flow deploy <file.ufs>")
	}
	if err := validateSpecFile(args[0]); err != nil {
		return err
	}
	if err := ensureFlowSetup(); err != nil {
		return err
	}

	cfg, err := LoadConfig()
	if err != nil {
		return err
	}

	fmt.Printf("⟳ Parsing %s ...\n", args[0])
	
	// Auto-load .ufsparam files from current directory
	params, _ := filepath.Glob("*.ufsparam")
	
	spec, err := engine.ParseDSL(args[0])
	if err != nil {
		return err
	}

	for _, pf := range params {
		fmt.Printf("  • Loading parameters from %s\n", pf)
		ps, err := engine.ParseDSL(pf)
		if err == nil {
			for k, v := range ps.Params {
				spec.Params[k] = v
			}
		}
	}

	engine.ResolveVariables(spec)

	if err := resolveModules(spec, cfg); err != nil {
		return err
	}

	st, err := flowstate.Load(stateFile)
	if err != nil {
		return err
	}

	desired := flowstate.DesiredFromSpec(spec)
	filteredSpec, newResources := flowstate.FilterSpecForCreate(spec, st)

	fmt.Println()
	fmt.Printf("✓ Deploy request parsed from %s\n", args[0])
	for _, line := range engine.BuildSummary(spec) {
		fmt.Printf("  %s\n", line)
	}
	fmt.Printf("  desired resources:  %d\n", len(desired))
	fmt.Printf("  new resources:      %d\n", len(newResources))
	fmt.Printf("  existing (skipped): %d\n", len(desired)-len(newResources))
	fmt.Println()

	if len(newResources) == 0 {
		adpt := getAdapter(cfg)
		if err := writeUFSTrack("deploy", args[0], cfg, adpt.Name(), "no_changes", spec, desired, newResources); err != nil {
			fmt.Printf("⚠ failed to update %s: %v\n", ufsTrackFile, err)
		}
		fmt.Println("✓ No new resources to create")
		return nil
	}

	adpt := getAdapter(cfg)
	if err := adpt.Apply(filteredSpec); err != nil {
		return err
	}

	st.Merge(newResources)
	if err := flowstate.Save(stateFile, st); err != nil {
		return err
	}
	if err := writeUFSTrack("deploy", args[0], cfg, adpt.Name(), "success", spec, desired, newResources); err != nil {
		fmt.Printf("⚠ failed to update %s: %v\n", ufsTrackFile, err)
	}

	fmt.Println()
	fmt.Printf("✓ Deploy finished via %s and state updated\n", adpt.Name())
	return nil
}

// ----- destroy -----

func handleDestroy(args []string) error {
	if len(args) != 0 {
		return errors.New("usage: flow destroy")
	}
	if err := ensureFlowSetup(); err != nil {
		return err
	}

	cfg, err := LoadConfig()
	if err != nil {
		return err
	}

	adpt := getAdapter(cfg)
	if err := adpt.Destroy(nil); err != nil {
		return err
	}

	empty := &flowstate.State{Resources: []flowstate.ResourceRecord{}}
	if err := flowstate.Save(stateFile, empty); err != nil {
		return err
	}

	fmt.Printf("✓ Destroy finished via %s and state reset\n", adpt.Name())
	return nil
}

// ----- status -----

func handleStatus(args []string) error {
	if len(args) != 0 {
		return errors.New("usage: flow status")
	}
	if err := ensureFlowSetup(); err != nil {
		return err
	}

	cfg, err := LoadConfig()
	if err != nil {
		return err
	}

	st, err := flowstate.Load(stateFile)
	if err != nil {
		return err
	}

	fmt.Printf("UniFlow Status\n")
	fmt.Printf("  Backend:   %s\n", cfg.Backend)
	fmt.Printf("  Resources: %d\n", len(st.Resources))
	fmt.Println()

	payload, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(payload))
	return nil
}

// ----- modules -----

func handleModules(args []string) error {
	if len(args) == 0 {
		fmt.Println("Usage:")
		fmt.Println("  flow modules list       List cached modules")
		fmt.Println("  flow modules update     Re-download modules for a spec")
		fmt.Println("  flow modules clean      Remove all cached modules")
		fmt.Println("  flow modules mappings   Show available abstract module names")
		return nil
	}

	switch args[0] {
	case "list":
		return handleModulesList()
	case "clean":
		return handleModulesClean()
	case "mappings":
		return handleModulesMappings()
	case "update":
		return handleModulesUpdate(args[1:])
	default:
		return fmt.Errorf("unknown modules subcommand: %s", args[0])
	}
}

func handleModulesList() error {
	cache := modules.NewCache(modulesDir)
	cached, err := cache.List()
	if err != nil {
		return err
	}

	if len(cached) == 0 {
		fmt.Println("No modules cached. Run 'flow plan <file>' to download modules.")
		return nil
	}

	fmt.Printf("Cached modules (%d):\n", len(cached))
	for _, m := range cached {
		fmt.Printf("  %s @ %s\n    %s\n", m.Source, m.Version, m.Path)
	}
	return nil
}

func handleModulesClean() error {
	cache := modules.NewCache(modulesDir)
	if err := cache.Clean(); err != nil {
		return err
	}
	fmt.Println("✓ Module cache cleaned")
	return nil
}

func handleModulesMappings() error {
	names := modules.ListMappings()
	sort.Strings(names)

	fmt.Printf("Available module mappings (%d):\n", len(names))
	for _, name := range names {
		fmt.Printf("  %s\n", name)
		for _, provider := range []string{"aws", "azure", "gcp"} {
			coords := modules.Resolve(name, provider)
			if coords != nil {
				fmt.Printf("    %s → %s\n", provider, coords.FullSource())
			}
		}
	}
	return nil
}

func handleModulesUpdate(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: flow modules update <file.ufl>")
	}
	if err := validateSpecFile(args[0]); err != nil {
		return err
	}

	cfg, err := LoadConfig()
	if err != nil {
		return err
	}

	spec, err := engine.ParseDSL(args[0])
	if err != nil {
		return err
	}

	// Clean existing cache first
	cache := modules.NewCache(cfg.ModuleCache)
	if err := cache.Clean(); err != nil {
		return err
	}

	// Re-download
	return resolveModules(spec, cfg)
}

// ----- helpers -----

func ensureFlowSetup() error {
	if err := os.MkdirAll(terraformDir, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", terraformDir, err)
	}
	if err := os.MkdirAll(pulumiDir, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", pulumiDir, err)
	}
	if err := os.MkdirAll(githubDir, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", githubDir, err)
	}

	if _, err := os.Stat(stateFile); errors.Is(err, os.ErrNotExist) {
		empty := &flowstate.State{Resources: []flowstate.ResourceRecord{}}
		if err := flowstate.Save(stateFile, empty); err != nil {
			return err
		}
	}
	return nil
}

func validateSpecFile(file string) error {
	ext := strings.ToLower(filepath.Ext(file))
	if ext != ".ufs" && ext != ".ufl" && ext != ".flow" && ext != ".fs" {
		return fmt.Errorf("unsupported spec extension %q: use .ufs (recommended), .ufl, .flow, or .fs", ext)
	}
	return nil
}

// resolveModules downloads all modules referenced in the spec.
func resolveModules(spec *engine.FlowSpec, cfg *Config) error {
	if spec == nil || len(spec.Apps) == 0 {
		return nil
	}

	registry := modules.NewRegistryClient(cfg.ModuleCache)
	if cfg.Registry != "" {
		registry.RegistryURL = cfg.Registry
	}

	// Load user mappings
	userMappings, _ := modules.LoadUserMappings(modulesJSON)

	fmt.Println("⟳ Resolving modules ...")

	seen := map[string]bool{}
	for _, app := range spec.Apps {
		provider := ""
		if app.Cloud != nil {
			provider = app.Cloud.Provider
		}

		for _, mod := range app.Modules {
			key := mod.Module + ":" + provider
			if seen[key] {
				continue
			}
			seen[key] = true

			// Skip if user provided explicit source
			if strings.TrimSpace(mod.Config["source"]) != "" {
				fmt.Printf("  • %s → using explicit source\n", mod.Module)
				continue
			}

			coords := modules.ResolveWithUser(mod.Module, provider, userMappings)
			if coords == nil {
				fmt.Printf("  ⚠ %s → no mapping found (will use legacy resolution)\n", mod.Module)
				continue
			}

			version := strings.TrimSpace(mod.Config["version"])
			localPath, err := registry.Ensure(coords, version)
			if err != nil {
				fmt.Printf("  ⚠ %s → download failed: %v (will use registry source)\n", mod.Module, err)
				continue
			}

			fmt.Printf("  ✓ %s → %s (%s)\n", mod.Module, coords.FullSource(), localPath)
		}
	}

	fmt.Println()
	return nil
}

// getAdapter returns the appropriate IaC adapter based on config.
func getAdapter(cfg *Config) adapter.IaCAdapter {
	switch cfg.Backend {
	case "github-actions":
		return githubactionsadapter.New(githubDir)
	case "pulumi":
		return pulumiadapter.New(pulumiDir)
	default:
		return terraformadapter.NewWithCache(terraformDir, cfg.ModuleCache)
	}
}

type ufsTrack struct {
	Version   string        `json:"version"`
	UpdatedAt string        `json:"updated_at"`
	Runs      []ufsTrackRun `json:"runs"`
}

type ufsTrackRun struct {
	Timestamp string                     `json:"timestamp"`
	Command   string                     `json:"command"`
	SpecFile  string                     `json:"spec_file"`
	Backend   string                     `json:"backend"`
	Adapter   string                     `json:"adapter"`
	Status    string                     `json:"status"`
	Summary   ufsTrackSummary            `json:"summary"`
	Apps      []string                   `json:"apps"`
	Desired   []flowstate.ResourceRecord `json:"desired_resources"`
	New       []flowstate.ResourceRecord `json:"new_resources"`
	Existing  []flowstate.ResourceRecord `json:"existing_resources"`
}

type ufsTrackSummary struct {
	DesiredCount  int `json:"desired_count"`
	NewCount      int `json:"new_count"`
	ExistingCount int `json:"existing_count"`
	AppCount      int `json:"app_count"`
	ModuleCount   int `json:"module_count"`
	ResourceCount int `json:"resource_count"`
}

func writeUFSTrack(
	command, specFile string,
	cfg *Config,
	adapterName, status string,
	spec *engine.FlowSpec,
	desired, newResources []flowstate.ResourceRecord,
) error {
	track, err := loadUFSTrack(ufsTrackFile)
	if err != nil {
		return err
	}
	absSpec, _ := filepath.Abs(specFile)
	appNames, moduleCount, resourceCount := summarizeSpec(spec)
	existing := subtractResources(desired, newResources)
	now := time.Now().UTC().Format(time.RFC3339)

	run := ufsTrackRun{
		Timestamp: now,
		Command:   command,
		SpecFile:  absSpec,
		Backend:   cfg.Backend,
		Adapter:   adapterName,
		Status:    status,
		Summary: ufsTrackSummary{
			DesiredCount:  len(desired),
			NewCount:      len(newResources),
			ExistingCount: len(existing),
			AppCount:      len(appNames),
			ModuleCount:   moduleCount,
			ResourceCount: resourceCount,
		},
		Apps:     appNames,
		Desired:  append([]flowstate.ResourceRecord{}, desired...),
		New:      append([]flowstate.ResourceRecord{}, newResources...),
		Existing: existing,
	}

	track.Version = "1"
	track.UpdatedAt = now
	track.Runs = append(track.Runs, run)

	payload, err := json.MarshalIndent(track, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode %s: %w", ufsTrackFile, err)
	}
	payload = append(payload, '\n')
	if err := os.WriteFile(ufsTrackFile, payload, 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", ufsTrackFile, err)
	}
	return nil
}

func loadUFSTrack(path string) (*ufsTrack, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &ufsTrack{Version: "1", Runs: []ufsTrackRun{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return &ufsTrack{Version: "1", Runs: []ufsTrackRun{}}, nil
	}

	var track ufsTrack
	if err := json.Unmarshal(data, &track); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", path, err)
	}
	if track.Runs == nil {
		track.Runs = []ufsTrackRun{}
	}
	if track.Version == "" {
		track.Version = "1"
	}
	return &track, nil
}

func summarizeSpec(spec *engine.FlowSpec) ([]string, int, int) {
	if spec == nil {
		return []string{}, 0, 0
	}
	apps := make([]string, 0, len(spec.Apps))
	moduleCount := 0
	resourceCount := 0
	for _, app := range spec.Apps {
		apps = append(apps, app.Name)
		moduleCount += len(app.Modules)
		resourceCount += len(app.Resources)
	}
	return apps, moduleCount, resourceCount
}

func subtractResources(all, toRemove []flowstate.ResourceRecord) []flowstate.ResourceRecord {
	remove := map[string]bool{}
	for _, r := range toRemove {
		remove[r.ID] = true
	}
	out := make([]flowstate.ResourceRecord, 0, len(all))
	for _, r := range all {
		if remove[r.ID] {
			continue
		}
		out = append(out, r)
	}
	return out
}
