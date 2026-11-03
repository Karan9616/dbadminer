package main

import (
	"context"
	"database/sql"
	"fmt"
	"html"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type dbHandle struct {
	name   string
	driver string
	db     *sql.DB
}

func runServe(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("serve takes no arguments")
	}
	addr := firstNonEmpty(os.Getenv("DEMO_ADDR"), ":8090")
	names, err := databaseNames(os.Getenv("DEMO_DATABASES"))
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return fmt.Errorf("set DEMO_DATABASES to a comma-separated list of database names")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var handles []dbHandle
	for _, name := range names {
		dsn, driver, err := resolveDSN(name)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		db, err := openReady(ctx, driver, dsn)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		handles = append(handles, dbHandle{name: name, driver: driver, db: db})
	}
	defer func() {
		for _, h := range handles {
			_ = h.db.Close()
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintln(w, "<!doctype html><html><head><meta charset=utf-8><title>demo</title>")
		fmt.Fprintln(w, "<style>body{font:16px/1.4 system-ui,sans-serif;margin:2rem;max-width:40rem}code{font:14px ui-monospace,monospace}</style>")
		fmt.Fprintln(w, "</head><body>")
		fmt.Fprintln(w, "<h1>Hello from the demo app</h1>")
		fmt.Fprintln(w, "<p>This tiny Go service reads the seeded databases. Use DBAdminer on <code>:8080</code> to browse and edit them.</p>")
		fmt.Fprintln(w, "<ul>")
		for _, h := range handles {
			n, err := countCustomers(r.Context(), h.db)
			label := html.EscapeString(h.name) + " (" + html.EscapeString(dialectLabel(h.driver)) + ")"
			if err != nil {
				fmt.Fprintf(w, "<li><strong>%s</strong>: error: %s</li>\n", label, html.EscapeString(err.Error()))
				continue
			}
			fmt.Fprintf(w, "<li><strong>%s</strong>: %d customers</li>\n", label, n)
		}
		fmt.Fprintln(w, "</ul></body></html>")
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		for _, h := range handles {
			if err := h.db.PingContext(r.Context()); err != nil {
				http.Error(w, h.name+": "+err.Error(), http.StatusServiceUnavailable)
				return
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	log.Printf("demo app listening on %s", addr)
	server := &http.Server{Addr: addr, Handler: mux}
	return server.ListenAndServe()
}

func openReady(ctx context.Context, driver, dsn string) (*sql.DB, error) {
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(90 * time.Second)
	for {
		if err := db.PingContext(ctx); err == nil {
			return db, nil
		} else if time.Now().After(deadline) {
			_ = db.Close()
			return nil, fmt.Errorf("ping: %w", err)
		}
		select {
		case <-ctx.Done():
			_ = db.Close()
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func countCustomers(ctx context.Context, db *sql.DB) (int64, error) {
	var n int64
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM customers`).Scan(&n)
	return n, err
}

func dialectLabel(driver string) string {
	switch driver {
	case "pgx":
		return "postgres"
	default:
		return strings.TrimSpace(driver)
	}
}
