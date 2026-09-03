package ast

import (
	"fmt"
	"strings"

	"github.com/shlllshlll/monkey/token"
)

// 通用的AST Node定义
type Node interface {
	TokenLiteral() string
	String() string
}

type Expression interface {
	Node
	expressionNode()
}

type Statement interface {
	Node
	statementNode()
}

type Program struct {
	Statements []Statement
}

func (p *Program) TokenLiteral() string {
	if len(p.Statements) > 0 {
		return p.Statements[0].TokenLiteral()
	} else {
		return ""
	}
}

func (p *Program) String() string {
	sb := strings.Builder{}

	for _, stat := range p.Statements {
		sb.WriteString(stat.String())
	}

	return sb.String()
}

// Statement
type LetStatement struct {
	Token token.Token // the token.LET token
	Name  *Identifier
	Value Expression
}

func (ls *LetStatement) statementNode() {}
func (ls *LetStatement) TokenLiteral() string {
	return ls.Token.Literal
}
func (ls *LetStatement) String() string {
	sb := strings.Builder{}

	sb.WriteString(ls.TokenLiteral())
	sb.WriteByte(' ')
	sb.WriteString(ls.Name.String())
	sb.WriteString(" = ")

	if ls.Value != nil {
		sb.WriteString(ls.Value.String())
	}

	sb.WriteByte(';')

	return sb.String()
}

type ReturnStatement struct {
	Token       token.Token // the token.RETRUN token
	ReturnValue Expression
}

func (rs *ReturnStatement) statementNode() {}
func (rs *ReturnStatement) TokenLiteral() string {
	return rs.Token.Literal
}
func (rs *ReturnStatement) String() string {
	sb := strings.Builder{}

	sb.WriteString(rs.TokenLiteral())
	sb.WriteByte(' ')

	if rs.ReturnValue != nil {
		sb.WriteString(rs.ReturnValue.String())
	}

	sb.WriteByte(';')

	return sb.String()
}

type ExpressionStatement struct {
	Token      token.Token // first toekn for the expression
	Expression Expression
}

func (es *ExpressionStatement) statementNode() {}
func (es *ExpressionStatement) TokenLiteral() string {
	return es.Token.Literal
}
func (es *ExpressionStatement) String() string {
	sb := strings.Builder{}

	if es.Expression != nil {
		sb.WriteString(es.Expression.String())
	}
	sb.WriteByte(';')

	return sb.String()
}

type BlockStatement struct {
	Token      token.Token // the token.LEFT_BRACE token
	Statements []Statement
}

func (bs *BlockStatement) statementNode() {}
func (bs *BlockStatement) TokenLiteral() string {
	return bs.Token.Literal
}
func (bs *BlockStatement) String() string {
	sb := strings.Builder{}

	sb.WriteString(bs.TokenLiteral())

	for _, stat := range bs.Statements {
		sb.WriteString(stat.String())
	}

	sb.WriteByte('}')

	return sb.String()
}

type AssignmentStatement struct {
	Token token.Token // first identifier token
	Value Expression
}

func (as *AssignmentStatement) statementNode()       {}
func (as *AssignmentStatement) TokenLiteral() string { return as.Token.Literal }
func (as *AssignmentStatement) String() string {
	sb := strings.Builder{}

	sb.WriteString(as.TokenLiteral())
	sb.WriteByte('=')
	sb.WriteString(as.Value.String())

	return sb.String()
}

type WhileStatement struct {
	Token     token.Token // the token.WHILE Eoken
	Condition Expression
	Body      Statement
}

func (ws *WhileStatement) statementNode()       {}
func (ws *WhileStatement) TokenLiteral() string { return ws.Token.Literal }
func (ws *WhileStatement) String() string {
	sb := strings.Builder{}

	sb.WriteString(ws.TokenLiteral())
	sb.WriteByte('(')
	sb.WriteString(ws.Condition.String())
	sb.WriteByte(')')
	sb.WriteString(ws.Body.String())

	return sb.String()
}

// Expressions
type Identifier struct {
	Token token.Token // the token.IDENT token
	Value string
}

func (id *Identifier) expressionNode() {}
func (id *Identifier) TokenLiteral() string {
	return id.Token.Literal
}
func (id *Identifier) String() string {
	return id.Value
}

type IntegerLiteral struct {
	Token token.Token
	Value int
}

func (il *IntegerLiteral) expressionNode() {}
func (il *IntegerLiteral) TokenLiteral() string {
	return il.Token.Literal
}
func (il *IntegerLiteral) String() string {
	return il.Token.Literal
}

type FloatLiteral struct {
	Token token.Token
	Value float32
}

func (fl *FloatLiteral) expressionNode() {}
func (fl *FloatLiteral) TokenLiteral() string {
	return fl.Token.Literal
}
func (fl *FloatLiteral) String() string {
	return fl.Token.Literal
}

type BooleanLiteral struct {
	Token token.Token
	Value bool
}

func (bl *BooleanLiteral) expressionNode() {}
func (bl *BooleanLiteral) TokenLiteral() string {
	return bl.Token.Literal
}
func (bl *BooleanLiteral) String() string {
	return bl.Token.Literal
}

type StringLiteral struct {
	Token token.Token
	Value string
}

func (sl *StringLiteral) expressionNode() {}
func (sl *StringLiteral) TokenLiteral() string {
	return sl.Token.Literal
}
func (sl *StringLiteral) String() string {
	return sl.Token.Literal
}

type Array struct {
	Token    token.Token
	Elements []Expression
}

func (a *Array) expressionNode() {}
func (a *Array) TokenLiteral() string {
	return a.Token.Literal
}
func (a *Array) String() string {
	sb := strings.Builder{}

	sb.WriteByte('[')
	for i, e := range a.Elements {
		if i != 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(e.String())
	}
	sb.WriteByte(']')

	return sb.String()
}

type HashPair struct {
	Key   Expression
	Value Expression
}

type HashList struct {
	Token token.Token
	Pairs []HashPair
}

func (hl *HashList) expressionNode() {}
func (hl *HashList) TokenLiteral() string {
	return hl.Token.Literal
}
func (hl *HashList) String() string {
	sb := strings.Builder{}

	sb.WriteByte('{')

	idx := 0
	for _, pair := range hl.Pairs {
		if idx != 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(pair.Key.String())
		sb.WriteByte(':')
		sb.WriteString(pair.Value.String())
		idx++
	}
	sb.WriteByte('}')

	return sb.String()
}

type IfBranch struct {
	Condition Expression
	Body      *BlockStatement
}

type IfExpression struct {
	Token token.Token

	Branches []IfBranch
}

func (ie *IfExpression) expressionNode() {}
func (ie *IfExpression) TokenLiteral() string {
	return ie.Token.Literal
}
func (ie *IfExpression) String() string {
	sb := strings.Builder{}

	for i, b := range ie.Branches {
		if b.Condition != nil {
			if i == 0 {
				sb.WriteString("if (")
			} else {
				sb.WriteString("else if (")
			}
			sb.WriteString(b.Condition.String())
			sb.WriteByte(')')
		} else {
			sb.WriteString("else")
		}
		sb.WriteString(b.Body.String())
	}

	return sb.String()
}

type FunctionExpression struct {
	Token      token.Token
	Parameters []Identifier
	Body       *BlockStatement
}

func (fe *FunctionExpression) expressionNode() {}
func (fe *FunctionExpression) TokenLiteral() string {
	return fe.Token.Literal
}
func (fe *FunctionExpression) String() string {
	sb := strings.Builder{}

	sb.WriteString("fn (")
	for i, p := range fe.Parameters {
		if i != 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(p.String())
	}
	sb.WriteByte(')')
	sb.WriteString(fe.Body.String())

	return sb.String()
}

type PrefixExpression struct {
	Token token.Token
	Right Expression
}

func (pe *PrefixExpression) expressionNode() {}
func (pe *PrefixExpression) TokenLiteral() string {
	return pe.Token.Literal
}
func (pe *PrefixExpression) String() string {
	return fmt.Sprintf("(%s%s)", pe.TokenLiteral(), pe.Right.String())
}

type InfixExpression struct {
	Token token.Token
	Left  Expression
	Right Expression
}

func (ie *InfixExpression) expressionNode() {}
func (ie *InfixExpression) TokenLiteral() string {
	return ie.Token.Literal
}
func (ie *InfixExpression) String() string {
	return fmt.Sprintf("(%s%s%s)", ie.Left.String(), ie.TokenLiteral(), ie.Right.String())
}

type PostfixExpression struct {
	Token     token.Token
	Left      Expression
	Operator  token.Token
	ValueList []Expression
}

func (pf *PostfixExpression) expressionNode() {}
func (pf *PostfixExpression) TokenLiteral() string {
	return pf.Token.Literal
}
func (pf *PostfixExpression) String() string {
	sb := strings.Builder{}

	sb.WriteString(pf.Left.String())
	sb.WriteString(pf.Operator.Literal)
	for i, v := range pf.ValueList {
		if i != 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(v.String())
	}

	switch pf.Operator.Type {
	case token.LEFT_PAREN:
		sb.WriteByte(')')
	default:
		sb.WriteByte(']')
	}

	return sb.String()
}
