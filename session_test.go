package sqlok

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

type TestUserBase struct {
	Id int `sqlok:"pk"`
}

type TestUser struct {
	TestUserBase
	Name string
}

type TestPointerUser struct {
	Id   *int `sqlok:"pk"`
	Name string
}

func TestSession_Add(t *testing.T) {
	s := NewSession(nil)

	t.Run("Should add new object without PK to pending", func(t *testing.T) {
		user := &TestUser{Name: "New User"}
		err := s.Add(user)
		assert.NoError(t, err)
		assert.Contains(t, s.pending, user)
		assert.Empty(t, s.identityMap)
	})

	t.Run("Should add object with PK to identity map", func(t *testing.T) {
		user := &TestUser{TestUserBase: TestUserBase{Id: 1}, Name: "Existing User"}
		err := s.Add(user)
		assert.NoError(t, err)

		reflectType := reflect.TypeFor[TestUser]()
		assert.NotNil(t, s.identityMap[reflectType])
		assert.Equal(t, user, s.identityMap[reflectType][1])
	})

	t.Run("Should fail on identity conflict", func(t *testing.T) {
		// Reset session for clean test
		s = NewSession(nil)
		user1 := &TestUser{TestUserBase: TestUserBase{Id: 10}, Name: "User 1"}
		user2 := &TestUser{TestUserBase: TestUserBase{Id: 10}, Name: "User 2"}

		err := s.Add(user1)
		assert.NoError(t, err)

		err = s.Add(user2)
		assert.ErrorIs(t, err, ErrIdentityConflict)
	})

	t.Run("Should handle pointer PK correctly", func(t *testing.T) {
		s = NewSession(nil)
		id0 := 0
		user0 := &TestPointerUser{Id: &id0, Name: "User with ID 0"}
		err := s.Add(user0)
		assert.NoError(t, err)

		reflectType := reflect.TypeFor[TestPointerUser]()
		assert.Equal(t, user0, s.identityMap[reflectType][0])

		userNil := &TestPointerUser{Id: nil, Name: "User with Nil ID"}
		err = s.Add(userNil)
		assert.NoError(t, err)
		assert.Contains(t, s.pending, userNil)
	})
}
