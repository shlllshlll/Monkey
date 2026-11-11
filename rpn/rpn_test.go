package rpn

import (
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/shlllshlll/monkey/ast"
	"github.com/shlllshlll/monkey/lexer"
	"github.com/shlllshlll/monkey/parser"
	"github.com/shlllshlll/monkey/token"
)

// parser 在错误路径上会 slog.Error，负例测试会刷屏，这里统一静音。
func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}

// testCorpus 同时服务于：栈平衡不变量、op 覆盖元测试、fuzz seed。
var testCorpus = []string{
	"0", "1", "1.5", "true", "false", `"hi"`,
	"1 + 2 * 3", "(1 + 2) * 3", "1 + 2 * 3 - 4 / 2",
	"-a * b", "!x", "-(-1)", "!!x",
	"a && b || c", "1 < 2", "1 <= 2", "3 > 4", "3 >= 4", "1 == 2", "1 != 2",
	"1 < 2 && 3 >= 4",
	"f()", "f(1)", "f(1, 2 + 3)", "f(g(1))",
	"xs[0]", "xs[0] + 1", `h["k"]`, "[1, 2][0]", `{"a": 1}["a"]`,
	"[]", "{}", "[1]", "[[1], [2]]", `[1, 2.5, "three"]`,
	`{"a": 1, "b": 2}`,
}

// ---------- helpers ----------

func tok(typ token.TokenType, lit string) token.Token {
	return token.Token{Type: typ, Literal: lit, Line: 1, Column: 1}
}

// parseOneExpr 解析单个表达式语句。表达式本身可以是 nil（例如解析失败被恢复），
// 这里当作前置条件直接失败，避免把解析问题混进引擎测试。
func parseOneExpr(t *testing.T, src string) *ast.Expression {
	t.Helper()

	p := parser.New(lexer.New(src), parser.Pratt)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("parse %q: %v", src, errs)
	}
	if len(prog.Statements) != 1 {
		t.Fatalf("parse %q: got %d statements, want 1", src, len(prog.Statements))
	}
	es, ok := prog.Statements[0].(*ast.ExpressionStatement)
	if !ok || es.Expression == nil {
		t.Fatalf("parse %q: statement is not an expression", src)
	}
	return &es.Expression
}

// lower 只测 AST -> RPN，降低阶段自身报错直接失败。
func lower(t *testing.T, src string) []Instruction {
	t.Helper()

	e := &RPNEngine{}
	e.ASTToRPN(parseOneExpr(t, src))
	if len(e.errors) != 0 {
		t.Fatalf("lower %q: %v", src, e.errors)
	}
	return e.RPNProgram
}

// evalSource 走完整链路 AST -> RPN -> Exec。
func evalSource(t *testing.T, src string) (any, error) {
	t.Helper()

	e := &RPNEngine{}
	e.ASTToRPN(parseOneExpr(t, src))
	return e.Exec()
}

func p(op OpType, operand any) Instruction {
	return Instruction{Op: op, Operand: operand}
}

func code(in ...Instruction) []Instruction { return in }

// wantErrContains 统一错误断言：只锁关键子串，不锁完整文本。
func wantErrContains(t *testing.T, err error, sub string) {
	t.Helper()
	if err == nil {
		t.Fatalf("want error containing %q, got nil", sub)
	}
	if !strings.Contains(err.Error(), sub) {
		t.Fatalf("err = %q, want substring %q", err.Error(), sub)
	}
}

// ---------- 模式 A：AST -> RPN 指令快照 ----------

func TestASTToRPNSnapshots(t *testing.T) {
	tests := []struct {
		input string
		want  []Instruction
	}{
		// 字面量
		{"1", code(p(PUSH_INT, 1))},
		{"1.5", code(p(PUSH_FLOAT, float32(1.5)))},
		{"true", code(p(PUSH_BOOL, true))},
		{`"hi"`, code(p(PUSH_STRING, "hi"))},
		{"x", code(p(LOAD, "x"))},

		// 优先级 / 结合性 / 括号
		{"1 + 2 * 3", code(
			p(PUSH_INT, 1), p(PUSH_INT, 2), p(PUSH_INT, 3), p(MUL, nil), p(ADD, nil))},
		{"(1 + 2) * 3", code(
			p(PUSH_INT, 1), p(PUSH_INT, 2), p(ADD, nil), p(PUSH_INT, 3), p(MUL, nil))},
		{"1 + 2 * 3 - 4 / 2", code(
			p(PUSH_INT, 1), p(PUSH_INT, 2), p(PUSH_INT, 3), p(MUL, nil), p(ADD, nil),
			p(PUSH_INT, 4), p(PUSH_INT, 2), p(DIV, nil), p(SUB, nil))},
		{"((((1))))", code(p(PUSH_INT, 1))}, // 分组括号不产生任何指令

		// 前缀
		{"-a * b", code(
			p(LOAD, "a"), p(NEG, nil), p(LOAD, "b"), p(MUL, nil))},
		{"!x", code(p(LOAD, "x"), p(NOT, nil))},
		{"-(-1)", code(p(PUSH_INT, 1), p(NEG, nil), p(NEG, nil))},
		{"!!x", code(p(LOAD, "x"), p(NOT, nil), p(NOT, nil))},

		// 逻辑
		{"a && b || c", code(
			p(LOAD, "a"), p(LOAD, "b"), p(AND, nil),
			p(LOAD, "c"), p(OR, nil))},

		// 比较：< 和 <= 在发射阶段交换左右操作数，改用 GT / GE
		{"1 < 2", code(p(PUSH_INT, 2), p(PUSH_INT, 1), p(GT, nil))},
		{"1 <= 2", code(p(PUSH_INT, 2), p(PUSH_INT, 1), p(GE, nil))},
		{"3 > 4", code(p(PUSH_INT, 3), p(PUSH_INT, 4), p(GT, nil))},
		{"3 >= 4", code(p(PUSH_INT, 3), p(PUSH_INT, 4), p(GE, nil))},
		{"1 == 2", code(p(PUSH_INT, 1), p(PUSH_INT, 2), p(EQ, nil))},
		{"1 != 2", code(p(PUSH_INT, 1), p(PUSH_INT, 2), p(NEQ, nil))},

		// 调用：callee 的指令必须先于实参入栈
		{"f()", code(p(LOAD, "f"), p(CALL, 0))},
		{"f(1, 2 + 3)", code(
			p(LOAD, "f"), p(PUSH_INT, 1),
			p(PUSH_INT, 2), p(PUSH_INT, 3), p(ADD, nil), p(CALL, 2))},
		{"f(g(1))", code(
			p(LOAD, "f"), p(LOAD, "g"), p(PUSH_INT, 1), p(CALL, 1), p(CALL, 1))},

		// 索引：被索引对象必须先在栈上
		{"xs[0] + 1", code(
			p(LOAD, "xs"), p(PUSH_INT, 0), p(INDEX, nil),
			p(PUSH_INT, 1), p(ADD, nil))},
		{`h["k"]`, code(
			p(LOAD, "h"), p(PUSH_STRING, "k"), p(INDEX, nil))},
		{"[1, 2][0]", code(
			p(PUSH_INT, 1), p(PUSH_INT, 2), p(MAKE_ARRAY, 2),
			p(PUSH_INT, 0), p(INDEX, nil))},

		// 复合字面量
		{"[1, 2, \"x\"]", code(
			p(PUSH_INT, 1), p(PUSH_INT, 2), p(PUSH_STRING, "x"), p(MAKE_ARRAY, 3))},
		{"[]", code(p(MAKE_ARRAY, 0))},
		{`{"a": 1, "b": 2}`, code(
			p(PUSH_STRING, "a"), p(PUSH_INT, 1),
			p(PUSH_STRING, "b"), p(PUSH_INT, 2), p(MAKE_HASH, 2))},
		{"{}", code(p(MAKE_HASH, 0))},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := lower(t, tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("program mismatch\n got: %v\nwant: %v", got, tt.want)
			}
		})
	}
}

// 指令操作数的具体类型也是契约：MAKE_* / CALL 的个数必须是 int。
func TestASTToRPNOperandTypes(t *testing.T) {
	for _, src := range testCorpus {
		for i, in := range lower(t, src) {
			switch in.Op {
			case PUSH_INT:
				if _, ok := in.Operand.(int); !ok {
					t.Errorf("%s: instr %d PUSH_INT operand = %T, want int", src, i, in.Operand)
				}
			case PUSH_FLOAT:
				if _, ok := in.Operand.(float32); !ok {
					t.Errorf("%s: instr %d PUSH_FLOAT operand = %T, want float32", src, i, in.Operand)
				}
			case PUSH_BOOL:
				if _, ok := in.Operand.(bool); !ok {
					t.Errorf("%s: instr %d PUSH_BOOL operand = %T, want bool", src, i, in.Operand)
				}
			case PUSH_STRING, LOAD:
				if _, ok := in.Operand.(string); !ok {
					t.Errorf("%s: instr %d %s operand = %T, want string", src, i, in.Op, in.Operand)
				}
			case CALL, MAKE_ARRAY, MAKE_HASH:
				n, ok := in.Operand.(int)
				if !ok || n < 0 {
					t.Errorf("%s: instr %d %s operand = %v (%T), want non-negative int",
						src, i, in.Op, in.Operand, in.Operand)
				}
			default:
				if in.Operand != nil {
					t.Errorf("%s: instr %d %s operand = %v, want nil", src, i, in.Op, in.Operand)
				}
			}
		}
	}
}

// ---------- 降低阶段错误分支（parser 造不出来的 AST，手工构造） ----------

func TestASTToRPNRecordsErrors(t *testing.T) {
	intExpr := func(v int) ast.Expression {
		return &ast.IntegerLiteral{Token: tok(token.INT, fmt.Sprint(v)), Value: v}
	}

	tests := []struct {
		name    string
		expr    ast.Expression
		wantErr string
	}{
		{
			name: "unsupported prefix operator",
			expr: &ast.PrefixExpression{
				Token: tok(token.ILLEGAL, "~"),
				Right: intExpr(1),
			},
			wantErr: "unsupported prefix expression operation type",
		},
		{
			name: "unsupported infix operator",
			expr: &ast.InfixExpression{
				Token: tok(token.ILLEGAL, "~"),
				Left:  intExpr(1),
				Right: intExpr(2),
			},
			wantErr: "unsupported prefix expression operation type",
		},
		{
			name: "invalid postfix operator",
			expr: &ast.PostfixExpression{
				Token:    tok(token.IDENT, "f"),
				Left:     &ast.Identifier{Token: tok(token.IDENT, "f"), Value: "f"},
				Operator: tok(token.ASSIGN, "="),
			},
			wantErr: "invalid operation type for postfix expression",
		},
		{
			name: "index with multiple expressions",
			expr: &ast.PostfixExpression{
				Token:    tok(token.IDENT, "xs"),
				Left:     &ast.Identifier{Token: tok(token.IDENT, "xs"), Value: "xs"},
				Operator: tok(token.LEFT_BRACKET, "["),
				ValueList: []ast.Expression{
					&ast.IntegerLiteral{Token: tok(token.INT, "0"), Value: 0},
					&ast.IntegerLiteral{Token: tok(token.INT, "1"), Value: 1},
				},
			},
			wantErr: "only one index expression can be accepted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &RPNEngine{}
			e.ASTToRPN(&tt.expr)
			_, err := e.Exec()
			wantErrContains(t, err, tt.wantErr)
		})
	}
}

// 多个错误必须全部聚合返回，而不是只留第一个。
func TestASTToRPNJoinsAllErrors(t *testing.T) {
	inner := ast.Expression(&ast.PrefixExpression{
		Token: tok(token.ILLEGAL, "~"),
		Right: &ast.IntegerLiteral{Token: tok(token.INT, "1"), Value: 1},
	})
	expr := ast.Expression(&ast.PrefixExpression{
		Token: tok(token.ILLEGAL, "~"),
		Right: inner,
	})

	e := &RPNEngine{}
	e.ASTToRPN(&expr)
	if len(e.errors) != 2 {
		t.Fatalf("got %d errors, want 2: %v", len(e.errors), e.errors)
	}
	_, err := e.Exec()
	if err == nil {
		t.Fatal("want joined error, got nil")
	}
	if got := strings.Count(err.Error(), "unsupported prefix expression operation type"); got != 2 {
		t.Fatalf("joined error contains %d messages, want 2: %v", got, err)
	}
}

// 空表达式：不发射任何指令，最后由栈检查报错，而不是 panic。
func TestASTToRPNNilExpression(t *testing.T) {
	var expr ast.Expression

	e := &RPNEngine{}
	e.ASTToRPN(&expr)
	if len(e.RPNProgram) != 0 {
		t.Fatalf("got %d instructions, want 0", len(e.RPNProgram))
	}
	_, err := e.Exec()
	wantErrContains(t, err, "values remain in the stack")
}

// ---------- 模式 B：源码 -> 求值 ----------

func TestEvalFromSource(t *testing.T) {
	tests := []struct {
		input   string
		want    any
		wantErr string
	}{
		// 字面量保持原始类型；经过运算后统一成 float64
		{"1", int(1), ""},
		{"1.5", float32(1.5), ""},
		{"true", true, ""},
		{`"hi"`, "hi", ""},

		// 算术（回归：float64 结果必须能继续参与运算）
		{"1 + 2", float64(3), ""},
		{"10 - 3", float64(7), ""},
		{"2 * 3", float64(6), ""},
		{"7 / 2", 3.5, ""},
		{"(1 + 2) * 3", float64(9), ""},
		{"1 + 2 * 3 - 4 / 2", float64(5), ""},
		{"-(1 + 2)", float64(-3), ""},
		{"-(-1)", float64(1), ""},
		{"true + true", float64(2), ""},

		// 除零不做检查
		{"1 / 0", math.Inf(1), ""},
		{"-1 / 0", math.Inf(-1), ""},
		{"0 / 0", math.NaN(), ""},

		// 真值
		{"!0", true, ""},
		{"!1", false, ""},
		{"!0.0", true, ""},
		{"!(0 / 0)", true, ""}, // NaN 视为 false
		{"1 && 0", false, ""},
		{"1 || 0", true, ""},
		{"true && false", false, ""},
		{"true || false", true, ""},

		// 比较边界（回归：< 和 <= 的严格性）
		{"1 < 2", true, ""},
		{"1 < 1", false, ""},
		{"2 < 1", false, ""},
		{"1 <= 1", true, ""},
		{"1 <= 2", true, ""},
		{"2 <= 1", false, ""},
		{"2 > 1", true, ""},
		{"1 >= 1", true, ""},
		{"1 >= 2", false, ""},
		{"1 == 1", true, ""},
		{"1 == 2", false, ""},
		{"1 != 1", false, ""},
		{"1 != 2", true, ""},
		{"1 < 2 && 3 >= 4", false, ""},

		// float32 字面量的精度语义：0.1/0.2 先按 float32 截断，再转 float64 相加。
		// float32(0.1)+float32(0.2) = 0.30000000447 < float32(0.3) = 0.30000001192
		{"0.1 + 0.2 == 0.3", false, ""},
		{"0.1 + 0.2 > 0.3", false, ""},
		{"0.1 + 0.2 < 0.3", true, ""},

		// 字符串不参与算术/比较
		{`"a" + "b"`, nil, "string connot do arithmetic"},
		{`"a" == "a"`, nil, "string connot do arithmetic"},

		// 数组/哈希
		{"[1, 2]", []any{1, 2}, ""},
		{"[1]", []any{1}, ""},
		{"[]", []any{}, ""},
		{`{"a": 1}`, map[any]any{"a": 1}, ""},
		{"{}", map[any]any{}, ""},
		{"[1, 2][0]", int(1), ""},
		{"[1, 2][1]", int(2), ""},
		{`{"a": 1}["a"]`, int(1), ""},
		{"[[1], [2]][1]", []any{2}, ""},

		// 尚未实现的指令
		// f() 的指令是 LOAD f, CALL 0，所以会先在 LOAD 上失败
		{"x", nil, "op LOAD not supported"},
		{"f()", nil, "op LOAD not supported"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := evalSource(t, tt.input)

			if tt.wantErr != "" {
				wantErrContains(t, err, tt.wantErr)
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}

			if wantF, ok := tt.want.(float64); ok && math.IsNaN(wantF) {
				gotF, ok := got.(float64)
				if !ok || !math.IsNaN(gotF) {
					t.Fatalf("got %v (%T), want NaN", got, got)
				}
				return
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v (%T), want %v (%T)", got, got, tt.want, tt.want)
			}
		})
	}
}

// ---------- 模式 C/D：直接构造指令，覆盖 AST 到不了的分支 ----------

func TestExecInstructionTable(t *testing.T) {
	tests := []struct {
		name    string
		code    []Instruction
		want    any
		wantErr string
	}{
		// 成功路径：每个 op 至少一条
		{"push-int", code(p(PUSH_INT, 7)), int(7), ""},
		{"push-float", code(p(PUSH_FLOAT, float32(1.5))), float32(1.5), ""},
		{"push-bool", code(p(PUSH_BOOL, false)), false, ""},
		{"push-string", code(p(PUSH_STRING, "s")), "s", ""},
		{"add", code(p(PUSH_INT, 2), p(PUSH_INT, 3), p(ADD, nil)), float64(5), ""},
		{"sub-order", code(p(PUSH_INT, 10), p(PUSH_INT, 3), p(SUB, nil)), float64(7), ""},
		{"mul", code(p(PUSH_INT, 4), p(PUSH_INT, 5), p(MUL, nil)), float64(20), ""},
		{"div-order", code(p(PUSH_INT, 10), p(PUSH_INT, 4), p(DIV, nil)), 2.5, ""},
		{"eq", code(p(PUSH_INT, 1), p(PUSH_FLOAT, float32(1)), p(EQ, nil)), true, ""},
		{"neq", code(p(PUSH_INT, 1), p(PUSH_INT, 2), p(NEQ, nil)), true, ""},
		{"gt", code(p(PUSH_INT, 2), p(PUSH_INT, 1), p(GT, nil)), true, ""},
		{"ge", code(p(PUSH_INT, 1), p(PUSH_INT, 1), p(GE, nil)), true, ""},
		{"and", code(p(PUSH_BOOL, true), p(PUSH_INT, 1), p(AND, nil)), true, ""},
		{"or", code(p(PUSH_BOOL, false), p(PUSH_INT, 1), p(OR, nil)), true, ""},
		{"neg", code(p(PUSH_INT, 5), p(NEG, nil)), float64(-5), ""},
		{"not", code(p(PUSH_INT, 0), p(NOT, nil)), true, ""},
		{"make-array", code(p(PUSH_INT, 1), p(PUSH_INT, 2), p(MAKE_ARRAY, 2)), []any{1, 2}, ""},
		{"make-array-empty", code(p(MAKE_ARRAY, 0)), []any{}, ""},
		{"make-hash", code(
			p(PUSH_STRING, "a"), p(PUSH_INT, 1), p(MAKE_HASH, 1)), map[any]any{"a": 1}, ""},
		{"make-hash-empty", code(p(MAKE_HASH, 0)), map[any]any{}, ""},
		{"index-array", code(
			p(PUSH_INT, 7), p(PUSH_INT, 8), p(MAKE_ARRAY, 2),
			p(PUSH_INT, 1), p(INDEX, nil)), int(8), ""},
		{"index-array-float-integral", code(
			p(PUSH_INT, 7), p(PUSH_INT, 8), p(MAKE_ARRAY, 2),
			p(PUSH_FLOAT, float32(1)), p(INDEX, nil)), int(8), ""},
		{"index-hash", code(
			p(PUSH_STRING, "a"), p(PUSH_INT, 1), p(MAKE_HASH, 1),
			p(PUSH_STRING, "a"), p(INDEX, nil)), int(1), ""},

		// 契约记录：bool 会被 toInt 当成 0/1 使用
		{"index-bool-coerced", code(
			p(PUSH_INT, 7), p(PUSH_INT, 8), p(MAKE_ARRAY, 2),
			p(PUSH_BOOL, true), p(INDEX, nil)), int(8), ""},

		// 未实现指令：直接构造指令才能分别命中 LOAD / CALL
		{"load", code(p(LOAD, "x")), nil, "op LOAD not supported yet."},
		{"call", code(p(CALL, 0)), nil, "op CALL not supported yet."},

		// 栈/操作数错误
		{"underflow-binary", code(p(ADD, nil)), nil, "Get 1th valued failed"},
		{"underflow-unary", code(p(NEG, nil)), nil, "Get 1th valued failed"},
		{"underflow-index", code(p(INDEX, nil)), nil, "Get index for INDEX command failed"},
		{"leftover-values", code(p(PUSH_INT, 1), p(PUSH_INT, 2)), nil, "values remain in the stack"},
		{"empty-program", code(), nil, "0 values remain in the stack"},
		{"unknown-op", code(p(OpType("NOPE"), nil)), nil, "Unknown op"},
		{"make-array-bad-operand", code(p(MAKE_ARRAY, "3")), nil, "MAKE_ARRAY operand should by int"},
		{"make-hash-bad-operand", code(p(MAKE_HASH, nil)), nil, "MAKE_HASH operand should by int"},

		// 索引错误
		{"index-array-out-of-range", code(
			p(PUSH_INT, 1), p(PUSH_INT, 2), p(MAKE_ARRAY, 2),
			p(PUSH_INT, 5), p(INDEX, nil)), nil, "index 5 out of range for array size 2"},
		{"index-array-negative", code(
			p(PUSH_INT, 1), p(PUSH_INT, 2), p(MAKE_ARRAY, 2),
			p(PUSH_INT, -1), p(INDEX, nil)), nil, "out of range"},
		{"index-non-integer", code(
			p(PUSH_INT, 1), p(MAKE_ARRAY, 1),
			p(PUSH_FLOAT, float32(1.5)), p(INDEX, nil)), nil, "value is not interger"},
		{"index-nan", code(
			p(PUSH_INT, 1), p(MAKE_ARRAY, 1),
			p(PUSH_FLOAT, float32(math.NaN())), p(INDEX, nil)), nil, "value is Nan or Inf"},
		{"index-hash-missing-key", code(
			p(PUSH_STRING, "a"), p(PUSH_INT, 1), p(MAKE_HASH, 1),
			p(PUSH_STRING, "b"), p(INDEX, nil)), nil, "not in hash map"},
		{"index-hash-uncomparable-index", code(
			p(PUSH_STRING, "a"), p(PUSH_INT, 1), p(MAKE_HASH, 1),
			p(PUSH_INT, 2), p(MAKE_ARRAY, 1), p(INDEX, nil)), nil, "is not comparable"},
		{"index-scalar", code(p(PUSH_INT, 1), p(PUSH_INT, 0), p(INDEX, nil)), nil, "Unsupported value type"},

		// 哈希 key 不可比较：必须是错误，不能 panic
		{"make-hash-uncomparable-key", code(
			p(PUSH_INT, 1), p(MAKE_ARRAY, 1),
			p(PUSH_INT, 2), p(MAKE_HASH, 1)), nil, "is not comparable"},

		// 类型错误
		{"string-arithmetic", code(
			p(PUSH_STRING, "a"), p(PUSH_STRING, "b"), p(ADD, nil)), nil, "string connot do arithmetic"},
		{"unsupported-operand-type", code(
			p(PUSH_INT, struct{}{}), p(PUSH_INT, 1), p(ADD, nil)), nil, "unsupported type struct {}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := (&RPNEngine{RPNProgram: tt.code}).Exec()

			if tt.wantErr != "" {
				wantErrContains(t, err, tt.wantErr)
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v (%T), want %v (%T)", got, got, tt.want, tt.want)
			}
		})
	}
}

// 负数长度当前被当成 0 处理；如果以后改成报错，这里会提醒你同步契约。
func TestExecNegativeCollectionSize(t *testing.T) {
	got, err := (&RPNEngine{RPNProgram: code(p(MAKE_ARRAY, -1))}).Exec()
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if arr, ok := got.([]any); !ok || len(arr) != 0 {
		t.Fatalf("got %v (%T), want empty []any", got, got)
	}

	got, err = (&RPNEngine{RPNProgram: code(p(MAKE_HASH, -1))}).Exec()
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if m, ok := got.(map[any]any); !ok || len(m) != 0 {
		t.Fatalf("got %v (%T), want empty map[any]any", got, got)
	}
}

// ---------- 模式 F：栈平衡不变量（值无关，先于求值定位发射错误） ----------

func arityOf(in Instruction) (pops, pushes int, err error) {
	switch in.Op {
	case PUSH_INT, PUSH_FLOAT, PUSH_BOOL, PUSH_STRING, LOAD:
		return 0, 1, nil
	case ADD, SUB, MUL, DIV, EQ, NEQ, GT, GE, AND, OR, INDEX:
		return 2, 1, nil
	case NEG, NOT:
		return 1, 1, nil
	case CALL:
		n, ok := in.Operand.(int)
		if !ok || n < 0 {
			return 0, 0, fmt.Errorf("bad CALL operand %v (%T)", in.Operand, in.Operand)
		}
		return n + 1, 1, nil
	case MAKE_ARRAY:
		n, ok := in.Operand.(int)
		if !ok || n < 0 {
			return 0, 0, fmt.Errorf("bad MAKE_ARRAY operand %v (%T)", in.Operand, in.Operand)
		}
		return n, 1, nil
	case MAKE_HASH:
		n, ok := in.Operand.(int)
		if !ok || n < 0 {
			return 0, 0, fmt.Errorf("bad MAKE_HASH operand %v (%T)", in.Operand, in.Operand)
		}
		return 2 * n, 1, nil
	default:
		return 0, 0, fmt.Errorf("unknown op %q", in.Op)
	}
}

func TestStackBalanceInvariant(t *testing.T) {
	for _, src := range testCorpus {
		t.Run(src, func(t *testing.T) {
			depth := 0
			for i, in := range lower(t, src) {
				pops, pushes, err := arityOf(in)
				if err != nil {
					t.Fatalf("instr %d: %v", i, err)
				}
				if depth < pops {
					t.Fatalf("instr %d %s: needs %d operand(s), stack depth is %d",
						i, in.Op, pops, depth)
				}
				depth += pushes - pops
			}
			if depth != 1 {
				t.Fatalf("final stack depth = %d, want 1", depth)
			}
		})
	}
}

// ---------- 模式 G：op 覆盖元测试 ----------

func TestAllOpsCovered(t *testing.T) {
	allOps := []OpType{
		PUSH_INT, PUSH_FLOAT, PUSH_BOOL, PUSH_STRING, LOAD,
		ADD, SUB, MUL, DIV, EQ, NEQ, GT, GE, AND, OR, NEG, NOT,
		CALL, INDEX, MAKE_ARRAY, MAKE_HASH,
	}

	seen := map[OpType]bool{}
	for _, src := range testCorpus {
		for _, in := range lower(t, src) {
			seen[in.Op] = true
		}
	}

	for _, op := range allOps {
		if !seen[op] {
			t.Errorf("op %s never appears in testCorpus", op)
		}
	}
}

// ---------- 模式 H：fuzz（只断言不 panic，不约束语义） ----------

func FuzzASTToRPNAndExec(f *testing.F) {
	for _, src := range testCorpus {
		f.Add(src)
	}

	f.Fuzz(func(t *testing.T, src string) {
		p := parser.New(lexer.New(src), parser.Pratt)
		prog := p.ParseProgram()
		if len(p.Errors()) != 0 || len(prog.Statements) == 0 {
			return
		}
		es, ok := prog.Statements[0].(*ast.ExpressionStatement)
		if !ok || es.Expression == nil {
			return
		}

		e := &RPNEngine{}
		e.ASTToRPN(&es.Expression) // 只允许记录错误
		_, _ = e.Exec()            // 只允许返回错误
	})
}
