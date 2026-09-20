package sqlok

import (
	"database/sql"
	"errors"
	"fmt"
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
