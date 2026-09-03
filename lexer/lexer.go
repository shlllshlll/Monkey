package lexer

import (
	"fmt"
	"strings"

	"github.com/shlllshlll/monkey/token"
)

type Lexer struct {
	Input        string
	InputRowsPos []int
	position     int  // current read positon
	readPosition int  // next read positon
	ch           byte // current character
}

func New(input string) *Lexer {
	l := &Lexer{Input: input, InputRowsPos: []int{0}}
	l.readChar()
	return l
}

func (l *Lexer) NextToken() token.Token {
	var tok token.Token
	l.skipWhiteChar()
	line := len(l.InputRowsPos)
	column := l.position - l.InputRowsPos[line-1] + 1
	advanced := false

	switch l.ch {
	case '=':
		if l.peekChar() == '=' {
			l.readChar()
			tok = token.Token{Type: token.EQUAL, Literal: "=="}
		} else {
			tok = token.Token{Type: token.ASSIGN, Literal: "="}
		}
	case '+':
		tok = token.Token{Type: token.PLUS, Literal: "+"}
	case '-':
		tok = token.Token{Type: token.MINUS, Literal: "-"}
	case '*':
		tok = token.Token{Type: token.MULTI, Literal: "*"}
	case '/':
		tok = token.Token{Type: token.DIVIDE, Literal: "/"}
	case '!':
		if l.peekChar() == '=' {
			l.readChar()
			tok = token.Token{Type: token.NOT_EQUAL, Literal: "!="}
		} else {
			tok = token.Token{Type: token.NOT, Literal: "!"}
		}
	case '>':
		if l.peekChar() == '=' {
			l.readChar()
			tok = token.Token{Type: token.GE, Literal: ">="}
		} else {
			tok = token.Token{Type: token.GT, Literal: ">"}
		}
	case '<':
		if l.peekChar() == '=' {
			l.readChar()
			tok = token.Token{Type: token.LE, Literal: "<="}
		} else {
			tok = token.Token{Type: token.LT, Literal: "<"}
		}
	case '&':
		if l.peekChar() == '&' {
			l.readChar()
			tok = token.Token{Type: token.AND, Literal: "&&"}
		} else {
			tok = token.Token{Type: token.ILLEGAL, Literal: "expected '&'"}
		}
	case '|':
		if l.peekChar() == '|' {
			l.readChar()
			tok = token.Token{Type: token.OR, Literal: "||"}
		} else {
			tok = token.Token{Type: token.ILLEGAL, Literal: "expected '|'"}
		}
	case ',':
		tok = token.Token{Type: token.COMMA, Literal: ","}
	case ':':
		tok = token.Token{Type: token.COLON, Literal: ":"}
	case ';':
		tok = token.Token{Type: token.SEMICOLON, Literal: ";"}
	case '"':
		tok = l.readString()
	case '{':
		tok = token.Token{Type: token.LEFT_BRACE, Literal: "{"}
	case '}':
		tok = token.Token{Type: token.RIGHT_BRACE, Literal: "}"}
	case '[':
		tok = token.Token{Type: token.LEFT_BRACKET, Literal: "["}
	case ']':
		tok = token.Token{Type: token.RIGHT_BRACKET, Literal: "]"}
	case '(':
		tok = token.Token{Type: token.LEFT_PAREN, Literal: "("}
	case ')':
		tok = token.Token{Type: token.RIGHT_PAREN, Literal: ")"}
	case 0:
		tok = token.Token{Type: token.EOF}
	default:
		if isLetter(l.ch) {
			ident := l.readIdentifier()
			advanced = true
			if token.IsKeyword(ident) {
				tok = token.Token{Type: token.KeywardsType[ident], Literal: ident}
			} else {
				tok = token.Token{Type: token.IDENT, Literal: ident}
			}
		} else if isDigit(l.ch) {
			isFloat := false
			position := l.position
			for isDigit(l.ch) || l.ch == '.' {
				if l.ch == '.' {
					if isFloat == true || !isDigit(l.peekChar()) {
						// multiple dot or next char is not digit => illegal
						tok = token.Token{Type: token.ILLEGAL, Literal: "Illegal float literal"}
						break
					}
					isFloat = true
				}
				l.readChar()
			}
			advanced = true

			if tok.Type != token.ILLEGAL {
				if isFloat {
					tok = token.Token{Type: token.FLOAT, Literal: l.Input[position:l.position]}
				} else {
					tok = token.Token{Type: token.INT, Literal: l.Input[position:l.position]}
				}
			}
		} else {
			tok = token.Token{Type: token.ILLEGAL, Literal: fmt.Sprintf("Unexpected token %c", l.ch)}
		}
	}

	tok.Line = line
	tok.Column = column
	if !advanced {
		l.readChar()
	}
	return tok
}

func (l *Lexer) GetLine(lno int) (string, error) {
	if lno < 1 || lno > len(l.InputRowsPos) {
		return "", fmt.Errorf("line number[%d] out of range[1-%d]", lno, len(l.InputRowsPos))
	}

	start := l.InputRowsPos[lno-1]
	end := len(l.Input)

	if lno < len(l.InputRowsPos) {
		end = l.InputRowsPos[lno]
	}

	return l.Input[start:end], nil
}

func (l *Lexer) readChar() byte {
	if l.readPosition >= len(l.Input) {
		l.ch = 0
		l.position = len(l.Input)
		return l.ch
	}

	l.ch = l.Input[l.readPosition]
	l.position = l.readPosition
	l.readPosition += 1
	return l.ch
}

func (l *Lexer) peekChar() byte {
	if l.readPosition >= len(l.Input) {
		return 0
	} else {
		return l.Input[l.readPosition]
	}
}

func (l *Lexer) readIdentifier() string {
	position := l.position
	for isLetter(l.ch) {
		l.readChar()
	}
	return l.Input[position:l.position]
}

func (l *Lexer) skipWhiteChar() {
	for l.ch == ' ' || l.ch == '\t' || l.ch == '\n' || l.ch == '\r' {
		isNewline := l.ch == '\n'
		l.readChar()
		if isNewline {
			l.InputRowsPos = append(l.InputRowsPos, l.position)
		}
	}
}

func (l *Lexer) readString() token.Token {
	sb := strings.Builder{}

	for {
		l.readChar()
		if l.ch == '"' {
			break
		} else if l.ch == 0 {
			return token.Token{Type: token.ILLEGAL, Literal: "unterminated string"}
		} else if l.ch == '\\' {
			l.readChar()
			switch l.ch {
			case 'n':
				sb.WriteByte('\n')
			case 'r':
				sb.WriteByte('\r')
			case 't':
				sb.WriteByte('\t')
			case '"':
				sb.WriteByte('"')
			case '\\':
				sb.WriteByte('\\')
			default:
				return token.Token{Type: token.ILLEGAL, Literal: "illegal escape sequence"}
			}
			continue
		}
		sb.WriteByte(l.ch)
	}
	return token.Token{Type: token.STRING, Literal: sb.String()}
}

func isLetter(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch == '_')
}

func isDigit(ch byte) bool {
	return (ch >= '0' && ch <= '9')
}
