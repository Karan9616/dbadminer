//go:build integration

package main

import (
	"context"
	"os"
	"testing"

	"github.com/dbadminer/dbadminer"
)

func TestIntegrationHandlers(t *testing.T) {
	if os.Getenv("DBADMINER_INTEGRATION") == "" {
		t.Skip("set DBADMINER_INTEGRATION=1 to run database integration tests")
	}
	db, err := Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	dbadminer.Exercise(t, db)
}
