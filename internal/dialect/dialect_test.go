package dialect

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewDefaultDialectUsesQuestionMark(t *testing.T) {
	dialect := NewDefaultDialect()

	assert.Equal(t, DialectQuestionMark, dialect.Name())
	assert.Equal(t, "?", dialect.Placeholder(3))
}

func TestNewDialectRejectsEmptyName(t *testing.T) {
	_, err := NewDialect("")

	assert.ErrorIs(t, err, ErrUnknownDialect)
}

func TestQuestionMarkDialectsKeepDistinctIdentities(t *testing.T) {
	mysql, err := NewDialect(DialectMySQL)
	assert.NoError(t, err)

	sqlite, err := NewDialect(DialectSQLite)
	assert.NoError(t, err)

	assert.Equal(t, DialectMySQL, mysql.Name())
	assert.Equal(t, DialectSQLite, sqlite.Name())
	assert.Equal(t, "?", mysql.Placeholder(0))
	assert.Equal(t, "?", sqlite.Placeholder(0))
}

func TestNewDialectUsesNumberedPostgresPlaceholders(t *testing.T) {
	dialect, err := NewDialect(DialectPostgres)

	assert.NoError(t, err)
	assert.Equal(t, DialectPostgres, dialect.Name())
	assert.Equal(t, "$1", dialect.Placeholder(0))
	assert.Equal(t, "$3", dialect.Placeholder(2))
}

func TestNewDialectRejectsUnknownDialect(t *testing.T) {
	_, err := NewDialect(DialectName("oracle"))

	assert.ErrorIs(t, err, ErrUnknownDialect)
}
