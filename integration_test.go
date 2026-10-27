//go:build integration

package dbadminer_test

import (
	"context"
	"os"
	"testing"

	"github.com/dbadminer/dbadminer"
	"github.com/dbadminer/dbadminer/dbopen"
)

func TestIntegrationHandlers(t *testing.T) {
	if os.Getenv("DBADMINER_INTEGRATION") == "" {
		t.Skip("set DBADMINER_INTEGRATION=1 to run database integration tests")
	}
	db, err := dbopen.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	dbadminer.Exercise(t, db)
}
