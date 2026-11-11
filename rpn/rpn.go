package rpn

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"reflect"

	"github.com/shlllshlll/monkey/ast"
	"github.com/shlllshlll/monkey/parser"
	"github.com/shlllshlll/monkey/token"
	"github.com/shlllshlll/monkey/util"
)

type OpType string

const (
	PUSH_INT    = "PUSH_INT"
	PUSH_FLOAT  = "PUSH_FLOAT"
	PUSH_BOOL   = "PUSH_BOOL"
	PUSH_STRING = "PUSH_STRING"
	LOAD        = "LOAD"
	ADD         = "ADD"
	SUB         = "SUB"
	MUL         = "MUL"
	DIV         = "DIV"
	EQ          = "EQ"
	NEQ         = "NEQ"
	GT          = "GT"
	GE          = "GE"
	AND         = "AND"
	OR          = "OR"
	NEG         = "NEG"
	NOT         = "NOT"
	CALL        = "CALL"
	INDEX       = "INDEX"
	MAKE_ARRAY  = "MAKE_ARRAY"
	MAKE_HASH   = "MAKE_HASH"
)

type Instruction struct {
	Op      OpType // 操作符
	Operand any    // 操作数 int/float/bool/string 字面值，或整数（CALL 的参数个数等）
}

func toFloat64(v any) (float64, error) {
	switch val := v.(type) {
	case int:
		return float64(val), nil
	case float32:
		return float64(val), nil
	case float64:
		return val, nil
	case bool:
		if val {
			return 1, nil
		} else {
			return 0, nil
		}
	case string:
		return 0, errors.New("string connot do arithmetic")
	default:
		return 0, fmt.Errorf("unsupported type %T", v)
	}
}

func floatToBool[T ~float32 | ~float64](v T) bool {
	if math.IsNaN(float64(v)) {
		return false
	}

	return v != 0
}

func floatToInt[T ~float32 | ~float64](v T) (int, error) {
	f := float64(v)
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, errors.New("value is Nan or Inf")
	}
	if f != math.Trunc(f) {
		return 0, errors.New("value is not interger")
	}

	if f < float64(math.MaxInt) && f > float64(math.MinInt) {
		return int(f), nil
	}

	return 0, errors.New("value is out of range for int")
}

func toInt(v any) (int, error) {
	f, err := toFloat64(v)
	if err != nil {
		return 0, err
	}
	i, err := floatToInt(f)
	return i, err
}

func isComparable(v any) bool {
	if v == nil {
		return false
	}

	t := reflect.TypeOf(v)
	return t.Comparable()
}

type RPNEngine struct {
	RPNProgram []Instruction
	errors     []parser.Error
}

func (p *RPNEngine) newError(message string, got token.Token) {
	slog.Error("[RPNEngine] error", "message", message, "token", got)
	err := parser.Error{
		Line:    got.Line,
		Column:  got.Column,
		Message: message,
		Got:     got,
	}
	p.errors = append(p.errors, err)
}

func (p *RPNEngine) emit(op OpType, operand any) {
	p.RPNProgram = append(p.RPNProgram, Instruction{Op: op, Operand: operand})
}

func (p *RPNEngine) ASTToRPN(expr *ast.Expression) {
	// 后序遍历将AST转为RPN指令
	switch node := (*expr).(type) {
	case *ast.IntegerLiteral:
		p.emit(PUSH_INT, node.Value)
	case *ast.FloatLiteral:
		p.emit(PUSH_FLOAT, node.Value)
	case *ast.BooleanLiteral:
		p.emit(PUSH_BOOL, node.Value)
	case *ast.StringLiteral:
		p.emit(PUSH_STRING, node.Value)
	case *ast.Identifier:
		p.emit(LOAD, node.Value)
	case *ast.PrefixExpression:
		p.ASTToRPN(&node.Right)
		switch node.Token.Type {
		case token.NOT:
			p.emit(NOT, nil)
		case token.MINUS:
			p.emit(NEG, nil)
		default:
			p.newError(fmt.Sprintf("unsupported prefix expression operation type: %s", node.Token.Type), node.Token)
		}
	case *ast.InfixExpression:
		if node.Token.Type == token.LT || node.Token.Type == token.LE {
			p.ASTToRPN(&node.Right)
			p.ASTToRPN(&node.Left)
		} else {
			p.ASTToRPN(&node.Left)
			p.ASTToRPN(&node.Right)
		}
		switch node.Token.Type {
		case token.PLUS:
			p.emit(ADD, nil)
		case token.MINUS:
			p.emit(SUB, nil)
		case token.MULTI:
			p.emit(MUL, nil)
		case token.DIVIDE:
			p.emit(DIV, nil)
		case token.OR:
			p.emit(OR, nil)
		case token.AND:
			p.emit(AND, nil)
		case token.GT:
			p.emit(GT, nil)
		case token.GE:
			p.emit(GE, nil)
		case token.LT:
			p.emit(GT, nil)
		case token.LE:
			p.emit(GE, nil)
		case token.EQUAL:
			p.emit(EQ, nil)
		case token.NOT_EQUAL:
			p.emit(NEQ, nil)
		default:
			p.newError(fmt.Sprintf("unsupported prefix expression operation type: %s", node.Token.Type), node.Token)
		}
	case *ast.Array:
		for idx := range node.Elements {
			element := &node.Elements[idx]
			p.ASTToRPN(element)
		}

		p.emit(MAKE_ARRAY, len(node.Elements))
	case *ast.HashList:
		for idx := range node.Pairs {
			pair := &node.Pairs[idx]
			p.ASTToRPN(&pair.Key)
			p.ASTToRPN(&pair.Value)
		}

		p.emit(MAKE_HASH, len(node.Pairs))
	case *ast.PostfixExpression:
		p.ASTToRPN(&node.Left)
		switch node.Operator.Type {
		case token.LEFT_PAREN:
			// function call
			for idx := range node.ValueList {
				value := &node.ValueList[idx]
				p.ASTToRPN(value)
			}
			p.emit(CALL, len(node.ValueList))
		case token.LEFT_BRACKET:
			// array or hash index
			if len(node.ValueList) != 1 {
				p.newError(fmt.Sprintf("only one index expression can be accepted, but got: %d", len(node.ValueList)), node.Operator)
			} else {
				p.ASTToRPN(&node.ValueList[0])
				p.emit(INDEX, nil)
			}
		default:
			p.newError("invalid operation type for postfix expression", node.Operator)
		}
	}
}

func (p *RPNEngine) Exec() (any, error) {
	// TODO: 支持内置函数/变量上下文
	// TODO：当前统一转换为float64计算，后续支持分类型计算
	if len(p.errors) > 0 {
		errs := make([]error, len(p.errors))
		for idx := range p.errors {
			errs[idx] = p.errors[idx]
		}

		return nil, errors.Join(errs...)
	}

	stack := util.Stack[any]{}
	pop_vals := func(n int) ([]any, error) {
		values := []any{}
		for idx := 0; idx < n; idx++ {
			ret, popped := stack.Pop()
			if !popped {
				return nil, fmt.Errorf("Get %dth valued failed", idx+1)
			}
			values = append([]any{ret}, values...)
		}

		return values, nil
	}

	// 依次扫描relay-cli proxy
	for idx := range p.RPNProgram {
		instruct := &p.RPNProgram[idx]
		op, operand := instruct.Op, instruct.Operand
		switch op {
		case PUSH_INT:
			stack.Push(operand)
		case PUSH_FLOAT:
			stack.Push(operand)
		case PUSH_BOOL:
			stack.Push(operand)
		case PUSH_STRING:
			stack.Push(operand)
		case LOAD:
			return nil, errors.New("op LOAD not supported yet.")
		case ADD, SUB, MUL, DIV, EQ, NEQ, GT, GE, AND, OR:
			values, err := pop_vals(2)
			if err != nil {
				return nil, err
			}

			va, err := toFloat64(values[0])
			if err != nil {
				return nil, err
			}

			vb, err := toFloat64(values[1])
			if err != nil {
				return nil, err
			}

			switch op {
			case ADD:
				vc := va + vb
				stack.Push(vc)
			case SUB:
				vc := va - vb
				stack.Push(vc)
			case MUL:
				vc := va * vb
				stack.Push(vc)
			case DIV:
				vc := va / vb
				stack.Push(vc)
			case EQ:
				vc := va == vb
				stack.Push(vc)
			case NEQ:
				vc := va != vb
				stack.Push(vc)
			case GT:
				vc := va > vb
				stack.Push(vc)
			case GE:
				vc := va >= vb
				stack.Push(vc)
			case AND:
				vc := floatToBool(va) && floatToBool(vb)
				stack.Push(vc)
			case OR:
				vc := floatToBool(va) || floatToBool(vb)
				stack.Push(vc)
			default:
				return nil, fmt.Errorf("unsupported op %s type", op)
			}
		case NEG, NOT:
			values, err := pop_vals(1)
			if err != nil {
				return nil, err
			}
			va, err := toFloat64(values[0])
			if err != nil {
				return nil, err
			}
			if op == NEG {
				vb := -va
				stack.Push(vb)
			} else if op == NOT {
				vb := !floatToBool(va)
				stack.Push(vb)
			} else {
				return nil, fmt.Errorf("unsupported op %s type", op)
			}
		case CALL:
			return nil, errors.New("op CALL not supported yet.")
		case INDEX:
			idx_val, popped := stack.Pop()
			if !popped {
				return nil, errors.New("Get index for INDEX command failed.")
			}

			val, popped := stack.Pop()
			if !popped {
				return nil, errors.New("Get 1 value failed for INDEX command.")
			}

			if arr, ok := val.([]any); ok {
				idx, err := toInt(idx_val)
				if err != nil {
					return nil, err
				}
				if idx < 0 || idx >= len(arr) {
					return nil, fmt.Errorf("index %d out of range for array size %d", idx, len(arr))
				}

				val := arr[idx]
				stack.Push(val)
			} else if hash, ok := val.(map[any]any); ok {
				if !isComparable(idx_val) {
					return nil, fmt.Errorf("index type %T is not comparable", idx_val)
				}

				val, ok := hash[idx_val]
				if !ok {
					return nil, fmt.Errorf("index %v not in hash map", idx_val)
				}
				stack.Push(val)
			} else {
				return nil, fmt.Errorf("Unsupported value type: %T", val)
			}
		case MAKE_ARRAY:
			arr_len, ok := operand.(int)
			if !ok {
				return nil, fmt.Errorf("MAKE_ARRAY operand should by int, but got %T", operand)
			}

			values, err := pop_vals(arr_len)
			if err != nil {
				return nil, fmt.Errorf("Get values for MAKE_ARRAY failed: %w", err)
			}

			stack.Push(values)

		case MAKE_HASH:
			hash_len, ok := operand.(int)
			if !ok {
				return nil, fmt.Errorf("MAKE_HASH operand should by int, but got %T", operand)
			}

			values, err := pop_vals(2 * hash_len)
			if err != nil {
				return nil, fmt.Errorf("Get values for MAKE_HASH failed: %w", err)
			}

			hash_map := make(map[any]any, hash_len)
			for idx := 0; idx < hash_len; idx++ {
				start_idx := 2 * idx
				key := values[start_idx]
				val := values[start_idx+1]
				if !isComparable(key) {
					return nil, fmt.Errorf("key %v is not comparable", key)
				}
				hash_map[key] = val
			}

			stack.Push(hash_map)
		default:
			return nil, fmt.Errorf("Unknown op %s type", op)
		}
	}

	// 执行完毕应该只有一个value在stack里
	if stack.Len() != 1 {
		return nil, fmt.Errorf("%d values remain in the stack: %v", stack.Len(), stack)
	}

	val, _ := stack.Pop()
	return val, nil
}
