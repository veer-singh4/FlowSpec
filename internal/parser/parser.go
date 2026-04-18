package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// --- Parser AST types (decoupled from engine to avoid import cycles) ---

// CloudSpec describes a cloud provider target.
type CloudSpec struct {
	Provider string
	Region   string
}

// ModuleSpec describes a module usage block.
type ModuleSpec struct {
	Module string
	Alias  string
	Config map[string]string
	Line   int
}

// ResourceSpec describes a single cloud resource block.
type ResourceSpec struct {
	Type   string            // e.g. "azurerm_resource_group"
	Alias  string            // e.g. "main-rg"
	Config map[string]string
	Line   int
}

type YAMLSpec struct {
	Name   string
	Config map[string]string
	Line   int
}

type AppSpec struct {
	Name      string
	Cloud     *CloudSpec
	Modules   []ModuleSpec
	Resources []ResourceSpec
	YAMLs     []YAMLSpec
	Params    map[string]string
	Line      int
}

// Spec is the top-level parsed representation of a .ufl file.
type Spec struct {
	Apps []AppSpec
	Params map[string]string
}

// Parser is a recursive-descent parser for UniFlow (.ufl) files.
type Parser struct {
	tokens  []Token
	pos     int
	BaseDir string
}

type appFunctions map[string][]Token

// ParseFile reads a .ufl file and returns a Spec.
func ParseFile(filePath string) (*Spec, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", filePath, err)
	}
	p := &Parser{BaseDir: filepath.Dir(filePath)}
	return p.ParseSource(string(data))
}

// ParseSource parses UniFlow source code and returns a Spec.
func (p *Parser) ParseSource(source string) (*Spec, error) {
	lexer := NewLexer(source)
	tokens, err := lexer.Tokenize()
	if err != nil {
		return nil, err
	}

	p.tokens = tokens
	p.pos = 0
	return p.parseSpec()
}

func (p *Parser) peek() Token {
	if p.pos >= len(p.tokens) {
		return Token{Type: TokenEOF}
	}
	return p.tokens[p.pos]
}

func (p *Parser) advance() Token {
	tok := p.peek()
	if p.pos < len(p.tokens) {
		p.pos++
	}
	return tok
}

func (p *Parser) expect(tt TokenType) (Token, error) {
	tok := p.peek()
	if tok.Type != tt {
		return tok, fmt.Errorf("line %d: expected %s but got %s (%q)", tok.Line, tt, tok.Type, tok.Value)
	}
	return p.advance(), nil
}

// parseSpec parses the top-level spec: zero or more app blocks.
func (p *Parser) parseSpec() (*Spec, error) {
	spec := &Spec{Apps: []AppSpec{}, Params: map[string]string{}}

	for p.peek().Type != TokenEOF {
		if p.peek().Type == TokenApp {
			app, err := p.parseApp()
			if err != nil {
				return nil, err
			}
			spec.Apps = append(spec.Apps, app)
		} else if p.peek().Type == TokenParam {
			params, err := p.parseParams()
			if err != nil {
				return nil, err
			}
			for k, v := range params {
				spec.Params[k] = v
			}
		} else if p.peek().Type == TokenInclude {
			incSpec, err := p.handleInclude()
			if err != nil {
				return nil, err
			}
			spec.Apps = append(spec.Apps, incSpec.Apps...)
			for k, v := range incSpec.Params {
				spec.Params[k] = v
			}
		} else {
			tok := p.peek()
			return nil, fmt.Errorf("line %d: expected 'app' or 'params' but got %s (%q)", tok.Line, tok.Type, tok.Value)
		}
	}

	return spec, nil
}

// parseApp parses: app <name> { ... }
func (p *Parser) parseApp() (AppSpec, error) {
	appTok, err := p.expect(TokenApp)
	if err != nil {
		return AppSpec{}, err
	}

	nameTok, err := p.expect(TokenIdent)
	if err != nil {
		return AppSpec{}, fmt.Errorf("line %d: expected app name after 'app'", appTok.Line)
	}

	if _, err := p.expect(TokenLBrace); err != nil {
		return AppSpec{}, fmt.Errorf("line %d: expected '{' after app name %q", nameTok.Line, nameTok.Value)
	}

	app := AppSpec{
		Name:      nameTok.Value,
		Modules:   []ModuleSpec{},
		Resources: []ResourceSpec{},
		YAMLs:     []YAMLSpec{},
		Params:    map[string]string{},
		Line:      appTok.Line,
	}

	if err := p.parseAppBody(&app, appFunctions{}, map[string]string{}, true); err != nil {
		return AppSpec{}, err
	}
	return app, nil
}

func (p *Parser) parseAppBody(app *AppSpec, funcs appFunctions, vars map[string]string, stopAtRBrace bool) error {
	for {
		tok := p.peek()
		if tok.Type == TokenEOF {
			if stopAtRBrace {
				return fmt.Errorf("line %d: unclosed app block %q", app.Line, app.Name)
			}
			return nil
		}
		if stopAtRBrace && tok.Type == TokenRBrace {
			p.advance()
			return nil
		}

		switch tok.Type {
		case TokenCloud:
			cloud, err := p.parseCloud()
			if err != nil {
				return err
			}
			cloud.Provider = interpolateVars(cloud.Provider, vars)
			cloud.Region = interpolateVars(cloud.Region, vars)
			app.Cloud = &cloud
		case TokenUse:
			mod, err := p.parseUse()
			if err != nil {
				return err
			}
			applyVarsToModule(&mod, vars)
			app.Modules = append(app.Modules, mod)
		case TokenResource:
			res, err := p.parseResource()
			if err != nil {
				return err
			}
			applyVarsToResource(&res, vars)
			app.Resources = append(app.Resources, res)
		case TokenParam:
			params, err := p.parseParams()
			if err != nil {
				return err
			}
			for k, v := range params {
				app.Params[k] = interpolateVars(v, vars)
			}
		case TokenInclude:
			incSpec, err := p.handleInclude()
			if err != nil {
				return err
			}
			for _, ia := range incSpec.Apps {
				for _, m := range ia.Modules {
					cm := m
					applyVarsToModule(&cm, vars)
					app.Modules = append(app.Modules, cm)
				}
				for _, r := range ia.Resources {
					cr := r
					applyVarsToResource(&cr, vars)
					app.Resources = append(app.Resources, cr)
				}
				for _, y := range ia.YAMLs {
					cy := y
					applyVarsToYAML(&cy, vars)
					app.YAMLs = append(app.YAMLs, cy)
				}
				for k, v := range ia.Params {
					app.Params[k] = interpolateVars(v, vars)
				}
				if ia.Cloud != nil && app.Cloud == nil {
					c := *ia.Cloud
					c.Provider = interpolateVars(c.Provider, vars)
					c.Region = interpolateVars(c.Region, vars)
					app.Cloud = &c
				}
			}
		case TokenIf:
			if err := p.parseIf(app, funcs, vars); err != nil {
				return err
			}
		case TokenFor:
			if err := p.parseFor(app, funcs, vars); err != nil {
				return err
			}
		case TokenFunc:
			if err := p.parseFunc(funcs); err != nil {
				return err
			}
		case TokenCall:
			if err := p.parseCall(app, funcs, vars); err != nil {
				return err
			}
		case TokenLet:
			if err := p.parseLet(app, vars); err != nil {
				return err
			}
		case TokenYAML:
			y, err := p.parseYAML()
			if err != nil {
				return err
			}
			applyVarsToYAML(&y, vars)
			app.YAMLs = append(app.YAMLs, y)
		default:
			return fmt.Errorf("line %d: unexpected %s (%q) in app block", tok.Line, tok.Type, tok.Value)
		}
	}
}

// parseCloud parses: cloud <provider> <region>
func (p *Parser) parseCloud() (CloudSpec, error) {
	p.advance() // consume 'cloud'

	providerTok, err := p.expect(TokenIdent)
	if err != nil {
		return CloudSpec{}, fmt.Errorf("line %d: expected provider after 'cloud'", p.peek().Line)
	}

	regionTok, err := p.expect(TokenIdent)
	if err != nil {
		return CloudSpec{}, fmt.Errorf("line %d: expected region after cloud provider", providerTok.Line)
	}

	return CloudSpec{
		Provider: providerTok.Value,
		Region:   regionTok.Value,
	}, nil
}

// parseUse parses: use <module.name>[@version] as <alias> { ... }
func (p *Parser) parseUse() (ModuleSpec, error) {
	useTok := p.advance() // consume 'use'

	// Read module name — could be dotted like "networking.vpc"
	moduleName, err := p.parseDottedIdent()
	if err != nil {
		return ModuleSpec{}, fmt.Errorf("line %d: expected module name after 'use'", useTok.Line)
	}

	// Optional version pin: @1.2.3
	version := ""
	if p.peek().Type == TokenAt {
		p.advance() // consume '@'
		verTok, err := p.expect(TokenIdent)
		if err != nil {
			return ModuleSpec{}, fmt.Errorf("line %d: expected version after '@'", p.peek().Line)
		}
		version = verTok.Value
	}

	var aliasTok Token
	if p.peek().Type == TokenAs {
		p.advance()
		aliasTok, err = p.expect(TokenIdent)
		if err != nil {
			return ModuleSpec{}, fmt.Errorf("line %d: expected alias after 'as'", p.peek().Line)
		}
	} else {
		aliasTok, err = p.expect(TokenIdent)
		if err != nil {
			return ModuleSpec{}, fmt.Errorf("line %d: expected alias after module name", p.peek().Line)
		}
	}

	if _, err := p.expect(TokenLBrace); err != nil {
		return ModuleSpec{}, fmt.Errorf("line %d: expected '{' after alias %q", aliasTok.Line, aliasTok.Value)
	}

	config := map[string]string{}
	if version != "" {
		config["version"] = version
	}

	// Parse config pairs until '}'
	for p.peek().Type != TokenRBrace {
		if p.peek().Type == TokenEOF {
			return ModuleSpec{}, fmt.Errorf("line %d: unclosed module block %q", useTok.Line, moduleName)
		}

		key, value, err := p.parseConfigPair()
		if err != nil {
			return ModuleSpec{}, err
		}
		config[key] = value
	}

	p.advance() // consume '}'

	return ModuleSpec{
		Module: moduleName,
		Alias:  aliasTok.Value,
		Config: config,
		Line:   useTok.Line,
	}, nil
}

// parseResource parses: resource <type> as <alias> { ... }
func (p *Parser) parseResource() (ResourceSpec, error) {
	resTok := p.advance() // consume 'resource'

	typeTok, err := p.expect(TokenIdent)
	if err != nil {
		return ResourceSpec{}, fmt.Errorf("line %d: expected resource type after 'resource'", resTok.Line)
	}

	var aliasTok Token
	if p.peek().Type == TokenAs {
		p.advance()
		aliasTok, err = p.expect(TokenIdent)
		if err != nil {
			return ResourceSpec{}, fmt.Errorf("line %d: expected alias after 'as'", p.peek().Line)
		}
	} else {
		aliasTok, err = p.expect(TokenIdent)
		if err != nil {
			return ResourceSpec{}, fmt.Errorf("line %d: expected alias after resource type", p.peek().Line)
		}
	}

	if _, err := p.expect(TokenLBrace); err != nil {
		return ResourceSpec{}, fmt.Errorf("line %d: expected '{' after alias %q", aliasTok.Line, aliasTok.Value)
	}

	config := map[string]string{}

	for p.peek().Type != TokenRBrace {
		if p.peek().Type == TokenEOF {
			return ResourceSpec{}, fmt.Errorf("line %d: unclosed resource block %q", resTok.Line, typeTok.Value)
		}

		key, value, err := p.parseConfigPair()
		if err != nil {
			return ResourceSpec{}, err
		}
		config[key] = value
	}

	p.advance() // consume '}'

	return ResourceSpec{
		Type:   typeTok.Value,
		Alias:  aliasTok.Value,
		Config: config,
		Line:   resTok.Line,
	}, nil
}

func (p *Parser) handleInclude() (*Spec, error) {
	p.advance() // consume 'include'
	pathTok, err := p.expect(TokenString)
	if err != nil {
		return nil, fmt.Errorf("line %d: expected string after 'include'", p.peek().Line)
	}

	incPath := pathTok.Value
	if !filepath.IsAbs(incPath) {
		incPath = filepath.Join(p.BaseDir, incPath)
	}

	return ParseFile(incPath)
}

func (p *Parser) parseIf(app *AppSpec, funcs appFunctions, vars map[string]string) error {
	ifTok := p.advance() // consume 'if'
	condParts := []string{}
	for p.peek().Type != TokenLBrace {
		if p.peek().Type == TokenEOF {
			return fmt.Errorf("line %d: expected condition and block after 'if'", ifTok.Line)
		}
		condParts = append(condParts, p.advance().Value)
	}
	if len(condParts) == 0 {
		return fmt.Errorf("line %d: expected condition after 'if'", ifTok.Line)
	}

	thenTokens, err := p.readBlockTokens()
	if err != nil {
		return err
	}

	condExpr := strings.Join(condParts, " ")
	cond := evaluateConditionExpression(condExpr, vars)
	var elseTokens []Token
	if p.peek().Type == TokenElse {
		p.advance() // consume 'else'
		elseTokens, err = p.readBlockTokens()
		if err != nil {
			return err
		}
	}

	if cond {
		return p.executeAppBlockTokens(app, funcs, vars, thenTokens)
	}
	if len(elseTokens) > 0 {
		return p.executeAppBlockTokens(app, funcs, vars, elseTokens)
	}
	return nil
}

func (p *Parser) parseFor(app *AppSpec, funcs appFunctions, vars map[string]string) error {
	forTok := p.advance() // consume 'for'
	varTok, err := p.expect(TokenIdent)
	if err != nil {
		return fmt.Errorf("line %d: expected loop variable after 'for'", forTok.Line)
	}
	if _, err := p.expect(TokenIn); err != nil {
		return fmt.Errorf("line %d: expected 'in' after loop variable", varTok.Line)
	}
	listTok := p.peek()
	if listTok.Type != TokenLBracket {
		return fmt.Errorf("line %d: expected list literal after 'in'", p.peek().Line)
	}
	p.advance()

	blockTokens, err := p.readBlockTokens()
	if err != nil {
		return err
	}

	values := parseListLiteral(listTok.Value)
	for _, raw := range values {
		childVars := cloneVars(vars)
		childVars[varTok.Value] = interpolateVars(raw, vars)
		if err := p.executeAppBlockTokens(app, funcs, childVars, blockTokens); err != nil {
			return err
		}
	}
	return nil
}

func (p *Parser) parseFunc(funcs appFunctions) error {
	funcTok := p.advance() // consume 'func'
	nameTok, err := p.expect(TokenIdent)
	if err != nil {
		return fmt.Errorf("line %d: expected function name after 'func'", funcTok.Line)
	}
	body, err := p.readBlockTokens()
	if err != nil {
		return err
	}
	funcs[nameTok.Value] = body
	return nil
}

func (p *Parser) parseCall(app *AppSpec, funcs appFunctions, vars map[string]string) error {
	callTok := p.advance() // consume 'call'
	nameTok, err := p.expect(TokenIdent)
	if err != nil {
		return fmt.Errorf("line %d: expected function name after 'call'", callTok.Line)
	}
	body, ok := funcs[nameTok.Value]
	if !ok {
		return fmt.Errorf("line %d: unknown function %q", nameTok.Line, nameTok.Value)
	}
	return p.executeAppBlockTokens(app, funcs, vars, body)
}

func (p *Parser) parseYAML() (YAMLSpec, error) {
	yTok := p.advance() // consume 'yaml'
	nameTok, err := p.expect(TokenIdent)
	if err != nil {
		return YAMLSpec{}, fmt.Errorf("line %d: expected yaml name after 'yaml'", yTok.Line)
	}
	if _, err := p.expect(TokenLBrace); err != nil {
		return YAMLSpec{}, fmt.Errorf("line %d: expected '{' after yaml name %q", nameTok.Line, nameTok.Value)
	}

	cfg := map[string]string{}
	for p.peek().Type != TokenRBrace {
		if p.peek().Type == TokenEOF {
			return YAMLSpec{}, fmt.Errorf("line %d: unclosed yaml block %q", yTok.Line, nameTok.Value)
		}
		k, v, err := p.parseConfigPair()
		if err != nil {
			return YAMLSpec{}, err
		}
		cfg[k] = v
	}
	p.advance() // consume '}'
	return YAMLSpec{
		Name:   nameTok.Value,
		Config: cfg,
		Line:   yTok.Line,
	}, nil
}

func (p *Parser) parseLet(app *AppSpec, vars map[string]string) error {
	letTok := p.advance() // consume 'let'
	nameTok, err := p.expect(TokenIdent)
	if err != nil {
		return fmt.Errorf("line %d: expected variable name after 'let'", letTok.Line)
	}
	if p.peek().Type == TokenIdent && p.peek().Value == "=" {
		p.advance()
	}
	val, err := p.parseValue()
	if err != nil {
		return fmt.Errorf("line %d: expected value after variable %q", letTok.Line, nameTok.Value)
	}
	val = interpolateVars(val, vars)
	vars[nameTok.Value] = val
	app.Params[nameTok.Value] = val
	return nil
}

func (p *Parser) readBlockTokens() ([]Token, error) {
	openTok, err := p.expect(TokenLBrace)
	if err != nil {
		return nil, fmt.Errorf("line %d: expected '{' to start block", p.peek().Line)
	}
	start := p.pos
	depth := 1
	for p.pos < len(p.tokens) {
		tok := p.advance()
		switch tok.Type {
		case TokenLBrace:
			depth++
		case TokenRBrace:
			depth--
			if depth == 0 {
				end := p.pos - 1
				return append([]Token{}, p.tokens[start:end]...), nil
			}
		}
	}
	return nil, fmt.Errorf("line %d: unclosed block", openTok.Line)
}

func (p *Parser) executeAppBlockTokens(app *AppSpec, funcs appFunctions, vars map[string]string, body []Token) error {
	toks := append(append([]Token{}, body...), Token{Type: TokenEOF})
	sub := &Parser{tokens: toks, BaseDir: p.BaseDir}
	return sub.parseAppBody(app, funcs, vars, false)
}

// parseVars parses: vars { <key> <value> ... }
func (p *Parser) parseParams() (map[string]string, error) {
	p.advance() // consume 'params'

	if _, err := p.expect(TokenLBrace); err != nil {
		return nil, fmt.Errorf("line %d: expected '{' after 'params'", p.peek().Line)
	}

	params := map[string]string{}
	for p.peek().Type != TokenRBrace {
		if p.peek().Type == TokenEOF {
			return nil, fmt.Errorf("line %d: unclosed params block", p.peek().Line)
		}

		key, value, err := p.parseConfigPair()
		if err != nil {
			return nil, err
		}
		params[key] = value
	}

	p.advance() // consume '}'
	return params, nil
}

// parseDottedIdent reads an identifier (the lexer already joins dotted names).
func (p *Parser) parseDottedIdent() (string, error) {
	tok, err := p.expect(TokenIdent)
	if err != nil {
		return "", err
	}
	return tok.Value, nil
}

// parseConfigPair reads: <key> <value>
func (p *Parser) parseConfigPair() (string, string, error) {
	keyTok, err := p.expect(TokenIdent)
	if err != nil {
		return "", "", fmt.Errorf("line %d: expected config key", p.peek().Line)
	}

	if p.peek().Type == TokenIdent && p.peek().Value == "=" {
		p.advance()
	}

	valTok := p.peek()
	switch valTok.Type {
	case TokenString:
		p.advance()
		return keyTok.Value, valTok.Value, nil
	case TokenNumber:
		p.advance()
		return keyTok.Value, valTok.Value, nil
	case TokenBool:
		p.advance()
		return keyTok.Value, valTok.Value, nil
	case TokenIdent:
		p.advance()
		return keyTok.Value, valTok.Value, nil
	case TokenLBracket:
		// Array literal — lexer read the whole [...] as one token
		p.advance()
		return keyTok.Value, valTok.Value, nil
	default:
		return "", "", fmt.Errorf("line %d: expected value after key %q, got %s (%q)",
			valTok.Line, keyTok.Value, valTok.Type, valTok.Value)
	}
}

func (p *Parser) parseValue() (string, error) {
	valTok := p.peek()
	switch valTok.Type {
	case TokenString, TokenNumber, TokenBool, TokenIdent, TokenLBracket:
		p.advance()
		return valTok.Value, nil
	default:
		return "", fmt.Errorf("line %d: expected value token, got %s (%q)", valTok.Line, valTok.Type, valTok.Value)
	}
}

// FormatError returns a human-readable parse error with context.
func FormatError(source string, err error) string {
	if err == nil {
		return ""
	}
	lines := strings.Split(source, "\n")
	_ = lines
	return err.Error()
}

func applyVarsToModule(mod *ModuleSpec, vars map[string]string) {
	mod.Module = interpolateVars(mod.Module, vars)
	mod.Alias = interpolateVars(mod.Alias, vars)
	for k, v := range mod.Config {
		mod.Config[k] = interpolateVars(v, vars)
	}
}

func applyVarsToResource(res *ResourceSpec, vars map[string]string) {
	res.Type = interpolateVars(res.Type, vars)
	res.Alias = interpolateVars(res.Alias, vars)
	for k, v := range res.Config {
		res.Config[k] = interpolateVars(v, vars)
	}
}

func applyVarsToYAML(y *YAMLSpec, vars map[string]string) {
	y.Name = interpolateVars(y.Name, vars)
	for k, v := range y.Config {
		y.Config[k] = interpolateVars(v, vars)
	}
}

func interpolateVars(value string, vars map[string]string) string {
	out := value
	for k, v := range vars {
		out = strings.ReplaceAll(out, "${"+k+"}", v)
		out = strings.ReplaceAll(out, "${loop."+k+"}", v)
	}
	return out
}

func evaluateConditionExpression(expr string, vars map[string]string) bool {
	normalized := strings.TrimSpace(interpolateVars(expr, vars))
	if normalized == "" {
		return false
	}
	return evalOr(normalized, vars)
}

func evalOr(expr string, vars map[string]string) bool {
	parts := strings.Split(expr, "||")
	for _, part := range parts {
		if evalAnd(strings.TrimSpace(part), vars) {
			return true
		}
	}
	return false
}

func evalAnd(expr string, vars map[string]string) bool {
	parts := strings.Split(expr, "&&")
	for _, part := range parts {
		if !evalComparison(strings.TrimSpace(part), vars) {
			return false
		}
	}
	return true
}

func evalComparison(expr string, vars map[string]string) bool {
	e := strings.TrimSpace(strings.Trim(expr, "()"))
	for _, op := range []string{"==", "!=", ">=", "<=", ">", "<"} {
		if idx := strings.Index(e, op); idx >= 0 {
			left := strings.TrimSpace(e[:idx])
			right := strings.TrimSpace(e[idx+len(op):])
			return compareValues(resolveValue(left, vars), resolveValue(right, vars), op)
		}
	}
	if strings.HasPrefix(e, "!") {
		return !truthy(resolveValue(strings.TrimSpace(e[1:]), vars))
	}
	return truthy(resolveValue(e, vars))
}

func resolveValue(raw string, vars map[string]string) string {
	raw = strings.TrimSpace(strings.Trim(raw, "()"))
	if v, ok := vars[raw]; ok {
		return v
	}
	return strings.Trim(raw, "\"")
}

func compareValues(left, right, op string) bool {
	ln, lok := parseFloat(left)
	rn, rok := parseFloat(right)
	if lok && rok {
		switch op {
		case "==":
			return ln == rn
		case "!=":
			return ln != rn
		case ">":
			return ln > rn
		case "<":
			return ln < rn
		case ">=":
			return ln >= rn
		case "<=":
			return ln <= rn
		}
	}
	lb, lbok := parseBool(left)
	rb, rbok := parseBool(right)
	if lbok && rbok {
		switch op {
		case "==":
			return lb == rb
		case "!=":
			return lb != rb
		default:
			return false
		}
	}
	switch op {
	case "==":
		return left == right
	case "!=":
		return left != right
	case ">":
		return left > right
	case "<":
		return left < right
	case ">=":
		return left >= right
	case "<=":
		return left <= right
	default:
		return false
	}
}

func truthy(value string) bool {
	v := strings.TrimSpace(strings.ToLower(value))
	switch v {
	case "", "false", "0", "no", "off", "null":
		return false
	default:
		return true
	}
}

func parseFloat(v string) (float64, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	var seenDot bool
	start := 0
	if v[0] == '-' {
		start = 1
	}
	if start == len(v) {
		return 0, false
	}
	for i := start; i < len(v); i++ {
		c := v[i]
		if c == '.' {
			if seenDot {
				return 0, false
			}
			seenDot = true
			continue
		}
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	var num float64
	for i := start; i < len(v); i++ {
		if v[i] == '.' {
			continue
		}
		num = num*10 + float64(v[i]-'0')
	}
	if seenDot {
		dot := strings.IndexByte(v, '.')
		decimals := len(v) - dot - 1
		div := 1.0
		for i := 0; i < decimals; i++ {
			div *= 10
		}
		num = num / div
	}
	if v[0] == '-' {
		num = -num
	}
	return num, true
}

func parseBool(v string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true":
		return true, true
	case "false":
		return false, true
	default:
		return false, false
	}
}

func parseListLiteral(lit string) []string {
	trimmed := strings.TrimSpace(lit)
	trimmed = strings.TrimPrefix(trimmed, "[")
	trimmed = strings.TrimSuffix(trimmed, "]")
	if strings.TrimSpace(trimmed) == "" {
		return []string{}
	}

	parts := strings.Split(trimmed, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		v := strings.TrimSpace(p)
		v = strings.Trim(v, "\"")
		if v == "" {
			continue
		}
		out = append(out, v)
	}
	return out
}

func cloneVars(vars map[string]string) map[string]string {
	out := make(map[string]string, len(vars))
	for k, v := range vars {
		out[k] = v
	}
	return out
}
