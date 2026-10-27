// Command http mounts DBAdminer on a net/http server.
package main

import (
	"database/sql"
	"log"
	"net/http"

	"github.com/dbadminer/dbadminer"
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
	mux := http.NewServeMux()
	mux.Handle("/", ex.Handler())
	log.Fatal(http.ListenAndServe(cfg.Addr, mux))
}
