package dbadminer

import (
	"database/sql"
	"testing"
)

// Exercise runs the shared handler checks. Integration tests live in the
// cmd/dbadminer module so they can open a database without a driver import here.
func Exercise(t *testing.T, db *sql.DB) {
	t.Helper()
	exercise(t, db)
}
