package terraform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/veer-singh4/FlowSpec/internal/adapter"
	"github.com/veer-singh4/FlowSpec/internal/engine"
	"github.com/veer-singh4/FlowSpec/internal/modules"
)

var _ adapter.IaCAdapter = (*Adapter)(nil)

// Adapter generates and executes Terraform code from a FlowSpec.
type Adapter struct {
	WorkDir      string
	ModuleCache  string // path to .flow/modules
	UseLocalCache bool  // if true, use local module paths instead of registry sources
}

// New creates a Terraform adapter.
func New(workDir string) *Adapter {
	return &Adapter{
		WorkDir:       workDir,
		ModuleCache:   filepath.Join(".flow", "modules"),
		UseLocalCache: true,
	}
}

// NewWithCache creates a Terraform adapter with a custom module cache directory.
func NewWithCache(workDir, cacheDir string) *Adapter {
	return &Adapter{
		WorkDir:       workDir,
		ModuleCache:   cacheDir,
		UseLocalCache: true,
	}
}

// Name returns the adapter name.
func (a *Adapter) Name() string {
	return "terraform"
}

// Init runs terraform init in the work directory.
func (a *Adapter) Init(config *engine.FlowSpec) error {
	if err := os.MkdirAll(a.WorkDir, 0o755); err != nil {
		return fmt.Errorf("failed to create terraform workdir: %w", err)
	}
	if config != nil {
		if err := a.writeTerraform(config); err != nil {
			return err
		}
	}
	return a.runTerraform("init", "-input=false")
}

// Plan generates Terraform code and runs terraform plan.
func (a *Adapter) Plan(config *engine.FlowSpec) error {
	if err := a.writeTerraform(config); err != nil {
		return err
	}
	if err := a.runTerraform("init", "-input=false"); err != nil {
		return err
	}
	return a.runTerraform("plan", "-input=false")
}

// Apply generates Terraform code and runs terraform apply.
func (a *Adapter) Apply(config *engine.FlowSpec) error {
	if err := a.writeTerraform(config); err != nil {
		return err
	}
	if err := a.runTerraform("init", "-input=false"); err != nil {
		return err
	}
	return a.runTerraform("apply", "-auto-approve", "-input=false")
}

// Destroy runs terraform destroy.
func (a *Adapter) Destroy(_ *engine.FlowSpec) error {
	if err := os.MkdirAll(a.WorkDir, 0o755); err != nil {
		return fmt.Errorf("failed to create terraform workdir: %w", err)
	}
	if err := a.runTerraform("init", "-input=false"); err != nil {
		return err
	}
	return a.runTerraform("destroy", "-auto-approve", "-input=false")
}

func (a *Adapter) writeTerraform(config *engine.FlowSpec) error {
	if config == nil || len(config.Apps) == 0 {
		return fmt.Errorf("empty FlowSpec config")
	}
	if err := os.MkdirAll(a.WorkDir, 0o755); err != nil {
		return fmt.Errorf("failed to create terraform workdir: %w", err)
	}

	mainTF, err := a.buildMainTF(config)
	if err != nil {
		return err
	}

	path := filepath.Join(a.WorkDir, "main.tf")
	if err := os.WriteFile(path, []byte(mainTF), 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	return nil
}

func (a *Adapter) runTerraform(args ...string) error {
	cmd := exec.Command("terraform", args...)
	cmd.Dir = a.WorkDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("terraform %s failed: %w", strings.Join(args, " "), err)
	}
	return nil
}

func (a *Adapter) buildMainTF(spec *engine.FlowSpec) (string, error) {
	provider, region, err := resolveCloud(spec)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	if provider == "aws" {
		b.WriteString(awsHeader(region))
	} else if provider == "azure" {
		b.WriteString(azureHeader(region))
	} else if provider == "gcp" {
		b.WriteString(gcpHeader(region))
	} else {
		return "", fmt.Errorf("unsupported provider: %s", provider)
	}

	seenModule := map[string]bool{}
	for _, app := range spec.Apps {
		// Generate resource blocks
		for _, res := range app.Resources {
			resLabel := sanitize(app.Name + "_" + res.Alias)
			block := resourceBlock(res.Type, resLabel, res.Config)
			b.WriteString(block)
		}

		// Generate module blocks
		for _, mod := range app.Modules {
			moduleLabel := sanitize(app.Name + "_" + mod.Alias)
			if seenModule[moduleLabel] {
				continue
			}
			seenModule[moduleLabel] = true

			block, err := a.moduleBlock(app.Name, provider, mod)
			if err != nil {
				return "", err
			}
			b.WriteString(block)
		}
	}

	return b.String(), nil
}

func (a *Adapter) moduleBlock(appName, provider string, mod engine.ModuleConfig) (string, error) {
	// First check if user set source explicitly
	source := strings.TrimSpace(mod.Config["source"])

	if source == "" {
		// Resolve from UniFlow module mappings
		coords := modules.Resolve(mod.Module, provider)
		if coords != nil {
			if a.UseLocalCache {
				// Check if any version is actually cached locally
				versionDir := a.findCachedVersion(coords)
				if versionDir != "" {
					source = versionDir
				} else {
					// Not cached — fall back to registry source (terraform init will download)
					source = coords.FullSource()
				}
			} else {
				source = coords.FullSource()
			}
		}
	}

	if source == "" {
		// Last resort: try the legacy inline resolution
		source = resolveModuleSourceLegacy(mod)
	}

	if source == "" {
		return "", fmt.Errorf(
			"module %s (alias=%s app=%s) is unknown; add module mapping or set 'source' explicitly",
			mod.Module, mod.Alias, appName,
		)
	}

	label := sanitize(appName + "_" + mod.Alias)
	var b strings.Builder
	b.WriteString(fmt.Sprintf("module \"%s\" {\n", label))
	b.WriteString(fmt.Sprintf("  source = %q\n", source))

	keys := sortedKeys(mod.Config)
	for _, key := range keys {
		if key == "source" || key == "version" {
			continue
		}
		b.WriteString(fmt.Sprintf("  %s = %s\n", sanitizeVarName(key), terraformLiteral(mod.Config[key])))
	}

	// Add version constraint if specified
	if v := strings.TrimSpace(mod.Config["version"]); v != "" {
		b.WriteString(fmt.Sprintf("  version = %q\n", v))
	}

	b.WriteString("}\n\n")
	return b.String(), nil
}

// resourceBlock generates a Terraform resource block.
func resourceBlock(resType, label string, config map[string]string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("resource \"%s\" \"%s\" {\n", resType, label))

	keys := sortedKeys(config)
	for _, key := range keys {
		b.WriteString(fmt.Sprintf("  %s = %s\n", sanitizeVarName(key), terraformLiteral(config[key])))
	}

	b.WriteString("}\n\n")
	return b.String()
}

// findCachedVersion looks for downloaded module versions in the cache.
func (a *Adapter) findCachedVersion(coords *modules.RegistryCoords) string {
	baseDir := filepath.Join(a.ModuleCache, coords.Namespace, coords.Name, coords.System)
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return ""
	}
	// Use the first (latest) version directory found
	for _, e := range entries {
		if e.IsDir() {
			// Return relative path from terraform workdir
			abs := filepath.Join(baseDir, e.Name())
			rel, err := filepath.Rel(a.WorkDir, abs)
			if err != nil {
				return abs
			}
			return rel
		}
	}
	return ""
}

func resolveCloud(spec *engine.FlowSpec) (string, string, error) {
	if len(spec.Apps) == 0 || spec.Apps[0].Cloud == nil {
		return "", "", fmt.Errorf("cloud provider/region is required")
	}
	provider := spec.Apps[0].Cloud.Provider
	region := spec.Apps[0].Cloud.Region

	for _, app := range spec.Apps {
		if app.Cloud == nil {
			return "", "", fmt.Errorf("app %s is missing cloud block", app.Name)
		}
		if app.Cloud.Provider != provider || app.Cloud.Region != region {
			return "", "", fmt.Errorf("mixed cloud providers/regions are not supported in MVP")
		}
	}
	return provider, region, nil
}

func awsHeader(region string) string {
	return fmt.Sprintf(`terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.0"
    }
  }
}

provider "aws" {
  region = "%s"
}

`, region)
}

func azureHeader(region string) string {
	return fmt.Sprintf(`terraform {
  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = "~> 3.0"
    }
  }
}

provider "azurerm" {
  features {}
}

locals {
  location = "%s"
}

`, region)
}

func gcpHeader(region string) string {
	return fmt.Sprintf(`terraform {
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = ">= 5.0"
    }
  }
}

provider "google" {
  region = "%s"
}

`, region)
}

func terraformLiteral(v string) string {
	trimmed := strings.TrimSpace(v)
	if trimmed == "" {
		return `""`
	}
	lower := strings.ToLower(trimmed)
	if lower == "true" || lower == "false" || lower == "null" {
		return lower
	}
	if isNumber(trimmed) {
		return trimmed
	}
	if isQuoted(trimmed) {
		return trimmed
	}
	if strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "{") {
		return trimmed
	}
	return fmt.Sprintf("%q", trimmed)
}

func isNumber(v string) bool {
	if _, err := strconv.ParseFloat(v, 64); err == nil {
		return true
	}
	return false
}

func isQuoted(v string) bool {
	return len(v) >= 2 && strings.HasPrefix(v, `"`) && strings.HasSuffix(v, `"`)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sanitize(value string) string {
	value = strings.ToLower(value)
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, ".", "_")
	return value
}

func sanitizeVarName(value string) string {
	return sanitize(value)
}

// resolveModuleSourceLegacy provides backward compatibility for old-style module resolution.
func resolveModuleSourceLegacy(mod engine.ModuleConfig) string {
	name := strings.TrimSpace(strings.ToLower(mod.Module))
	switch name {
	case "vm.basic":
		if strings.TrimSpace(mod.Config["ami"]) != "" {
			return "terraform-aws-modules/ec2-instance/aws"
		}
		if strings.TrimSpace(mod.Config["vm_size"]) != "" {
			return "Azure/compute/azurerm"
		}
	case "network.vpc", "networking.vpc":
		return "terraform-aws-modules/vpc/aws"
	case "network.vnet", "networking.vnet":
		return "Azure/network/azurerm"
	}
	return ""
}
