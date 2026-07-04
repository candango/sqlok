package dql

import (
	"testing"

	"github.com/candango/sqlok/internal/sst"
)

type fakeVisitor struct {
	visitedSelect bool
}

func (v *fakeVisitor) VisitSelect(s sst.SelectNode) error {
	v.visitedSelect = true
	return nil
}

func (v *fakeVisitor) VisitColumn(s sst.SelectColumnNode) error {
	return nil
}

func TestSelectAcceptVisitsSelect(t *testing.T) {
	visitor := &fakeVisitor{}
	selectNode := NewSelect()

	if err := selectNode.Accept(visitor); err != nil {
		t.Fatalf("Accept returned error: %v", err)
	}

	if !visitor.visitedSelect {
		t.Fatal("expected Select.Accept to call VisitSelect")
	}
}
