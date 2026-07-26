package dql

import (
	"errors"

	"github.com/candango/sqlok/internal/sst"
)

// SelectStatement is the root node of a SELECT statement.
type SelectStatement struct {
	columns     []sst.SelectColumnNode
	source      sst.FromSourceNode
	tailSource  sst.FromSourceNode
	pendingJoin *Join
	err         error
}

// Select creates a SELECT statement root with the provided projected columns.
func Select(columns ...sst.SelectColumnNode) *SelectStatement {
	s := &SelectStatement{
		columns: columns,
	}
	return s
}

// Accept dispatches the SELECT node to the provided visitor.
func (s *SelectStatement) Accept(v sst.Visitor) error {
	return v.VisitSelect(s)
}

// Err returns the first construction error recorded by the statement.
// Once an error is recorded, subsequent builder operations are no-ops.
func (s *SelectStatement) Err() error {
	return s.err
}

// Columns returns the projected columns in this SELECT statement.
func (s *SelectStatement) Columns() []sst.SelectColumnNode {
	return s.columns
}

// From sets the primary FROM source and returns the SELECT statement.
func (s *SelectStatement) From(source sst.FromSourceNode) *SelectStatement {
	if s.err != nil {
		return s
	}
	if source == nil {
		s.err = errors.New("FROM source cannot be nil")
		return s
	}
	s.source = source
	s.tailSource = source
	return s
}

// Join adds a source to the forward join chain and waits for On to complete
// the new join condition.
func (s *SelectStatement) Join(source sst.FromSourceNode) *SelectStatement {
	if s.err != nil {
		return s
	}
	if s.tailSource == nil {
		s.err = errors.New("JOIN requires a FROM source")
		return s
	}
	if s.pendingJoin != nil {
		s.err = errors.New("JOIN requires ON from the a pending join")
		return s
	}
	if source == nil {
		s.err = errors.New("JOIN source cannot be nil")
		return s
	}
	j := NewJoin(s.tailSource, source)

	if err := s.tailSource.Attach(j); err != nil {
		s.err = err
		return s
	}
	s.pendingJoin = j
	s.tailSource = source
	return s
}

// On completes the most recently created JOIN with its condition.
func (s *SelectStatement) On(condition sst.Node) *SelectStatement {
	if s.err != nil {
		return s
	}
	if s.pendingJoin == nil {
		s.err = errors.New("ON requires a pending JOIN")
		return s
	}
	if condition == nil {
		s.err = errors.New("JOIN condition cannot be nil")
		return s
	}

	s.pendingJoin.SetOn(condition)
	s.pendingJoin = nil
	return s
}

// Source returns the primary FROM source.
func (s *SelectStatement) Source() sst.FromSourceNode {
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

// Attach stores the next join on this source. The join's Left reference points
// back to this source; forward traversal must continue through Right instead.
func (fs *FromSource) Attach(j sst.JoinNode) error {
	if fs.join != nil {
		return errors.New("attaching join to an already filled form source")
	}
	fs.join = j
	return nil
}

// Join returns the next join attached to this source.
func (fs *FromSource) Join() sst.JoinNode {
	return fs.join
}

func (fs *FromSource) Table() sst.TableRefNode {
	return fs.table
}

func (fs *FromSource) Accept(v sst.Visitor) error {
	return v.VisitFromSource(fs)
}

type Join struct {
	left  sst.FromSourceNode
	right sst.FromSourceNode
	jtype sst.JoinType
	on    sst.Node
}

type JoinOption func(j *Join)

func NewJoin(left sst.FromSourceNode, right sst.FromSourceNode, options ...JoinOption) *Join {
	j := &Join{
		left:  left,
		right: right,
		jtype: sst.InnerJoin,
	}

	for _, option := range options {
		option(j)
	}

	return j
}

func WithJoinType(jtype sst.JoinType) JoinOption {
	return func(j *Join) {
		j.jtype = jtype
	}
}

// Left returns the source on the left side of the join. It is a
// back-reference and is not a forward traversal edge.
func (j *Join) Left() sst.FromSourceNode {
	return j.left
}

// Right returns the source introduced by the join and the next forward
// traversal point.
func (j *Join) Right() sst.FromSourceNode {
	return j.right
}

// Type returns the SQL join type.
func (j *Join) Type() sst.JoinType {
	return j.jtype
}

// On returns the condition attached to the join.
func (j *Join) On() sst.Node {
	return j.on
}

// SetOn attaches the condition while the statement builder completes the
// pending join.
func (j *Join) SetOn(condition sst.Node) {
	j.on = condition
}

// Accept dispatches the join to the provided visitor. Visitors should follow
// Right for forward traversal and treat Left as a back-reference.
func (j *Join) Accept(v sst.Visitor) error {
	return v.VisitJoin(j)
}
