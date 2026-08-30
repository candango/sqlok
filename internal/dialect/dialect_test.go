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

func TestQuestionMarkDialectIgnoresPosition(t *testing.T) {
	dialect := QuestionMarkDialect{}

	assert.Equal(t, "?", dialect.Placeholder(0))
	assert.Equal(t, "?", dialect.Placeholder(3))
}
