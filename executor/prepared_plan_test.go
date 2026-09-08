package executor

import (
	"context"
	"testing"

	"github.com/candango/sqlok/compiler"
	"github.com/candango/sqlok/dialect"
	"github.com/candango/sqlok/sst"
	"github.com/candango/sqlok/sst/dql"
	"github.com/stretchr/testify/assert"
)

func usersByIDStatement(id int) sst.StatementNode {
	return dql.Select(
		sst.NewColumnRef("users", "id"),
		sst.NewColumnRef("users", "name"),
	).
		From(sst.NewTableRef("users")).
		Where(sst.Eq(
			sst.NewColumnRef("users", "id"),
			sst.NewBindParam(id),
		))
}

// TestPreparedPlanRoundTripServesRepeatedRequests is the first end-to-end
// consumer of the prepared path. It walks the full sequence an ORM repository
// accessor is meant to follow: prepare once at startup, publish the shape
// under a stable application-owned plan ID, then serve each request through a
// registry lookup, a bind, and the executor.
//
// It asserts the property the whole design rests on: the SQL template is
// derived once and reused verbatim across requests, while only the runtime
// arguments change.
func TestPreparedPlanRoundTripServesRepeatedRequests(t *testing.T) {
	const planID compiler.PlanID = "users.get"

	cache, err := compiler.NewBoundedStatementCache(16)
	assert.NoError(t, err)

	shape, err := compiler.Prepare(
		cache,
		usersByIDStatement(1),
		dialect.NewDefaultDialect(),
	)
	assert.NoError(t, err)

	registry := compiler.NewPlanRegistry()
	assert.NoError(t, registry.Put(planID, shape))

	target := &recordingExecutor{}
	preparedSQL := shape.SQL()

	for _, userID := range []int{7, 99, 1234} {
		plan, found := registry.Get(planID)
		assert.True(t, found, "prepared plan must survive between requests")
		assert.Equal(t, preparedSQL, plan.SQL(),
			"the SQL template must be reused verbatim across requests")

		_, err := Query(context.Background(), target, plan, []any{userID})
		assert.NoError(t, err)
		assert.Equal(t, preparedSQL, target.querySQL)
		assert.Equal(t, []any{userID}, target.queryArgs)
	}

	assert.Equal(t, 3, target.queryCalls)
	assert.Equal(t, 1, cache.Len(),
		"runtime value variations must not fragment the statement cache")
	assert.Equal(t, 1, registry.Len())
}

// TestPreparedPlanRejectsWrongArgumentCount proves the bind argument count is
// enforced at the executor boundary rather than reaching the database driver.
func TestPreparedPlanRejectsWrongArgumentCount(t *testing.T) {
	shape, err := compiler.Prepare(
		compiler.NewStatementCache(),
		usersByIDStatement(1),
		dialect.NewDefaultDialect(),
	)
	assert.NoError(t, err)

	target := &recordingExecutor{}
	_, err = Query(context.Background(), target, shape, []any{1, 2})

	assert.Error(t, err)
	assert.Equal(t, 0, target.queryCalls,
		"a bind failure must not reach the executor")
}

// TestPreparedPathReusesCachedShapeForEquivalentStatements proves the ad-hoc
// path collapses structurally identical statements carrying different runtime
// values onto a single cached shape, which is what makes the prepared handle
// and the cached lookup agree on identity.
func TestPreparedPathReusesCachedShapeForEquivalentStatements(t *testing.T) {
	cache, err := compiler.NewBoundedStatementCache(16)
	assert.NoError(t, err)
	renderingDialect := dialect.NewDefaultDialect()

	first, err := compiler.Prepare(cache, usersByIDStatement(1), renderingDialect)
	assert.NoError(t, err)
	second, err := compiler.Prepare(cache, usersByIDStatement(9_999), renderingDialect)
	assert.NoError(t, err)

	assert.Equal(t, first.ShapeKey(), second.ShapeKey())
	assert.Equal(t, first.SQL(), second.SQL())
	assert.Equal(t, 1, cache.Len())
}
