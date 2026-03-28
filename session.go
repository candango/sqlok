package sqlok

import (
	"database/sql"
	"errors"
	"reflect"
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

// NewSession initializes a new Unit of Work with empty maps to prevent panics.
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

// getPrimaryKey uses recursion to find the field tagged with 'pk'
func (s *Session) getPrimaryKey(ent any) any {
	v := reflect.ValueOf(ent).Elem()
	return s.findPK(v)
}

// findPK recursively searches for the field tagged with 'pk' and handles pointers.
func (s *Session) findPK(v reflect.Value) any {
	t := v.Type()
	for i := range t.NumField() {
		field := t.Field(i)
		fieldVal := v.Field(i)

		// Recursive check for embedded structs (Composition)
		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			if pk := s.findPK(fieldVal); pk != nil {
				return pk
			}
		}

		// Look for the short 'pk' tag
		if tag := field.Tag.Get("sqlok"); tag == "pk" {
			// Pointer logic: if pointer is nil, ID is unset.
			if fieldVal.Kind() == reflect.Ptr {
				if fieldVal.IsNil() {
					return nil
				}
				return fieldVal.Elem().Interface()
			}

			// Value logic: if IsZero, ID is unset.
			if fieldVal.IsZero() {
				return nil
			}

			return fieldVal.Interface()
		}
	}
	return nil
}
