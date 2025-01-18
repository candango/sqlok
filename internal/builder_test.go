package sqlok

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSelectBuilder(t *testing.T) {
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

	t.Run("Should select with where clause with parameters and AND condition", func(t *testing.T) {
		b.Select(columns...).From(table1).Where(columns[0]+"=$1", 1).And(columns[1]+"=$2", 2)
		sql, args := b.Build()
		assert.Equal(t, "SELECT "+strings.Join(columns, ", ")+
			" FROM table1 WHERE "+columns[0]+"=$1 "+And(columns[1])+"=$2", sql)
		assert.Equal(t, []any{1, 2}, args)
	})

	t.Run("Should select with where clause with parameters and OR condition", func(t *testing.T) {
		b.Select(columns...).From(table1).Where(columns[0]+"=$1", 1).Or(columns[1]+"=$2", 2)
		sql, args := b.Build()
		assert.Equal(t, "SELECT "+strings.Join(columns, ", ")+
			" FROM table1 WHERE "+columns[0]+"=$1 "+Or(columns[1])+"=$2", sql)
		assert.Equal(t, []any{1, 2}, args)
	})
}

func TestInsertBuilder(t *testing.T) {
	columns := []string{"column1", "column2"}
	table1 := "table1"
	values1 := []any{"column1", "column2"}
	// values2 := []any{"column1", "column2"}

	b := NewInsertBuiler()
	t.Run("Should insert columns from table1 with one line of values", func(t *testing.T) {
		b.InsertInto(table1).Values(values1...)
		sql, args := b.Build()
		assert.Equal(t, "INSERT INTO "+table1+" VALUES($1, $2) RETURNING id", sql)
		assert.Equal(t, values1, args)
	})
	t.Run("Should insert columns from table1 with columns with one line of values", func(t *testing.T) {
		b.InsertInto(table1).Columns(columns...).Values(values1...)
		sql, args := b.Build()
		assert.Equal(t, "INSERT INTO "+table1+" ("+strings.Join(columns, ", ")+") VALUES($1, $2) RETURNING id", sql)
		assert.Equal(t, values1, args)
	})
	// FIXME: Fix more than one line insert
	// t.Run("Should insert columns from table1 with two line of values", func(t *testing.T) {
	// 	b.InsertInto(table1).Values(values1...).Values(values2...)
	// 	sql, args := b.Build()
	// 	assert.Equal(t, "INSERT INTO "+table1+" VALUES($1, $2), ($3, $4)", sql)
	// 	assert.Equal(t, values1, args)
	// })

}

func TestUpdateBuilder(t *testing.T) {
	columns := []string{"column1", "column2"}
	table1 := "table1"
	values := []any{"value1", "value2"}
	// values2 := []any{"column1", "column2"}

	b := NewUpdateBuilder()
	t.Run("Should update table1 setting just columns with values", func(t *testing.T) {
		b.Update(table1).Set(columns[0], values[0]).Set(columns[1], values[1])
		sql, args := b.Build()
		assert.Equal(t, "UPDATE "+table1+" SET column1 = $1, column2 = $2", sql)
		assert.Equal(t, values, args)
	})
	t.Run("Should update table1 setting columns with values and where clause", func(t *testing.T) {
		values := values
		values = append(values, "value3")
		b.Update(
			table1,
		).Set(columns[0], values[0]).Set(columns[1], values[1]).Where("column3=$3", values[2])
		sql, args := b.Build()
		assert.Equal(t, "UPDATE "+table1+
			" SET column1 = $1, column2 = $2 WHERE column3=$3", sql)
		assert.Equal(t, values, args)
	})
}

func TestDeleteBuilder(t *testing.T) {
	columns := []string{"column1", "column2"}
	table1 := "table1"
	values := []any{"value1", "value2"}

	b := NewDeleteBuiler()
	t.Run("Should delete from table1", func(t *testing.T) {
		b.Delete(table1)
		sql, _ := b.Build()
		assert.Equal(t, "DELETE FROM "+table1, sql)
		// assert.Equal(t, values, args)
	})
	t.Run("Should delete table1 and where clause and AND condition", func(t *testing.T) {
		b.Delete(table1).Where(columns[0]+"=$1", values[0]).And(columns[1]+"=$2", values[1])
		sql, args := b.Build()
		assert.Equal(t, "DELETE FROM "+table1+" WHERE column1=$1 AND column2=$2", sql)
		assert.Equal(t, values, args)
	})
	t.Run("Should delete table1 and where clause and OR condition", func(t *testing.T) {
		b.Delete(table1).Where(columns[0]+"=$1", values[0]).Or(columns[1]+"=$2", values[1])
		sql, args := b.Build()
		assert.Equal(t, "DELETE FROM "+table1+" WHERE column1=$1 OR column2=$2", sql)
		assert.Equal(t, values, args)
	})
}
