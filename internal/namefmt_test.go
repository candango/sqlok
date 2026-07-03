package sqlok

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBase(t *testing.T) {

	t.Run("Should camel case", func(t *testing.T) {
		assert.Equal(t, "AUser", CamelCase("a_user"))
		// assert.Equal(t, "SELECT "+strings.Join(columns, ", ")+" FROM table1", sql)
		// assert.Equal(t, "SELECT "+strings.Join(columns, ", ")+" FROM table1", sql)
	})

}
