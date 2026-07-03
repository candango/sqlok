package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSchema(t *testing.T) {
	t.Run("Should select columns from table1", func(t *testing.T) {
		// Table example
		t1 := &Table{TableName: "table1", Schema: "public"}

		// Validar Table.Name()
		assert.Equal(t, "table1", t1.Name())

		// Validar Table.Schema
		assert.Equal(t, "public", t1.Schema)

		// Validar Table.As() retorna string não vazia
		aliasedTable := t1.As("a")
		assert.NotEmpty(t, aliasedTable)

		// Field example
		f1 := &Field{FieldName: "column1", Type: "int"}

		// Validar Field.Name()
		assert.Equal(t, "column1", f1.Name())

		// Validar Field.Type
		assert.Equal(t, "int", f1.Type)

		// Validar Field.As() retorna string não vazia
		aliasedField := f1.As("alias1")
		assert.NotEmpty(t, aliasedField)

		// Validar WithPrefix() retorna string não vazia
		prefixed := WithPrefix("a", aliasedField)
		assert.NotEmpty(t, prefixed)
	})

}
