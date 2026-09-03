# 03 Parser：从 token 流恢复结构

## 本章要回答的问题

Token 流是线性的，AST 是树的。怎么建这棵树？为什么 `1 + 2 * 3` 必须建成 `1 + (2 * 3)`，而实现这一点只需要几个整数比较？为什么你的 parser 里每个函数返回时游标位置的约定，比函数本身的逻辑更容易出错？

## 1. 两种解析策略

Monkey 的语法分两部分，用两种策略。

**语句用递归下降。** 语句的种类由第一个 token 唯一决定：

```text
let    → parseLetStatement
return → parseReturnStatement
while  → parseWhileStatement
其他   → 赋值 或 表达式语句
```

一个 `switch` 就够了（parser.go:65-80）。这叫**递归下降**——每个语法规则一个函数，函数之间的调用关系镜像语法规则的嵌套关系。

**表达式用 Pratt parsing。** 表达式不能靠第一个 token 决定结构，因为优先级是**中间**的运算符决定的：

```text
a + b * c        树根是 +
a * b + c        树根是 +
a * b * c        树根是第二个 *
```

三个输入的第一个 token 都是标识符，但树的形状完全不同。递归下降处理这个需要为每个优先级层级写一个函数——你的 `README.md:34-43` 那七层 BNF（`logical-or` → `logical-and` → `equality` → `comparison` → `sum` → `product` → `prefix` → `postfix`）就是这种写法的直接翻译，要七个函数。

Pratt parsing 用**一张优先级表**替代这七个函数。

### 为什么你的实现有两个模式

```go
type ExpressionParseMode int

const (
	Pratt = iota
	RecursiveDescent
)
```

`RecursiveDescent` 目前是 `"not implement yet"`（parser.go:570）。这个开关**教学价值很高**，值得真的实现——把同一套语法用两种方法解析，然后断言两者产生完全相同的 AST：

```go
func TestBothModesAgree(t *testing.T) {
	for _, in := range inputs {
		a := New(lexer.New(in), Pratt).ParseProgram()
		b := New(lexer.New(in), RecursiveDescent).ParseProgram()
		if a.String() != b.String() {
			t.Errorf("%q: Pratt=%q RD=%q", in, a.String(), b.String())
		}
	}
}
```

这个测试会让你彻底搞懂优先级：递归下降里优先级体现为**函数的调用层次**，Pratt 里体现为**数字的大小比较**。两者等价，但表达方式完全不同。

（`const` 块少了显式类型：`Pratt = iota` 让 `Pratt` 是无类型常量 `0`，而不是 `ExpressionParseMode`。写成 `Pratt ExpressionParseMode = iota` 更严谨——否则任何 int 都能传进 `New`。）

## 2. Pratt parsing 的核心思想

先建立直觉，再看代码。

### 每个 token 有两种角色

一个 token 出现在表达式里，可能扮演两种角色：

| 角色 | 含义 | 例子 |
| --- | --- | --- |
| **prefix** | 出现在表达式**开头**，自己产生一个值 | `5`、`x`、`-a` 的 `-`、`(` 的分组、`[` 的数组 |
| **infix / postfix** | 已经有左值了，它**继续组合** | `+`、`*`、`==` 的中缀，`(` 的调用，`[` 的索引 |

同一个 token 可以有两种角色。`-` 在 `-5` 里是 prefix，在 `a - b` 里是 infix。`(` 在 `(1+2)` 里是 prefix（分组），在 `f(1)` 里是 postfix（调用）。**parser 靠"当前有没有左值"区分**，不需要回退。

### 主循环

```text
left = 解析一个 prefix        // 必须成功，否则不是合法表达式
loop:
    看当前 token
    如果它没有 infix/postfix 角色 → 退出
    如果它的优先级 < 我们的下限   → 退出
    left = 用 left 和它组合成新的 left
返回 left
```

关键在第二个判断——**"优先级 < 下限就退出"**。这一个比较同时实现了优先级和结合性。

### 手算 `a + b * c`

设 `+` 的优先级 50，`*` 的优先级 60。

```text
调用 parseExpression(minBP=0)
  prefix → left = a
  循环第 1 轮：
    当前 token 是 +，优先级 50
    50 >= 0（下限），继续
    解析右侧：递归 parseExpression(minBP=51)   ← 关键：51 = 50+1
      prefix → left = b
      循环第 1 轮：
        当前 token 是 *，优先级 60
        60 >= 51，继续                        ← * 优先级更高，被吸收
        解析右侧：parseExpression(minBP=61)
          prefix → left = c
          循环：当前是 ;，无 infix 角色 → 退出
          返回 c
        left = (b * c)
      循环第 2 轮：
        当前是 ;，退出
      返回 (b * c)
    left = (a + (b * c))
  循环第 2 轮：当前是 ;，退出
  返回 (a + (b * c))
```

**`*` 被"吸走"了**，因为在解析 `+` 的右操作数时，下限是 51，而 `*` 的 60 超过了它。

### 手算 `a + b + c`（左结合）

```text
parseExpression(0)
  left = a
  轮 1：+ 优先级 50 >= 0，右侧用 minBP=51
    parseExpression(51)
      left = b
      当前是第二个 +，优先级 50
      50 >= 51？  否 → 退出            ← 关键
      返回 b
    left = (a + b)
  轮 2：+ 优先级 50 >= 0，右侧用 minBP=51
    parseExpression(51) → c
    left = ((a + b) + c)
  返回 ((a + b) + c)
```

**第二个 `+` 没有被右侧递归吸走**，因为 `50 >= 51` 不成立。它退回到外层循环，成为第二轮的操作符——于是左边先结合。

**左结合 = 右操作数的下限比自己高 1。** 如果右操作数的下限等于自己（50 而不是 51），第二个 `+` 就会被吸走，得到 `a + (b + c)`——右结合。

**这就是全部秘密：`+1` 或不 `+1`，决定左结合还是右结合。**

### 你的 `BP` 结构

```go
type BP struct {
	lbp int      // left binding power
	rbp int      // right binding power
}

var INFIX_BP = map[token.TokenType]BP{
	token.OR:  {10, 11},
	token.AND: {20, 21},
	token.EQUAL: {30, 31}, token.NOT_EQUAL: {30, 31},
	token.GT: {40, 41}, token.LT: {40, 41}, token.GE: {40, 41}, token.LE: {40, 41},
	token.PLUS: {50, 51}, token.MINUS: {50, 51},
	token.MULTI: {60, 61}, token.DIVIDE: {60, 61},
}
```

`lbp` 用来和当前下限比较（"我够不够格被吸收"），`rbp` 传给右侧递归（"我的右操作数下限是多少"）。

**所有对都是 `{n, n+1}`，所以全部左结合。** 这是显式写出两个值的好处——想改成右结合，把某一对改成 `{n, n}` 就行，不需要理解 `+1` 藏在哪。

原版 Monkey 用单值 `precedence` 加 `LOWEST/EQUALS/LESSGREATER/SUM/PRODUCT/CALL` 枚举，右侧递归传 `precedence`（不加 1），靠 `peekPrecedence() > precedence`（严格大于）实现左结合。**两种写法等价**：`>` 配相同值，等于 `>=` 配 `+1` 值。你的写法把结合性变成了数据，更灵活；原版的写法枚举名有语义，更易读。

用 `{n, n}` 的典型场景是赋值和幂运算：

```text
a = b = c     应为 a = (b = c)    右结合
2 ** 3 ** 2   应为 2 ** (3 ** 2)  右结合
```

你的赋值是语句不是表达式，所以暂时不需要。但如果加 `**`，记得用 `{70, 70}`。

### 优先级数字用 10 的间隔

`10, 20, 30, 40, 50, 60` 而不是 `1,2,3,4,5,6`。留出插入空间——将来在 `&&`（20）和 `==`（30）之间加位运算符，可以用 25，不用重排整张表。**小事，但省事。**

## 3. 前缀与后缀的优先级

```go
const PREFIX_BP = 70
const POSTFIX_BP = 80
```

两个常量比所有中缀都高。推导一下为什么必须这样。

**`PREFIX_BP` 必须高于所有中缀：**

```text
-a * b
```

想要 `(-a) * b`，不是 `-(a * b)`。解析 `-` 时右操作数用 `PREFIX_BP=70` 作下限，`*` 的 lbp 是 60，`60 >= 70` 不成立，所以 `*` 不被吸进 `-` 的操作数里。`-a` 先成型，然后 `*` 在外层吸收它。

如果 `PREFIX_BP` 设成 55（介于 `+` 和 `*` 之间），`-a * b` 会变成 `-(a*b)` 而 `-a + b` 还是 `(-a)+b`——**部分正确的行为是最难调的**，因为测试可能刚好只覆盖了对的那半边。

**`POSTFIX_BP` 必须高于 `PREFIX_BP`：**

```text
-xs[0]
```

应该是 `-(xs[0])`——先取索引再取负。索引比一元负号结合更紧，所以 80 > 70。

**验证方法**：写这几个测试，它们精确地锁定了三个优先级层级的关系：

```go
{`-a * b;`,   `((-a)*b);`},        // PREFIX > 中缀
{`-a + b;`,   `((-a)+b);`},
{`!a && b;`,  `((!a)&&b);`},
{`-xs[0];`,   `(-xs[0]);`},        // POSTFIX > PREFIX
{`-f(1);`,    `(-f(1));`},
```

### 后缀在主循环里的特殊处理

后缀和中缀在主循环里的判断方式不同（parser.go:321-343）：

```go
for {
	// postfix: only func call
	if p.curToken.Type == token.LEFT_PAREN || p.curToken.Type == token.LEFT_BRACKET {
		if POSTFIX_BP < minBP {
			break
		}
		left = p.parsePrattPostfixExpression(left)
		continue                      // ← 注意 continue
	}

	// infix
	bp, ok := INFIX_BP[p.curToken.Type]
	if !ok { break }
	if bp.lbp < minBP { break }
	left = p.parsePrattInfixExpression(left, bp.rbp)
}
```

后缀不查 `INFIX_BP` 表，用硬编码的 `POSTFIX_BP` 比较。原因是后缀运算符**没有右操作数**——`f(1,2)` 的括号内容是一个列表，不是一个用优先级解析的表达式。所以它不需要 `rbp`，只需要 `lbp`。

`continue` 而非递归：后缀可以连续链接（`f(1)[0](2)`），每次循环吸收一个。这是**迭代实现的左结合**——和中缀靠 `rbp = lbp+1` 实现左结合不同，后缀天然左结合，因为循环就是从左往右吃。

`LEFT_PAREN`（调用）和 `LEFT_BRACKET`（索引）共用同一次判断、同一个 `POSTFIX_BP`——因为调用和索引优先级相同，这就是为什么 `xs[0](1)` 和 `f(1)[0]` 都能连续链接。早期版本这里只判了 `LEFT_PAREN`，漏了 `LEFT_BRACKET`，导致 `xs[0]` 解析不出来；现在两个 token 都判，这个洞已经补上（11 章缺陷 4 有完整的复现记录）。

## 4. 游标契约：你的实现最容易出错的地方

这是本章最重要的一节。**你的 parser 和书上的游标约定不同**，混用会导致 token 被多消费或少消费。

### 两种约定

**书上的约定**：`parseXxx` 返回时，`curToken` 指向该结构的**最后一个 token**。

```text
解析 5 完成后：curToken = INT(5)      ← 停在自己身上
```

**你的约定**：`parseXxx` 返回时，`curToken` 指向该结构**之后**的 token。

```text
解析 5 完成后：curToken = ;           ← 已经越过自己
```

证据在 `parsePrattPrefixExpression`（parser.go:545-553）：

```go
case token.INT:
	value, err := strconv.Atoi(p.curToken.Literal)
	// ...
	expr := ast.IntegerLiteral{Token: p.curToken, Value: value}
	p.readToken()                    // ← 消费掉自己，然后返回
	return &expr
```

每个字面量分支都有这个 `p.readToken()`。所以：

> **契约：进入 `parseXxx` 时 `curToken` 是该结构的第一个 token；返回时 `curToken` 是该结构之后的第一个 token。**

### 这个约定的好处

调用方不需要"再推进一次"。对比两种写法处理 `let x = 5;`：

```text
书上：                              你的：
parseExpression()                   parseExpression()
curToken 是 5                       curToken 是 ;
if peekToken is ; { nextToken() }   if curToken is ; { readToken() }
                                    ↑ 直接看 curToken，不需要 peek
```

你的约定让"检查可选分号"这类操作只看 `curToken`，不需要 `peek`。语句解析函数因此更简洁——`parseLetStatement`（parser.go:150-152）、`parseReturnStatement`（168-170）、`parseExpressionStatement`（185-187）都是同一个三行模式。

### 这个约定的代价

**每个 prefix 分支都必须记得 `readToken()`。** 漏了就会死循环——`curToken` 没动，主循环再次调用同一个 prefix 分支，无限重复。

检查一遍 prefix 分支：

| 分支 | 位置 | 是否推进 |
| --- | --- | --- |
| `LEFT_PAREN` 分组 | parser.go:349-361 | ✓（`)` 之后） |
| `ARRAY` | parser.go:362-389 | ✓（`]` 之后） |
| `HASH` | parser.go:390-431 | ✓（`}` 之后） |
| `IF` | 靠 `parseBlockStatement` | ✓ |
| `FUNC` | 靠 `parseBlockStatement` | ✓ |
| `NOT`/`MINUS` | parser.go:534-544 | 间接（递归调用负责） |
| `INT` | parser.go:545-553 | ✓ |
| `FLOAT` | parser.go:554-562 | ✓ |
| `STRING` | parser.go:563-566 | ✓ |
| `IDENT` | parser.go:567-570 | ✓ |
| `TRUE`/`FALSE` | parser.go:571-578 | ✓ |

全部 11 个分支都推进了游标。`TRUE`/`FALSE` 和 `LEFT_PAREN` 分组曾经是缺失的两个分支（11 章缺陷 2、缺陷 6），补上时踩的坑正是这条契约本身——**加任何新 prefix 分支，第一反应应该是"这条路径的 `readToken()` 在哪"**，而不是先想它的语义。

### 手算一遍游标

`let x = 5;`，标出每一步的 `curToken` / `nextToken`：

| 步骤 | 函数 | cur | next | 动作 |
| --- | --- | --- | --- | --- |
| 0 | `New` 后 | `let` | `x` | 两次 `readToken` 预填充 |
| 1 | `parseStatement` | `let` | `x` | switch → `parseLetStatement` |
| 2 | `parseLetStatement` | `x` | `=` | `readToken()`，检查是 IDENT ✓ |
| 3 | | `=` | `5` | `readToken()`，检查是 ASSIGN ✓ |
| 4 | | `5` | `;` | `readToken()`，准备解析表达式 |
| 5 | `parseExpression` | `5` | `;` | prefix → INT 分支 |
| 6 | | `;` | EOF | `readToken()`，返回 IntegerLiteral |
| 7 | 主循环 | `;` | EOF | `;` 不在 INFIX_BP → 退出 |
| 8 | `parseLetStatement` | EOF | EOF | `curToken` 是 `;` → `readToken()` |
| 9 | `ParseProgram` | EOF | EOF | `p.curToken.Type != token.EOF` 为假 → 循环结束 |

第 6 步是契约的体现：解析完 `5`，`curToken` 已经是 `;`。第 8 步因此可以直接判 `curToken == SEMICOLON`。

**建议你自己手算 `x = 1 + 2;`**——它经过赋值语句的双 `readToken()`（parser.go:205-206，一次跳过标识符、一次跳过 `=`），再走一遍表达式循环。

## 5. 语句分派与两 token lookahead

```go
func (p *Parser) parseStatement() ast.Statement {
	switch p.curToken.Type {
	case token.LET:    return p.parseLetStatement()
	case token.RETURN: return p.parseReturnStatement()
	case token.WHILE:  return p.parseWhileStatement()
	default:
		if p.curToken.Type == token.IDENT && p.nextToken.Type == token.ASSIGN {
			return p.parseAssignStatement()
		}
		return p.parseExpressionStatement()
	}
}
```

前三个用一个 token 就能决定。**赋值需要两个**：

```text
x = 1;      → 赋值语句      IDENT ASSIGN
x + 1;      → 表达式语句    IDENT PLUS
x;          → 表达式语句    IDENT SEMICOLON
```

只看 `x` 无法区分，必须看下一个 token 是不是 `=`。这就是 `nextToken` 字段存在的理由——**LL(2) 文法**（需要向前看 2 个 token）。

（`nextToken` 是你的命名，书上叫 `peekToken`。作为字段名 `peekToken` 更贴切——`next` 听起来像动词，容易和 `p.readToken()` 的动作混淆。而 lexer 的 `NextToken()` 确实是动词，两处 `next` 含义不同。）

### 这个设计的局限

只能处理 `标识符 = 值`。以下都不行：

```monkey
xs[0] = 1;        // 赋值目标是索引表达式
h["k"] = 1;       // 同上
obj.field = 1;    // 成员赋值（Monkey 暂无）
```

要支持它们，两 token lookahead 不够——`xs[0] = 1` 里 `=` 出现在第 5 个 token，而 `xs[0][1][2] = 1` 里更远。**不能靠固定长度的 lookahead 判断**。

标准解法：**先按表达式解析，再看是不是 `=`**。

```text
expr = parseExpression()
if curToken == ASSIGN {
    检查 expr 是合法的赋值目标（lvalue）
    readToken()
    value = parseExpression()
    return AssignmentStatement{Target: expr, Value: value}
}
return ExpressionStatement{Expression: expr}
```

"合法的赋值目标"是一个受限的表达式集合：标识符、索引表达式，不包括字面量（`5 = x` 非法）和调用（`f() = x` 非法）。这个检查叫 **lvalue 校验**，是语义检查而非语法检查——parser 可以做，也可以留给后面的阶段。

**这个改法同时简化了分派**：不需要两 token lookahead，`parseStatement` 的 `default` 分支只有一条路。

## 6. 错误处理

### 现状

早期版本这里有三个问题（11 章缺陷 11 和附加观察 A 有完整记录）：错误收集在 `p.errors []string`，没有恢复机制（首个语句失败即整体 `return nil`）；错误信息缺位置；一个错误会产生四条层层堆叠的消息，只有最内层那条有信息量。**这三个问题现在都已经修复**，值得看一下修完之后的样子，因为这是 parser 错误处理的标准形态。

**错误已经结构化**（parser.go:20-29）：

```go
type Error struct {
	Line    int
	Column  int
	Message string
	Got     token.Token
}

func (e Error) Error() string {
	return fmt.Sprintf("%d:%d: %s, got %s", e.Line, e.Column, e.Message, e.Got.Type)
}
```

**只在最内层记录。** 中间层（`parseLetStatement`、`parseExpressionStatement` 等）失败时调 `p.logFailure(context)`——只写调试日志，不追加用户可见的错误；只有真正定位到问题的那一层调 `p.newError(message, got)`。判断标准：**这条消息告诉用户新信息了吗？**"let 语句里的值解析失败"没有——用户已经从内层消息知道表达式解析失败了，也能从位置知道它在 let 语句里。

**同步点恢复**（parser.go:96-106）：

```go
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
```

三类同步点的区别：分号和右大括号是语句的**结束**标志，要越过；`let`/`return`/`while`/`if` 是**开始**标志，停在上面让下一轮解析它。`ParseProgram`（parser.go:74-94）在语句解析失败时调用它，然后继续循环——一次解析能报出所有根因错误，而不是只报第一个。

`TestSyncNoHang`（用 goroutine + timeout 跑 `let`、`let =`、`+`、`)`、`let a = ;` 等输入）确认了它不会死循环——循环体本身没有无条件推进，防死循环靠的是循环条件本身排除了所有同步点，最终一定会推进到 EOF 或某个同步点。

**`ParseProgram` 总是返回已解析出的部分**，配 `Errors()` 访问器（parser.go:61）：

```go
prog := p.ParseProgram()
if errs := p.Errors(); len(errs) > 0 {
	for _, e := range errs { fmt.Println(e) }
}
```

这样 REPL 能同时显示多个错误，而 language server 能在有错的情况下仍然拿到部分 AST 做补全。`TestParserRecordsOneErrorPerRootCauseAndRecovers` 和 `TestParserRecoveryKeepsFollowingStatements`（parser_test.go）就是在验证这一整套行为：一个根因一条消息、位置准确、后续语句仍能恢复解析。

## 7. 那个曾经存在的 `nesting` 标志

现在的 `parsePrattPrefixExpression()`（parser.go:347）不带参数，是一个统一的 prefix 分派函数——数组、哈希、`if`、`fn`、字面量全部在一处（parser.go:349-582）。这一节讲的是它**曾经**的样子,以及为什么那个设计是多余的,因为这个教训本身值得记住。

早期版本是 `parsePrattPrefixExpression(nesting bool)`，有两条路：`nesting == false` 时支持数组、哈希、`if`、`fn`；`nesting == true` 时只支持基础字面量。这个设计导致复合字面量**无法嵌套**——`[[1]]`、`f(fn(x){x})`、`{"a":[1]}` 全部解析失败（11 章缺陷 3 有完整分析和八处受影响的调用点）。这里讲它试图解决的问题，以及为什么标准 Pratt parser 不需要它。

### `{` 的真实歧义

```monkey
if (x) { 1 }          // { 是块语句
let h = {"a": 1};     // { 是哈希字面量
```

同一个 token，两种含义。这是真实的歧义，很多语言都遇到——JavaScript 里 `{}` 在语句位置是空块、在表达式位置是空对象，所以 `{} + []` 的结果让人困惑。

### 为什么不需要标志

关键认识：**块语句只出现在语法上确定的位置**。

数一下 Monkey 里块能出现的地方：`if` 的分支体、`while` 的循环体、`fn` 的函数体。**就这三个。** 而这三处都调用同一个 `parseBlockStatement`（parser.go:192）——`if` 在 472 行，`while` 在 242 行，`fn` 在 523 行——从不经过表达式解析器。

所以：**表达式解析器遇到 `{`，它必然是哈希。** 不需要标志，不需要模式，不需要 lookahead。

歧义是"语句位置的 `{`"和"表达式位置的 `{`"之间的，而 parser 的结构本身已经知道自己在哪个位置了——`parseStatement` 在语句位置，`parsePrattExpression` 在表达式位置。**位置信息已经编码在调用栈里**，不需要额外的参数传递。

（唯一残留的情况：语句位置出现 `{`。`{ }` 作为独立语句是什么？Monkey 没有独立的块语句语法——`README.md:27` 的 `<statement>` 里没有 `<block>`。所以 `parseStatement` 遇到 `{` 会走 `parseExpressionStatement`，把它当哈希解析。这是合理的：`{"a":1};` 是一个合法的表达式语句。）

### 修法：已采纳

1. 删掉 `nesting` 参数，改成 `parsePrattExpression(minBP int)`（parser.go:313）
2. 合并两个 prefix 函数，`parsePrattPrefixExpression()`（parser.go:347）不再带参数，包含全部分支
3. 补上了当时缺失的 `TRUE`/`FALSE`（parser.go:571-578）和 `LEFT_PAREN` 分组（parser.go:349-361）

现在的 prefix 分派表：

```text
INT FLOAT STRING IDENT TRUE FALSE  → 字面量 / 标识符（各自 readToken）
NOT MINUS                          → 前缀表达式，右侧 parsePrattExpression(PREFIX_BP)
LEFT_PAREN                         → 分组：readToken, parsePrattExpression(0), 要求 ), readToken
LEFT_BRACKET                       → 数组字面量
LEFT_BRACE                         → 哈希字面量（此处必然是哈希）
IF                                 → if 表达式（body 走 parseBlockStatement）
FUNC                               → 函数字面量（body 走 parseBlockStatement）
default                            → 错误
```

这是标准 Pratt parser 的 prefix 表。**它没有"模式"，因为不需要。**

### 更进一步：分派表 vs switch

原版 Monkey 用 map 注册解析函数：

```go
type prefixParseFn func() ast.Expression
type infixParseFn func(ast.Expression) ast.Expression

p.registerPrefix(token.IDENT, p.parseIdentifier)
p.registerInfix(token.PLUS, p.parseInfixExpression)
```

你用的是 `switch`。两者的权衡：

| | switch | map 注册 |
| --- | --- | --- |
| 可读性 | 所有分支在一处，顺序阅读 | 分散，要跳转 |
| 扩展性 | 改函数 | 加一行注册 |
| 性能 | 编译器优化的跳转表 | map 查找 + 闭包调用 |
| 缺失检测 | `default` 兜底 | `nil` 检查 |

**switch 对手写 parser 更合适**——分支数量有限（十来个），集中在一处更好读。map 注册的价值在于**可以运行时改**，比如让用户定义新运算符（Haskell、Raku 支持这个）。Monkey 不需要，所以你的选择更简单。

`INFIX_BP` 用 map 是对的——那是纯数据，不是行为。

## 8. 列表解析：五处重复的骨架

数组元素、哈希键值对、函数形参、调用实参、索引下标，五处都在解析"逗号分隔的列表"（分别在 parser.go:371-388、399-429、502-521、596-612、622-638）。当前是五份独立的代码，写法上都遵循同一个骨架：**先判 closing token 处理空列表，再进循环；循环体解析一个元素，nil 就立刻返回失败；然后看下一个 token 是逗号（继续）还是 closing（结束）还是别的（报错）。**

早期版本这五处曾经用 do-while 结构（先解析一个元素，再判断是否继续），导致空列表 `[]`、`{}`、`fn(){}`、`a()` 全部解析失败（11 章缺陷 5），而且其中两处漏了 nil 检查，让 nil 悄悄塞进 AST、延迟到 `String()` 才 panic。**现在五处都已经改成"先判 closing 再循环"，nil 检查也都补齐了**——可以用 `[]`、`{}`、`fn(){}`、`a()` 逐一验证，都能正常解析出空列表/空哈希/无参函数/无参调用。

不过**重复本身没有消失**——五份代码仍然是独立维护的五份，只是现在都恰好正确。这仍然是 bug 的温床：下次改动这个模式（比如加一种新的分隔符规则），得同时改五处,漏了一处不会报错,只会在特定输入下悄悄出错。抽成一个函数仍然值得做：

```go
// parseExpressionList 解析逗号分隔的表达式列表。
// 进入时 curToken 指向列表的第一个元素（或直接是 closing）；
// 返回时 curToken 已越过 closing。
// 第二个返回值为 false 表示解析失败（错误已记入 p.errors）。
func (p *Parser) parseExpressionList(closing token.TokenType) ([]ast.Expression, bool) {
	list := []ast.Expression{}

	if p.curToken.Type == closing {      // ← 空列表：先判再循环
		p.readToken()
		return list, true
	}

	for {
		expr := p.parsePrattExpression(0)
		if expr == nil {                 // ← nil 检查，一处即可
			return nil, false
		}
		list = append(list, expr)

		switch p.curToken.Type {
		case token.COMMA:
			p.readToken()
		case closing:
			p.readToken()
			return list, true
		default:
			p.newError(fmt.Sprintf("expected ',' or %s in list", closing), p.curToken)
			return nil, false
		}
	}
}
```

数组用 `parseExpressionList(token.RIGHT_BRACKET)`，调用实参用 `parseExpressionList(token.RIGHT_PAREN)`，索引下标同理。

哈希需要成对解析，形参只接受 IDENT，这两处不能直接复用——但可以照同一个骨架写（**先判空，再循环**）。或者把骨架参数化：

```go
func (p *Parser) parseSeparatedList(closing token.TokenType, parseItem func() bool) bool
```

**收益不只是省代码**：空列表这个边界只需要在一个地方正确，而不是五个地方各自正确。

`parseBlockStatement` 也是同一个模式（解析到 `}` 为止的语句序列），同样应该"先判 `}` 再循环"，同样漏了空的情况（缺陷 12）。它还漏了 EOF 检查——循环条件只判 `RIGHT_BRACE`，未闭合的块靠"语句解析失败"间接退出，这是靠副作用工作。

## 9. 边界与陷阱

**① 漏 `readToken()` 导致死循环。** 游标契约的直接后果。症状是 CPU 100% 而不是报错。**每加一个 prefix 分支，检查它是否推进了游标。**

**② 错误路径不消费 token。** `ParseProgram` 里如果改成"失败后 continue"但不推进游标，同样死循环。任何"跳过错误继续"的逻辑，第一步必须是无条件推进。

**③ do-while 结构处理可空列表。** 五处列表 + 一处块，全部踩了。**判空必须在循环之前。**

**④ 返回 nil 不检查。** `parsePrattExpression` 失败返回 nil，调用方必须检查。当前五处列表里有两处没检查，nil 进了 AST，延迟到 `String()` 或 evaluator 才崩。

**⑤ 类型断言不检查。** parser.go:477、528 有 `block.(*ast.BlockStatement)`——不带 `, ok` 的强制断言。`parseBlockStatement` 的返回类型是 `ast.Statement`，实际总是 `*BlockStatement`，所以现在安全。但**如果以后它在某条路径上返回了别的类型，这里会 panic**。改成返回具体类型 `*ast.BlockStatement` 更好——断言就不需要了。

**⑥ 用 `p.errors` 长度判断成功。** 测试里 `if len(p.errors) != 0` 是可以的，但**生产代码里不要用错误列表长度做控制流**。函数应该用返回值（nil 或 `ok`）表达失败。当前实现是对的（用 nil 返回），保持这样。

**⑦ 优先级表漏了 token。** 加了新中缀运算符但忘了填 `INFIX_BP`，症状是表达式在该运算符处"截断"——前半部分解析成功，后半部分变成下一个语句。**没有报错**，这类 bug 最难发现（`xs[0]` 早期就是这么被漏掉的：主循环没判 `LEFT_BRACKET`，`xs` 被解析成一个独立语句，`[0]` 变成下一条，见 11 章缺陷 4）。防御方法：写一个测试遍历所有中缀 token，断言每个都能解析。

```go
func TestAllInfixOperatorsParse(t *testing.T) {
	ops := []string{"+", "-", "*", "/", "==", "!=", "<", ">", "<=", ">=", "&&", "||"}
	for _, op := range ops {
		input := fmt.Sprintf("a %s b;", op)
		p := New(lexer.New(input), Pratt)
		prog := p.ParseProgram()
		if prog == nil || len(prog.Statements) != 1 {
			t.Errorf("%q: got %v statements, want 1", input, prog)
		}
	}
}
```

这个测试的好处是**加新运算符时会自动提醒你注册**——把 `ops` 列表和 `INFIX_BP` 表对齐检查，甚至可以直接遍历 `INFIX_BP` 的键。

## 10. 验收测试清单

现有的 `parser_test.go`（101 行，4 个测试函数）已经比早期版本完整得多：`TestParser` 断言语句类型和数量，`TestParserErrataCases` 补上了 `String()` 结构断言（覆盖 `true`、分组、嵌套数组/函数、`xs[0][1]`、`f(1)[0]`、空数组/空哈希、`if-else if`、哈希重复键等 14 个用例——正是 11 章那些缺陷的复现用例），`TestParserRecordsOneErrorPerRootCauseAndRecovers` 和 `TestParserRecoveryKeepsFollowingStatements` 验证了第 6 节的错误恢复行为。

但覆盖面还有明显缺口：没有完整的优先级/结合性矩阵（比如 `&&`/`||` 的相对优先级、`1 < 2 && 3 >= 4` 这类混合用例只有零星几条）、没有 float 混算的边界测试、没有错误位置（行列号）的系统性断言、双模式一致性测试完全没写（因为 `RecursiveDescent` 还是 stub）。以下补齐这些：

**① `String()` 结构断言**——扩展现有 `TestParserErrataCases`，把优先级和结合性覆盖完整：

```go
func TestPrecedence(t *testing.T) {
	tests := []struct{ input, want string }{
		// 算术优先级
		{`1 + 2 * 3;`, `(1+(2*3));`},
		{`1 * 2 + 3;`, `((1*2)+3);`},
		{`1 + 2 - 3;`, `((1+2)-3);`},          // 同级左结合
		{`1 * 2 / 3;`, `((1*2)/3);`},
		// 比较与逻辑（你的扩展）
		{`1 < 2 == true;`, `((1<2)==true);`},
		{`a && b || c;`, `((a&&b)||c);`},
		{`a || b && c;`, `(a||(b&&c));`},       // && 优先级高
		{`1 < 2 && 3 >= 4;`, `((1<2)&&(3>=4));`},
		// 前缀
		{`-a * b;`, `((-a)*b);`},
		{`!-a;`, `(!(-a));`},
		{`-(a + b);`, `(-(a+b));`},
		// 后缀
		{`f(1)(2);`, `f(1)(2);`},
		{`xs[0][1];`, `xs[0][1];`},
		{`-xs[0];`, `(-xs[0]);`},
		{`f(a + b);`, `f((a+b));`},
		// 分组
		{`(1 + 2) * 3;`, `((1+2)*3);`},
		{`((1));`, `1;`},
	}
	// 断言 prog.String() == want
}
```

**② 边界情况**

```text
空列表：       [];  {};  fn(){};  a();
单元素：       [1];  {"a":1};  fn(x){};  a(1);
嵌套：         [[1]];  {"a":{"b":1}};  f(fn(x){x});  1+[1][0];
空块：         fn(){};  while(c){}  if(a){}else{1};
无尾分号：     let a = 1        ← 曾经抓住 01 章缺陷 1（EOF 截断），现在已修复
布尔：         true;  false;  !true;  1==true;
浮点：         1.5;  1.5+2;  1+2.5;
if 全组合：    1分支 / 2分支带else / 2分支不带else / 3分支 / 4分支
```

**③ 错误情况**

```text
let 缺标识符：   let = 5;
let 缺等号：     let a 5;
let 缺值：       let a = ;
未闭合：         (1+2;   [1,2;   fn(x){  {"a":1
非法前缀：       + 5;    * 3;
多个错误：       let = 5; let a = 1;    ← 应报 1 错并解析出 1 个语句
错误在末尾：     let    let =    +      ← 不能死循环
```

**④ 双模式一致性**（实现 `RecursiveDescent` 后）

```go
func TestModesAgree(t *testing.T) { /* 对每个输入断言两种模式 String() 相同 */ }
```

**测试辅助函数**值得先写好：

```go
func parseOK(t *testing.T, input string) *ast.Program {
	t.Helper()
	p := New(lexer.New(input), Pratt)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("ParseProgram(%q) errors: %v", input, errs)
	}
	_ = prog.String()      // ← 无条件调一次，把 nil 元素类的延迟 panic 提前暴露
	return prog
}
```

最后那行 `_ = prog.String()` 是当年从缺陷 5 学到的一个习惯：nil 元素在解析时不报错，只在打印时 panic。**让每个解析测试都顺便打印一次**，这类问题就跑不掉——即使当前实现已经不会产生这种 nil 元素，这个习惯仍然值得保留，因为它防的是"未来引入的同类回归"，不是"当前已知的 bug"。

## 11. 练习

以下前六条对应的缺陷（2、3、4、5、12 和错误恢复）都已经修复,这里改成"验证并加固"式的练习——目标不是修 bug,而是补上当年没写的测试,把已经正确的行为用可执行的断言锁住,防止未来的改动悄悄回归。

**1. 给 `TRUE`/`FALSE` 和 `LEFT_PAREN` 分组写专门的表驱动测试。** 这两个分支曾经缺失（缺陷 2、6）。想一想：为什么分组不需要 AST 节点？

**2. 给嵌套复合字面量写回归测试。** `[[1]]`、`f(fn(x){x})`、`{"a":[1]}`、`1+[1][0]` 都应该正常解析——这是删掉 `nesting` 标志之后才能做到的（缺陷 3）。把这些用例固化成 `parser_test.go` 里的表驱动条目。

**3. 给多层索引/调用链写回归测试。** `xs[0][1]`、`f(1)[0]`、`xs[0](1)` 应该都能连续解析（缺陷 4 修复后的行为）。同时想一个问题：`xs[1,2]` 现在会发生什么？多下标是该支持还是该拒绝，BNF 需不需要跟着更新？

**4. 抽出 `parseExpressionList`。** 参照第 8 节的骨架，替换数组、哈希、形参、实参、下标五处重复代码。做完确认空列表全部工作，且 nil 检查只在一处。这仍然是一个真实的、值得做的重构——修复消除的是"行为错误"，没有消除"重复本身"。

**5. 给空块和未闭合块写测试。** `fn(){}`、`while(c){}`、`if(a){}else{}` 都应该正常返回空的 `BlockStatement`（缺陷 12 修复后的行为）；`fn(){`（未闭合）应该报错而不是死循环或 panic。

**6. 给同步点恢复写压力测试。** 在本章现有的 `TestSyncNoHang` 基础上，扩充输入集合：嵌套错误（`let = 1; let a = ; let b = 2;`）、错误发生在块内部（`fn(){ let = 1; }`）、连续多个同步点相邻（`;;;let a=1;`）。**必须配超时测试**——这是防死循环的安全网，即使当前实现已经正确，改动 `synchronize()` 时它能立刻告警。

**7. 让赋值支持索引目标。** 改成"先解析表达式，再看 `=`"。加 lvalue 校验：哪些表达式可以做赋值目标？`5 = x`、`f() = x`、`(a) = x` 分别该怎么处理？

**8. 实现 `RecursiveDescent` 模式。** 按 `README.md:34-43` 的七层 BNF 写七个函数。写完跑双模式一致性测试。**这是本章最有价值的练习**——写完你会真正理解"优先级表"和"函数调用层次"是同一件事的两种表达。

**9. 加一个右结合运算符。** 实现 `**`（幂），用 `{70, 70}`。测试 `2 ** 3 ** 2` 是 `(2**(3**2))` 而不是 `((2**3)**2)`。然后想：它和 `PREFIX_BP=70` 撞了，`-2 ** 2` 会怎样？该是 `-(2**2)` 还是 `(-2)**2`？（Python 选前者，多数语言选前者。你的优先级要怎么排？）

**10. 加三元运算符 `a ? b : c`。** 这是一个"中缀但吃两个操作数"的运算符，Pratt parser 处理它需要在 infix 函数里额外解析 `:` 和第三个操作数。它是右结合的：`a ? b : c ? d : e` 应为 `a ? b : (c ? d : e)`。

**11.（进阶）加 parser tracing。** 参考 `example/02/src/monkey/parser/parser_tracing.go`——在每个解析函数进出时打印缩进的日志。这是调试优先级问题最有效的工具，能直接看出递归的下限传递：

```text
BEGIN parseExpression(minBP=0)
  BEGIN prefix: a
  END prefix
  BEGIN parseInfix(+, rbp=51)
    BEGIN parseExpression(minBP=51)
      BEGIN prefix: b
      ...
```

**12.（进阶）错误消息带源码上下文。** `Token.Line`/`Column`（早期版本是缺陷 B 记录的恒为 0 的 `Pos`，现在已经正确设置）已经能定位错误，在此基础上用 `Lexer.GetLine` 取出源码那一行，让错误输出变成：

```text
3:12: expected ')' after arguments, got ';'
    let x = f(1, 2;
                  ^
```

需要：行列信息、按行切分源码、计算下划线位置。这是 parser 面向用户的最后一环，也是"好编译器"和"能用的编译器"的区别所在。

## 小结

- 语句用递归下降（第一个 token 决定），表达式用 Pratt（中间的运算符决定）
- Pratt 的全部秘密是一个比较：`当前运算符的 lbp >= 下限` 才吸收
- **左结合 = 右操作数的下限比自己高 1**；`{n, n}` 是右结合
- 你的 `BP{lbp, rbp}` 把结合性变成了数据，比原版的单值 + 严格大于更灵活
- `PREFIX_BP` 必须高于所有中缀（`-a*b` 是 `(-a)*b`），`POSTFIX_BP` 必须高于 `PREFIX_BP`（`-xs[0]` 是 `-(xs[0])`）
- **游标契约：返回时 `curToken` 指向结构之后**——这是你和书上最大的差异，每个 prefix 分支都必须 `readToken()`，漏了就死循环
- 赋值靠两 token lookahead 只能处理简单标识符；"先解析表达式再看 `=`"更通用
- 曾经的 `nesting` 标志想解决 `{` 的歧义，但块只出现在三个确定位置，位置信息已在调用栈里，不需要标志——现在已经删掉，合并成一个 prefix 函数
- 五处列表解析的重复曾经导致了同一个 bug（空列表失败）的五个副本——现在五处都已修复，但重复本身仍在，抽出骨架仍值得做
- 测试必须断言 `String()` 结构，只断言语句数量抓不住结构错误

下一章开始执行：先设计运行时对象和环境。

