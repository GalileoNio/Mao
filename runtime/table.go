package runtime

import "fmt"

const Version = "0.2.0-dev"

// === Optional ================================================================

type Optional[T any] struct {
	value   T
	present bool
}

func Some[T any](value T) Optional[T]            { return Optional[T]{value: value, present: true} }
func Null[T any]() Optional[T]                    { return Optional[T]{} }
func (v Optional[T]) IsNull() bool                { return !v.present }
func (v Optional[T]) Get() (T, bool)              { return v.value, v.present }
func (v Optional[T]) Value() T                     { if !v.present { panic("mao null optional has no value") }; return v.value }
func (v Optional[T]) Or(fallback T) T              { if v.present { return v.value }; return fallback }
func (v Optional[T]) String() string               { if !v.present { return "null" }; return fmt.Sprint(v.value) }

func Flatten[T any](value Optional[Optional[T]]) Optional[T] {
	if nested, present := value.Get(); present { return nested }
	return Null[T]()
}

// === Entry ==================================================================

type Entry[K comparable, V any] struct {
	Key   K
	Value V
}

// === Internal node ==========================================================

type node[K comparable, V any] struct {
	entry Entry[K, V]
	prev  *node[K, V]
	next  *node[K, V]
}

// === Table State ============================================================

type tableState[K comparable, V any] struct {
	head  *node[K, V]
	tail  *node[K, V]
	index map[K]*node[K, V]
	count int
}

// === Table ==================================================================

// Table is Mao's ordered, reference-semantics associative collection.
// Copies share the same underlying state (like a Go slice header).
type Table[K comparable, V any] struct {
	state *tableState[K, V]
}

func NewTable[K comparable, V any]() Table[K, V] {
	return Table[K, V]{state: &tableState[K, V]{index: make(map[K]*node[K, V])}}
}

func (t *Table[K, V]) s() *tableState[K, V] {
	if t.state == nil {
		t.state = &tableState[K, V]{index: make(map[K]*node[K, V])}
	}
	return t.state
}

func (t Table[K, V]) Len() int {
	if t.state == nil { return 0 }
	return t.state.count
}

func (t *Table[K, V]) Set(key K, value V) {
	s := t.s()
	if !validKey(key) {
		panic("mao table key must be self-equal; NaN-like keys rejected")
	}
	if existing, ok := s.index[key]; ok {
		existing.entry.Value = value
		return
	}
	n := &node[K, V]{entry: Entry[K, V]{Key: key, Value: value}}
	if s.tail != nil {
		s.tail.next = n
		n.prev = s.tail
		s.tail = n
	} else {
		s.head = n
		s.tail = n
	}
	s.index[key] = n
	s.count++
}

func (t Table[K, V]) Lookup(key K) (V, bool) {
	if t.state == nil { var z V; return z, false }
	if n, ok := t.state.index[key]; ok { return n.entry.Value, true }
	var z V
	return z, false
}

func (t Table[K, V]) Index(key K) Optional[V] {
	if v, ok := t.Lookup(key); ok { return Some(v) }
	return Null[V]()
}

func (t Table[K, V]) Get(key K, fallback V) V {
	if v, ok := t.Lookup(key); ok { return v }
	return fallback
}

func (t Table[K, V]) GetLazy(key K, fallback func() V) V {
	if v, ok := t.Lookup(key); ok { return v }
	return fallback()
}

func (t Table[K, V]) Has(key K) bool {
	if t.state == nil { return false }
	_, ok := t.state.index[key]
	return ok
}

func (t Table[K, V]) At(index int) Entry[K, V] {
	if t.state == nil || index < 0 || index >= t.state.count {
		panic("mao table index out of range")
	}
	cur := t.state.head
	for i := 0; i < index; i++ { cur = cur.next }
	return cur.entry
}

func (t *Table[K, V]) Delete(key K) {
	if t.state == nil { return }
	s := t.state
	n, ok := s.index[key]
	if !ok { return }
	unlinkNode(s, n)
	delete(s.index, key)
	s.count--
}

func (t *Table[K, V]) DeleteAt(index int) {
	if t.state == nil || index < 0 || index >= t.state.count {
		panic("mao table index out of range")
	}
	s := t.state
	cur := s.head
	for i := 0; i < index; i++ { cur = cur.next }
	unlinkNode(s, cur)
	delete(s.index, cur.entry.Key)
	s.count--
}

func unlinkNode[K comparable, V any](s *tableState[K, V], n *node[K, V]) {
	if n.prev != nil { n.prev.next = n.next } else { s.head = n.next }
	if n.next != nil { n.next.prev = n.prev } else { s.tail = n.prev }
}

func (t *Table[K, V]) Clear() {
	if t.state == nil { return }
	t.state.head = nil
	t.state.tail = nil
	clear(t.state.index)
	t.state.count = 0
}

func (t Table[K, V]) Keys() []K {
	if t.state == nil { return nil }
	keys := make([]K, 0, t.state.count)
	for cur := t.state.head; cur != nil; cur = cur.next {
		keys = append(keys, cur.entry.Key)
	}
	return keys
}

func (t Table[K, V]) Values() []V {
	if t.state == nil { return nil }
	vals := make([]V, 0, t.state.count)
	for cur := t.state.head; cur != nil; cur = cur.next {
		vals = append(vals, cur.entry.Value)
	}
	return vals
}

func (t Table[K, V]) Range(yield func(K, V) bool) {
	if t.state == nil { return }
	for cur := t.state.head; cur != nil; cur = cur.next {
		if !yield(cur.entry.Key, cur.entry.Value) { return }
	}
}

// === Explicit copy API ======================================================

func (t Table[K, V]) CopyToSlice() []V     { return t.Values() }

func (t Table[K, V]) CopyToArray(length int, alloc func(int) []V) []V {
	if t.Len() != length { panic("mao table length does not match array length") }
	result := alloc(length)
	i := 0
	for cur := t.state.head; cur != nil; cur = cur.next {
		result[i] = cur.entry.Value
		i++
	}
	return result
}

func (t Table[K, V]) CopyToMap() map[K]V {
	result := make(map[K]V, t.Len())
	for cur := t.state.head; cur != nil; cur = cur.next {
		result[cur.entry.Key] = cur.entry.Value
	}
	return result
}

// === Slice/Map conversion helpers ===========================================

func FromSlice[V any](values []V) Table[int, V] {
	result := NewTable[int, V]()
	for i, v := range values { result.Set(i, v) }
	return result
}

func FromArray[V any](length int, valueAt func(int) V) Table[int, V] {
	result := NewTable[int, V]()
	for i := 0; i < length; i++ { result.Set(i, valueAt(i)) }
	return result
}

func FromMap[K comparable, V any](values map[K]V) Table[K, V] {
	result := NewTable[K, V]()
	for k, v := range values { result.Set(k, v) }
	return result
}

func ToMap[K comparable, V any](t Table[K, V]) map[K]V { return t.CopyToMap() }

func ConvertValues[K comparable, V, W any](t Table[K, V], convert func(V) W) []W {
	result := make([]W, 0, t.Len())
	t.Range(func(_ K, v V) bool { result = append(result, convert(v)); return true })
	return result
}

func ConvertMap[K comparable, V, W any](t Table[K, V], convert func(V) W) map[K]W {
	result := make(map[K]W, t.Len())
	t.Range(func(k K, v V) bool { result[k] = convert(v); return true })
	return result
}

func OptionalValues[K comparable, V any](t Table[K, Optional[V]]) []any {
	result := make([]any, 0, t.Len())
	t.Range(func(_ K, v Optional[V]) bool {
		if val, present := v.Get(); present { result = append(result, val) } else { result = append(result, nil) }
		return true
	})
	return result
}

func OptionalValuesOr[K comparable, V any](t Table[K, Optional[V]], fallback V) []V {
	result := make([]V, 0, t.Len())
	t.Range(func(_ K, v Optional[V]) bool { result = append(result, v.Or(fallback)); return true })
	return result
}

func OptionalMap[K comparable, V any](t Table[K, Optional[V]]) map[K]any {
	result := make(map[K]any, t.Len())
	t.Range(func(k K, v Optional[V]) bool {
		if val, present := v.Get(); present { result[k] = val } else { result[k] = nil }
		return true
	})
	return result
}

func OptionalMapOr[K comparable, V any](t Table[K, Optional[V]], fallback V) map[K]V {
	result := make(map[K]V, t.Len())
	t.Range(func(k K, v Optional[V]) bool { result[k] = v.Or(fallback); return true })
	return result
}

// validKey rejects NaN-like keys without recover (NaN is the only Go value where x != x).
func validKey[K comparable](key K) bool { return key == key }
