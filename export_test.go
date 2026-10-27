package dbadminer

import (
	"database/sql"
	"testing"
)

// Exercise runs the shared handler checks. Integration tests live in the
// external test package so they can import dbopen without a cycle.
func Exercise(t *testing.T, db *sql.DB) {
	t.Helper()
	exercise(t, db)
}
