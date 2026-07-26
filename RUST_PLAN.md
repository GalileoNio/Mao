# Mao 对接 Rust 与双向源码转换方案

## 1. 目标

本方案分为先后两个目标，二者不是同一阶段同时实现。

### 初级目标

1. Mao 源码可以使用现有 `import` 和调用语法导入 Rust crate。
2. Mao 可以调用 Rust crate 中类型可表达、所有权关系可确定的公开函数、关联函数、方法、常量和类型。
3. Mao 源码可以转换为合法的 Rust 源码。
4. 生成的 Rust 不仅通过语法解析，还应在声明的依赖条件下通过 Rust 类型检查。

初级目标的处理方向为：

```text
Mao 源码
→ 解析 Rust crate 的公开接口
→ Mao 名称解析与类型检查
→ Rust 抽象语法树
→ 合法 Rust 源码
```

### 最终目标

在 Mao 到 Rust 的类型、调用和语义映射稳定后，增加 Rust 源码到 Mao 源码的转换：

```text
Mao 源码 ⇄ Rust 可转换子集
```

最终目标不承诺转换任意 Rust 程序。Rust 独有且 Mao 当前语法无法表达的所有权、生命周期、异步和底层内存能力必须报告错误，不得根据名称或代码形状推测作者意图。

两个阶段共同采用语义等价标准，不要求字符级还原。格式、临时变量名称、显式类型转换和编译器生成的辅助结构允许变化。

每个目标都需要独立的：

- 语法能力表。
- 数值配置。
- 标准库与模块加载规则。
- 不支持项。
- 运行时构建。
- 持续集成测试。

完成标准：同一 Mao 包及其锁定依赖具有一致的公开结果；实现差异必须在构建时诊断，不能由运行时静默选择不同语义。

本文不讨论 Mao 当前的 Go 后端、跨语言链接或库的二进制接口。

## 2. 设计原则

### 2.1 保持现有 Mao 语法

本方案不为 Rust 增加 Mao 关键字，不引入所有权、借用或生命周期语法。Mao 继续使用：

- 前置类型。
- `:=` 局部变量推断。
- `T?` 与 `null`。
- `table<K,V>`。
- `Generic<T>` 泛型实例化。
- Go 基线的函数、结构体、接口、方法和控制流语法。

Rust 中无法直接表达的 Mao 语义，通过生成的 Rust 辅助类型或结构化降级实现，不反向污染 Mao 语法。

### 2.2 不伪造完全对等关系

下列概念不存在普遍的一一对应：

| Mao 概念 | Rust 概念 | 差异 |
|---|---|---|
| 垃圾回收引用 | 所有权与借用 | 生命周期和释放时机不同 |
| `interface` | `trait` / `dyn Trait` | 对象安全、关联类型和泛型方法规则不同 |
| `any` | `dyn Any` | 可克隆性、可比较性和向下转换规则不同 |
| 多返回值 | 元组 | Rust 元组是一等值，Mao 多返回值不是 |
| `defer` | 作用域析构 | 执行时机接近，但捕获和 panic 行为不同 |
| goroutine | Rust 线程或异步任务 | 调度模型不同 |
| channel 与 `select` | Rust 通道生态 | Rust 标准库没有等价 `select` |
| `panic` / `recover` | `panic` / `catch_unwind` | 可恢复范围和安全保证不同 |
| `goto` | 无安全 Rust 对应语句 | Rust 不提供任意跳转 |

转换器只在规则明确时转换；规则不明确时给出源位置、相关语言特性和不能保持的语义。

### 2.3 先固定正向输出，再实现反向识别

初级目标生成的 Rust 从第一天起使用统一写法，使最终目标能够识别：

- 所有 Mao 运行时辅助类型统一位于 `mao_rt` 模块。
- Mao `table` 只生成 `mao_rt::Table<K,V>`。
- Mao 共享引用只生成 `mao_rt::Ref<T>`，不混用 `Rc<RefCell<T>>` 和 `Arc<RwLock<T>>` 的展开形式。
- Mao `any` 只生成 `mao_rt::Any`。
- Mao 的 `defer` 只生成规定的作用域守卫形式。
- Mao 数据枚举编码只生成规定的 trait 与变体结构组合。

最终阶段的 Rust 到 Mao 转换器只识别这些规范形式和本文明确列出的普通 Rust 子集，并恢复为对应的 Mao 语义。

## 3. 文件、包和名称

### 3.1 包与模块

| Mao | Rust |
|---|---|
| `package name` | 当前 Rust 模块或 crate 的规范模块 |
| `import "path"` | `use path;` |
| `import alias "path"` | `use path as alias;` |
| 首字母大写的导出名称 | `pub` 项 |
| 小写开头的包内名称 | 非 `pub` 项 |

Rust 没有通过名称首字母决定可见性的规则。Mao 到 Rust 时，转换器根据 Mao 的导出规则生成或省略 `pub`。Rust 到 Mao 时：

- `pub` 项生成导出名称。
- 非 `pub` 项生成包内名称。
- 如果原名称的首字母与目标可见性冲突，转换器进行确定的名称调整，并记录名称映射。

名称调整不得只改变大小写后导致两个项目同名。发生冲突时必须报告错误，由用户先消除冲突。

### 3.2 导入

Mao 使用现有带别名导入语法引用 Rust crate：

```mao
import regex "rust:regex"
import json "rust:serde_json"
import value "rust:serde_json/value"
```

规则如下：

- `rust:` 位于导入路径字符串内，不是新关键字。
- `rust:crate_name` 指向 crate 根。
- `rust:crate_name/module/path` 指向公开模块。
- 导入别名是 Mao 源码中的本地名称。
- 生成 Rust 时，crate 与模块路径使用 Rust 的 `::`。
- Rust package 名称与 Rust 源码使用的 crate 名称不一致时，以已经解析的 crate 元数据为准，不能只把连字符替换为下划线后假定结果正确。
- 没有 `rust:` 前缀的导入继续按 Mao 原有规则解析，不得自动尝试同名 Rust crate。

只导入 Rust crate 的公开项目。私有模块、私有类型、私有字段和私有函数不得通过 Mao 绕过 Rust 可见性。

### 3.3 Rust 名称和调用

Mao 继续使用 `.` 访问包成员、类型成员和实例方法。生成 Rust 时根据名称解析结果决定使用 `::` 还是 `.`：

| Mao | Rust |
|---|---|
| `json.to_string(value)` | `serde_json::to_string(value)` |
| `regex.Regex.new(pattern)` | `regex::Regex::new(pattern)` |
| `expression.is_match(text)` | `expression.is_match(text)` |
| `pkg.CONSTANT` | `crate_name::CONSTANT` |
| `pkg.Type` | `crate_name::Type` |
| `pkg.function<T>(value)` | `crate_name::function::<T>(value)` |

转换器必须先绑定到 Rust 的确定符号，不能只根据左侧首字母、成员名或调用形状决定分隔符。

例如：

```mao
package main

import regex "rust:regex"

bool containsNumber(string text) {
    pattern := regex.Regex.new("[0-9]+").unwrap()
    return pattern.is_match(text)
}
```

生成的核心 Rust 代码为：

```rust
use regex;

fn contains_number(text: String) -> bool {
    let pattern = regex::Regex::new("[0-9]+").unwrap();
    pattern.is_match(&text)
}
```

该例同时展示三项规则：

1. Rust 关联函数通过 Mao 的类型成员调用形式书写。
2. Rust `Result` 保持为结果类型；只有源码显式调用 `unwrap` 时才生成 Rust `unwrap`。
3. Rust 方法需要 `&str` 时，转换器可以在本次调用期间借用 Mao `string`，不要求 Mao 增加借用语法。

### 3.4 Rust 包接口的初级支持范围

初级目标允许调用：

- 公开自由函数。
- 公开结构体和无数据枚举。
- 公开关联函数。
- 接收者和参数借用关系能够确定的公开方法。
- 公开常量。
- 使用普通类型参数和 trait 上界的公开泛型项目。
- 参数和返回值均属于本文支持类型的接口。
- `Option<T>` 和 `Result<T,E>`。

初级目标暂不允许调用：

- `unsafe fn`。
- 宏和过程宏生成的调用语法；宏展开后形成的普通公开项目不受此限制。
- 返回与输入生命周期关联的借用。
- 接受或返回裸指针的接口。
- `async fn` 和返回 `Future` 的接口。
- 使用 const 泛型、关联类型等式或高阶生命周期约束的接口。
- 需要用户在 Mao 中书写生命周期参数的接口。
- 无法确定移动、复制或借用关系的接口。

### 3.5 调用 Rust 时的所有权处理

Mao 不增加所有权语法。转换器根据 Rust 函数签名和 Mao 变量后续使用情况处理：

| Rust 参数 | Mao 调用规则 |
|---|---|
| `T: Copy` | 直接传值 |
| `&T` | 对当前 Mao 表达式建立只读临时借用 |
| `&mut T` | 参数必须是可寻址、可修改且当前没有冲突使用的 Mao 局部值 |
| 按值 `T`，调用后不再使用 | 移动该值 |
| 按值 `T`，调用后仍要使用，且 `T: Clone` | 在 Rust 调用处生成 `.clone()` |
| 按值 `T`，调用后仍要使用，且 `T` 不可克隆 | 编译错误 |

返回借用不能存入 Mao 普通变量。初级目标遇到 `&T`、`&mut T` 或带生命周期的返回类型时报告错误。

Rust 的方法自动借用只在名称解析已经确认方法接收者类型后使用。转换器不得因为一次 Rust 类型检查失败就无条件增加 `&`、`&mut` 或 `.clone()`。

名称映射只作用于已经解析到确定声明的符号，不改写字符串、注释、用户声明或同名的其他对象。

## 4. 基础类型

### 4.1 数值和布尔类型

| Mao | Rust |
|---|---|
| `bool` | `bool` |
| `int8` | `i8` |
| `int16` | `i16` |
| `int32` | `i32` |
| `int64` | `i64` |
| `uint8` / `byte` | `u8` |
| `uint16` | `u16` |
| `uint32` | `u32` |
| `uint64` | `u64` |
| `int` | `isize` |
| `uint` | `usize` |
| `uintptr` | `usize` |
| `float32` | `f32` |
| `float` / `float64` | `f64` |

Mao `rune` 表示 Unicode 码点对应的整数语义，而 Rust `char` 只允许 Unicode 标量值。转换规则如下：

- 能够静态证明为合法 Unicode 标量值的 Mao `rune` 可以生成 Rust `char`。
- 普通 `rune` 变量生成 `i32`，避免把 Rust `char` 的合法性约束错误地施加给 Mao。
- Rust `char` 转成 Mao `rune`。
- Rust `i32` 只有在源代码语义明确为字符码点时才转成 Mao `rune`，否则转成 `int32`。

### 4.2 数值转换

Mao 允许的无损自动扩宽在生成 Rust 时改写为显式 `From`、`Into` 或确定的转换表达式，因为 Rust 不执行 Mao 所规定的整数自动扩宽。

例如：

```mao
int16 small = 7
int64 large = small
```

生成：

```rust
let small: i16 = 7;
let large: i64 = i64::from(small);
```

Rust 到 Mao 时：

- `T::from(value)` 和无损的 `value.into()` 恢复为 Mao 自动赋值。
- `as` 只在转换器证明目标 Mao 显式转换具有相同截断、符号和浮点行为时转换。
- 依赖 Rust 版本或目标架构的转换不自动简化。

### 4.3 字符串

Mao `string` 生成 Rust `String`。Mao 字符串按值传递且内容不可直接修改；生成 Rust 时采用以下规则：

- 字符串字面量生成 `"text".to_owned()`。
- 函数参数和返回值使用拥有所有权的 `String`。
- Mao 中复制字符串变量时，Rust 生成 `.clone()`。
- 只读临时使用可以由 Rust 优化为 `&str`，但该优化不属于可逆源码形式。

Rust 到 Mao 的可转换子集接受：

- `String`。
- 能够在当前表达式内立即生成拥有值的 `&'static str` 字面量。

普通借用的 `&str` 涉及生命周期，不直接转成 Mao。转换器可以在明确要求复制的位置生成 `string(value)`；未经用户声明不得自动改变复制成本。

## 5. 可空值

Mao `T?` 与 Rust `Option<T>` 直接对应：

| Mao | Rust |
|---|---|
| `T?` | `Option<T>` |
| `null` | `None` |
| 非空 `T` 进入 `T?` | `Some(value)` |
| `value == null` | `value.is_none()` |
| `value != null` 后的收窄 | `if let Some(value) = value` 或 `match` |

例如：

```mao
int? find(bool exists) {
    if exists {
        return 3
    }
    return null
}
```

生成：

```rust
fn find(exists: bool) -> Option<isize> {
    if exists {
        return Some(3);
    }
    None
}
```

Rust 的以下 `Option` 用法不能直接反向转换：

- 返回对局部值的借用，如 `Option<&T>`。
- 包含显式生命周期参数。
- 依赖 niche layout 的内存布局代码。
- 对 `Pin`、裸指针或自引用对象的可空包装。

嵌套 `Option<Option<T>>` 具有三种状态，而 Mao 规定 `(T?)?` 归一化为 `T?`。因此嵌套 Rust `Option` 不属于可转换子集。

## 6. 集合

### 6.1 `table`

Mao `table<K,V>` 生成 `mao_rt::Table<K,V>`，不能生成 Rust `HashMap<K,V>`，原因是 Mao `table` 具有以下确定语义：

- 保持插入顺序。
- 赋值和参数传递共享状态。
- 重复键替换值但保留首次插入位置。
- 支持按插入位置访问和删除。
- 零值可以直接读写。
- 索引读取返回归一化的 `V?`。

`mao_rt::Table<K,V>` 必须在 Rust 中保持这些语义。转换器不展开其内部实现，生成代码只使用规范方法：

| Mao | Rust 规范形式 |
|---|---|
| `[]` | `mao_rt::Table::new()` |
| `[key: value]` | `mao_rt::table![(key, value)]` |
| `[value1, value2]` | `mao_rt::table![(0, value1), (1, value2)]` |
| `table[key]` | `table.index(&key)` |
| `table.get(key, fallback)` | `table.get_or(&key, || fallback)` |
| `table.has(key)` | `table.has(&key)` |
| `table.at(index)` | `table.at(index)` |
| `table[key] = value` | `table.set(key, value)` |
| `table.Delete(key)` | `table.delete(&key)` |
| `table.DeleteAt(index)` | `table.delete_at(index)` |
| `table.clear()` | `table.clear()` |
| `len(table)` | `table.len()` |
| `range table` | `table.entries()` 的插入顺序迭代 |

Rust 到 Mao 只把 `mao_rt::Table<K,V>` 和上述规范调用恢复为 Mao `table`。普通 `HashMap`、`BTreeMap`、`Vec` 和第三方有序映射不会自动解释为 Mao `table`。

### 6.2 原生数组、切片和映射类型

`PLAN.md` 将 `T[]`、`T[N]` 和 `K:V[]` 定位为互操作使用的原生集合类型。源码转换采用：

| Mao | Rust |
|---|---|
| `T[N]` | `[T; N]` |
| `T[]` | `Vec<T>` |
| `K:V[]` | `std::collections::HashMap<K,V>` |

数组可以双向转换，但 Rust 的重复数组表达式 `[value; N]` 只有在 `value` 可安全重复求值时才能转换成 Mao。

`Vec<T>` 和 `HashMap<K,V>` 只在不依赖共享底层存储的代码中双向转换。Mao 代码如果依赖切片赋值后的共享修改、容量、重新切片或追加后的底层数组关系，转换器必须拒绝转换，不能用 Rust `.clone()` 改变可观察行为。

Rust 切片借用 `&[T]` 和 `&mut [T]` 不直接对应 Mao `T[]`，因为借用期限和别名规则无法在当前 Mao 类型中表达。

### 6.3 `any`

Mao `any` 生成 `mao_rt::Any`，不直接生成 `Box<dyn std::any::Any>`。辅助类型需要保留 Mao 所需的：

- 动态类型信息。
- 赋值和参数传递语义。
- 对基础值、字符串、可空值和 Mao 运行时集合的容纳能力。
- 运行时类型断言。

Rust 到 Mao 只接受规范的 `mao_rt::Any`。任意 trait 对象不会自动转为 `any`。

## 7. 变量、常量和赋值

### 7.1 局部变量

Mao：

```mao
name := "Mao"
int count = 0
```

Rust：

```rust
let name = "Mao".to_owned();
let mut count: isize = 0;
```

Mao 没有局部不可变绑定语法。Mao 到 Rust 时，转换器通过作用域内的赋值分析决定：

- 从未重新赋值的变量生成 `let`。
- 发生重新赋值的变量生成 `let mut`。

Rust 到 Mao 时，`let` 和 `let mut` 都生成 Mao 局部变量。失去的是 Rust 编译期不可变约束，不改变程序的运行结果。再次生成 Rust 时，转换器重新执行赋值分析。

### 7.2 常量

Mao `const` 对应 Rust `const`。只有两侧都能在编译期求值的表达式可以双向转换。

Rust `static` 表示具有固定存储位置的全局对象，不等同于 Mao `const`。可变 `static`、线程局部存储和惰性全局对象不属于第一版可转换子集。

### 7.3 移动和复制

Mao 变量赋值不使源变量失效。生成 Rust 时：

- Rust `Copy` 类型直接赋值。
- `String`、结构体和其他拥有值生成 `.clone()`。
- `mao_rt::Table` 与 `mao_rt::Ref` 的 `.clone()` 保持共享对象语义。
- 当前表达式之后不再使用的临时值可以直接移动，但这属于不影响反向转换的优化。

Rust 到 Mao 时，只转换以下所有权使用：

- `Copy`。
- 显式 `.clone()`。
- 值在移动后确实不再使用的普通移动。
- `mao_rt` 规范共享类型。

依赖析构先后顺序、部分移动、移动出结构体字段或手工 `drop` 时机的 Rust 代码不自动转换。

## 8. 函数和返回值

### 8.1 普通函数

```mao
int add(int left, int right) {
    return left + right
}
```

生成：

```rust
fn add(left: isize, right: isize) -> isize {
    left + right
}
```

Rust 尾表达式转回 Mao 时生成显式 `return`，避免给 Mao 增加尾表达式语义。

### 8.2 多返回值

Mao 多返回值只在函数返回和对应接收位置转换为 Rust 元组：

```mao
(int, string) parse(string text) {
    return 1, text
}
```

生成：

```rust
fn parse(text: String) -> (isize, String) {
    (1, text)
}
```

Rust 元组只有在以下位置可以转成 Mao：

- 函数返回类型。
- `return` 表达式。
- 立即接收对应函数结果的解构绑定。

作为结构体字段、集合元素或独立变量保存的 Rust 元组不是 Mao 多返回值，第一版不转换。

### 8.3 闭包

不逃逸且只按值捕获的 Rust 闭包可以转成 Mao 函数字面量。Mao 函数字面量转 Rust 时：

- 只读捕获生成普通闭包捕获。
- 修改捕获变量时生成 `mao_rt::Cell<T>` 规范形式。
- 闭包逃逸时，所有捕获值必须具有可拥有的规范表示。

借用局部值并在局部值失效后仍可能存活的闭包、`FnOnce` 的部分移动闭包和自引用闭包不属于可转换子集。

## 9. 结构体、方法和引用

### 9.1 结构体

Mao 前置字段类型转换为 Rust 后置字段类型：

```mao
type User struct {
    string Name
    int Age
}
```

生成：

```rust
#[derive(Clone)]
pub struct User {
    pub name: String,
    pub age: isize,
}
```

字段可见性根据 Mao 导出规则生成。字段名称映射由符号表保存，不能只依赖大小写变化反向猜测。

Mao 结构体赋值具有值复制语义，因此生成的 Rust 结构体必须能够按 Mao 规则克隆。字段包含不能安全克隆的 Rust 资源时，该 Rust 结构体不能反向转换为 Mao 普通结构体。

### 9.2 方法

Mao 值接收者生成 Rust `&self` 方法，并在方法体需要 Mao 值副本时克隆接收对象。Mao 指针接收者生成 Rust `&mut self`：

```mao
func (counter *Counter) Add(int delta) int {
    counter.Value = counter.Value + delta
    return counter.Value
}
```

生成：

```rust
impl Counter {
    fn add(&mut self, delta: isize) -> isize {
        self.value += delta;
        self.value
    }
}
```

Rust 方法只有在接收者是 `self`、`&self` 或 `&mut self`，且生命周期没有进入公开签名时才直接转成 Mao 方法。

### 9.3 普通引用

Mao 指针允许别名和空值，Rust 引用要求有效、非空并遵守借用规则，两者不能直接普遍互换。

规范转换如下：

- 能够证明只在一次方法调用期间使用的 Mao 指针接收者，可以生成 `&mut self`。
- 能够证明只在一次函数调用期间只读使用的 Mao 指针参数，可以生成 `&T`。
- 其他可能逃逸、共享或为空的 Mao 指针生成 `mao_rt::Ref<T>` 或 `Option<mao_rt::Ref<T>>`。
- Rust 的 `mao_rt::Ref<T>` 恢复为 Mao 指针或包含指针语义的规范类型。
- 带显式生命周期、返回借用、内部自引用或 `Pin` 的 Rust 类型不转换。

转换器必须先完成逃逸和别名分析，不能仅根据是否出现赋值选择 `&T` 或 `&mut T`。

## 10. 接口、trait 和泛型

### 10.1 接口与 trait

满足以下条件的 Mao `interface` 可以生成 Rust `trait`：

- 只包含方法。
- 方法没有 Mao/Rust 无法对应的泛型参数。
- 方法参数和返回值均可转换。
- 不依赖 Go 特有的类型集合语义。

Mao 接口值生成 `mao_rt::Dyn<Trait>` 规范包装，不直接固定为 `Box<dyn Trait>`，以便保持 Mao 接口值的复制、空值和动态类型语义。

Rust trait 只有满足以下条件时可以转成 Mao 接口：

- 不含关联常量。
- 不含关联类型。
- 不含显式生命周期参数。
- 不使用 `Self` 作为方法返回值，除非方法受 `Self: Sized` 限制且不会进入接口对象。
- 不依赖自动 trait、负实现、特化或上转型。
- 所有方法都能转换成 Mao 方法签名。

### 10.2 实现关系

Mao 采用结构化接口满足关系，类型不显式声明实现接口。Rust 要求显式 `impl Trait for Type`。

Mao 到 Rust 时，转换器根据实际赋值、参数传递和返回位置收集需要的接口实现，并生成相应 `impl`。未被使用的潜在结构化实现不生成。

Rust 到 Mao 时删除可由方法集合推导出的 `impl Trait for Type` 声明。若 Rust `impl` 包含默认方法覆盖、关联项或与同名结构化接口不一致的行为，则拒绝转换。

### 10.3 泛型

Mao：

```mao
func Identity<T>(T value) T {
    return value
}

Box<string> value
```

Rust：

```rust
fn identity<T>(value: T) -> T {
    value
}

let value: Box<String>;
```

第一版双向支持：

- 类型参数。
- 普通 trait/interface 上界。
- 多个上界的交集。
- 可转换类型作为泛型实参。

第一版不支持：

- Rust 生命周期参数。
- const 泛型。
- 高阶 trait bound。
- 关联类型等式。
- `impl Trait`。
- Rust 特化。
- Mao 使用 `~T` 表达的底层类型约束；Rust 没有保持命名类型集合语义的直接对应。

## 11. 枚举和结果类型

### 11.1 无数据枚举

Rust 无数据枚举转换为 Mao 命名整数类型和常量：

```rust
enum Direction {
    Left,
    Right,
}
```

生成：

```mao
type Direction int

const (
    DirectionLeft Direction = 0
    DirectionRight Direction = 1
)
```

反向转换只识别转换器生成或符合相同完整模式的命名类型与常量组。普通业务常量不得自动猜测为 Rust 枚举。

### 11.2 携带数据的枚举

Mao 当前没有代数数据类型语法。Rust 携带数据的枚举使用规范接口与变体结构编码：

```rust
enum Message {
    Quit,
    Move { x: i32, y: i32 },
    Write(String),
}
```

对应 Mao：

```mao
type Message interface {
    messageVariant()
}

type MessageQuit struct {}

func (MessageQuit) messageVariant() {}

type MessageMove struct {
    int32 X
    int32 Y
}

func (MessageMove) messageVariant() {}

type MessageWrite struct {
    string Value
}

func (MessageWrite) messageVariant() {}
```

转换器同时生成完整的构造函数和类型选择访问。只有这套规范结构能够恢复为 Rust 数据枚举。

由于 Mao 接口不是封闭类型集合，其他 Mao 类型理论上也能实现标记方法。转换器必须把该接口标记为生成的封闭枚举，并在同一转换单元内检查没有额外实现；否则不能恢复为 Rust `enum`。

### 11.3 `Result`

初级目标中，Rust 包返回的 `Result<T,E>` 保持为 Rust 结果类型。Mao 可以通过类型推断使用它，也可以导入标准结果模块后显式标注：

```mao
import result "rust:std/result"

result.Result<string, ParseError> parsed = parser.parse(text)
```

生成 Rust 时恢复为 `std::result::Result<String, ParseError>`。转换器不把错误隐式改写为 panic，也不把 `Err` 改写为多返回值。

最终目标把普通 Rust `Result<T,E>` 转换为 Mao 时，使用 `mao_rt.Result<T,E>` 规范类型表示，从而在 Mao 当前语法中保留 `Ok` 和 `Err` 两个变体。

| Rust | Mao |
|---|---|
| `Ok(value)` | `mao_rt.Ok<T,E>(value)` |
| `Err(error)` | `mao_rt.Err<T,E>(error)` |
| `match result` | 对规范结果类型的类型选择 |
| `?` | 显式检查并提前返回 |

Mao 当前没有 `?` 提前返回运算符。Rust 到 Mao 时必须展开为显式控制流；Mao 再生成 Rust 时可以识别该规范控制流并恢复 `?`，但恢复不是语义正确性的必要条件。

## 12. 控制流

### 12.1 条件

`if` 和 `else` 直接双向转换。Rust `if` 表达式只有在结果立即赋给一个变量或立即返回，并且各分支类型一致时，才能展开为 Mao 语句。

Mao 条件必须是 `bool`，因此不需要引入真值转换。

### 12.2 循环

| Mao | Rust |
|---|---|
| `for {}` | `loop {}` |
| `for condition {}` | `while condition {}` |
| `for init; condition; post {}` | 初始化语句加 `while`，循环尾执行 post |
| `for value := range sequence` | `for value in sequence` |
| `for key, value := range table` | `for (key, value) in table.entries()` |
| `break` | `break` |
| `continue` | `continue` |

将三段式 Mao `for` 生成 Rust `while` 时，`continue` 必须先执行 Mao 的 post 语句，再进入下一次条件判断。转换器应通过循环尾辅助块或内部标签保持此语义。

Rust 带值 `break value`、循环表达式结果和任意迭代器适配链只有在能够展开为现有 Mao 语句且不改变惰性求值顺序时才能转换。

### 12.3 `switch` 与 `match`

Mao 表达式 `switch` 可以生成 Rust `match`，条件是：

- 每个 `case` 都能转换为不重叠的 Rust 模式或守卫。
- 不使用 `fallthrough`。
- 分支不依赖 Mao 特有的求值顺序差异。

其他 `switch` 生成 `if` / `else if` 链。

Rust `match` 可以转成 Mao：

- 常量模式生成 `case`。
- `_` 生成 `default`。
- 简单枚举模式生成类型选择或规范枚举分支。
- 守卫生成分支内条件。

切片模式、范围模式、绑定模式、`@` 模式和复杂解构必须先展开；无法保持穷尽性和绑定作用域时拒绝转换。

### 12.4 `defer`

Mao `defer` 生成作用域守卫，并保持：

- 参数在遇到 `defer` 时求值。
- 延迟调用按后进先出顺序执行。
- 普通返回和 panic 展开路径都执行。

规范 Rust 形式：

```rust
let _mao_defer_1 = mao_rt::defer(move || {
    close(resource);
});
```

多个 `defer` 通过局部守卫声明顺序保证逆序析构。若 Rust 代码手工控制 `drop` 顺序、泄漏守卫或把守卫移出当前作用域，则不能反向转换成 Mao `defer`。

### 12.5 不可直接转换的控制流

- Mao `goto` 没有安全 Rust 对应语句，第一版拒绝。
- Mao `fallthrough` 必须先由前端展开成无 `fallthrough` 的控制流，再生成 Rust。
- Rust 标签只用于循环时可以转换；任意块标签和带值跳出需要先展开。
- Rust `return`、`break` 或 `continue` 中依赖临时值析构时机的代码，需要验证析构顺序后才能转换。

## 13. 并发

Mao 的 `go`、`chan` 和 `select` 沿用 Go 语义，而 Rust 标准库没有完整等价组合。为了保持当前 Mao 语法，Mao 到 Rust 统一生成 `mao_rt` 并发类型：

| Mao | Rust 规范形式 |
|---|---|
| `chan T` | `mao_rt::Chan<T>` |
| `make(chan T, n)` | `mao_rt::Chan::buffered(n)` |
| `go call()` | `mao_rt::spawn(move || call())` |
| 发送 | `channel.send(value)` |
| 接收 | `channel.recv()` |
| `close(channel)` | `channel.close()` |
| `select` | `mao_rt::select(...)` |

运行时必须保持 Mao 对阻塞、关闭、缓冲和选择的既定语义。不得把 `select` 机械改成轮询。

Rust 到 Mao 只识别上述规范形式。以下 Rust 并发代码不属于第一版可转换子集：

- `async` / `await`。
- `Future`、任务执行器和异步 trait。
- `std::thread` 的线程本地状态或作用域线程借用。
- 第三方通道及选择宏。
- 原子内存顺序操作。
- 锁守卫跨复杂控制流的代码。

## 14. panic 与恢复

Mao `panic` 可以生成 Rust `panic!`。Mao `recover` 不能简单替换为 `catch_unwind`，因为两者的可调用位置和恢复模型不同。

第一版规则：

- 不含 `recover` 的 Mao 函数可以生成普通 Rust panic 行为。
- 含 `recover` 的 Mao 函数生成 `mao_rt::catch_panic` 规范边界。
- 恢复值使用 `mao_rt::Any` 表示。
- 转换器保持延迟调用先执行、随后观察 panic 的顺序。

Rust 到 Mao 只转换由生成器产生的 `mao_rt::catch_panic` 规范形式。任意 `catch_unwind`、自定义 panic hook、`resume_unwind` 和依赖 `UnwindSafe` 细节的代码不自动转换。

## 15. Rust 可转换子集

第一版 Rust 到 Mao 支持：

- 基础标量、`String`、`Option<T>`。
- 数组，以及不依赖借用语义的 `Vec<T>` 和 `HashMap<K,V>`。
- `mao_rt::Table<K,V>`、`mao_rt::Any`、`mao_rt::Ref<T>`。
- 普通结构体及可克隆字段。
- 无数据枚举和规范数据枚举。
- 普通函数、拥有值的参数和返回值。
- 受限方法、trait 和泛型。
- `if`、`while`、`loop`、简单 `for`、简单 `match`。
- 可展开的闭包。
- 规范 `defer`、并发和 panic 辅助形式。

第一版明确不转换：

- 显式生命周期参数和返回借用。
- `unsafe` 块、裸指针解引用和外部 ABI。
- 宏定义；宏调用只有在展开后源码可用时处理展开结果。
- `async`、`await`、`Future`。
- const 泛型、关联类型、特化和高阶 trait bound。
- 自引用类型、`Pin` 和依赖准确析构时机的类型。
- `union`。
- 内联汇编。
- 平台相关表示属性和布局推断。
- 任意第三方智能指针、集合、通道或运行时类型的自动语义猜测。

## 16. 往返转换规则

### 16.1 Mao → Rust → Mao

允许以下规范化：

- 中文或英文兼容名统一为 Mao 格式化器选择的规范名。
- 中文代码标点统一为 ASCII 标点。
- 自动数值扩宽恢复为 Mao 隐式规则。
- Rust 尾表达式恢复为显式 Mao `return`。
- 不变局部量恢复为 Mao 普通局部变量。
- 编译器生成的临时变量重新命名。
- `switch` 与等价 `if` 链之间转换。

不得发生：

- `table` 变成无序映射。
- `null` 与零值合并。
- 共享对象变成独立副本。
- panic、`defer` 或循环 post 语句的顺序变化。
- 整数宽度、符号或溢出行为变化。

### 16.2 Rust → Mao → Rust

只保证可转换子集内的运行语义。以下 Rust 静态约束可能不在 Mao 文本中直接保留，但再次生成 Rust 时重新推导：

- 局部变量是否需要 `mut`。
- 可省略的显式 `return`。
- 能够安全使用的临时借用。
- 能够安全移动而无需克隆的临时值。

Rust 的公开类型、函数签名、枚举变体、错误分支和可观察控制流必须保持。

## 17. 诊断要求

转换失败时必须报告：

- 源文件和准确位置。
- 无法转换的源语言结构。
- 目标语言缺少的表达能力。
- 如果可以先进行等价重构，说明必须满足的语义条件。

示例：

```text
user.rs:18:9：不能把返回借用 &'a str 转换为 Mao。
当前 Mao 类型系统不能表示返回值与参数 'a 的生命周期关系。
请先把返回类型改为拥有所有权的 String；只有在该复制符合原接口语义时才能继续转换。
```

不得在以下情况下自动处理：

- 根据方法名推测容器语义。
- 根据字段名推测所有权。
- 把编译失败解释为需要 `.clone()` 并无条件插入。
- 把所有 trait 对象统一改成 `any`。
- 把所有 Rust 错误统一改成 Mao panic。

## 18. 实施顺序

### 阶段一：Mao 导入并调用 Rust 包

- 实现 `rust:crate/module` 导入路径。
- 读取并绑定 Rust crate 的公开模块、类型、函数、关联函数、方法和常量。
- 支持基础类型、字符串、`Option<T>`、`Result<T,E>` 和普通结构体。
- 根据 Rust 签名处理按值参数、`&T` 和 `&mut T`。
- 实现 Rust 关联函数、实例方法和普通泛型函数调用。
- 拒绝返回借用、`unsafe`、异步函数和无法表达的泛型约束。

完成标准：Mao 示例可以导入真实 Rust crate，调用其公开 API，并生成能够通过 Rust 语法解析和类型检查的源码；所有权不明确的调用产生准确诊断。

### 阶段二：完整 Mao → Rust 源码转换

- 变量、常量、赋值和数值转换。
- 普通函数、返回值和多返回值。
- `if`、循环、`switch` 和基础闭包。
- 结构体、方法和可见性映射。
- `table` 与 `mao_rt::Table`。
- 数组以及受限原生集合。
- 值复制、克隆、共享状态和逃逸分析。
- Mao 本地声明与 Rust 包类型混合使用。

完成标准：不含明确排除特性的 Mao 包可以整体生成合法 Rust；结构体方法、字符串复制、`table` 别名修改、插入顺序、可空读取和 Rust 包调用保持预期行为。

### 阶段三：正向语义补全与规范化

- Mao 接口与 Rust trait。
- 受限泛型上界。
- 无数据枚举。
- 数据枚举规范编码。
- `Result<T,E>` 与显式错误控制流。
- 闭包捕获、`defer` 和 panic。
- `go`、channel 和 `select` 的规范运行时形式。
- 固定所有为最终反向转换保留的 Rust 规范形式。

完成标准：Mao 到 Rust 的支持边界稳定；相同 Mao 输入重复生成结构一致的 Rust 抽象语法树；接口、泛型、错误传播和复杂控制流具有行为测试。

### 阶段四：Rust → Mao

- 解析普通 Rust 源码和阶段三确定的规范 Rust 输出。
- 转换拥有值、无显式生命周期、无 `unsafe`、无异步状态机的 Rust 子集。
- 转换结构体、函数、方法、`Option`、`Result`、简单 trait、泛型和枚举。
- 把 Rust 借用、移动和克隆关系验证为 Mao 可以保持的语义。
- 建立 Mao → Rust → Mao 和 Rust → Mao → Rust 往返测试。

完成标准：受支持 Rust 子集能够生成合法 Mao 源码；生成 Mao 通过 Mao 解析和类型检查；往返后公开接口、求值顺序、别名关系、错误分支和运行结果一致。

## 19. 验收条件

### 19.1 正向转换

- Mao 能用 `rust:` 导入路径引用 Rust crate 和公开模块。
- 包函数、关联函数、实例方法、公开常量和普通泛型调用绑定到正确 Rust 符号。
- `&T`、`&mut T`、按值移动和必要克隆符合 Rust 签名。
- 返回借用、`unsafe`、异步函数和无法表达的约束在 Mao 源位置产生诊断。
- Mao 基础类型生成确定的 Rust 类型。
- Mao 自动扩宽生成无损 Rust 显式转换。
- `T?`、`null` 和类型收窄生成 `Option<T>` 语义。
- `table` 的顺序、共享状态、缺失键和空值保持。
- Mao 变量复制不会因 Rust move 使源变量失效。
- 多返回值只在允许的位置生成元组。
- 结构体、方法、接口和泛型的公开行为保持。
- `defer` 参数求值时机和逆序执行保持。
- 三段式循环中的 `continue` 正确执行 post 语句。
- `goto` 等无法保持的语法产生明确错误。

### 19.2 反向转换

- Rust 拥有值、`Option`、普通结构体和函数正确生成 Mao。
- Rust `let` 与 `let mut` 的运行行为保持。
- 移动、克隆和析构相关代码只在规则明确时转换。
- Rust 元组不会被错误当成任意 Mao 多返回值。
- 普通 `HashMap` 不会被错误当成 Mao `table`。
- trait 的关联类型、生命周期和对象安全问题被拒绝。
- 数据枚举只通过规范封闭编码转换。
- `?` 展开为显式 Mao 错误控制流。
- `async`、`unsafe` 和返回借用产生准确诊断。

### 19.3 往返一致性

对每个支持特性建立三类测试：

1. 源码结构测试：检查生成的抽象语法树和规范形式。
2. 行为测试：使用相同输入比较转换前后的结果、状态变化和错误。
3. 往返测试：执行 A → B → A，并比较规范化后的公开接口和行为。

文本相同不作为验收标准；公开类型、求值顺序、别名关系、错误分支和运行结果一致才构成通过。

## 20. 第一版结论

现有 Mao 语法可以先实现对 Rust 包的直接使用和 Mao 到 Rust 的源码转换，再以规范化的 Rust 输出为基础实现反向转换。

初级目标的核心是：

- 使用 `import alias "rust:crate/module"` 引用 Rust 包，不增加 Mao 关键字。
- 解析 Rust 的真实公开接口，使 Mao 的成员访问和调用绑定到确定符号。
- 根据 Rust 签名自动处理当前调用所需的借用、移动和克隆，并拒绝当前 Mao 类型系统不能表达的返回借用。
- 把前置类型、可空值、函数、结构体和控制流转换为 Rust 对应语法。
- 用 `mao_rt::Table`、`mao_rt::Any`、`mao_rt::Ref` 等少量规范类型保存 Rust 原生类型无法直接表达的 Mao 语义。
- 生成能够通过 Rust 解析和类型检查的合法源码。

最终目标的核心是：

- 复用初级目标已经固定的类型映射和规范 Rust 形式。
- 接受拥有值、无显式生命周期、无 `unsafe`、无异步状态机的确定子集。
- 把 `Option`、结构体、函数、简单 trait、泛型和枚举转换为现有 Mao 语法或规范编码。
- 对返回借用、关联类型、自引用、复杂析构和其他无法表达的 Rust 语义明确报错。

这一边界能够保持现有 Mao 语法，同时使“双向转换”具有可以实现和验证的语义定义。
