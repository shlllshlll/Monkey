# 16 调试器：断点、单步与跨执行路径的统一协议

## 本章要回答的问题

到目前为止，"调试"这个词在本系列文档里只有一个含义：**你**在调试**自己的实现**——lexer 有没有按不变量走、AST 建得对不对、VM 的栈平衡有没有被破坏。[10 章](10-debugging.md)给的是这件事的方法论：分层定位、差分测试、症状→根因映射表。那一章的读者是实现者，工具是内部诊断（token 流打印、`prog.String()`、反汇编、栈 trace）。

这一章问一个不同的问题：**Monkey 语言的使用者**想在自己写的 Monkey 程序里设置断点、单步执行、在暂停时查看变量——就像用 pdb 调试 Python、用 delve 调试 Go 一样。这不是"打印更多内部状态"，是**暂停一个正在运行的程序，让人在特定的执行点检视它，然后决定下一步**。这件事要跨越本系列定义的四条执行路径都能做到：

- **Evaluator**（[05 章](05-evaluator.md)）：树遍历，Go 的函数调用栈就是 Monkey 的调用栈
- **RPN 引擎**（[03b 章](03b-rpn-engine.md)）：线性指令，没有跳转
- **VM**（[06-09 章](06-bytecode.md)）：栈式字节码，有帧、有跳转
- **Native**（[15 章](15-native-codegen.md)）：机器码，三条路线里只有一条对调试友好

四条路径的执行模型天差地别——树 vs 线性数组 vs 带跳转的字节码 vs 机器码——但断点这件事在用户眼里应该是同一个体验：**在第 N 行设一个断点，程序跑到那一行就停，此刻我能看见调用栈和变量**。这一章要做的是找出这四条路径**唯一共享的坐标**（源码行号），在此之上定义一套统一协议，再逐条路径回答"这套协议在这条路径上具体怎么落地"。

**现状提醒**：写这一章时，`evaluator`/`code`/`compiler`/`vm`/native 后端全部还没有代码，只有 [04](04-runtime.md)-[15](15-native-codegen.md) 章的设计文档。这一章延续同样的写法——**先把契约定下来，实现的时候照着写，调试钩子就是设计自带的，不是事后拿榔头砸进去的补丁**。所以这里不会有可以直接跑的 Go 程序；练习部分会指出"等你写到 05 章/08 章代码时，回来做这几件事"。

## 1. 唯一共享的坐标：源码行号

四条路径各自的"位置"概念完全不同：

| 路径 | 原生的"位置"概念 |
| --- | --- |
| Evaluator | 当前 `ast.Node`（有 `Token.Line/Column`），加上 Go 调用栈的深度 |
| RPN | 指令数组的下标 `i` |
| VM | `Frame.ip`（字节码字节偏移）+ `frames` 栈深度 |
| Native | 机器指令地址（若无调试信息，无法映射回任何东西） |

这四个"位置"没有一个是彼此的子集或超集，唯独有一件事是公共的：**它们最初都来自同一份源码，而源码有行号**。`token.Token{Line, Column}`（[01 章](01-lexer.md)）已经在 lexer 阶段把行列信息钉在每个 token 上，AST 的每个节点又都嵌了自己的 `Token`（[02 章](02-ast.md)），所以"这段字节码/这条 RPN 指令/这次 `Eval` 调用，对应源码第几行"这件事，只要在编译/降低阶段把行号带下去，任何一条路径都答得出来。

**这决定了断点的坐标系：断点按（文件, 行号）设置，不是按 AST 节点指针、不是按字节码偏移、不是按指令下标。** 后三者是每条路径的内部实现细节，会在编译、优化甚至每次运行之间变化；行号是用户实际看到、实际会输入的东西。

一个必须承认的限制（[02 章边界与陷阱④](02-ast.md)已经指出）：**AST 节点只有起始位置，没有结束位置**。`if (a) { 1 } else { 2 };` 整个 `IfExpression` 只知道自己从 `if` 那个 token 开始，不知道到哪结束。这意味着"在这一行设断点"在语义上更准确的说法是"在**以这一行为起点的**语句/表达式的求值/执行入口处设断点"——多语句共享一行（`let a=1; let b=2;`）时，粒度只能到"这一行第一次被触碰"，不能精确定位到行内第二个语句。这是 v1 协议要接受的粗粒度，不是这一章要顺手解决的事；02 章已经指出"现在改代价小，等 evaluator 写完再改要动所有节点"，如果未来要做行内精确断点，得先回去给 AST 加结束位置。

## 2. 统一协议：一个接口，四种实现

```go
type SourcePos struct {
    Line   int
    Column int
}

type StopReason int

const (
    StopEntry      StopReason = iota // 程序刚开始，尚未执行第一条语句
    StopBreakpoint                   // 命中断点
    StopStep                         // 单步完成
    StopExited                       // 程序正常结束
    StopError                        // 求值/执行出错
)

type FrameInfo struct {
    FuncName string     // "<main>" 或函数名；native 模式下可能是 "<unknown>"
    Pos      SourcePos
    Mode     string     // "eval" / "rpn" / "vm" / "native"
}

type Variable struct {
    Name  string
    Value string // Inspect() 的结果，调试器只关心可读的字符串表示
}

type Debugger interface {
    SetBreakpoint(line int) (id int)
    ClearBreakpoint(id int)

    Run() StopReason        // 从入口跑到第一次停下
    Continue() StopReason   // 从当前停止点继续跑到下一次停下
    StepInto() StopReason   // 执行下一步，若是函数调用则进入
    StepOver() StopReason   // 执行下一步，函数调用整体跨过

    Frames() []FrameInfo     // 从当前帧到最外层，索引 0 是当前帧
    Locals(frameIndex int) []Variable
}
```

这个接口故意**不包含**"查看字节码"、"查看寄存器"这类某条路径特有的能力——那些是每条实现自己额外提供的调试信息（[10 章](10-debugging.md)的分层观察工具本来就够用，直接复用，不用在这个接口里重新定义一遍反汇编器）。`Debugger` 接口只承诺四条路径都答得出来的最小公共集：断点、运行控制、调用栈、变量。**任何一条路径如果连这四样都给不出来，就不算是"支持调试"，只能算是"支持打印内部状态"。**

`StepOver` 和 `StepInto` 的区别只在"遇到函数调用时怎么办"，这个区别在四条路径上都要能表达，但实现代价完全不同——evaluator 里"跨过"意味着让内部的递归 `Eval` 调用自然完成（本来就是这样，因为 Go 调用栈天然处理了这件事）；VM 里"跨过"意味着记住当前 `framesIndex`，一直跑到 `framesIndex` 回到同一层为止；RPN 因为没有跳转和分支，`StepOver`/`StepInto` 在一条 `CALL` 指令上等价于"要不要在被调函数内部也允许暂停"。

## 3. Evaluator 模式：调用栈是 Go 白送的

这是四条路径里实现代价最低的一条，理由很直接：`Eval(node ast.Node, env *object.Environment) object.Object`（[05 章](05-evaluator.md)）本身就是递归，Monkey 程序的调用栈**就是** Go 的调用栈。不需要自己维护一份栈结构去追踪"现在在哪一层"。

调试钩子挂在 `Eval` 的入口：

```go
type EvalDebugger struct {
    breakpoints map[int]bool          // 行号 -> 是否启用
    frameStack  []FrameInfo           // 显式维护，因为 Go 调用栈里的局部变量名拿不到
    stopChan    chan StopReason       // 简化表示，实际可以用更合适的同步原语
}

func (d *EvalDebugger) onBeforeEval(node ast.Node, env *object.Environment) {
    line := node.(interface{ TokenLine() int }).TokenLine() // 示意：从 node 的 Token 取 Line
    if d.breakpoints[line] {
        d.pause(node, env)
    }
}
```

**为什么 `frameStack`要显式维护，而不是查 Go 自己的调用栈**：Go 的调用栈知道"这是第 17 层 `Eval` 调用"，但不知道"这一层对应 Monkey 函数 `fib`，此刻局部变量 `n` 等于 5"——这些语义信息只存在于 `ast.Node` 和 `object.Environment` 里，Go runtime 不认识 Monkey 的函数概念。所以每次进入 `evalCall`（对应 `ast.PostfixExpression{Operator: LEFT_PAREN}` 的求值，见 05 章）时，调试器要显式 push 一个 `FrameInfo{FuncName: ..., Mode: "eval"}`；`Eval` 返回时 pop。这个栈和 Go 调用栈**同构但独立**——它是为了给 `Frames()` 提供语义信息，不是为了控制执行流程。

`Locals(frameIndex int)` 直接依赖 [04 章练习 7](04-runtime.md) 提议过、但当时没有实现的 `Environment.Names() []string`——返回整条 `outer` 链上所有可见的名字（内层遮蔽外层，去重）。**这一章把这个方法从"练习提议"提升为"debugger 的硬依赖"**：写 04 章的 `Environment` 时如果打算支持调试器，这个方法不能再等到"以后需要再加"。

暂停/恢复的控制流：`onBeforeEval` 命中断点后，调试器需要**阻塞当前的 `Eval` 调用**直到用户发出 `Continue`/`StepInto`/`StepOver`。最简单的实现是让 `pause` 在一个 channel 上阻塞读，调试器的命令处理协程往这个 channel 写；这意味着 evaluator 模式下调试器天然是"单线程程序在一个 goroutine 里跑，调试命令从另一个 goroutine 发进来"的结构，不需要信号、不需要进程间通信——这是树遍历模式最大的隐性优势：**暂停一个 Go 函数调用栈，只需要让它在某处 block，不需要真的"冻住"任何东西。**

## 4. RPN 模式：最简单，因为没有跳转

`03b-rpn-engine.md` 定义的 `RPNProgram []Instruction` 是纯线性结构——**没有 `if`/`while`，所以没有跳转指令**（第 3 节已经说明这是刻意的设计边界）。这个限制反过来让调试变得异常简单：

```go
type RPNDebugger struct {
    program     RPNProgram
    pc          int             // 下一条要执行的指令下标
    stack       []object.Object
    breakpoints map[int]bool    // 这里的"行"其实是指令下标或对应的源码行，取决于是否保留了行号映射
}
```

因为程序是一条直线，"运行到下一个断点"不存在"可能要等很久"的问题——最多执行 `len(program)` 步就结束（`CALL` 会递归进被调用函数的指令序列，但依然没有循环，深度受调用栈限制）。`StepInto`/`StepOver` 在这里的区别退化成"遇到 `CALL` 时，要不要把被调函数的指令序列也当成可暂停的一部分"。

RPN debugger 的价值不在于它本身有多少人会用——**它是验证第 2 节这套统一接口是否设计合理的最廉价试验场**。如果一个协议在"没有跳转、没有分支、没有帧"的最简单模型上都显得笨重，那它在 VM/native 上只会更笨重。建议的实现顺序（第 7 节）把 RPN 排在 evaluator 之后、VM 之前，就是这个理由。

`Instruction.Operand any` 是字面量或整数，没有变量名信息（`LOAD` 的操作数是变量名字符串，但没有"当前作用域有哪些变量"这个概念——RPN 引擎本身没有 `Environment`，只在需要 `LOAD` 时才查一个外部传入的环境）。所以 `Locals()` 在 RPN 模式下要么依赖外部传入的同一个 `object.Environment`（如果 RPN 求值时确实接了一个），要么只能返回栈上的值，没有名字——这是 RPN 作为"纯计算"引擎的代价，调试器不能假装它有 evaluator 那样的作用域信息。

## 5. VM 模式：断点要靠一张还不存在的表

这是最复杂的一条路径，因为 VM 的"位置"（`Frame.ip`，字节码字节偏移）和用户的"位置"（源码行号）之间没有现成的映射。

### 5.1 前提条件：ip → 行号表

[08 章 §12](08-vm.md) 已经指出这个缺口：字节码没有行号信息，VM 报错定位不到源码行；[08 章练习 11](08-vm.md) 提议给 `CompiledFunction` 加一个类比 Python `co_lnotab`、Go pcln 表的行号表，但当时只是"练习提议"，不是必须做的事。

**这一章把它从提议变成前提条件**：VM 模式下按行设置的断点，唯一可行的实现方式是维护

```go
type CompiledFunction struct {
    Instructions  code.Instructions
    NumLocals     int
    NumParameters int
    Lines         []int   // Lines[i] = Instructions 第 i 字节所属的源码行号
}
```

编译期发出每条指令时，同步往 `Lines` 写入对应的行号（来源是被编译的 `ast.Node.Token.Line`）。断点命中判据不是扫描字节码找某一行对应哪个 offset（那是查找方向反了），而是**编译器发出跳转/调用/每条语句的第一条指令之前，检查这一行是不是断点行，是就在这个 offset 插入一个 `OpBreakpoint`**（新增的一条 0 操作数指令，行为等价于"陷入调试器"）——或者更不侵入的做法：VM 主循环每轮取指后查一次 `Lines[ip]` 有没有变化且新行是断点行。两种做法的权衡：前者（插入指令）零运行时开销但每次改断点要重新编译；后者（主循环查表）不需要重新编译但每条指令都多一次查表开销。**这一章不替读者做最终选择，但要求两者之一必须存在**——没有 `Lines` 表，VM 模式的断点根本无法按用户输入的行号定位。

### 5.2 单步执行必须原样复刻 ip 的既有约定

[08 章](08-vm.md)的取指循环是：

```go
for vm.currentFrame().ip < len(vm.currentFrame().Instructions())-1 {
    vm.currentFrame().ip++
    ip := vm.currentFrame().ip
    ins := vm.currentFrame().Instructions()
    op := code.Opcode(ins[ip])
    switch op {
        // 每个跳转/回填目标写入的都是 target - 1，因为循环头会再 ++ 一次
    }
}
```

`Frame.ip` 初始化成 `-1`（`NewFrame` 里），因为循环头是"先 `++` 再取指"。**单步执行的实现如果不是直接调用这同一段 `Run()` 逻辑，而是自己重新写一套"跑一条指令"的代码，必须一字不差地复刻这个 `ip++`-在前、跳转目标写`target-1` 的约定**——08 章反复强调这是 VM 最常见的 bug 来源，调试器的单步实现如果自己重新发明这套逻辑而不是复用 `Run()` 内部的一次迭代，等于给自己新开一个可能出现"差一个字节"的地方。**正确做法是把 `Run()` 的主循环体重构成一个可以被"跑一轮"和"跑到停"两种模式共用的函数**，调试器的 `StepInto` 直接调用"跑一轮"，不重新实现取指逻辑。

### 5.3 查看局部变量需要符号表活到运行期

`Locals(frameIndex int)` 要回答"当前帧里，槨位 0 叫什么名字、值是什么"。值好拿——`stack[frame.basePointer+i]`；名字不好拿，因为 [07 章](07-compiler.md)的 `SymbolTable{Outer, FreeSymbols, ...}` 和它内部的 `Symbol{Name, Scope, Index}` 是**编译期**结构，编译完成后通常就被丢弃了，`Bytecode{Instructions, Constants}`（06 章）里根本不包含它。

**这一章要求：如果打算支持 VM 模式下的变量查看，编译期符号表（或者至少一份 `slot index → name` 的映射）必须和 `CompiledFunction` 一起保留到运行期**，不能等到写 VM 的时候才发现查不到名字、回头再加。具体做法可以是 `CompiledFunction` 除了 `Lines` 外再加一个 `LocalNames []string`（下标对应 `OpGetLocal`/`OpSetLocal` 的操作数），全局变量同理需要一份 `GlobalNames`。这份映射只在调试模式下会被用到，正常执行路径完全不碰它，所以不会拖慢没开调试器的运行性能——**前提是编译器在生成 `CompiledFunction` 的时候就把它写进去，不是运行时反向猜**。

### 5.4 复用现成的观测原语

`vm.LastPoppedStackElem()`（08 章 §2，靠 `pop()` 不清零槽位实现）和 [08 章练习 10](08-vm.md) 提议的 `vm.trace bool` 开关（打印 `ip/op/sp/stack/framesIndex`）都可以直接作为"暂停后打印当前状态"的原语复用——这正是第 2 节接口里没有单独定义"查看栈"方法的原因：**这类信息属于 VM 特有的调试细节，不需要塞进跨路径的公共接口，直接用 08/10 章已经设计好的工具**。`Frames()` 的实现就是遍历 `vm.frames[0:vm.framesIndex]`，每个 `Frame` 换算出 `FrameInfo{FuncName: 从 cl.Fn 反查函数名（如果有的话）, Pos: 用 Lines[ip] 换算, Mode: "vm"}`。

## 6. Native 模式：三条路线，调试支持不对等

[15 章](15-native-codegen.md)"责任转移"一节已经点破这件事：VM 能做 `LastPoppedStackElem` 是因为 VM 自己知道每个栈值的类型和含义；机器码没有这层信息，"这个寄存器此刻的值代表什么"要靠**单独生成的调试信息**（DWARF 或等价格式）才能回答，这是**额外的工程，不是免费的**。15 章三条路线在这件事上的处境完全不对等，这一章要如实说清楚，不假装它们一样难：

- **路线三（转译到 C/Go）**：调试体验基本是白送的。生成的 C/Go 源码本身就是可调试的文本，`gcc -g` / `go build -gcflags="-N -l"` 加上 gdb/delve 直接可用，**不需要自己实现任何调试协议**——设置断点这件事，直接对生成的 C/Go 源码打断点即可，前提是转译时保留了"哪一行 Monkey 源码对应哪一行生成代码"的映射（这本身也是一种简化版的行号表，和 5.1 节 VM 的 `Lines` 是同一个思路，只是映射的目标从字节码 offset 换成了生成代码的行号）。**如果三条路线里必须选一条支持调试器，选这条，工作量和另外两条完全不是一个量级。**
- **路线二（LLVM IR）**：LLVM 自带的工具链原生支持生成 DWARF 调试信息（`llvm.dbg.*` intrinsic），意味着理论上可以复用 gdb/lldb，但 15 章本身没有展开这部分怎么接线——如果要走这条路，第一步是去查 LLVM 的 debug info 生成 API，这属于"15 章地图之外的下一步"，这一章不重新展开，只指出方向存在。
- **路线一（手写机器码 + ELF）**：什么都没有，需要手写 `.debug_line`/`.debug_info` 这类 DWARF 段——这和 15 章本身说"路线一性价比最低"的结论完全一致，调试信息这一项进一步加重了这个结论的分量。如果确实要走这条路，工作量级和 15 章描述的"寄存器分配、指令选择"是同一个级别，这一章不给出具体方案。

**结论**：native 模式的调试支持不是"实现一遍统一协议就完事"，是**路线选择本身决定了调试器有没有存在的可能**。第 2 节的 `Debugger` 接口在路线三上可以照抄 VM 模式的思路（`Lines` 换成"生成代码行号"，`Locals` 换成读生成代码里对应的变量），在路线一/二上目前没有可执行的落地方案。

## 7. 实现顺序建议

按已经确认的复杂度递增排：

1. **Evaluator**——调用栈是 Go 白送的，唯一要做的事是显式维护 `frameStack` 和补上 `Environment.Names()`。用 `fib(10)` 递归验证在某一层暂停、查看 `n` 的值。
2. **RPN**——没有跳转，验证同一套 `Debugger` 接口是否够用，成本几乎为零。
3. **VM**——需要先落地 `Lines` 表和 `LocalNames`/`GlobalNames` 映射，单步执行要复用 `Run()` 的主循环体而不是重新发明。
4. **Native（路线三优先）**——转译路线直接依赖目标语言现成的调试器；路线一/二暂不实现，只在文档里留出方向。

每一步都在验证同一套接口，成本递增，这样如果第 3 步发现接口设计不够用，返工成本还没有堆到第 4 步那么高。

## 8. 边界与陷阱

**① AST 没有终止位置，断点粒度只能到语句/表达式起点。** 一行多个语句时，断点会在这一行第一次被触碰时命中，无法区分行内第二个语句——这是 02 章已经指出的既有限制，不是这一章要解决的事。

**② VM 断点依赖的 `Lines` 表如果没跟编译器同步更新，会指向错误的行。** 编译器每加一种新语法结构、每次调整指令发射顺序，都必须同步检查 `Lines` 的写入点有没有跟着改——这张表和字节码本身一样，是编译器输出的一部分，不是事后补的元数据，漏更新的后果是"断点设在第 10 行，实际停在第 8 行执行的指令上"，比"完全没有行号信息"更危险，因为它看起来是对的。

**③ `Locals()` 的 side-table 如果按指针缓存，会踩 12 章同一个坑。** [12 章 §13 陷阱⑩](12-static-monkey.md)记录过：`IfBranch.Body`/`FunctionExpression.Body` 是值类型不是指针（严格说是 `*BlockStatement`，见 11 章附加观察 E 的修复记录；但类似的值类型字段在 AST 别处仍存在），如果调试器要给"watch 表达式"做结果缓存，用 `for _, x := range slice { cache[&x] = ... }` 这种写法会缓存到循环临时变量的地址而不是真正的元素地址，查的时候永远查不到。用索引遍历（`&slice[i]`）或者干脆不按指针做 key。

**④ Evaluator 模式下的暂停会让整个宿主进程的这个 goroutine 阻塞。** 如果调试器和被调试程序跑在同一个进程里（第 3 节的设计默认如此），断点命中后 `Eval` 调用栈会一直占着这些 Go 栈帧不释放，直到用户 `Continue`。这在交互式场景下没问题，但如果调试器要支持"超时自动继续"之类的功能，需要额外的定时器机制，不是这一章接口本身管的事。

**⑤ VM 单步如果重新实现取指逻辑而不是复用 `Run()` 的循环体，几乎必然会在 `ip++`/`target-1` 的约定上出错。** 见 5.2 节，这是本章最容易踩、后果最隐蔽的一条——症状是"单步跟着跳转走，落地位置永远差一个字节，执行的是操作数而不是下一条指令"。

## 9. 练习

**1. 给 evaluator 加最小可用的 `onBeforeEval` 钩子。** 等你写到 05 章的代码时，支持一个断点（写死在一个全局变量里，不用做完整的 `SetBreakpoint` API），验证在 `fib(10)` 递归到某一层时能暂停，并打印当前 `env` 里的 `n`。

**2. 补上 04 章练习 7 提议的 `Environment.Names()`。** 这是本章 evaluator 模式 `Locals()` 的硬依赖，动手实现比只读练习描述更能看出"去重时保留哪一层"这个问题的具体样子。

**3. 给 RPN 引擎的 `CALL` 指令加一个"是否允许进入被调函数暂停"的开关。** 用来体会 `StepOver`/`StepInto` 在没有跳转、只有调用的模型上是什么样子——这比在 VM 上直接实现容易验证十倍。

**4. 设计 `CompiledFunction.Lines` 的具体编码。** 最简单的是 `[]int`，下标对应字节码字节偏移，但大多数字节偏移的行号和前一个字节相同，直接用定长数组会有很多冗余——研究一下 Python 的 `co_lnotab`/`co_linetable` 或者 Go 的 pcln 表用了什么压缩方式，想一想值不值得在 Monkey 上照搬。

**5.（进阶）验证 5.2 节的担忧。** 先按"重新实现一套跑单条指令的逻辑"的方式写一版 VM 单步，故意不去对照 `Run()` 的主循环体，构造一个包含 `while` 循环的测试程序，单步执行，看看跳转发生时会不会真的落在错误的字节上。再把单步改成复用 `Run()` 内部抽出来的"跑一轮"函数，对比两版的行为差异。

## 小结

- 这一章解决的是"给 Monkey 用户内建一个交互式调试器"，和 10 章"调试你自己写的实现"是互补关系，不是同一件事的重复
- 四条执行路径（evaluator/RPN/VM/native）内部的"位置"概念互不相同，唯一共享的坐标是**源码行号**——断点必须按行设置，不是按 AST 指针/字节码偏移/指令下标
- 统一协议只承诺四条路径都答得出来的最小公共集：`SetBreakpoint`/运行控制/`Frames`/`Locals`；每条路径特有的调试细节（反汇编、trace）直接复用已有章节的工具，不塞进这个接口
- **Evaluator 最简单**：调用栈是 Go 白送的，只需要显式维护一份带语义信息的 `frameStack`，`Locals` 依赖 04 章提议但未实现的 `Environment.Names()`
- **RPN 最适合做验证**：没有跳转，用来低成本检验这套协议设计是否合理
- **VM 最复杂**：需要一张原本只是"练习提议"的 `Lines`（ip→行号）表，需要把编译期符号表的名字信息带到运行期，单步执行必须复用 `Run()` 的主循环体，绝不能重新实现取指逻辑——`ip++` 在前、跳转目标写 `target-1` 的约定差一步就会让单步落错位置
- **Native 三条路线的调试支持完全不对等**：转译路线基本免费，LLVM IR 路线有方向但没展开，手写机器码路线目前没有可执行方案——这不是这一章要解决的事，是路线选择本身带来的既定代价
- 建议实现顺序 evaluator → RPN → VM → native(转译优先)，每一步验证同一套接口，返工成本递增但可控
