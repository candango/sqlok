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

type TestCompositeUser struct {
	OrgId  int `sqlok:"pk"`
	UserId int `sqlok:"pk"`
	Name   string
}

type TestPrimaryKeyTagUser struct {
	ID   int `sqlok:"primary_key"`
	Name string
}

type TestUnkeyedUser struct {
	Name string
}

type TestInvalidPrimaryKeyUser struct {
	ID []int `sqlok:"pk"`
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

	t.Run("Should handle composite PK correctly", func(t *testing.T) {
		s = NewSession(nil)
		user := &TestCompositeUser{OrgId: 1, UserId: 100, Name: "Joint User"}
		err := s.Add(user)
		assert.NoError(t, err)

		mapper, mapperErr := NewMapper[TestCompositeUser]()
		assert.NoError(t, mapperErr)
		compositeKey, present, keyErr := mapper.PrimaryKey(user)
		assert.NoError(t, keyErr)
		assert.True(t, present)

		reflectType := reflect.TypeOf(TestCompositeUser{})
		assert.Equal(t, user, s.identityMap[reflectType][compositeKey])
	})

	t.Run("Should add entities without a primary-key mapping to pending", func(t *testing.T) {
		s = NewSession(nil)
		user := &TestUnkeyedUser{Name: "New User"}
		assert.NoError(t, s.Add(user))
		assert.Contains(t, s.pending, user)
	})

	t.Run("Should reject non-comparable primary keys", func(t *testing.T) {
		s = NewSession(nil)
		user := &TestInvalidPrimaryKeyUser{ID: []int{1}}
		assert.Error(t, s.Add(user))
		assert.Empty(t, s.pending)
	})

	t.Run("Should recognize the primary_key tag", func(t *testing.T) {
		s = NewSession(nil)
		user := &TestPrimaryKeyTagUser{ID: 11, Name: "Tagged User"}
		assert.NoError(t, s.Add(user))

		reflectType := reflect.TypeFor[TestPrimaryKeyTagUser]()
		assert.Equal(t, user, s.identityMap[reflectType][11])
	})

	t.Run("Should reject nil and non-struct pointers", func(t *testing.T) {
		var nilUser *TestUser
		assert.Error(t, s.Add(nilUser))
		assert.Error(t, s.Add(new(int)))
	})
}

func TestSession_Load(t *testing.T) {
	s := NewSession(nil)
	user := &TestUser{TestUserBase: TestUserBase{Id: 50}, Name: "Database User"}
	assert.NoError(t, s.Add(user))

	t.Run("Should load existing object from identity map", func(t *testing.T) {
		loaded, err := Load[TestUser](s, 50)
		assert.NoError(t, err)
		assert.NotNil(t, loaded)
		assert.Equal(t, user, loaded)
		assert.Equal(t, "Database User", loaded.Name)
	})

	t.Run("Should load existing composite object from identity map", func(t *testing.T) {
		comp := &TestCompositeUser{OrgId: 1, UserId: 200, Name: "Comp User"}
		assert.NoError(t, s.Add(comp))

		mapper, mapperErr := NewMapper[TestCompositeUser]()
		assert.NoError(t, mapperErr)
		identity, present, keyErr := mapper.PrimaryKey(comp)
		assert.NoError(t, keyErr)
		assert.True(t, present)

		loaded, err := Load[TestCompositeUser](s, identity)
		assert.NoError(t, err)
		assert.Equal(t, comp, loaded)
	})

	t.Run("Should return nil when object not in session", func(t *testing.T) {
		loaded, err := Load[TestUser](s, 999)
		assert.NoError(t, err)
		assert.Nil(t, loaded)
	})
}
