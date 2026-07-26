package runtime

import "fmt"

const Version = "0.1.0-dev"

// Optional represents a Mao value that can be null.
type Optional[T any] struct {
	value   T
	present bool
}

func Some[T any](value T) Optional[T] {
	return Optional[T]{value: value, present: true}
}

func Null[T any]() Optional[T] {
	return Optional[T]{}
}

func Flatten[T any](value Optional[Optional[T]]) Optional[T] {
	if nested, present := value.Get(); present {
		return nested
	}
	return Null[T]()
}

func (value Optional[T]) IsNull() bool {
	return !value.present
}

func (value Optional[T]) Get() (T, bool) {
	return value.value, value.present
}

func (value Optional[T]) Value() T {
	if !value.present {
		panic("mao null optional has no value")
	}
	return value.value
}

func (value Optional[T]) Or(fallback T) T {
	if value.present {
		return value.value
	}
	return fallback
}

func (value Optional[T]) String() string {
	if !value.present {
		return "null"
	}
	return fmt.Sprint(value.value)
}

type Entry[K comparable, V any] struct {
	Key   K
	Value V
}

type tableState[K comparable, V any] struct {
	entries []Entry[K, V]
	index   map[K]int
}

// Table is Mao's ordered associative collection.
type Table[K comparable, V any] struct {
	state *tableState[K, V]
}

func NewTable[K comparable, V any]() Table[K, V] {
	return Table[K, V]{
		state: &tableState[K, V]{index: make(map[K]int)},
	}
}

func (table *Table[K, V]) ensure() *tableState[K, V] {
	if table.state == nil {
		table.state = &tableState[K, V]{index: make(map[K]int)}
	}
	return table.state
}

func (table Table[K, V]) Len() int {
	if table.state == nil {
		return 0
	}
	return len(table.state.entries)
}

func (table *Table[K, V]) Set(key K, value V) {
	state := table.ensure()
	if !validKey(key) {
		panic("mao table key must be comparable and equal to itself")
	}
	if index, exists := state.index[key]; exists {
		state.entries[index].Value = value
		return
	}
	state.index[key] = len(state.entries)
	state.entries = append(state.entries, Entry[K, V]{Key: key, Value: value})
}

func (table Table[K, V]) Lookup(key K) (V, bool) {
	if table.state != nil {
		if index, exists := table.state.index[key]; exists {
			return table.state.entries[index].Value, true
		}
	}
	var zero V
	return zero, false
}

func (table Table[K, V]) Index(key K) Optional[V] {
	if value, exists := table.Lookup(key); exists {
		return Some(value)
	}
	return Null[V]()
}

func (table Table[K, V]) Get(key K, fallback V) V {
	if value, exists := table.Lookup(key); exists {
		return value
	}
	return fallback
}

func (table Table[K, V]) GetLazy(key K, fallback func() V) V {
	if value, exists := table.Lookup(key); exists {
		return value
	}
	return fallback()
}

func (table Table[K, V]) Has(key K) bool {
	if table.state == nil {
		return false
	}
	_, exists := table.state.index[key]
	return exists
}

func (table Table[K, V]) At(index int) Entry[K, V] {
	if table.state == nil || index < 0 || index >= len(table.state.entries) {
		panic("mao table index out of range")
	}
	return table.state.entries[index]
}

func (table *Table[K, V]) Delete(key K) {
	if table.state == nil {
		return
	}
	index, exists := table.state.index[key]
	if !exists {
		return
	}
	table.deleteAt(index)
}

func (table *Table[K, V]) DeleteAt(index int) {
	if table.state == nil || index < 0 || index >= len(table.state.entries) {
		panic("mao table index out of range")
	}
	table.deleteAt(index)
}

func (table *Table[K, V]) deleteAt(index int) {
	state := table.state
	delete(state.index, state.entries[index].Key)
	copy(state.entries[index:], state.entries[index+1:])
	var zero Entry[K, V]
	state.entries[len(state.entries)-1] = zero
	state.entries = state.entries[:len(state.entries)-1]

	// ponytail: O(n) index repair keeps At and ordered iteration simple.
	// Replace entries with linked nodes only if deletion profiles as a bottleneck.
	for current := index; current < len(state.entries); current++ {
		state.index[state.entries[current].Key] = current
	}
}

func (table *Table[K, V]) Clear() {
	if table.state == nil {
		return
	}
	clear(table.state.index)
	clear(table.state.entries)
	table.state.entries = table.state.entries[:0]
}

func (table Table[K, V]) Keys() []K {
	keys := make([]K, table.Len())
	if table.state != nil {
		for index, entry := range table.state.entries {
			keys[index] = entry.Key
		}
	}
	return keys
}

func (table Table[K, V]) Values() []V {
	values := make([]V, table.Len())
	if table.state != nil {
		for index, entry := range table.state.entries {
			values[index] = entry.Value
		}
	}
	return values
}

func (table Table[K, V]) Range(yield func(K, V) bool) {
	if table.state == nil {
		return
	}
	for _, entry := range table.state.entries {
		if !yield(entry.Key, entry.Value) {
			return
		}
	}
}

func FromSlice[V any](values []V) Table[int, V] {
	result := NewTable[int, V]()
	for index, value := range values {
		result.Set(index, value)
	}
	return result
}

func FromArray[V any](length int, valueAt func(int) V) Table[int, V] {
	result := NewTable[int, V]()
	for index := 0; index < length; index++ {
		result.Set(index, valueAt(index))
	}
	return result
}

func FromMap[K comparable, V any](values map[K]V) Table[K, V] {
	result := NewTable[K, V]()
	for key, value := range values {
		result.Set(key, value)
	}
	return result
}

func ToMap[K comparable, V any](table Table[K, V]) map[K]V {
	result := make(map[K]V, table.Len())
	table.Range(func(key K, value V) bool {
		result[key] = value
		return true
	})
	return result
}

func ConvertValues[K comparable, V, W any](table Table[K, V], convert func(V) W) []W {
	result := make([]W, 0, table.Len())
	table.Range(func(_ K, value V) bool {
		result = append(result, convert(value))
		return true
	})
	return result
}

func ConvertMap[K comparable, V, W any](table Table[K, V], convert func(V) W) map[K]W {
	result := make(map[K]W, table.Len())
	table.Range(func(key K, value V) bool {
		result[key] = convert(value)
		return true
	})
	return result
}

func OptionalValues[K comparable, V any](table Table[K, Optional[V]]) []any {
	result := make([]any, 0, table.Len())
	table.Range(func(_ K, value Optional[V]) bool {
		if item, present := value.Get(); present {
			result = append(result, item)
		} else {
			result = append(result, nil)
		}
		return true
	})
	return result
}

func OptionalValuesOr[K comparable, V any](table Table[K, Optional[V]], fallback V) []V {
	result := make([]V, 0, table.Len())
	table.Range(func(_ K, value Optional[V]) bool {
		result = append(result, value.Or(fallback))
		return true
	})
	return result
}

func OptionalMap[K comparable, V any](table Table[K, Optional[V]]) map[K]any {
	result := make(map[K]any, table.Len())
	table.Range(func(key K, value Optional[V]) bool {
		if item, present := value.Get(); present {
			result[key] = item
		} else {
			result[key] = nil
		}
		return true
	})
	return result
}

func OptionalMapOr[K comparable, V any](table Table[K, Optional[V]], fallback V) map[K]V {
	result := make(map[K]V, table.Len())
	table.Range(func(key K, value Optional[V]) bool {
		result[key] = value.Or(fallback)
		return true
	})
	return result
}

func validKey[K comparable](key K) (valid bool) {
	defer func() {
		if recover() != nil {
			valid = false
		}
	}()
	return key == key
}
