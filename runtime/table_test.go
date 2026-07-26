package runtime

import (
	"math"
	"testing"
)

func TestTableOrderAndDeletion(t *testing.T) {
	table := NewTable[int, string]()
	table.Set(0, "first")
	table.Set(1, "second")
	table.Set(2, "third")
	table.Set(1, "updated")

	if got := table.Keys(); len(got) != 3 || got[0] != 0 || got[1] != 1 || got[2] != 2 {
		t.Fatalf("unexpected keys: %v", got)
	}
	if got := table.Get(1, "missing"); got != "updated" {
		t.Fatalf("unexpected updated value: %q", got)
	}

	alias := table
	alias.DeleteAt(1)
	if table.Len() != 2 || table.At(1).Key != 2 {
		t.Fatalf("deletion did not preserve shared order: %#v", table.Keys())
	}
}

func TestTableRejectsNonReflexiveKey(t *testing.T) {
	table := NewTable[float64, int]()
	defer func() {
		if recover() == nil {
			t.Fatal("expected NaN key to panic")
		}
	}()
	table.Set(math.NaN(), 1)
}

func TestOptionalDistinguishesZeroAndNull(t *testing.T) {
	zero := Some(0)
	if value, ok := zero.Get(); !ok || value != 0 {
		t.Fatalf("zero was not preserved: %v %v", value, ok)
	}
	if !Null[int]().IsNull() {
		t.Fatal("null optional reported a value")
	}
}

func TestGetLazyOnlyEvaluatesMissingFallback(t *testing.T) {
	table := NewTable[string, int]()
	table.Set("present", 7)
	calls := 0
	fallback := func() int {
		calls++
		return 9
	}
	if got := table.GetLazy("present", fallback); got != 7 || calls != 0 {
		t.Fatalf("present lookup evaluated fallback: value=%d calls=%d", got, calls)
	}
	if got := table.GetLazy("missing", fallback); got != 9 || calls != 1 {
		t.Fatalf("missing lookup did not evaluate fallback once: value=%d calls=%d", got, calls)
	}
}

func TestCollectionConversionsAreIndependent(t *testing.T) {
	source := []int{3, 3, 5}
	table := FromSlice(source)
	source[0] = 99
	if got := table.Get(0, 0); got != 3 {
		t.Fatalf("slice mutation affected table: %d", got)
	}

	native := ToMap(table)
	native[0] = 42
	if got := table.Get(0, 0); got != 3 {
		t.Fatalf("map mutation affected table: %d", got)
	}
}

func TestOptionalConversionsPreserveOrReplaceNull(t *testing.T) {
	table := NewTable[string, Optional[int]]()
	table.Set("empty", Null[int]())
	table.Set("zero", Some(0))

	values := OptionalValues(table)
	if values[0] != nil || values[1] != 0 {
		t.Fatalf("unexpected preserved values: %#v", values)
	}
	filled := OptionalMapOr(table, 8)
	if filled["empty"] != 8 || filled["zero"] != 0 {
		t.Fatalf("unexpected filled map: %#v", filled)
	}
}

func TestDuplicateKeyKeepsFirstPosition(t *testing.T) {
	table := NewTable[any, any]()
	table.Set(0, "implicit")
	table.Set("name", "first")
	table.Set(0, "replacement")
	table.Set("name", "second")

	if table.Len() != 2 {
		t.Fatalf("duplicate keys changed length: %d", table.Len())
	}
	if first := table.At(0); first.Key != 0 || first.Value != "replacement" {
		t.Fatalf("first key moved or retained old value: %#v", first)
	}
	if second := table.At(1); second.Key != "name" || second.Value != "second" {
		t.Fatalf("second key moved or retained old value: %#v", second)
	}
}

func TestMissingAndNullRemainDistinguishable(t *testing.T) {
	table := NewTable[string, Optional[int]]()
	table.Set("empty", Null[int]())
	if !table.Has("empty") || table.Has("missing") {
		t.Fatal("Has did not distinguish present and missing keys")
	}
	if !Flatten(table.Index("empty")).IsNull() || !Flatten(table.Index("missing")).IsNull() {
		t.Fatal("index lookup did not normalize null values")
	}
	if fallback := table.Get("empty", Some(9)); !fallback.IsNull() {
		t.Fatal("Get replaced an existing null value")
	}
}

func TestDeleteAndClearMaintainOrder(t *testing.T) {
	table := NewTable[int, int]()
	for index := 0; index < 4; index++ {
		table.Set(index, index)
	}
	table.Delete(1)
	table.Delete(99)
	table.DeleteAt(1)
	if got := table.Keys(); len(got) != 2 || got[0] != 0 || got[1] != 3 {
		t.Fatalf("unexpected keys after deletion: %v", got)
	}
	table.Clear()
	if table.Len() != 0 || len(table.Keys()) != 0 {
		t.Fatal("Clear did not empty the table")
	}
}

func TestZeroTableCanBeInitialized(t *testing.T) {
	var table Table[string, int]
	table.Set("answer", 42)
	alias := table
	alias.Set("second", 2)
	if table.Get("second", 0) != 2 {
		t.Fatal("initialized zero table did not retain reference semantics")
	}
}

func TestAnyRejectsDynamicallyUncomparableKey(t *testing.T) {
	table := NewTable[any, int]()
	defer func() {
		if recover() == nil {
			t.Fatal("expected slice key to panic")
		}
	}()
	table.Set([]int{1}, 1)
}

func TestCompositeNaNKeyIsRejected(t *testing.T) {
	type key struct{ Value float64 }
	table := NewTable[key, int]()
	defer func() {
		if recover() == nil {
			t.Fatal("expected composite NaN key to panic")
		}
	}()
	table.Set(key{Value: math.NaN()}, 1)
}

func TestAtAndDeleteAtBounds(t *testing.T) {
	table := NewTable[int, string]()
	table.Set(0, "first")
	table.Set(1, "last")
	if table.At(0).Value != "first" || table.At(1).Value != "last" {
		t.Fatalf("At did not return boundary entries: %#v", table.Values())
	}

	for _, operation := range []struct {
		name string
		run  func()
	}{
		{"negative At", func() { table.At(-1) }},
		{"past-end At", func() { table.At(2) }},
		{"negative DeleteAt", func() { table.DeleteAt(-1) }},
		{"past-end DeleteAt", func() { table.DeleteAt(2) }},
		{"empty At", func() { NewTable[int, int]().At(0) }},
		{"empty DeleteAt", func() { value := NewTable[int, int](); value.DeleteAt(0) }},
	} {
		t.Run(operation.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected table index panic")
				}
			}()
			operation.run()
		})
	}
}

func TestTableParameterSharesState(t *testing.T) {
	table := NewTable[string, int]()
	table.Set("first", 1)
	update := func(alias Table[string, int]) {
		alias.Set("second", 2)
		alias.Delete("first")
	}
	update(table)
	if table.Has("first") || table.Get("second", 0) != 2 {
		t.Fatalf("parameter did not share table state: %#v", table.Keys())
	}
}

func BenchmarkTableLookup(b *testing.B) {
	table := NewTable[int, int]()
	for index := 0; index < 1_000; index++ {
		table.Set(index, index)
	}
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		table.Lookup(index % 1_000)
	}
}
