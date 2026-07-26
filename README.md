# Mao

Mao 是一门编译到 Go 的静态类型语言方言。编译器生成 Go 抽象语法树，并由官方 Go 工具链完成构建、运行和测试。

当前已经可以：

- 使用 `:=` 推断局部变量类型；
- 使用 `float` 表示 Go `float64`；
- 使用 `[value, ...]` 和 `[key: value, ...]` 构造保持插入顺序的 `table`；
- 在同一 `table` 中混合键类型或值类型，并推断为 `any`；
- 使用 `key:` 或 `key: null` 保存可空值；
- 使用 `get`、`has`、`at`、`keys`、`values`、`clear`、`Delete`、`DeleteAt` 和 `len`；
- 使用函数、方法、泛型、结构体、接口、指针、通道、控制流和包级声明；
- 在 `table` 与 Go 数组、切片和 `map` 之间执行显式复制转换；
- 在同一包中混合 `.mao` 与 `.go` 文件；
- 生成格式化的 Go 源码并调用官方 Go 工具链构建、检查、运行或测试。

命令示例：

```sh
go run ./cmd/mao emit-go examples/table/main.mao
go run ./cmd/mao check "./examples/..."
go run ./cmd/mao build "./examples/..."
go run ./cmd/mao run ./examples/interop
go run ./cmd/mao test ./examples/mixed
go run ./cmd/mao fmt ./examples
```

`build`、`check` 和 `test` 接受文件、目录及 Go 风格的 `...` 递归包模式。编译器在系统临时目录生成 Go 文件，并通过 `//line` 指令将 Go 编译错误和运行时堆栈指回 `.mao` 源文件。

语言规范见 [SPEC.md](SPEC.md)，Go 迁移对照见 [MIGRATION.md](MIGRATION.md)，实现计划和验收规则见 [PLAN.md](PLAN.md)。

按照当前开发范围，中文关键字、中文标点和中文引号尚未进入词法分析器；字符串内容仍然可以使用 Unicode。
