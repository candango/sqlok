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

	// ErrLoadedEntityWithoutPrimaryKey reports a database row that cannot be
	// registered in the Identity Map.
	ErrLoadedEntityWithoutPrimaryKey = errors.New(
		"loaded entity has no primary key",
	)

	// ErrNilFlushContext reports a Flush call without a context.
	ErrNilFlushContext = errors.New("session flush context cannot be nil")

	// ErrNilFlushTransaction reports a Flush call without a caller-owned tx.
	ErrNilFlushTransaction = errors.New("session flush transaction cannot be nil")

	// ErrNilSessionTransaction reports binding a nil transaction to a Session.
	ErrNilSessionTransaction = errors.New("session transaction cannot be nil")

	// ErrSessionTransactionAlreadyBound reports replacing an active transaction
	// binding without first unbinding it.
	ErrSessionTransactionAlreadyBound = errors.New("session transaction is already bound")

	// ErrPrimaryKeyMutation reports a tracked object whose identity changed.
	ErrPrimaryKeyMutation = errors.New("tracked entity primary key changed")

	// ErrGeneratedKeyUnsupported reports a pending insert whose generated
	// primary key cannot be read from the executor result.
	ErrGeneratedKeyUnsupported = errors.New(
		"generated primary key is unsupported by the executor",
	)

	// ErrEntityNotTracked reports deleting an entity that is not managed by the
	// Session identity map or pending queue.
	ErrEntityNotTracked = errors.New("entity is not tracked by session")
)

// Session represents the Unit of Work. It tracks object states and
// manages the identity of entities in memory.
type Session struct {
	// db is the underlying SQL database connection.
	db *sql.DB

	// tx is the caller-owned transaction used for bound reads and autoflush.
	tx *sql.Tx

	// readCache and readPlans hold Session-private prepared read plans. They
	// keep compiler plumbing out of the ORM facade and do not own a global cache.
	readCache *compiler.StatementCache
	readPlans *compiler.PlanRegistry

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

	// deleted holds persistent objects marked for DELETE at the next Flush.
	// deleteIdentities preserves the identity captured when Delete was called.
	deleted          []any
	deleteIdentities map[any]any
}

// NewSession initializes a new Unit of Work with empty state and private read/write plans.
func NewSession(db *sql.DB) *Session {
	return &Session{
		db:               db,
		readCache:        compiler.NewStatementCache(),
		readPlans:        compiler.NewPlanRegistry(),
		flushCache:       compiler.NewStatementCache(),
		flushPlans:       compiler.NewPlanRegistry(),
		identityMap:      make(map[reflect.Type]map[any]any),
		snapshots:        make(map[any]map[string]fieldSnapshot),
		deleteIdentities: make(map[any]any),
	}
}

// BindTransaction binds a caller-owned transaction to the Session. Bound reads
// use the transaction and autoflush pending or dirty entities before querying.
func (s *Session) BindTransaction(tx *sql.Tx) error {
	if s == nil {
		return ErrNilSession
	}
	if tx == nil {
		return ErrNilSessionTransaction
	}
	if s.tx != nil && s.tx != tx {
		return ErrSessionTransactionAlreadyBound
	}
	s.tx = tx
	return nil
}

// UnbindTransaction removes the caller-owned transaction from the Session.
// It never commits or rolls back the transaction.
func (s *Session) UnbindTransaction() {
	if s == nil {
		return
	}
	s.tx = nil
}

func (s *Session) selectExecutor(ctx context.Context) (executor.Executor, error) {
	if s == nil {
		return nil, ErrNilSession
	}
	if s.tx != nil {
		if err := s.Flush(ctx, s.tx); err != nil {
			return nil, fmt.Errorf("autoflush session before SELECT: %w", err)
		}
		return s.tx, nil
	}
	if s.db == nil {
		return nil, ErrNilSessionDatabase
	}
	return s.db, nil
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

	if err := s.registerIdentity(ent, entityType, identity); err != nil {
		return err
	}
	if s.snapshots == nil {
		s.snapshots = make(map[any]map[string]fieldSnapshot)
	}
	s.snapshots[ent] = snapshotMappedValues(
		descriptor.mappedValues(value.Elem()),
	)
	return nil
}

// Delete marks a tracked entity for deletion at the next Flush. Pending
// inserts are removed without issuing a DELETE statement.
func (s *Session) Delete(entity any) error {
	if s == nil {
		return ErrNilSession
	}

	descriptor, root, err := mapperDescriptorForEntity(entity)
	if err != nil {
		return fmt.Errorf("map session entity for delete: %w", err)
	}
	if s.isDeleted(entity) {
		return nil
	}
	if s.removePending(entity) {
		delete(s.snapshots, entity)
		return nil
	}

	identity, present, err := descriptor.loadedPrimaryKey(root)
	if err != nil {
		return fmt.Errorf("read session entity primary key for delete: %w", err)
	}
	if !present {
		return fmt.Errorf("%w: %s", ErrNoPrimaryKey, descriptor.typ)
	}
	tracked, found := s.identityMap[descriptor.typ][identity]
	if !found || tracked != entity {
		return fmt.Errorf("%w: %s", ErrEntityNotTracked, descriptor.typ)
	}

	if s.deleteIdentities == nil {
		s.deleteIdentities = make(map[any]any)
	}
	s.deleted = append(s.deleted, entity)
	s.deleteIdentities[entity] = identity
	return nil
}

func (s *Session) isDeleted(entity any) bool {
	if s == nil || s.deleteIdentities == nil {
		return false
	}
	_, found := s.deleteIdentities[entity]
	return found
}

func (s *Session) removePending(entity any) bool {
	if len(s.pending) == 0 {
		return false
	}

	found := false
	remaining := s.pending[:0]
	for _, candidate := range s.pending {
		if candidate == entity {
			found = true
			continue
		}
		remaining = append(remaining, candidate)
	}
	if found {
		s.pending = remaining
		if len(s.pending) == 0 {
			s.pending = nil
		}
	}
	return found
}

func (s *Session) registerIdentity(
	entity any,
	entityType reflect.Type,
	identity any,
) error {
	if s.identityMap == nil {
		s.identityMap = make(map[reflect.Type]map[any]any)
	}
	if s.identityMap[entityType] == nil {
		s.identityMap[entityType] = make(map[any]any)
	}
	if existing, exists := s.identityMap[entityType][identity]; exists {
		if existing != entity {
			return ErrIdentityConflict
		}
		return nil
	}
	s.identityMap[entityType][identity] = entity
	return nil
}

func (s *Session) registerLoadedEntity(
	entity any,
	entityType reflect.Type,
	identity any,
	values []MappedValue,
) error {
	if err := s.registerIdentity(entity, entityType, identity); err != nil {
		return err
	}
	if s.snapshots == nil {
		s.snapshots = make(map[any]map[string]fieldSnapshot)
	}
	s.snapshots[entity] = snapshotMappedValues(values)
	return nil
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
	if err := s.flushDeleted(ctx, tx); err != nil {
		return err
	}
	if err := s.registerPendingEntities(); err != nil {
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
	s.finalizeDeleted()
	return nil
}

type pendingInsert struct {
	entity      any
	descriptor  *mapperDescriptor
	root        reflect.Value
	generatedID int64
	generated   bool
}

func (s *Session) flushPending(
	ctx context.Context,
	tx *sql.Tx,
) (map[any]map[string]fieldSnapshot, error) {
	snapshots := make(map[any]map[string]fieldSnapshot, len(s.pending))
	inserts := make([]pendingInsert, 0, len(s.pending))
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
		generated := !includePrimary && len(descriptor.primaryFields) > 0
		if generated {
			if err := validateGeneratedPrimaryKey(descriptor); err != nil {
				return nil, fmt.Errorf("validate pending session entity: %w", err)
			}
		}

		values := descriptor.mappedValues(root)
		plan, err := s.insertPlan(descriptor, includePrimary)
		if err != nil {
			return nil, err
		}
		arguments, err := bindInsertValues(plan, values, includePrimary)
		if err != nil {
			return nil, err
		}
		result, err := executor.Exec(ctx, tx, plan, arguments)
		if err != nil {
			return nil, fmt.Errorf("insert pending session entity: %w", err)
		}

		insert := pendingInsert{
			entity:     entity,
			descriptor: descriptor,
			root:       root,
			generated:  generated,
		}
		if generated {
			if result == nil {
				return nil, fmt.Errorf(
					"read generated primary key for %s: %w",
					descriptor.typ,
					ErrGeneratedKeyUnsupported,
				)
			}
			insert.generatedID, err = result.LastInsertId()
			if err != nil {
				return nil, fmt.Errorf(
					"read generated primary key for %s: %w: %v",
					descriptor.typ,
					ErrGeneratedKeyUnsupported,
					err,
				)
			}
		}
		inserts = append(inserts, insert)
	}

	for _, insert := range inserts {
		if insert.generated {
			field := insert.descriptor.fields[insert.descriptor.primaryFields[0]]
			if err := assignGeneratedPrimaryKey(
				insert.root,
				field,
				insert.generatedID,
			); err != nil {
				return nil, fmt.Errorf(
					"assign generated primary key for %s: %w",
					insert.descriptor.typ,
					err,
				)
			}
		}

		snapshots[insert.entity] = snapshotMappedValues(
			insert.descriptor.mappedValues(insert.root),
		)
	}
	return snapshots, nil
}

func (s *Session) registerPendingEntities() error {
	for _, entity := range s.pending {
		descriptor, root, err := mapperDescriptorForEntity(entity)
		if err != nil {
			return fmt.Errorf("map inserted session entity: %w", err)
		}
		identity, present, err := descriptor.primaryKey(root)
		if err != nil && !errors.Is(err, ErrNoPrimaryKey) {
			return fmt.Errorf("read inserted entity primary key: %w", err)
		}
		if !present {
			continue
		}
		if err := s.registerIdentity(entity, descriptor.typ, identity); err != nil {
			return fmt.Errorf("register inserted entity: %w", err)
		}
	}
	return nil
}

func (s *Session) flushDirty(
	ctx context.Context,
	tx *sql.Tx,
) (map[any]map[string]fieldSnapshot, error) {
	snapshots := make(map[any]map[string]fieldSnapshot)
	for entityType, entities := range s.identityMap {
		for identity, entity := range entities {
			if s.isDeleted(entity) {
				continue
			}
			descriptor, root, err := mapperDescriptorForEntity(entity)
			if err != nil {
				return nil, fmt.Errorf("map persistent session entity: %w", err)
			}
			if descriptor.typ != entityType {
				return nil, fmt.Errorf("identity map type does not match entity %s", descriptor.typ)
			}
			currentIdentity, present, err := descriptor.loadedPrimaryKey(root)
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

func (s *Session) flushDeleted(ctx context.Context, tx *sql.Tx) error {
	for _, entity := range s.deleted {
		descriptor, root, err := mapperDescriptorForEntity(entity)
		if err != nil {
			return fmt.Errorf("map deleted session entity: %w", err)
		}
		expectedIdentity, found := s.deleteIdentities[entity]
		if !found {
			return fmt.Errorf("%w: %s", ErrEntityNotTracked, descriptor.typ)
		}
		identity, present, err := descriptor.loadedPrimaryKey(root)
		if err != nil {
			return fmt.Errorf("read deleted entity primary key: %w", err)
		}
		if !present || !reflect.DeepEqual(identity, expectedIdentity) {
			return fmt.Errorf("%w: %s", ErrPrimaryKeyMutation, descriptor.typ)
		}
		tracked, trackedFound := s.identityMap[descriptor.typ][expectedIdentity]
		if !trackedFound || tracked != entity {
			return fmt.Errorf("%w: %s", ErrEntityNotTracked, descriptor.typ)
		}

		values := descriptor.mappedValues(root)
		plan, err := s.deletePlan(descriptor)
		if err != nil {
			return err
		}
		arguments, err := bindDeleteValues(plan, values)
		if err != nil {
			return err
		}
		if _, err := executor.Exec(ctx, tx, plan, arguments); err != nil {
			return fmt.Errorf("delete session entity: %w", err)
		}
	}
	return nil
}

func (s *Session) finalizeDeleted() {
	for _, entity := range s.deleted {
		identity, found := s.deleteIdentities[entity]
		if !found {
			continue
		}
		descriptor, _, err := mapperDescriptorForEntity(entity)
		if err == nil {
			entities := s.identityMap[descriptor.typ]
			if entities != nil && entities[identity] == entity {
				delete(entities, identity)
				if len(entities) == 0 {
					delete(s.identityMap, descriptor.typ)
				}
			}
		}
		delete(s.snapshots, entity)
		delete(s.deleteIdentities, entity)
	}
	s.deleted = nil
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

func (s *Session) deletePlan(
	descriptor *mapperDescriptor,
) (compiler.CompiledStatement, error) {
	if s.flushPlans == nil {
		s.flushPlans = compiler.NewPlanRegistry()
	}
	if s.flushCache == nil {
		s.flushCache = compiler.NewStatementCache()
	}

	planID := compiler.PlanID(fmt.Sprintf(
		"orm.flush.delete.%s.%s",
		descriptor.typ.PkgPath(),
		descriptor.typ.Name(),
	))
	if plan, found := s.flushPlans.Get(planID); found {
		return plan, nil
	}

	statement := dml.Delete(sst.NewTableRef(descriptor.table))
	predicates := make([]sst.ExpressionNode, len(descriptor.primaryFields))
	for position, fieldPosition := range descriptor.primaryFields {
		field := descriptor.fields[fieldPosition]
		predicates[position] = sst.Eq(
			sst.NewColumnRef(descriptor.table, field.column),
			sst.NewNamedParameterSlot(primarySlotName(position)),
		)
	}
	if len(predicates) == 0 {
		return compiler.CompiledStatement{}, fmt.Errorf(
			"build session delete plan: %w: %s",
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
			"prepare session delete plan: %w",
			err,
		)
	}
	if err := s.flushPlans.Put(planID, plan); err != nil {
		return compiler.CompiledStatement{}, fmt.Errorf(
			"register session delete plan: %w",
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
			sst.NewNamedParameterSlot(primarySlotName(position)),
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

func bindDeleteValues(
	plan compiler.CompiledStatement,
	values []MappedValue,
) (*compiler.ArgumentBuffer, error) {
	arguments := plan.NewArgumentBuffer()
	primaryPosition := 0
	for _, value := range values {
		if !value.Primary {
			continue
		}
		if err := arguments.Set(primarySlotName(primaryPosition), value.Value); err != nil {
			return nil, fmt.Errorf("bind session delete primary key: %w", err)
		}
		primaryPosition++
	}
	if primaryPosition == 0 {
		return nil, fmt.Errorf("bind session delete values: %w", ErrNoPrimaryKey)
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
			if err := arguments.Set(primarySlotName(primaryPosition), value.Value); err != nil {
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

func primarySlotName(position int) string {
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
