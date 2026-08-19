package dql

import (
	"errors"
	"fmt"

	"github.com/candango/sqlok/internal/sst"
)

// SelectStatement is the root node of a SELECT statement.
type SelectStatement struct {
	columns     *sst.ExpressionList
	source      sst.FromSourceNode
	tailSource  sst.FromSourceNode
	pendingJoin *Join
	err         error
}

// Select creates a SELECT statement root with the provided projected expressions.
func Select(columns ...sst.ExpressionNode) *SelectStatement {
	s := &SelectStatement{}
	if len(columns) > 0 {
		s.columns = sst.NewExpressionList(columns...)
	}
	return s
}

// Accept dispatches the SELECT node to the provided visitor.
func (s *SelectStatement) Accept(v sst.Visitor) error {
	if err := v.VisitStatement(s); err != nil {
		return err
	}
	if s.columns != nil {
		if err := s.columns.Accept(v); err != nil {
			return err
		}
	}
	if s.source != nil {
		if err := v.VisitClause(s.source); err != nil {
			return err
		}

		if err := s.source.Accept(v); err != nil {
			return err
		}
	}
	return nil
}

// Declaration returns the SELECT statement keyword.
func (s *SelectStatement) Declaration() string {
	return "SELECT"
}

// Err returns the first construction error recorded by the statement.
// Once an error is recorded, subsequent builder operations are no-ops.
func (s *SelectStatement) Err() error {
	return s.err
}

// Columns returns the projected expressions in this SELECT statement.
func (s *SelectStatement) Columns() *sst.ExpressionList {
	return s.columns
}

// From sets the primary FROM source and returns the SELECT statement.
func (s *SelectStatement) From(table sst.TableRefNode) *SelectStatement {
	if s.err != nil {
		return s
	}
	if table == nil {
		s.err = errors.New("FROM table cannot be nil")
		return s
	}

	s.source = NewFromSource(table)
	s.tailSource = s.source
	return s
}

// Join adds a source with the JOIN type.
func (s *SelectStatement) Join(table sst.TableRefNode) *SelectStatement {
	if s.err != nil {
		return s
	}
	if err := s.addJoin(table, sst.Join); err != nil {
		s.err = err
		return s
	}
	return s
}

// InnerJoin adds a source with the INNER JOIN type.
func (s *SelectStatement) InnerJoin(table sst.TableRefNode) *SelectStatement {
	if s.err != nil {
		return s
	}
	if err := s.addJoin(table, sst.InnerJoin); err != nil {
		s.err = err
		return s
	}
	return s
}

// addJoin appends a source with the requested SQL join type, attaches the
// resulting join to the current source, advances the forward chain, and marks
// the join as pending for a subsequent On condition.
func (s *SelectStatement) addJoin(table sst.TableRefNode, jtype sst.JoinType) error {
	if s.tailSource == nil {
		return fmt.Errorf("%s requires a FROM source", jtype)
	}
	if table == nil {
		return fmt.Errorf("%s table cannot be nil", jtype)
	}
	source := NewFromSource(table)
	j := NewJoin(s.tailSource, source, WithJoinType(jtype))
	if err := s.tailSource.Attach(j); err != nil {
		return err
	}
	s.pendingJoin = j
	s.tailSource = source
	return nil
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

// FromSource represents a SELECT source table and its next attached join.
type FromSource struct {
	table sst.TableRefNode
	join  sst.JoinNode
}

// FromSourceOption configures a FromSource during construction.
type FromSourceOption func(fs *FromSource)

// NewFromSource creates a SELECT source from a table reference.
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

// Declaration returns the FROM clause keyword.
func (fs *FromSource) Declaration() string {
	return "FROM"
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

// Table returns the table attached to this source.
func (fs *FromSource) Table() sst.TableRefNode {
	return fs.table
}

// Accept dispatches the source to the provided visitor.
func (fs *FromSource) Accept(v sst.Visitor) error {
	return v.VisitFromSource(fs)
}

// Join represents one relationship between a left source and a right source.
// Its On condition may be nil until the builder or dialect validation resolves
// the incomplete join.
type Join struct {
	left  sst.FromSourceNode
	right sst.FromSourceNode
	jtype sst.JoinType
	on    sst.Node
}

// JoinOption configures a Join during construction.
type JoinOption func(j *Join)

// NewJoin creates a Join between the left and right sources.
func NewJoin(left sst.FromSourceNode, right sst.FromSourceNode, options ...JoinOption) *Join {
	j := &Join{
		left:  left,
		right: right,
		jtype: sst.Join,
	}

	for _, option := range options {
		option(j)
	}

	return j
}

// WithJoinType configures the SQL join type.
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
