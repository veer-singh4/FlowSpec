package parser

import (
	"fmt"
	"strings"
	"unicode"
)

// TokenType represents the type of a lexical token.
type TokenType int

const (
	TokenEOF TokenType = iota
	TokenApp
	TokenCloud
	TokenUse
	TokenAs
	TokenResource
	TokenIdent
	TokenString
	TokenNumber
	TokenBool
	TokenLBrace
	TokenRBrace
	TokenLBracket
	TokenRBracket
	TokenComma
	TokenDot
	TokenAt
	TokenNewline
	TokenParam
	TokenInclude
	TokenIf
	TokenElse
	TokenFor
	TokenIn
	TokenFunc
	TokenCall
	TokenLet
	TokenYAML
)

func (t TokenType) String() string {
	switch t {
	case TokenEOF:
		return "EOF"
	case TokenApp:
		return "APP"
	case TokenCloud:
		return "CLOUD"
	case TokenUse:
		return "USE"
	case TokenAs:
		return "AS"
	case TokenResource:
		return "RESOURCE"
	case TokenIdent:
		return "IDENT"
	case TokenString:
		return "STRING"
	case TokenNumber:
		return "NUMBER"
	case TokenBool:
		return "BOOL"
	case TokenLBrace:
		return "LBRACE"
	case TokenRBrace:
		return "RBRACE"
	case TokenLBracket:
		return "LBRACKET"
	case TokenRBracket:
		return "RBRACKET"
	case TokenComma:
		return "COMMA"
	case TokenDot:
		return "DOT"
	case TokenAt:
		return "AT"
	case TokenNewline:
		return "NEWLINE"
	case TokenParam:
		return "PARAMS"
	case TokenInclude:
		return "INCLUDE"
	case TokenIf:
		return "IF"
	case TokenElse:
		return "ELSE"
	case TokenFor:
		return "FOR"
	case TokenIn:
		return "IN"
	case TokenFunc:
		return "FUNC"
	case TokenCall:
		return "CALL"
	case TokenLet:
		return "LET"
	case TokenYAML:
		return "YAML"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", int(t))
	}
}

// Token represents a single lexical token.
type Token struct {
	Type    TokenType
	Value   string
	Line    int
	Col     int
}

func (t Token) String() string {
	return fmt.Sprintf("Token(%s, %q, line=%d, col=%d)", t.Type, t.Value, t.Line, t.Col)
}

// Lexer tokenizes a UniFlow (.ufl) source string.
type Lexer struct {
	input  []rune
	pos    int
	line   int
	col    int
	tokens []Token
}

// NewLexer creates a new lexer for the given source.
func NewLexer(source string) *Lexer {
	return &Lexer{
		input: []rune(source),
		pos:   0,
		line:  1,
		col:   1,
	}
}

// Tokenize processes the entire input and returns all tokens.
func (l *Lexer) Tokenize() ([]Token, error) {
	for {
		tok, err := l.next()
		if err != nil {
			return nil, err
		}
		// Skip newlines in the token stream — they're only whitespace in UniFlow.
		if tok.Type == TokenNewline {
			continue
		}
		l.tokens = append(l.tokens, tok)
		if tok.Type == TokenEOF {
			break
		}
	}
	return l.tokens, nil
}

func (l *Lexer) peek() rune {
	if l.pos >= len(l.input) {
		return 0
	}
	return l.input[l.pos]
}

func (l *Lexer) advance() rune {
	if l.pos >= len(l.input) {
		return 0
	}
	ch := l.input[l.pos]
	l.pos++
	if ch == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return ch
}

func (l *Lexer) skipWhitespace() {
	for l.pos < len(l.input) {
		ch := l.input[l.pos]
		if ch == ' ' || ch == '\t' || ch == '\r' {
			l.advance()
		} else {
			break
		}
	}
}

func (l *Lexer) skipComment() {
	for l.pos < len(l.input) && l.input[l.pos] != '\n' {
		l.advance()
	}
}

func (l *Lexer) next() (Token, error) {
	l.skipWhitespace()

	if l.pos >= len(l.input) {
		return Token{Type: TokenEOF, Value: "", Line: l.line, Col: l.col}, nil
	}

	ch := l.peek()
	line, col := l.line, l.col

	// Comments
	if ch == '#' {
		l.skipComment()
		return l.next()
	}

	// Newline
	if ch == '\n' {
		l.advance()
		return Token{Type: TokenNewline, Value: "\n", Line: line, Col: col}, nil
	}

	// Single-character tokens
	switch ch {
	case '{':
		l.advance()
		return Token{Type: TokenLBrace, Value: "{", Line: line, Col: col}, nil
	case '}':
		l.advance()
		return Token{Type: TokenRBrace, Value: "}", Line: line, Col: col}, nil
	case '[':
		// Read the entire array literal as one token value
		return l.readArrayLiteral(line, col)
	case ',':
		l.advance()
		return Token{Type: TokenComma, Value: ",", Line: line, Col: col}, nil
	case '@':
		l.advance()
		return Token{Type: TokenAt, Value: "@", Line: line, Col: col}, nil
	}

	// Quoted string
	if ch == '"' {
		return l.readString(line, col)
	}

	// Number (digits or leading -)
	if unicode.IsDigit(ch) || (ch == '-' && l.pos+1 < len(l.input) && unicode.IsDigit(l.input[l.pos+1])) {
		return l.readNumber(line, col)
	}

	// Identifier or keyword
	if isIdentStart(ch) {
		return l.readIdentOrKeyword(line, col)
	}

	// CIDR-like or IP-like or path-like values that start with digit are handled above.
	// Anything else that looks like a bare value (e.g. ami-0f5ee..., 10.10.0.0/16)
	if isValueChar(ch) {
		return l.readBareValue(line, col)
	}

	return Token{}, fmt.Errorf("line %d col %d: unexpected character %q", line, col, string(ch))
}

// readString reads a double-quoted string (no escape handling needed for MVP).
func (l *Lexer) readString(line, col int) (Token, error) {
	l.advance() // skip opening "
	var buf strings.Builder
	for l.pos < len(l.input) {
		ch := l.peek()
		if ch == '"' {
			l.advance() // skip closing "
			return Token{Type: TokenString, Value: buf.String(), Line: line, Col: col}, nil
		}
		if ch == '\n' {
			return Token{}, fmt.Errorf("line %d col %d: unterminated string", line, col)
		}
		buf.WriteRune(l.advance())
	}
	return Token{}, fmt.Errorf("line %d col %d: unterminated string at end of file", line, col)
}

// readNumber reads a numeric literal or a CIDR/IP/complex bare value that starts with a digit.
func (l *Lexer) readNumber(line, col int) (Token, error) {
	var buf strings.Builder
	if l.peek() == '-' {
		buf.WriteRune(l.advance())
	}
	for l.pos < len(l.input) && (unicode.IsDigit(l.peek()) || l.peek() == '.') {
		buf.WriteRune(l.advance())
	}
	// If followed by '/' or letters, this is a bare value (e.g. CIDR 10.10.0.0/16, version 5.0.1-beta)
	if l.pos < len(l.input) && (l.peek() == '/' || unicode.IsLetter(l.peek()) || l.peek() == '-') {
		for l.pos < len(l.input) && isBareValueChar(l.peek()) {
			buf.WriteRune(l.advance())
		}
		return Token{Type: TokenIdent, Value: buf.String(), Line: line, Col: col}, nil
	}
	return Token{Type: TokenNumber, Value: buf.String(), Line: line, Col: col}, nil
}

// readIdentOrKeyword reads an identifier and checks for keywords.
func (l *Lexer) readIdentOrKeyword(line, col int) (Token, error) {
	var buf strings.Builder
	for l.pos < len(l.input) && isIdentChar(l.peek()) {
		buf.WriteRune(l.advance())
	}
	word := buf.String()

	switch word {
	case "app":
		return Token{Type: TokenApp, Value: word, Line: line, Col: col}, nil
	case "cloud":
		return Token{Type: TokenCloud, Value: word, Line: line, Col: col}, nil
	case "use":
		return Token{Type: TokenUse, Value: word, Line: line, Col: col}, nil
	case "as":
		return Token{Type: TokenAs, Value: word, Line: line, Col: col}, nil
	case "resource":
		return Token{Type: TokenResource, Value: word, Line: line, Col: col}, nil
	case "params":
		return Token{Type: TokenParam, Value: word, Line: line, Col: col}, nil
	case "include":
		return Token{Type: TokenInclude, Value: word, Line: line, Col: col}, nil
	case "if":
		return Token{Type: TokenIf, Value: word, Line: line, Col: col}, nil
	case "else":
		return Token{Type: TokenElse, Value: word, Line: line, Col: col}, nil
	case "for":
		return Token{Type: TokenFor, Value: word, Line: line, Col: col}, nil
	case "in":
		return Token{Type: TokenIn, Value: word, Line: line, Col: col}, nil
	case "func":
		return Token{Type: TokenFunc, Value: word, Line: line, Col: col}, nil
	case "call":
		return Token{Type: TokenCall, Value: word, Line: line, Col: col}, nil
	case "let":
		return Token{Type: TokenLet, Value: word, Line: line, Col: col}, nil
	case "yaml":
		return Token{Type: TokenYAML, Value: word, Line: line, Col: col}, nil
	case "true", "false":
		return Token{Type: TokenBool, Value: word, Line: line, Col: col}, nil
	default:
		return Token{Type: TokenIdent, Value: word, Line: line, Col: col}, nil
	}
}

// readBareValue reads unquoted values like ami-0f5ee..., CIDR blocks, etc.
func (l *Lexer) readBareValue(line, col int) (Token, error) {
	var buf strings.Builder
	for l.pos < len(l.input) && isBareValueChar(l.peek()) {
		buf.WriteRune(l.advance())
	}
	return Token{Type: TokenIdent, Value: buf.String(), Line: line, Col: col}, nil
}

// readArrayLiteral reads an entire [...] array as a single token.
func (l *Lexer) readArrayLiteral(line, col int) (Token, error) {
	var buf strings.Builder
	depth := 0
	for l.pos < len(l.input) {
		ch := l.peek()
		if ch == '[' {
			depth++
		} else if ch == ']' {
			depth--
		}
		buf.WriteRune(l.advance())
		if depth == 0 {
			return Token{Type: TokenLBracket, Value: buf.String(), Line: line, Col: col}, nil
		}
	}
	return Token{}, fmt.Errorf("line %d col %d: unterminated array literal", line, col)
}

func isIdentStart(ch rune) bool {
	return unicode.IsLetter(ch) || ch == '_'
}

func isIdentChar(ch rune) bool {
	return unicode.IsLetter(ch) || unicode.IsDigit(ch) || ch == '_' || ch == '-' || ch == '.'
}

func isValueChar(ch rune) bool {
	return unicode.IsLetter(ch) || unicode.IsDigit(ch) || ch == '_' || ch == '-' || ch == '.' || ch == '/' || ch == ':' || ch == '$' || ch == '{' || ch == '}' || ch == '=' || ch == '!' || ch == '>' || ch == '<' || ch == '&' || ch == '|' || ch == '(' || ch == ')'
}

func isBareValueChar(ch rune) bool {
	return unicode.IsLetter(ch) || unicode.IsDigit(ch) || ch == '_' || ch == '-' || ch == '.' || ch == '/' || ch == ':' || ch == '$' || ch == '{' || ch == '}' || ch == '=' || ch == '!' || ch == '>' || ch == '<' || ch == '&' || ch == '|' || ch == '(' || ch == ')'
}
