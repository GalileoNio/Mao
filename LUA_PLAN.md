# Mao 对接 Lua 与双向源码转换方案

## 1. 目标

本方案规定 Mao 调用 Lua 模块、Mao 源码生成 Lua，以及 Lua 可转换子集生成 Mao 的统一边界。

`PLAN.md` 当前规定的是 Mao 生成 Go、Mao 调用 Go 包和 Go 调用生成后的 Mao API，并没有规定任意 Go 源码反向生成 Mao。本文的“双向”含义更严格：除正向后端外，还定义 Lua 可转换子集及 Lua → Mao 的拒绝规则；不能把现有 Go 后端的互操作能力直接称为反向源码转换。

实施分为两个先后目标。

### 初级目标

1. Mao 使用现有 `import` 语法引用 Lua 模块。
2. 编译器读取 Lua 模块源码和已有的 LuaCATS 定义文件，绑定能够确认类型的公开接口。
3. Mao 源码可以生成合法 Lua 源码。
4. 生成结果在指定 Lua 版本下通过语法检查，并通过运行时行为测试。

初级目标的处理方向为：

```text
Mao 源码
→ 解析 Lua 模块及 LuaCATS 定义
→ Mao 名称解析与类型检查
→ Mao 特性降级
→ Lua 抽象语法树
→ 规范 Lua 源码
→ 指定版本 Lua 解释器
```

### 最终目标

在正向输出形式稳定后，增加 Lua 源码到 Mao 源码的转换：

```text
Mao 源码 ⇄ Lua 可转换子集
```

最终目标不承诺转换任意 Lua 程序。Lua 是动态类型语言，变量可以在运行期间保存不同类型的值，元表可以改变读取、写入、运算、调用和长度行为。Mao 是静态类型语言，且保留 Go 基线的值语义、可比较性和控制流规则。转换器只能转换能够证明这些语义一致的 Lua 程序。

双向转换以运行行为和公开接口等价为标准，不要求字符级还原。局部变量名称、注释位置、格式、辅助函数和展开后的控制流可以发生规范化。

本文讨论源码转换及 Mao 调用 Lua 模块，不讨论：

- 在现有 Go 后端中嵌入 Lua 虚拟机。
- 通过 Lua C API 或 Go 与 Lua 的外部函数接口传递对象。
- 把 Go 对象自动暴露为 Lua userdata。
- LuaJIT 的外部函数接口、JIT 编译行为或 Lua 5.1 扩展。

这些能力需要独立的跨语言运行时和生命周期方案。

## 2. 版本与设计原则

### 2.1 固定目标版本

第一版目标固定为 Lua 5.1 语言与标准库语义。生成代码只使用 PUC-Lua 5.1 与不启用扩展的 LuaJIT 2.x 共同支持的 Lua 5.1 能力，不生成后续版本语法，也不依赖 LuaJIT 外部函数接口或 `bit` 库。

编译器不得把“Lua 5.x”当作单一且无差异的目标。

选择固定版本的原因是：

- 数值表示、整数运算、标准库和语法能力存在版本差异。
- 大量现有宿主与 LuaJIT 运行环境以 Lua 5.1 语法和模块约定为兼容边界。
- 宿主程序可能裁剪标准库或使用不同的数值配置。

项目配置固定记录：

```text
lua.version = "5.1"
lua.profile = "portable"
lua.number = "binary64"
mao.int_bits = 64
```

构建时检查实际解释器的 `_VERSION`，并通过 `mao_rt` 数值探针确认 `number` 采用符合项目要求的双精度模型。Lua 5.1 没有 `math.maxinteger`、`math.type` 或整数子类型，不能使用后续版本的检查方式。配置与解释器不一致时停止构建。

PUC-Lua 5.1 和 LuaJIT 2.x 必须分别运行兼容测试。LuaJIT 扩展语法、外部函数接口、`cdata` 和实现专用优化不属于第一版可转换子集。

### 2.2 保持现有 Mao 语法

Lua 后端不为 Mao 增加关键字。Mao 继续使用：

- 前置类型。
- `:=` 局部变量推断。
- `T?` 与 `null`。
- `table<K,V>`。
- `Generic<T>` 泛型实例化。
- Go 基线的函数、结构体、接口、方法和控制流语法。

Lua 无法原生表达的 Mao 语义由 `mao_rt` 运行时模块保存。运行时辅助结构不反向进入 Mao 语法。

### 2.3 不伪造完全对等关系

下列概念不存在普遍的一一对应：

| Mao 概念 | Lua 概念 | 主要差异 |
|---|---|---|
| 静态变量类型 | 值携带动态类型 | Lua 变量可在运行期间改变值类型 |
| `null` 可作为已有项目的值 | `nil` | Lua 表字段赋值为 `nil` 会删除该字段 |
| 有序 `table<K,V>` | Lua `table` | Lua 通用遍历顺序不稳定 |
| 从 0 开始的裸元素键 | Lua 序列惯例 | Lua 序列通常从 1 开始 |
| 结构体值复制 | Lua 表引用 | Lua 表赋值只复制引用 |
| 数组和结构体按值比较 | Lua 表按身份比较 | 普通 Lua 表不执行字段比较 |
| 固定宽度整数 | `number` | Lua 5.1 没有整数子类型，双精度数值不能精确保存全部 64 位整数 |
| `float32` | `number` | 常规 Lua `number` 不能保留每一步 32 位舍入 |
| 布尔条件 | Lua 真值规则 | Lua 只把 `false` 和 `nil` 视为假 |
| 方法集合与接口实现 | 元表与函数字段 | Lua 没有静态接口实现关系 |
| `defer` | 无直接语法 | Lua 5.1 没有待关闭变量，必须使用规范运行时调用栈 |
| goroutine、channel、`select` | coroutine | Lua coroutine 是协作式执行，不是 Go 并发模型 |
| 多返回值 | 多结果表达式 | Lua 的结果数量由表达式位置决定 |

转换器只在类型、求值顺序、别名关系和错误行为均可确定时转换。

### 2.4 先固定正向输出，再实现反向识别

Mao 生成 Lua 时必须使用统一形式：

- Mao `null` 只生成 `mao_rt.null`。
- Mao `table<K,V>` 只生成 `mao_rt.Table`。
- Mao 固定宽度算术只调用相应的 `mao_rt` 数值辅助函数。
- Mao 结构体只生成带有 `__mao_type` 标记和规范方法表的构造形式。
- Mao 接口值只生成 `mao_rt.Interface`。
- Mao `defer` 只生成规范的函数边界与延迟调用栈。
- Mao panic 与 recover 只生成规范的错误封装。
- 编译器生成的模块统一返回明确的导出表。

Lua 到 Mao 转换器识别上述规范形式和本文列出的普通 Lua 子集。相似但未经过绑定的用户代码不得根据名称自动解释为 Mao 语义。

## 3. 文件、包、模块和名称

### 3.1 包与模块

Mao 包生成一个 Lua 模块。多文件 Mao 包先完成包级名称解析，再生成一个模块文件或一个入口文件加若干私有分片；分片方式不能改变初始化顺序。

规范模块结构：

```lua
local mao_rt = require("mao_rt")
local M = {}

local function private_helper()
    -- ...
end

function M.PublicFunction()
    -- ...
end

return M
```

规则如下：

- Mao 导出名称成为返回模块表的字段。
- Mao 包内名称生成 Lua `local`。
- 不向 `_G` 写入包级声明。
- 生成模块通过显式返回表供 `require` 使用，不调用 Lua 5.1 的 `module(...)`，也不改变函数环境。
- Mao 包级初始化按 Mao 文件顺序规则生成确定顺序。
- 第一版拒绝存在包初始化环的 Lua 目标；不得依赖 `package.loaded` 中的半初始化模块状态。

### 3.2 导入 Lua 模块

Mao 使用带别名的现有导入语法：

```mao
import json "lua:dkjson"
import path "lua:pl.path"
```

生成：

```lua
local json = require("dkjson")
local path = require("pl.path")
```

规则如下：

- `lua:` 位于导入路径字符串中，不是新关键字。
- `lua:module.name` 按 Lua `require` 的模块名处理。
- 无 `lua:` 前缀的导入继续使用 Mao 原有 Go 包规则。
- 模块查找路径由项目配置确定；Lua 5.1 使用 `package.loaders`、`package.path` 和 `package.cpath`，不得误用后续版本的 `package.searchers` 名称。
- 不读取构建进程之外的任意用户环境配置。
- 构建清单记录实际解析到的 Lua 文件、原生模块和版本。
- 同一模块名解析到不同文件时产生错误。
- 调用现有 Lua 5.1 模块时允许其内部使用 `module(...)`；Lua → Mao 源码转换只识别位于模块顶层、名称为常量且不附带动态选项的规范 `module("name")` 声明。`module("name", package.seeall)` 会通过元表暴露环境中的其他全局名称，不属于第一版反向转换子集。

### 3.3 Lua 接口类型来源

Lua 模块没有官方静态接口元数据。Mao 按以下优先级取得接口：

1. 项目锁定的 LuaCATS `@meta` 定义文件。
2. 模块源码中的 LuaCATS `@param`、`@return`、`@class`、`@field`、`@generic` 和 `@overload`。
3. 能够从字面量和函数体完整证明的私有局部类型。

不得使用：

- 运行一次模块后观察到的字段集合。
- 根据函数名、字段名或示例调用推测的参数类型。
- 把未注明类型的所有参数自动解释为 Mao `any`。
- 只根据 LuaLS 没有报告错误就认定接口与 Mao 兼容。

公开接口缺少必要类型时，`mao bind-lua` 生成待补充的 LuaCATS 定义文件草稿并报告缺少的位置。生成草稿不是接口确认；用户补齐并纳入项目后才能调用。

LuaCATS 类型只作为绑定输入。编译器必须再次验证类型是否属于 Mao 可表达范围。例如 `table<any, any>`、联合类型、可变参数和函数重载不能因为注解语法有效就直接视为可转换。

### 3.4 名称与调用

已经绑定的 Lua 模块字段使用 Mao 的成员访问语法：

```mao
string output = json.encode(value)
```

生成：

```lua
local output = mao_rt.expect_string(json.encode(value), "dkjson.encode return")
```

跨动态边界的参数和返回值必须按照绑定声明检查。检查位置规则如下：

- Mao 传给 Lua 的值在调用前转换并检查。
- Lua 返回 Mao 的值在调用后立即检查。
- 错误信息包含模块、函数、参数序号或返回值序号。
- 受信任模式只能由项目配置对锁定版本的单个模块启用，不能成为全局默认。

冒号方法调用和点调用不能仅按函数是否位于表字段中判断。绑定声明必须明确接收者是否作为第一个参数传递。

## 4. 编译器架构与命令

### 4.1 独立后端

Lua 转换必须建立在 Mao 抽象语法树、名称解析和类型检查结果上，不得把生成的 Go 源码再翻译为 Lua。

```text
                    → Go 降级 → Go AST → Go 源码
Mao AST + 类型信息
                    → Lua 降级 → Lua AST → Lua 源码
```

Go 与 Lua 后端共享：

- 词法分析和解析。
- Mao 名称解析。
- Mao 类型表示。
- 常量求值。
- `table` 字面量类型推断。
- 可空值收窄。
- 与目标无关的诊断。

后端分别负责：

- 目标语言名称绑定。
- 数值和集合表示。
- 目标运行时辅助类型。
- 目标特有的不支持诊断。
- 源码位置映射。

### 4.2 命令

增加：

```text
mao emit-lua [files]
mao check-lua [packages]
mao run-lua <file-or-package>
mao from-lua [files]
mao bind-lua <module>
```

- `emit-lua` 只输出规范 Lua 源码。
- `check-lua` 生成到临时目录，调用固定版本解释器执行语法检查和加载检查。
- `run-lua` 使用同一临时构建结果运行入口。
- `from-lua` 只转换本文定义的 Lua 子集。
- `bind-lua` 读取真实模块与已有注解，生成缺失接口的待确认草稿。

所有命令使用同一项目配置和模块锁定文件。

### 4.3 不使用文本替换

两个方向都必须经过各自的词法分析、语法树、作用域和类型分析。以下处理不能使用正则表达式或全文替换：

- `local` 与包级名称判断。
- 点调用与冒号调用。
- 多结果表达式调整。
- Lua 表构造器分类。
- 元表模式识别。
- Mao `null` 和 Lua `nil` 转换。
- `continue`、`defer` 和三段式循环展开。

## 5. 基础类型

### 5.1 直接表示与运行时表示

Lua 5.1 只有 `number` 数值类型，标准配置通常使用双精度浮点。它没有整数子类型，不能直接承担 Mao 的全部整数语义。

| Mao | Lua 表示 |
|---|---|
| `bool` | Lua `boolean` |
| `string` | Lua `string` |
| `float` / `float64` | Lua `number` |
| `float32` | Lua `number` 加规范 32 位舍入辅助函数 |
| `int8` / `int16` / `int32` | Lua `number` 加规范整数与溢出辅助函数 |
| `uint8` / `uint16` / `uint32` | Lua `number` 加规范无符号与溢出辅助函数 |
| `rune` | 经过 `int32` 规则检查的 Lua `number` |
| `int64` | `mao_rt.Int64` |
| `int` | 64 位 Mao 目标中的 `mao_rt.Int64` |
| `uint64` | `mao_rt.UInt64` |
| `uint` / `uintptr` | 按目标配置使用 `mao_rt.UInt64` |
| `byte` | `uint8` 的别名 |

不能把全部 Mao 整数直接当作 Lua `number`：

- Mao 固定宽度整数运算具有相应宽度的溢出结果。
- 双精度 `number` 只能连续精确表示绝对值不超过 `2^53` 的整数，不能保存全部 `int64` 和 `uint64`。
- Lua 5.1 的 `/` 始终执行数值除法，没有整数除法运算符。
- Lua 5.1 没有原生位运算符。
- 负数取模结果规则不同。

因此，转换器按静态类型生成规范辅助调用。`mao_rt.Int64` 和 `mao_rt.UInt64` 使用两个 32 位部分或等价的精确表示实现，不能在内部通过 Lua `number` 暂存超出安全整数范围的中间结果。能够证明数值与中间结果均处于精确范围且运算规则一致时，才可直接生成 Lua 运算符。

`Int64` 和 `UInt64` 必须表现为不可变值：复制不建立可观察的可变共享状态，相等和排序按数值执行，作为 `mao_rt.Table` 键时按数值散列。普通 Lua 表按对象身份处理表键，因此这些包装值不能直接作为普通 Lua 表的等价整数键。

跨普通 Lua 模块边界时：

- 绑定声明接受 `mao_rt.Int64` 或 `mao_rt.UInt64`，可以直接传递精确包装值。
- 绑定声明接受普通 Lua `number`，只有编译器和运行时都能证明当前值处于双精度安全整数范围时才允许转换。
- 无法证明范围时产生边界错误，不得静默舍入。

### 5.2 数值字面量与转换

- 整数字面量先按 Mao 常量规则求值，再检查目标类型。
- `int64` 和 `uint64` 字面量分别生成 `mao_rt.i64("...")` 与 `mao_rt.u64("...")`，不能先转为 Lua `number`。
- `float32` 的赋值和每个产生 `float32` 结果的运算都必须执行 32 位舍入。
- 浮点到整数、符号变化和窄化使用 Mao 的显式转换规则。
- Lua 5.1 的 `number` 本身不携带整数类型信息。Lua 到 Mao 时，只有 LuaCATS 注解或规范运行时类型明确声明整数类型，才能转换成 Mao 整数。
- LuaCATS 的普通 `integer` 只说明数值没有小数部分，不说明位宽。Lua 模块绑定必须补充对应 Mao 整数类型或可验证范围；只有 `integer` 而没有位宽约束时，`bind-lua` 要求用户确认，不能默认解释为 `int64`。
- 普通 Lua 模块返回整数时，边界检查先验证其处于双精度安全整数范围，再验证所声明 Mao 类型的范围。已经在 Lua 中丢失精度的数值不能恢复为 `int64` 或 `uint64`。
- 未注明的普通 Lua `number` 转为 Mao `float`；不能根据当前值没有小数部分而改成 `int`。

### 5.3 字符串

Mao `string` 与 Lua 字符串都可以保存任意字节，包括零字节。转换时保持字节内容，不执行隐式 UTF-8 校验。

Mao `rune` 不直接映射为单字符 Lua 字符串。Lua 5.1 字符串索引和 `string.sub` 按字节工作，标准库不提供后续版本的 `utf8` 模块；转换器不得根据长度为一的字符串推测其为 `rune`。项目使用第三方 UTF-8 模块时，按普通 Lua 模块绑定处理。

### 5.4 布尔条件

Mao 条件表达式必须是 `bool`。生成 Lua 时继续保持这一限制：

```lua
if mao_rt.expect_bool(condition, "if condition") then
    -- ...
end
```

对编译器已经证明为 Mao `bool` 的内部表达式可以省略重复检查。来自 Lua 动态边界的值必须检查，不能使用 Lua 真值规则代替 Mao 类型规则。

## 6. `null`、`nil` 与可空值

### 6.1 规范表示

Mao `null` 生成唯一哨兵 `mao_rt.null`，不直接生成 Lua `nil`。

原因如下：

- Lua 表不能保存值为 `nil` 的已有字段。
- Lua 多结果中的缺项与显式 `nil` 很难在普通表构造中区分。
- Mao 必须区分 `null`、类型零值和缺失键。

| Mao | 规范 Lua |
|---|---|
| `null` | `mao_rt.null` |
| `T?` | `T` 的 Lua 表示或 `mao_rt.null` |
| `value == null` | `value == mao_rt.null` |
| Lua 边界返回 `nil` | 转换为 `mao_rt.null` |
| Mao `null` 传给 Lua | 转换为 Lua `nil` |

只在调用普通 Lua 模块、返回普通 Lua API 或写入普通 Lua 表的边界执行 `nil` 与 `mao_rt.null` 转换。Mao 生成代码内部不得混用两者。

### 6.2 嵌套与缺失

Mao 规定 `(T?)?` 与 `T?` 相同，因此 Lua 的以下状态不能全部反向保留：

- 字段不存在。
- 字段存在且值为自定义空值哨兵。
- 外层可空包装为空。
- 内层可空包装为空。

Lua 代码依赖三个或更多空值层级时不属于可转换子集。

### 6.3 Lua 表边界

向普通 Lua 表写入 Mao `null` 会删除字段，这是 Lua 本身的行为。转换器必须要求调用绑定明确选择以下语义之一：

- `nil_means_absent`：Mao `null` 转成 `nil`，允许字段不存在。
- `sentinel`：模块声明接受 `mao_rt.null`。
- `forbid_null`：参数类型不可空，传入 `null` 产生错误。

未声明时不得自动选择。

## 7. Mao `table` 与 Lua 表

### 7.1 Mao `table` 使用规范运行时

Mao `table<K,V>` 不能直接生成普通 Lua 表。`mao_rt.Table` 必须保持：

- 插入顺序。
- 从 0 开始的裸元素键。
- 键存在且值为 `null`。
- 赋值和参数传递共享状态。
- 重复键替换值但保留第一次插入位置。
- `Delete` 按键删除。
- `DeleteAt` 按插入位置删除。
- 缺失读取返回归一化的 `V?`。
- `NaN` 等不满足自反相等的键被拒绝。

规范形式：

| Mao | Lua |
|---|---|
| `[]` | `mao_rt.Table.new()` |
| `[value1, value2]` | `mao_rt.Table.from_entries({{0, value1}, {1, value2}})` |
| `[key: value]` | `mao_rt.Table.from_entries({{key, value}})` |
| `table[key]` | `table:index(key)` |
| `table.get(key, fallback)` | `table:get_or(key, function() return fallback end)` |
| `table.has(key)` | `table:has(key)` |
| `table.at(index)` | `table:at(index)` |
| `table[key] = value` | `table:set(key, value)` |
| `table.Delete(key)` | `table:delete(key)` |
| `table.DeleteAt(index)` | `table:delete_at(index)` |
| `table.clear()` | `table:clear()` |
| `len(table)` | `table:len()` |
| `range table` | `table:entries()` 返回的规范迭代器 |

Lua 到 Mao 只把已确认的 `mao_rt.Table` 及其规范调用恢复成 Mao `table`。

### 7.2 普通 Lua 表分类

普通 Lua 表不能仅根据当前键形状自动认定为数组、对象或 Mao `table`。分类必须来自：

- LuaCATS 明确类型。
- 转换器在同一局部作用域内完整分析的表构造器和全部写入。
- 编译器生成的规范标记。

允许的受限转换：

| Lua 源类型 | Mao 目标 |
|---|---|
| 连续 `1..n` 且声明为 `T[]` | Mao 原生互操作切片 `T[]`，索引语义在边界调整 |
| 固定字段且声明为 class/record | Mao 结构体 |
| 键值类型确定、顺序不参与行为 | Mao 原生互操作映射 `K:V[]` |
| `mao_rt.Table<K,V>` | Mao `table<K,V>` |

以下情况拒绝：

- 同一表同时被当作序列和对象使用。
- `#table` 作用于存在空洞的表。
- 依赖 `pairs` 或 `next` 的遍历顺序。
- 运行期间改变键和值的类型集合。
- 通过元表改变读取、写入、比较或算术行为。
- 将函数字段作为运行时可替换的方法。

### 7.3 1 基与 0 基索引

普通 Lua 序列的首个惯用索引为 1，Mao 裸 `table` 元素的首键为 0。转换器不得全局把整数键加一或减一。

只有声明为 Lua 序列并转换到 Mao `T[]` 时执行边界调整。普通映射的整数键保持原值。`mao_rt.Table` 始终保存 Mao 原始键，不调整。

### 7.4 顺序

普通 Lua 表的 `pairs` 和 `next` 遍历顺序不构成 Mao 插入顺序。将普通 Lua 映射转换为 Mao `table` 时：

- 若后续行为读取插入位置、使用 `at`、使用 `DeleteAt` 或按顺序遍历，转换失败。
- 若程序只执行按键查询，允许生成 Mao 原生映射，不生成 Mao `table`。
- 不通过排序键来伪造插入顺序，因为排序会引入原程序不存在的语义。

## 8. 变量、常量、赋值和作用域

### 8.1 局部变量

Mao：

```mao
name := "Mao"
int count = 0
```

Lua：

```lua
local name = "Mao"
local count = 0
```

所有 Mao 局部变量生成 Lua `local`。包级私有变量也保持模块局部，不写入 `_G`。

Lua 到 Mao 时：

- 未使用 `local` 的赋值视为全局写入，第一版拒绝。
- 同一局部变量的全部赋值必须具有可合并的 Mao 类型。
- 仅因控制流当前分支没有赋值而出现的 Lua `nil`，必须转成显式 Mao `T?`。
- Lua 多重赋值先完整计算右侧，再写入左侧；转换成 Mao 时必须保持该求值顺序，必要时生成临时变量。

### 8.2 常量

Mao `const` 只在表达式同时满足 Mao 编译期常量规则时生成 Lua 字面量或模块局部常量。

Lua 5.1 没有常量声明或 `<const>` 局部变量属性。Mao 生成 Lua 时：

- 能够直接替换且不会重复求值的 Mao 常量可以生成字面量。
- 需要保留名称的常量生成带规范类型注解的 `local`，并由转换器保证生成代码不再赋值。
- Mao → Lua → Mao 通过编译器规范标记恢复 `const`。

普通 Lua 5.1 的 `local` 即使从未再次赋值，也默认恢复为 Mao 局部变量，不能仅凭使用形状推测作者声明了编译期常量。

### 8.3 值复制与引用

Lua 表、函数、线程和 userdata 按引用传递。Mao 结构体和数组具有值复制语义。

Mao 到 Lua 时：

- Mao 结构体赋值生成规范字段复制。
- Mao 数组赋值生成定长内容复制。
- Mao `table` 继续共享 `mao_rt.Table` 状态。
- Mao 指针通过规范 `mao_rt.Ref` 表示。

Lua 到 Mao 时，普通表只有在没有别名或全部别名都符合目标 Mao 引用语义时才能转换。转换器不得插入未经证明的浅拷贝或深拷贝。

## 9. 函数、闭包和多返回值

### 9.1 普通函数

Mao 函数参数和返回类型由静态类型确定。生成 Lua 时在函数入口和动态边界执行必要检查：

```mao
int add(int left, int right) {
    return left + right
}
```

生成的核心形式：

```lua
local function add(left, right)
    left = mao_rt.expect_i64(left, "add.left")
    right = mao_rt.expect_i64(right, "add.right")
    return mao_rt.add_i64(left, right)
end
```

同一编译单元内已经静态验证的调用可以由优化阶段删除重复边界检查。优化前后的错误行为必须通过测试确认。

### 9.2 多返回值

Lua 函数调用可能返回任意数量结果，并且调用在表达式列表中的位置会改变保留结果数量。Mao 多返回值规则继承 Go，不具备完全相同的上下文调整。

正向转换规则：

- Mao 多返回函数可以生成多个 Lua 返回值。
- 调用结果只允许出现在 Mao 本来允许多值展开的位置。
- 多返回调用作为另一个函数的非末尾实参时，生成临时变量以固定结果数量。
- Mao 单返回函数的 Lua 调用结果必须显式固定为一个值，避免被宿主 Lua 调用者误解为可变结果。
- 零个、一个和多个返回值的公开签名写入 LuaCATS 注解。

反向转换只接受：

- 返回数量在所有路径上固定。
- `return` 表达式数量与注解一致。
- 多结果调用只出现在赋值、返回或调用实参列表中能够静态确定调整规则的位置。
- 不使用 `select("#", ...)` 观察动态结果数量。

### 9.3 可变参数

Mao 的 Go 基线可变参数可以生成 Lua `...`，但必须在入口处打包并逐项检查。Lua 到 Mao 只接受具有 LuaCATS `@vararg` 类型且不依赖缺项与显式 `nil` 差异的函数。

### 9.4 闭包

两侧都支持词法闭包，但捕获语义需要验证：

- Mao 循环变量捕获按 Mao 当前版本语义生成独立或共享绑定。
- Lua 局部变量被闭包捕获后共享同一 upvalue。
- Lua 的 `debug` 库可以读取和替换 upvalue；使用该能力的函数不转换。
- 通过 `string.dump`、`load`、`loadstring`、`getfenv` 或 `setfenv` 观察或修改函数实现与环境的代码不转换。

## 10. 运算与比较

### 10.1 算术

已知类型的算术按 Mao 规则生成：

- 固定宽度溢出由 `mao_rt` 保持。
- 有符号整数除法使用向零截断辅助函数。
- 有符号余数使用 Mao 对应规则。
- `float32` 每一步执行 32 位舍入。
- 无符号比较和移位使用相应辅助函数。
- 除数为零、无效移位和溢出边界保持 Mao/Go 基线行为。

Lua 字符串到数值的动态转换不属于 Mao 自动转换。依赖 Lua 运算符自动接收数值字符串的代码不能反向转换。

### 10.2 逻辑运算

Lua `and` 和 `or` 返回操作数本身，并执行真值判断；Mao 的 `&&` 和 `||` 只接受并返回 `bool`。

只有两个操作数均证明为布尔值时才双向转换。常见 Lua 写法：

```lua
local value = condition and first or second
```

不能机械转换为 Mao 布尔表达式，应展开成赋值分支，并验证 `first` 为 `false` 时原 Lua 行为是否仍符合预期。

### 10.3 相等与顺序

- Mao 标量比较生成 Lua 对应运算或规范辅助函数。
- Mao 数组和可比较结构体按值比较，生成字段级规范比较。
- Mao 指针、函数、切片、映射和 `table` 的可比较规则继续由 Mao 类型检查器决定。
- 普通 Lua 表的 `==` 是身份比较，只有目标 Mao 类型也使用身份语义时才能转换。
- 定义 `__eq`、`__lt` 或 `__le` 元方法的值不属于普通比较转换子集。

## 11. 结构体、方法、接口和泛型

### 11.1 结构体

Mao 结构体生成规范类型表、构造函数和实例标记：

```lua
local Person = {}
Person.__index = Person
Person.__mao_type = "example.Person"

function Person.new(name, age)
    return setmetatable({
        __mao_type = Person.__mao_type,
        Name = name,
        Age = age,
    }, Person)
end
```

字段访问可以直接生成表字段访问，但以下规则必须保持：

- 值赋值复制字段，不共享整个 Lua 表。
- 未导出字段不通过模块表暴露。
- 零值构造全部字段的 Mao 零值。
- 字段类型不能在运行时改变。
- 禁止用户替换实例元表或方法表。

Lua 到 Mao 只识别编译器规范结构，或满足完整 LuaCATS class 定义且没有动态增删字段、元表替换和原型链变化的简单记录。

### 11.2 方法

Mao 方法的接收者类型与值/指针语义必须保留。Lua 冒号语法只负责传递第一个参数，不表达值接收者复制。

- Mao 指针接收者生成对同一规范对象的修改。
- Mao 值接收者在调用前生成结构体副本。
- Lua 到 Mao 时，冒号函数只有在接收者类型由 LuaCATS 或规范类型标记确认后才能转成方法。
- 把函数保存到实例字段并在运行期间替换的方法模式不转换。

### 11.3 接口

Mao 接口是静态方法集合。普通 Lua 的“鸭子类型”不能证明实现关系。

正向使用 `mao_rt.Interface` 保存：

- 动态值。
- 已确认的 Mao 类型标识。
- 接口所需方法表。
- Mao 类型断言和类型选择所需信息。

Lua 模块返回值只有在绑定声明列出完整方法集合，并且运行时检查通过后，才能进入 Mao 接口值。

Lua 到 Mao 不根据对象当前恰好具有同名字段就声明其实现接口。使用动态 `__index` 提供方法的对象第一版拒绝。

### 11.4 泛型

Lua 没有静态泛型实例化。Mao 泛型到 Lua 采用擦除后的函数或类型实现，同时保留运行时类型描述：

- 每个实例化在 Mao 编译期完成约束检查。
- 需要类型相关操作时向规范函数传入类型描述。
- 不依赖类型参数的实现可以共享 Lua 函数体。
- 公开 LuaCATS 注解记录泛型参数，但不替代 Mao 类型检查。

Lua 到 Mao 只接受 LuaCATS `@generic` 能够映射为 Mao 类型参数，并且函数体不执行 Mao 无法表达的动态类型分派的项目。

## 12. 控制流

### 12.1 条件与 `switch`

Mao `if` 生成 Lua `if`，条件保持布尔检查。

Mao `switch` 按以下顺序生成：

1. 能够证明为互斥常量比较时生成 `if` / `elseif`。
2. 类型选择生成规范类型标识分支。
3. 包含 `fallthrough` 时先在 Mao 中间表示中展开，再生成 Lua。

Lua 没有原生 `switch`。Lua 到 Mao 只把结构清晰、条件互斥且没有中间副作用的 `if` / `elseif` 链恢复为 `switch`；恢复不是语义正确性的必要条件。

### 12.2 循环

| Mao | Lua |
|---|---|
| `for {}` | `while true do` |
| `for condition {}` | `while condition do` |
| 三段式 `for` | 初始化加 `while` 加规范 post 块 |
| `range table` | `mao_rt.Table:entries()` |
| `break` | `break` |
| `continue` | 内层单次 `repeat` 块与控制标记 |

Lua 5.1 没有 `continue` 和 `goto`。含 `continue` 的 Mao 循环生成外层实际循环、内层单次 `repeat ... until true` 块和独立的退出标记：

- Mao `continue` 生成内层 `break`，随后执行三段式循环的 post 语句。
- Mao `break` 先设置退出标记，再离开内层块；外层在 post 之前检查标记并退出。
- 嵌套循环分别使用独立标记。
- 该降级不能把函数体包装成额外闭包，因为闭包会改变 `return`、可变参数和错误堆栈。

Lua 到 Mao 支持：

- `while`。
- `repeat` 能够改写为条件位于循环尾且作用域不变的循环。
- 数值 `for` 在起点、终点、步长和整数/浮点行为可保持时转换。
- 普通迭代器 `for` 只有在迭代器签名与结束条件可静态确认时转换。

依赖 Lua 数值 `for` 对控制变量、溢出或浮点终点的特定处理时，需要专门验证，不能统一改成 Mao 三段式循环。

### 12.3 `goto`

Lua 5.1 没有标签和 `goto`。第一版 Lua 后端拒绝 Mao `goto`，Lua → Mao 也不存在相应源语法。不得自动生成状态机，因为状态机可能改变局部变量作用域、`defer` 边界、错误堆栈和执行成本。

## 13. `defer`、panic 与错误

### 13.1 `defer`

Lua 5.1 没有待关闭变量或 `__close` 元方法。Mao `defer` 必须完全由规范运行时保存以下语义：

- 遇到 `defer` 时立即求值函数和参数。
- 函数返回时按后进先出顺序调用。
- panic 展开时也执行。
- `recover` 只能在规定上下文观察 panic。

Lua 后端统一为含 `defer` 的 Mao 函数生成规范函数边界：

```lua
local function body(...)
    local deferred = mao_rt.DeferStack.new()
    -- deferred:push(function() ... end)
    return mao_rt.run_with_defers(deferred, function()
        -- 原函数体
    end)
end
```

实际生成器可以使用更少分配的等价形式，但反向转换必须能够识别。任意手写清理栈、`pcall` 清理包装或 userdata `__gc` 元方法不自动转成 Mao `defer`。

### 13.2 panic 与 recover

Mao panic 使用带有不可伪造标记的 `mao_rt.Panic` 错误对象。普通 Lua `error` 与 Mao panic 的边界如下：

- Mao 内部 panic 通过规范对象传播。
- Lua 模块抛出的普通错误由绑定声明决定是转为 Mao panic，还是作为显式错误返回；未声明时停止调用并报告边界错误。
- Mao `recover` 只捕获规范 Mao panic。
- Lua `pcall` 或 `xpcall` 捕获任意错误的代码不能直接恢复为 Mao `recover`。
- 错误对象的值、延迟调用顺序和重新抛出行为必须保持。

Lua 到 Mao 可以转换不捕获错误的 `error(value)` 为 `panic(value)`。包含自定义错误处理器、错误字符串重写或依赖堆栈格式的代码不自动转换。

## 14. 并发与 coroutine

Mao 的 `go`、channel 和 `select` 沿用 Go 并发语义。Lua coroutine 是单线程协作式执行单元，不提供等价的并发、阻塞、关闭和选择模型。

第一版 Lua 后端拒绝：

- `go` 语句。
- channel 类型、发送、接收和关闭。
- `select`。
- 依赖 Go 内存模型或原子操作的代码。

不得把 goroutine 自动改成 coroutine。

Lua 到 Mao 的 coroutine 也不转换成 `go`。使用 `coroutine.create`、`resume`、`yield`、`wrap` 或 `close` 的代码属于独立的协程能力，直到 Mao 具有明确协程语义之前均不转换。

后续若提供调度运行时，应作为新的目标能力单独规划，并明确：

- 单线程还是多线程。
- 抢占还是协作。
- channel 阻塞与关闭规则。
- `select` 的公平性。
- 外部 Lua 调用能否 yield。

## 15. Lua 可转换子集

第一版 Lua 到 Mao 支持：

- Lua 5.1 词法作用域内的 `local` 声明。
- 类型稳定的基础值。
- 具有 LuaCATS 参数和返回类型的普通函数。
- 固定返回数量。
- 能够静态确定的多重赋值。
- 无动态字段变化的简单记录。
- 声明为序列的连续表。
- 不依赖遍历顺序的固定键值映射。
- 编译器规范 `mao_rt.Table`、`null`、结构体和接口形式。
- `if`、`while`、受限 `repeat`、受限数值 `for` 和已知迭代器。
- 不改变作用域语义的函数闭包。
- 无元方法参与的已知类型运算。
- 简单 `error`，以及规范 Mao panic 边界。
- 受限的顶层 `module("name")` 声明；不接受 `package.seeall` 或其他环境修改选项。

第一版明确不转换：

- 未声明或动态变化的全局变量。
- `load`、`loadstring`、`loadfile`、`dofile`、`string.dump` 和运行时代码生成。
- `getfenv`、`setfenv`、带动态选项的 `module(...)`，以及依赖自定义函数环境的代码。
- `debug` 库。
- 任意元表和非规范元方法。
- 弱表和垃圾回收元方法。
- coroutine。
- userdata、light userdata 和 Lua C 函数的未知行为。
- 依赖表遍历顺序的代码。
- 有空洞表上的长度运算。
- 参数或返回数量不固定的未注解函数。
- 运行期间改变变量、字段、键或值类型的代码。
- 依赖尾调用消除来避免栈增长或观察调试栈的代码。
- LuaJIT 外部函数接口和扩展语法。

## 16. 往返转换规则

### 16.1 Mao → Lua → Mao

允许以下规范化：

- Mao 中文兼容名恢复为格式化器选定的规范名。
- 中文代码标点统一为 ASCII 标点。
- Lua 中的显式运行时辅助调用恢复为 Mao 运算和类型转换。
- 模块返回表恢复为 Mao 包级导出。
- 编译器生成的临时变量重新命名。
- `switch` 与等价 `if` / `elseif` 链之间转换。
- 展开的 `continue`、`defer` 和多值固定逻辑恢复为 Mao 结构。

不得发生：

- `null` 与 Lua `nil` 或缺失键合并。
- Mao `table` 变成普通 Lua 表。
- 0 基键变成 1 基键。
- 结构体值复制变成 Lua 表引用共享。
- 整数宽度、符号、溢出、除法或取模行为变化。
- `float32` 舍入时机变化。
- 多返回值数量因表达式位置改变。
- `defer`、panic 或循环 post 语句顺序变化。

### 16.2 Lua → Mao → Lua

只保证可转换子集的运行语义。以下 Lua 表面结构可以规范化：

- 局部函数声明与保存函数值的局部变量。
- `repeat` 与等价循环。
- 简单记录构造与规范 Mao 结构体构造。
- 已声明序列与 Mao 原生互操作切片。
- 固定多重赋值与临时变量形式。

必须保持：

- 模块公开字段和函数签名。
- 每个表达式的求值次数与顺序。
- 表或对象的别名关系。
- 固定返回值数量。
- 显式 `nil` 与缺失能够在目标语义中表达的区别。
- 错误分支和可观察状态变化。

若 Lua 程序依赖 Mao 无法表达的动态约束，转换在源位置失败，不生成“近似”代码。

### 16.3 类型注解保留

Mao 生成 Lua 时输出 LuaCATS 注解，供编辑器和反向转换使用。注解至少包含：

- 公开函数参数、返回值和可变参数。
- 公开结构体、字段和方法。
- 泛型参数及约束中能够用 LuaCATS 表达的部分。
- Mao `table`、可空值、接口和固定宽度数值的规范类型名。

LuaCATS 不是 Mao 类型系统的替代物。往返时以编译器生成的规范标记和重新分析结果为准；注解与实现冲突时产生错误。

## 17. 诊断要求

转换失败必须报告：

- 源文件和准确位置。
- 无法转换的源语言结构。
- Mao 与 Lua 的具体语义差异。
- 需要用户补充的接口类型或版本信息。
- 如果存在等价重构，列出其必须满足的条件。

示例：

```text
config.lua:18:12：不能把该普通 Lua table 转换为 Mao table。
此值通过 pairs 遍历，并且后续结果依赖遍历顺序；Lua 5.1 不保证 pairs 的顺序，
而 Mao table 固定保持插入顺序。请先在 Lua 源码中建立明确的顺序数据结构。
```

```text
codec.lua:7:1：函数 decode 缺少返回类型。
Lua 源码和现有 LuaCATS 定义不能确定该函数的返回值数量与类型。
请在锁定的定义文件中补充 @return 后重新执行 mao bind-lua。
```

不得自动执行：

- 把所有未注解值改成 `any`。
- 把 Lua `nil` 一律改成 Mao `null`，而忽略字段删除。
- 把所有 Lua 表改成 Mao `table`。
- 把 Lua 冒号调用一律改成 Mao 方法。
- 把 coroutine 改成 goroutine。
- 通过键排序构造虚假的 Lua 表顺序。
- 为通过类型检查而自动插入未经证明的复制。

## 18. 运行时与发布边界

### 18.1 `mao_rt` Lua 模块

运行时至少提供：

- `null` 唯一哨兵。
- `Table`。
- 固定宽度有符号与无符号算术。
- `Int64`。
- `UInt64`。
- `Float32` 舍入。
- Lua 5.1 双精度数值模型验证。
- 结构体复制和类型标识。
- `Interface`。
- 参数与返回值边界检查。
- `DeferStack`。
- Mao panic 封装。
- 源位置辅助信息。

运行时 API 是生成代码的兼容边界，必须具有版本号。生成文件记录所需运行时版本；版本不匹配时加载失败并给出明确诊断。

### 18.2 安全与可复现构建

分析 Lua 模块时默认只解析源码，不执行模块。必须执行生成程序时：

- 使用项目锁定的 `package.path` 与 `package.cpath`。
- 不自动继承 `LUA_INIT`。
- 记录解释器路径和版本。
- 原生模块必须在锁定清单中。
- `bind-lua` 不通过执行未知模块来发现接口。

### 18.3 源码位置

生成 Lua 保留 Mao 文件、行和列的映射表。运行时错误经过 `mao_rt` 格式化后优先指向 `.mao` 源位置。

普通 Lua 模块内部错误仍指向实际 `.lua` 文件。跨边界堆栈同时显示 Mao 调用位置和 Lua 模块位置，不把 Lua 错误伪装成 Mao 编译错误。

## 19. 实施顺序

### 阶段一：Lua AST、模块绑定与基础调用

- 建立 Lua 5.1 词法分析器、语法树和格式化器。
- 实现 `lua:` 导入路径和锁定模块解析。
- 读取 LuaCATS 定义文件及源码注解。
- 支持布尔、字符串、`int32`、`float` 和不可空普通函数。
- 实现参数与返回值边界检查。
- 实现 `emit-lua`、`check-lua` 和 `bind-lua`。
- 拒绝未注解公开接口、元表、coroutine 和动态代码加载。

完成标准：Mao 可以调用一个真实的纯 Lua 5.1 模块；绑定来自锁定源码和定义文件；生成 Lua 分别通过 PUC-Lua 5.1 与 LuaJIT 2.x 基础兼容模式的语法检查并产生预期结果；错误类型在 Mao 调用位置报告。

### 阶段二：Mao 基础语义的完整正向转换

- 实现全部基础数值及规范算术。
- 实现 `null` 和 `T?`。
- 实现 `mao_rt.Table`。
- 实现变量、函数、固定多返回值和闭包。
- 实现条件、循环、`switch` 和规范 `continue`。
- 实现结构体值复制、方法和包初始化。
- 建立运行时版本和源码位置映射。

完成标准：不含接口、泛型、`defer`、panic 和明确排除特性的 Mao 包能够整体生成并运行；数值边界、表顺序、空值、别名和多返回值通过行为测试。

### 阶段三：正向语义补全

- 实现接口与类型断言。
- 实现泛型擦除及类型描述。
- 实现 `defer`。
- 实现 panic 与 recover。
- 固定全部供反向转换识别的规范 Lua 形式。
- 完成 `run-lua`。
- 对 `go`、channel 和 `select` 提供稳定的不支持诊断。

完成标准：Lua 后端的支持边界稳定；相同 Mao 输入重复生成结构一致的 Lua 抽象语法树；公开接口、错误传播、延迟调用和动态边界具有行为测试。

### 阶段四：Lua → Mao

- 分析 Lua 局部作用域、控制流和类型稳定性。
- 读取并校验 LuaCATS。
- 转换基础值、函数、固定多返回值、记录、序列和无序映射。
- 识别规范 `mao_rt` 形式。
- 验证表别名、字段变化、元表和遍历顺序。
- 建立 Mao → Lua → Mao 和 Lua → Mao → Lua 往返测试。

完成标准：受支持 Lua 子集生成合法 Mao；生成 Mao 通过 Mao 解析和类型检查；往返后公开接口、求值顺序、返回数量、别名关系、错误分支和运行结果一致。

### 阶段五：Lua 5.1 生态兼容与发布

- 在 PUC-Lua 5.1.5 与 LuaJIT 2.x 基础兼容模式中运行同一套生成代码和行为测试。
- 覆盖显式返回模块表与传统顶层 `module("name")` 两类 Lua 5.1 模块。
- 覆盖通过 `package.loaders`、`package.path`、`package.cpath` 和 `package.preload` 解析的锁定模块。
- 纯 Lua 模块必须提供源码与 LuaCATS 定义。
- C 模块必须锁定目标平台构件、Lua 5.1 应用二进制接口及 LuaCATS 定义；编译器不执行二进制模块来发现接口。
- 验证生成结果不含 Lua 5.2 及后续版本语法和标准库名称。
- 验证生成结果不依赖 LuaJIT `bit`、外部函数接口、`cdata` 或 JIT 编译行为。

每个目标都需要独立的：

- 语法能力表。
- 数值配置。
- 标准库与模块加载规则。
- 不支持项。
- 运行时构建。
- 持续集成测试。

完成标准：同一 Mao 包及其锁定依赖具有一致的公开结果；实现差异必须在构建时诊断，不能由运行时静默选择不同语义。

## 20. 测试与验收

### 20.1 正向转换

- `lua:` 导入绑定到锁定的真实模块。
- 缺失或冲突的 LuaCATS 定义产生准确诊断。
- 不执行模块即可完成接口分析。
- Mao 标量、字符串和数值转换保持类型与边界。
- PUC-Lua 5.1 与 LuaJIT 2.x 基础兼容模式分别通过相同测试。
- 启动时拒绝不符合项目双精度要求的 Lua 5.1 数值配置。
- `int64` 和 `uint64` 的大值及中间结果不经过 Lua `number`。
- 有符号除法、余数、移位和固定宽度溢出符合 Mao 规则。
- `float32` 每一步舍入符合 32 位行为。
- `null`、Lua `nil`、缺失键和零值能够按声明区分。
- Mao `table` 保持 0 基裸键、插入顺序和已有空值。
- 结构体赋值复制字段，`table` 赋值共享状态。
- 动态边界参数和每个返回值均执行相应检查。
- 多返回值在不同表达式位置保持固定数量。
- 三段式循环中的 `continue` 执行 post。
- `defer` 参数求值时机和逆序执行保持。
- panic、recover 和普通 Lua error 不被错误合并。
- goroutine、channel 和 `select` 产生明确不支持诊断。

### 20.2 反向转换

- 类型稳定的 Lua 局部变量生成确定 Mao 类型。
- 未声明全局变量被拒绝。
- 未注解公开函数被拒绝并指出缺少的签名。
- 普通 Lua 序列、记录和映射只在分类可证明时转换。
- `pairs` 顺序不被解释为 Mao 插入顺序。
- 有空洞表上的 `#` 被拒绝。
- 动态元表、`getfenv`、`setfenv`、`debug`、运行时代码生成和 coroutine 被拒绝。
- Lua `and` / `or` 的操作数返回语义不被误译成 Mao 布尔运算。
- 多结果表达式调整保持求值次数和结果数量。
- 表身份比较不被改成 Mao 结构体值比较。
- Lua error 只在规则明确时转成 Mao panic。

### 20.3 往返一致性

每个支持特性建立四类测试：

1. 语法树测试：比较规范化后的源语言和目标语言抽象语法树。
2. 类型测试：比较公开签名、局部推断和动态边界检查。
3. 行为测试：在相同输入下比较结果、状态变化、错误和值别名。
4. 往返测试：执行 A → B → A，比较规范化后的公开接口和行为。

文本相同不作为验收标准。类型、求值顺序、返回数量、别名关系、表顺序、错误分支和运行结果一致才构成通过。

### 20.4 差分测试

正向行为测试同时运行：

- Mao 当前 Go 后端。
- Mao Lua 后端。

只对两个后端共同支持的语言子集进行差分比较。比较内容包括：

- 标准输出和返回值。
- panic 或运行时错误类别。
- `table` 项目、顺序和空值状态。
- 固定宽度整数边界。
- 函数调用副作用和 `defer` 顺序。

后端目标本身不同导致的公开接口差异单独测试，不通过忽略结果掩盖。

## 21. 第一版结论

Mao 可以建立 Lua 双向源码转换，但正确方案不是把 Mao `table` 替换为 Lua 表，也不是为动态 Lua 源码自动补全静态类型。

第一版应当采用以下唯一边界：

- 固定 Lua 5.1 语言与标准库语义；生成代码保持 PUC-Lua 5.1 与不启用扩展的 LuaJIT 2.x 基础兼容。
- 使用 `import alias "lua:module"` 调用 Lua 模块。
- 从真实模块源码与锁定的 LuaCATS 定义取得接口。
- 直接从 Mao 类型化抽象语法树生成 Lua 抽象语法树。
- 使用 `mao_rt.null`、`mao_rt.Table`、固定宽度数值辅助类型、结构体复制、接口和错误辅助结构保存 Mao 语义。
- 对所有 Lua 动态边界执行声明驱动的参数和返回值检查。
- 明确拒绝 goroutine、channel、`select`、coroutine、任意元表、动态代码加载和无法确认类型的接口。
- 先稳定 Mao → Lua 的规范输出，再实现规范输出及受限普通 Lua 的反向转换。

这一范围能够保持 Mao 现有静态类型、可空值、有序集合和值语义，同时允许 Mao 使用类型信息完整的 Lua 模块，并为双向转换提供可实现、可测试的语义定义。
