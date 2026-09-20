package compiler

import "strings"

// ArgumentBuffer is an execution-owned set of values addressed by logical
// slots. A buffer is created from one CompiledStatement and can be reset and
// reused for subsequent executions of that same shape.
type ArgumentBuffer struct {
	shapeKey ShapeKey
	layout   []Binding
	values   []any
	assigned []bool
}

// NewArgumentBuffer creates a slot-addressed argument buffer for the compiled
// statement. The buffer must not be reused concurrently by multiple
// executions.
func (s CompiledStatement) NewArgumentBuffer() *ArgumentBuffer {
	return &ArgumentBuffer{
		shapeKey: s.shapeKey,
		layout:   append([]Binding(nil), s.bindLayout...),
		values:   make([]any, len(s.bindLayout)),
		assigned: make([]bool, len(s.bindLayout)),
	}
}

// Reset clears all values while retaining the buffer's storage for reuse.
func (b *ArgumentBuffer) Reset() {
	if b == nil {
		return
	}
	clear(b.values)
	clear(b.assigned)
}

// Set assigns a value to every SQL position associated with a logical slot.
// The value is emitted once per occurrence of that slot in SQL order.
func (b *ArgumentBuffer) Set(slot string, value any) error {
	if b == nil {
		return ErrNilArgumentBuffer
	}
	slot = strings.TrimSpace(slot)
	if slot == "" {
		return ErrEmptyParameterSlotName
	}

	found := false
	for position, binding := range b.layout {
		if binding.source != slot {
			continue
		}
		found = true
		if b.assigned[position] {
			return ErrDuplicateBindSlot
		}
	}
	if !found {
		return ErrUnknownBindSlot
	}

	for position, binding := range b.layout {
		if binding.source == slot {
			b.values[position] = value
			b.assigned[position] = true
		}
	}
	return nil
}

// SetPosition assigns a value to an unnamed positional binding. Named slots
// must use Set so their logical identity remains explicit.
func (b *ArgumentBuffer) SetPosition(position int, value any) error {
	if b == nil {
		return ErrNilArgumentBuffer
	}
	if position < 0 || position >= len(b.layout) {
		return ErrInvalidBindPosition
	}
	if b.layout[position].source != "" {
		return ErrNamedBindPosition
	}
	if b.assigned[position] {
		return ErrDuplicateBindSlot
	}
	b.values[position] = value
	b.assigned[position] = true
	return nil
}
