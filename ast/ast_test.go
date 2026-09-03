package ast_test

import (
	"testing"

	"github.com/shlllshlll/monkey/lexer"
	"github.com/shlllshlll/monkey/parser"
)

func TestPrefixString(t *testing.T) {
	for _, tt := range []struct{ input, want string }{
		{"-5;", "(-5);"}, {"!x;", "(!x);"}, {"!-x;", "(!(-x));"},
	} {
		p := parser.New(lexer.New(tt.input), parser.Pratt)
		if got := p.ParseProgram().String(); got != tt.want {
			t.Errorf("%q: got %q, want %q", tt.input, got, tt.want)
		}
	}
}
