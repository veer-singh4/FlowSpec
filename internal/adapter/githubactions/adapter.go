package githubactions

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/veer-singh4/FlowSpec/internal/adapter"
	"github.com/veer-singh4/FlowSpec/internal/engine"
)

var _ adapter.IaCAdapter = (*Adapter)(nil)

// Adapter generates GitHub Actions workflow YAML from FlowSpec.
type Adapter struct {
	WorkDir     string
	ProjectRoot string
}

// New creates a GitHub Actions adapter.
func New(workDir string) *Adapter {
	return &Adapter{
		WorkDir:     workDir,
		ProjectRoot: ".",
	}
}

// Name returns the adapter name.
func (a *Adapter) Name() string {
	return "github-actions"
}

// Init prepares backend directories.
func (a *Adapter) Init(_ *engine.FlowSpec) error {
	return a.ensureDirs()
}

// Plan generates workflow YAML in backend workdir.
func (a *Adapter) Plan(config *engine.FlowSpec) error {
	if err := a.ensureDirs(); err != nil {
		return err
	}

	docs := collectYAMLDocs(config)
	if len(docs) == 0 {
		return fmt.Errorf("no yaml blocks found; add `yaml <name> { ... }` in your .ufs app")
	}

	for _, doc := range docs {
		out := filepath.Join(a.WorkDir, doc.FileName)
		if err := os.WriteFile(out, []byte(renderYAML(doc.Data)), 0o644); err != nil {
			return fmt.Errorf("failed to write workflow file: %w", err)
		}
		fmt.Printf("✓ YAML generated at %s\n", out)
	}
	return nil
}

// Apply generates workflow YAML and publishes to .github/workflows.
func (a *Adapter) Apply(config *engine.FlowSpec) error {
	if err := a.ensureDirs(); err != nil {
		return err
	}
	docs := collectYAMLDocs(config)
	if len(docs) == 0 {
		return fmt.Errorf("no yaml blocks found; add `yaml <name> { ... }` in your .ufs app")
	}

	for _, doc := range docs {
		content := []byte(renderYAML(doc.Data))

		backendOut := filepath.Join(a.WorkDir, doc.FileName)
		if err := os.WriteFile(backendOut, content, 0o644); err != nil {
			return fmt.Errorf("failed to write backend workflow file: %w", err)
		}

		target := filepath.Join(a.ProjectRoot, doc.TargetPath)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("failed to create target dir for %s: %w", target, err)
		}
		if err := os.WriteFile(target, content, 0o644); err != nil {
			return fmt.Errorf("failed to write %s: %w", target, err)
		}
		fmt.Printf("✓ YAML published to %s\n", target)
	}
	return nil
}

// Destroy removes generated workflow files.
func (a *Adapter) Destroy(config *engine.FlowSpec) error {
	docs := collectYAMLDocs(config)
	for _, doc := range docs {
		backendPath := filepath.Join(a.WorkDir, doc.FileName)
		if err := os.Remove(backendPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove %s: %w", backendPath, err)
		}
		target := filepath.Join(a.ProjectRoot, doc.TargetPath)
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove %s: %w", target, err)
		}
	}
	fmt.Println("✓ Generated YAML files cleaned")
	return nil
}

func (a *Adapter) ensureDirs() error {
	if err := os.MkdirAll(a.WorkDir, 0o755); err != nil {
		return fmt.Errorf("failed to create github-actions workdir: %w", err)
	}
	return nil
}

type yamlDoc struct {
	FileName   string
	TargetPath string
	Data       map[string]any
}

func collectYAMLDocs(spec *engine.FlowSpec) []yamlDoc {
	if spec == nil {
		return []yamlDoc{}
	}
	out := []yamlDoc{}
	for _, app := range spec.Apps {
		for _, y := range app.YAMLs {
			name := sanitizeFileName(y.Name)
			target := filepath.ToSlash(filepath.Join(".github", "workflows", name+".yml"))
			root := map[string]any{}

			for k, v := range y.Config {
				if k == "_target" && strings.TrimSpace(v) != "" {
					target = filepath.ToSlash(strings.TrimSpace(v))
					continue
				}
				setNestedValue(root, k, inferValue(v))
			}

			out = append(out, yamlDoc{
				FileName:   name + ".yml",
				TargetPath: target,
				Data:       root,
			})
		}
	}
	return out
}

func sanitizeFileName(v string) string {
	s := strings.TrimSpace(strings.ToLower(v))
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.ReplaceAll(s, "/", "-")
	if s == "" {
		return "generated"
	}
	return s
}

func setNestedValue(root map[string]any, key string, value any) {
	parts := strings.Split(key, ".")
	cur := root
	for i := 0; i < len(parts)-1; i++ {
		part := strings.TrimSpace(parts[i])
		if part == "" {
			continue
		}
		next, ok := cur[part].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[part] = next
		}
		cur = next
	}
	last := strings.TrimSpace(parts[len(parts)-1])
	if last != "" {
		cur[last] = value
	}
}

func inferValue(raw string) any {
	v := strings.TrimSpace(raw)
	lower := strings.ToLower(v)
	switch lower {
	case "true":
		return true
	case "false":
		return false
	case "null":
		return nil
	}
	if n, err := strconv.ParseFloat(v, 64); err == nil {
		if strings.Contains(v, ".") {
			return n
		}
		return int64(n)
	}
	if strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]") {
		return parseArray(v)
	}
	return strings.Trim(v, "\"")
}

func parseArray(lit string) []any {
	body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(lit, "["), "]"))
	if body == "" {
		return []any{}
	}
	parts := splitCSV(body)
	out := make([]any, 0, len(parts))
	for _, p := range parts {
		out = append(out, inferValue(strings.TrimSpace(p)))
	}
	return out
}

func splitCSV(s string) []string {
	parts := []string{}
	var cur strings.Builder
	inQuotes := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '"' {
			inQuotes = !inQuotes
			cur.WriteByte(ch)
			continue
		}
		if ch == ',' && !inQuotes {
			parts = append(parts, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(ch)
	}
	if cur.Len() > 0 {
		parts = append(parts, cur.String())
	}
	return parts
}

func renderYAML(root map[string]any) string {
	var b strings.Builder
	writeMap(&b, root, 0)
	return b.String()
}

func writeMap(b *strings.Builder, m map[string]any, indent int) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		writeIndent(b, indent)
		v := m[k]
		switch tv := v.(type) {
		case map[string]any:
			b.WriteString(k)
			b.WriteString(":\n")
			writeMap(b, tv, indent+2)
		case []any:
			b.WriteString(k)
			b.WriteString(":\n")
			writeArray(b, tv, indent+2)
		default:
			b.WriteString(k)
			b.WriteString(": ")
			b.WriteString(formatScalar(tv))
			b.WriteByte('\n')
		}
	}
}

func writeArray(b *strings.Builder, arr []any, indent int) {
	if len(arr) == 0 {
		writeIndent(b, indent)
		b.WriteString("[]\n")
		return
	}
	for _, v := range arr {
		writeIndent(b, indent)
		b.WriteString("-")
		switch tv := v.(type) {
		case map[string]any:
			b.WriteByte('\n')
			writeMap(b, tv, indent+2)
		case []any:
			b.WriteByte('\n')
			writeArray(b, tv, indent+2)
		default:
			b.WriteByte(' ')
			b.WriteString(formatScalar(tv))
			b.WriteByte('\n')
		}
	}
}

func writeIndent(b *strings.Builder, n int) {
	for i := 0; i < n; i++ {
		b.WriteByte(' ')
	}
}

func formatScalar(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case bool:
		if t {
			return "true"
		}
		return "false"
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case string:
		return strconv.Quote(t)
	default:
		return strconv.Quote(fmt.Sprintf("%v", t))
	}
}
