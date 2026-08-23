package dialect

import (
	"errors"
	"fmt"
	"strconv"
)

// DialectName identifies a built-in SQL dialect.
type DialectName string

const (
	// DialectQuestionMark is the default dialect identity for question-mark
	// placeholders without a vendor-specific behavior selected.
	DialectQuestionMark DialectName = "question-mark"
	DialectMySQL        DialectName = "mysql"
	DialectSQLite       DialectName = "sqlite"
	DialectPostgres     DialectName = "postgres"
)

// ErrUnknownDialect reports an unsupported dialect name.
var ErrUnknownDialect = errors.New("unknown SQL dialect")

// Dialect supplies rendering behavior that varies by database.
type Dialect interface {
	Name() DialectName
	Placeholder(position int) string
}

// QuestionMarkDialect renders positional values with question-mark
// placeholders. MySQL and SQLite compose this behavior while retaining their
// own dialect identities.
type QuestionMarkDialect struct {
	name DialectName
}

// MySQLDialect is the MySQL dialect composed from question-mark rendering.
type MySQLDialect struct {
	QuestionMarkDialect
}

// SQLiteDialect is the SQLite dialect composed from question-mark rendering.
type SQLiteDialect struct {
	QuestionMarkDialect
}

// PostgresDialect renders numbered PostgreSQL placeholders.
type PostgresDialect struct{}

// NewDefaultDialect returns the default question-mark dialect.
func NewDefaultDialect() Dialect {
	return newQuestionMark(DialectQuestionMark)
}

// NewDialect resolves a built-in dialect once for compiler/application setup.
func NewDialect(name DialectName) (Dialect, error) {
	switch name {
	case DialectQuestionMark:
		return NewDefaultDialect(), nil
	case DialectMySQL:
		return MySQLDialect{
			QuestionMarkDialect: newQuestionMark(DialectMySQL),
		}, nil
	case DialectSQLite:
		return SQLiteDialect{
			QuestionMarkDialect: newQuestionMark(DialectSQLite),
		}, nil
	case DialectPostgres:
		return PostgresDialect{}, nil
	default:
		return nil, fmt.Errorf("%w %q", ErrUnknownDialect, name)
	}
}

func newQuestionMark(name DialectName) QuestionMarkDialect {
	return QuestionMarkDialect{name: name}
}

// Name returns the dialect identity used in cache shape keys.
func (d QuestionMarkDialect) Name() DialectName {
	return d.name
}

// Placeholder returns the question-mark placeholder for any position.
func (QuestionMarkDialect) Placeholder(int) string {
	return "?"
}

// Name returns the PostgreSQL dialect identity.
func (PostgresDialect) Name() DialectName {
	return DialectPostgres
}

// Placeholder returns a one-based PostgreSQL positional placeholder.
func (PostgresDialect) Placeholder(position int) string {
	return "$" + strconv.Itoa(position+1)
}
