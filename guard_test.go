package dbadminer

import (
	"strings"
	"testing"
)

func TestIsReadOnlySQL(t *testing.T) {
	cases := []struct {
		sql string
		ok  bool
	}{
		{sql: "select 1", ok: true},
		{sql: "  SeLeCt 1;  ", ok: true},
		{sql: "select 1; select 2", ok: false},
		{sql: "select 1; drop table t", ok: false},
		{sql: "select ';'", ok: true},
		{sql: "select 'delete from t'", ok: true},
		{sql: "/* drop table t */ select 1", ok: true},
		{sql: "-- drop\nselect 1", ok: true},
		{sql: "select 1 /*! drop table t */", ok: false},
		{sql: "select 1 /*!12345 drop table t */", ok: false},
		{sql: "with x as (select 1) select * from x", ok: true},
		{sql: "with x as (delete from t returning id) select id from x", ok: false},
		{sql: "delete from t", ok: false},
		{sql: "insert into t values (1)", ok: false},
		{sql: "update t set a = 1", ok: false},
		{sql: "drop table t", ok: false},
		{sql: "select * from t for update", ok: false},
		{sql: "select replace(name, 'a', 'b') from t", ok: true},
		{sql: "replace into t values (1)", ok: false},
		{sql: "explain select 1", ok: true},
		{sql: "explain analyze select 1", ok: true},
		{sql: "explain analyze delete from t", ok: false},
		{sql: "select a into outfile '/tmp/x' from t", ok: false},
		{sql: "show create table t", ok: true},
		{sql: "show databases", ok: true},
		{sql: "values (1)", ok: true},
		{sql: "table t", ok: true},
		{sql: "pragma table_info(t)", ok: true},
		{sql: "pragma journal_mode=delete", ok: false},
		{sql: "pragma writable_schema=on", ok: false},
		{sql: "select $tag$; drop table t;$tag$", ok: true},
		{sql: "select 'unterminated", ok: false},
		{sql: "", ok: false},
		{sql: "/* only a comment */", ok: false},
		{sql: "select users.set from t", ok: true},
		{sql: "# mysql comment\nselect 1", ok: true},
		{sql: "select 1\x00", ok: false},
	}
	for _, tc := range cases {
		ok, err := isReadOnlySQL(tc.sql)
		if ok != tc.ok {
			t.Errorf("isReadOnlySQL(%q) = %v, %v; want ok=%v", tc.sql, ok, err, tc.ok)
		}
		if ok && err != nil {
			t.Errorf("isReadOnlySQL(%q) ok with error %v", tc.sql, err)
		}
	}
}

func TestReturnsRows(t *testing.T) {
	cases := []struct {
		sql string
		ok  bool
	}{
		{"select 1", true},
		{"with x as (select 1) select * from x", true},
		{"explain select 1", true},
		{"values (1)", true},
		{"table t", true},
		{"show tables", true},
		{"pragma table_info(t)", true},
		{"delete from t", false},
		{"insert into t values (1)", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := returnsRows(tc.sql); got != tc.ok {
			t.Errorf("returnsRows(%q) = %v, want %v", tc.sql, got, tc.ok)
		}
	}
}

func TestIsLoopbackAddr(t *testing.T) {
	ok := []string{"127.0.0.1", "127.0.0.1:8080", "localhost", "localhost:80", "::1", "[::1]:8080", "LOCALHOST:9"}
	bad := []string{"", "0.0.0.0:8080", "8.8.8.8", "example.com", "example.com:80", "[::]:8080", "127.0.0.1.evil.com"}
	for _, addr := range ok {
		if !isLoopbackAddr(addr) {
			t.Errorf("%q should be loopback", addr)
		}
	}
	for _, addr := range bad {
		if isLoopbackAddr(addr) {
			t.Errorf("%q should not be loopback", addr)
		}
	}
}

func TestCheckRequestHost(t *testing.T) {
	if err := checkRequestHost("127.0.0.1:8080", ""); err != nil {
		t.Fatal(err)
	}
	if err := checkRequestHost("127.0.0.1:8080", "http://127.0.0.1:8080"); err != nil {
		t.Fatal(err)
	}
	if err := checkRequestHost("evil.example:8080", ""); err == nil {
		t.Fatal("expected public host to fail")
	}
	if err := checkRequestHost("127.0.0.1:8080", "http://evil.example"); err == nil {
		t.Fatal("expected foreign origin to fail")
	}
	if err := checkRequestHost("127.0.0.1:8080", "http://127.0.0.1:9"); err == nil {
		t.Fatal("expected origin port mismatch to fail")
	}
	if err := checkRequestHost("localhost:8080", "http://127.0.0.1:8080"); err == nil {
		t.Fatal("expected different loopback names to fail")
	}
}

func FuzzIsReadOnlySQL(f *testing.F) {
	seeds := []string{
		"select 1",
		"select 1; drop table t",
		"/* c */ select 'delete'",
		"with x as (select 1) select * from x",
		"delete from t",
		"select 1;/*",
		"select 'unterminated",
		";;;",
		"SeLeCt 1",
		"explain select 1",
		"pragma table_info(t)",
		"pragma journal_mode=delete",
		"show create table t",
		"select replace(a,'b','c')",
		"--\nselect 1",
		"select $tag$ drop table t $tag$",
		"select 'drop table t'",
		"insert into t values (1)",
		"select 1 union select 2",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, q string) {
		ok, err := isReadOnlySQL(q)
		if ok && err != nil {
			t.Fatalf("ok with error: %v", err)
		}
		if !ok {
			return
		}
		ok2, err2 := isReadOnlySQL(q)
		if !ok2 || err2 != nil {
			t.Fatal("not stable")
		}
		stripped, err := stripSQL(q)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, part := range strings.Split(stripped, ";") {
			if strings.TrimSpace(part) != "" {
				n++
			}
		}
		if n > 1 {
			t.Fatal("read-only allowed multiple statements")
		}
	})
}
