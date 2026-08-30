package compiler

import (
	"strconv"

	"github.com/candango/sqlok/dialect"
)

type namedQuestionMarkTestDialect struct {
	name dialect.DialectName
}

func (d namedQuestionMarkTestDialect) Name() dialect.DialectName {
	return d.name
}

func (namedQuestionMarkTestDialect) Placeholder(int) string {
	return "?"
}

type postgresTestDialect struct{}

func (postgresTestDialect) Name() dialect.DialectName {
	return "postgres"
}

func (postgresTestDialect) Placeholder(position int) string {
	return "$" + strconv.Itoa(position+1)
}

var _ dialect.Dialect = namedQuestionMarkTestDialect{}
var _ dialect.Dialect = postgresTestDialect{}
