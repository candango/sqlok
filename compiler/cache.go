package compiler

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/candango/sqlok/dialect"
	"github.com/candango/sqlok/sst"
)

const defaultCompilerVersion = "compiler-v1"

var (
	// ErrNilStatementCache reports an attempt to compile through a nil cache.
	ErrNilStatementCache = errors.New("statement cache cannot be nil")

	// ErrEmptyShapeKey reports a cache operation without a canonical shape key.
	ErrEmptyShapeKey = errors.New("statement shape key cannot be empty")

	// ErrInvalidCacheLimit reports a bounded cache without a positive limit.
	ErrInvalidCacheLimit = errors.New("statement cache limit must be positive")

	// ErrNilPlanRegistry reports an operation through a nil plan registry.
	ErrNilPlanRegistry = errors.New("plan registry cannot be nil")

	// ErrEmptyPlanID reports a registry operation without an external plan ID.
	ErrEmptyPlanID = errors.New("plan ID cannot be empty")

	// ErrInvalidDialect reports a missing or unnamed rendering dialect.
	ErrInvalidDialect = errors.New("statement dialect is invalid")

	// ErrUnboundParameterSlot reports a value-compilation attempt for a
	// shape-only parameter slot.
	ErrUnboundParameterSlot = errors.New(
		"compiled statement contains unbound parameter slots",
	)

	// ErrEmptyParameterSlotName reports a named slot without an identity.
	ErrEmptyParameterSlotName = errors.New("parameter slot name cannot be empty")

	// ErrSlotAddressedArgumentsRequired reports positional input for a layout
	// that carries explicit logical slot identities.
	ErrSlotAddressedArgumentsRequired = errors.New(
		"compiled statement requires slot-addressed arguments",
	)

	// ErrNilArgumentBuffer reports binding through a nil argument buffer.
	ErrNilArgumentBuffer = errors.New("argument buffer cannot be nil")

	// ErrArgumentBufferShapeMismatch reports a buffer created for another
	// compiled statement shape.
	ErrArgumentBufferShapeMismatch = errors.New(
		"argument buffer does not match compiled statement",
	)

	// ErrUnknownBindSlot reports a value addressed to an unknown logical slot.
	ErrUnknownBindSlot = errors.New("unknown compiled statement bind slot")

	// ErrDuplicateBindSlot reports a logical slot assigned more than once.
	ErrDuplicateBindSlot = errors.New("compiled statement bind slot already set")

	// ErrMissingBindSlot reports a layout position without a runtime value.
	ErrMissingBindSlot = errors.New("compiled statement bind slot is missing")

	// ErrInvalidBindPosition reports an argument buffer position outside its
	// compiled statement layout.
	ErrInvalidBindPosition = errors.New("compiled statement bind position is invalid")

	// ErrNamedBindPosition reports positional addressing of a named slot.
	ErrNamedBindPosition = errors.New(
		"named compiled statement bind slot requires its logical name",
	)
)

// ShapeKey identifies one canonical statement shape. Runtime values must not
// be included in the key.
type ShapeKey string

// shapeContext stores the internal inputs that affect a statement shape.
type shapeContext struct {
	dialect         dialect.Dialect
	compilerVersion string
}

var defaultDialect = dialect.NewDefaultDialect()

func defaultShapeContext() shapeContext {
	return shapeContext{
		dialect:         defaultDialect,
		compilerVersion: defaultCompilerVersion,
	}
}

func newShapeContext(renderingDialect dialect.Dialect) (shapeContext, error) {
	context := shapeContext{
		dialect:         renderingDialect,
		compilerVersion: defaultCompilerVersion,
	}
	if err := context.validate(); err != nil {
		return shapeContext{}, err
	}
	return context, nil
}

func (c shapeContext) validate() error {
	if c.dialect == nil ||
		strings.TrimSpace(string(c.dialect.Name())) == "" ||
		strings.TrimSpace(c.compilerVersion) == "" {
		return ErrInvalidDialect
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
	source   string
}

// Position returns the zero-based runtime argument position.
func (b Binding) Position() int {
	return b.position
}

// Kind returns the source kind of the runtime argument position.
func (b Binding) Kind() SlotKind {
	return b.kind
}

// Source returns the logical source slot name, when the binding is explicitly
// named. An empty source denotes a legacy positional binding.
func (b Binding) Source() string {
	return b.source
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

// Bind validates positional runtime arguments for a legacy layout and
// returns them in SQL order. Named layouts must use BindBuffer so slot
// identity is explicit.
func (s CompiledStatement) Bind(args []any) ([]any, error) {
	return s.bindPositional(args)
}

// BindBuffer validates slot-addressed runtime arguments and returns them in
// SQL order.
func (s CompiledStatement) BindBuffer(buffer *ArgumentBuffer) ([]any, error) {
	return s.bindArgumentBuffer(buffer)
}

func (s CompiledStatement) bindPositional(args []any) ([]any, error) {
	if s.hasNamedBindings() {
		return nil, ErrSlotAddressedArgumentsRequired
	}
	if len(args) != len(s.bindLayout) {
		return nil, fmt.Errorf(
			"compiled statement expects %d arguments, got %d",
			len(s.bindLayout),
			len(args),
		)
	}
	return args, nil
}

func (s CompiledStatement) hasNamedBindings() bool {
	for _, binding := range s.bindLayout {
		if binding.source != "" {
			return true
		}
	}
	return false
}

func (s CompiledStatement) bindArgumentBuffer(buffer *ArgumentBuffer) ([]any, error) {
	if buffer == nil {
		return nil, ErrNilArgumentBuffer
	}
	if buffer.shapeKey != s.shapeKey || !sameBindings(buffer.layout, s.bindLayout) {
		return nil, ErrArgumentBufferShapeMismatch
	}
	for position, assigned := range buffer.assigned {
		if !assigned {
			return nil, fmt.Errorf("%w at position %d", ErrMissingBindSlot, position)
		}
	}
	return buffer.values, nil
}

func sameBindings(left, right []Binding) bool {
	if len(left) != len(right) {
		return false
	}
	for position := range left {
		if left[position] != right[position] {
			return false
		}
	}
	return true
}

// StatementCache stores immutable compiled statement shapes by canonical key.
// A zero maxEntries value keeps the compatibility behavior of an unbounded
// cache. Bounded caches evict entries in insertion order.
type StatementCache struct {
	mu         sync.RWMutex
	entries    map[ShapeKey]CompiledStatement
	order      []ShapeKey
	head       int
	maxEntries int
}

// PlanID identifies a prepared plan through an application-owned stable name.
type PlanID string

// PlanRegistry stores immutable prepared plans by external application ID.
// Unlike StatementCache, it does not derive a shape key during lookup.
type PlanRegistry struct {
	mu    sync.RWMutex
	plans map[PlanID]CompiledStatement
}

// NewPlanRegistry creates an empty concurrent prepared-plan registry.
func NewPlanRegistry() *PlanRegistry {
	return &PlanRegistry{
		plans: make(map[PlanID]CompiledStatement),
	}
}

// Put publishes a prepared plan under an application-owned stable ID.
func (r *PlanRegistry) Put(id PlanID, plan CompiledStatement) error {
	if r == nil {
		return ErrNilPlanRegistry
	}
	if strings.TrimSpace(string(id)) == "" {
		return ErrEmptyPlanID
	}

	r.mu.Lock()
	if r.plans == nil {
		r.plans = make(map[PlanID]CompiledStatement)
	}
	r.plans[id] = cloneCompiledStatement(plan)
	r.mu.Unlock()
	return nil
}

// Get returns a prepared plan without deriving or traversing a statement.
func (r *PlanRegistry) Get(id PlanID) (CompiledStatement, bool) {
	if r == nil || strings.TrimSpace(string(id)) == "" {
		return CompiledStatement{}, false
	}

	r.mu.RLock()
	plan, ok := r.plans[id]
	r.mu.RUnlock()
	return plan, ok
}

// Len returns the number of published prepared plans.
func (r *PlanRegistry) Len() int {
	if r == nil {
		return 0
	}

	r.mu.RLock()
	length := len(r.plans)
	r.mu.RUnlock()
	return length
}

// NewStatementCache creates an empty concurrent unbounded statement cache.
// Use NewBoundedStatementCache when a memory limit is required.
func NewStatementCache() *StatementCache {
	return &StatementCache{
		entries: make(map[ShapeKey]CompiledStatement),
	}
}

// NewBoundedStatementCache creates a concurrent statement cache with an
// insertion-order eviction limit.
func NewBoundedStatementCache(maxEntries int) (*StatementCache, error) {
	if maxEntries <= 0 {
		return nil, ErrInvalidCacheLimit
	}
	return &StatementCache{
		entries:    make(map[ShapeKey]CompiledStatement, maxEntries),
		maxEntries: maxEntries,
	}, nil
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
	return shape, true
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
	_, exists := c.entries[key]
	c.entries[key] = cloneCompiledStatement(shape)
	if c.maxEntries > 0 && !exists {
		c.order = append(c.order, key)
		c.evictOverflowLocked()
	}
	c.compactOrderLocked()
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
	if c.maxEntries > 0 {
		c.removeOrderKeyLocked(key)
		c.compactOrderLocked()
	}
	c.mu.Unlock()
	return existed
}

// removeOrderKeyLocked removes every queued occurrence of key from the
// bounded eviction order. Invalidation is infrequent compared with cache
// reads, so the linear scan keeps the hot Get path simple while ensuring that
// removed entries cannot retain queue storage or affect later eviction.
func (c *StatementCache) removeOrderKeyLocked(key ShapeKey) {
	if c.head >= len(c.order) {
		c.order = nil
		c.head = 0
		return
	}

	write := c.head
	for _, queuedKey := range c.order[c.head:] {
		if queuedKey == key {
			continue
		}
		c.order[write] = queuedKey
		write++
	}
	clear(c.order[write:])
	c.order = c.order[:write]
	if c.head == len(c.order) {
		c.order = nil
		c.head = 0
	}
}

// Clear removes every compiled shape from the cache.
func (c *StatementCache) Clear() {
	if c == nil {
		return
	}

	c.mu.Lock()
	c.entries = make(map[ShapeKey]CompiledStatement, c.maxEntries)
	c.order = nil
	c.head = 0
	c.mu.Unlock()
}

func (c *StatementCache) evictOverflowLocked() {
	for len(c.entries) > c.maxEntries && c.head < len(c.order) {
		key := c.order[c.head]
		c.order[c.head] = ""
		c.head++
		delete(c.entries, key)
	}
}

func (c *StatementCache) compactOrderLocked() {
	if c.head == 0 || c.head*2 < len(c.order) {
		return
	}
	c.order = append([]ShapeKey(nil), c.order[c.head:]...)
	c.head = 0
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
// dialect without retaining its current runtime values.
func CompileShape(stmt sst.StatementNode) (CompiledStatement, error) {
	return compileShapeWithContext(stmt, defaultShapeContext())
}

// CompileShapeWithDialect compiles a statement using the supplied dialect
// without retaining its current runtime values.
func CompileShapeWithDialect(
	stmt sst.StatementNode,
	renderingDialect dialect.Dialect,
) (CompiledStatement, error) {
	context, err := newShapeContext(renderingDialect)
	if err != nil {
		return CompiledStatement{}, err
	}
	return compileShapeWithContext(stmt, context)
}

func compileShapeWithContext(
	stmt sst.StatementNode,
	context shapeContext,
) (CompiledStatement, error) {
	key, err := deriveShapeKeyWithContext(stmt, context)
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
	renderingDialect dialect.Dialect,
) (CompiledStatement, error) {
	if cache == nil {
		return CompiledStatement{}, ErrNilStatementCache
	}
	context, err := newShapeContext(renderingDialect)
	if err != nil {
		return CompiledStatement{}, err
	}
	return prepareWithContext(cache, stmt, context)
}

// CompileCached returns a cached SQL shape and the supplied runtime values
// using the default shape context. The shape key is derived from the statement.
func CompileCached(
	cache *StatementCache,
	stmt sst.StatementNode,
	args []any,
) (CompiledStatement, []any, error) {
	return compileCachedWithContext(cache, stmt, args, defaultShapeContext())
}

// CompileCachedWithDialect returns a cached SQL shape and supplied runtime
// values using the supplied dialect. The shape key is derived from canonical
// statement structure and the internal compiler context.
func CompileCachedWithDialect(
	cache *StatementCache,
	stmt sst.StatementNode,
	args []any,
	renderingDialect dialect.Dialect,
) (CompiledStatement, []any, error) {
	context, err := newShapeContext(renderingDialect)
	if err != nil {
		return CompiledStatement{}, nil, err
	}
	return compileCachedWithContext(cache, stmt, args, context)
}

func compileCachedWithContext(
	cache *StatementCache,
	stmt sst.StatementNode,
	args []any,
	context shapeContext,
) (CompiledStatement, []any, error) {
	shape, err := prepareWithContext(cache, stmt, context)
	if err != nil {
		return CompiledStatement{}, nil, err
	}
	args, err = shape.Bind(args)
	if err != nil {
		return CompiledStatement{}, nil, err
	}
	return shape, args, nil
}

func prepareWithContext(
	cache *StatementCache,
	stmt sst.StatementNode,
	context shapeContext,
) (CompiledStatement, error) {
	if cache == nil {
		return CompiledStatement{}, ErrNilStatementCache
	}
	key, err := deriveShapeKeyWithContext(stmt, context)
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

func compileStatement(
	stmt sst.StatementNode,
	context shapeContext,
	key ShapeKey,
) (CompiledStatement, []any, error) {
	sqlText, args, bindings, err := compileStatementWithContext(stmt, context)
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
	context := defaultShapeContext()
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
	context shapeContext,
) CompiledStatement {
	return CompiledStatement{
		sql:             sqlText,
		bindLayout:      append([]Binding(nil), bindings...),
		shapeKey:        key,
		dialect:         string(context.dialect.Name()),
		compilerVersion: context.compilerVersion,
	}
}
