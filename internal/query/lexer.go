package query

import (
	"fmt"
	"strings"
	"unicode"
)

// TokenType represents the type of a lexer token
type TokenType int

const (
	// Special tokens
	TokenEOF TokenType = iota
	TokenError

	// Literals
	TokenIdent  // field names, function names
	TokenString // "quoted" or 'quoted' strings
	TokenNumber // integers
	TokenDate   // 2024-01-15

	// Operators
	TokenEq          // =
	TokenNeq         // !=
	TokenLt          // <
	TokenGt          // >
	TokenLte         // <=
	TokenGte         // >=
	TokenContains    // ~
	TokenNotContains // !~

	// Boolean operators
	TokenAnd // AND, &&
	TokenOr  // OR, ||
	TokenNot // NOT, !, -

	// Delimiters
	TokenLParen // (
	TokenRParen // )
	TokenComma  // ,
	TokenDot    // .

	// Special values
	TokenAtMe  // @me
	TokenEmpty // EMPTY
	TokenNull  // NULL

	// Sort clause
	TokenSort // sort:field or sort:-field
)

var tokenNames = map[TokenType]string{
	TokenEOF:         "EOF",
	TokenError:       "ERROR",
	TokenIdent:       "IDENT",
	TokenString:      "STRING",
	TokenNumber:      "NUMBER",
	TokenDate:        "DATE",
	TokenEq:          "=",
	TokenNeq:         "!=",
	TokenLt:          "<",
	TokenGt:          ">",
	TokenLte:         "<=",
	TokenGte:         ">=",
	TokenContains:    "~",
	TokenNotContains: "!~",
	TokenAnd:         "AND",
	TokenOr:          "OR",
	TokenNot:         "NOT",
	TokenLParen:      "(",
	TokenRParen:      ")",
	TokenComma:       ",",
	TokenDot:         ".",
	TokenAtMe:        "@me",
	TokenEmpty:       "EMPTY",
	TokenNull:        "NULL",
	TokenSort:        "SORT",
}

func (t TokenType) String() string {
	if name, ok := tokenNames[t]; ok {
		return name
	}
	return fmt.Sprintf("Token(%d)", t)
}

// Token represents a lexer token
type Token struct {
	Type   TokenType
	Value  string
	Pos    int // position in input
	Line   int
	Column int
}

func (t Token) String() string {
	if t.Value != "" {
		return fmt.Sprintf("%s(%q)", t.Type, t.Value)
	}
	return t.Type.String()
}

// Lexer tokenizes TDQ query strings
type Lexer struct {
	input  string
	pos    int
	line   int
	column int
	tokens []Token
}

// NewLexer creates a new lexer for the given input
func NewLexer(input string) *Lexer {
	return &Lexer{
		input:  input,
		pos:    0,
		line:   1,
		column: 1,
	}
}

// Tokenize returns all tokens from the input
func (l *Lexer) Tokenize() ([]Token, error) {
	l.tokens = nil
	for {
		tok := l.nextToken()
		l.tokens = append(l.tokens, tok)
		if tok.Type == TokenEOF {
			break
		}
		if tok.Type == TokenError {
			return l.tokens, fmt.Errorf("lexer error at line %d, column %d: %s", tok.Line, tok.Column, tok.Value)
		}
	}
	return l.tokens, nil
}

func (l *Lexer) nextToken() Token {
	l.skipWhitespace()

	if l.pos >= len(l.input) {
		return Token{Type: TokenEOF, Pos: l.pos, Line: l.line, Column: l.column}
	}

	// Strip shell escape backslashes before operator characters
	// Agents often escape ! as \! to avoid bash history expansion
	if l.pos < len(l.input) && l.input[l.pos] == '\\' {
		if l.pos+1 < len(l.input) {
			next := l.input[l.pos+1]
			if next == '!' || next == '<' || next == '>' || next == '=' || next == '~' {
				l.advance() // skip the backslash
			}
		}
	}

	startPos := l.pos
	startLine := l.line
	startCol := l.column
	ch := l.input[l.pos]

	// Single character tokens
	switch ch {
	case '(':
		l.advance()
		return Token{Type: TokenLParen, Value: "(", Pos: startPos, Line: startLine, Column: startCol}
	case ')':
		l.advance()
		return Token{Type: TokenRParen, Value: ")", Pos: startPos, Line: startLine, Column: startCol}
	case ',':
		l.advance()
		return Token{Type: TokenComma, Value: ",", Pos: startPos, Line: startLine, Column: startCol}
	case '.':
		l.advance()
		return Token{Type: TokenDot, Value: ".", Pos: startPos, Line: startLine, Column: startCol}
	case '~':
		l.advance()
		return Token{Type: TokenContains, Value: "~", Pos: startPos, Line: startLine, Column: startCol}
	case '=':
		l.advance()
		return Token{Type: TokenEq, Value: "=", Pos: startPos, Line: startLine, Column: startCol}
	}

	// Two-character operators
	if l.pos+1 < len(l.input) {
		two := l.input[l.pos : l.pos+2]
		switch two {
		case "!=":
			l.advance()
			l.advance()
			return Token{Type: TokenNeq, Value: "!=", Pos: startPos, Line: startLine, Column: startCol}
		case "!~":
			l.advance()
			l.advance()
			return Token{Type: TokenNotContains, Value: "!~", Pos: startPos, Line: startLine, Column: startCol}
		case "<=":
			l.advance()
			l.advance()
			return Token{Type: TokenLte, Value: "<=", Pos: startPos, Line: startLine, Column: startCol}
		case ">=":
			l.advance()
			l.advance()
			return Token{Type: TokenGte, Value: ">=", Pos: startPos, Line: startLine, Column: startCol}
		case "&&":
			l.advance()
			l.advance()
			return Token{Type: TokenAnd, Value: "&&", Pos: startPos, Line: startLine, Column: startCol}
		case "||":
			l.advance()
			l.advance()
			return Token{Type: TokenOr, Value: "||", Pos: startPos, Line: startLine, Column: startCol}
		}
	}

	// Single-char operators that need checking after two-char
	switch ch {
	case '<':
		l.advance()
		return Token{Type: TokenLt, Value: "<", Pos: startPos, Line: startLine, Column: startCol}
	case '>':
		l.advance()
		return Token{Type: TokenGt, Value: ">", Pos: startPos, Line: startLine, Column: startCol}
	case '!':
		l.advance()
		return Token{Type: TokenNot, Value: "!", Pos: startPos, Line: startLine, Column: startCol}
	case ':':
		// Legacy syntax: field:value (treat ':' as '=')
		l.advance()
		return Token{Type: TokenEq, Value: ":", Pos: startPos, Line: startLine, Column: startCol}
	}

	// Quoted strings
	if ch == '"' || ch == '\'' {
		return l.scanString(ch)
	}

	// @me special value
	if ch == '@' {
		return l.scanAtValue()
	}

	// - can be NOT or part of a number/date/relative-date
	if ch == '-' {
		return l.scanDashOrNegative()
	}

	// + for relative dates like +7d
	if ch == '+' {
		return l.scanRelativeDate('+')
	}

	// Numbers or dates
	if unicode.IsDigit(rune(ch)) {
		return l.scanNumberOrDate()
	}

	// Identifiers and keywords
	if isIdentStart(ch) {
		return l.scanIdentOrKeyword()
	}

	// Unknown character
	l.advance()
	return Token{
		Type:   TokenError,
		Value:  fmt.Sprintf("unexpected character: %q", ch),
		Pos:    startPos,
		Line:   startLine,
		Column: startCol,
	}
}

func (l *Lexer) advance() {
	if l.pos < len(l.input) {
		if l.input[l.pos] == '\n' {
			l.line++
			l.column = 1
		} else {
			l.column++
		}
		l.pos++
	}
}

func (l *Lexer) skipWhitespace() {
	for l.pos < len(l.input) && unicode.IsSpace(rune(l.input[l.pos])) {
		l.advance()
	}
}

func (l *Lexer) scanString(quote byte) Token {
	startPos := l.pos
	startLine := l.line
	startCol := l.column
	l.advance() // skip opening quote

	var sb strings.Builder
	for l.pos < len(l.input) {
		ch := l.input[l.pos]
		if ch == quote {
			l.advance() // skip closing quote
			return Token{
				Type:   TokenString,
				Value:  sb.String(),
				Pos:    startPos,
				Line:   startLine,
				Column: startCol,
			}
		}
		if ch == '\\' && l.pos+1 < len(l.input) {
			l.advance()
			// Handle escape sequences
			switch l.input[l.pos] {
			case 'n':
				sb.WriteByte('\n')
			case 't':
				sb.WriteByte('\t')
			case '\\':
				sb.WriteByte('\\')
			case '"':
				sb.WriteByte('"')
			case '\'':
				sb.WriteByte('\'')
			default:
				sb.WriteByte(l.input[l.pos])
			}
			l.advance()
			continue
		}
		sb.WriteByte(ch)
		l.advance()
	}

	return Token{
		Type:   TokenError,
		Value:  "unterminated string",
		Pos:    startPos,
		Line:   startLine,
		Column: startCol,
	}
}

func (l *Lexer) scanAtValue() Token {
	startPos := l.pos
	startLine := l.line
	startCol := l.column
	l.advance() // skip @

	var sb strings.Builder
	sb.WriteByte('@')
	for l.pos < len(l.input) && isIdentChar(l.input[l.pos]) {
		sb.WriteByte(l.input[l.pos])
		l.advance()
	}

	value := sb.String()
	if value == "@me" {
		return Token{Type: TokenAtMe, Value: value, Pos: startPos, Line: startLine, Column: startCol}
	}

	return Token{
		Type:   TokenError,
		Value:  fmt.Sprintf("unknown special value: %s", value),
		Pos:    startPos,
		Line:   startLine,
		Column: startCol,
	}
}

func (l *Lexer) scanDashOrNegative() Token {
	startPos := l.pos
	startLine := l.line
	startCol := l.column

	// Look ahead to determine what follows
	if l.pos+1 < len(l.input) {
		next := l.input[l.pos+1]
		// -7d, -2w, -1m, -3h (relative dates)
		if unicode.IsDigit(rune(next)) {
			return l.scanRelativeDate('-')
		}
	}

	// Standalone - is NOT operator
	l.advance()
	return Token{Type: TokenNot, Value: "-", Pos: startPos, Line: startLine, Column: startCol}
}

func (l *Lexer) scanRelativeDate(sign byte) Token {
	startPos := l.pos
	startLine := l.line
	startCol := l.column
	l.advance() // skip + or -

	var sb strings.Builder
	sb.WriteByte(sign)

	// Scan digits
	for l.pos < len(l.input) && unicode.IsDigit(rune(l.input[l.pos])) {
		sb.WriteByte(l.input[l.pos])
		l.advance()
	}

	// Expect unit: d, w, m, h
	if l.pos < len(l.input) {
		unit := l.input[l.pos]
		if unit == 'd' || unit == 'w' || unit == 'm' || unit == 'h' {
			sb.WriteByte(unit)
			l.advance()
			return Token{
				Type:   TokenDate,
				Value:  sb.String(),
				Pos:    startPos,
				Line:   startLine,
				Column: startCol,
			}
		}
	}

	// If no valid unit, it's an error for + but could be a number for -
	if sign == '+' {
		return Token{
			Type:   TokenError,
			Value:  fmt.Sprintf("invalid relative date: %s (expected d, w, m, or h suffix)", sb.String()),
			Pos:    startPos,
			Line:   startLine,
			Column: startCol,
		}
	}

	// For -, return as negative number
	return Token{
		Type:   TokenNumber,
		Value:  sb.String(),
		Pos:    startPos,
		Line:   startLine,
		Column: startCol,
	}
}

func (l *Lexer) scanNumberOrDate() Token {
	startPos := l.pos
	startLine := l.line
	startCol := l.column

	var sb strings.Builder
	for l.pos < len(l.input) && (unicode.IsDigit(rune(l.input[l.pos])) || l.input[l.pos] == '-') {
		sb.WriteByte(l.input[l.pos])
		l.advance()
	}

	value := sb.String()

	// Check if it looks like a date (YYYY-MM-DD)
	if len(value) == 10 && value[4] == '-' && value[7] == '-' {
		return Token{Type: TokenDate, Value: value, Pos: startPos, Line: startLine, Column: startCol}
	}

	// Check for relative date suffix (e.g., 7d would have been caught if preceded by - or +)
	// This handles bare numbers that might have a suffix
	if l.pos < len(l.input) {
		suffix := l.input[l.pos]
		if suffix == 'd' || suffix == 'w' || suffix == 'm' || suffix == 'h' {
			sb.WriteByte(suffix)
			l.advance()
			return Token{Type: TokenDate, Value: sb.String(), Pos: startPos, Line: startLine, Column: startCol}
		}
	}

	return Token{Type: TokenNumber, Value: value, Pos: startPos, Line: startLine, Column: startCol}
}

func (l *Lexer) scanIdentOrKeyword() Token {
	startPos := l.pos
	startLine := l.line
	startCol := l.column

	var sb strings.Builder
	for l.pos < len(l.input) && isIdentChar(l.input[l.pos]) {
		sb.WriteByte(l.input[l.pos])
		l.advance()
	}

	value := sb.String()
	upper := strings.ToUpper(value)

	// Check for sort: prefix
	if strings.ToLower(value) == "sort" && l.pos < len(l.input) && l.input[l.pos] == ':' {
		return l.scanSortClause(startPos, startLine, startCol)
	}

	// Check for keywords
	switch upper {
	case "AND":
		return Token{Type: TokenAnd, Value: value, Pos: startPos, Line: startLine, Column: startCol}
	case "OR":
		return Token{Type: TokenOr, Value: value, Pos: startPos, Line: startLine, Column: startCol}
	case "NOT":
		return Token{Type: TokenNot, Value: value, Pos: startPos, Line: startLine, Column: startCol}
	case "EMPTY":
		return Token{Type: TokenEmpty, Value: value, Pos: startPos, Line: startLine, Column: startCol}
	case "NULL":
		return Token{Type: TokenNull, Value: value, Pos: startPos, Line: startLine, Column: startCol}
	}

	// Check for relative date keywords
	switch strings.ToLower(value) {
	case "today", "yesterday", "this_week", "last_week", "this_month", "last_month":
		return Token{Type: TokenDate, Value: strings.ToLower(value), Pos: startPos, Line: startLine, Column: startCol}
	}

	return Token{Type: TokenIdent, Value: value, Pos: startPos, Line: startLine, Column: startCol}
}

// scanSortClause parses sort:field or sort:-field
// Value format: "field" for ascending, "-field" for descending
func (l *Lexer) scanSortClause(startPos, startLine, startCol int) Token {
	l.advance() // skip ':'

	var sb strings.Builder

	// Check for descending prefix
	if l.pos < len(l.input) && l.input[l.pos] == '-' {
		sb.WriteByte('-')
		l.advance()
	}

	// Scan field name
	if l.pos >= len(l.input) || !isIdentStart(l.input[l.pos]) {
		return Token{
			Type:   TokenError,
			Value:  "sort: requires a field name",
			Pos:    startPos,
			Line:   startLine,
			Column: startCol,
		}
	}

	for l.pos < len(l.input) && isIdentChar(l.input[l.pos]) {
		sb.WriteByte(l.input[l.pos])
		l.advance()
	}

	field := sb.String()

	// Validate field name (strip - prefix for validation)
	fieldName := field
	if len(field) > 0 && field[0] == '-' {
		fieldName = field[1:]
	}

	validSortFields := map[string]bool{
		"created":  true,
		"updated":  true,
		"closed":   true,
		"deleted":  true,
		"priority": true,
		"id":       true,
		"title":    true,
		"status":   true,
		"points":   true,
	}

	if !validSortFields[fieldName] {
		return Token{
			Type:   TokenError,
			Value:  fmt.Sprintf("invalid sort field: %s (valid: created, updated, closed, deleted, priority, id, title, status, points)", fieldName),
			Pos:    startPos,
			Line:   startLine,
			Column: startCol,
		}
	}

	return Token{
		Type:   TokenSort,
		Value:  field, // includes - prefix if descending
		Pos:    startPos,
		Line:   startLine,
		Column: startCol,
	}
}

func isIdentStart(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_'
}

func isIdentChar(ch byte) bool {
	return isIdentStart(ch) || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-'
}
