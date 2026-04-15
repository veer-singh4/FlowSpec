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

type AppSpec struct {
	Name      string
	Cloud     *CloudSpec
	Modules   []ModuleSpec
	Resources []ResourceSpec
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
		Params:    map[string]string{},
		Line:      appTok.Line,
}

	// Parse app body until '}'
	for p.peek().Type != TokenRBrace {
		if p.peek().Type == TokenEOF {
			return AppSpec{}, fmt.Errorf("line %d: unclosed app block %q", appTok.Line, nameTok.Value)
		}

		switch p.peek().Type {
		case TokenCloud:
			cloud, err := p.parseCloud()
			if err != nil {
				return AppSpec{}, err
			}
			app.Cloud = &cloud
		case TokenUse:
			mod, err := p.parseUse()
			if err != nil {
				return AppSpec{}, err
			}
			app.Modules = append(app.Modules, mod)
		case TokenResource:
			res, err := p.parseResource()
			if err != nil {
				return AppSpec{}, err
			}
			app.Resources = append(app.Resources, res)
		case TokenParam:
			params, err := p.parseParams()
			if err != nil {
				return AppSpec{}, err
			}
			for k, v := range params {
				app.Params[k] = v
			}
		case TokenInclude:
			incSpec, err := p.handleInclude()
			if err != nil {
				return AppSpec{}, err
			}
			// For includes inside an app, we only take resources and modules
			for _, ia := range incSpec.Apps {
				app.Modules = append(app.Modules, ia.Modules...)
				app.Resources = append(app.Resources, ia.Resources...)
				for k, v := range ia.Params {
					app.Params[k] = v
				}
			}
		default:
			tok := p.peek()
			return AppSpec{}, fmt.Errorf("line %d: unexpected %s (%q) in app block", tok.Line, tok.Type, tok.Value)
		}
	}

	p.advance() // consume '}'
	return app, nil
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

	// "as" keyword
	if _, err := p.expect(TokenAs); err != nil {
		return ModuleSpec{}, fmt.Errorf("line %d: expected 'as' after module name %q", p.peek().Line, moduleName)
	}

	aliasTok, err := p.expect(TokenIdent)
	if err != nil {
		return ModuleSpec{}, fmt.Errorf("line %d: expected alias after 'as'", p.peek().Line)
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

	if _, err := p.expect(TokenAs); err != nil {
		return ResourceSpec{}, fmt.Errorf("line %d: expected 'as' after resource type %q", typeTok.Line, typeTok.Value)
	}

	aliasTok, err := p.expect(TokenIdent)
	if err != nil {
		return ResourceSpec{}, fmt.Errorf("line %d: expected alias after 'as'", p.peek().Line)
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

// FormatError returns a human-readable parse error with context.
func FormatError(source string, err error) string {
	if err == nil {
		return ""
	}
	lines := strings.Split(source, "\n")
	_ = lines
	return err.Error()
}
