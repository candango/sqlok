package compiler

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/candango/sqlok/internal/dialect"
	"github.com/candango/sqlok/internal/sst"
)

const defaultCompilerVersion = "compiler-v1"

var (
	// ErrNilStatementCache reports an attempt to compile through a nil cache.
	ErrNilStatementCache = errors.New("statement cache cannot be nil")

	// ErrEmptyShapeKey reports a cache operation without a canonical shape key.
	ErrEmptyShapeKey = errors.New("statement shape key cannot be empty")

	// ErrInvalidShapeContext reports incomplete dialect/compiler identity.
	ErrInvalidShapeContext = errors.New("statement shape context is incomplete")

	// ErrUnboundParameterSlot reports a value-compilation attempt for a
	// shape-only parameter slot.
	ErrUnboundParameterSlot = errors.New(
		"compiled statement contains unbound parameter slots",
	)
)

// ShapeKey identifies one canonical statement shape. Runtime values must not
// be included in the key.
type ShapeKey string

// ShapeContext identifies the rendering inputs that affect a statement shape.
type ShapeContext struct {
	Dialect         dialect.Dialect
	CompilerVersion string
}

var defaultDialect = dialect.NewDefaultDialect()

// DefaultShapeContext returns the current default compiler identity.
func DefaultShapeContext() ShapeContext {
	return ShapeContext{
		Dialect:         defaultDialect,
		CompilerVersion: defaultCompilerVersion,
	}
}

func (c ShapeContext) validate() error {
	if c.Dialect == nil ||
		strings.TrimSpace(string(c.Dialect.Name())) == "" ||
		strings.TrimSpace(c.CompilerVersion) == "" {
		return ErrInvalidShapeContext
	}
	return nil
}

// SlotKind identifies the source of one runtime argument position.
type SlotKind string

const (
	SlotBind      SlotKind = "bind"
	SlotParameter SlotKind = "parameter"
	SlotLimit     SlotKind = "limit"
	SlotOffset    SlotKind = "offset"
)

// Binding identifies one runtime value position in a compiled statement.
type Binding struct {
	position int
	kind     SlotKind
}

// Position returns the zero-based runtime argument position.
func (b Binding) Position() int {
	return b.position
}

// Kind returns the source kind of the runtime argument position.
func (b Binding) Kind() SlotKind {
	return b.kind
}

// CompiledStatement is an immutable SQL template and its bind layout.
type CompiledStatement struct {
	sql             string
	bindLayout      []Binding
	shapeKey        ShapeKey
	dialect         string
	compilerVersion string
}

// SQL returns the placeholder-based SQL template.
func (s CompiledStatement) SQL() string {
	return s.sql
}

// BindLayout returns a copy of the immutable binding layout.
func (s CompiledStatement) BindLayout() []Binding {
	return append([]Binding(nil), s.bindLayout...)
}

// ShapeKey returns the canonical identity of the compiled statement.
func (s CompiledStatement) ShapeKey() ShapeKey {
	return s.shapeKey
}

// Dialect returns the dialect identity used during compilation.
func (s CompiledStatement) Dialect() string {
	return s.dialect
}

// CompilerVersion returns the compiler artifact version used during
// compilation.
func (s CompiledStatement) CompilerVersion() string {
	return s.compilerVersion
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

// CompileShape compiles a statement into an immutable shape using the default
// shape context without retaining its current runtime values.
func CompileShape(stmt sst.StatementNode) (CompiledStatement, error) {
	return CompileShapeWithContext(stmt, DefaultShapeContext())
}

// CompileShapeWithContext compiles a statement using explicit rendering
// identity without retaining its current runtime values.
func CompileShapeWithContext(
	stmt sst.StatementNode,
	context ShapeContext,
) (CompiledStatement, error) {
	if err := context.validate(); err != nil {
		return CompiledStatement{}, err
	}
	key, err := DeriveShapeKey(stmt, context)
	if err != nil {
		return CompiledStatement{}, err
	}
	shape, _, err := compileStatement(stmt, context, key)
	return shape, err
}

// Prepare derives or retrieves an immutable compiled statement shape.
// Callers can reuse the returned shape and call Bind for the warm path.
func Prepare(
	cache *StatementCache,
	stmt sst.StatementNode,
	context ShapeContext,
) (CompiledStatement, error) {
	if cache == nil {
		return CompiledStatement{}, ErrNilStatementCache
	}
	if err := context.validate(); err != nil {
		return CompiledStatement{}, err
	}
	key, err := DeriveShapeKey(stmt, context)
	if err != nil {
		return CompiledStatement{}, err
	}
	if shape, ok := cache.Get(key); ok {
		return shape, nil
	}

	shape, _, err := compileStatement(stmt, context, key)
	if err != nil {
		return CompiledStatement{}, err
	}
	if err := cache.Put(key, shape); err != nil {
		return CompiledStatement{}, err
	}
	return shape, nil
}

// CompileCached returns a cached SQL shape and the supplied runtime values
// using the default shape context. The shape key is derived from the statement.
func CompileCached(
	cache *StatementCache,
	stmt sst.StatementNode,
	args []any,
) (CompiledStatement, []any, error) {
	return CompileCachedWithContext(cache, stmt, args, DefaultShapeContext())
}

// CompileCachedWithContext returns a cached SQL shape and supplied runtime
// values. The shape key is derived from canonical statement structure/context.
func CompileCachedWithContext(
	cache *StatementCache,
	stmt sst.StatementNode,
	args []any,
	context ShapeContext,
) (CompiledStatement, []any, error) {
	shape, err := Prepare(cache, stmt, context)
	if err != nil {
		return CompiledStatement{}, nil, err
	}
	args, err = shape.Bind(args)
	if err != nil {
		return CompiledStatement{}, nil, err
	}
	return shape, args, nil
}

func compileStatement(
	stmt sst.StatementNode,
	context ShapeContext,
	key ShapeKey,
) (CompiledStatement, []any, error) {
	sqlText, args, bindings, err := compileWithContext(stmt, context)
	if err != nil {
		return CompiledStatement{}, nil, err
	}
	return newCompiledStatementWithContext(sqlText, bindings, key, context), args, nil
}

func cloneCompiledStatement(shape CompiledStatement) CompiledStatement {
	shape.bindLayout = append([]Binding(nil), shape.bindLayout...)
	return shape
}

func newCompiledStatement(sqlText string, argumentCount int) CompiledStatement {
	context := DefaultShapeContext()
	bindings := make([]Binding, argumentCount)
	for position := range bindings {
		bindings[position] = Binding{
			position: position,
			kind:     SlotBind,
		}
	}
	return newCompiledStatementWithContext(
		sqlText,
		bindings,
		ShapeKey("manual-test-shape"),
		context,
	)
}

func newCompiledStatementWithContext(
	sqlText string,
	bindings []Binding,
	key ShapeKey,
	context ShapeContext,
) CompiledStatement {
	return CompiledStatement{
		sql:             sqlText,
		bindLayout:      append([]Binding(nil), bindings...),
		shapeKey:        key,
		dialect:         string(context.Dialect.Name()),
		compilerVersion: context.CompilerVersion,
	}
}
