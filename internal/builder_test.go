package sqlok

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuilder(t *testing.T) {
	columns := []string{"column1", "column2"}
	table1 := "table1"

	b := NewSelectBuiler()
	t.Run("Should select columns from table1", func(t *testing.T) {
		b.Select(columns...).From(table1)
		sql, _ := b.Build()
		assert.Equal(t, "SELECT "+strings.Join(columns, ", ")+" FROM table1", sql)
	})

	t.Run("Should select with where clause no parameters", func(t *testing.T) {
		b.Select(columns...).From(table1).Where(columns[0] + "=1")
		sql, args := b.Build()
		assert.Equal(t, "SELECT "+strings.Join(columns, ", ")+" FROM table1 WHERE "+columns[0]+"=1", sql)
		assert.Equal(t, []any{}, args)
	})

	t.Run("Should select with where clause with parameters", func(t *testing.T) {
		b.Select(columns...).From(table1).Where(columns[0]+"=$1", 1).And(columns[1]+"=$2", 2)
		sql, args := b.Build()
		assert.Equal(t, "SELECT "+strings.Join(columns, ", ")+" FROM table1 WHERE "+columns[0]+"=$1 AND "+columns[1]+"=$2", sql)
		assert.Equal(t, []any{1, 2}, args)
	})
}
