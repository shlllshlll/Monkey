package lexer

import (
	"fmt"
	"strings"
	"testing"

	"github.com/shlllshlll/monkey/token"
)

func TestNextToken(t *testing.T) {
	input := `let five = 5;
let ten = 10;

let add = fn(x, y) {
  x + y;
};

let result = add(five, ten);
!-/*5;
5 < 10 > 5;

if (5 < 10) {
	return true;
} else {
	return false;
}

10 == 10;
10 != 9;
`
	tests := []struct {
		expectedType    token.TokenType
		expectedLiteral string
	}{
		{token.LET, "let"},
		{token.IDENT, "five"},
		{token.ASSIGN, "="},
		{token.INT, "5"},
		{token.SEMICOLON, ";"},
		{token.LET, "let"},
		{token.IDENT, "ten"},
		{token.ASSIGN, "="},
		{token.INT, "10"},
		{token.SEMICOLON, ";"},
		{token.LET, "let"},
		{token.IDENT, "add"},
		{token.ASSIGN, "="},
		{token.FUNC, "fn"},
		{token.LEFT_PAREN, "("},
		{token.IDENT, "x"},
		{token.COMMA, ","},
		{token.IDENT, "y"},
		{token.RIGHT_PAREN, ")"},
		{token.LEFT_BRACE, "{"},
		{token.IDENT, "x"},
		{token.PLUS, "+"},
		{token.IDENT, "y"},
		{token.SEMICOLON, ";"},
		{token.RIGHT_BRACE, "}"},
		{token.SEMICOLON, ";"},
		{token.LET, "let"},
		{token.IDENT, "result"},
		{token.ASSIGN, "="},
		{token.IDENT, "add"},
		{token.LEFT_PAREN, "("},
		{token.IDENT, "five"},
		{token.COMMA, ","},
		{token.IDENT, "ten"},
		{token.RIGHT_PAREN, ")"},
		{token.SEMICOLON, ";"},
		{token.NOT, "!"},
		{token.MINUS, "-"},
		{token.DIVIDE, "/"},
		{token.MULTI, "*"},
		{token.INT, "5"},
		{token.SEMICOLON, ";"},
		{token.INT, "5"},
		{token.LT, "<"},
		{token.INT, "10"},
		{token.GT, ">"},
		{token.INT, "5"},
		{token.SEMICOLON, ";"},
		{token.IF, "if"},
		{token.LEFT_PAREN, "("},
		{token.INT, "5"},
		{token.LT, "<"},
		{token.INT, "10"},
		{token.RIGHT_PAREN, ")"},
		{token.LEFT_BRACE, "{"},
		{token.RETURN, "return"},
		{token.TRUE, "true"},
		{token.SEMICOLON, ";"},
		{token.RIGHT_BRACE, "}"},
		{token.ELSE, "else"},
		{token.LEFT_BRACE, "{"},
		{token.RETURN, "return"},
		{token.FALSE, "false"},
		{token.SEMICOLON, ";"},
		{token.RIGHT_BRACE, "}"},
		{token.INT, "10"},
		{token.EQUAL, "=="},
		{token.INT, "10"},
		{token.SEMICOLON, ";"},
		{token.INT, "10"},
		{token.NOT_EQUAL, "!="},
		{token.INT, "9"},
		{token.SEMICOLON, ";"},
		{token.EOF, ""},
	}

	l := New(input)

	for i, tt := range tests {
		tok := l.NextToken()

		fmt.Printf("#%d - type[%q], literal[%q]\n", i, strings.ToUpper(string(tok.Type)), tok.Literal)
		if tok.Type != tt.expectedType {
			t.Fatalf("tests[%d] - tokentype wrong. expected=%q, got=%q",
				i, tt.expectedType, tok.Type)
		}

		if tok.Literal != tt.expectedLiteral {
			t.Fatalf("tests[%d] - literal wrong. expected=%q, got=%q",
				i, tt.expectedLiteral, tok.Literal)
		}
	}
}

func TestTokenPositions(t *testing.T) {
	l := New("let x = 1;\n  return x;")
	tests := []struct {
		type_  token.TokenType
		line   int
		column int
	}{
		{token.LET, 1, 1}, {token.IDENT, 1, 5}, {token.ASSIGN, 1, 7},
		{token.INT, 1, 9}, {token.SEMICOLON, 1, 10}, {token.RETURN, 2, 3},
		{token.IDENT, 2, 10}, {token.SEMICOLON, 2, 11},
	}
	for i, tt := range tests {
		got := l.NextToken()
		if got.Type != tt.type_ || got.Line != tt.line || got.Column != tt.column {
			t.Fatalf("token %d: got %+v, want type=%s at %d:%d", i, got, tt.type_, tt.line, tt.column)
		}
	}
}

func TestTokenAtEOF(t *testing.T) {
	tests := []struct {
		input, literal string
		type_          token.TokenType
	}{
		{"ab", "ab", token.IDENT}, {"12", "12", token.INT},
		{"1.5", "1.5", token.FLOAT}, {"let", "let", token.LET},
		{"return", "return", token.RETURN},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := New(tt.input).NextToken()
			if got.Type != tt.type_ || got.Literal != tt.literal {
				t.Fatalf("got %s/%q, want %s/%q", got.Type, got.Literal, tt.type_, tt.literal)
			}
		})
	}
}

func TestEOFIsStable(t *testing.T) {
	l := New("x")
	l.NextToken()
	for i := 0; i < 5; i++ {
		if got := l.NextToken(); got.Type != token.EOF {
			t.Fatalf("call %d: got %s, want EOF", i, got.Type)
		}
	}
}
