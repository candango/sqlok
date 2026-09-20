package sqlok

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type MapperTestBase struct {
	ID int `sqlok:"column=id,primary_key"`
}

type MapperTestProfile struct {
	MapperTestBase
	DisplayName string `sqlok:"column=display_name"`
	Age         int
	Ignored     string `sqlok:"-"`
}

func (*MapperTestProfile) TableName() string {
	return "user_profiles"
}

type MapperPointerBase struct {
	ID *int `sqlok:"pk"`
}

type MapperPointerEntity struct {
	*MapperPointerBase
	Name string
}

type MapperCompositeEntity struct {
	TenantID int `sqlok:"primary_key"`
	UserID   int `sqlok:"primary_key"`
	Name     string
}

type mapperDuplicateColumns struct {
	First  int `sqlok:"column=value"`
	Second int `sqlok:"column=value"`
}

type mapperNoFields struct {
	hidden int
}

type mapperSliceScanner struct {
	values []any
	err    error
}

func (s mapperSliceScanner) Scan(destinations ...any) error {
	if s.err != nil {
		return s.err
	}
	if len(destinations) != len(s.values) {
		return fmt.Errorf(
			"scanner received %d destinations; want %d",
			len(destinations),
			len(s.values),
		)
	}
	for position, destination := range destinations {
		target := reflect.ValueOf(destination)
		if target.Kind() != reflect.Pointer || target.IsNil() {
			return fmt.Errorf("destination %d is not a pointer", position)
		}
		value := s.values[position]
		if value == nil {
			target.Elem().SetZero()
			continue
		}
		current := reflect.ValueOf(value)
		destination := target.Elem()
		if destination.Kind() == reflect.Pointer {
			pointed := reflect.New(destination.Type().Elem())
			if current.Type().AssignableTo(pointed.Elem().Type()) {
				pointed.Elem().Set(current)
				destination.Set(pointed)
				continue
			}
			if current.Type().ConvertibleTo(pointed.Elem().Type()) {
				pointed.Elem().Set(current.Convert(pointed.Elem().Type()))
				destination.Set(pointed)
				continue
			}
		}
		if current.Type().AssignableTo(destination.Type()) {
			destination.Set(current)
			continue
		}
		if current.Type().ConvertibleTo(destination.Type()) {
			destination.Set(current.Convert(destination.Type()))
			continue
		}
		return fmt.Errorf(
			"value %d has type %s; want %s",
			position,
			current.Type(),
			target.Elem().Type(),
		)
	}
	return nil
}

func TestMapperBuildsDeterministicMetadata(t *testing.T) {
	mapper, err := NewMapper[MapperTestProfile]()
	require.NoError(t, err)

	assert.Equal(t, "user_profiles", mapper.Table())
	assert.Equal(t, []string{"id", "display_name", "age"}, mapper.Columns())
	assert.Equal(t, []string{"id"}, mapper.PrimaryColumns())

	columns := mapper.Columns()
	columns[0] = "mutated"
	assert.Equal(t, []string{"id", "display_name", "age"}, mapper.Columns())
}

func TestMapperScansEmbeddedFieldsAndExtractsValues(t *testing.T) {
	mapper, err := NewMapper[MapperTestProfile]()
	require.NoError(t, err)

	entity, err := mapper.Scan(mapperSliceScanner{values: []any{7, "Ana", 42}})
	require.NoError(t, err)
	assert.Equal(t, MapperTestProfile{
		MapperTestBase: MapperTestBase{ID: 7},
		DisplayName:    "Ana",
		Age:            42,
	}, *entity)

	identity, present, err := mapper.PrimaryKey(entity)
	require.NoError(t, err)
	assert.True(t, present)
	assert.Equal(t, 7, identity)

	values, err := mapper.Values(entity)
	require.NoError(t, err)
	assert.Equal(t, []MappedValue{
		{Column: "id", Value: 7, Primary: true},
		{Column: "display_name", Value: "Ana"},
		{Column: "age", Value: 42},
	}, values)
}

func TestMapperReusesScanAndValueBuffers(t *testing.T) {
	mapper, err := NewMapper[MapperTestProfile]()
	require.NoError(t, err)

	scanBuffer := mapper.NewScanBuffer()
	entity := new(MapperTestProfile)
	require.NoError(t, mapper.ScanIntoWithBuffer(
		mapperSliceScanner{values: []any{7, "Ana", 42}},
		entity,
		scanBuffer,
	))
	assert.Equal(t, MapperTestProfile{
		MapperTestBase: MapperTestBase{ID: 7},
		DisplayName:    "Ana",
		Age:            42,
	}, *entity)
	assert.Equal(t, []any{nil, nil, nil}, scanBuffer.destinations)
	assert.ErrorIs(t, mapper.ScanIntoWithBuffer(
		mapperSliceScanner{},
		entity,
		nil,
	), ErrNilMapperScanBuffer)

	valueBuffer := mapper.NewValueBuffer()
	values, err := mapper.ValuesInto(entity, valueBuffer)
	require.NoError(t, err)
	assert.Equal(t, []MappedValue{
		{Column: "id", Value: 7, Primary: true},
		{Column: "display_name", Value: "Ana"},
		{Column: "age", Value: 42},
	}, values)
	assert.ErrorIs(t, func() error {
		_, err := mapper.ValuesInto(entity, nil)
		return err
	}(), ErrNilMapperValueBuffer)
}

func TestMapperScansConcurrently(t *testing.T) {
	mapper, err := NewMapper[MapperTestProfile]()
	require.NoError(t, err)

	const workers = 16
	errCh := make(chan error, workers)
	var wait sync.WaitGroup
	for worker := range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			entity, scanErr := mapper.Scan(mapperSliceScanner{values: []any{
				worker + 1,
				"worker",
				worker + 100,
			}})
			if scanErr != nil {
				errCh <- scanErr
				return
			}
			if entity.ID != worker+1 || entity.DisplayName != "worker" ||
				entity.Age != worker+100 {
				errCh <- fmt.Errorf("worker %d received another scan result", worker)
			}
		}()
	}
	wait.Wait()
	close(errCh)
	for scanErr := range errCh {
		assert.NoError(t, scanErr)
	}
}

func TestMapperAllocatesEmbeddedPointerDuringScan(t *testing.T) {
	mapper, err := NewMapper[MapperPointerEntity]()
	require.NoError(t, err)

	entity, err := mapper.Scan(mapperSliceScanner{values: []any{0, "zero"}})
	require.NoError(t, err)
	require.NotNil(t, entity.MapperPointerBase)
	require.NotNil(t, entity.ID)
	assert.Equal(t, 0, *entity.ID)

	identity, present, err := mapper.PrimaryKey(entity)
	require.NoError(t, err)
	assert.True(t, present)
	assert.Equal(t, 0, identity)
}

func TestMapperReportsMissingAndCompositePrimaryKeys(t *testing.T) {
	pointerMapper, err := NewMapper[MapperPointerEntity]()
	require.NoError(t, err)

	identity, present, err := pointerMapper.PrimaryKey(&MapperPointerEntity{})
	require.NoError(t, err)
	assert.False(t, present)
	assert.Nil(t, identity)

	compositeMapper, err := NewMapper[MapperCompositeEntity]()
	require.NoError(t, err)
	identity, present, err = compositeMapper.PrimaryKey(&MapperCompositeEntity{
		TenantID: 3,
		UserID:   9,
	})
	require.NoError(t, err)
	assert.True(t, present)
	assert.Equal(t, "composite:5:int:35:int:9", identity)
}

func TestMapperRejectsInvalidMappings(t *testing.T) {
	_, err := NewMapper[int]()
	assert.ErrorIs(t, err, ErrMapperType)

	_, err = NewMapper[mapperNoFields]()
	assert.ErrorIs(t, err, ErrNoMappedFields)

	_, err = NewMapper[mapperDuplicateColumns]()
	assert.ErrorIs(t, err, ErrDuplicateColumn)

	type noPrimaryKey struct {
		Name string
	}
	mapper, err := NewMapper[noPrimaryKey]()
	require.NoError(t, err)
	_, _, err = mapper.PrimaryKey(&noPrimaryKey{Name: "Ana"})
	assert.ErrorIs(t, err, ErrNoPrimaryKey)
}

func TestMapperReturnsActionableScanErrors(t *testing.T) {
	mapper, err := NewMapper[MapperTestProfile]()
	require.NoError(t, err)

	scanErr := errors.New("driver conversion failed")
	_, err = mapper.Scan(mapperSliceScanner{err: scanErr})
	assert.ErrorIs(t, err, scanErr)
	assert.Contains(t, err.Error(), "MapperTestProfile")

	assert.ErrorIs(t, mapper.ScanInto(mapperSliceScanner{}, nil), ErrNilEntity)
}

func TestMapperReusesImmutableDescriptorConcurrently(t *testing.T) {
	first, err := NewMapper[MapperTestProfile]()
	require.NoError(t, err)
	second, err := NewMapper[MapperTestProfile]()
	require.NoError(t, err)
	assert.Same(t, first.descriptor, second.descriptor)

	const workers = 16
	var wait sync.WaitGroup
	for worker := range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			entity := &MapperTestProfile{
				MapperTestBase: MapperTestBase{ID: worker + 1},
				DisplayName:    "worker",
			}
			values, valueErr := first.Values(entity)
			assert.NoError(t, valueErr)
			assert.Len(t, values, 3)
		}()
	}
	wait.Wait()
}
