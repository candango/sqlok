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

	// identityMap ensures that only one instance of an entity exists in memory.
	// Structure: [reflect.Type][PrimaryKey] -> *ObjectPointer
	identityMap map[reflect.Type]map[any]any

	// snapshots stores the field-level hashes for each tracked object.
	// Structure: *ObjectPointer -> map[ColumnName]Hash (uint64)
	snapshots map[any]map[string]uint64

	// pending holds new objects that have been Added but not yet Inserted into the DB.
	pending []any
}

// NewSession initializes a new Unit of Work with empty state and private load plans.
func NewSession(db *sql.DB) *Session {
	return &Session{
		db:          db,
		loadCache:   compiler.NewStatementCache(),
		loadPlans:   compiler.NewPlanRegistry(),
		identityMap: make(map[reflect.Type]map[any]any),
		snapshots:   make(map[any]map[string]uint64),
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
	values, err := mapper.Values(entity)
	if err != nil {
		return nil, fmt.Errorf("snapshot loaded entity: %w", err)
	}
	if err := s.Add(entity); err != nil {
		return nil, fmt.Errorf("register loaded entity: %w", err)
	}
	if s.snapshots == nil {
		s.snapshots = make(map[any]map[string]uint64)
	}
	s.snapshots[entity] = snapshotMappedValues(values)
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

// snapshotMappedValues records one stable fast-path fingerprint per mapped
// column. Flush will use this as a cheap change detector before exact checks.
func snapshotMappedValues(values []MappedValue) map[string]uint64 {
	snapshot := make(map[string]uint64, len(values))
	for _, value := range values {
		hash := fnv.New64a()
		// hash.Hash implementations do not return write errors.
		_, _ = fmt.Fprintf(hash, "%T:%#v", value.Value, value.Value)
		snapshot[value.Column] = hash.Sum64()
	}
	return snapshot
}
