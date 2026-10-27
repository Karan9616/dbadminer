// Command chi mounts DBAdminer on a chi router.
package main

import (
	"database/sql"
	"log"
	"net/http"

	"github.com/dbadminer/dbadminer"
	"github.com/go-chi/chi/v5"
	_ "modernc.org/sqlite"
)

func main() {
	db, err := sql.Open("sqlite", "file:dbadminer.db?cache=shared")
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	cfg := dbadminer.DefaultConfig()
	ex, err := dbadminer.New(db, cfg)
	if err != nil {
		log.Fatal(err)
	}
	r := chi.NewRouter()
	r.Mount("/", ex.Handler())
	log.Fatal(http.ListenAndServe(cfg.Addr, r))
}
