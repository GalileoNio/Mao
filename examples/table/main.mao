package main

import "fmt"

func main() {
	values := [1, 1, 2]
	values[3] = 4
	values.DeleteAt(1)

	settings := ["theme": "dark", "retries": 3]

	fmt.Println(values.keys(), values.values())
	fmt.Println(settings.get("theme", "light"))
}
