# Monkey：从解释器到字节码虚拟机

用 Go 实现一门小型动态语言 Monkey：同一棵 AST 先由 evaluator 直接执行，再由 compiler 编译成字节码交给 VM 执行。两条执行路径必须对同一份源码给出相同结果——这个约束贯穿整个项目。

```text
源代码 → Lexer → Token → Parser → AST ──→ Evaluator → Object
                                      └──→ Compiler → Bytecode → VM → Object
```

## 当前状态

```text
token/     ✓   lexer/     ✓   ast/       ✓   parser/    ✓（Pratt）
repl/      ✓   只打印 token
object/    ✗   evaluator/ ✗   code/      ✗   compiler/  ✗   vm/  ✗
```

**`go build ./... && go vet ./... && go test ./...` 现在全绿。** [11 章](docs/11-errata.md)记录的 12 个缺陷（EOF 截断、`true`/`false` 无法解析、复合字面量不能嵌套、`xs[0]` 解析成两个语句、零参调用 panic、括号分组缺失等）已在 `cc3132f` 全部修复并有回归测试。**该章保留下来是回顾性质**——展示这些 bug 长什么样、根因在哪、怎么定位、怎么修，这本身是词法/语法分析里最典型的一批错误案例。章节顶部有状态标注说明当前已修复。

仍未实现：`RecursiveDescent` 解析模式（`parseExpressionRecursiveDescent` 是占位）；`<identifier>` 的 BNF 允许标识符中间带数字（`x1`），当前实现会切成 `ident("x")` + `int("1")` 两个 token。这两点不是缺陷，是待办——留给 03 章练习。

## 语言简介

Monkey 支持变量、整数/浮点数/布尔值/字符串、前缀和中缀运算、条件、`while`、函数、闭包、调用、数组、哈希和索引。`let` 创建新绑定，`=` 更新已有绑定（不创建）。

```monkey
let add = fn(a, b) { a + b };
let xs = [1, 2, 3];
let total = 0;
let i = 0;
while (i < 3) { total = total + xs[i]; i = i + 1; }
add(total, 4);
```

### 相对原版 Monkey 的扩展

| 扩展 | 说明 |
| --- | --- |
| 浮点数 | `IntegerLiteral` / `FloatLiteral` 双节点，混合运算提升为 float |
| `while` 语句 | 原版无循环 |
| 赋值语句 | `x = 1`，靠 `IDENT` + `ASSIGN` 双 token lookahead 与表达式语句区分 |
| `&&` `\|\|` | 短路求值；单个 `&` / `\|` 是词法错误（不提供按位运算） |
| `>=` `<=` | 原版只有 `<` `>` |
| 字符串转义 | `\n \r \t \\` 在词法阶段解码 |
| `Token.Line`/`Column` | 位置字段，用于错误定位（`Lexer.GetLine` 可取回源码行） |
| if 分支扁平化 | `Branches []IfBranch` 替代嵌套的 `Alternative` |
| 统一后缀节点 | `PostfixExpression` 合并了调用和索引 |
| 显式 binding power | `BP{lbp, rbp}` 替代单值 precedence，结合性成为数据 |
| 双解析模式开关 | `Pratt` / `RecursiveDescent`（后者待实现） |

## 核心 BNF

```bnf
<program> ::= { <statement> } EOF
<statement> ::= <let> | <assignment> | <return> | <while> | <expr-statement>
<let> ::= "let" <identifier> "=" <expression> [ ";" ]
<assignment> ::= <identifier> "=" <expression> [ ";" ]
<return> ::= "return" <expression> [ ";" ]
<while> ::= "while" "(" <expression> ")" <block>
<expr-statement> ::= <expression> [ ";" ]
<block> ::= "{" { <statement> } "}"
<expression> ::= <logical-or>
<logical-or> ::= <logical-and> { "||" <logical-and> }
<logical-and> ::= <equality> { "&&" <equality> }
<equality> ::= <comparison> { ( "==" | "!=" ) <comparison> }
<comparison> ::= <sum> { ( "<" | ">" | "<=" | ">=" ) <sum> }
<sum> ::= <product> { ( "+" | "-" ) <product> }
<product> ::= <prefix> { ( "*" | "/" ) <prefix> }
<prefix> ::= ( "!" | "-" ) <prefix> | <postfix>
<postfix> ::= <primary> { "(" [ <arguments> ] ")" | "[" <expression> "]" }
<primary> ::= <identifier> | <number> | <boolean> | <string> | <array> | <hash>
            | "(" <expression> ")" | <if-expression> | <function>
<if-expression> ::= "if" "(" <expression> ")" <block> [ "else" <block> ]
<function> ::= "fn" "(" [ <parameters> ] ")" <block>
<arguments> ::= <expression> { "," <expression> }
<array> ::= "[" [ <expression> { "," <expression> } ] "]"
<hash> ::= "{" [ <expression> ":" <expression> { "," <expression> ":" <expression> } ] "}"
<number> ::= <integer> | <float>
<integer> ::= <digit> { <digit> }
<float> ::= <digit> { <digit> } "." <digit> { <digit> }
<identifier> ::= <letter> { <letter> | <digit> }
```

优先级从低到高：`||`、`&&`、`== !=`、比较、`+ -`、`* /`、前缀、调用/索引；二元运算左结合。

> **这份 BNF 与实现只剩一处已知差距**：`<identifier> ::= <letter> { <letter> | <digit> }` 允许标识符中间带数字，当前 lexer 不允许（`x1` 被切成 `ident("x")` + `int("1")` 两个 token）。括号分组和布尔字面量此前也不一致，已在 [11 章](docs/11-errata.md)记录的修复中补上。

## 文档导航

| 章节 | 内容 |
| --- | --- |
| [00 总览与心智模型](docs/00-overview.md) | 两条执行路径、各层不变量、**语义契约表**、推荐路线 |
| [01 Lexer](docs/01-lexer.md) | 游标不变量、最长匹配、float/转义/`&&` 的扩展代价、手算 trace |
| [02 AST](docs/02-ast.md) | 从 BNF 推导节点、marker 方法模式、两处设计取舍、`String()` 作为规范化打印器 |
| [03 Parser](docs/03-parser.md) | 递归下降 + Pratt、`BP{lbp,rbp}` 与左右结合推导、**游标契约**、`nesting` 标志分析、错误恢复 |
| [03b 支线：RPN 表达式引擎](docs/03b-rpn-engine.md) | 不建 AST 直接发射 RPN vs 建树后降低，两条路径为何殊途同归 |
| [04 运行时对象与环境](docs/04-runtime.md) | Object 接口、Environment 嵌套与遮蔽、`Define`/`Lookup`/`Assign` 三分、闭包捕获 |
| [05 Evaluator](docs/05-evaluator.md) | Eval 分派、return 与错误传播、int/float 混算、短路求值、闭包与递归 |
| [06 字节码](docs/06-bytecode.md) | 从抽象机器推导指令集、栈效果契约、编码与反汇编、常量池 |
| [07 Compiler](docs/07-compiler.md) | 栈平衡不变量、**跳转回填**、扁平 if 链的编译、符号表与作用域 |
| [08 VM](docs/08-vm.md) | 取指-执行循环、栈与 `sp` 不变量、**Frame 与 basePointer**、调用返回协议 |
| [09 闭包与集合](docs/09-closures.md) | free 变量解析顺序、`OpClosure`/`OpGetFree`、`OpCurrentClosure` 与递归 |
| [10 调试与验收](docs/10-debugging.md) | 分层定位、**差分测试**、症状→根因映射表、从缺陷学测试设计 |
| [11 勘误](docs/11-errata.md) | 当前实现的 12 个缺陷：复现、根因、修法、回归测试 |
| [12 静态版 Monkey](docs/12-static-monkey.md) | 类型表示、checker、推断、typed AST 降低成非装箱字节码 |
| [13 优化](docs/13-optimization.md) | 常量折叠、死代码消除、窥孔优化、内联缓存、类型特化的正确性论证、**AOT 原生优化速览** |
| [14 JIT 探索](docs/14-jit.md) | 热点探测、method/tracing JIT 的取舍、guard 与去优化、OSR |
| [15 原生代码生成](docs/15-native-codegen.md) | 直写机器码/ELF、对接 LLVM IR、transpile 到 C/Go 三条路线，**runtime 与 GC 的取舍** |
| [16 调试器](docs/16-debugger.md) | 跨 evaluator/RPN/VM/native 的统一断点协议、按行号定位、`Frame`/`Locals` 接口 |

前 3 章对照已有代码讲设计动机与扩展代价；04 章之后给数据结构、契约和推导，**不给完整可抄的实现**——中间的部分留给你写。13-15 章是可选的进阶探索，建议在 10 章的差分测试跑通之后才开始，因为优化和后端替换的唯一验收标准就是"行为不变"。16 章跨越 evaluator/VM/native 三条执行路径，建议等对应章节的代码写出来之后再回头对照实现。

## 开发循环

```bash
cd compiler/monkey
go build ./... && go vet ./... && go test ./...
```

三条都绿才算一次有效迭代——`go vet` 是零成本的，曾经抓出过一个格式化错误（[11 章缺陷 7](docs/11-errata.md)），值得保留在流程里。

## 参考

`example/01`–`example/04` 是《Writing an Interpreter in Go》各阶段的书上代码，可作对照。注意本项目的语言已扩展，AST 与 parser 结构和书上不同。
