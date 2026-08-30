package dialect

// DialectName identifies a SQL rendering dialect.
type DialectName string

const (
	// DialectQuestionMark is the default dialect identity for question-mark
	// placeholders.
	DialectQuestionMark DialectName = "question-mark"
)

// Dialect supplies rendering behavior that varies by database.
type Dialect interface {
	Name() DialectName
	Placeholder(position int) string
}

// QuestionMarkDialect renders positional values with question-mark
// placeholders. Vendor adapters may provide their own identity and behavior.
type QuestionMarkDialect struct{}

// NewDefaultDialect returns the default question-mark dialect.
func NewDefaultDialect() Dialect {
	return QuestionMarkDialect{}
}

// Name returns the default question-mark dialect identity.
func (QuestionMarkDialect) Name() DialectName {
	return DialectQuestionMark
}

// Placeholder returns the question-mark placeholder for any position.
func (QuestionMarkDialect) Placeholder(int) string {
	return "?"
}
