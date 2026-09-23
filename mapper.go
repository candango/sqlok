package sqlok

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"unicode"
	"unsafe"
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

	// ErrNilMapperScanBuffer reports scanning with a nil reusable buffer.
	ErrNilMapperScanBuffer = errors.New("mapper scan buffer cannot be nil")

	// ErrNilMapperValueBuffer reports extracting values with a nil reusable buffer.
	ErrNilMapperValueBuffer = errors.New("mapper value buffer cannot be nil")
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

// MapperScanBuffer owns reusable scan destinations for one Mapper descriptor.
// A buffer must not be used by concurrent scans.
type MapperScanBuffer[T any] struct {
	descriptor   *mapperDescriptor
	destinations []any
}

// MapperValueBuffer owns reusable extracted values for one Mapper descriptor.
// Callers must consume ValuesInto's result before reusing the buffer.
type MapperValueBuffer[T any] struct {
	descriptor *mapperDescriptor
	values     []MappedValue
}

type mappedField struct {
	column      string
	index       []int
	primary     bool
	direct      bool
	offset      uintptr
	destination mapperDestinationKind
	typ         reflect.Type
}

// mapperDestinationKind identifies built-in field types that may use the
// unsafe fast path. It applies only to direct paths with no embedded pointers;
// all other fields retain reflection traversal.
type mapperDestinationKind uint8

const (
	mapperDestinationReflect mapperDestinationKind = iota
	mapperDestinationBool
	mapperDestinationInt
	mapperDestinationInt8
	mapperDestinationInt16
	mapperDestinationInt32
	mapperDestinationInt64
	mapperDestinationUint
	mapperDestinationUint8
	mapperDestinationUint16
	mapperDestinationUint32
	mapperDestinationUint64
	mapperDestinationUintptr
	mapperDestinationFloat32
	mapperDestinationFloat64
	mapperDestinationString
	mapperDestinationBytes
)

type mapperDescriptor struct {
	typ           reflect.Type
	table         string
	fields        []mappedField
	primaryFields []int
	valueTemplate []MappedValue
	scanBuffers   sync.Pool
}

type mapperScanDestinations struct {
	values []any
}

type descriptorEntry struct {
	once       sync.Once
	descriptor *mapperDescriptor
	err        error
}

var (
	mapperDescriptors = sync.Map{}
	mapperBoolType    = reflect.TypeFor[bool]()
	mapperIntType     = reflect.TypeFor[int]()
	mapperInt8Type    = reflect.TypeFor[int8]()
	mapperInt16Type   = reflect.TypeFor[int16]()
	mapperInt32Type   = reflect.TypeFor[int32]()
	mapperInt64Type   = reflect.TypeFor[int64]()
	mapperUintType    = reflect.TypeFor[uint]()
	mapperUint8Type   = reflect.TypeFor[uint8]()
	mapperUint16Type  = reflect.TypeFor[uint16]()
	mapperUint32Type  = reflect.TypeFor[uint32]()
	mapperUint64Type  = reflect.TypeFor[uint64]()
	mapperUintptrType = reflect.TypeFor[uintptr]()
	mapperFloat32Type = reflect.TypeFor[float32]()
	mapperFloat64Type = reflect.TypeFor[float64]()
	mapperStringType  = reflect.TypeFor[string]()
	mapperBytesType   = reflect.TypeFor[[]byte]()
)

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
	if actual, found := mapperDescriptors.Load(typ); found {
		entry := actual.(*descriptorEntry)
		return initializeMapperDescriptor(entry, typ)
	}

	candidate := &descriptorEntry{}
	actual, _ := mapperDescriptors.LoadOrStore(typ, candidate)
	return initializeMapperDescriptor(actual.(*descriptorEntry), typ)
}

func initializeMapperDescriptor(
	entry *descriptorEntry,
	typ reflect.Type,
) (*mapperDescriptor, error) {
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
	if err := appendMappedFields(descriptor, typ, nil, 0, false, columns); err != nil {
		return nil, err
	}
	if len(descriptor.fields) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNoMappedFields, typ)
	}
	fieldCount := len(descriptor.fields)
	descriptor.valueTemplate = make([]MappedValue, fieldCount)
	for position, field := range descriptor.fields {
		descriptor.valueTemplate[position] = MappedValue{
			Column:  field.column,
			Primary: field.primary,
		}
	}
	descriptor.scanBuffers.New = func() any {
		return &mapperScanDestinations{values: make([]any, fieldCount)}
	}
	return descriptor, nil
}

func appendMappedFields(
	descriptor *mapperDescriptor,
	typ reflect.Type,
	prefix []int,
	offset uintptr,
	pointerPath bool,
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
			if err := appendMappedFields(
				descriptor,
				embeddedType,
				index,
				offset+field.Offset,
				pointerPath || field.Type.Kind() == reflect.Pointer,
				columns,
			); err != nil {
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
			column:      column,
			index:       index,
			primary:     tag.primary,
			direct:      !pointerPath,
			offset:      offset + field.Offset,
			destination: mapperDestinationKindFor(field.Type),
			typ:         field.Type,
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

func mapperDestinationKindFor(typ reflect.Type) mapperDestinationKind {
	switch typ {
	case mapperBoolType:
		return mapperDestinationBool
	case mapperIntType:
		return mapperDestinationInt
	case mapperInt8Type:
		return mapperDestinationInt8
	case mapperInt16Type:
		return mapperDestinationInt16
	case mapperInt32Type:
		return mapperDestinationInt32
	case mapperInt64Type:
		return mapperDestinationInt64
	case mapperUintType:
		return mapperDestinationUint
	case mapperUint8Type:
		return mapperDestinationUint8
	case mapperUint16Type:
		return mapperDestinationUint16
	case mapperUint32Type:
		return mapperDestinationUint32
	case mapperUint64Type:
		return mapperDestinationUint64
	case mapperUintptrType:
		return mapperDestinationUintptr
	case mapperFloat32Type:
		return mapperDestinationFloat32
	case mapperFloat64Type:
		return mapperDestinationFloat64
	case mapperStringType:
		return mapperDestinationString
	case mapperBytesType:
		return mapperDestinationBytes
	default:
		return mapperDestinationReflect
	}
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
	if err := m.validateScan(scanner, entity); err != nil {
		return err
	}
	buffer := m.descriptor.scanBuffers.Get().(*mapperScanDestinations)
	err := m.scanInto(scanner, entity, buffer.values)
	m.descriptor.scanBuffers.Put(buffer)
	return err
}

// NewScanBuffer creates reusable destinations for scans through m. The buffer
// is execution-owned and must not be used concurrently.
func (m *Mapper[T]) NewScanBuffer() *MapperScanBuffer[T] {
	if m == nil || m.descriptor == nil {
		return nil
	}
	return &MapperScanBuffer[T]{
		descriptor:   m.descriptor,
		destinations: make([]any, len(m.descriptor.fields)),
	}
}

// ScanIntoWithBuffer populates entity while reusing buffer's destinations.
func (m *Mapper[T]) ScanIntoWithBuffer(
	scanner Scanner,
	entity *T,
	buffer *MapperScanBuffer[T],
) error {
	if err := m.validateScan(scanner, entity); err != nil {
		return err
	}
	if buffer == nil {
		return ErrNilMapperScanBuffer
	}
	if buffer.descriptor != m.descriptor {
		return errors.New("mapper scan buffer belongs to another mapper")
	}
	return m.scanInto(scanner, entity, buffer.destinations)
}

func (m *Mapper[T]) validateScan(scanner Scanner, entity *T) error {
	if scanner == nil {
		return errors.New("mapper scanner cannot be nil")
	}
	if entity == nil {
		return ErrNilEntity
	}
	if m == nil || m.descriptor == nil {
		return errors.New("mapper cannot be nil")
	}
	return nil
}

func (m *Mapper[T]) scanInto(
	scanner Scanner,
	entity *T,
	destinations []any,
) error {
	rootPointer := unsafe.Pointer(entity)
	var root reflect.Value
	for position, field := range m.descriptor.fields {
		if field.direct && field.destination != mapperDestinationReflect {
			destinations[position] = mapperScanDestination(rootPointer, field)
			continue
		}
		if !root.IsValid() {
			root = reflect.ValueOf(entity).Elem()
		}
		value, err := writableMappedField(root, field)
		if err != nil {
			clear(destinations)
			return fmt.Errorf("map column %q: %w", field.column, err)
		}
		destinations[position] = value.Addr().Interface()
	}
	err := scanner.Scan(destinations...)
	clear(destinations)
	if err != nil {
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

	values := append([]MappedValue(nil), m.descriptor.valueTemplate...)
	return m.extractValues(entity, values), nil
}

// NewValueBuffer creates reusable storage for values extracted through m. The
// buffer is execution-owned and must not be used concurrently.
func (m *Mapper[T]) NewValueBuffer() *MapperValueBuffer[T] {
	if m == nil || m.descriptor == nil {
		return nil
	}
	values := append([]MappedValue(nil), m.descriptor.valueTemplate...)
	return &MapperValueBuffer[T]{descriptor: m.descriptor, values: values}
}

// ValuesInto extracts mapped values while reusing buffer's storage. The
// returned slice becomes invalid when buffer is reused.
func (m *Mapper[T]) ValuesInto(
	entity *T,
	buffer *MapperValueBuffer[T],
) ([]MappedValue, error) {
	if entity == nil {
		return nil, ErrNilEntity
	}
	if m == nil || m.descriptor == nil {
		return nil, errors.New("mapper cannot be nil")
	}
	if buffer == nil {
		return nil, ErrNilMapperValueBuffer
	}
	if buffer.descriptor != m.descriptor {
		return nil, errors.New("mapper value buffer belongs to another mapper")
	}
	return m.extractValues(entity, buffer.values), nil
}

func (m *Mapper[T]) extractValues(entity *T, values []MappedValue) []MappedValue {
	rootPointer := unsafe.Pointer(entity)
	var root reflect.Value
	for position, field := range m.descriptor.fields {
		if field.direct && field.destination != mapperDestinationReflect {
			values[position].Value = mapperFieldValue(rootPointer, field)
			continue
		}
		if !root.IsValid() {
			root = reflect.ValueOf(entity).Elem()
		}
		value, present := readableMappedField(root, field)
		if present {
			values[position].Value = value.Interface()
			continue
		}
		values[position].Value = nil
	}
	return values
}

// mappedValues is the descriptor-owned reflection path used when the entity
// type is known only at runtime, such as Session.Flush.
func (d *mapperDescriptor) mappedValues(root reflect.Value) []MappedValue {
	values := append([]MappedValue(nil), d.valueTemplate...)
	for position, field := range d.fields {
		value, present := readableMappedField(root, field)
		if present {
			values[position].Value = value.Interface()
		}
	}
	return values
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
	if len(d.primaryFields) == 1 {
		return d.primaryKeyField(root, d.fields[d.primaryFields[0]])
	}

	values := make([]any, len(d.primaryFields))
	for position, fieldPosition := range d.primaryFields {
		current, present, err := d.primaryKeyField(root, d.fields[fieldPosition])
		if err != nil || !present {
			return current, present, err
		}
		values[position] = current
	}
	return encodeCompositeKey(values), true, nil
}

func (d *mapperDescriptor) primaryKeyField(
	root reflect.Value,
	field mappedField,
) (any, bool, error) {
	if field.direct && field.typ == mapperIntType {
		identity := int(directMappedField(root, field.index).Int())
		if identity == 0 {
			return nil, false, nil
		}
		return identity, true, nil
	}

	value, present := readableMappedField(root, field)
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
	return current, true, nil
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

// mapperFieldValue reads a direct built-in field. field.offset is the sum of
// value-embedded struct offsets, and root remains live for the whole call.
func mapperFieldValue(root unsafe.Pointer, field mappedField) any {
	pointer := unsafe.Add(root, field.offset)
	switch field.destination {
	case mapperDestinationBool:
		return *(*bool)(pointer)
	case mapperDestinationInt:
		return *(*int)(pointer)
	case mapperDestinationInt8:
		return *(*int8)(pointer)
	case mapperDestinationInt16:
		return *(*int16)(pointer)
	case mapperDestinationInt32:
		return *(*int32)(pointer)
	case mapperDestinationInt64:
		return *(*int64)(pointer)
	case mapperDestinationUint:
		return *(*uint)(pointer)
	case mapperDestinationUint8:
		return *(*uint8)(pointer)
	case mapperDestinationUint16:
		return *(*uint16)(pointer)
	case mapperDestinationUint32:
		return *(*uint32)(pointer)
	case mapperDestinationUint64:
		return *(*uint64)(pointer)
	case mapperDestinationUintptr:
		return *(*uintptr)(pointer)
	case mapperDestinationFloat32:
		return *(*float32)(pointer)
	case mapperDestinationFloat64:
		return *(*float64)(pointer)
	case mapperDestinationString:
		return *(*string)(pointer)
	case mapperDestinationBytes:
		return *(*[]byte)(pointer)
	default:
		panic("sqlok: unsupported mapper field value")
	}
}

// mapperScanDestination returns a typed pointer to a direct built-in field.
// The pointer is consumed synchronously by Scanner.Scan before destinations
// are cleared, so it cannot outlive entity.
func mapperScanDestination(root unsafe.Pointer, field mappedField) any {
	pointer := unsafe.Add(root, field.offset)
	switch field.destination {
	case mapperDestinationBool:
		return (*bool)(pointer)
	case mapperDestinationInt:
		return (*int)(pointer)
	case mapperDestinationInt8:
		return (*int8)(pointer)
	case mapperDestinationInt16:
		return (*int16)(pointer)
	case mapperDestinationInt32:
		return (*int32)(pointer)
	case mapperDestinationInt64:
		return (*int64)(pointer)
	case mapperDestinationUint:
		return (*uint)(pointer)
	case mapperDestinationUint8:
		return (*uint8)(pointer)
	case mapperDestinationUint16:
		return (*uint16)(pointer)
	case mapperDestinationUint32:
		return (*uint32)(pointer)
	case mapperDestinationUint64:
		return (*uint64)(pointer)
	case mapperDestinationUintptr:
		return (*uintptr)(pointer)
	case mapperDestinationFloat32:
		return (*float32)(pointer)
	case mapperDestinationFloat64:
		return (*float64)(pointer)
	case mapperDestinationString:
		return (*string)(pointer)
	case mapperDestinationBytes:
		return (*[]byte)(pointer)
	default:
		panic("sqlok: unsupported mapper scan destination")
	}
}

func directMappedField(root reflect.Value, index []int) reflect.Value {
	switch len(index) {
	case 1:
		return root.Field(index[0])
	case 2:
		return root.Field(index[0]).Field(index[1])
	case 3:
		return root.Field(index[0]).Field(index[1]).Field(index[2])
	default:
		return root.FieldByIndex(index)
	}
}

func writableMappedField(root reflect.Value, field mappedField) (reflect.Value, error) {
	if field.direct {
		value := directMappedField(root, field.index)
		if !value.CanAddr() || !value.CanSet() {
			return reflect.Value{}, errors.New("mapped field is not writable")
		}
		return value, nil
	}

	current := root
	for offset, position := range field.index {
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

func readableMappedField(root reflect.Value, field mappedField) (reflect.Value, bool) {
	if field.direct {
		return directMappedField(root, field.index), true
	}

	current := root
	for _, position := range field.index {
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
