# UniFlow Language + Flow CLI

UniFlow is a **universal infrastructure language** for developers.
Write once in `.ufs`, the CLI converts it to Terraform (or Pulumi) in the background.

- Language name: **UniFlow Spec**
- File extension: **`.ufs`** (recommended)
- CLI command: **`flow`**

## Why UniFlow?

| Problem | UniFlow Solution |
|---------|-----------------|
| Developers must learn Terraform HCL, Pulumi, Bicep... | Learn only UniFlow — one language for all backends |
| Terraform module sources are long and cryptic | Write `use vm.basic as my-vm` — UniFlow resolves module sources automatically |
| Module downloads happen implicitly during `terraform init` | UniFlow manages its own module cache (like Crossplane providers) |
| Switching IaC tools means rewriting everything | Same `.ufs` file, different backend — `flow init --backend pulumi` |

## How it works

```
.ufs file → Native Go Parser → Module Resolver → Terraform/Pulumi Backend
                (no Python!)      (auto-download)    (generates HCL/code)
```

## Example (`.ufs`)

```ufs
app payment-service {
  cloud aws ap-south-1

  use vm.basic as app-vm {
    name payment-service-vm
    ami ami-0f5ee92e2d63afc18
    instance_type t3.micro
  }

  use networking.vpc as app-vpc {
    name payment-vpc
    cidr 10.10.0.0/16
    azs ["ap-south-1a","ap-south-1b"]
    private_subnets ["10.10.1.0/24","10.10.2.0/24"]
    public_subnets ["10.10.101.0/24","10.10.102.0/24"]
  }
}
```

## Simple Language Mode

You can now write a simpler style with:
- `let` variables
- optional `=` assignments
- optional `as` (alias can be direct)
- condition expressions (`==`, `!=`, `>`, `<`, `>=`, `<=`, `&&`, `||`, `!`)
- backend automatic type inference (string/bool/number/array/null)

No explicit type declaration is required in your `.ufs` file.

```ufs
app demo {
  cloud azure centralindia

  let env = "prod"
  let replicas = 2

  func make-subnet {
    resource azurerm_subnet ${subnet_name} {
      name = ${subnet_name}
      resource_group_name = app-rg
      virtual_network_name = app-vnet
      address_prefixes ["10.1.1.0/24"]
    }
  }

  for subnet_name in ["app-subnet","data-subnet"] {
    if env == prod && replicas >= 2 {
      call make-subnet
    } else {
      resource azurerm_network_security_group skipped-nsg {
        name = skipped
        location = centralindia
        resource_group_name = app-rg
      }
    }
  }
}
```

Notes:
- `if <expression> { ... } else { ... }` supports comparison and boolean operators.
- `for <var> in [a,b,c] { ... }` repeats the block and exposes `${var}` (or `${loop.var}`).
- `func <name> { ... }` defines a reusable block in the same `app`.
- `call <name>` expands the function block inline.
- Config lines can be written as `key value` or `key = value`.
- In Terraform generation, values are auto-detected as bool/number/null/list/string.

## CLI Commands

```bash
flow init                          # Initialize workspace
flow init --backend pulumi         # Use Pulumi backend
flow init --backend github-actions # Generate YAML from `yaml` blocks in .ufs
flow plan   app.ufs                # Preview infrastructure changes
flow deploy app.ufs                # Apply infrastructure changes
flow status                        # Show managed resources
flow destroy                       # Tear down everything
flow modules list                  # Show cached modules
flow modules mappings              # Show available abstract module names
flow modules update app.ufs        # Re-download modules
flow modules clean                 # Clear module cache
flow version                       # Print CLI version
```

Pulumi backend currently runs in compatibility mode using the same generated plan/apply engine as Terraform, so the same `.ufs` code works without backend-specific changes.
`flow plan` and `flow deploy` also update a project-root `.ufstrack` file to track desired/new/existing resources per run.
GitHub Actions backend reads `yaml` blocks from your `.ufs` and generates `.yml` automatically (no separate YAML language needed).

Example YAML block:
```ufs
app ci {
  yaml release {
    _target .github/workflows/release.yml
    name "Release Pipeline"
    on.workflow_dispatch true
    jobs.build.runs-on ubuntu-latest
    jobs.build.steps ["checkout","build","deploy"]
  }
}
```

## Available Module Mappings

UniFlow resolves abstract module names to real Terraform modules:

| UniFlow Module | AWS | Azure |
|---|---|---|
| `vm.basic` | terraform-aws-modules/ec2-instance/aws | Azure/compute/azurerm |
| `networking.vpc` | terraform-aws-modules/vpc/aws | — |
| `networking.vnet` | — | Azure/network/azurerm |
| `storage.s3` | terraform-aws-modules/s3-bucket/aws | — |
| `storage.blob` | — | Azure/avm-res-storage-storageaccount/azurerm |
| `database.rds` | terraform-aws-modules/rds/aws | — |
| `security.sg` | terraform-aws-modules/security-group/aws | — |
| `container.ecs` | terraform-aws-modules/ecs/aws | — |
| `container.aks` | — | Azure/aks/azurerm |
| `dns.route53` | terraform-aws-modules/route53/aws | — |
| `loadbalancer.alb` | terraform-aws-modules/alb/aws | — |

Custom mappings can be added via `.flow/modules.json`.

## Installation

### Quick Install (One-liner)

**Linux / macOS:**
```bash
curl -sSL https://raw.githubusercontent.com/veer-singh4/FlowSpec/main/install.sh | sh
```

**Windows (PowerShell):**
```powershell
iwr https://raw.githubusercontent.com/veer-singh4/FlowSpec/main/install.ps1 | iex
```

### Install via Go
If you have Go 1.21+ installed, you can install directly from source:
```bash
go install github.com/veer-singh4/FlowSpec/cmd/flow@latest
```

### Prerequisites
- [Terraform](https://www.terraform.io/downloads) installed and in your PATH.
- (Optional) [Go 1.21+](https://golang.org/doc/install) if building from source.

### Cross-platform Release Binaries

```bash
mkdir -p dist
GOOS=linux   GOARCH=amd64 go build -o dist/flow-linux-amd64 ./cmd/flow
GOOS=linux   GOARCH=arm64 go build -o dist/flow-linux-arm64 ./cmd/flow
GOOS=darwin  GOARCH=amd64 go build -o dist/flow-darwin-amd64 ./cmd/flow
GOOS=darwin  GOARCH=arm64 go build -o dist/flow-darwin-arm64 ./cmd/flow
GOOS=windows GOARCH=amd64 go build -o dist/flow-windows-amd64.exe ./cmd/flow
GOOS=windows GOARCH=arm64 go build -o dist/flow-windows-arm64.exe ./cmd/flow
```

## Architecture

```
flowspec/
├── cmd/flow/main.go              # CLI entry point
├── internal/
│   ├── parser/                   # Native Go lexer + parser (no Python!)
│   │   ├── lexer.go              # Tokenizer with line/col tracking
│   │   └── parser.go             # Recursive descent parser → AST
│   ├── engine/engine.go          # AST → FlowSpec conversion
│   ├── modules/                  # Module registry & cache
│   │   ├── mapping.go            # Abstract name → registry coordinates
│   │   ├── registry.go           # Terraform Registry API client
│   │   └── cache.go              # Local module cache manager
│   ├── adapter/                  # Multi-backend adapters
│   │   ├── adapter.go            # IaCAdapter interface
│   │   ├── terraform/adapter.go  # Terraform code generator
│   │   └── pulumi/adapter.go     # Pulumi stub (coming soon)
│   ├── cli/                      # CLI command handlers
│   │   ├── cli.go                # Main command router
│   │   └── config.go             # .flow/config.json manager
│   └── state/state.go            # Resource state tracking
├── examples/                     # Example .ufs files
└── test.ufs                      # Test spec file
```

## Notes

- **No Python dependency** — the parser is written in pure Go
- `.ufl`, `.flow`, and `.fs` are accepted for backward compatibility
- `.ufs` is the recommended extension
