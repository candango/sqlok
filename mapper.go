package sqlok

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"unicode"
)

var (
	// ErrMapperType reports a Mapper instantiated for a non-struct type.
	ErrMapperType = errors.New("mapper type must be a non-pointer struct")

	// ErrNoMappedFields reports an entity without exported mapped fields.
	ErrNoMappedFields = errors.New("mapper type has no mapped fields")

	// ErrDuplicateColumn reports two fields mapped to the same column.
	ErrDuplicateColumn = errors.New("mapper type has duplicate columns")

	// ErrNoPrimaryKey reports an entity mapping without a primary-key field.
	ErrNoPrimaryKey = errors.New("mapper type has no primary key")

	// ErrNilEntity reports an operation requiring a non-nil entity pointer.
	ErrNilEntity = errors.New("mapper entity cannot be nil")
)

// Scanner is the row-scanning behavior implemented by *sql.Row and *sql.Rows.
type Scanner interface {
	Scan(...any) error
}

// MappedValue is one mapped entity value in deterministic column order.
type MappedValue struct {
	Column  string
	Value   any
	Primary bool
}

// Mapper maps one Go struct type to database columns. Mapper metadata is
// immutable and shared by every Mapper instantiated for the same type.
type Mapper[T any] struct {
	descriptor *mapperDescriptor
}

type mappedField struct {
	column  string
	index   []int
	primary bool
	typ     reflect.Type
}

type mapperDescriptor struct {
	typ           reflect.Type
	table         string
	fields        []mappedField
	primaryFields []int
}

type descriptorEntry struct {
	once       sync.Once
	descriptor *mapperDescriptor
	err        error
}

var mapperDescriptors sync.Map

// NewMapper returns a Mapper backed by the immutable descriptor for T.
func NewMapper[T any]() (*Mapper[T], error) {
	typ := reflect.TypeFor[T]()
	descriptor, err := mapperDescriptorFor(typ)
	if err != nil {
		return nil, err
	}
	return &Mapper[T]{descriptor: descriptor}, nil
}

func mapperDescriptorFor(typ reflect.Type) (*mapperDescriptor, error) {
	if typ == nil || typ.Kind() != reflect.Struct {
		return nil, fmt.Errorf("%w: got %v", ErrMapperType, typ)
	}

	candidate := &descriptorEntry{}
	actual, _ := mapperDescriptors.LoadOrStore(typ, candidate)
	entry := actual.(*descriptorEntry)
	entry.once.Do(func() {
		entry.descriptor, entry.err = buildMapperDescriptor(typ)
	})
	return entry.descriptor, entry.err
}

func buildMapperDescriptor(typ reflect.Type) (*mapperDescriptor, error) {
	descriptor := &mapperDescriptor{
		typ:   typ,
		table: mapperTableName(typ),
	}
	columns := make(map[string]string)
	if err := appendMappedFields(descriptor, typ, nil, columns); err != nil {
		return nil, err
	}
	if len(descriptor.fields) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNoMappedFields, typ)
	}
	return descriptor, nil
}

func appendMappedFields(
	descriptor *mapperDescriptor,
	typ reflect.Type,
	prefix []int,
	columns map[string]string,
) error {
	for position := range typ.NumField() {
		field := typ.Field(position)
		if field.PkgPath != "" {
			continue
		}

		tag := parseMapperTag(field.Tag.Get("sqlok"))
		if tag.skip {
			continue
		}

		index := appendIndex(prefix, position)
		embeddedType := field.Type
		if embeddedType.Kind() == reflect.Pointer {
			embeddedType = embeddedType.Elem()
		}
		if field.Anonymous && embeddedType.Kind() == reflect.Struct && tag.column == "" {
			if err := appendMappedFields(descriptor, embeddedType, index, columns); err != nil {
				return err
			}
			continue
		}

		column := tag.column
		if column == "" {
			column = snakeCase(field.Name)
		}
		if previous, exists := columns[column]; exists {
			return fmt.Errorf(
				"%w: column %q maps both %s and %s",
				ErrDuplicateColumn,
				column,
				previous,
				field.Name,
			)
		}
		columns[column] = field.Name

		mappedPosition := len(descriptor.fields)
		descriptor.fields = append(descriptor.fields, mappedField{
			column:  column,
			index:   index,
			primary: tag.primary,
			typ:     field.Type,
		})
		if tag.primary {
			descriptor.primaryFields = append(
				descriptor.primaryFields,
				mappedPosition,
			)
		}
	}
	return nil
}

type mapperTag struct {
	column  string
	primary bool
	skip    bool
}

func parseMapperTag(value string) mapperTag {
	var tag mapperTag
	for _, option := range strings.Split(value, ",") {
		option = strings.TrimSpace(option)
		switch {
		case option == "-":
			tag.skip = true
		case option == "pk" || option == "primary_key":
			tag.primary = true
		case strings.HasPrefix(option, "column="):
			tag.column = strings.TrimSpace(strings.TrimPrefix(option, "column="))
		}
	}
	return tag
}

func appendIndex(prefix []int, position int) []int {
	index := make([]int, len(prefix)+1)
	copy(index, prefix)
	index[len(prefix)] = position
	return index
}

func mapperTableName(typ reflect.Type) string {
	candidate := reflect.New(typ).Interface()
	if named, ok := candidate.(interface{ TableName() string }); ok {
		if name := strings.TrimSpace(named.TableName()); name != "" {
			return name
		}
	}
	return snakeCase(typ.Name())
}

// Table returns the entity table name. A TableName method on *T overrides the
// default snake_case type name.
func (m *Mapper[T]) Table() string {
	if m == nil || m.descriptor == nil {
		return ""
	}
	return m.descriptor.table
}

// Columns returns mapped columns in deterministic scan order.
func (m *Mapper[T]) Columns() []string {
	if m == nil || m.descriptor == nil {
		return nil
	}
	columns := make([]string, len(m.descriptor.fields))
	for position, field := range m.descriptor.fields {
		columns[position] = field.column
	}
	return columns
}

// PrimaryColumns returns primary-key columns in deterministic declaration
// order.
func (m *Mapper[T]) PrimaryColumns() []string {
	if m == nil || m.descriptor == nil {
		return nil
	}
	columns := make([]string, len(m.descriptor.primaryFields))
	for position, fieldPosition := range m.descriptor.primaryFields {
		columns[position] = m.descriptor.fields[fieldPosition].column
	}
	return columns
}

// Scan creates one entity and populates it from scanner in Columns order.
func (m *Mapper[T]) Scan(scanner Scanner) (*T, error) {
	entity := new(T)
	if err := m.ScanInto(scanner, entity); err != nil {
		return nil, err
	}
	return entity, nil
}

// ScanInto populates entity from scanner in Columns order.
func (m *Mapper[T]) ScanInto(scanner Scanner, entity *T) error {
	if scanner == nil {
		return errors.New("mapper scanner cannot be nil")
	}
	if entity == nil {
		return ErrNilEntity
	}
	if m == nil || m.descriptor == nil {
		return errors.New("mapper cannot be nil")
	}

	root := reflect.ValueOf(entity).Elem()
	destinations := make([]any, len(m.descriptor.fields))
	for position, field := range m.descriptor.fields {
		value, err := writableMappedField(root, field.index)
		if err != nil {
			return fmt.Errorf("map column %q: %w", field.column, err)
		}
		destinations[position] = value.Addr().Interface()
	}
	if err := scanner.Scan(destinations...); err != nil {
		return fmt.Errorf("scan %s: %w", m.descriptor.typ, err)
	}
	return nil
}

// Values extracts mapped entity values in deterministic Columns order.
func (m *Mapper[T]) Values(entity *T) ([]MappedValue, error) {
	if entity == nil {
		return nil, ErrNilEntity
	}
	if m == nil || m.descriptor == nil {
		return nil, errors.New("mapper cannot be nil")
	}

	root := reflect.ValueOf(entity).Elem()
	values := make([]MappedValue, len(m.descriptor.fields))
	for position, field := range m.descriptor.fields {
		value, present := readableMappedField(root, field.index)
		var current any
		if present {
			current = value.Interface()
		}
		values[position] = MappedValue{
			Column:  field.column,
			Value:   current,
			Primary: field.primary,
		}
	}
	return values, nil
}

// PrimaryKey returns the entity identity and whether every primary-key field
// is present. Non-pointer zero values are treated as absent; pointer zero
// values are present when the pointer is non-nil.
func (m *Mapper[T]) PrimaryKey(entity *T) (any, bool, error) {
	if entity == nil {
		return nil, false, ErrNilEntity
	}
	if m == nil || m.descriptor == nil {
		return nil, false, errors.New("mapper cannot be nil")
	}
	return m.descriptor.primaryKey(reflect.ValueOf(entity).Elem())
}

func (d *mapperDescriptor) primaryKey(root reflect.Value) (any, bool, error) {
	if len(d.primaryFields) == 0 {
		return nil, false, fmt.Errorf("%w: %s", ErrNoPrimaryKey, d.typ)
	}

	values := make([]any, len(d.primaryFields))
	for position, fieldPosition := range d.primaryFields {
		field := d.fields[fieldPosition]
		value, present := readableMappedField(root, field.index)
		if !present {
			return nil, false, nil
		}

		wasPointer := value.Kind() == reflect.Pointer
		for value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
			if value.IsNil() {
				return nil, false, nil
			}
			value = value.Elem()
		}
		if !wasPointer && value.IsZero() {
			return nil, false, nil
		}
		current := value.Interface()
		if !reflect.TypeOf(current).Comparable() {
			return nil, false, fmt.Errorf(
				"primary key column %q has non-comparable type %T",
				field.column,
				current,
			)
		}
		values[position] = current
	}

	if len(values) == 1 {
		return values[0], true, nil
	}
	return encodeCompositeKey(values), true, nil
}

func encodeCompositeKey(values []any) string {
	var builder strings.Builder
	builder.WriteString("composite:")
	for _, value := range values {
		formatted := fmt.Sprintf("%T:%v", value, value)
		_, _ = fmt.Fprintf(&builder, "%d:%s", len(formatted), formatted)
	}
	return builder.String()
}

func writableMappedField(root reflect.Value, index []int) (reflect.Value, error) {
	current := root
	for offset, position := range index {
		for current.Kind() == reflect.Pointer {
			if current.IsNil() {
				if !current.CanSet() {
					return reflect.Value{}, errors.New("embedded pointer cannot be set")
				}
				current.Set(reflect.New(current.Type().Elem()))
			}
			current = current.Elem()
		}
		if current.Kind() != reflect.Struct {
			return reflect.Value{}, fmt.Errorf(
				"field path crosses non-struct %s at depth %d",
				current.Type(),
				offset,
			)
		}
		current = current.Field(position)
	}
	if !current.CanAddr() || !current.CanSet() {
		return reflect.Value{}, errors.New("mapped field is not writable")
	}
	return current, nil
}

func readableMappedField(root reflect.Value, index []int) (reflect.Value, bool) {
	current := root
	for _, position := range index {
		for current.Kind() == reflect.Pointer {
			if current.IsNil() {
				return reflect.Value{}, false
			}
			current = current.Elem()
		}
		if current.Kind() != reflect.Struct {
			return reflect.Value{}, false
		}
		current = current.Field(position)
	}
	return current, true
}

func snakeCase(value string) string {
	var builder strings.Builder
	runes := []rune(value)
	for position, current := range runes {
		if unicode.IsUpper(current) && position > 0 {
			previous := runes[position-1]
			nextLower := position+1 < len(runes) && unicode.IsLower(runes[position+1])
			if unicode.IsLower(previous) || unicode.IsDigit(previous) || nextLower {
				builder.WriteByte('_')
			}
		}
		builder.WriteRune(unicode.ToLower(current))
	}
	return builder.String()
}
