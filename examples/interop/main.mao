package main

import "fmt"

func sum(table<int, int> values) int {
	total := 0
	for _, value := range values {
		total = total + value
	}
	return total
}

func main() {
	values := [2, 3, 5]
	int[] nativeValues = values
	roundTrip := table(nativeValues)

	settings := ["width": 800, "height": 600]
	string:int[] nativeSettings = map(settings)
	settingsAgain := table(nativeSettings)

	missing := values[99]
	if missing == null {
		fmt.Println(sum(roundTrip), settingsAgain.get("width", 0))
	}

	table<int, int32> small = [1, 2]
	int64[] widened = small
	int64[2] fixed = small
	int:int64[] widenedMap = map(small)
	fmt.Println(widened, fixed, widenedMap[0])

	nullable := ["empty": null, "zero": 0]
	any[] preserved = nullable.values()
	int[] filled = nullable.values(9)
	string:any[] preservedMap = map(nullable)
	string:int[] filledMap = map(nullable, 9)
	fmt.Println(preserved, filled, preservedMap["empty"], filledMap["empty"])

	calls := 0
	fallback := func() int {
		calls++
		return 9
	}
	present := values.get(0, fallback())
	fmt.Println(present, calls)
	missingValue := values.get(99, fallback())
	fmt.Println(missingValue, calls)
}
