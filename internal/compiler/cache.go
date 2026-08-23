package compiler

import (
	"errors"
	"fmt"
	"sync"

	"github.com/candango/sqlok/internal/sst"
)

var (
	// ErrNilStatementCache reports an attempt to compile through a nil cache.
	ErrNilStatementCache = errors.New("statement cache cannot be nil")

	// ErrEmptyShapeKey reports a cache operation without a canonical shape key.
	ErrEmptyShapeKey = errors.New("statement shape key cannot be empty")
)

// ShapeKey identifies one canonical statement shape. Runtime values must not
// be included in the key.
type ShapeKey string

// Binding identifies one runtime value position in a compiled statement.
type Binding struct {
	position int
}

// Position returns the zero-based runtime argument position.
func (b Binding) Position() int {
	return b.position
}

// CompiledStatement is an immutable SQL template and its bind layout.
type CompiledStatement struct {
	sql        string
	bindLayout []Binding
}

// SQL returns the placeholder-based SQL template.
func (s CompiledStatement) SQL() string {
	return s.sql
}

// BindLayout returns a copy of the immutable binding layout.
func (s CompiledStatement) BindLayout() []Binding {
	return append([]Binding(nil), s.bindLayout...)
}

// Bind validates the current runtime argument count for this statement shape.
func (s CompiledStatement) Bind(args []any) ([]any, error) {
	if len(args) != len(s.bindLayout) {
		return nil, fmt.Errorf(
			"compiled statement expects %d arguments, got %d",
			len(s.bindLayout),
			len(args),
		)
	}
	return args, nil
}

// StatementCache stores immutable compiled statement shapes by canonical key.
type StatementCache struct {
	mu      sync.RWMutex
	entries map[ShapeKey]CompiledStatement
}

// NewStatementCache creates an empty concurrent statement-shape cache.
func NewStatementCache() *StatementCache {
	return &StatementCache{
		entries: make(map[ShapeKey]CompiledStatement),
	}
}

// Get returns a compiled shape without exposing mutable cache state.
func (c *StatementCache) Get(key ShapeKey) (CompiledStatement, bool) {
	if c == nil {
		return CompiledStatement{}, false
	}

	c.mu.RLock()
	shape, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok {
		return CompiledStatement{}, false
	}
	return cloneCompiledStatement(shape), true
}

// Put publishes a compiled shape under its canonical key.
func (c *StatementCache) Put(key ShapeKey, shape CompiledStatement) error {
	if c == nil {
		return ErrNilStatementCache
	}
	if key == "" {
		return ErrEmptyShapeKey
	}

	c.mu.Lock()
	if c.entries == nil {
		c.entries = make(map[ShapeKey]CompiledStatement)
	}
	c.entries[key] = cloneCompiledStatement(shape)
	c.mu.Unlock()
	return nil
}

// Invalidate removes one compiled shape and reports whether it existed.
func (c *StatementCache) Invalidate(key ShapeKey) bool {
	if c == nil {
		return false
	}

	c.mu.Lock()
	_, existed := c.entries[key]
	delete(c.entries, key)
	c.mu.Unlock()
	return existed
}

// Clear removes every compiled shape from the cache.
func (c *StatementCache) Clear() {
	if c == nil {
		return
	}

	c.mu.Lock()
	c.entries = make(map[ShapeKey]CompiledStatement)
	c.mu.Unlock()
}

// Len returns the number of published shapes.
func (c *StatementCache) Len() int {
	if c == nil {
		return 0
	}

	c.mu.RLock()
	length := len(c.entries)
	c.mu.RUnlock()
	return length
}

// CompileShape compiles a statement into an immutable shape without retaining
// its current runtime values.
func CompileShape(stmt sst.StatementNode) (CompiledStatement, error) {
	shape, _, err := compileStatement(stmt)
	return shape, err
}

// CompileCached returns a cached SQL shape and the current bound values. Cache
// hits collect current values without re-rendering the SQL template.
func CompileCached(
	cache *StatementCache,
	key ShapeKey,
	stmt sst.StatementNode,
) (CompiledStatement, []any, error) {
	if cache == nil {
		return CompiledStatement{}, nil, ErrNilStatementCache
	}
	if key == "" {
		return CompiledStatement{}, nil, ErrEmptyShapeKey
	}

	if shape, ok := cache.Get(key); ok {
		args, err := CollectArgs(stmt)
		if err != nil {
			return CompiledStatement{}, nil, err
		}
		args, err = shape.Bind(args)
		if err != nil {
			return CompiledStatement{}, nil, err
		}
		return shape, args, nil
	}

	shape, args, err := compileStatement(stmt)
	if err != nil {
		return CompiledStatement{}, nil, err
	}
	if _, err := shape.Bind(args); err != nil {
		return CompiledStatement{}, nil, err
	}
	if err := cache.Put(key, shape); err != nil {
		return CompiledStatement{}, nil, err
	}
	return shape, args, nil
}

func compileStatement(stmt sst.StatementNode) (CompiledStatement, []any, error) {
	sqlText, args, err := Compile(stmt)
	if err != nil {
		return CompiledStatement{}, nil, err
	}
	return newCompiledStatement(sqlText, len(args)), args, nil
}

func cloneCompiledStatement(shape CompiledStatement) CompiledStatement {
	shape.bindLayout = append([]Binding(nil), shape.bindLayout...)
	return shape
}

func newCompiledStatement(sqlText string, argumentCount int) CompiledStatement {
	bindLayout := make([]Binding, argumentCount)
	for position := range bindLayout {
		bindLayout[position] = Binding{position: position}
	}
	return CompiledStatement{
		sql:        sqlText,
		bindLayout: bindLayout,
	}
}
