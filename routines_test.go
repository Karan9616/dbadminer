package dbadminer

import (
	"net/http"
	"testing"
)

func TestNormalizeRoutineType(t *testing.T) {
	if got := normalizeRoutineType("PROCEDURE"); got != "procedure" {
		t.Fatalf("got %q", got)
	}
	if got := normalizeRoutineType("Function"); got != "function" {
		t.Fatalf("got %q", got)
	}
}

func TestSQLiteRoutinesEmpty(t *testing.T) {
	db := openMem(t)
	execSQL(t, db, `CREATE TABLE t (id INTEGER PRIMARY KEY)`)
	ex, err := New(db, Config{Access: AccessReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	h := ex.Handler()

	rr := doRequest(h, http.MethodGet, "/api/routines", nil, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("list %d %s", rr.Code, rr.Body.String())
	}
	var payload routinesPayload
	decodeJSON(t, rr, &payload)
	if len(payload.Routines) != 0 {
		t.Fatalf("expected no routines, got %+v", payload.Routines)
	}

	rr = doRequest(h, http.MethodGet, "/api/routines/main/missing?type=procedure", nil, "")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("detail %d %s", rr.Code, rr.Body.String())
	}
}
