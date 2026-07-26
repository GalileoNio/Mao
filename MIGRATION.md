# 从 Go 迁移到 Mao

Go 包结构、导入路径、导出命名和官方工具链保持不变。迁移时只修改 Mao 明确替换的语法。

| Go | Mao |
|---|---|
| `var age int = 3` | `int age = 3` |
| `var names []string` | `string[] names` |
| `var scores [3]int` | `int[3] scores` |
| `var ages map[string]int` | `string:int[] ages` |
| `Box[string]` | `Box<string>` |
| `float64` | `float` 或 `float64` |
| `nil` | `null` |
| 数组、切片、映射字面量 | Mao `table` 字面量 `[...]` |
| `delete(values, key)` | `values.Delete(key)` |
| `clear(values)` | `values.clear()` |
| `value, ok := values[key]` | `value := values[key]` 与 `values.has(key)` |

Go 原生集合只用于 API 互操作：

```mao
values := [1, 2, 3]
int[] nativeValues = values
roundTrip := table(nativeValues)

settings := ["width": 800]
string:int[] nativeSettings = map(settings)
```

同一目录可以同时包含 `.mao` 和 `.go` 文件。使用 `mao build`、`mao test` 或 `mao check` 生成临时 Go 源码并调用官方工具链；生成文件不会写入源码目录。
