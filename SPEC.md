# Mao 语言规范

本文件定义 Mao 第一版的稳定语言边界。完整语义示例和设计依据见 [PLAN.md](PLAN.md)。

## 编译模型

Mao 是编译到 Go 的静态类型语言。`.mao` 文件经过词法分析、Mao 抽象语法树、类型检查和 Go 抽象语法树生成，再交给官方 Go 工具链。Mao 不提供独立虚拟机、包管理器、垃圾回收器或 Go 编译器分支。

## 声明与类型

- 局部推断声明使用 `name := value`。
- 显式声明使用 `Type name = value`，不要求 `var`；`var Type name` 也可用于声明组。
- 常量使用 `const name = value`。
- `float` 对应 Go `float64`。
- 切片、数组、原生映射、泛型实例和可空类型分别写作 `T[]`、`T[N]`、`K:V[]`、`Generic<T>` 和 `T?`。
- 函数参数、结构字段和具名返回值均使用类型前置形式。

## `table`

`table<K,V>` 是 Mao 唯一的语言级集合。所有 `[...]` 字面量都生成 `table`：

```mao
numbers := [1, 2, 3]
settings := ["width": 800, "theme": "dark"]
nullable := ["missing":, "present": 0]
```

裸元素依次获得整数键 `0`、`1`、`2`。重复键替换值但保留首次插入位置。`table` 支持 `get`、`has`、`at`、`keys`、`values`、`clear`、`Delete`、`DeleteAt`、索引读写、`len` 和稳定顺序遍历。

## `null`

`null` 替换 Mao 源码中的预声明空值 `nil`。`T?` 使用 `Optional[T]` 区分 `null` 与 `T` 的零值；指针、切片、原生映射、通道、函数和接口则生成 Go `nil`。`table` 索引读取返回归一化后的 `V?`。

## Go 互操作

Mao 可以导入 Go 包并调用导出 API。`table(goCollection)` 从 Go 数组、切片或映射构造独立 `table`；显式切片或数组目标、`values(...)` 和 `map(...)` 负责转换回 Go 集合。所有转换都会复制集合。

`.mao` 与 `.go` 可以位于同一包。`mao build`、`mao run`、`mao test`、`mao check` 和 `mao emit-go` 在临时目录生成 Go 源码并调用官方 Go 工具链；`mao fmt` 校验 Mao 语法并规范文件末尾、行尾空白。包命令接受 Go 风格的 `...` 递归模式。生成代码通过 `//line` 指令把 Go 诊断和运行时堆栈映射到 `.mao` 文件。

生成代码自动导入 `github.com/GalileoNio/Mao/runtime`。这是 Mao 导出 `Table` 与 `Optional` 时面向 Go 调用方的公开兼容边界；Mao 源文件本身不需要手动导入该包。

## 暂缓项

按照当前开发指令，中文代码标点、中文引号和中文语言方案不属于当前实现；字符串内容可以包含 Unicode。
