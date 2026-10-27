// Command cobra serves DBAdminer from a cobra command.
package main

import (
	"database/sql"
	"os"

	"github.com/dbadminer/dbadminer"
	"github.com/spf13/cobra"
	_ "modernc.org/sqlite"
)

func main() {
	var dsn string
	cmd := &cobra.Command{
		Use:   "dbadminer",
		Short: "Serve DBAdminer for a SQLite database",
		RunE: func(cmd *cobra.Command, _ []string) error {
			db, err := sql.Open("sqlite", dsn)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()
			cfg := dbadminer.DefaultConfig()
			ex, err := dbadminer.New(db, cfg)
			if err != nil {
				return err
			}
			return ex.ListenAndServe(cmd.Context())
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "file:dbadminer.db?cache=shared", "SQLite DSN")
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
