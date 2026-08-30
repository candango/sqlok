package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/candango/sqlok/dialect"
	"github.com/candango/sqlok/sst"
)

// DeriveShapeKey creates a canonical key from statement structure and the
// supplied dialect. Runtime bind values are intentionally excluded.
func DeriveShapeKey(
	stmt sst.StatementNode,
	renderingDialect dialect.Dialect,
) (ShapeKey, error) {
	context, err := newShapeContext(renderingDialect)
	if err != nil {
		return "", err
	}
	return deriveShapeKeyWithContext(stmt, context)
}

func deriveShapeKeyWithContext(
	stmt sst.StatementNode,
	context shapeContext,
) (ShapeKey, error) {
	if stmt == nil {
		return "", errors.New("statement cannot be nil")
	}
	if err := context.validate(); err != nil {
		return "", err
	}
	if err := stmt.Err(); err != nil {
		return "", err
	}

	fingerprint := &shapeFingerprint{}
	fingerprint.token("dialect", string(context.dialect.Name()))
	fingerprint.token("compiler", context.compilerVersion)
	if err := stmt.Accept(fingerprint); err != nil {
		return "", err
	}

	digest := sha256.Sum256([]byte(fingerprint.builder.String()))
	return ShapeKey(hex.EncodeToString(digest[:])), nil
}

type shapeFingerprint struct {
	builder strings.Builder
}

var _ sst.Visitor = (*shapeFingerprint)(nil)

func (f *shapeFingerprint) token(kind string, values ...string) {
	f.builder.WriteString(kind)
	for _, value := range values {
		_, _ = fmt.Fprintf(&f.builder, "%d:%s", len(value), value)
	}
	f.builder.WriteByte(';')
}

func (f *shapeFingerprint) VisitStatement(stmt sst.StatementNode) error {
	f.token("statement", stmt.Declaration())
	return nil
}

func (f *shapeFingerprint) VisitClause(clause sst.ClauseNode) error {
	f.token("clause", clause.Declaration())
	return nil
}

func (f *shapeFingerprint) VisitExpression(expr sst.ExpressionNode) error {
	text, ok := expr.(sst.ExpressionTextNode)
	if !ok {
		return fmt.Errorf("expression %T has no SQL text", expr)
	}
	f.token("expression", text.Expr())
	return nil
}

func (f *shapeFingerprint) VisitBindParam(sst.BindParamNode) error {
	f.token("bind")
	return nil
}

func (f *shapeFingerprint) VisitParameterSlot(slot sst.ParameterSlotNode) error {
	f.token("parameter-slot", strconv.Itoa(slot.Position()))
	return nil
}

func (f *shapeFingerprint) VisitExpressionGroupStart() error {
	f.token("expression-group-start")
	return nil
}

func (f *shapeFingerprint) VisitExpressionGroupEnd() error {
	f.token("expression-group-end")
	return nil
}

func (f *shapeFingerprint) VisitSpace() error {
	f.token("space")
	return nil
}

func (f *shapeFingerprint) VisitFromSource(source sst.FromSourceNode) error {
	f.token("from-source")
	if table := source.Table(); table != nil {
		if err := table.Accept(f); err != nil {
			return err
		}
	}
	if join := source.Join(); join != nil {
		return join.Accept(f)
	}
	return nil
}

func (f *shapeFingerprint) VisitJoin(join sst.JoinNode) error {
	f.token("join", string(join.Type()))

	right := join.Right()
	if table := right.Table(); table != nil {
		if err := table.Accept(f); err != nil {
			return err
		}
	}

	if condition := join.On(); condition != nil {
		f.token("join-on")
		if err := condition.Accept(f); err != nil {
			return err
		}
	} else {
		f.token("join-on-nil")
	}

	if next := right.Join(); next != nil {
		return next.Accept(f)
	}
	return nil
}

func (f *shapeFingerprint) VisitColumnRef(column sst.ColumnRefNode) error {
	f.token("column", column.Schema(), column.Table(), column.Name())
	return nil
}

func (f *shapeFingerprint) VisitListSeparator(index int, separator string) error {
	f.token("separator", strconv.Itoa(index), separator)
	return nil
}

func (f *shapeFingerprint) VisitTableRef(table sst.TableRefNode) error {
	f.token("table", table.Schema(), table.Name())
	return nil
}

func (f *shapeFingerprint) VisitOrderItem(item sst.OrderItemNode) error {
	f.token("order", string(item.Direction()))
	return nil
}

func (f *shapeFingerprint) VisitLimit(sst.LimitNode) error {
	f.token("limit-bind")
	return nil
}

func (f *shapeFingerprint) VisitOffset(sst.OffsetNode) error {
	f.token("offset-bind")
	return nil
}
