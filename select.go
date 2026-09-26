package sqlok

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/candango/sqlok/compiler"
	"github.com/candango/sqlok/dialect"
	"github.com/candango/sqlok/executor"
	"github.com/candango/sqlok/sst"
	"github.com/candango/sqlok/sst/dql"
)

var (
	ErrNilSelectQuery       = errors.New("select query cannot be nil")
	ErrNilSelectContext     = errors.New("select context cannot be nil")
	ErrEmptySelectColumn    = errors.New("select criterion column cannot be empty")
	ErrUnmappedSelectColumn = errors.New("select criterion column is not mapped")
	ErrEmptySelectCriteria  = errors.New("select requires at least one criterion")
	ErrNilSelectValue       = errors.New("equality criteria do not support NULL values yet")
	ErrNoSelectRows         = errors.New("select query returned no rows")
	ErrMultipleSelectRows   = errors.New("select query returned more than one row")
)

// SelectCriterion is a predicate accepted by SelectQuery.Where.
// Construct criteria with comparison functions such as Eq.
type SelectCriterion struct {
	column string
	value  any
	err    error
}

// Eq creates a bound equality criterion for a mapped database column. Nil
// values are rejected until NULL predicates are available.
func Eq(column string, value any) SelectCriterion {
	column = strings.TrimSpace(column)
	if column == "" {
		return SelectCriterion{err: ErrEmptySelectColumn}
	}
	if isNilSelectValue(value) {
		return SelectCriterion{err: ErrNilSelectValue}
	}
	return SelectCriterion{column: column, value: value}
}

func isNilSelectValue(value any) bool {
	if value == nil {
		return true
	}

	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice, reflect.UnsafePointer:
		return reflected.IsNil()
	default:
		return false
	}
}

// SelectQuery is a typed, composable SELECT statement for one mapped entity.
// Build it with Select and execute it through All, One, or OneOrNone.
type SelectQuery[T any] struct {
	descriptor *mapperDescriptor
	criteria   []SelectCriterion
	err        error
}

// Select starts a typed entity query. The entity value supplies T; its fields
// are not read. For example: Select(User{}).Where(Eq("name", "Ana")).
func Select[T any](_ T) SelectQuery[T] {
	descriptor, err := mapperDescriptorFor(reflect.TypeFor[T]())
	query := SelectQuery[T]{descriptor: descriptor, err: err}
	if err != nil {
		return query
	}
	return query
}

// Where returns a new SELECT query with the criteria combined using AND.
func (q SelectQuery[T]) Where(criteria ...SelectCriterion) SelectQuery[T] {
	if q.err != nil {
		return q
	}
	if q.descriptor == nil {
		q.err = ErrNilSelectQuery
		return q
	}

	next := q
	if len(criteria) == 0 {
		next.err = ErrEmptySelectCriteria
		return next
	}

	next.criteria = make([]SelectCriterion, 0, len(q.criteria)+len(criteria))
	next.criteria = append(next.criteria, q.criteria...)
	next.criteria = append(next.criteria, criteria...)
	if err := validateSelectCriteria(next.descriptor, next.criteria); err != nil {
		next.err = err
		return next
	}
	return next
}

// All executes the SELECT through session, maps every row to T, and reuses
// tracked entity pointers through the Session Identity Map.
func (q SelectQuery[T]) All(
	ctx context.Context,
	session *Session,
) ([]*T, error) {
	if q.err != nil {
		return nil, q.err
	}
	if q.descriptor == nil {
		return nil, ErrNilSelectQuery
	}
	planID := selectPlanID(q.descriptor, q.criteria, nil)
	rows, err := q.rows(ctx, session, planID, nil)
	if err != nil {
		return nil, err
	}

	entities := make([]*T, 0)
	for rows.Next() {
		entity, err := q.scanEntity(rows, session)
		if err != nil {
			return nil, closeSelectRows(rows, err)
		}
		entities = append(entities, entity)
	}
	if err := rows.Err(); err != nil {
		return nil, closeSelectRows(rows, fmt.Errorf("iterate SELECT rows: %w", err))
	}
	if err := closeSelectRows(rows, nil); err != nil {
		return nil, err
	}
	return entities, nil
}

// OneOrNone returns the sole matching entity, or nil when there are no matches.
// It reads at most two rows to detect a non-unique result.
func (q SelectQuery[T]) OneOrNone(
	ctx context.Context,
	session *Session,
) (*T, error) {
	if q.err != nil {
		return nil, q.err
	}
	if q.descriptor == nil {
		return nil, ErrNilSelectQuery
	}
	limit := 2
	planID := selectPlanID(q.descriptor, q.criteria, &limit)
	rows, err := q.rows(ctx, session, planID, &limit)
	if err != nil {
		return nil, err
	}
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, closeSelectRows(rows, fmt.Errorf("iterate SELECT rows: %w", err))
		}
		if err := closeSelectRows(rows, nil); err != nil {
			return nil, err
		}
		return nil, nil
	}

	entity, err := q.scanEntity(rows, session)
	if err != nil {
		return nil, closeSelectRows(rows, err)
	}
	if rows.Next() {
		return nil, closeSelectRows(rows, ErrMultipleSelectRows)
	}
	if err := rows.Err(); err != nil {
		return nil, closeSelectRows(rows, fmt.Errorf("iterate SELECT rows: %w", err))
	}
	if err := closeSelectRows(rows, nil); err != nil {
		return nil, err
	}
	return entity, nil
}

// One returns exactly one matching entity or an error for zero or multiple rows.
func (q SelectQuery[T]) One(
	ctx context.Context,
	session *Session,
) (*T, error) {
	entity, err := q.OneOrNone(ctx, session)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return nil, ErrNoSelectRows
	}
	return entity, nil
}

type selectRows interface {
	Scanner
	Next() bool
	Err() error
	Close() error
}

func closeSelectRows(rows selectRows, primary error) error {
	closeErr := rows.Close()
	if closeErr == nil {
		return primary
	}
	closeErr = fmt.Errorf("close SELECT rows: %w", closeErr)
	if primary == nil {
		return closeErr
	}
	return errors.Join(primary, closeErr)
}

func (q SelectQuery[T]) rows(
	ctx context.Context,
	session *Session,
	planID compiler.PlanID,
	limit *int,
) (selectRows, error) {
	if ctx == nil {
		return nil, ErrNilSelectContext
	}
	if session == nil {
		return nil, ErrNilSession
	}
	if session.db == nil {
		return nil, ErrNilSessionDatabase
	}

	plan, found := session.readPlans.Get(planID)
	if !found {
		statement, err := buildSelectStatement(q.descriptor, q.criteria, limit)
		if err != nil {
			return nil, fmt.Errorf("build SELECT query: %w", err)
		}
		plan, err = session.preparedReadPlan(planID, statement)
		if err != nil {
			return nil, fmt.Errorf("prepare SELECT query: %w", err)
		}
	}
	arguments := plan.NewArgumentBuffer()
	for position, criterion := range q.criteria {
		if err := arguments.Set(selectWhereSlotName(position), criterion.value); err != nil {
			return nil, fmt.Errorf("bind SELECT criterion: %w", err)
		}
	}
	for _, binding := range plan.BindLayout() {
		if binding.Kind() != compiler.SlotLimit {
			continue
		}
		if limit == nil {
			return nil, errors.New("SELECT plan requires an unconfigured LIMIT")
		}
		if err := arguments.SetPosition(binding.Position(), *limit); err != nil {
			return nil, fmt.Errorf("bind SELECT limit: %w", err)
		}
	}

	rows, err := executor.Query(ctx, session.db, plan, arguments)
	if err != nil {
		return nil, fmt.Errorf("query selected entities: %w", err)
	}
	return rows, nil
}

func (q SelectQuery[T]) scanEntity(scanner Scanner, session *Session) (*T, error) {
	mapper := Mapper[T]{descriptor: q.descriptor}
	entity, err := mapper.Scan(scanner)
	if err != nil {
		return nil, fmt.Errorf("map selected entity: %w", err)
	}
	identity, present, err := q.descriptor.loadedPrimaryKey(reflect.ValueOf(entity).Elem())
	if err != nil {
		return nil, fmt.Errorf("read selected entity identity: %w", err)
	}
	if !present {
		return nil, ErrLoadedEntityWithoutPrimaryKey
	}

	if tracked := session.identityMap[q.descriptor.typ]; tracked != nil {
		if existing, found := tracked[identity]; found {
			return existing.(*T), nil
		}
	}
	values, err := mapper.Values(entity)
	if err != nil {
		return nil, fmt.Errorf("extract selected entity values: %w", err)
	}
	if err := session.registerLoadedEntity(entity, q.descriptor.typ, identity, values); err != nil {
		return nil, fmt.Errorf("register selected entity: %w", err)
	}
	return entity, nil
}

func buildSelectStatement(
	descriptor *mapperDescriptor,
	criteria []SelectCriterion,
	limit *int,
) (*dql.SelectStatement, error) {
	if descriptor == nil {
		return nil, errors.New("select mapper cannot be nil")
	}

	columns := make([]sst.ExpressionNode, len(descriptor.fields))
	for position, field := range descriptor.fields {
		columns[position] = sst.NewColumnRef(descriptor.table, field.column)
	}
	statement := dql.Select(columns...).From(sst.NewTableRef(descriptor.table))

	if err := validateSelectCriteria(descriptor, criteria); err != nil {
		return nil, err
	}
	for position, criterion := range criteria {
		statement.Where(sst.Eq(
			sst.NewColumnRef(descriptor.table, criterion.column),
			sst.NewNamedParameterSlot(selectWhereSlotName(position)),
		))
	}
	if limit != nil {
		statement.Limit(*limit)
	}
	if err := statement.Err(); err != nil {
		return nil, err
	}
	return statement, nil
}

func validateSelectCriteria(
	descriptor *mapperDescriptor,
	criteria []SelectCriterion,
) error {
	for _, criterion := range criteria {
		if criterion.err != nil {
			return criterion.err
		}
		if !descriptorHasColumn(descriptor, criterion.column) {
			return fmt.Errorf("%w: %q on %s", ErrUnmappedSelectColumn, criterion.column, descriptor.typ)
		}
	}
	return nil
}

func descriptorHasColumn(descriptor *mapperDescriptor, column string) bool {
	for _, field := range descriptor.fields {
		if field.column == column {
			return true
		}
	}
	return false
}

func selectPlanID(
	descriptor *mapperDescriptor,
	criteria []SelectCriterion,
	limit *int,
) compiler.PlanID {
	var shape strings.Builder
	shape.WriteString("orm.select")
	writeSelectPlanPart(&shape, descriptor.typ.PkgPath())
	writeSelectPlanPart(&shape, descriptor.typ.Name())
	for _, criterion := range criteria {
		writeSelectPlanPart(&shape, criterion.column)
	}
	if limit != nil {
		shape.WriteString("/limit:")
		var number [20]byte
		shape.Write(strconv.AppendInt(number[:0], int64(*limit), 10))
	}
	return compiler.PlanID(shape.String())
}

func writeSelectPlanPart(shape *strings.Builder, part string) {
	var number [20]byte
	shape.WriteByte('/')
	shape.Write(strconv.AppendInt(number[:0], int64(len(part)), 10))
	shape.WriteByte(':')
	shape.WriteString(part)
}

func selectWhereSlotName(position int) string {
	return fmt.Sprintf("where_%d", position)
}

func (s *Session) preparedReadPlan(
	planID compiler.PlanID,
	statement sst.StatementNode,
) (compiler.CompiledStatement, error) {
	if s.readPlans == nil {
		s.readPlans = compiler.NewPlanRegistry()
	}
	if s.readCache == nil {
		s.readCache = compiler.NewStatementCache()
	}
	if plan, found := s.readPlans.Get(planID); found {
		return plan, nil
	}

	plan, err := compiler.Prepare(s.readCache, statement, dialect.NewDefaultDialect())
	if err != nil {
		return compiler.CompiledStatement{}, err
	}
	if err := s.readPlans.Put(planID, plan); err != nil {
		return compiler.CompiledStatement{}, err
	}
	return plan, nil
}
