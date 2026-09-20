package sqlok

import (
	"fmt"
	"reflect"
	"testing"
)

type MapperBenchmarkBase struct {
	ID int `sqlok:"primary_key"`
}

type MapperBenchmarkEntity struct {
	MapperBenchmarkBase
	Name   string
	Active bool
}

type mapperBenchmarkScanner struct {
	id     int
	name   string
	active bool
}

func (s *mapperBenchmarkScanner) Scan(destinations ...any) error {
	*destinations[0].(*int) = s.id
	*destinations[1].(*string) = s.name
	*destinations[2].(*bool) = s.active
	return nil
}

var (
	mapperBenchmarkDescriptor  *mapperDescriptor
	mapperBenchmarkMapper      *Mapper[MapperBenchmarkEntity]
	mapperBenchmarkScanBuffer  *MapperScanBuffer[MapperBenchmarkEntity]
	mapperBenchmarkValueBuffer *MapperValueBuffer[MapperBenchmarkEntity]
	mapperBenchmarkEntitySink  *MapperBenchmarkEntity
	mapperBenchmarkValues      []MappedValue
	mapperBenchmarkIdentity    any
	mapperBenchmarkPresent     bool
	mapperBenchmarkErr         error
)

func BenchmarkMapperDescriptor(b *testing.B) {
	typ := reflect.TypeFor[MapperBenchmarkEntity]()

	b.Run("cold", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			mapperBenchmarkDescriptor, mapperBenchmarkErr = buildMapperDescriptor(typ)
		}
	})

	b.Run("cached", func(b *testing.B) {
		mapper, err := NewMapper[MapperBenchmarkEntity]()
		if err != nil {
			b.Fatal(err)
		}
		mapperBenchmarkMapper = mapper

		b.ReportAllocs()
		for b.Loop() {
			mapperBenchmarkMapper, mapperBenchmarkErr = NewMapper[MapperBenchmarkEntity]()
		}
	})
}

func BenchmarkMapperRows(b *testing.B) {
	for _, rows := range []int{1, 100, 1_000} {
		b.Run(fmt.Sprintf("handwritten/rows=%d", rows), func(b *testing.B) {
			scanner := &mapperBenchmarkScanner{name: "Ana", active: true}
			b.ReportAllocs()
			b.ReportMetric(float64(rows), "rows/op")
			b.ResetTimer()
			for b.Loop() {
				for row := range rows {
					scanner.id = row + 1
					entity := new(MapperBenchmarkEntity)
					mapperBenchmarkErr = scanner.Scan(
						&entity.ID,
						&entity.Name,
						&entity.Active,
					)
					mapperBenchmarkEntitySink = entity
				}
			}
		})

		b.Run(fmt.Sprintf("mapper/rows=%d", rows), func(b *testing.B) {
			mapper, err := NewMapper[MapperBenchmarkEntity]()
			if err != nil {
				b.Fatal(err)
			}
			scanner := &mapperBenchmarkScanner{name: "Ana", active: true}
			b.ReportAllocs()
			b.ReportMetric(float64(rows), "rows/op")
			b.ResetTimer()
			for b.Loop() {
				for row := range rows {
					scanner.id = row + 1
					mapperBenchmarkEntitySink, mapperBenchmarkErr = mapper.Scan(scanner)
				}
			}
		})

		b.Run(fmt.Sprintf("mapper-buffer/rows=%d", rows), func(b *testing.B) {
			mapper, err := NewMapper[MapperBenchmarkEntity]()
			if err != nil {
				b.Fatal(err)
			}
			mapperBenchmarkScanBuffer = mapper.NewScanBuffer()
			scanner := &mapperBenchmarkScanner{name: "Ana", active: true}
			b.ReportAllocs()
			b.ReportMetric(float64(rows), "rows/op")
			b.ResetTimer()
			for b.Loop() {
				for row := range rows {
					scanner.id = row + 1
					entity := new(MapperBenchmarkEntity)
					mapperBenchmarkErr = mapper.ScanIntoWithBuffer(
						scanner,
						entity,
						mapperBenchmarkScanBuffer,
					)
					mapperBenchmarkEntitySink = entity
				}
			}
		})
	}
}

func BenchmarkMapperExtraction(b *testing.B) {
	mapper, err := NewMapper[MapperBenchmarkEntity]()
	if err != nil {
		b.Fatal(err)
	}
	entity := &MapperBenchmarkEntity{
		MapperBenchmarkBase: MapperBenchmarkBase{ID: 42},
		Name:                "Ana",
		Active:              true,
	}

	b.Run("handwritten-values", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			mapperBenchmarkValues = []MappedValue{
				{Column: "id", Value: entity.ID, Primary: true},
				{Column: "name", Value: entity.Name},
				{Column: "active", Value: entity.Active},
			}
		}
	})

	b.Run("mapper-values", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			mapperBenchmarkValues, mapperBenchmarkErr = mapper.Values(entity)
		}
	})

	b.Run("mapper-values-buffer", func(b *testing.B) {
		mapperBenchmarkValueBuffer = mapper.NewValueBuffer()
		b.ReportAllocs()
		for b.Loop() {
			mapperBenchmarkValues, mapperBenchmarkErr = mapper.ValuesInto(
				entity,
				mapperBenchmarkValueBuffer,
			)
		}
	})

	b.Run("handwritten-primary-key", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			mapperBenchmarkIdentity = entity.ID
			mapperBenchmarkPresent = entity.ID != 0
		}
	})

	b.Run("mapper-primary-key", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			mapperBenchmarkIdentity, mapperBenchmarkPresent, mapperBenchmarkErr =
				mapper.PrimaryKey(entity)
		}
	})
}
