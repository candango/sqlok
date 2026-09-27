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
	ErrNilSelectQuery         = errors.New("select query cannot be nil")
	ErrNilSelectContext       = errors.New("select context cannot be nil")
	ErrEmptySelectColumn      = errors.New("select criterion column cannot be empty")
	ErrUnmappedSelectColumn   = errors.New("select criterion column is not mapped")
	ErrEmptySelectCriteria    = errors.New("select requires at least one criterion")
	ErrEmptySelectProjection  = errors.New("select projection requires at least one mapped column")
	ErrNilSelectValue         = errors.New("comparison criteria do not support NULL values; use IsNull or IsNotNull")
	ErrNoSelectRows           = errors.New("select query returned no rows")
	ErrMultipleSelectRows     = errors.New("select query returned more than one row")
	ErrScalarSelectProjection = errors.New("scalar SELECT requires exactly one projected column")
)

type selectCriterionOperator uint8

const (
	selectCriterionEqual selectCriterionOperator = iota
	selectCriterionNotEqual
	selectCriterionGreaterThan
	selectCriterionGreaterThanOrEqual
	selectCriterionLessThan
	selectCriterionLessThanOrEqual
	selectCriterionIsNull
	selectCriterionIsNotNull
)

func (operator selectCriterionOperator) isNull() bool {
	return operator == selectCriterionIsNull || operator == selectCriterionIsNotNull
}

func (operator selectCriterionOperator) comparison() sst.ComparisonOperator {
	switch operator {
	case selectCriterionNotEqual:
		return sst.NotEqual
	case selectCriterionGreaterThan:
		return sst.GreaterThan
	case selectCriterionGreaterThanOrEqual:
		return sst.GreaterThanOrEqual
	case selectCriterionLessThan:
		return sst.LessThan
	case selectCriterionLessThanOrEqual:
		return sst.LessThanOrEqual
	default:
		return sst.Equal
	}
}

func (operator selectCriterionOperator) nullOperator() sst.NullOperator {
	if operator == selectCriterionIsNotNull {
		return sst.IsNotNull
	}
	return sst.IsNull
}

func (operator selectCriterionOperator) planPart() string {
	switch operator {
	case selectCriterionNotEqual:
		return "ne"
	case selectCriterionGreaterThan:
		return "gt"
	case selectCriterionGreaterThanOrEqual:
		return "gte"
	case selectCriterionLessThan:
		return "lt"
	case selectCriterionLessThanOrEqual:
		return "lte"
	case selectCriterionIsNull:
		return "is-null"
	case selectCriterionIsNotNull:
		return "is-not-null"
	default:
		return "eq"
	}
}

type selectCriterionError uint8

const (
	selectCriterionNoError selectCriterionError = iota
	selectCriterionEmptyColumn
	selectCriterionNilValue
)

func (criterionError selectCriterionError) error() error {
	switch criterionError {
	case selectCriterionEmptyColumn:
		return ErrEmptySelectColumn
	case selectCriterionNilValue:
		return ErrNilSelectValue
	default:
		return nil
	}
}

// SelectCriterion is a predicate accepted by SelectQuery.Where.
// Construct criteria with comparison functions such as Eq or null predicates
// such as IsNull.
type SelectCriterion struct {
	column   string
	value    any
	operator selectCriterionOperator
	err      selectCriterionError
}

// Eq creates a bound equality criterion for a mapped database column.
func Eq(column string, value any) SelectCriterion {
	return comparisonCriterion(column, selectCriterionEqual, value)
}

// Ne creates a bound not-equal criterion for a mapped database column.
func Ne(column string, value any) SelectCriterion {
	return comparisonCriterion(column, selectCriterionNotEqual, value)
}

// Gt creates a bound greater-than criterion for a mapped database column.
func Gt(column string, value any) SelectCriterion {
	return comparisonCriterion(column, selectCriterionGreaterThan, value)
}

// Gte creates a bound greater-than-or-equal criterion for a mapped database
// column.
func Gte(column string, value any) SelectCriterion {
	return comparisonCriterion(column, selectCriterionGreaterThanOrEqual, value)
}

// Lt creates a bound less-than criterion for a mapped database column.
func Lt(column string, value any) SelectCriterion {
	return comparisonCriterion(column, selectCriterionLessThan, value)
}

// Lte creates a bound less-than-or-equal criterion for a mapped database
// column.
func Lte(column string, value any) SelectCriterion {
	return comparisonCriterion(column, selectCriterionLessThanOrEqual, value)
}

// IsNull creates an IS NULL criterion for a mapped database column.
func IsNull(column string) SelectCriterion {
	return nullCriterion(column, selectCriterionIsNull)
}

// IsNotNull creates an IS NOT NULL criterion for a mapped database column.
func IsNotNull(column string) SelectCriterion {
	return nullCriterion(column, selectCriterionIsNotNull)
}

func comparisonCriterion(
	column string,
	operator selectCriterionOperator,
	value any,
) SelectCriterion {
	column = strings.TrimSpace(column)
	if column == "" {
		return SelectCriterion{err: selectCriterionEmptyColumn}
	}
	if isNilSelectValue(value) {
		return SelectCriterion{err: selectCriterionNilValue}
	}
	return SelectCriterion{
		column:   column,
		value:    value,
		operator: operator,
	}
}

func nullCriterion(column string, operator selectCriterionOperator) SelectCriterion {
	column = strings.TrimSpace(column)
	if column == "" {
		return SelectCriterion{err: selectCriterionEmptyColumn}
	}
	return SelectCriterion{column: column, operator: operator}
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

// SelectProjection is a mapped-column SELECT query that returns rows or scalar
// values instead of entities.
type SelectProjection struct {
	descriptor *mapperDescriptor
	columns    []string
	criteria   []SelectCriterion
	err        error
}

// SelectRow contains one projected result row in requested column order.
type SelectRow struct {
	columns []string
	values  []any
}

// Columns returns the projected column names in result order.
func (r SelectRow) Columns() []string {
	return append([]string(nil), r.columns...)
}

// Values returns the projected values in column order.
func (r SelectRow) Values() []any {
	return append([]any(nil), r.values...)
}

// Value returns one projected value by mapped column name.
func (r SelectRow) Value(column string) (any, bool) {
	column = strings.TrimSpace(column)
	for position, name := range r.columns {
		if name == column {
			return r.values[position], true
		}
	}
	return nil, false
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

// Columns starts a mapped-column projection from the entity query.
func (q SelectQuery[T]) Columns(columns ...string) SelectProjection {
	projection := SelectProjection{
		descriptor: q.descriptor,
		criteria:   append([]SelectCriterion(nil), q.criteria...),
		err:        q.err,
	}
	if projection.err != nil {
		return projection
	}
	if projection.descriptor == nil {
		projection.err = ErrNilSelectQuery
		return projection
	}
	projection.columns, projection.err = validateSelectProjection(
		projection.descriptor,
		columns,
	)
	return projection
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
	next.criteria, next.err = appendSelectCriteria(q.descriptor, q.criteria, criteria)
	return next
}

// Where returns a new projection query with criteria combined using AND.
func (q SelectProjection) Where(criteria ...SelectCriterion) SelectProjection {
	if q.err != nil {
		return q
	}
	if q.descriptor == nil {
		q.err = ErrNilSelectQuery
		return q
	}

	next := q
	next.criteria, next.err = appendSelectCriteria(q.descriptor, q.criteria, criteria)
	return next
}

func appendSelectCriteria(
	descriptor *mapperDescriptor,
	current []SelectCriterion,
	criteria []SelectCriterion,
) ([]SelectCriterion, error) {
	if len(criteria) == 0 {
		return nil, ErrEmptySelectCriteria
	}

	next := make([]SelectCriterion, 0, len(current)+len(criteria))
	next = append(next, current...)
	next = append(next, criteria...)
	if err := validateSelectCriteria(descriptor, next); err != nil {
		return nil, err
	}
	return next, nil
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
	planID := selectPlanID(q.descriptor, nil, q.criteria, nil)
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

// All executes the projection and returns rows in the requested column order.
func (q SelectProjection) All(
	ctx context.Context,
	session *Session,
) ([]SelectRow, error) {
	if q.err != nil {
		return nil, q.err
	}
	if q.descriptor == nil {
		return nil, ErrNilSelectQuery
	}
	planID := selectPlanID(q.descriptor, q.columns, q.criteria, nil)
	rows, err := queryRows(ctx, session, q.descriptor, q.columns, q.criteria, planID, nil)
	if err != nil {
		return nil, err
	}

	result := make([]SelectRow, 0)
	for rows.Next() {
		row, err := scanSelectRow(rows, q.columns)
		if err != nil {
			return nil, closeSelectRows(rows, err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, closeSelectRows(rows, fmt.Errorf("iterate projected SELECT rows: %w", err))
	}
	if err := closeSelectRows(rows, nil); err != nil {
		return nil, err
	}
	return result, nil
}

// Scalars executes a single-column projection and returns its values.
func (q SelectProjection) Scalars(
	ctx context.Context,
	session *Session,
) ([]any, error) {
	if q.err != nil {
		return nil, q.err
	}
	if q.descriptor == nil {
		return nil, ErrNilSelectQuery
	}
	if len(q.columns) != 1 {
		return nil, ErrScalarSelectProjection
	}
	planID := selectPlanID(q.descriptor, q.columns, q.criteria, nil)
	rows, err := queryRows(ctx, session, q.descriptor, q.columns, q.criteria, planID, nil)
	if err != nil {
		return nil, err
	}

	values := make([]any, 0)
	for rows.Next() {
		var value any
		if err := rows.Scan(&value); err != nil {
			return nil, closeSelectRows(rows, fmt.Errorf("scan scalar SELECT value: %w", err))
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, closeSelectRows(rows, fmt.Errorf("iterate scalar SELECT rows: %w", err))
	}
	if err := closeSelectRows(rows, nil); err != nil {
		return nil, err
	}
	return values, nil
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
	planID := selectPlanID(q.descriptor, nil, q.criteria, &limit)
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

func scanSelectRow(rows selectRows, columns []string) (SelectRow, error) {
	values := make([]any, len(columns))
	destinations := make([]any, len(values))
	for position := range values {
		destinations[position] = &values[position]
	}
	if err := rows.Scan(destinations...); err != nil {
		return SelectRow{}, fmt.Errorf("scan projected SELECT row: %w", err)
	}
	return SelectRow{
		columns: append([]string(nil), columns...),
		values:  values,
	}, nil
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
	return queryRows(ctx, session, q.descriptor, nil, q.criteria, planID, limit)
}

func queryRows(
	ctx context.Context,
	session *Session,
	descriptor *mapperDescriptor,
	columns []string,
	criteria []SelectCriterion,
	planID compiler.PlanID,
	limit *int,
) (selectRows, error) {
	if ctx == nil {
		return nil, ErrNilSelectContext
	}
	if session == nil {
		return nil, ErrNilSession
	}
	target, err := session.selectExecutor(ctx)
	if err != nil {
		return nil, err
	}

	plan, found := session.readPlans.Get(planID)
	if !found {
		statement, err := buildSelectStatement(descriptor, columns, criteria, limit)
		if err != nil {
			return nil, fmt.Errorf("build SELECT query: %w", err)
		}
		plan, err = session.preparedReadPlan(planID, statement)
		if err != nil {
			return nil, fmt.Errorf("prepare SELECT query: %w", err)
		}
	}
	arguments := plan.NewArgumentBuffer()
	for position, criterion := range criteria {
		if criterion.operator.isNull() {
			continue
		}
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

	rows, err := executor.Query(ctx, target, plan, arguments)
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
	selectedColumns []string,
	criteria []SelectCriterion,
	limit *int,
) (*dql.SelectStatement, error) {
	if descriptor == nil {
		return nil, errors.New("select mapper cannot be nil")
	}

	columns, err := selectProjectionExpressions(descriptor, selectedColumns)
	if err != nil {
		return nil, err
	}
	statement := dql.Select(columns...).From(sst.NewTableRef(descriptor.table))

	if err := validateSelectCriteria(descriptor, criteria); err != nil {
		return nil, err
	}
	for position, criterion := range criteria {
		column := sst.NewColumnRef(descriptor.table, criterion.column)
		if criterion.operator.isNull() {
			statement.Where(sst.NewNullExpression(column, criterion.operator.nullOperator()))
			continue
		}
		statement.Where(sst.NewBinaryExpression(
			column,
			sst.NewNamedParameterSlot(selectWhereSlotName(position)),
			criterion.operator.comparison(),
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

func validateSelectProjection(
	descriptor *mapperDescriptor,
	columns []string,
) ([]string, error) {
	if len(columns) == 0 {
		return nil, ErrEmptySelectProjection
	}

	normalized := make([]string, 0, len(columns))
	for _, column := range columns {
		column = strings.TrimSpace(column)
		if column == "" {
			return nil, ErrEmptySelectColumn
		}
		if !descriptorHasColumn(descriptor, column) {
			return nil, fmt.Errorf("%w: %q on %s", ErrUnmappedSelectColumn, column, descriptor.typ)
		}
		normalized = append(normalized, column)
	}
	return normalized, nil
}

func selectProjectionExpressions(
	descriptor *mapperDescriptor,
	selectedColumns []string,
) ([]sst.ExpressionNode, error) {
	if selectedColumns == nil {
		selectedColumns = make([]string, 0, len(descriptor.fields))
		for _, field := range descriptor.fields {
			selectedColumns = append(selectedColumns, field.column)
		}
	} else {
		var err error
		selectedColumns, err = validateSelectProjection(descriptor, selectedColumns)
		if err != nil {
			return nil, err
		}
	}

	columns := make([]sst.ExpressionNode, len(selectedColumns))
	for position, column := range selectedColumns {
		columns[position] = sst.NewColumnRef(descriptor.table, column)
	}
	return columns, nil
}

func validateSelectCriteria(
	descriptor *mapperDescriptor,
	criteria []SelectCriterion,
) error {
	for _, criterion := range criteria {
		if err := criterion.err.error(); err != nil {
			return err
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
	selectedColumns []string,
	criteria []SelectCriterion,
	limit *int,
) compiler.PlanID {
	var shape strings.Builder
	shape.WriteString("orm.select")
	writeSelectPlanPart(&shape, descriptor.typ.PkgPath())
	writeSelectPlanPart(&shape, descriptor.typ.Name())
	if selectedColumns != nil {
		writeSelectPlanPart(&shape, "projection")
		for _, column := range selectedColumns {
			writeSelectPlanPart(&shape, column)
		}
	}
	for _, criterion := range criteria {
		writeSelectPlanPart(&shape, criterion.column)
		writeSelectPlanPart(&shape, criterion.operator.planPart())
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
