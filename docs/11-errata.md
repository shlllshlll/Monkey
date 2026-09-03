# 11 勘误：曾经的 12 个缺陷（已全部修复）

> **状态：本章记录的问题已在提交 `cc3132f` 全部修复。** `go build ./... && go vet ./... && go test ./...` 现在全绿。这一章从"当前问题清单"变成了**回顾式勘误**——内容一字未删，因为它的教学价值从未依赖"bug 还在"这个事实：下面 12 条缺陷覆盖了词法/语法分析里最典型的一批错误模式（EOF 边界、do-while 式的空列表处理、格式化字符串类型不匹配、map 遍历顺序不确定……），值得完整读一遍，把每条的**症状 → 根因 → 修法**当作案例库，而不是当作待办事项。想看实际怎么修的，直接读当前的 `parser.go`/`ast.go`/`lexer.go`——已经是修复后的版本。

这一章和其他章不同：它不讲设计，只讲**这些代码曾经错在哪**。

所有结论都来自在仓库里实跑的临时探针测试，不是阅读推断——测试针对的是修复前的代码状态,已随本次标注一并存档,不代表现在还能复现。每条都给：复现输入、实测输出、根因（精确到文件行，**行号对应修复前的版本**）、修法方向、回归测试。

先看修复前的一个事实：

```bash
cd compiler/monkey && go test ./...
```

```text
# github.com/shlllshlll/monkey/repl
repl/repl.go:29:47: undefined: lexer.EOF
FAIL	github.com/shlllshlll/monkey [build failed]
?   	github.com/shlllshlll/monkey/ast	[no test files]
ok  	github.com/shlllshlll/monkey/lexer	1.245s
ok  	github.com/shlllshlll/monkey/parser	1.971s
FAIL	github.com/shlllshlll/monkey/repl [build failed]
```

**当时测试是红的。** `lexer` 和 `parser` 两个包绿，但根包和 `repl` 编译失败。而且——这是本章最重要的一课，至今没变——那两个"绿"是假的绿：下面 12 条缺陷里有 8 条落在这两个包，当时的测试没抓住它们。

## 缺陷总览

| # | 症状 | 位置 | 严重度 |
| --- | --- | --- | --- |
| 1 | 输入末尾的标识符/数字被截断一个字符 | `lexer.go:135-145` | **致命** |
| 2 | `true` / `false` 无法解析 | `parser.go:466-509` | **致命** |
| 3 | 复合字面量无法嵌套（`[[1]]`、`f(fn(){})`） | `parser.go:297-301` | **致命** |
| 4 | 索引 `xs[0]` 被解析成两个语句 | `parser.go:271-292` | **致命** |
| 5 | 零参调用 `a()` 产生 nil 元素并 panic | `parser.go:518-519` | **致命** |
| 6 | `(1+2)*3` 括号分组无法解析 | `parser.go:466-509` | **致命** |
| 7 | `PrefixExpression.String()` 输出乱码 | `ast.go:371` | 中 |
| 8 | `IfExpression.String()` 丢 `else if` 前缀 | `ast.go:319-323` | 中 |
| 9 | 哈希 `String()` 顺序不稳定 | `ast.go:285` | 中 |
| 10 | `repl.go` 编译失败 | `repl.go:29` | **致命** |
| 11 | `ParseProgram` 无错误恢复，首错即放弃 | `parser.go:52-58` | 中 |
| 12 | `parseBlockStatement` 拒绝空块 `{ }` | `parser.go:152-164` | 中 |

另有两条设计层面的问题（`Parser.errors` 未导出、`Token.Pos` 只对部分 token 赋值）放在末尾的「附加观察」。

---

## 1. EOF 截断：最隐蔽也最致命

### 复现

```go
l := lexer.New("ab")
tok := l.NextToken()
// 实测 tok.Literal == "a"，期望 "ab"
```

实测一组输入（探针输出，格式 `类型/"字面量"/@位置`）：

```text
LEX "ab"         -> [ident/"a"/@0]
LEX "foo"        -> [ident/"fo"/@0]
LEX "12"         -> [int/"1"/@0]
LEX "x y"        -> [ident/"x"/@0  ident/""/@0]
LEX "let a = 5"  -> [let/"let"/@0  ident/"a"/@0  =/"="/@6  int/""/@0]
```

注意最后一行：`5` 变成了**空字符串**。于是：

```text
PARSE "let a = 1"  -> nil, errs=["parse integer literal failed", ...]
```

一个完全合法的 Monkey 程序，只因末尾没有分号就解析失败。

### 根因

[lexer.go:135-145](../lexer/lexer.go#L135-L145)：

```go
func (l *Lexer) readChar() byte {
	if l.readPosition >= len(l.Input) {
		l.ch = 0
		return l.ch          // ← 提前返回，position 没有推进
	}

	l.ch = l.Input[l.readPosition]
	l.position = l.readPosition
	l.readPosition += 1
	return l.ch
}
```

EOF 分支只设了 `l.ch = 0`，**没有推进 `l.position`**。而 `readIdentifier` 靠 `position` 切片：

```go
func (l *Lexer) readIdentifier() string {
	position := l.position
	for isLetter(l.ch) {
		l.readChar()
	}
	return l.Input[position:l.position]   // ← position 停在最后一个字符上，不是它之后
}
```

手工 trace `"ab"`（探针实测输出）：

```text
after New:  pos=0  readPos=1  ch='a'
readChar1:  pos=1  readPos=2  ch='b'
readChar2:  pos=1  readPos=2  ch='\x00'   ← EOF，position 未推进
Input[0:1] = "a"                          ← 期望 Input[0:2] == "ab"
```

正常情况下 `position` 应该指向"已消费区域的下一个字符"，切片 `[start:position)` 才是完整 token。EOF 时这个不变量被破坏了。

### 为什么现有测试没抓到

看 [lexer_test.go:12-31](../lexer/lexer_test.go#L12-L31) 的输入：

```go
input := `let five = 5;
...
10 != 9;
`
```

最后一个字符是 `\n`。任何 token 后面都还有至少一个字符，`readChar` 永远不会在 token 内部撞到 EOF。加一个尾部空格就能"修好"症状：

```text
with trailing space: New("ab ") -> "ab"   ← 正确
```

**这是测试设计的教训**：词法分析器的测试输入必须包含「token 紧贴 EOF」这一情形。你的测试用了一整段漂亮的程序，却漏掉了最短、最脏的输入。

### 修法方向

让 `position` 在 EOF 时也满足不变量。两条路：

- **路 A**：EOF 分支里把 `l.position` 推到 `len(l.Input)`，同时 `readPosition` 也推进，保持二者关系一致。
- **路 B**：改变切片依据，`readIdentifier` / 数字扫描不用 `l.position` 而用独立计数，或在扫描结束后显式判断是否到达 EOF。

路 A 改动更小，但要小心：`position` 推到 `len` 之后，`NextToken` 末尾那句 `tok.Pos = l.position` 会给 EOF token 一个越界值；另外重复调用 `readChar` 不能让 `position` 越过 `len`。

### 回归测试

```go
func TestLexerTokenAtEOF(t *testing.T) {
	tests := []struct {
		input           string
		wantType        token.TokenType
		wantLiteral     string
	}{
		{"ab", token.IDENT, "ab"},
		{"a", token.IDENT, "a"},
		{"12", token.INT, "12"},
		{"1.5", token.FLOAT, "1.5"},
		{"let", token.LET, "let"},
		{"return", token.RETURN, "return"},
	}
	for _, tt := range tests {
		l := New(tt.input)
		tok := l.NextToken()
		if tok.Type != tt.wantType || tok.Literal != tt.wantLiteral {
			t.Errorf("New(%q).NextToken() = %s/%q, want %s/%q",
				tt.input, tok.Type, tok.Literal, tt.wantType, tt.wantLiteral)
		}
	}
}

func TestLexerEOFIsStable(t *testing.T) {
	l := New("x")
	l.NextToken()                     // ident
	for i := 0; i < 5; i++ {          // 反复取 EOF 不应 panic 或越界
		if tok := l.NextToken(); tok.Type != token.EOF {
			t.Fatalf("call %d: got %s, want EOF", i, tok.Type)
		}
	}
}
```

第二个测试同样重要：修完 bug 1 之后，很容易引入「EOF 后继续 readChar 导致 position 越界」的新问题。

---

## 2. `true` / `false` 无法解析

### 复现

```text
PARSE "true;"   -> nil
errs=["unexpected prefix expression", "parse pratt first token failed",
      "parse expression fail for expression statemnt",
      "parse statement error{token: true, raw: t}"]
```

`!true;` 同样失败。

### 根因

链路上每一环都准备好了，只差最后一步：

- `token.go:16-17` 定义了 `TRUE = "true"`、`FALSE = "false"`
- `token.go:75-76` 把它们登记进 `KeywardsType`，lexer 能正确产出 `TRUE` / `FALSE` token（`lexer_test.go:92,98` 也验证了这点）
- `ast.go:220-231` 定义了完整的 `BooleanLiteral` 节点

但 [parser.go:466-509](../parser/parser.go#L466-L509) 的 `parsePrattNestingPrefixExpression` 的 switch 只有 `NOT/MINUS`、`INT`、`FLOAT`、`STRING`、`IDENT` 五个分支，没有 `TRUE`/`FALSE`，直接落到 `default` 报 "unexpected prefix expression"。

**`ast.BooleanLiteral` 是一个没有任何构造者的死类型。** 用 `grep -rn "BooleanLiteral" .` 可以确认：只在 `ast.go` 里出现，parser 从未引用。

这类 bug 有个通用信号：**定义了却没人构造的 AST 节点**。写 parser 时可以拿 AST 里的节点列表当 checklist，逐个确认"谁构造它"。

### 修法方向

在 `parsePrattNestingPrefixExpression` 加一个分支，注意三点：

1. `TRUE` 和 `FALSE` 可以合并到同一个 case，`Value` 字段由 `p.curToken.Type == token.TRUE` 决定
2. 别忘了 `p.readToken()`——这是本 parser 的游标契约（见 03 章），每个 prefix 解析函数返回时 `curToken` 必须已经越过自己消费的 token
3. 布尔字面量属于「nesting 版也必须支持」的基础字面量，放在 `parsePrattNestingPrefixExpression` 里而不是外层，否则 `1 == true` 会失败

### 回归测试

```go
{`true;`, []string{"*ast.ExpressionStatement"}},
{`false;`, []string{"*ast.ExpressionStatement"}},
{`!true;`, []string{"*ast.ExpressionStatement"}},
{`let a = true;`, []string{"*ast.LetStatement"}},
{`1 == true;`, []string{"*ast.ExpressionStatement"}},   // 验证 nesting 位置也支持
{`if (true) { 1 };`, []string{"*ast.ExpressionStatement"}},
```

---

## 3. `nesting` 标志阻断了所有嵌套复合字面量

这条不只是 bug，是**设计问题**。它值得最长的篇幅。

### 复现

```text
PARSE "let a = [[1,2],[3]];"        -> nil   errs=["unexpected prefix expression", ...]
PARSE "f(fn(x){x});"                -> nil   errs=["unexpected prefix expression", ...]
PARSE "1 + [1][0];"                 -> nil   errs=["unexpected prefix expression", ...]
PARSE "let a = [1];"                -> OK    ← 单层可以
PARSE "[1][0];"                     -> OK    ← 最外层可以
```

规律：**复合字面量（数组、哈希、`if`、`fn`）只能出现在表达式的最外层。** 一旦嵌进另一个表达式，立刻失败。

### 根因

[parser.go:297-301](../parser/parser.go#L297-L301)：

```go
func (p *Parser) parsePrattPrefixExpression(nesting bool) ast.Expression {
	if nesting {
		return p.parsePrattNestingPrefixExpression()   // ← 复合字面量在这条路上全部缺失
	}

	switch p.curToken.Type {
	case token.LEFT_BRACKET:   // 数组
	case token.LEFT_BRACE:     // 哈希
	case token.IF:             // if 表达式
	case token.FUNC:           // 函数字面量
	default:
		return p.parsePrattNestingPrefixExpression()
	}
}
```

数组/哈希/if/fn 四个分支只在 `nesting == false` 时可达。而 `nesting == false` 只有一个来源——`parseExpression`（parser.go:224）的 `p.parsePrattExpression(0, false)`，也就是**语句层的第一次调用**。

其余全部递归点都传 `true`：

| 调用点 | 位置 | 影响 |
| --- | --- | --- |
| 数组元素 | parser.go:308 | `[[1]]` 失败 |
| 哈希键 | parser.go:331 | `{[1]: 2}` 失败 |
| 哈希值 | parser.go:343 | `{"a": [1]}` 失败 |
| `if` 条件 | parser.go:392 | `if ([1][0]) {}` 失败 |
| 前缀操作数 | parser.go:471 | `!true` 失败（叠加缺陷 2） |
| 调用实参 | parser.go:519 | `f(fn(x){x})` 失败 |
| 索引下标 | parser.go:536 | `xs[[0][0]]` 失败 |
| 中缀右操作数 | parser.go:559 | `1 + [1][0]` 失败 |

于是任何深度 ≥ 2 的复合结构都无法解析。这不是边角情形——`let matrix = [[1,2],[3,4]];`、`map(xs, fn(x){ x*2 })` 都是日常代码。

### 这个标志想解决什么

`nesting` 不是凭空来的，它在试图解决一个**真实的语法歧义**：`{` 既可以开始哈希字面量，也可以开始块语句。

```monkey
if (x) { 1 }        // { 是块
let h = {"a": 1};   // { 是哈希
```

用 `nesting` 区分是一种朴素方案，但它把「`{` 的歧义」扩大成了「所有复合字面量的位置限制」——代价远超收益。

### 正确的解法

关键认识：**块语句只出现在语法上确定的位置**——`if` 分支体、`while` 体、`fn` 体。这些位置由 `parseBlockStatement` 直接处理，从不经过表达式解析器。

所以表达式解析器遇到 `{` 时，它**必然**是哈希字面量，不需要任何标志。

修法：

1. 删掉 `nesting` 参数，`parsePrattExpression(minBP int)` 只留一个参数
2. 把 `parsePrattPrefixExpression` 和 `parsePrattNestingPrefixExpression` 合并成一个函数，包含全部 prefix 分支：字面量、标识符、`TRUE`/`FALSE`、前缀运算符、`(` 分组、`[`、`{`、`if`、`fn`
3. 确认 `if`/`while`/`fn` 的 body 走的是 `parseBlockStatement`，不走表达式路径（当前代码 parser.go:403、192、450 已经是这样，所以这一步无需改动）

合并后 prefix 分派表长这样：

```text
INT FLOAT STRING IDENT TRUE FALSE   → 字面量 / 标识符
NOT MINUS                           → 前缀表达式，右侧用 PREFIX_BP 递归
LEFT_PAREN                          → 分组，解析内部表达式后要求 )
LEFT_BRACKET                        → 数组字面量
LEFT_BRACE                          → 哈希字面量（此处 { 必然是哈希）
IF                                  → if 表达式，body 用 parseBlockStatement
FUNC                                → 函数字面量，body 用 parseBlockStatement
default                             → 错误
```

这就是标准 Pratt parser 的 prefix 表。它没有"模式"，因为不需要。

### 回归测试

```go
{`let a = [[1,2],[3]];`, []string{"*ast.LetStatement"}},
{`f(fn(x){ x });`, []string{"*ast.ExpressionStatement"}},
{`1 + [1][0];`, []string{"*ast.ExpressionStatement"}},
{`{"a": [1, 2]};`, []string{"*ast.ExpressionStatement"}},
{`[fn(x){x}, fn(y){y}];`, []string{"*ast.ExpressionStatement"}},
{`let m = {"k": {"nested": 1}};`, []string{"*ast.LetStatement"}},
{`if (x) { 1 } else { {"a":1} };`, []string{"*ast.ExpressionStatement"}},
```

最后一条特别关键：它同时验证「块位置的 `{` 当块解析」和「表达式位置的 `{` 当哈希解析」。

---

## 4. 索引不参与 postfix 链

### 复现

```text
PARSE "xs[0];"      -> stmts=2   String(): "xs;[0];"
PARSE "x[1];"       -> stmts=2   String(): "x;[1];"
PARSE "f(1,2)[0];"  -> panic（叠加缺陷 5）
```

`xs[0];` 应该是**一个**表达式语句，实际被解析成**两个**：标识符 `xs`，然后一个数组字面量 `[0]`。没有报错——这是最危险的一类 bug，parser 悄悄给出了结构上合法但语义完全错误的 AST。

### 根因

[parser.go:271-292](../parser/parser.go#L271-L292)：

```go
for {
	// postfix: only func call
	if p.curToken.Type == token.LEFT_PAREN {     // ← 只判 (，不判 [
		if POSTFIX_BP < minBP {
			break
		}
		left = p.parsePrattPostfixExpression(left)
		continue
	}

	bp, ok := INFIX_BP[p.curToken.Type]
	if !ok {
		break                                     // ← [ 落到这里，循环退出
	}
	...
}
```

注释 `// postfix: only func call` 说明这是有意为之的临时状态。但 `parsePrattPostfixExpression`（parser.go:531-547）**已经实现了 `LEFT_BRACKET` 分支**——索引的解析代码写好了，只是主循环从不调用它。

于是 `xs` 解析完，`curToken` 是 `[`，既不匹配 `LEFT_PAREN` 也不在 `INFIX_BP` 里，循环退出，`xs` 作为完整表达式返回。语句层看 `curToken` 不是分号，于是开始解析下一个语句——`[0]` 被当成数组字面量。

### 修法方向

主循环的 postfix 判断改成同时接受 `LEFT_PAREN` 和 `LEFT_BRACKET`。注意：

- 两者共用 `POSTFIX_BP`，因为调用和索引优先级相同且左结合，`f(1)[0](2)` 要能连续吸收
- `continue` 而非 `break`，保证链式可以继续
- 判 `POSTFIX_BP < minBP` 的逻辑对两者都要生效

顺手可以想一个问题：`POSTFIX_BP = 80` 比 `PREFIX_BP = 70` 高，所以 `-xs[0]` 会解析成 `-(xs[0])`。这是对的吗？（对的，索引应该比一元负号先结合。）

### 回归测试

```go
{`xs[0];`, []string{"*ast.ExpressionStatement"}},          // 必须是 1 个语句
{`xs[0][1];`, []string{"*ast.ExpressionStatement"}},       // 链式索引
{`f(1)[0];`, []string{"*ast.ExpressionStatement"}},        // 调用后索引
{`xs[0](1);`, []string{"*ast.ExpressionStatement"}},       // 索引后调用
{`-xs[0];`, []string{"*ast.ExpressionStatement"}},         // 与前缀运算符的优先级
```

再加一个断言 `String()` 的测试——语句数量对了不代表结构对了：

```go
func TestPostfixChainStructure(t *testing.T) {
	tests := []struct{ input, want string }{
		{`xs[0];`, `xs[0];`},
		{`xs[0][1];`, `xs[0][1];`},
		{`f(1)[0];`, `f(1)[0];`},
		{`-xs[0];`, `(-xs[0]);`},
	}
	for _, tt := range tests {
		p := New(lexer.New(tt.input), Pratt)
		prog := p.ParseProgram()
		if prog == nil {
			t.Fatalf("%q: nil program, errs=%v", tt.input, p.errors)
		}
		if got := prog.String(); got != tt.want {
			t.Errorf("%q: String() = %q, want %q", tt.input, got, tt.want)
		}
	}
}
```

---

## 5. 空列表形式：nil 元素与 panic

### 复现

```text
PARSE "[];"             -> nil    errs=["unexpected prefix expression", ...]
PARSE "{};"             -> nil    errs=["unexpected prefix expression", ...]
PARSE "let f = fn(){};" -> nil    errs=["expected identifier", ...]
PARSE "a();"            -> ValueList[0] == nil，随后 String() panic
```

`a()` 这条最严重，直接崩：

```text
panic: runtime error: invalid memory address or nil pointer dereference
ast.(*PostfixExpression).String(...)
	/compiler/monkey/ast/ast.go:408
```

### 根因

四处都是同一个模式错误：**用 do-while 结构解析可空列表**。以调用实参为例，[parser.go:517-529](../parser/parser.go#L517-L529)：

```go
p.readToken()                 // 越过 (
for {
	expr.ValueList = append(expr.ValueList, p.parsePrattExpression(0, true))
	//                                      ↑ 无条件解析，curToken 是 ) 时返回 nil
	//                                        且 nil 被 append 进去了
	if p.curToken.Type == token.COMMA {
		p.readToken()
	} else if p.curToken.Type == token.RIGHT_PAREN {
		p.readToken()
		break
	} else {
		...
	}
}
```

循环体先执行、后判退出，所以**至少解析一个元素**。空列表时第一次迭代就撞上闭合符号。

这里还有第二层错误：返回值没做 nil 检查就 `append`。对比数组元素的写法（parser.go:308-314）用了 `if expr != nil` 保护，而调用实参和索引下标（parser.go:519、536）**没有**。所以数组会报错，调用会塞 nil 进 AST 然后延迟到 `String()` 才崩。

四处位置：

| 构造 | 位置 | 症状 |
| --- | --- | --- |
| 数组元素 | parser.go:307 | `[]` 报错 |
| 哈希键值对 | parser.go:330 | `{}` 报错 |
| 函数形参 | parser.go:430 | `fn(){}` 报错（"expected identifier"） |
| 调用实参 | parser.go:518 | `a()` 塞 nil → panic |
| 索引下标 | parser.go:535 | `xs[]` 塞 nil → panic |

### 修法方向

统一改成「先判空，再进循环」：

```text
readToken()                    // 越过开括号
if curToken == 闭合符号 {
    readToken()
    return 空列表
}
for {
    解析一个元素
    if 元素 == nil { 报错; return nil }
    append
    if curToken == COMMA { readToken(); continue }
    if curToken == 闭合符号 { readToken(); break }
    报错; return nil
}
```

这个骨架五处都一样，值得抽成一个辅助函数。签名可以是：

```go
// parseList 解析形如 <open> [elem {"," elem}] <close> 的列表。
// 进入时 curToken 指向 open，返回时 curToken 已越过 close。
func (p *Parser) parseList(closing token.TokenType) ([]ast.Expression, bool)
```

抽出来之后，「空列表」这个边界只需要在一个地方正确，而不是五个地方各自正确。**重复的解析骨架是 bug 的温床**——这五处本来是同一份逻辑抄了五遍，其中两遍漏了 nil 检查。

哈希稍有不同（要成对解析键值），形参也不同（只接受 IDENT，不是任意表达式），但「先判空后循环」的结构可以共享。

### 回归测试

```go
{`[];`, []string{"*ast.ExpressionStatement"}},
{`{};`, []string{"*ast.ExpressionStatement"}},
{`let f = fn(){};`, []string{"*ast.LetStatement"}},
{`a();`, []string{"*ast.ExpressionStatement"}},
{`f()();`, []string{"*ast.ExpressionStatement"}},
{`let f = fn(){ }; f();`, []string{"*ast.LetStatement", "*ast.ExpressionStatement"}},
```

并且一定要调用 `prog.String()`——nil 元素只在打印时才暴露。建议在所有 parser 测试的公共断言里加上「无条件调一次 `String()`」，把这类延迟崩溃提前到解析测试阶段。

---

## 6. 括号分组无法解析

### 复现

```text
PARSE "(1 + 2) * 3;"  -> nil
errs=["unexpected prefix expression", "parse pratt first token failed", ...]
```

### 根因

`token.LEFT_PAREN` 在两个地方有含义：

- **中缀/后缀位置**：函数调用，已在主循环处理（parser.go:273）
- **前缀位置**：分组表达式，**完全没有实现**

`parsePrattNestingPrefixExpression`（parser.go:466-509）和 `parsePrattPrefixExpression`（parser.go:302-463）的 switch 里都没有 `LEFT_PAREN` 分支。

这也解释了 `README.md:44` 的 BNF 里写着 `"(" <expression> ")"` 却无法工作——**文档描述的语言比实现的语言大**。

### 修法方向

在 prefix 分派里加 `LEFT_PAREN`：越过 `(`，以 `minBP = 0` 递归解析内部表达式（分组内部优先级完全重置），要求 `curToken` 是 `)`，越过它，返回内部表达式本身。

一个设计选择：**要不要为分组建立 AST 节点？** 不要。括号的作用是改变结合方式，这个作用在解析完成后已经体现在树的形状里了。返回内部表达式即可，AST 里没有 `GroupedExpression` 这种东西。所以：

```text
(1 + 2) * 3   →   ((1+2)*3)
1 + 2 * 3     →   (1+(2*3))
```

两棵树形状不同，但都不包含"括号节点"。

### 回归测试

```go
func TestGrouping(t *testing.T) {
	tests := []struct{ input, want string }{
		{`(1 + 2) * 3;`, `((1+2)*3);`},
		{`1 + 2 * 3;`, `(1+(2*3));`},        // 对照：无括号
		{`((1));`, `1;`},                    // 嵌套括号不产生额外节点
		{`-(1 + 2);`, `(-(1+2));`},
		{`(a);`, `a;`},
	}
	// ...断言 prog.String() == want
}
```

---

## 7. `PrefixExpression.String()` 打印了结构体

### 复现

```text
PARSE "-5;"  -> ({- - %!s(int=0)}5);
PARSE "!x;"  -> ({! ! %!s(int=0)}x);
```

期望 `(-5);` 和 `(!x);`。

### 根因

[ast.go:371](../ast/ast.go#L371)：

```go
func (pe *PrefixExpression) String() string {
	return fmt.Sprintf("(%s%s)", pe.Token, pe.Right.String())
	//                             ↑ Token 是 struct，不是 string
}
```

`pe.Token` 是 `token.Token{Type, Literal, Pos}`，用 `%s` 格式化会退化成 Go 的结构体默认表示 `{- - %!s(int=0)}`——三个字段依次是 `Type`（`-`）、`Literal`（`-`）、`Pos`（`%!s(int=0)` 是 `%s` 用在 int 上的错误标记）。

对比同文件的 `InfixExpression.String()`（ast.go:385）用的是 `ie.TokenLiteral()`，写法是对的。**同一个文件里两个平行节点用了不同写法，其中一个错了**——这正是 `go vet` 能抓到的问题：

```bash
go vet ./ast/
```

`go vet` 会报 `fmt.Sprintf format %s has arg pe.Token of wrong type token.Token`。当前 `go vet` 没被纳入流程，所以漏过去了。

### 一个比乱码更严重的后果

**这条缺陷让 `ast` 包里的测试完全无法运行。** `go test` 默认会跑一个 vet 子集（含 `printf` 检查），vet 失败就直接 build failed，测试根本不执行：

```bash
$ go test ./ast/
# github.com/shlllshlll/monkey/ast
ast/ast.go:371:23: fmt.Sprintf format %s has arg pe.Token of wrong type token.Token
FAIL	github.com/shlllshlll/monkey/ast [build failed]
```

所以 02 章建议的"给 `ast/` 包补测试"在修掉这一行之前**做不到**——你会写完测试却看到一个和测试内容毫无关系的报错。（临时绕过是 `go test -vet=off ./ast/`，但那是掩盖问题，不是解决问题。）

这也解释了 `go test ./...` 输出里 `ast` 那行为什么是 `[no test files]` 而不是失败：**目前确实没有测试文件，所以 vet 错误没有被触发**。一旦你加第一个测试文件，这条缺陷立刻从"打印乱码"升级成"整个包的测试基础设施瘫痪"。

### 修法方向

改用 `pe.Token.Literal` 或 `pe.TokenLiteral()`。后者更一致——同文件其他节点都走 `TokenLiteral()`。

顺带把 `go vet ./...` 加入你的开发循环。它是零成本的，能抓格式化字符串、无用赋值、结构体标签等一批错误。

### 回归测试

```go
func TestPrefixString(t *testing.T) {
	tests := []struct{ input, want string }{
		{`-5;`, `(-5);`},
		{`!x;`, `(!x);`},
		{`-a * b;`, `((-a)*b);`},
		{`!-x;`, `(!(-x));`},
	}
	// ...
}
```

---

## 8. `IfExpression.String()` 丢 `else if` 前缀

### 复现

```text
"if (a) { 1 } else if (b) { 2 };"              -> if (a){1;}b){2;};
                                                            ↑ 少了 "else if ("
"if (a) { 1 } else if (b) { 2 } else { 3 };"   -> if (a){1;}else if (b){2;}else{3;};
                                                            ↑ 这个是对的
```

只有**没有末尾 `else` 的 `if/else if`** 才出错。

### 根因

[ast.go:317-328](../ast/ast.go#L317-L328)：

```go
for i, b := range ie.Branches {
	if b.Condition != nil {
		if i == 0 {
			sb.WriteString("if (")
		} else if i != len(ie.Branches)-1 {      // ← 这个条件是错的
			sb.WriteString("else if (")
		}
		sb.WriteString(b.Condition.String())
		sb.WriteByte(')')
	} else {
		sb.WriteString("else")
	}
	sb.WriteString(b.Body.String())
}
```

`i != len-1` 想表达"不是最后一个分支"，但**最后一个分支是不是 `else` 应该看 `Condition == nil`，不是看索引**。

`if (a){...} else if (b){...}` 有两个分支，都带 condition。第二个分支 `i == 1 == len-1`，于是 `i == 0` 假、`i != len-1` 也假，两个前缀都没打印，只剩下 `b){2;}`——condition 和它后面的 `)` 裸露出来。

三分支的情形侥幸正确：最后一个分支 `Condition == nil` 走 else 分支，中间那个 `i=1 != 2` 成立，打印了 `else if (`。

### 修法方向

分支前缀只由两件事决定：是不是第一个（`i == 0`），有没有 condition（`b.Condition != nil`）。索引和总长度无关。真值表：

| i == 0 | Condition != nil | 前缀 |
| --- | --- | --- |
| 是 | 是 | `if (` |
| 否 | 是 | `else if (` |
| 否 | 否 | `else` |
| 是 | 否 | 非法状态——第一个分支必须有条件 |

第四行值得留意：可以考虑在 parser 里保证它不出现，或者在 `String()` 里显式处理。这类"结构上可表达但语义上非法"的状态，是扁平化 `Branches` 设计的代价（见 02 章的取舍讨论）。

### 回归测试

```go
func TestIfString(t *testing.T) {
	tests := []struct{ input, want string }{
		{`if (a) { 1 };`, `if (a){1;};`},
		{`if (a) { 1 } else { 2 };`, `if (a){1;}else{2;};`},
		{`if (a) { 1 } else if (b) { 2 };`, `if (a){1;}else if (b){2;};`},
		{`if (a) { 1 } else if (b) { 2 } else { 3 };`, `if (a){1;}else if (b){2;}else{3;};`},
		{`if (a) { 1 } else if (b) { 2 } else if (c) { 3 };`,
			`if (a){1;}else if (b){2;}else if (c){3;};`},
	}
	// ...
}
```

五条覆盖了「1 分支 / 2 分支带 else / 2 分支不带 else / 3 分支带 else / 3 分支不带 else」。当前实现只有第 1、2、4 条通过——**测试矩阵不完整时，错误的索引条件正好躲在没测的格子里**。

---

## 9. 哈希打印顺序不稳定

### 复现

同一份输入 `{"a": 1, "b": 2, "c": 3};`，连续跑三次：

```text
run0: {a:1,b:2,c:3};
run1: {a:1,b:2,c:3};
run2: {b:2,c:3,a:1};      ← 顺序变了
```

### 根因

两层问题，第二层比第一层严重。

**第一层：遍历 map 不排序。** [ast.go:285](../ast/ast.go#L285) 直接 `for k, v := range hl.Pairs`。Go 故意随机化 map 迭代顺序，所以输出不可预测。任何断言 `String()` 的测试都会随机失败。

**第二层：`Pairs` 的类型选错了。** [ast.go:272](../ast/ast.go#L272)：

```go
type HashList struct {
	Token token.Token
	Pairs map[Expression]Expression      // ← key 是接口，比较的是指针
}
```

`Expression` 是接口，装的是 `*ast.StringLiteral` 这类指针。map 用指针相等做键比较，所以：

```monkey
{"a": 1, "a": 2}
```

两个 `"a"` 是两个不同的 `*StringLiteral` 对象，map 认为它们是不同的键，都被保留下来。**AST 层无法表达也无法检测重复键**，而且键的顺序信息永久丢失。

更根本的问题是：**AST 应该忠实记录源码写了什么，去重是语义阶段的事。** 用 map 意味着 AST 已经做了一次（错误的）语义处理。

### 修法方向

把 `Pairs` 改成有序切片：

```go
type HashPair struct {
	Key   Expression
	Value Expression
}

type HashList struct {
	Token token.Token
	Pairs []HashPair
}
```

这样：源码顺序保留，`String()` 天然稳定，重复键留给 evaluator/compiler 决定（报错还是后者覆盖前者），也不再依赖指针相等。

改完记得同步 parser.go:327、349 的构造代码——从 `make(map[...])` + `hashList.Pairs[key] = value` 变成 `append`。

如果你想保留 map 形态（比如为了 O(1) 查找），那至少要：`String()` 里对键的字符串表示排序，并在 parser 里显式检测重复键。但对 AST 来说切片是更合适的选择——AST 的用途是被遍历，不是被查找。

### 回归测试

```go
func TestHashStringIsStable(t *testing.T) {
	input := `{"a": 1, "b": 2, "c": 3};`
	want := `{a:1,b:2,c:3};`
	for i := 0; i < 20; i++ {          // 跑多次，随机化才会暴露
		p := New(lexer.New(input), Pratt)
		if got := p.ParseProgram().String(); got != want {
			t.Fatalf("run %d: String() = %q, want %q", i, got, want)
		}
	}
}

func TestHashPreservesDuplicateKeys(t *testing.T) {
	// AST 层应保留两个 pair，去重是语义阶段的决定
	p := New(lexer.New(`{"a": 1, "a": 2};`), Pratt)
	prog := p.ParseProgram()
	// 断言 pair 数量 == 2
}
```

第一个测试的 `for i := 0; i < 20` 不是多余的——map 随机化不保证每次都乱序，跑一次很可能"通过"。

---

## 10. `repl.go` 编译失败

### 复现

```bash
go build ./...
# github.com/shlllshlll/monkey/repl
# repl/repl.go:29:47: undefined: lexer.EOF
```

### 根因

[repl.go:29](../repl/repl.go#L29)：

```go
for tok := l.NextToken(); tok.Type != lexer.EOF; tok = l.NextToken() {
```

`EOF` 定义在 `token` 包（token.go:59），不在 `lexer` 包。而 `repl.go` 的 import 只有 `lexer`，没有 `token`。

这说明 `repl` 包从来没被编译过——加一行 `import "github.com/shlllshlll/monkey/token"` 并改成 `token.EOF` 即可。

### 为什么值得单列一条

因为它揭示了流程问题：**`go build ./...` 不在你的开发循环里。** `go test ./lexer/ ./parser/` 会绿，但整个模块编译不过。

最小的流程修正：

```bash
go build ./... && go vet ./... && go test ./...
```

三个命令都要绿才算一次有效迭代。当前三个全红（build 失败、vet 会报 ast.go:371、test 因 build 失败而 FAIL）。

---

## 11. `ParseProgram` 没有错误恢复

### 复现

```text
PARSE "true; let a = 1;"  -> nil，只报第一个错误
```

一个错误就放弃整个程序，后面的语句完全不解析。

### 根因

[parser.go:52-58](../parser/parser.go#L52-L58)：

```go
for !p.curTokenIs(token.EOF) {
	if stat := p.parseStatement(); stat != nil {
		program.Statements = append(program.Statements, stat)
	} else {
		p.errors = append(p.errors, fmt.Sprintf("parse statement error{...}"))
		return nil                       // ← 首错即退出
	}
}
```

这与 `03-parser.md` 宣称的"跳过到分号、右大括号或 EOF 等同步点，继续解析后面的 statement，这样一次输入可以报告多个错误"直接矛盾。**文档写了设计，代码没实现。**

还有一个隐患：如果改成 `continue` 而不推进 token，会**死循环**——`parseStatement` 失败时不保证消费了任何 token。

### 修法方向

实现同步点恢复：

```text
for curToken != EOF {
    stmt := parseStatement()
    if stmt != nil {
        append(stmt)
        continue
    }
    // 恢复：跳到下一个同步点
    synchronize()
}

func synchronize() {
    // 关键：至少消费一个 token，否则死循环
    readToken()
    for curToken != EOF {
        if curToken == SEMICOLON { readToken(); return }
        if curToken == RIGHT_BRACE { readToken(); return }
        if curToken ∈ {LET, RETURN, WHILE, IF} { return }   // 语句起始 token
        readToken()
    }
}
```

三类同步点各有用意：分号和右大括号是**结束**标志（越过它们），`let`/`return`/`while` 是**开始**标志（停在它们上，不消费）。

`synchronize` 第一行的 `readToken()` 是防死循环的关键。写完之后一定要测一个"错误在最后一个 token"的输入，确认不会挂住。

同时 `ParseProgram` 的返回约定要改：现在是「有错返回 nil」，应该改成「总是返回已解析出的 program，错误通过 `Errors()` 查询」。这样 REPL 和工具可以同时拿到部分 AST 和错误列表。

### 回归测试

```go
func TestParserRecovery(t *testing.T) {
	// 第一个语句非法，后两个合法：应报 1 个错误并解析出 2 个语句
	p := New(lexer.New(`let = 5; let a = 1; let b = 2;`), Pratt)
	prog := p.ParseProgram()
	if prog == nil {
		t.Fatal("program is nil; recovery should return partial AST")
	}
	if len(prog.Statements) != 2 {
		t.Errorf("got %d statements, want 2", len(prog.Statements))
	}
	if len(p.errors) != 1 {
		t.Errorf("got %d errors, want 1: %v", len(p.errors), p.errors)
	}
}

func TestParserRecoveryTerminates(t *testing.T) {
	// 防死循环：错误落在最后一个 token
	for _, in := range []string{`let`, `let =`, `let a =`, `+`, `)`, `}`, `let a = ;`} {
		done := make(chan bool, 1)
		go func() {
			New(lexer.New(in), Pratt).ParseProgram()
			done <- true
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("ParseProgram(%q) did not terminate", in)
		}
	}
}
```

第二个测试用超时检测死循环。写错误恢复必须配这种测试——死循环是这类代码最常见的失败模式。

---

## 12. `parseBlockStatement` 拒绝空块

### 复现

```monkey
let f = fn(x) { };        // 解析失败
while (c) { }             // 解析失败
if (a) { } else { 1 }     // 解析失败
```

### 根因

[parser.go:151-164](../parser/parser.go#L151-L164)：

```go
p.readToken()             // 越过 {
for {
	if sub_stmt := p.parseStatement(); sub_stmt != nil {    // ← 先解析
		stmt.Statements = append(stmt.Statements, sub_stmt)
	} else {
		p.errors = append(p.errors, "parse sub statement failed")
		return nil
	}

	if p.curToken.Type == token.RIGHT_BRACE {              // ← 后判退出
		p.readToken()
		break
	}
}
```

又是缺陷 5 的同一个模式：do-while 结构解析可空序列。空块时第一次迭代就拿 `}` 去解析语句，失败。

还有一个更隐蔽的问题：循环退出条件**只判 `RIGHT_BRACE`，不判 `EOF`**。输入 `{ 1;` （未闭合）时，`parseStatement` 在 EOF 上会失败并返回 nil，所以侥幸靠 error 分支退出了。但这是靠"另一个错误"来终止循环，不是靠正确的边界检查。如果 `parseStatement` 某天对 EOF 返回了非 nil（比如实现错误恢复之后），这里就会死循环。

### 修法方向

```text
readToken()                          // 越过 {
for curToken != RIGHT_BRACE && curToken != EOF {
    stmt := parseStatement()
    if stmt == nil { 报错; return nil }
    append(stmt)
}
if curToken != RIGHT_BRACE { 报错 "unclosed block"; return nil }
readToken()                          // 越过 }
```

把 EOF 放进循环条件，把「必须以 `}` 结束」变成显式检查。这样空块自然工作，未闭合块给出准确的错误信息而不是靠副作用退出。

### 回归测试

```go
{`let f = fn(x) { };`, []string{"*ast.LetStatement"}},
{`while (c) { }`, []string{"*ast.WhileStatement"}},
{`if (a) { } else { 1 };`, []string{"*ast.ExpressionStatement"}},
{`let f = fn() { };  f();`, []string{"*ast.LetStatement", "*ast.ExpressionStatement"}},
```

未闭合块单独测（应报错但不挂住）：

```go
func TestUnclosedBlock(t *testing.T) {
	for _, in := range []string{`fn(){ 1;`, `while(c){`, `if(a){ 1;`} {
		p := New(lexer.New(in), Pratt)
		p.ParseProgram()
		if len(p.errors) == 0 {
			t.Errorf("%q: expected an error, got none", in)
		}
	}
}
```

---

## 附加观察

以下不算 bug，但会在你继续往下写时变成阻碍。

### A. `Parser.errors` 没有导出访问器

`errors` 字段（parser.go:21）未导出，也没有 `Errors()` 方法。`parser_test.go:30` 能访问 `p.errors` 只因为测试在同包内。REPL、`main.go`、以及将来的 compiler 驱动都在包外，**拿不到错误信息**。

书上的惯例是：

```go
func (p *Parser) Errors() []string { return p.errors }
```

配合缺陷 11 的修法（`ParseProgram` 总是返回 program），调用方的用法变成：

```go
prog := p.ParseProgram()
if errs := p.Errors(); len(errs) > 0 {
	// 打印所有错误
	return
}
// 使用 prog
```

### B. `Token.Pos` 只对部分 token 赋值

探针实测：

```text
LEX "let a = 5"  -> [let/"let"/@0  ident/"a"/@0  =/"="/@6  int/"5"/@0]
                          ↑ @0        ↑ @0         ↑ @6 对   ↑ @0
```

只有 `=` 的位置是对的。原因在 [lexer.go:130-131](../lexer/lexer.go#L130-L131)：

```go
	tok.Pos = l.position
	l.readChar()
	return tok
}
```

`tok.Pos` 的赋值在函数**末尾**，而标识符、数字、字符串三条路径都在 switch 内部 `return tok` 提前返回了（lexer.go:108、124，以及 `readString` 内部），跳过了这行。

`Pos` 是你相对原版 Monkey 的扩展，用途是错误定位。现在它对最需要定位的三类 token（标识符、字面量、字符串）全是 0，用不上。

修法：在每条路径开头记录 `startPos := l.position`，构造 token 时统一带上。或者把 `Pos` 赋值提到 switch 之前。

顺便建议把 `Pos` 升级成 `Line` + `Column`——单个字节偏移对用户不友好，报错时你需要"第 3 行第 12 列"而不是"第 47 个字符"。`skipWhiteChar` 里遇到 `\n` 时递增行号即可。

### C. 浮点数词法过于宽松

`1.2.3` 被 lexer 接受为**单个** `FLOAT` token（lexer.go:112 的循环条件 `isDigit(l.ch) || l.ch == '.'` 不限制点的个数），然后在 parser 的 `strconv.ParseFloat` 阶段才失败：

```text
PARSE "1.2.3;"  -> nil, errs=["parse float literal failed", ...]
```

能报错，但**错在了错误的层**。词法错误应该由 lexer 报告（返回 ILLEGAL 并说明"数字里有多个小数点"），而不是让 parser 去解释一个语法上合法但语义无效的 token。

实测一组输入：

```text
"1.2.3"  -> float/"1.2.3"      ← 应为词法错误
"1."     -> float/"1."          ← BNF 要求点后至少一位数字
"1..2"   -> float/"1..2"        ← 连续两个点也接受
".5"     -> illegal             ← 这个符合 BNF（点前必须有数字）
"12"     -> int/"12"            ← 正确
```

`README.md:52` 的 BNF 要求小数点两侧都有数字（`<float> ::= <digit> {<digit>} "." <digit> {<digit>}`），lexer 只强制了左侧。修法：扫描数字时记录小数点个数，超过一个就返回 ILLEGAL；点后必须紧跟数字，否则也是 ILLEGAL。

### D. 字符串转义缺 `\"`

```text
PARSE "let s = \"a\\\"b\";"  -> nil, errs=[..., "token: illegal", ...]
```

即源码 `let s = "a\"b";` 无法解析。`readString`（lexer.go:178-192）支持 `\n \r \t \\`，**唯独漏了 `\"`**——而这是转义序列里最必要的一个，因为没有它就无法在字符串里写引号。

修法是在 switch 里加 `case '"': sb.WriteByte('"')`。

顺带一个设计问题：当前实现在**词法阶段就解码**转义，`Literal` 存的是解码后的值。这个选择是合理的（parser 拿到的就是最终字符串），但要注意 `StringLiteral.String()`（ast.go:242）返回 `sl.Token.Literal`，也就是解码后的值——所以 AST 打印出来的字符串**没有引号也没有转义**，`"a\nb"` 会打印成真正的换行。如果你希望 `String()` 输出可以重新解析的形式，需要在打印时重新转义。

### E. `IfBranch.Body` / `FunctionExpression.Body` 取地址会拿到循环副本（已被 02 章建议的重构顺手修复）

**这条在当前代码里已经不成立了，保留下来是因为它是一个值得理解的教训：一次"统一类型风格"的重构,顺手消灭了一整类未来才会暴露的 bug。**

修复前，`IfBranch.Body` 和 `FunctionExpression.Body` 都是值类型 `BlockStatement`：

```go
type IfBranch struct {
	Condition Expression
	Body      BlockStatement    // ← 修复前：值类型，不是指针
}
```

02 章第 8 节当时指出这是"值类型 vs 指针类型不统一"的四种风格之一，建议统一改成指针。这个建议后来被采纳——现在 `IfBranch.Body` 和 `FunctionExpression.Body`（ast.go:301、343）都是 `*BlockStatement`。

#### 修复前的复现

```go
ie := &ast.IfExpression{Branches: []ast.IfBranch{{}, {}}}

for i, branch := range ie.Branches {
	fmt.Println(&branch.Body == &ie.Branches[i].Body)   // false，两次都是
}
```

修复前实测两个分支全部输出 `false`。

#### 根因（修复前）

`for _, branch := range ie.Branches` 里的 `branch` 是**每次迭代都被拷贝出来的临时变量**（Go 的 `range` 语义，不是逃逸分析能改变的）。`&branch.Body` 取到的是这个临时变量的地址，和 `&ie.Branches[i].Body`（切片里真正的元素地址）是两个不同的地址。

如果你用指针当 map 的键——典型场景是**编译期给每个 AST 节点关联一份额外信息**（符号表条目、推断出的类型、诊断位置）——用 `range` 拷贝出的地址去建表，之后按真实节点地址查，**永远查不到**：

```go
types := map[*ast.BlockStatement]Type{}

for _, branch := range ie.Branches {
	types[&branch.Body] = someType        // 键是循环变量的地址
}

for i := range ie.Branches {
	t, ok := types[&ie.Branches[i].Body]  // 查真实元素的地址
	// ok 恒为 false
}
```

**这类 bug 不会报错，也不会 panic，只是安静地查不到东西**，然后你会去别的地方找原因——检查类型推断逻辑对不对、检查符号解析对不对——因为"按指针查表查不到"看起来不像是取地址的问题。

#### 影响范围（修复前的评估）

修复前，当时代码没有任何地方需要按地址索引 AST 节点，所以**当时不触发**。但 12 章的类型检查器（`types map[*ast.BlockStatement]Type` 之类的侧表）和更精细的符号表设计（把符号和它所属的作用域节点关联）都是这个模式的直接候选。[12 章 §2 和陷阱 ⑩](12-static-monkey.md) 已经把这个坑标了出来，作为静态类型系统实现时的必读项。

#### 修法方向：已采纳方案 2

当时给出两条路：

1. 改用索引遍历（`for i := range ie.Branches { ... &ie.Branches[i].Body ... }`），代价是每次都要多写一层 `[i]`。
2. **把 `Body` 改成指针类型**（`*BlockStatement`），呼应 02 章"AST 节点统一用指针"的建议。

**实际采纳了方案 2。** 现在 `IfBranch.Body` 和 `FunctionExpression.Body`（[ast.go:301](../ast/ast.go#L301)、[ast.go:343](../ast/ast.go#L343)）都是 `*BlockStatement`。这个改动让原来的陷阱**整个消失**，不是被绕过：`range` 拷贝的是指针值本身（一个内存地址），而不是它指向的 `BlockStatement` 结构体，所以 `branch.Body`（指针的值）在每次迭代里都和 `ie.Branches[i].Body` 相等——不再需要用 `&branch.Body` 取地址，直接用 `branch.Body` 当 map 键即可命中：

```go
types := map[*ast.BlockStatement]Type{}

for _, branch := range ie.Branches {
	types[branch.Body] = someType     // 键是指针本身的值，不是它的地址
}

for i := range ie.Branches {
	_, ok := types[ie.Branches[i].Body]
	// ok 恒为 true——实测验证过
}
```

**这是"统一类型风格"这类看似只关乎代码整洁的重构，顺手消灭一整类未来才会暴露的 bug 的一个真实例子。** 02 章当时给出这条建议的理由是"避免拷贝、方法集一致、nil 可以表示缺失"——**没有一条提到"侧表查找"**，但改完之后这个问题自动不存在了。这提示一个更普遍的教训：**结构性的一致（同一个概念永远用同一种类型）往往比事后针对具体症状打补丁更省心**——它消灭的不是"这一个 bug"，是"这一整类因为值/指针混用而产生的 bug"，包括你现在还没遇到过、以后才会遇到的那些。

---

## 建议的修复顺序

按依赖关系和收益排：

1. **缺陷 10**（`repl.go`）——一行，先让 `go build ./...` 绿
2. **缺陷 1**（EOF 截断）——影响一切，且修完会暴露其他问题
3. **缺陷 7**（`String()` 乱码）——一行，两个理由都很硬：`String()` 是你调试其他缺陷的主要工具必须先可信；而且它不修的话 **`ast` 包一个测试都跑不起来**（`go test` 默认跑 vet）
4. **缺陷 2**（布尔）+ **缺陷 6**（括号）——各加一个 prefix 分支
5. **缺陷 3**（`nesting`）——最大的一次重构，删掉标志、合并两个 prefix 函数
6. **缺陷 4**（索引 postfix）——主循环加一个 token 判断
7. **缺陷 5** + **缺陷 12**（空列表 / 空块）——统一抽 `parseList` 骨架
8. **缺陷 9**（哈希改切片）——涉及 AST 类型变更，波及 parser
9. **缺陷 8**（`else if` 打印）——真值表重写
10. **缺陷 11**（错误恢复）+ **附加观察 A**（`Errors()`）——一起做，涉及 `ParseProgram` 返回约定变更
11. **附加观察 B/C/D**——`Pos`、浮点、`\"`

顺序的理由：3 在 2 之前是因为你需要一个可信的 `String()` 来验证后面所有修复；5 放在 4 之前是因为 `nesting` 重构会大幅改动 prefix 分派，先做完再动主循环，避免两处同时改。

每修一条，跑：

```bash
go build ./... && go vet ./... && go test ./...
```

并把该条对应的回归测试加进去。修完 12 条之后，`README.md` 里那份 BNF 描述的语言才真正等于你实现的语言——现在两者之间的差距，就是这一章的全部内容。

