package main

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/dbadminer/dbadminer"
)

func TestVersion(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	err = Run(context.Background(), []string{"--version"})
	os.Stdout = stdout
	_ = w.Close()
	if err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) != dbadminer.Version {
		t.Fatalf("version %q", out)
	}
}
