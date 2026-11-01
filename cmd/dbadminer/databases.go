package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/dbadminer/dbadminer"
)

type openedDB struct {
	name string
	db   *sql.DB
	ex   *dbadminer.Explorer
}

// openConfigured opens every name in DBADMINER_DATABASES. An empty list opens
// the single database selected by dsn and the unprefixed variables.
func openConfigured(ctx context.Context, cfg dbadminer.Config, getenv func(string) string, dsn string) ([]openedDB, error) {
	raw := ""
	if getenv != nil {
		raw = getenv("DBADMINER_DATABASES")
	}
	raw, err := cleanValue(raw)
	if err != nil {
		return nil, err
	}
	names, err := databaseNames(raw)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return openOne(ctx, cfg, getenv, dsn, "")
	}
	if strings.TrimSpace(dsn) != "" {
		return nil, errors.New("dbadminer: --dsn cannot be combined with DBADMINER_DATABASES")
	}
	var opened []openedDB
	for _, name := range names {
		item, err := openOne(ctx, cfg, prefixedGetenv(getenv, name), "", name)
		if err != nil {
			for _, prev := range opened {
				_ = prev.db.Close()
			}
			return nil, fmt.Errorf("dbadminer: database %s: %w", name, err)
		}
		opened = append(opened, item[0])
	}
	return opened, nil
}

func openOne(ctx context.Context, cfg dbadminer.Config, getenv func(string) string, dsn, name string) ([]openedDB, error) {
	db, schema, err := open(ctx, getenv, dsn, cfg.ReadOnly)
	if err != nil {
		return nil, err
	}
	cfg.Schema = schema
	ex, err := dbadminer.New(db, cfg)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return []openedDB{{name: name, db: db, ex: ex}}, nil
}

func handlerFor(items []openedDB) http.Handler {
	mux := http.NewServeMux()
	dbadminer.MountUI(mux)
	items[0].ex.MountAPI(mux, "/api")
	if items[0].name != "" {
		mux.HandleFunc("GET /api/databases", func(w http.ResponseWriter, r *http.Request) {
			writeDatabases(w, items)
		})
		for _, item := range items {
			item.ex.MountAPI(mux, "/api/databases/"+item.name)
		}
		missing := func(w http.ResponseWriter, r *http.Request) {
			writeDatabaseError(w, http.StatusNotFound, "database not found")
		}
		mux.HandleFunc("GET /api/databases/{name}/{path...}", missing)
		mux.HandleFunc("POST /api/databases/{name}/{path...}", missing)
	}
	return items[0].ex.Wrap(mux)
}

func writeDatabases(w http.ResponseWriter, items []openedDB) {
	list := make([]dbadminer.DatabaseInfo, 0, len(items))
	for _, item := range items {
		list = append(list, item.ex.DatabaseInfo(item.name))
	}
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(true)
	if err := enc.Encode(struct {
		Databases []dbadminer.DatabaseInfo `json:"databases"`
	}{Databases: list}); err != nil {
		http.Error(w, "encode error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(buf.String()))
}

func writeDatabaseError(w http.ResponseWriter, status int, msg string) {
	body, err := json.Marshal(struct {
		Error string `json:"error"`
	}{Error: msg})
	if err != nil {
		http.Error(w, "encode error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n'))
}
