package sqlok

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"reflect"
	"testing"
)

var (
	sessionBenchmarkEntity *TestUser
	sessionBenchmarkErr    error
)

func newSessionBenchmarkDB(b *testing.B, response sessionTestResponse) *sql.DB {
	b.Helper()
	sessionTestDatabase.Lock()
	sessionTestDatabase.response = response
	sessionTestDatabase.queries = nil
	sessionTestDatabase.execs = nil
	sessionTestDatabase.Unlock()

	db, err := sql.Open(sessionTestDriverName, "")
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = db.Close() })
	return db
}

func BenchmarkSessionIdentityMapHit(b *testing.B) {
	session := NewSession(nil)
	entity := &TestUser{TestUserBase: TestUserBase{Id: 7}, Name: "Ana"}
	if err := session.Add(entity); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	for b.Loop() {
		sessionBenchmarkEntity, sessionBenchmarkErr = Load[TestUser](session, 7)
	}
}

func BenchmarkSessionLoadPreparedMiss(b *testing.B) {
	db := newSessionBenchmarkDB(b, sessionTestResponse{
		columns:          []string{"id", "name"},
		rows:             [][]driver.Value{{int64(7), "Ana"}},
		disableRecording: true,
	})
	session := NewSession(db)
	entityType := reflect.TypeFor[TestUser]()

	b.ReportAllocs()
	for b.Loop() {
		sessionBenchmarkEntity, sessionBenchmarkErr = LoadContext[TestUser](
			context.Background(),
			session,
			7,
		)
		delete(session.identityMap, entityType)
		delete(session.snapshots, sessionBenchmarkEntity)
	}
}

func BenchmarkSessionFlush(b *testing.B) {
	db := newSessionBenchmarkDB(b, sessionTestResponse{disableRecording: true})
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = tx.Rollback() })

	b.Run("pending-insert", func(b *testing.B) {
		session := NewSession(db)
		b.ReportAllocs()
		for b.Loop() {
			entity := &TestUser{Name: "Ana"}
			if err := session.Add(entity); err != nil {
				b.Fatal(err)
			}
			sessionBenchmarkErr = session.Flush(context.Background(), tx)
			delete(session.snapshots, entity)
		}
	})

	b.Run("dirty-update", func(b *testing.B) {
		session := NewSession(db)
		entity := &TestUser{TestUserBase: TestUserBase{Id: 7}, Name: "Ana"}
		if err := session.Add(entity); err != nil {
			b.Fatal(err)
		}

		b.ReportAllocs()
		for iteration := 0; b.Loop(); iteration++ {
			if iteration%2 == 0 {
				entity.Name = "Bia"
			} else {
				entity.Name = "Ana"
			}
			sessionBenchmarkErr = session.Flush(context.Background(), tx)
		}
	})
}
