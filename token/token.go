package token

type TokenType string

const (
	// keywords
	LET    = "let"
	FUNC   = "fn"
	IF     = "if"
	ELSE   = "else"
	WHILE  = "while"
	RETURN = "return"

	// literals
	TRUE  = "true"
	FALSE = "false"

	// operators
	ASSIGN = "="
	PLUS   = "+"
	MINUS  = "-"
	MULTI  = "*"
	DIVIDE = "/"
	NOT    = "!"

	// comparison operators
	EQUAL     = "=="
	NOT_EQUAL = "!="
	GT        = ">"
	LT        = "<"
	GE        = ">="
	LE        = "<="

	// logical operators
	AND = "&&"
	OR  = "||"

	// delimiters
	COMMA         = ","
	COLON         = ":"
	SEMICOLON     = ";"
	LEFT_BRACE    = "{"
	RIGHT_BRACE   = "}"
	LEFT_BRACKET  = "["
	RIGHT_BRACKET = "]"
	LEFT_PAREN    = "("
	RIGHT_PAREN   = ")"

	// literal
	INT    = "int"
	FLOAT  = "float"
	STRING = "string"

	// identifier
	IDENT = "ident"
	// illegal
	ILLEGAL = "illegal"
	// eof
	EOF = "eof"
)

type Token struct {
	Type    TokenType
	Literal string
	Line    int
	Column  int
}

var KeywardsType = map[string]TokenType{
	"let":    LET,
	"fn":     FUNC,
	"if":     IF,
	"else":   ELSE,
	"while":  WHILE,
	"return": RETURN,
	"true":   TRUE,
	"false":  FALSE,
}

func IsKeyword(token string) bool {
	return KeywardsType[token] != ""
}
