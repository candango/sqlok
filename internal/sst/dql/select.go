package dql

import (
	"github.com/candango/sqlok/internal/sst"
)

// Select is the root node of a SELECT statement.
type Select struct {
	columns []sst.SelectColumnNode
	source  sst.TableRefNode
}

// NewSelect creates a SELECT statement root with the provided projected columns.
func NewSelect(columns ...sst.SelectColumnNode) *Select {
	s := &Select{
		columns: columns,
	}
	return s
}

// Accept dispatches the SELECT node to the provided visitor.
func (s *Select) Accept(v sst.Visitor) error {
	return v.VisitSelect(s)
}

// Columns returns the projected columns in this SELECT statement.
func (s *Select) Columns() []sst.SelectColumnNode {
	return s.columns
}

// From sets the primary FROM source and returns the SELECT statement.
func (s *Select) From(source sst.TableRefNode) *Select {
	s.source = source
	return s
}

// Source returns the primary FROM source.
func (s *Select) Source() sst.TableRefNode {
	return s.source
}
