package mixed

import "testing"

func TestMaoCallsGo(*testing.T test) {
	values := Values()
	if values.get("cat", 0) != 3 {
		test.Fatalf("unexpected value: %v", values.values())
	}
}
