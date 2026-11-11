package parser

import (
	"fmt"
	"log/slog"
	"strconv"

	"github.com/shlllshlll/monkey/ast"
	"github.com/shlllshlll/monkey/lexer"
	"github.com/shlllshlll/monkey/token"
)

type ExpressionParseMode int

const (
	Pratt = iota
	RecursiveDescent
)

type Error struct {
	Line    int
	Column  int
	Message string
	Got     token.Token
}

func (e Error) Error() string {
	return fmt.Sprintf("%d:%d: %s, got %s", e.Line, e.Column, e.Message, e.Got.Type)
}

type Parser struct {
	l      *lexer.Lexer
	errors []Error

	curToken  token.Token
	nextToken token.Token

	exprParseMode ExpressionParseMode
}

func New(l *lexer.Lexer, exprParseMode ExpressionParseMode) *Parser {
	p := &Parser{l: l, exprParseMode: exprParseMode}
	p.readToken()
	p.readToken()

	return p
}

func (p *Parser) newError(message string, got token.Token) {
	slog.Error("[Parser] parse error", "message", message, "token", got)
	err := Error{
		Line:    got.Line,
		Column:  got.Column,
		Message: message,
		Got:     got,
	}

	p.errors = append(p.errors, err)
}

func (p *Parser) Errors() []Error { return append([]Error(nil), p.errors...) }

func (p *Parser) logFailure(context string) {
	slog.Debug("[Parser] parse failure", "context", context, "token", p.curToken)
}

func (p *Parser) readToken() token.Token {
	p.curToken = p.nextToken
	p.nextToken = p.l.NextToken()
	return p.curToken
}

// program parser
func (p *Parser) ParseProgram() *ast.Program {
	program := &ast.Program{}
	program.Statements = []ast.Statement{}

	for p.curToken.Type != token.EOF {
		if p.curToken.Type == token.ILLEGAL {
			p.newError(p.curToken.Literal, p.curToken)
			p.readToken()
			continue
		}

		if stat := p.parseStatement(); stat != nil {
			program.Statements = append(program.Statements, stat)
		} else {
			p.logFailure("statement")
			p.synchronize()
		}
	}

	return program
}

func (p *Parser) synchronize() {
	for p.curToken.Type != token.EOF && p.curToken.Type != token.SEMICOLON &&
		p.curToken.Type != token.RIGHT_BRACE && p.curToken.Type != token.LET &&
		p.curToken.Type != token.RETURN && p.curToken.Type != token.WHILE &&
		p.curToken.Type != token.IF {
		p.readToken()
	}
	if p.curToken.Type == token.SEMICOLON || p.curToken.Type == token.RIGHT_BRACE {
		p.readToken()
	}
}

// statemnt parser
func (p *Parser) parseStatement() ast.Statement {
	switch p.curToken.Type {
	case token.LET:
		return p.parseLetStatement()
	case token.RETURN:
		return p.parseReturnStatement()
	case token.WHILE:
		return p.parseWhileStatement()
	default:
		if p.curToken.Type == token.IDENT && p.nextToken.Type == token.ASSIGN {
			return p.parseAssignStatement()
		} else {
			return p.parseExpressionStatement()
		}
	}
}

func (p *Parser) parseLetStatement() ast.Statement {
	stmt := ast.LetStatement{Token: p.curToken}

	curToken := p.readToken()
	if curToken.Type != token.IDENT {
		p.newError("expected identifier after let", curToken)
		return nil
	}
	stmt.Name = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}

	curToken = p.readToken()
	if curToken.Type != token.ASSIGN {
		p.newError("expected '=' after identifier", curToken)
		return nil
	}

	p.readToken()
	if expr := p.parseExpression(); expr != nil {
		stmt.Value = expr
	} else {
		p.logFailure("let value")
		return nil
	}

	if p.curToken.Type == token.SEMICOLON {
		p.readToken()
	}

	return &stmt
}

func (p *Parser) parseReturnStatement() ast.Statement {
	stmt := ast.ReturnStatement{Token: p.curToken}

	p.readToken()
	if expr := p.parseExpression(); expr != nil {
		stmt.ReturnValue = expr
	} else {
		p.logFailure("return value")
		return nil
	}

	if p.curToken.Type == token.SEMICOLON {
		p.readToken()
	}

	return &stmt
}

func (p *Parser) parseExpressionStatement() ast.Statement {
	stmt := ast.ExpressionStatement{Token: p.curToken}

	if expr := p.parseExpression(); expr != nil {
		stmt.Expression = expr
	} else {
		p.logFailure("expression statement")
		return nil
	}

	if p.curToken.Type == token.SEMICOLON {
		p.readToken()
	}

	return &stmt
}

func (p *Parser) parseBlockStatement() ast.Statement {
	stmt := ast.BlockStatement{Token: p.curToken}

	p.readToken()

	if p.curToken.Type == token.RIGHT_BRACE {
		p.readToken()
		return &stmt
	}

	for {
		if sub_stmt := p.parseStatement(); sub_stmt != nil {
			stmt.Statements = append(stmt.Statements, sub_stmt)
		} else {
			p.logFailure("block statement")
			return nil
		}

		if p.curToken.Type == token.RIGHT_BRACE {
			p.readToken()
			break
		}
	}

	return &stmt
}

func (p *Parser) parseWhileStatement() ast.Statement {
	stmt := ast.WhileStatement{Token: p.curToken}

	p.readToken()
	if p.curToken.Type != token.LEFT_PAREN {
		p.newError("expected '(' after while", p.curToken)
		return nil
	}

	p.readToken()
	if expr := p.parseExpression(); expr != nil {
		stmt.Condition = expr
	} else {
		p.logFailure("while condition")
		return nil
	}

	if p.curToken.Type != token.RIGHT_PAREN {
		p.newError("expected ')' after while condition", p.curToken)
		return nil
	}

	p.readToken()
	if body := p.parseBlockStatement(); body != nil {
		stmt.Body = body
	} else {
		p.logFailure("while body")
		return nil
	}

	return &stmt
}

func (p *Parser) parseAssignStatement() ast.Statement {
	stmt := ast.AssignmentStatement{Token: p.curToken}

	p.readToken()
	p.readToken()
	if expr := p.parseExpression(); expr != nil {
		stmt.Value = expr
	} else {
		p.logFailure("assignment value")
		return nil
	}
	if p.curToken.Type == token.SEMICOLON {
		p.readToken()
	}

	return &stmt
}

// expression parser
func (p *Parser) parseExpression() ast.Expression {
	switch p.exprParseMode {
	case Pratt:
		return p.parsePrattExpression(0)
	case RecursiveDescent:
		return p.parseExpressionRecursiveDescent()
	default:
		p.newError(fmt.Sprintf("unexpected expression parse mode: %d", p.exprParseMode), p.curToken)
	}
	return nil
}

// pratt parser
// binding poewer dedine
type BP struct {
	lbp int
	rbp int
}

var INFIX_BP = map[token.TokenType]BP{
	token.OR:  {10, 11},
	token.AND: {20, 21},

	token.EQUAL:     {30, 31},
	token.NOT_EQUAL: {30, 31},

	token.GT: {40, 41},
	token.LT: {40, 41},
	token.GE: {40, 41},
	token.LE: {40, 41},

	token.PLUS:  {50, 51},
	token.MINUS: {50, 51},

	token.MULTI:  {60, 61},
	token.DIVIDE: {60, 61},
}

const PREFIX_BP = 70

const POSTFIX_BP = 80

func (p *Parser) parsePrattExpression(minBP int) ast.Expression {
	// prefix
	left := p.parsePrattPrefixExpression()
	if left == nil {
		p.logFailure("Pratt prefix")
		return nil
	}

	for {
		// postfix: only func call
		if p.curToken.Type == token.LEFT_PAREN || p.curToken.Type == token.LEFT_BRACKET {
			if POSTFIX_BP < minBP {
				break
			}

			left = p.parsePrattPostfixExpression(left)
			continue
		}

		// infix
		bp, ok := INFIX_BP[p.curToken.Type]
		if !ok {
			break
		}
		if bp.lbp < minBP {
			break
		}

		left = p.parsePrattInfixExpression(left, bp.rbp)
	}

	return left
}

func (p *Parser) parsePrattPrefixExpression() ast.Expression {
	switch p.curToken.Type {
	case token.LEFT_PAREN:
		p.readToken()
		expr := p.parsePrattExpression(0)
		if expr == nil {
			p.logFailure("parenthesized expression")
			return nil
		}
		if p.curToken.Type != token.RIGHT_PAREN {
			p.newError("expected ')' after expression", p.curToken)
			return nil
		}
		p.readToken()
		return expr
	case token.LEFT_BRACKET:
		array := ast.Array{Token: p.curToken}
		p.readToken()

		if p.curToken.Type == token.RIGHT_BRACKET {
			p.readToken()
			return &array
		}

		for {
			expr := p.parsePrattExpression(0)
			if expr != nil {
				array.Elements = append(array.Elements, expr)
			} else {
				p.logFailure("array element")
				return nil
			}
			if p.curToken.Type == token.COMMA {
				p.readToken()
			} else if p.curToken.Type == token.RIGHT_BRACKET {
				p.readToken()
				break
			} else {
				p.newError("expected ',' or ']' after array element", p.curToken)
				return nil
			}
		}
		return &array
	case token.LEFT_BRACE:
		hashList := ast.HashList{Token: p.curToken}
		p.readToken()

		if p.curToken.Type == token.RIGHT_BRACE {
			p.readToken()
			return &hashList
		}

		for {
			key := p.parsePrattExpression(0)
			if key == nil {
				p.logFailure("hash key")
				return nil
			}

			if p.curToken.Type != token.COLON {
				p.newError("expected ':' after hash key", p.curToken)
				return nil
			}
			p.readToken()

			value := p.parsePrattExpression(0)
			if value == nil {
				p.logFailure("hash value")
				return nil
			}

			hashList.Pairs = append(hashList.Pairs, ast.HashPair{Key: key, Value: value})

			if p.curToken.Type == token.COMMA {
				p.readToken()
			} else if p.curToken.Type == token.RIGHT_BRACE {
				p.readToken()
				break
			} else {
				p.newError("expected ',' or '}' after hash pair", p.curToken)
				return nil
			}
		}

		return &hashList
	case token.IF:
		ifExpr := ast.IfExpression{Token: p.curToken}

		elseCondi := false
		for {
			if len(ifExpr.Branches) == 0 {
				// first condition
				p.readToken()
			} else {
				if p.curToken.Type == token.ELSE {
					p.readToken()
					if p.curToken.Type == token.IF {
						p.readToken()
					} else {
						elseCondi = true
					}
				} else {
					p.newError("expected 'else'", p.curToken)
					return nil
				}
			}

			var condi ast.Expression
			if !elseCondi {
				if p.curToken.Type != token.LEFT_PAREN {
					p.newError("expected '(' after if", p.curToken)
					return nil
				}
				p.readToken()
				condi = p.parsePrattExpression(0)
				if condi == nil {
					p.logFailure("if condition")
					return nil
				}
				if p.curToken.Type != token.RIGHT_PAREN {
					p.newError("expected ')' after if condition", p.curToken)
					return nil
				}
				p.readToken()
			}
			block := p.parseBlockStatement()
			if block == nil {
				p.logFailure("if block")
				return nil
			}
			ifExpr.Branches = append(ifExpr.Branches, ast.IfBranch{Condition: condi, Body: block.(*ast.BlockStatement)})

			if p.curToken.Type != token.ELSE {
				break
			}
		}

		if len(ifExpr.Branches) == 0 {
			p.newError("if expression has no branch", p.curToken)
			return nil
		}
		return &ifExpr
	case token.FUNC:
		fnExpr := ast.FunctionExpression{Token: p.curToken}
		p.readToken()

		if p.curToken.Type != token.LEFT_PAREN {
			p.newError("expected '(' after fn", p.curToken)
			return nil
		}
		p.readToken()

		if p.curToken.Type == token.RIGHT_PAREN {
			p.readToken()
		} else {
			for {
				if p.curToken.Type != token.IDENT {
					p.newError("expected parameter identifier", p.curToken)
					return nil
				}

				fnExpr.Parameters = append(fnExpr.Parameters, ast.Identifier{Token: p.curToken, Value: p.curToken.Literal})
				p.readToken()

				if p.curToken.Type == token.COMMA {
					p.readToken()
				} else if p.curToken.Type == token.RIGHT_PAREN {
					p.readToken()
					break
				} else {
					p.newError("expected ',' or ')' after parameter", p.curToken)
					return nil
				}
			}
		}

		block := p.parseBlockStatement()
		if block == nil {
			p.logFailure("function body")
			return nil
		}
		fnExpr.Body = block.(*ast.BlockStatement)

		if p.curToken.Type == token.SEMICOLON {
			p.readToken()
		}
		return &fnExpr
	case token.NOT, token.MINUS:
		expr := ast.PrefixExpression{Token: p.curToken}
		p.readToken()
		rightExpr := p.parsePrattExpression(PREFIX_BP)
		if rightExpr == nil {
			p.logFailure("prefix operand")
			return nil
		}
		expr.Right = rightExpr

		return &expr
	case token.INT:
		value, err := strconv.Atoi(p.curToken.Literal)
		if err != nil {
			p.newError("invalid integer literal", p.curToken)
			return nil
		}
		expr := ast.IntegerLiteral{Token: p.curToken, Value: value}
		p.readToken()
		return &expr
	case token.FLOAT:
		value, err := strconv.ParseFloat(p.curToken.Literal, 32)
		if err != nil {
			p.newError("invalid float literal", p.curToken)
			return nil
		}
		expr := ast.FloatLiteral{Token: p.curToken, Value: float32(value)}
		p.readToken()
		return &expr
	case token.STRING:
		expr := ast.StringLiteral{Token: p.curToken, Value: p.curToken.Literal}
		p.readToken()
		return &expr
	case token.IDENT:
		expr := ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
		p.readToken()
		return &expr
	case token.TRUE, token.FALSE:
		isTrue := false
		if p.curToken.Type == token.TRUE {
			isTrue = true
		}
		expr := ast.BooleanLiteral{Token: p.curToken, Value: isTrue}
		p.readToken()
		return &expr
	default:
		p.newError("expected expression", p.curToken)
		return nil
	}
}

func (p *Parser) parsePrattPostfixExpression(left ast.Expression) ast.Expression {
	expr := ast.PostfixExpression{Token: p.curToken, Left: left}
	switch p.curToken.Type {
	case token.LEFT_PAREN:
		// function call
		expr.Operator = p.curToken
		p.readToken()
		if p.curToken.Type == token.RIGHT_PAREN {
			p.readToken()
			return &expr
		}
		for {
			arg := p.parsePrattExpression(0)
			if arg == nil {
				p.logFailure("call argument")
				return nil
			}
			expr.ValueList = append(expr.ValueList, arg)
			if p.curToken.Type == token.COMMA {
				p.readToken()
			} else if p.curToken.Type == token.RIGHT_PAREN {
				p.readToken()
				break
			} else {
				p.newError("expected ',' or ')' after argument", p.curToken)
				return nil
			}
		}
		return &expr
	case token.LEFT_BRACKET:
		// array / hash index
		expr.Operator = p.curToken
		p.readToken()
		if p.curToken.Type == token.RIGHT_BRACKET {
			p.readToken()
			return &expr
		}
		for {
			index := p.parsePrattExpression(0)
			if index == nil {
				p.logFailure("index expression")
				return nil
			}
			expr.ValueList = append(expr.ValueList, index)
			if p.curToken.Type == token.COMMA {
				p.readToken()
			} else if p.curToken.Type == token.RIGHT_BRACKET {
				p.readToken()
				break
			} else {
				p.newError("expected ',' or ']' after index", p.curToken)
				return nil
			}
		}
		return &expr
	default:
		p.newError("unexpected postfix operator", p.curToken)
		return nil
	}
}

func (p *Parser) parsePrattInfixExpression(left ast.Expression, bp int) ast.Expression {
	// curtoken is infix token
	infixExpr := ast.InfixExpression{Token: p.curToken, Left: left}
	p.readToken()

	rightExpr := p.parsePrattExpression(bp)
	if rightExpr == nil {
		p.logFailure("infix right operand")
		return nil
	}

	infixExpr.Right = rightExpr
	return &infixExpr
}

func (p *Parser) parseExpressionRecursiveDescent() ast.Expression {
	p.newError("recursive descent parser is not implemented", p.curToken)
	return nil
}
