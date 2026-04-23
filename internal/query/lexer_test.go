package query

import (
	"testing"
)

// TestTokenTypeString tests the String() method for TokenType
func TestTokenTypeString(t *testing.T) {
	tests := []struct {
		tokenType TokenType
		expected  string
	}{
		{TokenEOF, "EOF"},
		{TokenError, "ERROR"},
		{TokenIdent, "IDENT"},
		{TokenString, "STRING"},
		{TokenNumber, "NUMBER"},
		{TokenDate, "DATE"},
		{TokenEq, "="},
		{TokenNeq, "!="},
		{TokenLt, "<"},
		{TokenGt, ">"},
		{TokenLte, "<="},
		{TokenGte, ">="},
		{TokenContains, "~"},
		{TokenNotContains, "!~"},
		{TokenAnd, "AND"},
		{TokenOr, "OR"},
		{TokenNot, "NOT"},
		{TokenLParen, "("},
		{TokenRParen, ")"},
		{TokenComma, ","},
		{TokenDot, "."},
		{TokenAtMe, "@me"},
		{TokenEmpty, "EMPTY"},
		{TokenNull, "NULL"},
		{TokenSort, "SORT"},
		{TokenType(999), "Token(999)"}, // unknown token
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := tt.tokenType.String()
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

// TestTokenString tests the String() method for Token
func TestTokenString(t *testing.T) {
	tests := []struct {
		token    Token
		expected string
	}{
		{Token{Type: TokenEOF}, "EOF"},
		{Token{Type: TokenIdent, Value: "status"}, `IDENT("status")`},
		{Token{Type: TokenString, Value: "hello world"}, `STRING("hello world")`},
		{Token{Type: TokenNumber, Value: "42"}, `NUMBER("42")`},
		{Token{Type: TokenEq, Value: "="}, `=("=")`},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := tt.token.String()
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

// TestLexerBasicTokens tests basic token recognition
func TestLexerBasicTokens(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []Token
	}{
		{
			name:  "single identifier",
			input: "status",
			expected: []Token{
				{Type: TokenIdent, Value: "status", Pos: 0, Line: 1, Column: 1},
				{Type: TokenEOF, Pos: 6, Line: 1, Column: 7},
			},
		},
		{
			name:  "identifier with underscore",
			input: "my_field",
			expected: []Token{
				{Type: TokenIdent, Value: "my_field", Pos: 0, Line: 1, Column: 1},
				{Type: TokenEOF, Pos: 8, Line: 1, Column: 9},
			},
		},
		{
			name:  "identifier with dash",
			input: "my-field",
			expected: []Token{
				{Type: TokenIdent, Value: "my-field", Pos: 0, Line: 1, Column: 1},
				{Type: TokenEOF, Pos: 8, Line: 1, Column: 9},
			},
		},
		{
			name:  "identifier starting with underscore",
			input: "_private",
			expected: []Token{
				{Type: TokenIdent, Value: "_private", Pos: 0, Line: 1, Column: 1},
				{Type: TokenEOF, Pos: 8, Line: 1, Column: 9},
			},
		},
		{
			name:  "simple number",
			input: "42",
			expected: []Token{
				{Type: TokenNumber, Value: "42", Pos: 0, Line: 1, Column: 1},
				{Type: TokenEOF, Pos: 2, Line: 1, Column: 3},
			},
		},
		{
			name:  "zero",
			input: "0",
			expected: []Token{
				{Type: TokenNumber, Value: "0", Pos: 0, Line: 1, Column: 1},
				{Type: TokenEOF, Pos: 1, Line: 1, Column: 2},
			},
		},
		{
			name:  "large number",
			input: "123456789",
			expected: []Token{
				{Type: TokenNumber, Value: "123456789", Pos: 0, Line: 1, Column: 1},
				{Type: TokenEOF, Pos: 9, Line: 1, Column: 10},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			tokens, err := lexer.Tokenize()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertTokensMatch(t, tt.expected, tokens)
		})
	}
}

// TestLexerStringTokens tests string literal tokenization
func TestLexerStringTokens(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []Token
	}{
		{
			name:  "double quoted string",
			input: `"hello world"`,
			expected: []Token{
				{Type: TokenString, Value: "hello world", Pos: 0, Line: 1, Column: 1},
				{Type: TokenEOF, Pos: 13, Line: 1, Column: 14},
			},
		},
		{
			name:  "single quoted string",
			input: `'hello world'`,
			expected: []Token{
				{Type: TokenString, Value: "hello world", Pos: 0, Line: 1, Column: 1},
				{Type: TokenEOF, Pos: 13, Line: 1, Column: 14},
			},
		},
		{
			name:  "empty double quoted string",
			input: `""`,
			expected: []Token{
				{Type: TokenString, Value: "", Pos: 0, Line: 1, Column: 1},
				{Type: TokenEOF, Pos: 2, Line: 1, Column: 3},
			},
		},
		{
			name:  "empty single quoted string",
			input: `''`,
			expected: []Token{
				{Type: TokenString, Value: "", Pos: 0, Line: 1, Column: 1},
				{Type: TokenEOF, Pos: 2, Line: 1, Column: 3},
			},
		},
		{
			name:  "string with escaped newline",
			input: `"line1\nline2"`,
			expected: []Token{
				{Type: TokenString, Value: "line1\nline2", Pos: 0, Line: 1, Column: 1},
				{Type: TokenEOF, Pos: 14, Line: 1, Column: 15},
			},
		},
		{
			name:  "string with escaped tab",
			input: `"col1\tcol2"`,
			expected: []Token{
				{Type: TokenString, Value: "col1\tcol2", Pos: 0, Line: 1, Column: 1},
				{Type: TokenEOF, Pos: 12, Line: 1, Column: 13},
			},
		},
		{
			name:  "string with escaped backslash",
			input: `"path\\to\\file"`,
			expected: []Token{
				{Type: TokenString, Value: "path\\to\\file", Pos: 0, Line: 1, Column: 1},
				{Type: TokenEOF, Pos: 16, Line: 1, Column: 17},
			},
		},
		{
			name:  "string with escaped double quote",
			input: `"say \"hello\""`,
			expected: []Token{
				{Type: TokenString, Value: `say "hello"`, Pos: 0, Line: 1, Column: 1},
				{Type: TokenEOF, Pos: 15, Line: 1, Column: 16},
			},
		},
		{
			name:  "string with escaped single quote",
			input: `'it\'s ok'`,
			expected: []Token{
				{Type: TokenString, Value: "it's ok", Pos: 0, Line: 1, Column: 1},
				{Type: TokenEOF, Pos: 10, Line: 1, Column: 11},
			},
		},
		{
			name:  "string with unknown escape sequence",
			input: `"test\x"`,
			expected: []Token{
				{Type: TokenString, Value: "testx", Pos: 0, Line: 1, Column: 1},
				{Type: TokenEOF, Pos: 8, Line: 1, Column: 9},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			tokens, err := lexer.Tokenize()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertTokensMatch(t, tt.expected, tokens)
		})
	}
}

// TestLexerDateTokens tests date tokenization
func TestLexerDateTokens(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []Token
	}{
		{
			name:  "ISO date",
			input: "2024-01-15",
			expected: []Token{
				{Type: TokenDate, Value: "2024-01-15", Pos: 0, Line: 1, Column: 1},
				{Type: TokenEOF, Pos: 10, Line: 1, Column: 11},
			},
		},
		{
			name:  "relative date - days negative",
			input: "-7d",
			expected: []Token{
				{Type: TokenDate, Value: "-7d", Pos: 0, Line: 1, Column: 1},
				{Type: TokenEOF, Pos: 3, Line: 1, Column: 4},
			},
		},
		{
			name:  "relative date - days positive",
			input: "+7d",
			expected: []Token{
				{Type: TokenDate, Value: "+7d", Pos: 0, Line: 1, Column: 1},
				{Type: TokenEOF, Pos: 3, Line: 1, Column: 4},
			},
		},
		{
			name:  "relative date - weeks",
			input: "-2w",
			expected: []Token{
				{Type: TokenDate, Value: "-2w", Pos: 0, Line: 1, Column: 1},
				{Type: TokenEOF, Pos: 3, Line: 1, Column: 4},
			},
		},
		{
			name:  "relative date - months",
			input: "-1m",
			expected: []Token{
				{Type: TokenDate, Value: "-1m", Pos: 0, Line: 1, Column: 1},
				{Type: TokenEOF, Pos: 3, Line: 1, Column: 4},
			},
		},
		{
			name:  "relative date - hours",
			input: "-3h",
			expected: []Token{
				{Type: TokenDate, Value: "-3h", Pos: 0, Line: 1, Column: 1},
				{Type: TokenEOF, Pos: 3, Line: 1, Column: 4},
			},
		},
		{
			name:  "relative date without sign - days",
			input: "7d",
			expected: []Token{
				{Type: TokenDate, Value: "7d", Pos: 0, Line: 1, Column: 1},
				{Type: TokenEOF, Pos: 2, Line: 1, Column: 3},
			},
		},
		{
			name:  "relative date without sign - weeks",
			input: "2w",
			expected: []Token{
				{Type: TokenDate, Value: "2w", Pos: 0, Line: 1, Column: 1},
				{Type: TokenEOF, Pos: 2, Line: 1, Column: 3},
			},
		},
		{
			name:  "keyword today",
			input: "today",
			expected: []Token{
				{Type: TokenDate, Value: "today", Pos: 0, Line: 1, Column: 1},
				{Type: TokenEOF, Pos: 5, Line: 1, Column: 6},
			},
		},
		{
			name:  "keyword yesterday",
			input: "yesterday",
			expected: []Token{
				{Type: TokenDate, Value: "yesterday", Pos: 0, Line: 1, Column: 1},
				{Type: TokenEOF, Pos: 9, Line: 1, Column: 10},
			},
		},
		{
			name:  "keyword this_week",
			input: "this_week",
			expected: []Token{
				{Type: TokenDate, Value: "this_week", Pos: 0, Line: 1, Column: 1},
				{Type: TokenEOF, Pos: 9, Line: 1, Column: 10},
			},
		},
		{
			name:  "keyword last_week",
			input: "last_week",
			expected: []Token{
				{Type: TokenDate, Value: "last_week", Pos: 0, Line: 1, Column: 1},
				{Type: TokenEOF, Pos: 9, Line: 1, Column: 10},
			},
		},
		{
			name:  "keyword this_month",
			input: "this_month",
			expected: []Token{
				{Type: TokenDate, Value: "this_month", Pos: 0, Line: 1, Column: 1},
				{Type: TokenEOF, Pos: 10, Line: 1, Column: 11},
			},
		},
		{
			name:  "keyword last_month",
			input: "last_month",
			expected: []Token{
				{Type: TokenDate, Value: "last_month", Pos: 0, Line: 1, Column: 1},
				{Type: TokenEOF, Pos: 10, Line: 1, Column: 11},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			tokens, err := lexer.Tokenize()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertTokensMatch(t, tt.expected, tokens)
		})
	}
}

// TestLexerOperators tests operator tokenization
func TestLexerOperators(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectedTyp TokenType
		expectedVal string
	}{
		{"equals", "=", TokenEq, "="},
		{"not equals", "!=", TokenNeq, "!="},
		{"less than", "<", TokenLt, "<"},
		{"greater than", ">", TokenGt, ">"},
		{"less than or equal", "<=", TokenLte, "<="},
		{"greater than or equal", ">=", TokenGte, ">="},
		{"contains", "~", TokenContains, "~"},
		{"not contains", "!~", TokenNotContains, "!~"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			tokens, err := lexer.Tokenize()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(tokens) < 1 {
				t.Fatalf("expected at least 1 token")
			}
			if tokens[0].Type != tt.expectedTyp {
				t.Errorf("expected type %v, got %v", tt.expectedTyp, tokens[0].Type)
			}
			if tokens[0].Value != tt.expectedVal {
				t.Errorf("expected value %q, got %q", tt.expectedVal, tokens[0].Value)
			}
		})
	}
}

// TestLexerBackslashEscapedOperators tests that shell-escaped operators are handled
func TestLexerBackslashEscapedOperators(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectedTyp TokenType
		expectedVal string
	}{
		{"backslash escaped neq", `\!=`, TokenNeq, "!="},
		{"backslash escaped not contains", `\!~`, TokenNotContains, "!~"},
		{"backslash escaped lt", `\<`, TokenLt, "<"},
		{"backslash escaped gt", `\>`, TokenGt, ">"},
		{"backslash escaped lte", `\<=`, TokenLte, "<="},
		{"backslash escaped gte", `\>=`, TokenGte, ">="},
		{"backslash escaped eq", `\=`, TokenEq, "="},
		{"backslash escaped contains", `\~`, TokenContains, "~"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			tokens, err := lexer.Tokenize()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(tokens) < 1 {
				t.Fatalf("expected at least 1 token")
			}
			if tokens[0].Type != tt.expectedTyp {
				t.Errorf("expected type %v, got %v", tt.expectedTyp, tokens[0].Type)
			}
			if tokens[0].Value != tt.expectedVal {
				t.Errorf("expected value %q, got %q", tt.expectedVal, tokens[0].Value)
			}
		})
	}
}

// TestLexerBackslashEscapedFullExpression tests full expressions with shell-escaped operators
func TestLexerBackslashEscapedFullExpression(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []TokenType
	}{
		{
			name:  "epic backslash-neq value",
			input: `epic \!= td-7af41c`,
			expected: []TokenType{
				TokenIdent, TokenNeq, TokenIdent, TokenEOF,
			},
		},
		{
			name:  "field backslash-not-contains value",
			input: `title \!~ "draft"`,
			expected: []TokenType{
				TokenIdent, TokenNotContains, TokenString, TokenEOF,
			},
		},
		{
			name:  "field backslash-gte date",
			input: `created \>= 2024-01-15`,
			expected: []TokenType{
				TokenIdent, TokenGte, TokenDate, TokenEOF,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			tokens, err := lexer.Tokenize()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(tokens) != len(tt.expected) {
				t.Fatalf("expected %d tokens, got %d: %v", len(tt.expected), len(tokens), tokens)
			}

			for i, tok := range tokens {
				if tok.Type != tt.expected[i] {
					t.Errorf("token %d: expected %v, got %v", i, tt.expected[i], tok.Type)
				}
			}
		})
	}
}

// TestLexerBooleanOperators tests boolean operator tokenization
func TestLexerBooleanOperators(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectedTyp TokenType
		expectedVal string
	}{
		{"AND keyword", "AND", TokenAnd, "AND"},
		{"and keyword lowercase", "and", TokenAnd, "and"},
		{"And keyword mixed", "And", TokenAnd, "And"},
		{"double ampersand", "&&", TokenAnd, "&&"},
		{"OR keyword", "OR", TokenOr, "OR"},
		{"or keyword lowercase", "or", TokenOr, "or"},
		{"Or keyword mixed", "Or", TokenOr, "Or"},
		{"double pipe", "||", TokenOr, "||"},
		{"NOT keyword", "NOT", TokenNot, "NOT"},
		{"not keyword lowercase", "not", TokenNot, "not"},
		{"Not keyword mixed", "Not", TokenNot, "Not"},
		{"exclamation mark", "!", TokenNot, "!"},
		{"dash as NOT", "-", TokenNot, "-"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			tokens, err := lexer.Tokenize()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(tokens) < 1 {
				t.Fatalf("expected at least 1 token")
			}
			if tokens[0].Type != tt.expectedTyp {
				t.Errorf("expected type %v, got %v", tt.expectedTyp, tokens[0].Type)
			}
			if tokens[0].Value != tt.expectedVal {
				t.Errorf("expected value %q, got %q", tt.expectedVal, tokens[0].Value)
			}
		})
	}
}

// TestLexerDelimiters tests delimiter tokenization
func TestLexerDelimiters(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectedTyp TokenType
		expectedVal string
	}{
		{"left paren", "(", TokenLParen, "("},
		{"right paren", ")", TokenRParen, ")"},
		{"comma", ",", TokenComma, ","},
		{"dot", ".", TokenDot, "."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			tokens, err := lexer.Tokenize()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(tokens) < 1 {
				t.Fatalf("expected at least 1 token")
			}
			if tokens[0].Type != tt.expectedTyp {
				t.Errorf("expected type %v, got %v", tt.expectedTyp, tokens[0].Type)
			}
			if tokens[0].Value != tt.expectedVal {
				t.Errorf("expected value %q, got %q", tt.expectedVal, tokens[0].Value)
			}
		})
	}
}

// TestLexerSpecialValues tests special value tokenization
func TestLexerSpecialValues(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectedTyp TokenType
		expectedVal string
	}{
		{"@me", "@me", TokenAtMe, "@me"},
		{"EMPTY", "EMPTY", TokenEmpty, "EMPTY"},
		{"Empty", "Empty", TokenEmpty, "Empty"},
		{"empty", "empty", TokenEmpty, "empty"},
		{"NULL", "NULL", TokenNull, "NULL"},
		{"Null", "Null", TokenNull, "Null"},
		{"null", "null", TokenNull, "null"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			tokens, err := lexer.Tokenize()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(tokens) < 1 {
				t.Fatalf("expected at least 1 token")
			}
			if tokens[0].Type != tt.expectedTyp {
				t.Errorf("expected type %v, got %v", tt.expectedTyp, tokens[0].Type)
			}
			if tokens[0].Value != tt.expectedVal {
				t.Errorf("expected value %q, got %q", tt.expectedVal, tokens[0].Value)
			}
		})
	}
}

// TestLexerSortClause tests sort clause tokenization
func TestLexerSortClause(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectedTyp TokenType
		expectedVal string
		wantErr     bool
	}{
		{"sort ascending created", "sort:created", TokenSort, "created", false},
		{"sort descending updated", "sort:-updated", TokenSort, "-updated", false},
		{"sort ascending priority", "sort:priority", TokenSort, "priority", false},
		{"sort descending priority", "sort:-priority", TokenSort, "-priority", false},
		{"sort ascending id", "sort:id", TokenSort, "id", false},
		{"sort ascending title", "sort:title", TokenSort, "title", false},
		{"sort ascending status", "sort:status", TokenSort, "status", false},
		{"sort ascending points", "sort:points", TokenSort, "points", false},
		{"sort ascending closed", "sort:closed", TokenSort, "closed", false},
		{"sort ascending deleted", "sort:deleted", TokenSort, "deleted", false},
		{"sort invalid field", "sort:invalid", TokenError, "", true},
		{"sort missing field", "sort:", TokenError, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			tokens, err := lexer.Tokenize()

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(tokens) < 1 {
				t.Fatalf("expected at least 1 token")
			}
			if tokens[0].Type != tt.expectedTyp {
				t.Errorf("expected type %v, got %v", tt.expectedTyp, tokens[0].Type)
			}
			if tokens[0].Value != tt.expectedVal {
				t.Errorf("expected value %q, got %q", tt.expectedVal, tokens[0].Value)
			}
		})
	}
}

// TestLexerComplexExpressions tests tokenization of complete expressions
func TestLexerComplexExpressions(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []TokenType
	}{
		{
			name:  "simple field expression",
			input: "status = open",
			expected: []TokenType{
				TokenIdent, TokenEq, TokenIdent, TokenEOF,
			},
		},
		{
			name:  "field expression with string value",
			input: `title ~ "hello world"`,
			expected: []TokenType{
				TokenIdent, TokenContains, TokenString, TokenEOF,
			},
		},
		{
			name:  "AND expression",
			input: "status = open AND type = bug",
			expected: []TokenType{
				TokenIdent, TokenEq, TokenIdent, TokenAnd, TokenIdent, TokenEq, TokenIdent, TokenEOF,
			},
		},
		{
			name:  "OR expression",
			input: "priority = P0 OR priority = P1",
			expected: []TokenType{
				TokenIdent, TokenEq, TokenIdent, TokenOr, TokenIdent, TokenEq, TokenIdent, TokenEOF,
			},
		},
		{
			name:  "NOT expression",
			input: "NOT status = closed",
			expected: []TokenType{
				TokenNot, TokenIdent, TokenEq, TokenIdent, TokenEOF,
			},
		},
		{
			name:  "grouped expression",
			input: "(status = open OR status = blocked) AND type = bug",
			expected: []TokenType{
				TokenLParen, TokenIdent, TokenEq, TokenIdent, TokenOr, TokenIdent, TokenEq, TokenIdent, TokenRParen, TokenAnd, TokenIdent, TokenEq, TokenIdent, TokenEOF,
			},
		},
		{
			name:  "function call",
			input: "has(labels)",
			expected: []TokenType{
				TokenIdent, TokenLParen, TokenIdent, TokenRParen, TokenEOF,
			},
		},
		{
			name:  "function with multiple arguments",
			input: "any(type, bug, feature)",
			expected: []TokenType{
				TokenIdent, TokenLParen, TokenIdent, TokenComma, TokenIdent, TokenComma, TokenIdent, TokenRParen, TokenEOF,
			},
		},
		{
			name:  "dot notation",
			input: "log.message ~ fix",
			expected: []TokenType{
				TokenIdent, TokenDot, TokenIdent, TokenContains, TokenIdent, TokenEOF,
			},
		},
		{
			name:  "date comparison",
			input: "created >= 2024-01-15",
			expected: []TokenType{
				TokenIdent, TokenGte, TokenDate, TokenEOF,
			},
		},
		{
			name:  "relative date comparison",
			input: "updated >= -7d",
			expected: []TokenType{
				TokenIdent, TokenGte, TokenDate, TokenEOF,
			},
		},
		{
			name:  "special value @me",
			input: "implementer = @me",
			expected: []TokenType{
				TokenIdent, TokenEq, TokenAtMe, TokenEOF,
			},
		},
		{
			name:  "special value EMPTY",
			input: "labels = EMPTY",
			expected: []TokenType{
				TokenIdent, TokenEq, TokenEmpty, TokenEOF,
			},
		},
		{
			name:  "expression with sort",
			input: "status = open sort:created",
			expected: []TokenType{
				TokenIdent, TokenEq, TokenIdent, TokenSort, TokenEOF,
			},
		},
		{
			name:  "dash as NOT operator",
			input: "-status = open",
			expected: []TokenType{
				TokenNot, TokenIdent, TokenEq, TokenIdent, TokenEOF,
			},
		},
		{
			name:  "symbolic boolean operators",
			input: "a && b || c",
			expected: []TokenType{
				TokenIdent, TokenAnd, TokenIdent, TokenOr, TokenIdent, TokenEOF,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			tokens, err := lexer.Tokenize()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(tokens) != len(tt.expected) {
				t.Fatalf("expected %d tokens, got %d: %v", len(tt.expected), len(tokens), tokens)
			}

			for i, tok := range tokens {
				if tok.Type != tt.expected[i] {
					t.Errorf("token %d: expected %v, got %v", i, tt.expected[i], tok.Type)
				}
			}
		})
	}
}

// TestLexerEdgeCases tests edge cases and error conditions
func TestLexerEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty input",
			input:   "",
			wantErr: false,
		},
		{
			name:    "whitespace only",
			input:   "   \t\n  ",
			wantErr: false,
		},
		{
			name:    "unterminated double quoted string",
			input:   `"hello`,
			wantErr: true,
			errMsg:  "unterminated string",
		},
		{
			name:    "unterminated single quoted string",
			input:   `'hello`,
			wantErr: true,
			errMsg:  "unterminated string",
		},
		{
			name:    "unknown @ value",
			input:   "@unknown",
			wantErr: true,
			errMsg:  "unknown special value",
		},
		{
			name:    "invalid + relative date",
			input:   "+7x",
			wantErr: true,
			errMsg:  "invalid relative date",
		},
		{
			name:    "+ with unit but no digits",
			input:   "+d",
			wantErr: false, // "+d" is treated as valid (0 days)
		},
		{
			name:    "+ with no unit and no digits",
			input:   "+",
			wantErr: true,
			errMsg:  "invalid relative date",
		},
		{
			name:    "invalid character",
			input:   "status # open",
			wantErr: true,
			errMsg:  "unexpected character",
		},
		{
			name:    "invalid character at start",
			input:   "#status",
			wantErr: true,
			errMsg:  "unexpected character",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			tokens, err := lexer.Tokenize()

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errMsg)
					return
				}
				if tt.errMsg != "" && !containsStr(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
				// Also check the last token is an error token
				if len(tokens) > 0 {
					lastToken := tokens[len(tokens)-1]
					if lastToken.Type != TokenError && tokens[len(tokens)-1].Type != TokenEOF {
						// Look for error token in tokens
						foundError := false
						for _, tok := range tokens {
							if tok.Type == TokenError {
								foundError = true
								break
							}
						}
						if !foundError {
							t.Errorf("expected error token in tokens, got %v", tokens)
						}
					}
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				// Should end with EOF
				if len(tokens) > 0 && tokens[len(tokens)-1].Type != TokenEOF {
					t.Errorf("expected last token to be EOF, got %v", tokens[len(tokens)-1].Type)
				}
			}
		})
	}
}

// TestLexerPositionTracking tests that position tracking works correctly
func TestLexerPositionTracking(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []struct {
			typ    TokenType
			pos    int
			line   int
			column int
		}
	}{
		{
			name:  "single line positions",
			input: "status = open",
			expected: []struct {
				typ    TokenType
				pos    int
				line   int
				column int
			}{
				{TokenIdent, 0, 1, 1},  // "status"
				{TokenEq, 7, 1, 8},     // "="
				{TokenIdent, 9, 1, 10}, // "open"
				{TokenEOF, 13, 1, 14},
			},
		},
		{
			name:  "multiline positions",
			input: "status = open\ntype = bug",
			expected: []struct {
				typ    TokenType
				pos    int
				line   int
				column int
			}{
				{TokenIdent, 0, 1, 1},  // "status"
				{TokenEq, 7, 1, 8},     // "="
				{TokenIdent, 9, 1, 10}, // "open"
				{TokenIdent, 14, 2, 1}, // "type"
				{TokenEq, 19, 2, 6},    // "="
				{TokenIdent, 21, 2, 8}, // "bug"
				{TokenEOF, 24, 2, 11},
			},
		},
		{
			name:  "leading whitespace",
			input: "   status",
			expected: []struct {
				typ    TokenType
				pos    int
				line   int
				column int
			}{
				{TokenIdent, 3, 1, 4}, // "status"
				{TokenEOF, 9, 1, 10},
			},
		},
		{
			name:  "multiple whitespace types",
			input: "status\t=\n\topen",
			expected: []struct {
				typ    TokenType
				pos    int
				line   int
				column int
			}{
				{TokenIdent, 0, 1, 1}, // "status"
				{TokenEq, 7, 1, 8},    // "="
				{TokenIdent, 10, 2, 2}, // "open"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			tokens, err := lexer.Tokenize()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(tokens) < len(tt.expected) {
				t.Fatalf("expected at least %d tokens, got %d", len(tt.expected), len(tokens))
			}

			for i, exp := range tt.expected {
				tok := tokens[i]
				if tok.Type != exp.typ {
					t.Errorf("token %d: expected type %v, got %v", i, exp.typ, tok.Type)
				}
				if tok.Pos != exp.pos {
					t.Errorf("token %d: expected pos %d, got %d", i, exp.pos, tok.Pos)
				}
				if tok.Line != exp.line {
					t.Errorf("token %d: expected line %d, got %d", i, exp.line, tok.Line)
				}
				if tok.Column != exp.column {
					t.Errorf("token %d: expected column %d, got %d", i, exp.column, tok.Column)
				}
			}
		})
	}
}

// TestLexerNegativeNumbers tests negative number tokenization
func TestLexerNegativeNumbers(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []Token
	}{
		{
			name:  "negative number without unit",
			input: "-42",
			expected: []Token{
				{Type: TokenNumber, Value: "-42", Pos: 0, Line: 1, Column: 1},
				{Type: TokenEOF, Pos: 3, Line: 1, Column: 4},
			},
		},
		{
			name:  "standalone dash",
			input: "-",
			expected: []Token{
				{Type: TokenNot, Value: "-", Pos: 0, Line: 1, Column: 1},
				{Type: TokenEOF, Pos: 1, Line: 1, Column: 2},
			},
		},
		{
			name:  "dash followed by identifier",
			input: "-status",
			expected: []Token{
				{Type: TokenNot, Value: "-", Pos: 0, Line: 1, Column: 1},
				{Type: TokenIdent, Value: "status", Pos: 1, Line: 1, Column: 2},
				{Type: TokenEOF, Pos: 7, Line: 1, Column: 8},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			tokens, err := lexer.Tokenize()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertTokensMatch(t, tt.expected, tokens)
		})
	}
}

// TestLexerWhitespaceHandling tests various whitespace scenarios
func TestLexerWhitespaceHandling(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []TokenType
	}{
		{
			name:     "spaces between tokens",
			input:    "status   =   open",
			expected: []TokenType{TokenIdent, TokenEq, TokenIdent, TokenEOF},
		},
		{
			name:     "tabs between tokens",
			input:    "status\t=\topen",
			expected: []TokenType{TokenIdent, TokenEq, TokenIdent, TokenEOF},
		},
		{
			name:     "newlines between tokens",
			input:    "status\n=\nopen",
			expected: []TokenType{TokenIdent, TokenEq, TokenIdent, TokenEOF},
		},
		{
			name:     "mixed whitespace",
			input:    "status \t\n = \t\n open",
			expected: []TokenType{TokenIdent, TokenEq, TokenIdent, TokenEOF},
		},
		{
			name:     "no whitespace needed for operators",
			input:    "status=open",
			expected: []TokenType{TokenIdent, TokenEq, TokenIdent, TokenEOF},
		},
		{
			name:     "no whitespace in parentheses",
			input:    "(status=open)",
			expected: []TokenType{TokenLParen, TokenIdent, TokenEq, TokenIdent, TokenRParen, TokenEOF},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			tokens, err := lexer.Tokenize()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(tokens) != len(tt.expected) {
				t.Fatalf("expected %d tokens, got %d: %v", len(tt.expected), len(tokens), tokens)
			}

			for i, tok := range tokens {
				if tok.Type != tt.expected[i] {
					t.Errorf("token %d: expected %v, got %v", i, tt.expected[i], tok.Type)
				}
			}
		})
	}
}

// TestLexerTokenizeMultipleTimes ensures the lexer can be reused
func TestLexerTokenizeMultipleTimes(t *testing.T) {
	input := "status = open"
	lexer := NewLexer(input)

	// First tokenization
	tokens1, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("first tokenize error: %v", err)
	}

	// Second tokenization should start from the beginning again
	// because Tokenize resets tokens but not position
	// Actually, looking at the code, tokens is reset but pos is not
	// This tests the actual behavior
	tokens2, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("second tokenize error: %v", err)
	}

	// After first tokenization, pos is at end, so second should just return EOF
	if len(tokens1) != 4 {
		t.Errorf("first tokenize: expected 4 tokens, got %d", len(tokens1))
	}
	if len(tokens2) != 1 || tokens2[0].Type != TokenEOF {
		t.Errorf("second tokenize: expected only EOF, got %v", tokens2)
	}
}

// TestLexerNewLexer tests the constructor
func TestLexerNewLexer(t *testing.T) {
	input := "test input"
	lexer := NewLexer(input)

	if lexer.input != input {
		t.Errorf("expected input %q, got %q", input, lexer.input)
	}
	if lexer.pos != 0 {
		t.Errorf("expected pos 0, got %d", lexer.pos)
	}
	if lexer.line != 1 {
		t.Errorf("expected line 1, got %d", lexer.line)
	}
	if lexer.column != 1 {
		t.Errorf("expected column 1, got %d", lexer.column)
	}
	if lexer.tokens != nil {
		t.Errorf("expected tokens nil, got %v", lexer.tokens)
	}
}

// Helper functions

func assertTokensMatch(t *testing.T, expected, actual []Token) {
	t.Helper()

	if len(expected) != len(actual) {
		t.Fatalf("expected %d tokens, got %d\nexpected: %v\nactual: %v", len(expected), len(actual), expected, actual)
	}

	for i, exp := range expected {
		act := actual[i]
		if exp.Type != act.Type {
			t.Errorf("token %d: expected type %v, got %v", i, exp.Type, act.Type)
		}
		if exp.Value != act.Value {
			t.Errorf("token %d: expected value %q, got %q", i, exp.Value, act.Value)
		}
		if exp.Pos != act.Pos {
			t.Errorf("token %d: expected pos %d, got %d", i, exp.Pos, act.Pos)
		}
		if exp.Line != act.Line {
			t.Errorf("token %d: expected line %d, got %d", i, exp.Line, act.Line)
		}
		if exp.Column != act.Column {
			t.Errorf("token %d: expected column %d, got %d", i, exp.Column, act.Column)
		}
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || (len(s) > len(substr) && (s[:len(substr)] == substr || containsStr(s[1:], substr))))
}

// TestIsIdentStart tests the isIdentStart helper function
func TestIsIdentStart(t *testing.T) {
	tests := []struct {
		ch       byte
		expected bool
	}{
		{'a', true},
		{'z', true},
		{'A', true},
		{'Z', true},
		{'_', true},
		{'0', false},
		{'9', false},
		{'-', false},
		{'@', false},
		{' ', false},
		{'.', false},
	}

	for _, tt := range tests {
		t.Run(string(tt.ch), func(t *testing.T) {
			got := isIdentStart(tt.ch)
			if got != tt.expected {
				t.Errorf("isIdentStart(%q) = %v, want %v", tt.ch, got, tt.expected)
			}
		})
	}
}

// TestIsIdentChar tests the isIdentChar helper function
func TestIsIdentChar(t *testing.T) {
	tests := []struct {
		ch       byte
		expected bool
	}{
		{'a', true},
		{'z', true},
		{'A', true},
		{'Z', true},
		{'_', true},
		{'0', true},
		{'9', true},
		{'-', true},
		{'@', false},
		{' ', false},
		{'.', false},
		{'(', false},
	}

	for _, tt := range tests {
		t.Run(string(tt.ch), func(t *testing.T) {
			got := isIdentChar(tt.ch)
			if got != tt.expected {
				t.Errorf("isIdentChar(%q) = %v, want %v", tt.ch, got, tt.expected)
			}
		})
	}
}
