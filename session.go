package sqlok

import (
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
)

var ErrIdentityConflict = errors.New("identity map conflict: another object with the same ID already exists in the session")

// Session represents the Unit of Work. It tracks object states and
// manages the identity of entities in memory.
type Session struct {
	// db is the underlying SQL database connection.
	db *sql.DB

	// identityMap ensures that only one instance of an entity exists in memory.
	// Structure: [reflect.Type][PrimaryKey] -> *ObjectPointer
	identityMap map[reflect.Type]map[any]any

	// snapshots stores the field-level hashes for each tracked object.
	// Structure: *ObjectPointer -> map[ColumnName]Hash (uint32)
	snapshots map[any]map[string]uint32

	// pending holds new objects that have been Added but not yet Inserted into the DB.
	pending []any
}

// NewSession initializes a new Unit of Work with empty maps.
func NewSession(db *sql.DB) *Session {
	return &Session{
		db:          db,
		identityMap: make(map[reflect.Type]map[any]any),
		snapshots:   make(map[any]map[string]uint32),
	}
}

// Add registers an entity into the session's identity map.
// If the entity has no primary key, it is added to the pending queue for INSERT.
func (s *Session) Add(ent any) error {
	v := reflect.ValueOf(ent)
	if v.Kind() != reflect.Ptr {
		return errors.New("only pointers to structs can be added to session")
	}

	t := v.Type().Elem()
	id := s.getPrimaryKey(ent)

	if id != nil {
		if s.identityMap[t] == nil {
			s.identityMap[t] = make(map[any]any)
		}

		if existing, ok := s.identityMap[t][id]; ok {
			if existing != ent {
				// TODO: Future - implement merge strategy here
				return ErrIdentityConflict
			}
			return nil // Object already tracked, skipping.
		}

		// Register the pointer
		s.identityMap[t][id] = ent

		// Take the initial "snapshot" for dirty checking later
		// s.takeSnapshot(ent)
		return nil
	}

	// No ID? It's a new entity, queue for Flush -> INSERT
	s.pending = append(s.pending, ent)

	return nil
}

// Load retrieves an entity of type T by its primary key from the session's identity map.
// If the entity is not found in the session, it returns nil, nil for now.
func Load[T any](s *Session, id any) (*T, error) {
	var t T
	reflectType := reflect.TypeOf(t)

	// Check the Identity Map first
	if typeMap, ok := s.identityMap[reflectType]; ok {
		if existing, found := typeMap[id]; found {
			return existing.(*T), nil
		}
	}

	// TODO: Future - Database lookup using Mapper and Builder
	return nil, nil
}

// getPrimaryKey scans for all fields tagged with 'pk' and returns a single or composite identity.
func (s *Session) getPrimaryKey(ent any) any {
	v := reflect.ValueOf(ent).Elem()
	pks := s.collectPKs(v)

	if len(pks) == 0 {
		return nil
	}

	// Simple PK: Return the single value (int, string, etc.)
	if len(pks) == 1 {
		return pks[0]
	}

	// Composite PK: Build a unique string key for the identity map.
	var sb strings.Builder
	sb.WriteString("composite:")
	for i, pk := range pks {
		if i > 0 {
			sb.WriteString("|")
		}
		sb.WriteString(fmt.Sprintf("%v", pk))
	}
	return sb.String()
}

// collectPKs recursively gathers all field values marked with 'pk'.
func (s *Session) collectPKs(v reflect.Value) []any {
	var pks []any
	t := v.Type()
	for i := range t.NumField() {
		field := t.Field(i)
		fieldVal := v.Field(i)

		// Support for embedded structs (Composition)
		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			pks = append(pks, s.collectPKs(fieldVal)...)
			continue
		}

		if tag := field.Tag.Get("sqlok"); tag == "pk" {
			val := s.extractValue(fieldVal)
			if val != nil {
				pks = append(pks, val)
			}
		}
	}
	return pks
}

// extractValue handles pointer vs value logic for PK fields.
func (s *Session) extractValue(v reflect.Value) any {
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil
		}
		return v.Elem().Interface()
	}
	if v.IsZero() {
		return nil
	}
	return v.Interface()
}
