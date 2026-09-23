package sqlok

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"reflect"

	"github.com/candango/sqlok/compiler"
	"github.com/candango/sqlok/dialect"
	"github.com/candango/sqlok/executor"
	"github.com/candango/sqlok/sst"
	"github.com/candango/sqlok/sst/dml"
	"github.com/candango/sqlok/sst/dql"
)

var (
	// ErrIdentityConflict reports two distinct pointers with one identity.
	ErrIdentityConflict = errors.New(
		"identity map conflict: another object with the same ID already exists in the session",
	)

	// ErrNilSession reports an operation through a nil Session.
	ErrNilSession = errors.New("session cannot be nil")

	// ErrNilSessionDatabase reports a database-backed operation without a DB.
	ErrNilSessionDatabase = errors.New("session database cannot be nil")

	// ErrNilLoadContext reports a database-backed load without a context.
	ErrNilLoadContext = errors.New("session load context cannot be nil")

	// ErrCompositeLoadKey reports an invalid composite identity supplied to Load.
	ErrCompositeLoadKey = errors.New("composite load key must match mapper primary keys")

	// ErrLoadedEntityWithoutPrimaryKey reports a database row that cannot be
	// registered in the Identity Map.
	ErrLoadedEntityWithoutPrimaryKey = errors.New(
		"loaded entity has no primary key",
	)

	// ErrNilFlushContext reports a Flush call without a context.
	ErrNilFlushContext = errors.New("session flush context cannot be nil")

	// ErrNilFlushTransaction reports a Flush call without a caller-owned tx.
	ErrNilFlushTransaction = errors.New("session flush transaction cannot be nil")

	// ErrPrimaryKeyMutation reports a tracked object whose identity changed.
	ErrPrimaryKeyMutation = errors.New("tracked entity primary key changed")
)

// CompositeKey supplies primary-key values in mapper declaration order for a
// Load of an entity with more than one primary-key field.
type CompositeKey []any

// Session represents the Unit of Work. It tracks object states and
// manages the identity of entities in memory.
type Session struct {
	// db is the underlying SQL database connection.
	db *sql.DB

	// loadCache and loadPlans hold Session-private prepared read plans. They
	// keep compiler plumbing out of the ORM facade and do not own a global cache.
	loadCache *compiler.StatementCache
	loadPlans *compiler.PlanRegistry

	// flushCache and flushPlans hold Session-private prepared write plans.
	flushCache *compiler.StatementCache
	flushPlans *compiler.PlanRegistry

	// identityMap ensures that only one instance of an entity exists in memory.
	// Structure: [reflect.Type][PrimaryKey] -> *ObjectPointer
	identityMap map[reflect.Type]map[any]any

	// snapshots stores copied field values and their hashes for dirty checks.
	// Structure: *ObjectPointer -> map[ColumnName]fieldSnapshot
	snapshots map[any]map[string]fieldSnapshot

	// pending holds new objects that have been Added but not yet Inserted into the DB.
	pending []any
}

// NewSession initializes a new Unit of Work with empty state and private load plans.
func NewSession(db *sql.DB) *Session {
	return &Session{
		db:          db,
		loadCache:   compiler.NewStatementCache(),
		loadPlans:   compiler.NewPlanRegistry(),
		flushCache:  compiler.NewStatementCache(),
		flushPlans:  compiler.NewPlanRegistry(),
		identityMap: make(map[reflect.Type]map[any]any),
		snapshots:   make(map[any]map[string]fieldSnapshot),
	}
}

// Add registers an entity into the session's identity map.
// If the entity has no primary key, it is added to the pending queue for INSERT.
func (s *Session) Add(ent any) error {
	value := reflect.ValueOf(ent)
	if !value.IsValid() || value.Kind() != reflect.Pointer || value.IsNil() ||
		value.Elem().Kind() != reflect.Struct {
		return errors.New("only non-nil pointers to structs can be added to session")
	}

	entityType := value.Elem().Type()
	descriptor, err := mapperDescriptorFor(entityType)
	if err != nil {
		return fmt.Errorf("map session entity %s: %w", entityType, err)
	}
	identity, present, err := descriptor.primaryKey(value.Elem())
	if errors.Is(err, ErrNoPrimaryKey) {
		s.pending = append(s.pending, ent)
		return nil
	}
	if err != nil {
		return fmt.Errorf("read session primary key for %s: %w", entityType, err)
	}
	if !present {
		s.pending = append(s.pending, ent)
		return nil
	}

	if s.identityMap[entityType] == nil {
		s.identityMap[entityType] = make(map[any]any)
	}
	if existing, exists := s.identityMap[entityType][identity]; exists {
		if existing != ent {
			return ErrIdentityConflict
		}
		return nil
	}

	s.identityMap[entityType][identity] = ent
	if s.snapshots == nil {
		s.snapshots = make(map[any]map[string]fieldSnapshot)
	}
	s.snapshots[ent] = snapshotMappedValues(
		descriptor.mappedValues(value.Elem()),
	)
	return nil
}

// Load retrieves an entity by primary key. It first returns the tracked
// pointer from the Identity Map; on a miss it uses context.Background for a
// prepared database query. Prefer LoadContext in request-scoped code.
func Load[T any](s *Session, id any) (*T, error) {
	return LoadContext[T](context.Background(), s, id)
}

// LoadContext retrieves an entity by primary key, querying the database on an
// Identity Map miss. Composite identities use CompositeKey in mapper primary
// field declaration order.
func LoadContext[T any](ctx context.Context, s *Session, id any) (*T, error) {
	if ctx == nil {
		return nil, ErrNilLoadContext
	}
	if s == nil {
		return nil, ErrNilSession
	}

	entityType := reflect.TypeFor[T]()
	descriptor, err := mapperDescriptorFor(entityType)
	if err != nil {
		return nil, fmt.Errorf("map loaded entity %s: %w", entityType, err)
	}
	identity, primaryValues, err := descriptor.loadIdentity(id)
	if err != nil {
		return nil, err
	}
	if typeMap := s.identityMap[entityType]; typeMap != nil {
		if existing, found := typeMap[identity]; found {
			return existing.(*T), nil
		}
	}
	if s.db == nil {
		return nil, ErrNilSessionDatabase
	}

	plan, err := s.loadPlan(descriptor)
	if err != nil {
		return nil, err
	}
	arguments := plan.NewArgumentBuffer()
	for position, value := range primaryValues {
		if err := arguments.Set(loadSlotName(position), value); err != nil {
			return nil, fmt.Errorf("bind session load primary key: %w", err)
		}
	}
	for _, binding := range plan.BindLayout() {
		if binding.Kind() != compiler.SlotLimit {
			continue
		}
		if err := arguments.SetPosition(binding.Position(), 1); err != nil {
			return nil, fmt.Errorf("bind session load limit: %w", err)
		}
	}
	rows, err := executor.Query(ctx, s.db, plan, arguments)
	if err != nil {
		return nil, fmt.Errorf("query session load: %w", err)
	}
	if !rows.Next() {
		queryErr := rows.Err()
		closeErr := rows.Close()
		if queryErr != nil {
			return nil, fmt.Errorf("iterate session load: %w", queryErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close session load rows: %w", closeErr)
		}
		return nil, nil
	}

	mapper, err := NewMapper[T]()
	if err != nil {
		_ = rows.Close()
		return nil, err
	}
	entity, err := mapper.Scan(rows)
	if err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("map session load row: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close session load rows: %w", err)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate session load: %w", err)
	}

	loadedIdentity, present, err := mapper.PrimaryKey(entity)
	if err != nil {
		return nil, fmt.Errorf("read loaded entity primary key: %w", err)
	}
	if !present {
		return nil, ErrLoadedEntityWithoutPrimaryKey
	}
	if loadedIdentity != identity {
		return nil, fmt.Errorf(
			"loaded entity identity %v does not match requested identity %v",
			loadedIdentity,
			identity,
		)
	}
	if err := s.Add(entity); err != nil {
		return nil, fmt.Errorf("register loaded entity: %w", err)
	}
	return entity, nil
}

func (s *Session) loadPlan(
	descriptor *mapperDescriptor,
) (compiler.CompiledStatement, error) {
	if s.loadPlans == nil {
		s.loadPlans = compiler.NewPlanRegistry()
	}
	if s.loadCache == nil {
		s.loadCache = compiler.NewStatementCache()
	}

	planID := compiler.PlanID(fmt.Sprintf(
		"orm.load.%s.%s",
		descriptor.typ.PkgPath(),
		descriptor.typ.Name(),
	))
	if plan, found := s.loadPlans.Get(planID); found {
		return plan, nil
	}

	columns := make([]sst.ExpressionNode, len(descriptor.fields))
	for position, field := range descriptor.fields {
		columns[position] = sst.NewColumnRef(descriptor.table, field.column)
	}
	predicates := make([]sst.ExpressionNode, len(descriptor.primaryFields))
	for position, fieldPosition := range descriptor.primaryFields {
		field := descriptor.fields[fieldPosition]
		predicates[position] = sst.Eq(
			sst.NewColumnRef(descriptor.table, field.column),
			sst.NewNamedParameterSlot(loadSlotName(position)),
		)
	}
	if len(predicates) == 0 {
		return compiler.CompiledStatement{}, fmt.Errorf(
			"build session load plan: %w: %s",
			ErrNoPrimaryKey,
			descriptor.typ,
		)
	}

	where := predicates[0]
	if len(predicates) > 1 {
		where = sst.And(predicates...)
	}
	statement := dql.Select(columns...).
		From(sst.NewTableRef(descriptor.table)).
		Where(where).
		Limit(1)
	plan, err := compiler.Prepare(
		s.loadCache,
		statement,
		dialect.NewDefaultDialect(),
	)
	if err != nil {
		return compiler.CompiledStatement{}, fmt.Errorf(
			"prepare session load plan: %w",
			err,
		)
	}
	if err := s.loadPlans.Put(planID, plan); err != nil {
		return compiler.CompiledStatement{}, fmt.Errorf(
			"register session load plan: %w",
			err,
		)
	}
	return plan, nil
}

// Flush writes pending inserts and dirty persistent entities through a
// caller-owned transaction. It never begins, commits, or rolls back tx.
func (s *Session) Flush(ctx context.Context, tx *sql.Tx) error {
	if ctx == nil {
		return ErrNilFlushContext
	}
	if s == nil {
		return ErrNilSession
	}
	if tx == nil {
		return ErrNilFlushTransaction
	}

	pendingSnapshots, err := s.flushPending(ctx, tx)
	if err != nil {
		return err
	}
	dirtySnapshots, err := s.flushDirty(ctx, tx)
	if err != nil {
		return err
	}

	if len(s.pending) > 0 {
		s.pending = nil
	}
	if s.snapshots == nil {
		s.snapshots = make(map[any]map[string]fieldSnapshot)
	}
	for entity, snapshot := range pendingSnapshots {
		s.snapshots[entity] = snapshot
	}
	for entity, snapshot := range dirtySnapshots {
		s.snapshots[entity] = snapshot
	}
	return nil
}

func (s *Session) flushPending(
	ctx context.Context,
	tx *sql.Tx,
) (map[any]map[string]fieldSnapshot, error) {
	snapshots := make(map[any]map[string]fieldSnapshot, len(s.pending))
	for _, entity := range s.pending {
		descriptor, root, err := mapperDescriptorForEntity(entity)
		if err != nil {
			return nil, fmt.Errorf("map pending session entity: %w", err)
		}
		_, present, keyErr := descriptor.primaryKey(root)
		if keyErr != nil && !errors.Is(keyErr, ErrNoPrimaryKey) {
			return nil, fmt.Errorf("read pending entity primary key: %w", keyErr)
		}
		includePrimary := keyErr == nil && present
		values := descriptor.mappedValues(root)
		plan, err := s.insertPlan(descriptor, includePrimary)
		if err != nil {
			return nil, err
		}
		arguments, err := bindInsertValues(plan, values, includePrimary)
		if err != nil {
			return nil, err
		}
		if _, err := executor.Exec(ctx, tx, plan, arguments); err != nil {
			return nil, fmt.Errorf("insert pending session entity: %w", err)
		}
		snapshots[entity] = snapshotMappedValues(values)
	}
	return snapshots, nil
}

func (s *Session) flushDirty(
	ctx context.Context,
	tx *sql.Tx,
) (map[any]map[string]fieldSnapshot, error) {
	snapshots := make(map[any]map[string]fieldSnapshot)
	for entityType, entities := range s.identityMap {
		for identity, entity := range entities {
			descriptor, root, err := mapperDescriptorForEntity(entity)
			if err != nil {
				return nil, fmt.Errorf("map persistent session entity: %w", err)
			}
			if descriptor.typ != entityType {
				return nil, fmt.Errorf("identity map type does not match entity %s", descriptor.typ)
			}
			currentIdentity, present, err := descriptor.primaryKey(root)
			if err != nil {
				return nil, fmt.Errorf("read persistent entity primary key: %w", err)
			}
			if !present || currentIdentity != identity {
				return nil, fmt.Errorf("%w: %s", ErrPrimaryKeyMutation, descriptor.typ)
			}

			values := descriptor.mappedValues(root)
			if !mappedValuesDirty(values, s.snapshots[entity]) {
				continue
			}
			plan, err := s.updatePlan(descriptor)
			if err != nil {
				return nil, err
			}
			arguments, err := bindUpdateValues(plan, values)
			if err != nil {
				return nil, err
			}
			if _, err := executor.Exec(ctx, tx, plan, arguments); err != nil {
				return nil, fmt.Errorf("update dirty session entity: %w", err)
			}
			snapshots[entity] = snapshotMappedValues(values)
		}
	}
	return snapshots, nil
}

func mapperDescriptorForEntity(entity any) (*mapperDescriptor, reflect.Value, error) {
	value := reflect.ValueOf(entity)
	if !value.IsValid() || value.Kind() != reflect.Pointer || value.IsNil() ||
		value.Elem().Kind() != reflect.Struct {
		return nil, reflect.Value{}, errors.New(
			"session entity must be a non-nil pointer to a struct",
		)
	}
	descriptor, err := mapperDescriptorFor(value.Elem().Type())
	if err != nil {
		return nil, reflect.Value{}, err
	}
	return descriptor, value.Elem(), nil
}

func (s *Session) insertPlan(
	descriptor *mapperDescriptor,
	includePrimary bool,
) (compiler.CompiledStatement, error) {
	if s.flushPlans == nil {
		s.flushPlans = compiler.NewPlanRegistry()
	}
	if s.flushCache == nil {
		s.flushCache = compiler.NewStatementCache()
	}

	planID := compiler.PlanID(fmt.Sprintf(
		"orm.flush.insert.%s.%s.%t",
		descriptor.typ.PkgPath(),
		descriptor.typ.Name(),
		includePrimary,
	))
	if plan, found := s.flushPlans.Get(planID); found {
		return plan, nil
	}

	columns := make([]sst.ColumnRefNode, 0, len(descriptor.fields))
	arguments := make([]sst.ExpressionNode, 0, len(descriptor.fields))
	for _, field := range descriptor.fields {
		if field.primary && !includePrimary {
			continue
		}
		position := len(arguments)
		columns = append(columns, sst.NewColumnRef("", field.column))
		arguments = append(arguments, sst.NewNamedParameterSlot(
			flushValueSlotName(position),
		))
	}
	if len(columns) == 0 {
		return compiler.CompiledStatement{}, errors.New(
			"insert session entity has no writable columns",
		)
	}

	statement := dml.Insert(sst.NewTableRef(descriptor.table), columns...)
	statement.Values(arguments)
	plan, err := compiler.Prepare(
		s.flushCache,
		statement,
		dialect.NewDefaultDialect(),
	)
	if err != nil {
		return compiler.CompiledStatement{}, fmt.Errorf(
			"prepare session insert plan: %w",
			err,
		)
	}
	if err := s.flushPlans.Put(planID, plan); err != nil {
		return compiler.CompiledStatement{}, fmt.Errorf(
			"register session insert plan: %w",
			err,
		)
	}
	return plan, nil
}

func (s *Session) updatePlan(
	descriptor *mapperDescriptor,
) (compiler.CompiledStatement, error) {
	if s.flushPlans == nil {
		s.flushPlans = compiler.NewPlanRegistry()
	}
	if s.flushCache == nil {
		s.flushCache = compiler.NewStatementCache()
	}

	planID := compiler.PlanID(fmt.Sprintf(
		"orm.flush.update.%s.%s",
		descriptor.typ.PkgPath(),
		descriptor.typ.Name(),
	))
	if plan, found := s.flushPlans.Get(planID); found {
		return plan, nil
	}

	statement := dml.Update(sst.NewTableRef(descriptor.table))
	valuePosition := 0
	for _, field := range descriptor.fields {
		if field.primary {
			continue
		}
		statement.Set(
			sst.NewColumnRef("", field.column),
			sst.NewNamedParameterSlot(flushValueSlotName(valuePosition)),
		)
		valuePosition++
	}
	if valuePosition == 0 {
		return compiler.CompiledStatement{}, errors.New(
			"update session entity has no writable columns",
		)
	}

	predicates := make([]sst.ExpressionNode, len(descriptor.primaryFields))
	for position, fieldPosition := range descriptor.primaryFields {
		field := descriptor.fields[fieldPosition]
		predicates[position] = sst.Eq(
			sst.NewColumnRef(descriptor.table, field.column),
			sst.NewNamedParameterSlot(loadSlotName(position)),
		)
	}
	if len(predicates) == 0 {
		return compiler.CompiledStatement{}, fmt.Errorf(
			"build session update plan: %w: %s",
			ErrNoPrimaryKey,
			descriptor.typ,
		)
	}
	where := predicates[0]
	if len(predicates) > 1 {
		where = sst.And(predicates...)
	}
	statement.Where(where)

	plan, err := compiler.Prepare(
		s.flushCache,
		statement,
		dialect.NewDefaultDialect(),
	)
	if err != nil {
		return compiler.CompiledStatement{}, fmt.Errorf(
			"prepare session update plan: %w",
			err,
		)
	}
	if err := s.flushPlans.Put(planID, plan); err != nil {
		return compiler.CompiledStatement{}, fmt.Errorf(
			"register session update plan: %w",
			err,
		)
	}
	return plan, nil
}

func bindInsertValues(
	plan compiler.CompiledStatement,
	values []MappedValue,
	includePrimary bool,
) (*compiler.ArgumentBuffer, error) {
	arguments := plan.NewArgumentBuffer()
	position := 0
	for _, value := range values {
		if value.Primary && !includePrimary {
			continue
		}
		if err := arguments.Set(flushValueSlotName(position), value.Value); err != nil {
			return nil, fmt.Errorf("bind session insert value: %w", err)
		}
		position++
	}
	return arguments, nil
}

func bindUpdateValues(
	plan compiler.CompiledStatement,
	values []MappedValue,
) (*compiler.ArgumentBuffer, error) {
	arguments := plan.NewArgumentBuffer()
	valuePosition := 0
	primaryPosition := 0
	for _, value := range values {
		if value.Primary {
			if err := arguments.Set(loadSlotName(primaryPosition), value.Value); err != nil {
				return nil, fmt.Errorf("bind session update primary key: %w", err)
			}
			primaryPosition++
			continue
		}
		if err := arguments.Set(flushValueSlotName(valuePosition), value.Value); err != nil {
			return nil, fmt.Errorf("bind session update value: %w", err)
		}
		valuePosition++
	}
	return arguments, nil
}

func flushValueSlotName(position int) string {
	return fmt.Sprintf("value_%d", position)
}

func mappedValuesDirty(
	values []MappedValue,
	snapshot map[string]fieldSnapshot,
) bool {
	if len(snapshot) != len(values) {
		return true
	}
	for _, value := range values {
		previous, found := snapshot[value.Column]
		if !found {
			return true
		}
		if hashMappedValue(value.Value) == previous.hash {
			continue
		}
		if !reflect.DeepEqual(value.Value, previous.value) {
			return true
		}
	}
	return false
}

func (d *mapperDescriptor) loadIdentity(id any) (any, []any, error) {
	if len(d.primaryFields) == 0 {
		return nil, nil, fmt.Errorf("%w: %s", ErrNoPrimaryKey, d.typ)
	}
	if len(d.primaryFields) == 1 {
		if id == nil || !reflect.TypeOf(id).Comparable() {
			return nil, nil, fmt.Errorf("invalid session load identity %T", id)
		}
		return id, []any{id}, nil
	}

	values, ok := id.(CompositeKey)
	if !ok || len(values) != len(d.primaryFields) {
		return nil, nil, fmt.Errorf("%w: got %T", ErrCompositeLoadKey, id)
	}
	for _, value := range values {
		if value == nil || !reflect.TypeOf(value).Comparable() {
			return nil, nil, fmt.Errorf("%w: component %T", ErrCompositeLoadKey, value)
		}
	}
	identityValues := append([]any(nil), values...)
	return encodeCompositeKey(identityValues), identityValues, nil
}

func loadSlotName(position int) string {
	return fmt.Sprintf("pk_%d", position)
}

type fieldSnapshot struct {
	hash  uint64
	value any
}

// snapshotMappedValues records copied values and FNV-1a hashes. Flush checks a
// hash first and only uses reflection for fields whose hash changed.
func snapshotMappedValues(values []MappedValue) map[string]fieldSnapshot {
	snapshot := make(map[string]fieldSnapshot, len(values))
	for _, value := range values {
		snapshot[value.Column] = fieldSnapshot{
			hash:  hashMappedValue(value.Value),
			value: cloneSnapshotValue(value.Value),
		}
	}
	return snapshot
}

func hashMappedValue(value any) uint64 {
	hash := fnv.New64a()
	// hash.Hash implementations do not return write errors.
	_, _ = fmt.Fprintf(hash, "%T:%#v", value, value)
	return hash.Sum64()
}

func cloneSnapshotValue(value any) any {
	if value == nil {
		return nil
	}
	return cloneSnapshotReflect(reflect.ValueOf(value)).Interface()
}

func cloneSnapshotReflect(value reflect.Value) reflect.Value {
	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		clone := reflect.New(value.Type().Elem())
		clone.Elem().Set(cloneSnapshotReflect(value.Elem()))
		return clone
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		clone := reflect.New(value.Type()).Elem()
		clone.Set(cloneSnapshotReflect(value.Elem()))
		return clone
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		clone := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for index := range value.Len() {
			clone.Index(index).Set(cloneSnapshotReflect(value.Index(index)))
		}
		return clone
	case reflect.Array:
		clone := reflect.New(value.Type()).Elem()
		for index := range value.Len() {
			clone.Index(index).Set(cloneSnapshotReflect(value.Index(index)))
		}
		return clone
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		clone := reflect.MakeMapWithSize(value.Type(), value.Len())
		iterator := value.MapRange()
		for iterator.Next() {
			clone.SetMapIndex(
				iterator.Key(),
				cloneSnapshotReflect(iterator.Value()),
			)
		}
		return clone
	case reflect.Struct:
		for index := range value.NumField() {
			if value.Type().Field(index).PkgPath != "" {
				return value
			}
		}
		clone := reflect.New(value.Type()).Elem()
		for index := range value.NumField() {
			clone.Field(index).Set(cloneSnapshotReflect(value.Field(index)))
		}
		return clone
	default:
		return value
	}
}
