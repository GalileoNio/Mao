//go:build mao

package mixed

import "testing"

func TestGoCallsMao(t *testing.T) {
	values := Values()
	if values.Len() != 2 || values.Get("dog", 0) != 5 {
		t.Fatalf("unexpected Mao table: %#v", values.Keys())
	}
}
