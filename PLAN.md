# Mao 语言第一版实现计划

## 1. 项目目标

Mao 是一门编译到 Go 的静态类型语言方言，使用狸花猫作为项目形象。第一版以“减少 Go 日常语法负担，同时保持 Go 生态兼容”为目标：

- 类型采用前置写法。
- 局部变量允许直接推断类型，不需要 `var`；只有 `const` 声明常量。
- 数组、列表、切片和字典统一为保持插入顺序的 `table` 集合类型，并使用统一的方括号字面量。
- `table` 可以通过 `any` 容纳不同类型的键或值；能够推断共同类型时仍保留精确类型。
- 代码区允许部分中文标点作为 Go 标点的等价写法，并由格式化器统一输出 ASCII 标点。
- 显式 `table` 项允许省略冒号后的值，省略值等同于 `null`；裸元素作为具有隐式整数键的列表值。
- `table.Delete(key)` 按键删除，`table.DeleteAt(index)` 按插入位置删除。
- 提供统一的 `float` 类型，默认对应 Go `float64`。
- Mao 文件能够导入 Go 包，并通过明确转换与 Go 数组、切片和 `map` 互操作。

第一版不修改 Go 编译器，也不引入自定义虚拟机。`mao` 编译器将 `.mao` 源码转换为可读的 `.go` 源码，再调用官方 Go 工具链完成构建、运行和测试。生成代码自动链接 Mao 内部运行时提供的 `Table` 和可空值实现，Mao 项目不需要手动导入该运行时。

## 2. 语言规则

### 2.1 关键字与预声明标识符

Mao 完整沿用 Go 的 25 个关键字，不增加新关键字：

```text
break       default     func        interface   select
case        defer       go          map         struct
chan        else        goto        package     switch
const       fallthrough if          range       type
continue    for         import      return       var
```

Mao 继承 Go 的预声明标识符，并作以下明确调整：

| 名称 | 分类 | 规则 |
|---|---|---|
| `null` | 预声明空值 | 替换 Go 的 `nil` |
| `float` | 预声明类型 | 固定对应 Go `float64` |
| `table` | 预声明类型和转换函数 | Mao 的统一集合类型，并负责从 Go 数组、切片或 `map` 转换 |
| `any` | 预声明类型 | 直接继承 Go 的 `any`，用于异构集合及其他 Go 兼容场景 |

`null` 不是关键字，作用域与遮蔽规则沿用 Go 的 `nil`。Mao 的预声明环境不再提供 `nil`；如果用户自行声明该名称，按普通标识符处理。`map` 保留关键字身份，但在 Mao 中用于把 `table` 显式转换为 Go 原生 `map`，不再用于定义 Mao 集合。Go 的预声明函数 `delete` 和 `clear` 只对互操作产生的 Go 原生类型保持有效。`get`、`has`、`at`、`keys`、`values`、`Delete`、`DeleteAt` 和 `clear` 是 `table` 的上下文操作，不是关键字，也不占用普通类型的方法名；实际声明的同名方法优先。

### 2.2 中文标点兼容

Mao 词法分析器在代码记号中接受以下中文标点：

| 中文标点 | 等价 ASCII 记号 | 用途示例 |
|---|---|---|
| `。` | `.` | `fmt。Println(value)` |
| `【` | `[` | `items【index】` |
| `】` | `]` | `int【】` 与 `values【key】` |
| `；` | `;` | `value := 1； next := 2` |
| `，` | `,` | `[1，2，3]` 与 `call(a，b)` |
| `《` | `<` | `Result《string》` 与 `a《b` |
| `》` | `>` | `Result《string》` 与 `a》b` |
| `？` | `?` | 保持与 ASCII `?` 相同的词法结果 |
| `“` 与 `”` | `"` 与 `"` | `message := “Mao”` |
| `‘` 与 `’` | `'` 与 `'` | `letter := ‘猫’` |
| `～` | `~` | 类型约束 `～int` |

转换由词法分析器把中文标点识别为相应记号完成，不得在解析前对源码进行全文字符串替换。`“…”` 作为 Go 解释字符串的中文定界形式，`‘…’` 作为 Go rune 字面量的中文定界形式；内容遵循 Go 转义和 rune 数量规则，生成 Go 时由格式化器改写为 ASCII 引号并正确转义内容。

已经位于 ASCII 字符串、中文引号字符串、字符字面量、原始字符串或注释内部的其他中文标点保持原样：

```mao
text := "【】。；，《》？～"
```

`？` 转换成 ASCII `?`。第一版只把 `?` 用于可空类型后缀，不新增三元运算符、可选链或空值合并语义；出现在其他位置时产生语法错误。`mao fmt` 将所有受支持的中文代码标点统一输出为对应 ASCII 形式，同时保持字符串内容和注释不变。

### 2.3 声明、类型与泛型

```mao
name := "Mao"                    // 推断为 string
int age = 3                      // 显式类型
const maxLives = 9               // 只有 const 是常量

string[] names                   // Go: []string
int[3] scores                    // Go: [3]int
string:int[] ages                // Go: map[string]int
Result<string> result            // Go: Result[string]
table<string, int> tableAges     // Mao 统一集合
int? optionalAge                 // 可以保存 int 或 null
```

类型后缀按照从右向左的确定规则解析：

| Mao 类型 | 生成的 Go 类型 |
|---|---|
| `T[]` | `[]T` |
| `T[N]` | `[N]T` |
| `K:V[]` | `map[K]V` |
| `string:int[][]` | `map[string][]int` |
| `(string:int[])[]` | `[]map[string]int` |
| `string:(int:bool[])[]` | `map[string]map[int]bool` |
| `Generic<T>` | `Generic[T]` |
| `table<K, V>` | Mao 运行时 `Table[K, V]` |
| `T?` | Mao 运行时可空值 `Optional[T]` |

括号只用于消除复合类型的结合歧义，不改变底层 Go 类型。

`T?` 表示值可以是 `T` 或 `null`。`null` 值在生成代码中使用带状态的可空值表示，不用 `T` 的零值代替，因此 `0`、`false`、`""` 与 `null` 始终可以区分。对已经能够保存 Go `nil` 的类型，`T?` 仍保留“是否为 Mao `null`”这一显式状态，不依赖底层值是否为 `nil`。

`T[]`、`T[N]` 和 `K:V[]` 只表示与 Go API 互操作时使用的原生切片、数组和 `map` 类型，不属于 Mao 集合类型；它们不再是 Mao 集合字面量的默认结果。Mao 源码中的 `[...]` 始终首先构造 `table`。

标准 Go 的 `map[K]V` 类型写法不在 `.mao` 中接受，因为 `map` 已用于转换表达式；需要显式标注 Go 原生 `map` 时使用前置类型 `K:V[]`。

### 2.4 浮点类型

`float` 是 Mao 的标准浮点类型，固定生成 Go `float64`：

```mao
float price = 3.14
ratio := 0.5       // 默认推断为 float
```

为兼容 Go 接口，Mao 仍允许显式使用 `float32` 和 `float64`。从 `float` 或 `float64` 转为 `float32` 可能损失精度，因此必须显式写 `float32(value)`；编译器不得静默缩窄。无类型浮点常量可按照 Go 的常量表示规则赋给目标浮点类型。

### 2.5 统一 `table`

所有集合字面量 `[...]` 都生成 `table`。裸元素是列表值，由编译器分配从 0 开始的隐式整数键；显式 `key:value` 项使用给定键：

```mao
numbers := [1, 2, 3]
// 等价于 [0: 1, 1: 2, 2: 3]
// 推断为 table<int, int>

ages := ["cat": 3, "dog": 5]
// 推断为 table<string, int>

nullableAges := ["cat": null, "dog": 5]
// 推断为 table<string, int?>

profile := ["name": "Mao", "age": 3]
// 推断为 table<string, any>

mixed := [1, "name": "Mao", "enabled": true]
// 等价于 [0: 1, "name": "Mao", "enabled": true]
// 推断为 table<any, any>

duplicates := [1, 1, 2]
// 等价于 [0: 1, 1: 1, 2: 2]，重复值完整保留

empty := []
// 推断为 table<any, any>
```

第一版采用以下规则：

1. 不带冒号的 `value` 是裸元素，编译器依次为裸元素分配隐式键 `0`、`1`、`2`；重复值因为隐式键不同而完整保留。
2. `key:value` 是显式键值项。`key:` 与 `key:null` 完全等价，均表示键存在且值为 `null`。
3. 同一个字面量可以混合裸元素与显式键值项。显式整数键不改变裸元素的独立编号计数。
4. 空 `table` 写作 `[]`，无目标类型时推断为 `table<any, any>`；不再使用用于区分字典的 `[:]`。
5. `table` 保持项目的插入顺序，遍历和 `at`、`DeleteAt` 均使用该顺序。
6. 键必须唯一。显式键之间，或者显式键与隐式整数键发生重复时，源代码中靠后的项目替换原值，但保留该键第一次出现的位置。
7. 键必须满足 Go 的可比较性要求。编译器拒绝能够静态确定为不可比较的键；`any` 中无法静态确定的动态值沿用 Go 的运行时检查和 panic 行为。
8. 无目标类型时，编译器分别推断全部隐式及显式键和值的共同类型；不存在精确共同类型时，对应类型使用 `any`。
9. 没有 `null` 时，值类型保持推断出的类型 `V`。同时出现不可空 `T` 和 `null` 时，值类型推断为 `T?`；共同类型已经是 `any` 或 Go 可空类型时直接使用该类型。只有 `null` 时推断为 `any`。可空类型归一化，`(T?)?` 与 `T?` 相同。
10. 显式目标类型优先于自动推断。不能转换为目标键类型或目标值类型的项目产生编译错误；目标值类型不可空时，`null` 项产生编译错误。
11. 项分隔符接受中文逗号 `，` 和 ASCII 逗号 `,`；格式化器统一输出 ASCII 逗号。

`table<K, V>` 的 `K` 是键类型，`V` 是实际保存的值类型。`V` 可以是普通类型、`T?` 或 `any`；普通值不会仅因位于 `table` 中就自动包装为可空值。索引读取需要表示键缺失，因此 `table<K,V>[key]` 的结果类型是 `V?`，并按上述归一化规则避免产生嵌套可空类型。`table` 是 Mao 的正式运行时集合类型，不再根据字面量形式生成不同的 Go 数组、切片或 `map`。

`table` 采用引用语义。变量赋值、参数传递和返回值传递共享同一个集合状态，对任一引用执行新增、替换或删除都会被其他引用观察到。`table` 的零值是已经初始化语义上的空表，可以直接读取和写入，不要求显式构造。第一版不提供隐式深复制。

键除满足 Go 可比较性外，还必须满足自反相等，即 `key == key` 为真。包含 `NaN` 等不满足自反相等条件的键在运行时被拒绝，避免产生无法再次查找或删除的项目。

### 2.6 `null`

Mao 源码只暴露 `null`，不暴露 `nil`：

```mao
table<string, int?> values = ["cat": null, "dog": 5]
int? age = values["cat"]
```

- 显式 `key:` 项省略冒号后的值与 `key:null` 完全等价；裸元素是列表值，不表示 `null`。
- `table<K,V>` 直接保存 `V`。普通 `table<string,int>` 不能保存 `null`；含 `null` 的对应类型是 `table<string,int?>`。
- `table` 之外，`null` 可以赋给 `T?` 以及 Go 中可为空的类型：指针、切片、映射、通道、函数和接口。
- 向整数、浮点数、布尔、字符串、结构体或数组赋 `null` 是编译错误。
- 将 `T?` 直接赋给 `T` 是编译错误；应先与 `null` 比较完成类型收窄，或者在转换到不可空 Go 类型时明确提供替代值。
- `null` 在 Mao 的预声明环境中占据 Go `nil` 的位置，具有相同的作用域和可遮蔽规则，不是关键字。
- 生成的 Go 可以在内部使用 `nil` 和 Mao 运行时的可空值表示，但诊断信息和 Mao 格式化结果只使用 `null`。

条件判断可以对局部可空值进行类型收窄：

```mao
age := values["cat"] // int?
if age != null {
    int exactAge = age
}
```

第一版只对当前作用域内未被重新赋值或别名修改的局部值执行这种收窄；无法证明稳定时仍要求显式处理可空值。

### 2.7 `table` 读取、顺序与删除

```mao
value := values[key]                   // V?；键缺失或值为空时均为 null
value := values.get(key, defaultValue) // V；仅在键缺失时使用默认值
exists := values.has(key)              // 只判断键是否存在
entry := values.at(index)              // 按插入位置读取 Entry<K,V>
values[key] = value                    // 新增或替换
values.Delete(key)                     // 按键删除
values.DeleteAt(index)                 // 按插入位置删除
values.clear()                         // 删除全部项目
```

第一版采用以下操作规则：

- `values[key]` 返回归一化后的 `V?`。键不存在时结果为 `null`；当 `V` 本身可空时，已有键的值也可能是 `null`，使用 `has` 区分这两种情况。
- `get(key, defaultValue)` 返回 `V`。键存在时返回保存的值，包括 `V` 允许的 `null`；只在键不存在时返回默认值。默认值表达式只在键不存在时求值。
- `has(key)` 只判断键是否存在。已有键的值为 `null` 时仍返回 `true`。
- `at(index)` 按从零开始的插入位置返回 `Entry<K,V>`；项目已经确定存在，因此 `.value` 的类型是 `V`。索引越界时产生运行时 panic。
- `values[key] = value` 在键不存在时把项目追加到末尾；键已经存在时只替换值，不改变位置。
- `Delete(key)` 按键删除；键不存在时静默忽略。
- `DeleteAt(index)` 按从零开始的插入位置删除；索引越界时产生运行时 panic。
- `clear()` 删除全部项目，调用后长度为零。
- 不支持 `del`、`remove`、`pop` 或根据参数类型改变含义的备用删除接口。
- `len(values)` 返回项目数量。

`table` 遍历固定采用插入顺序：

```mao
for key, value := range values {
    // key 的类型为 K，value 的类型为 V
}

for key := range values {
    // 单变量形式只取得键
}
```

生成代码必须保证接收对象、键、索引和值表达式各求值一次。`get` 的默认值表达式只在键不存在时求值，不得提前求值。

### 2.8 `table` 与 Go 集合互操作

Mao `table` 与 Go 原生集合不是同一种内存布局，必须通过明确转换互操作：

```mao
values := [1, 2, 3]                    // table<int, int>
int[] nativeValues = values            // 按插入顺序转换为 Go []int

nullableValues := [1, null, 3]         // table<int, int?>
any[] preservedValues = nullableValues.values() // null 生成 Go nil
int[] filledValues = nullableValues.values(0)    // null 替换为 0

settings := ["width": 800]             // table<string, int>
string:int[] nativeSettings = map(settings)

nullableSettings := ["width": null, "height": 600] // table<string, int?>
string:any[] preservedSettings = map(nullableSettings)
string:int[] filledSettings = map(nullableSettings, 0)

fromSlice := table(nativeValues)        // 索引成为键，元素成为值
fromMap := table(nativeSettings)        // 保留 Go map 的键和值
```

- Go 数组或切片转换为 `table` 时，索引 `0`、`1`、`2` 依次成为键，原元素成为值并保持顺序；重复元素完整保留。
- Go `map` 转换为 `table` 时保留键和值。Go `map` 没有稳定遍历顺序，因此转换产生的初始插入顺序不作保证。
- `keys()` 按插入顺序返回 Go 兼容切片。
- `table<K,V>` 的 `V` 不是 Mao `T?` 且可赋给目标元素类型时，可以在具有明确 Go 切片目标类型的声明或赋值中直接转换；项目值按插入顺序复制，键不进入切片。
- `values()` 在 `V` 不是 Mao `T?` 时返回 `V[]`；在 `V` 为 `T?` 时返回 `any[]`，使用 Go `nil` 保存 Mao `null`。
- `values(defaultValue)` 用于可空值表，返回 `T[]` 并使用默认值替换全部 `null`。
- `map(tableValue)` 是由 `map` 关键字引导的转换表达式。`V` 不是 Mao `T?` 时返回 Go `map[K]V`；`V` 为 `T?` 时返回 Go `map[K]any`，并使用 Go `nil` 保存 Mao `null`。
- `map(tableValue, defaultValue)` 用于可空值表，返回 Go `map[K]T`，使用默认值替换全部 `null`。
- `map(...)` 只负责向 Go 原生 `map` 转换，不构造 Mao 集合；Mao 集合始终使用 `table(...)` 或 `[...]`。
- 从 `table` 转换为 Go 数组时，项目数量必须与数组长度相等；转换按照插入顺序复制值。
- 所有 `table` 与 Go 原生集合转换都会创建独立集合。转换后对一方的新增、删除、重新排序或元素赋值不会自动同步到另一方。
- `table` 作为 Mao 导出函数的参数或返回值时，对应生成的 Go API 使用 Mao 运行时 `Table[K,V]`；Go 调用方需要依赖该公开运行时类型。

### 2.9 自动推断与转换

第一版的自动转换以不静默丢失信息为边界：

- 字面量根据目标类型进行 Go 允许的常量表示转换。
- 相同符号类别的定宽整数变量可以自动转换到位宽更大的类型，例如 `int8` 到 `int16`、`uint16` 到 `uint32`。
- `int8`、`int16` 和 `int32` 可以自动转换为 `int64`；`uint8`、`uint16` 和 `uint32` 可以自动转换为 `uint64`。
- `int` 可以自动转换为 `int64`，`uint` 和 `uintptr` 可以自动转换为 `uint64`。固定宽度整数不得自动转换为依赖目标架构宽度的 `int`、`uint` 或 `uintptr`。
- `float32` 可自动扩宽为 `float`/`float64`。
- 整数变量转换为 `float`、有符号与无符号整数互转、浮点转整数以及 `float` 转 `float32` 必须显式转换。
- 不同的命名类型之间沿用 Go 的显式转换要求，即使它们的底层整数类型满足扩宽条件；类型别名按其所指类型处理。
- 字符串与字节、字符序列之间沿用 Go 的显式转换规则。
- 无法证明安全的自动转换产生编译错误，诊断信息给出所需的显式转换写法。

这些自动转换规则统一适用于赋值、实参传递和返回值检查，不改变算术表达式的 Go 类型规则。

## 3. 与 Go、Lua 及其他语言的定位对照

| 能力 | Mao | Go | Lua 5.x | Python |
|---|---|---|---|---|
| 类型系统 | 静态类型，支持 `any` | 静态类型 | 动态类型 | 动态类型 |
| 显式类型位置 | `int age` | `var age int` | 不声明静态类型 | `age: int` 类型注解 |
| 推断变量 | `age := 3` | `age := 3` | `age = 3` | `age = 3` |
| 默认浮点 | `float`，对应 64 位 | `float64` | `number` 的浮点表示通常为双精度 | `float`，通常为 64 位 |
| 统一集合 | `table<K,V>` | 无；数组、切片和 `map` 分离 | `table` | 无；`list` 与 `dict` 分离 |
| 集合顺序 | 固定为插入顺序 | 切片有序，`map` 无序 | 数组部分有惯用顺序，通用遍历顺序不保证 | `list` 有序，`dict` 保持插入顺序 |
| 异构集合 | 通过 `any` | 通过 `any` | 原生支持 | 原生支持 |
| 缺失键读取 | `null` | `map` 值类型零值 | `nil` | 抛出 `KeyError` |
| 保存空值项目 | 键存在且值为 `null` | 取决于值类型 | `nil` 会删除键，不能保存 | 可以保存 `None` |
| 带默认值读取 | `t.get(k, d)` | 双值查询后自行选择 | `t[k]` 配合条件判断 | `d.get(k, d)` |
| 是否存在 | `t.has(k)` | `_, ok := m[k]` | 通常用 `t[k] ~= nil` | `k in d` |
| 按键删除 | `t.Delete(k)` | `delete(m, k)` | `t[k] = nil` | `del d[k]` |
| 按位置删除 | `t.DeleteAt(i)` | `slices.Delete(s, i, i+1)` | `table.remove(t, i)` | `del values[i]` |
| 清空 | `t.clear()` | `clear(m)` 或 `clear(s)` | 逐项设为 `nil` | `values.clear()` |
| 空值 | `null` | `nil` | `nil` | `None` |

Lua 的 `table` 同时承担数组、列表、集合、对象和字典等用途；所谓数组通常只是从整数键 `1` 开始连续存储的 `table`。给某个键赋 `nil` 会移除该键，因此 Lua 的 `table` 不能把 `nil` 作为可区分于“键不存在”的普通值保存。

Mao 的 `table` 同样统一集合类型，但与 Lua 存在三项明确差异：Mao 保持静态类型，固定保留插入顺序，并允许键存在而值为 `null`。Mao `table` 是运行时类型，不与 Go 数组、切片或 `map` 共享内存布局。

## 4. 编译器架构

### 4.1 工具链

第一版使用 Go 实现以下命令：

```text
mao build [packages]
mao run <file-or-package>
mao test [packages]
mao fmt [files]
mao check [packages]
mao emit-go [files]
```

处理流程固定为：

```text
.mao 源码
→ 词法分析
→ Mao 抽象语法树
→ 名称解析与类型检查
→ Mao 特性降级
→ Go 抽象语法树
→ go/format 生成 Go
→ 官方 go build/run/test
```

不得通过正则表达式或文本替换实现语言转换。类型前置、嵌套复合类型、统一字面量、可空类型和 `table` 操作都必须经过语法树与类型检查阶段。

### 4.2 包和 Go 互操作

- `.mao` 与 `.go` 文件可以位于同一 Go 包。
- Mao 可以直接 `import` Go 模块并调用其导出标识符。
- 生成代码保持 Go 包名、导入路径、导出规则和普通 Go 类型的结构体布局及函数调用约定；`table` 和 `T?` 明确使用 Mao 运行时类型。
- 构建缓存放在项目临时目录中，不把生成的 `.go` 文件混入用户源目录。
- 生成代码写入 `//line` 指令，使 Go 编译错误和运行时堆栈尽量指回 `.mao` 文件。
- `mao emit-go` 用于查看生成结果和诊断编译问题。
- “兼容 Go”指生态、包级调用和通过明确转换完成的数据互操作；由于 Mao 改用了类型前置、尖括号泛型并增加了 `table` 运行时类型，不承诺把任意 `.go` 文件改名为 `.mao` 后仍可解析，也不承诺 `table` 与 Go 原生集合零成本互换。

### 4.3 运行时支持

第一版提供由编译器自动链接的内部运行时包。Mao 项目不需要手动编写 `import`，但生成的 Go 代码会依赖该包。

运行时至少提供以下泛型结构：

```go
type Optional[T any] struct {
    value   T
    present bool
}

type Entry[K comparable, V any] struct {
    Key   K
    Value V
}

type Table[K comparable, V any] struct {
    state *tableState[K, V]
}

type tableState[K comparable, V any] struct {
    entries []Entry[K, V]
    index   map[K]int
}
```

- `entries` 保存稳定的插入顺序。
- `index` 提供按键查询；删除项目后更新受影响的位置索引。
- `Table.state` 使赋值和参数传递保持引用语义；状态为空时由首次写操作延迟初始化。
- `Entry.Value` 直接保存 `V`；普通 `table<K,V>` 不为每个项目创建 `Optional[V]`。
- `Optional.present` 只用于源码中的 `T?` 以及可能缺失的索引读取，区分 Mao `null` 与 `T` 的零值。编译器负责把 `(T?)?` 归一化为 `T?`。
- `get`、`has`、`at`、`Delete`、`DeleteAt`、`clear`、`keys`、`values` 和遍历由编译器生成对运行时类型的直接调用；`map(...)` 转换由编译器生成运行时遍历及 Go 原生 `map` 构造代码。
- 运行时包属于 Mao 工具链的公开兼容边界。其导出 Go API 必须有版本策略，因为 Go 代码可能直接调用 Mao 导出的 `Table` 类型。
- 第一版不使用反射实现 `table` 基本操作；异构内容通过 Go 接口值 `any` 保存。

## 5. 实施阶段

### 阶段一：语法、可空值与生成闭环

- 建立 `mao` 命令、词法分析器、解析器、抽象语法树和位置映射。
- 实现全部中文标点等价记号、中文字符串与 rune 定界符，以及对应的源码位置跟踪。
- 支持包、导入、函数、控制流、表达式及 Go 基础声明。
- 实现类型前置、`T[]`、`T[N]`、`K:V[]`、`Generic<T>`、`table<K,V>` 和 `T?`。
- 建立 `Optional` 与 `Table` 运行时包，并实现编译器自动导入。
- 生成格式正确且可由当前 Go 工具链编译的代码。

完成标准：包含 Mao 与 Go 文件的同包示例能够通过 `mao build`，生成代码可由 `gofmt` 无修改接受。

### 阶段二：统一 `table`

- 实现所有集合字面量到 `table` 的类型推断和代码生成。
- 实现裸元素隐式整数键、同类型与异构键值推断、省略值、`null`、`any`、重复键及对应错误诊断。
- 实现索引读取、`get`、`has`、`at`、`Delete`、`DeleteAt`、`clear`、`len` 和稳定顺序遍历。

完成标准：重复裸值、值为 `null`、键不存在、重复显式键、异构项目、稳定顺序、嵌套 `table` 和全部删除操作均通过行为测试。

### 阶段三：Go 集合互操作、转换与开发工具

- 按安全转换规则实现赋值、参数和返回值检查。
- 实现 `float` 到 Go `float64` 的完整映射。
- 实现数组、切片、`map` 与 `table` 的双向转换及错误诊断。
- 完成 `mao fmt`、`mao check`、`mao test`、`mao emit-go`。
- 校正 `.mao` 源码位置、错误信息和运行时堆栈。

完成标准：错误均指向 Mao 源码；格式化结果稳定；同一输入重复生成完全一致的 Go 代码。

### 阶段四：发布准备

- 编写语言规范、Go 对照迁移指南和最小示例项目。
- 建立 Linux、macOS、Windows 的构建与测试流程。
- 固定第一版语法，新增解析快照和兼容性测试。
- 狸花猫形象只用于名称、图标和文档，不进入编译器语义。

## 6. 测试与验收

### 6.1 解析和格式化

- 覆盖所有前置类型及多层嵌套组合。
- 覆盖 `。→.`、`【→[`、`】→]`、`；→;`、`，→,`、`《→<`、`》→>`、`？→?`、`～→~`，验证 `mao fmt` 统一输出 ASCII 标点。
- 覆盖 `“…”` 字符串和 `‘…’` rune，验证转义、Unicode、非法闭合及 rune 数量诊断。
- 验证字符串、字符字面量、原始字符串和注释中的中文标点不被转换。
- 覆盖 `table<K,V>`、`T?`、重复裸值、纯显式键值、混合项目、省略显式值和空 `table` 字面量。
- 覆盖 `map(tableValue)` 和 `map(tableValue, defaultValue)`，并验证标准 Go `map[K]V` 类型写法在 `.mao` 中产生明确诊断。
- 验证 `mao fmt` 幂等：连续运行两次内容不再变化。

### 6.2 类型检查

- 验证无目标类型和显式 `table<K,V>` 目标类型的推断。
- 验证同类型键值保留精确类型，异构键或值分别推断为 `any`。
- 验证只有 `null` 值时值类型推断为 `any`，空 `[]` 推断为 `table<any,any>`。
- 验证显式目标类型优先于默认推断，不兼容的键和值产生编译错误。
- 验证 `table<K,V>` 索引类型为归一化后的 `V?`，而 `at` 和项目遍历值保持为实际存储类型 `V`。
- 验证 `T?` 与 `null` 比较后的局部类型收窄，以及不安全的 `T?` 到 `T` 赋值被拒绝。
- 验证 `float`、`float32`、`float64` 的扩宽和禁止缩窄。
- 验证每一条整数扩宽边界、禁止跨有符号与无符号自动转换，以及不同命名类型必须显式转换。
- 验证 `null` 只能进入可空类型、Go 可空类型或值类型可空的 `table` 项目。
- 验证键的可比较性和 `any` 动态键的运行时行为。
- 验证 `NaN` 及包含 `NaN` 的复合键因不满足自反相等而被拒绝。
- 验证 `get` 默认值与实际存储类型 `V` 兼容，并且返回类型为 `V`。
- 验证数组或切片的元素作为 `table` 值时可以是不可比较类型，因为转换后的键固定为整数索引。

### 6.3 运行行为

- 验证 `key:` 与 `key:null` 产生相同结果，同时与 `V` 的零值不同。
- `[1, 1, 2]` 使用隐式键 `0`、`1`、`2` 并完整保留两个值 `1`。
- 混合字面量中的显式整数键不改变裸元素从 0 开始的独立编号。
- 显式键与隐式键冲突时，后项覆盖值但保留该键第一次出现的位置。
- `values[key]` 在键缺失时返回 `null`，不返回 `V` 的零值。
- `has` 能区分缺失键与值为 `null` 的已有键。
- `get` 只在键缺失时求值默认值表达式；使用具有可观察副作用的默认值函数验证。
- `get` 在键存在且值为 `null` 时返回 `null`，不使用默认值。
- 重复键替换值但保留第一次出现的位置。
- 新键赋值追加到末尾，已有键赋值不改变位置。
- `table` 赋值和参数传递共享状态，零值表可以直接读写。
- `at(index)` 分别验证首项、末项、空表和越界行为。
- `Delete(key)` 按键删除并保持其余项目的相对顺序；缺失键静默忽略。
- `DeleteAt(index)` 按插入位置删除；分别验证首项、末项、空表和越界行为。
- `Delete(1)` 始终删除键 `1`，`DeleteAt(1)` 始终删除位置 `1`，两者不得根据参数类型改变含义。
- 任何类型使用 `del`、`pop` 或 `remove` 均按未声明标识符或普通成员解析，不提供内置语义。
- `clear()` 删除全部项目并使长度归零。
- `range`、`keys()`、`at()` 和删除后的遍历顺序保持一致。

### 6.4 Go 兼容

- Mao 调用 Go 标准库、第三方模块和泛型 API。
- Go 文件调用同包 Mao 生成的导出函数与类型。
- Go 数组和切片转换为 `table` 后，索引成为键、元素成为值，重复元素完整保留。
- Go `map` 转换为 `table` 后键值保持一致，但不承诺初始顺序。
- 值类型不是 Mao `T?` 的 `table` 可以通过明确的 Go 目标类型直接转换为数组或切片，不要求提供默认值。
- 值类型是 Mao `T?` 的 `table` 转换为不可空 Go 数组、切片或 `map` 时必须提供默认值，不得静默使用 Go 零值。
- `keys()`、`values()`、`values(defaultValue)`、`map(tableValue)` 和 `map(tableValue, defaultValue)` 的类型、空值替换及顺序丢失规则符合规范。
- Go 调用方能够通过 Mao 公开运行时 API 接收、构造和读取 `Table[K,V]`。
- `go test`、竞态检测和基准测试能够作用于生成后的包。
- 编译错误和 panic 的位置能够映射回 `.mao` 源文件。

## 7. 第一版边界

第一版不包含宏、运算符重载、除 `Table` 与 `Optional` 以外的通用对象运行时、垃圾回收器、独立包管理器、Go 编译器分支或编辑器语言服务器。编辑器支持在语法和编译接口稳定后单独规划。

第一版保持静态类型，并以唯一的 Mao `table` 类型统一数组、列表、切片和字典用途。异构键值使用 Go `any`，而不是引入完全动态的值系统。Mao 运行时依赖 Go 垃圾回收器和泛型，不实现独立内存管理。

Mao 以 Go 语法和语义为基线。计划书只列出 Mao 明确增加或替换的部分；没有明确修改的 Go 关键字、表达式、语句、预声明标识符和运行行为继续有效。

第一版明确增加或替换的语法如下：

- 所有方括号集合字面量生成保持插入顺序的 `table<K,V>`；裸元素获得从 0 开始的隐式整数键，显式 `key:` 的省略值等价于 `null`。
- `table` 的异构键值使用 Go `any`；能够推断共同类型时保留精确的 `K` 和 `V`。
- `table` 直接保存推断出的 `V`；不可空类型需要保存 `null` 时才提升为 `T?`，已经能够保存 `null` 的 `any` 和 Go 可空类型不重复包装，普通值不自动包装为可空值。
- `table` 索引返回 `V?`，缺失键返回 `null`；`has` 用于区分缺失键和值为 `null` 的已有键。
- `table.Delete(key)` 固定按键删除，`table.DeleteAt(index)` 固定按插入位置删除，不增加 `del`、`remove` 或 `pop`。
- `table.clear()` 删除全部项目；Go 的预声明函数 `clear` 对 Go 原生集合仍然有效。
- `table(goCollection)` 负责从 Go 数组、切片或 `map` 转换；`keys()`、`values()` 和由 `map` 关键字引导的转换表达式负责转换回 Go 集合。
- Mao 源码以 `null` 替换预声明空值 `nil`，并增加 `T?` 表示可空值；生成代码使用 Go `nil` 和 Mao `Optional`。
- 新增固定对应 Go `float64` 的 `float`，不增加上下文相关的可变宽度浮点类型。
- 赋值、实参和返回值允许计划所列的无损整数扩宽及 `float32` 到 `float`/`float64` 的扩宽；其他数值转换沿用显式转换要求。
- 复合类型使用 Mao 的前置写法，不同时接受 Go 的后置复合类型；这一项是对 `.mao` 类型语法的明确替换。
- 泛型实例化使用 `Generic<T>`，不同时接受 Go 的 `Generic[T]`；同一项目中的 `.go` 文件继续使用标准 Go 语法。
- 代码记号接受计划所列中文标点并格式化为对应 ASCII 形式；这些等价记号不改变对应 Go 记号的语法和语义。

除上述明确差异外，生成的内部 Go 代码和未修改的 Mao 表面语法均沿用 Go。

## 8. 当前实现状态

截至 2026 年 7 月 27 日，除明确暂缓的中文语言方案外，第一版计划已经形成可运行实现：

- 已建立 Go 模块、`mao` 命令入口、基于 `text/scanner` 的词法分析、Mao 抽象语法树、递归下降解析器、静态类型检查和 Go 抽象语法树生成器；转换过程不使用正则表达式或全文字符串替换。
- 已实现包、导入、函数、方法、泛型、结构体、接口、函数类型、指针、通道、包级声明、局部声明、主要控制流与表达式，以及前置数组、切片、原生映射、泛型和可空类型。
- 已实现赋值、实参、返回值和多值返回结果的类型传播；计划列出的整数与浮点扩宽规则、命名类型限制及不安全转换诊断均有自动化测试。
- 已实现 `Optional[T]` 与保持插入顺序、采用引用语义的 `Table[K,V]` 运行时，以及字面量推断、重复键、异构键值、`null`、索引读写、全部 `table` 操作和稳定遍历。
- 已实现 `table` 与 Go 数组、切片和原生 `map` 的双向复制转换，包括可空值保留、默认值替换、值类型扩宽和数组长度检查。
- 已实现 `.mao` 与 `.go` 同包构建、多 Mao 文件共享函数及类型信息、导入 Go 标准库与泛型 API，以及 `build`、`run`、`test`、`fmt`、`check`、`emit-go` 全部命令。命令接受文件、目录和 Go 风格的 `...` 递归包模式。
- 临时生成代码不会写入源码目录；生成结果确定且经过 `go/format`，Go 编译诊断和运行时堆栈通过 `//line` 指令指向 `.mao` 文件。
- 已建立运行时、编译器、命令行、同包互操作、错误路径、竞态检测和基准测试，并配置 Linux、macOS、Windows 持续集成。

根据此前确定的实现范围，中文关键字、中文代码标点和中文引号仍暂不实现。词法分析器会明确拒绝代码记号及标识符中的非 ASCII 字符，但允许字符串内容包含 Unicode；第 2.2 节保留为后续实现规范，不计入本轮完成范围。
