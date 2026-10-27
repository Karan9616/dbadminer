// Command dbadminer connects to PostgreSQL, MySQL, or SQLite from the
// environment variables already used by most applications, then serves the
// explorer. Programs that already hold a *sql.DB should import
// github.com/dbadminer/dbadminer instead of running this command.
package main

import (
	"context"
	"fmt"
	"os"
)

func main() {
	if err := Run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
