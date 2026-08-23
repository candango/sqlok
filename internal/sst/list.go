package sst

// ListNode represents an ordered list of semantic-tree nodes.
type ListNode[T Node] interface {
	Node

	Append(...T)

	Clear()

	// Items returns the list elements in traversal order.
	Items() []T
}

// SeparatedListNode represents an ordered list with a separator between
// adjacent items.
type SeparatedListNode[T Node] interface {
	ListNode[T]

	// Separator returns the token emitted between adjacent items.
	Separator() string
}

// List represents an ordered list of semantic-tree nodes.
type List[T Node] struct {
	items []T
}

var _ ListNode[Node] = (*List[Node])(nil)

// NewList creates an ordered list from the provided nodes.
func NewList[T Node](items ...T) *List[T] {
	return &List[T]{
		items: append([]T(nil), items...),
	}
}

// Append adds items to the end of the list.
func (l *List[T]) Append(items ...T) {
	l.items = append(l.items, items...)
}

// Clear removes all items from the list.
func (l *List[T]) Clear() {
	l.items = nil
}

// Items returns the list elements in traversal order.
func (l *List[T]) Items() []T {
	return l.items
}

// Accept visits each list element in order.
func (l *List[T]) Accept(v Visitor) error {
	for _, item := range l.items {
		if err := item.Accept(v); err != nil {
			return err
		}
	}
	return nil
}

// CommaSeparatedList represents a list separated by comma and space.
type CommaSeparatedList[T Node] struct {
	*List[T]
}

var _ SeparatedListNode[Node] = (*CommaSeparatedList[Node])(nil)

// NewCommaSeparatedList creates a comma-separated list.
func NewCommaSeparatedList[T Node](items ...T) *CommaSeparatedList[T] {
	return &CommaSeparatedList[T]{
		List: NewList(items...),
	}
}

// Separator returns the list separator.
func (l *CommaSeparatedList[T]) Separator() string {
	return ", "
}

// Accept visits items and emits separators between them.
func (l *CommaSeparatedList[T]) Accept(v Visitor) error {
	for i, item := range l.items {
		if err := v.VisitListSeparator(i, l.Separator()); err != nil {
			return err
		}
		if err := item.Accept(v); err != nil {
			return err
		}
	}
	return nil
}
