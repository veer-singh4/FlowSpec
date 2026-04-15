package modules

import "strings"

// RegistryCoords holds the namespace/name/system for a Terraform Registry module.
type RegistryCoords struct {
	Namespace string // e.g. "terraform-aws-modules"
	Name      string // e.g. "vpc"
	System    string // e.g. "aws"
}

// FullSource returns the Terraform-compatible source string.
func (r RegistryCoords) FullSource() string {
	return r.Namespace + "/" + r.Name + "/" + r.System
}

// ModuleMapping maps a UniFlow abstract module name to provider-specific registry coordinates.
type ModuleMapping struct {
	AWS   *RegistryCoords
	Azure *RegistryCoords
	GCP   *RegistryCoords
}

// DefaultMappings maps UniFlow abstract module names to Terraform Registry modules.
// Users can override or extend these via .flow/modules.json.
var DefaultMappings = map[string]ModuleMapping{
	"vm.basic": {
		AWS: &RegistryCoords{
			Namespace: "terraform-aws-modules",
			Name:      "ec2-instance",
			System:    "aws",
		},
		Azure: &RegistryCoords{
			Namespace: "Azure",
			Name:      "compute",
			System:    "azurerm",
		},
	},
	"networking.vpc": {
		AWS: &RegistryCoords{
			Namespace: "terraform-aws-modules",
			Name:      "vpc",
			System:    "aws",
		},
	},
	"network.vpc": {
		AWS: &RegistryCoords{
			Namespace: "terraform-aws-modules",
			Name:      "vpc",
			System:    "aws",
		},
	},
	"networking.vnet": {
		Azure: &RegistryCoords{
			Namespace: "Azure",
			Name:      "network",
			System:    "azurerm",
		},
	},
	"network.vnet": {
		Azure: &RegistryCoords{
			Namespace: "Azure",
			Name:      "network",
			System:    "azurerm",
		},
	},
	"storage.s3": {
		AWS: &RegistryCoords{
			Namespace: "terraform-aws-modules",
			Name:      "s3-bucket",
			System:    "aws",
		},
	},
	"storage.blob": {
		Azure: &RegistryCoords{
			Namespace: "Azure",
			Name:      "avm-res-storage-storageaccount",
			System:    "azurerm",
		},
	},
	"database.rds": {
		AWS: &RegistryCoords{
			Namespace: "terraform-aws-modules",
			Name:      "rds",
			System:    "aws",
		},
	},
	"security.sg": {
		AWS: &RegistryCoords{
			Namespace: "terraform-aws-modules",
			Name:      "security-group",
			System:    "aws",
		},
	},
	"container.ecs": {
		AWS: &RegistryCoords{
			Namespace: "terraform-aws-modules",
			Name:      "ecs",
			System:    "aws",
		},
	},
	"container.aks": {
		Azure: &RegistryCoords{
			Namespace: "Azure",
			Name:      "aks",
			System:    "azurerm",
		},
	},
	"dns.route53": {
		AWS: &RegistryCoords{
			Namespace: "terraform-aws-modules",
			Name:      "route53",
			System:    "aws",
		},
	},
	"loadbalancer.alb": {
		AWS: &RegistryCoords{
			Namespace: "terraform-aws-modules",
			Name:      "alb",
			System:    "aws",
		},
	},
}

// Resolve looks up a UniFlow abstract module name and returns the registry coordinates
// for the given cloud provider. Returns nil if no mapping is found.
func Resolve(moduleName, provider string) *RegistryCoords {
	moduleName = strings.TrimSpace(strings.ToLower(moduleName))
	provider = strings.TrimSpace(strings.ToLower(provider))

	mapping, ok := DefaultMappings[moduleName]
	if !ok {
		return nil
	}

	switch provider {
	case "aws":
		return mapping.AWS
	case "azure":
		return mapping.Azure
	case "gcp":
		return mapping.GCP
	default:
		return nil
	}
}

// ResolveSource returns the full Terraform source string for a module, or empty if not found.
func ResolveSource(moduleName, provider string) string {
	coords := Resolve(moduleName, provider)
	if coords == nil {
		return ""
	}
	return coords.FullSource()
}

// ListMappings returns all known abstract module names.
func ListMappings() []string {
	seen := map[string]bool{}
	out := []string{}
	for name := range DefaultMappings {
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}
