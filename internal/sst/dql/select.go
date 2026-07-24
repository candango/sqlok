package dql

import (
	"github.com/candango/sqlok/internal/sst"
)

// Select is the root node of a SELECT statement.
type Select struct {
	columns []sst.SelectColumnNode
	source  sst.FromSourceNode
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
func (s *Select) From(source sst.FromSourceNode) *Select {
	s.source = source
	return s
}

// Source returns the primary FROM source.
func (s *Select) Source() sst.FromSourceNode {
	return s.source
}

type FromSource struct {
	table sst.TableRefNode
	join  sst.JoinNode
}

type FromSourceOption func(fs *FromSource)

func NewFromSource(table sst.TableRefNode, options ...FromSourceOption) *FromSource {
	fs := &FromSource{
		table: table,
	}

	for _, option := range options {
		option(fs)
	}

	return fs
}

// WithJoinNode configures the source with a composed join node.
func WithJoinNode(join sst.JoinNode) FromSourceOption {
	return func(fs *FromSource) {
		fs.join = join
	}
}

func (fs *FromSource) Join() sst.JoinNode {
	return fs.join
}

func (fs *FromSource) Table() sst.TableRefNode {
	return fs.table
}

func (fs *FromSource) Accept(v sst.Visitor) error {
	return v.VisitFromSource(fs)
}
