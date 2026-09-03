package parser

import (
	"fmt"
	"testing"

	"github.com/shlllshlll/monkey/lexer"
)

func TestParser(t *testing.T) {
	tests := []struct {
		input string
		types []string
	}{
		{`let x = 5; return x; x = x + 1;`, []string{"*ast.LetStatement", "*ast.ReturnStatement", "*ast.AssignmentStatement"}},
		{`while (x < 10) { x = x + 1; }`, []string{"*ast.WhileStatement"}},
		{`if (x < y) { x; }`, []string{"*ast.ExpressionStatement"}},
		{`if (x < y) { x; } else { y; }`, []string{"*ast.ExpressionStatement"}},
		{`if (x < y) { x; } else if (x == y) { y; } else { 0; }`, []string{"*ast.ExpressionStatement"}},
		{`let f = fn(x, y) { return x * y; }; f(2, 3);`, []string{"*ast.LetStatement", "*ast.ExpressionStatement"}},
		{`!-x + 2 * 3; 1 < 2 && 3 != 4 || 5 >= 4;`, []string{"*ast.ExpressionStatement", "*ast.ExpressionStatement"}},
		{`[1, 2.5, "three"];`, []string{"*ast.ExpressionStatement"}},
		{`{"one": 1, "two": 2};`, []string{"*ast.ExpressionStatement"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			p := New(lexer.New(tt.input), Pratt)
			program := p.ParseProgram()
			if len(p.errors) != 0 {
				t.Fatalf("parse errors: %v", p.errors)
			}
			if len(program.Statements) != len(tt.types) {
				t.Fatalf("got %d statements, want %d (%s)", len(program.Statements), len(tt.types), program.String())
			}
			for i, want := range tt.types {
				if got := fmt.Sprintf("%T", program.Statements[i]); got != want {
					t.Errorf("statement %d: got %s, want %s", i, got, want)
				}
			}
		})
	}
}

func TestParserRecordsOneErrorPerRootCauseAndRecovers(t *testing.T) {
	p := New(lexer.New("let = 1;\nlet x = 2;"), Pratt)
	program := p.ParseProgram()
	errs := p.Errors()

	if len(errs) != 1 {
		t.Fatalf("got %d errors, want 1: %v", len(errs), errs)
	}
	if got, want := errs[0].Message, "expected identifier after let"; got != want {
		t.Errorf("error message: got %q, want %q", got, want)
	}
	if got := errs[0]; got.Line != 1 || got.Column != 5 {
		t.Errorf("error position: got %d:%d, want 1:5", got.Line, got.Column)
	}
	if len(program.Statements) != 1 {
		t.Fatalf("got %d recovered statements, want 1", len(program.Statements))
	}
}

func TestParserErrataCases(t *testing.T) {
	tests := []struct{ input, want string }{
		{"true;", "true;"},
		{"!true;", "(!true);"},
		{"(1 + 2) * 3;", "((1+2)*3);"},
		{"((1));", "1;"},
		{"let a = [[1,2],[3]];", "let a = [[1,2],[3]];"},
		{"f(fn(x){x});", "f(fn (x){x;});"},
		{"xs[0][1];", "xs[0][1];"},
		{"f(1)[0];", "f(1)[0];"},
		{"[];", "[];"},
		{"{};", "{};"},
		{"let f = fn(){}; f();", "let f = fn (){};f();"},
		{"if (a) { 1 } else if (b) { 2 };", "if (a){1;}else if (b){2;};"},
		{"if (a) {} else {}", "if (a){}else{};"},
		{"{\"a\": 1, \"a\": 2};", "{a:1,a:2};"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			p := New(lexer.New(tt.input), Pratt)
			prog := p.ParseProgram()
			if len(p.Errors()) != 0 {
				t.Fatalf("parse errors: %v", p.Errors())
			}
			if got := prog.String(); got != tt.want {
				t.Fatalf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParserRecoveryKeepsFollowingStatements(t *testing.T) {
	p := New(lexer.New("let = 5; let a = 1; let b = 2;"), Pratt)
	prog := p.ParseProgram()
	if prog == nil || len(prog.Statements) != 2 || len(p.Errors()) != 1 {
		t.Fatalf("got program=%v statements/errors=%d/%d, want 2 statements and 1 error", prog, len(prog.Statements), len(p.Errors()))
	}
}
