package sqlok

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

type User struct {
	ID    int    `sqlok:"primary_key"`
	Name  string `sqlok:"max_lenght=255"`
	Email string `sqlok:"unique"`
}

func TestHttpTransport(t *testing.T) {
	user := User{}
	userType := reflect.TypeOf(user)
	for i := range userType.NumField() {
		field := userType.Field(i)
		tag := field.Tag.Get("sqlok")

		if tag == "" {
			t.Errorf("error reading the tag sqlok for the field %s", field.Name)
			continue
		}
		// t.Error(tag)
	}
	assert.True(t, true)
}
