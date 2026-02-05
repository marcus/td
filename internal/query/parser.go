// Package query implements the TDQ (td query) language parser, lexer, AST,
// and evaluator for filtering issues.
package query

import (
	"fmt"
	"strconv"
	"strings"
)

// MaxQueryDepth limits nesting to prevent stack overflow
const MaxQueryDepth = 50

// Parser parses TDQ query strings into an AST
type Parser struct {
	tokens []Token
	pos    int
	input  string
	depth  int // Current nesting depth
}

// ParseError represents a parsing error with position information
type ParseError struct {
	Message  string
	Pos      int
	Line     int
	Column   int
	Token    Token
	Expected string
}

func (e *ParseError) Error() string {
	if e.Expected != "" {
		return fmt.Sprintf("parse error at line %d, column %d: %s (expected %s, got %s)",
			e.Line, e.Column, e.Message, e.Expected, e.Token.String())
	}
	return fmt.Sprintf("parse error at line %d, column %d: %s", e.Line, e.Column, e.Message)
}

// Parse parses a TDQ query string and returns a Query AST
func Parse(input string) (*Query, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return &Query{Root: nil, Raw: input}, nil
	}

	lexer := NewLexer(input)
	tokens, err := lexer.Tokenize()
	if err != nil {
		return nil, err
	}

	// Extract sort clause(s) from tokens
	var sortClause *SortClause
	var filteredTokens []Token
	for _, tok := range tokens {
		if tok.Type == TokenSort {
			if sortClause != nil {
				return nil, &ParseError{
					Message: "multiple sort clauses not allowed",
					Pos:     tok.Pos,
					Line:    tok.Line,
					Column:  tok.Column,
					Token:   tok,
				}
			}
			sortClause = parseSortToken(tok.Value)
		} else {
			filteredTokens = append(filteredTokens, tok)
		}
	}

	p := &Parser{
		tokens: filteredTokens,
		pos:    0,
		input:  input,
	}

	// If only sort clause and no filter, return query with just sort
	if p.isAtEnd() {
		return &Query{Root: nil, Raw: input, Sort: sortClause}, nil
	}

	root, err := p.parseQuery()
	if err != nil {
		return nil, err
	}

	// Ensure we consumed all tokens
	if !p.isAtEnd() {
		tok := p.current()
		return nil, &ParseError{
			Message: "unexpected token after expression",
			Pos:     tok.Pos,
			Line:    tok.Line,
			Column:  tok.Column,
			Token:   tok,
		}
	}

	return &Query{Root: root, Raw: input, Sort: sortClause}, nil
}

// parseSortToken converts a sort token value to a SortClause
// Value format: "field" or "-field" (descending)
func parseSortToken(value string) *SortClause {
	descending := false
	field := value

	if len(value) > 0 && value[0] == '-' {
		descending = true
		field = value[1:]
	}

	// Map user field name to DB column
	dbColumn := field
	if col, ok := SortFieldToColumn[field]; ok {
		dbColumn = col
	}

	return &SortClause{
		Field:      dbColumn,
		Descending: descending,
	}
}

func (p *Parser) parseQuery() (Node, error) {
	return p.parseOr()
}

// parseOr handles OR expressions (lowest precedence)
func (p *Parser) parseOr() (Node, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}

	for p.match(TokenOr) {
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Op: OpOr, Left: left, Right: right}
	}

	return left, nil
}

// parseAnd handles AND expressions
func (p *Parser) parseAnd() (Node, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}

	// AND can be explicit or implicit (just whitespace between expressions)
	for {
		if p.match(TokenAnd) {
			right, err := p.parseUnary()
			if err != nil {
				return nil, err
			}
			left = &BinaryExpr{Op: OpAnd, Left: left, Right: right}
			continue
		}

		// Check for implicit AND: next token starts a new expression
		if !p.isAtEnd() && p.isExpressionStart() {
			right, err := p.parseUnary()
			if err != nil {
				return nil, err
			}
			left = &BinaryExpr{Op: OpAnd, Left: left, Right: right}
			continue
		}

		break
	}

	return left, nil
}

// parseUnary handles NOT/- prefix
func (p *Parser) parseUnary() (Node, error) {
	if p.match(TokenNot) {
		expr, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &UnaryExpr{Op: OpNot, Expr: expr}, nil
	}

	return p.parsePrimary()
}

// parsePrimary handles field expressions, functions, text search, and grouping
func (p *Parser) parsePrimary() (Node, error) {
	// Grouped expression
	if p.match(TokenLParen) {
		p.depth++
		if p.depth > MaxQueryDepth {
			tok := p.current()
			return nil, &ParseError{
				Message:  fmt.Sprintf("query exceeds maximum nesting depth of %d", MaxQueryDepth),
				Pos:      tok.Pos,
				Line:     tok.Line,
				Column:   tok.Column,
				Token:    tok,
			}
		}
		expr, err := p.parseQuery()
		p.depth--
		if err != nil {
			return nil, err
		}
		if !p.match(TokenRParen) {
			tok := p.current()
			return nil, &ParseError{
				Message:  "missing closing parenthesis",
				Pos:      tok.Pos,
				Line:     tok.Line,
				Column:   tok.Column,
				Token:    tok,
				Expected: ")",
			}
		}
		return expr, nil
	}

	// Identifier could be: field expression, function call, or text search
	if p.check(TokenIdent) {
		return p.parseIdentExpr()
	}

	// Quoted string is text search
	if p.check(TokenString) {
		tok := p.advance()
		return &TextSearch{Text: tok.Value}, nil
	}

	// Unexpected token
	tok := p.current()
	return nil, &ParseError{
		Message:  "unexpected token",
		Pos:      tok.Pos,
		Line:     tok.Line,
		Column:   tok.Column,
		Token:    tok,
		Expected: "field, function, or quoted text",
	}
}

// parseIdentExpr handles identifiers which could be field expressions or functions
func (p *Parser) parseIdentExpr() (Node, error) {
	tok := p.advance()
	name := tok.Value

	// Check if this is a function call
	if p.check(TokenLParen) {
		return p.parseFunctionCall(name)
	}

	// Build field name (handle dot notation like log.message)
	field := name
	for p.match(TokenDot) {
		if !p.check(TokenIdent) {
			nextTok := p.current()
			return nil, &ParseError{
				Message:  "expected field name after '.'",
				Pos:      nextTok.Pos,
				Line:     nextTok.Line,
				Column:   nextTok.Column,
				Token:    nextTok,
				Expected: "identifier",
			}
		}
		subField := p.advance()
		field = field + "." + subField.Value
	}

	// Check for operator
	op, err := p.parseOperator()
	if err != nil {
		// No operator - treat as text search
		return &TextSearch{Text: field}, nil
	}

	// Parse value
	value, err := p.parseValue()
	if err != nil {
		return nil, err
	}

	return &FieldExpr{
		Field:    field,
		Operator: op,
		Value:    value,
	}, nil
}

func (p *Parser) parseFunctionCall(name string) (Node, error) {
	p.advance() // consume '('

	var args []interface{}

	// Empty args
	if p.match(TokenRParen) {
		return &FunctionCall{Name: name, Args: args}, nil
	}

	// Parse arguments
	for {
		arg, err := p.parseFunctionArg()
		if err != nil {
			return nil, err
		}
		args = append(args, arg)

		if !p.match(TokenComma) {
			break
		}
	}

	if !p.match(TokenRParen) {
		tok := p.current()
		return nil, &ParseError{
			Message:  "missing closing parenthesis in function call",
			Pos:      tok.Pos,
			Line:     tok.Line,
			Column:   tok.Column,
			Token:    tok,
			Expected: ")",
		}
	}

	return &FunctionCall{Name: name, Args: args}, nil
}

func (p *Parser) parseFunctionArg() (interface{}, error) {
	tok := p.current()

	switch tok.Type {
	case TokenIdent:
		p.advance()
		// Check for dot notation
		value := tok.Value
		for p.match(TokenDot) {
			if !p.check(TokenIdent) {
				break
			}
			subField := p.advance()
			value = value + "." + subField.Value
		}
		return value, nil
	case TokenString:
		p.advance()
		return tok.Value, nil
	case TokenNumber:
		p.advance()
		n, err := strconv.ParseInt(tok.Value, 10, 64)
		if err != nil {
			return nil, &ParseError{
				Message:  fmt.Sprintf("invalid number: %s", tok.Value),
				Pos:      tok.Pos,
				Line:     tok.Line,
				Column:   tok.Column,
				Token:    tok,
				Expected: "valid integer",
			}
		}
		return int(n), nil
	case TokenDate:
		p.advance()
		return &DateValue{Raw: tok.Value, Relative: isRelativeDate(tok.Value)}, nil
	case TokenAtMe:
		p.advance()
		return &SpecialValue{Type: "me"}, nil
	case TokenEmpty:
		p.advance()
		return &SpecialValue{Type: "empty"}, nil
	case TokenNull:
		p.advance()
		return &SpecialValue{Type: "null"}, nil
	default:
		return nil, &ParseError{
			Message:  "invalid function argument",
			Pos:      tok.Pos,
			Line:     tok.Line,
			Column:   tok.Column,
			Token:    tok,
			Expected: "identifier, string, number, or special value",
		}
	}
}

func (p *Parser) parseOperator() (string, error) {
	tok := p.current()
	var op string

	switch tok.Type {
	case TokenEq:
		op = OpEq
	case TokenNeq:
		op = OpNeq
	case TokenLt:
		op = OpLt
	case TokenGt:
		op = OpGt
	case TokenLte:
		op = OpLte
	case TokenGte:
		op = OpGte
	case TokenContains:
		op = OpContains
	case TokenNotContains:
		op = OpNotContains
	default:
		return "", fmt.Errorf("not an operator")
	}

	p.advance()
	return op, nil
}

func (p *Parser) parseValue() (interface{}, error) {
	tok := p.current()

	switch tok.Type {
	case TokenIdent:
		p.advance()
		return tok.Value, nil
	case TokenString:
		p.advance()
		return tok.Value, nil
	case TokenNumber:
		p.advance()
		n, err := strconv.ParseInt(tok.Value, 10, 64)
		if err != nil {
			return nil, &ParseError{
				Message:  fmt.Sprintf("invalid number: %s", tok.Value),
				Pos:      tok.Pos,
				Line:     tok.Line,
				Column:   tok.Column,
				Token:    tok,
				Expected: "valid integer",
			}
		}
		return int(n), nil
	case TokenDate:
		p.advance()
		return &DateValue{Raw: tok.Value, Relative: isRelativeDate(tok.Value)}, nil
	case TokenAtMe:
		p.advance()
		return &SpecialValue{Type: "me"}, nil
	case TokenEmpty:
		p.advance()
		return &SpecialValue{Type: "empty"}, nil
	case TokenNull:
		p.advance()
		return &SpecialValue{Type: "null"}, nil
	case TokenLParen:
		return p.parseListValue()
	default:
		return nil, &ParseError{
			Message:  "expected value",
			Pos:      tok.Pos,
			Line:     tok.Line,
			Column:   tok.Column,
			Token:    tok,
			Expected: "identifier, string, number, date, or special value",
		}
	}
}

func (p *Parser) parseListValue() (*ListValue, error) {
	p.advance() // consume '('

	var values []interface{}

	if p.match(TokenRParen) {
		return &ListValue{Values: values}, nil
	}

	for {
		val, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		values = append(values, val)

		if !p.match(TokenComma) {
			break
		}
	}

	if !p.match(TokenRParen) {
		tok := p.current()
		return nil, &ParseError{
			Message:  "missing closing parenthesis in list",
			Pos:      tok.Pos,
			Line:     tok.Line,
			Column:   tok.Column,
			Token:    tok,
			Expected: ")",
		}
	}

	return &ListValue{Values: values}, nil
}

// Helper methods

func (p *Parser) current() Token {
	if p.pos >= len(p.tokens) {
		return Token{Type: TokenEOF}
	}
	return p.tokens[p.pos]
}

func (p *Parser) advance() Token {
	tok := p.current()
	if !p.isAtEnd() {
		p.pos++
	}
	return tok
}

func (p *Parser) check(typ TokenType) bool {
	return p.current().Type == typ
}

func (p *Parser) match(typ TokenType) bool {
	if p.check(typ) {
		p.advance()
		return true
	}
	return false
}

func (p *Parser) isAtEnd() bool {
	return p.current().Type == TokenEOF
}

// isExpressionStart checks if the current token can start a new expression
// Used for implicit AND detection
func (p *Parser) isExpressionStart() bool {
	tok := p.current()
	switch tok.Type {
	case TokenIdent, TokenString, TokenLParen, TokenNot:
		return true
	default:
		return false
	}
}

func isRelativeDate(s string) bool {
	if s == "" {
		return false
	}
	// Keywords
	switch s {
	case "today", "yesterday", "this_week", "last_week", "this_month", "last_month":
		return true
	}
	// Offset format: -7d, +3w, etc.
	if (s[0] == '-' || s[0] == '+') && len(s) >= 2 {
		return true
	}
	// Bare offset: 7d, 3w (less common)
	if len(s) >= 2 {
		last := s[len(s)-1]
		if last == 'd' || last == 'w' || last == 'm' || last == 'h' {
			return true
		}
	}
	return false
}

// Validate checks the query AST for semantic errors
func (q *Query) Validate() []error {
	if q.Root == nil {
		return nil
	}
	var errs []error
	validateNode(q.Root, &errs)
	return errs
}

func validateNode(n Node, errs *[]error) {
	switch node := n.(type) {
	case *BinaryExpr:
		validateNode(node.Left, errs)
		validateNode(node.Right, errs)
	case *UnaryExpr:
		validateNode(node.Expr, errs)
	case *FieldExpr:
		validateFieldExpr(node, errs)
	case *FunctionCall:
		validateFunctionCall(node, errs)
	case *TextSearch:
		// Text search is always valid
	}
}

func validateFieldExpr(f *FieldExpr, errs *[]error) {
	// Check if field is known
	parts := strings.Split(f.Field, ".")
	baseName := parts[0]

	fieldType, ok := KnownFields[baseName]
	if !ok {
		*errs = append(*errs, fmt.Errorf("unknown field: %s", f.Field))
		return
	}

	// If it's a prefix (cross-entity), validate the sub-field
	if fieldType == "prefix" && len(parts) > 1 {
		subFields, ok := CrossEntityFields[baseName]
		if !ok {
			*errs = append(*errs, fmt.Errorf("unknown entity prefix: %s", baseName))
			return
		}
		subField := parts[1]
		if _, ok := subFields[subField]; !ok {
			*errs = append(*errs, fmt.Errorf("unknown field: %s.%s", baseName, subField))
		}
	}

	// Validate enum values
	if enumVals, ok := EnumValues[f.Field]; ok {
		if strVal, ok := f.Value.(string); ok {
			found := false
			for _, v := range enumVals {
				if strings.EqualFold(v, strVal) {
					f.Value = v // normalize to canonical form
					found = true
					break
				}
			}
			if !found {
				*errs = append(*errs, fmt.Errorf("invalid value for %s: %q (expected one of: %s)",
					f.Field, strVal, strings.Join(enumVals, ", ")))
			}
		}
	}
}

func validateFunctionCall(fn *FunctionCall, errs *[]error) {
	spec, ok := KnownFunctions[fn.Name]
	if !ok {
		*errs = append(*errs, fmt.Errorf("unknown function: %s", fn.Name))
		return
	}

	argc := len(fn.Args)
	if argc < spec.MinArgs {
		*errs = append(*errs, fmt.Errorf("function %s requires at least %d argument(s), got %d",
			fn.Name, spec.MinArgs, argc))
	}
	if spec.MaxArgs > 0 && argc > spec.MaxArgs {
		*errs = append(*errs, fmt.Errorf("function %s accepts at most %d argument(s), got %d",
			fn.Name, spec.MaxArgs, argc))
	}

	// Normalize enum values in function arguments.
	// Functions like any(field, v1, v2), all(), none(), is() have enum args.
	switch fn.Name {
	case "is":
		// is(status) - single arg is a status enum value
		if len(fn.Args) >= 1 {
			if strVal, ok := fn.Args[0].(string); ok {
				if enumVals, ok := EnumValues["status"]; ok {
					for _, v := range enumVals {
						if strings.EqualFold(v, strVal) {
							fn.Args[0] = v
							break
						}
					}
				}
			}
		}
	case "any", "all", "none":
		// First arg is field name, remaining args are values to match
		if len(fn.Args) >= 2 {
			fieldName := fmt.Sprintf("%v", fn.Args[0])
			if enumVals, ok := EnumValues[fieldName]; ok {
				for i := 1; i < len(fn.Args); i++ {
					if strVal, ok := fn.Args[i].(string); ok {
						for _, v := range enumVals {
							if strings.EqualFold(v, strVal) {
								fn.Args[i] = v
								break
							}
						}
					}
				}
			}
		}
	}
}
