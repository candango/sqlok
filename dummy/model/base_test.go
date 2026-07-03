package model

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBase(t *testing.T) {

	t.Run("Should read tags", func(t *testing.T) {
		userType := reflect.TypeOf(Auser{}.AuserBase)
		// for i := 0; i < userType.NumField(); i++ {
		// 	field := userType.Field(i)
		// 	tag := field.Tag.Get("sqlok")
		// 	t.Error(tag)
		// }
		assert.Equal(t, userType.Field(0).Tag.Get("sqlok"), "primary_key")
		assert.Equal(t, userType.Field(1).Tag.Get("sqlok"), "max_lenght=255")
		assert.Equal(t, userType.Field(2).Tag.Get("sqlok"), "text")
		// assert.Equal(t, "SELECT "+strings.Join(columns, ", ")+" FROM table1", sql)
		// assert.Equal(t, "SELECT "+strings.Join(columns, ", ")+" FROM table1", sql)
	})

}
