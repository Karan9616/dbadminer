// Package guard checks SQL text and request hosts for the explorer.
package guard

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

const maxQueryChars = 100_000

var (
	// ErrEmpty is returned for a blank statement.
	ErrEmpty = errors.New("dbadminer: empty query")
	// ErrMulti is returned when more than one statement is present.
	ErrMulti = errors.New("dbadminer: multiple statements are not allowed")
)

// writeKeywords are rejected anywhere they appear as their own word while the
// explorer is read-only. Function calls such as replace(...) are exempt.
var writeKeywords = map[string]struct{}{
	"alter": {}, "analyze": {}, "attach": {}, "begin": {}, "call": {},
	"cluster": {}, "comment": {}, "commit": {}, "copy": {}, "create": {},
	"deallocate": {}, "delete": {}, "detach": {}, "discard": {}, "do": {},
	"drop": {}, "execute": {}, "grant": {}, "handler": {}, "import": {},
	"insert": {}, "install": {}, "into": {}, "listen": {}, "load": {},
	"lock": {}, "merge": {}, "notify": {}, "optimize": {}, "prepare": {},
	"reassign": {}, "refresh": {}, "reindex": {}, "release": {}, "rename": {},
	"repair": {}, "replace": {}, "reset": {}, "revoke": {}, "rollback": {},
	"savepoint": {}, "security": {}, "set": {}, "truncate": {}, "unlisten": {},
	"update": {}, "upsert": {}, "vacuum": {},
}

var readPragmas = map[string]struct{}{
	"collation_list": {}, "compile_options": {}, "data_version": {},
	"database_list": {}, "foreign_key_check": {}, "foreign_key_list": {},
	"foreign_keys": {}, "function_list": {}, "index_info": {}, "index_list": {},
	"index_xinfo": {}, "integrity_check": {}, "module_list": {}, "pragma_list": {},
	"quick_check": {}, "table_info": {}, "table_list": {}, "table_xinfo": {},
}

type sqlToken struct {
	kind string
	word string
}

// isReadOnlySQL reports whether sql is a single read-only statement.
// Comments and quoted text are ignored. MySQL executable comments are not.
func isReadOnlySQL(sql string) (bool, error) {
	if err := validateStatement(sql); err != nil {
		return false, err
	}
	if err := ensureReadOnlyKeywords(sql); err != nil {
		return false, err
	}
	return true, nil
}

func validateStatement(sql string) error {
	if strings.Contains(sql, "\x00") {
		return errors.New("dbadminer: query contains NUL")
	}
	if len(sql) > maxQueryChars {
		return errors.New("dbadminer: query is too long")
	}
	stripped, err := stripSQL(sql)
	if err != nil {
		return err
	}
	n := countStatements(stripped)
	if n == 0 {
		return ErrEmpty
	}
	if n > 1 {
		return ErrMulti
	}
	return nil
}

func ensureReadOnlyKeywords(sql string) error {
	stripped, err := stripSQL(sql)
	if err != nil {
		return err
	}
	toks := tokenize(stripped)
	var words []string
	for _, tok := range toks {
		if tok.kind == "word" {
			words = append(words, tok.word)
		}
	}
	if len(words) == 0 {
		return ErrEmpty
	}
	first := words[0]
	switch first {
	case "select", "with", "explain", "values", "table", "show", "pragma":
	default:
		return fmt.Errorf("dbadminer: %s statements are not allowed in read-only mode", first)
	}
	if first == "pragma" {
		if strings.Contains(stripped, "=") {
			return errors.New("dbadminer: pragma assignments are not allowed in read-only mode")
		}
		if len(words) < 2 {
			return errors.New("dbadminer: pragma is not allowed in read-only mode")
		}
		if _, ok := readPragmas[words[1]]; !ok {
			return errors.New("dbadminer: pragma is not allowed in read-only mode")
		}
		return nil
	}
	if first == "show" {
		return nil
	}
	for i, tok := range toks {
		if tok.kind != "word" {
			continue
		}
		if _, bad := writeKeywords[tok.word]; !bad {
			continue
		}
		if tok.word == "analyze" && first == "explain" {
			continue
		}
		if tok.word == "replace" && i+1 < len(toks) && toks[i+1].kind == "lparen" {
			continue
		}
		return fmt.Errorf("dbadminer: %s is not allowed in read-only mode", tok.word)
	}
	return nil
}

// returnsRows reports whether the statement produces a row set.
func returnsRows(sql string) bool {
	stripped, err := stripSQL(sql)
	if err != nil {
		return false
	}
	for _, tok := range tokenize(stripped) {
		if tok.kind != "word" {
			continue
		}
		switch tok.word {
		case "select", "with", "explain", "values", "table", "show", "pragma":
			return true
		default:
			return false
		}
	}
	return false
}

func countStatements(stripped string) int {
	n := 0
	for _, part := range strings.Split(stripped, ";") {
		if strings.TrimSpace(part) != "" {
			n++
		}
	}
	return n
}

func stripSQL(sql string) (string, error) {
	var b strings.Builder
	s := sql
	i := 0
	for i < len(s) {
		c := s[i]
		if c == '-' && i+1 < len(s) && s[i+1] == '-' {
			i += 2
			for i < len(s) && s[i] != '\n' {
				i++
			}
			b.WriteByte(' ')
			continue
		}
		if c == '#' {
			i++
			for i < len(s) && s[i] != '\n' {
				i++
			}
			b.WriteByte(' ')
			continue
		}
		if c == '/' && i+1 < len(s) && s[i+1] == '*' {
			if i+2 < len(s) && s[i+2] == '!' {
				j := i + 3
				for j < len(s) && s[j] >= '0' && s[j] <= '9' {
					j++
				}
				end := strings.Index(s[j:], "*/")
				if end < 0 {
					return "", errors.New("dbadminer: unterminated comment")
				}
				b.WriteByte(' ')
				b.WriteString(s[j : j+end])
				b.WriteByte(' ')
				i = j + end + 2
				continue
			}
			end := strings.Index(s[i+2:], "*/")
			if end < 0 {
				return "", errors.New("dbadminer: unterminated comment")
			}
			i += 2 + end + 2
			b.WriteByte(' ')
			continue
		}
		if c == '\'' {
			j, err := skipQuoted(s, i, '\'')
			if err != nil {
				return "", err
			}
			b.WriteString("''")
			i = j
			continue
		}
		if c == '"' || c == '`' {
			j, err := skipQuoted(s, i, c)
			if err != nil {
				return "", err
			}
			b.WriteString(s[i:j])
			i = j
			continue
		}
		if c == '$' {
			j, ok, err := skipDollar(s, i)
			if err != nil {
				return "", err
			}
			if ok {
				b.WriteString("''")
				i = j
				continue
			}
		}
		b.WriteByte(c)
		i++
	}
	return b.String(), nil
}

func skipQuoted(s string, i int, quote byte) (int, error) {
	i++
	for i < len(s) {
		if s[i] == quote {
			if i+1 < len(s) && s[i+1] == quote {
				i += 2
				continue
			}
			return i + 1, nil
		}
		i++
	}
	return 0, errors.New("dbadminer: unterminated string")
}

func skipDollar(s string, i int) (int, bool, error) {
	j := i + 1
	for j < len(s) && isIdentByte(s[j]) {
		j++
	}
	if j >= len(s) || s[j] != '$' {
		return 0, false, nil
	}
	tag := s[i : j+1]
	rest := s[j+1:]
	k := strings.Index(rest, tag)
	if k < 0 {
		return 0, false, errors.New("dbadminer: unterminated dollar quote")
	}
	return j + 1 + k + len(tag), true, nil
}

func isIdentByte(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func isWordByte(c byte) bool {
	return c == '.' || isIdentByte(c)
}

func tokenize(stripped string) []sqlToken {
	var out []sqlToken
	var cur strings.Builder
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		out = append(out, sqlToken{kind: "word", word: strings.ToLower(cur.String())})
		cur.Reset()
	}
	inQuote := byte(0)
	for i := 0; i < len(stripped); i++ {
		c := stripped[i]
		if inQuote != 0 {
			if c == inQuote {
				if i+1 < len(stripped) && stripped[i+1] == inQuote {
					i++
					continue
				}
				inQuote = 0
			}
			continue
		}
		if c == '"' || c == '`' || c == '\'' {
			flush()
			inQuote = c
			continue
		}
		if isWordByte(c) {
			cur.WriteByte(c)
			continue
		}
		flush()
		switch c {
		case '(':
			out = append(out, sqlToken{kind: "lparen"})
		case ')':
			out = append(out, sqlToken{kind: "rparen"})
		case '=':
			out = append(out, sqlToken{kind: "eq"})
		}
	}
	flush()
	return out
}

// isLoopbackAddr reports whether addr is localhost or a loopback IP, with or
// without a port. Bracketed IPv6 addresses are accepted.
func isLoopbackAddr(addr string) bool {
	host, err := hostOf(addr)
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func hostOf(addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" || strings.ContainsAny(addr, " \t\r\n\\") {
		return "", errors.New("dbadminer: empty host")
	}
	host := addr
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if host == "" || strings.Contains(host, " ") {
		return "", errors.New("dbadminer: empty host")
	}
	return host, nil
}

func checkRequestHost(hostHeader, origin string) error {
	if !isLoopbackAddr(hostHeader) {
		return errors.New("dbadminer: host is not loopback")
	}
	if _, _, err := net.SplitHostPort(hostHeader); err != nil {
		return errors.New("dbadminer: host is not loopback")
	}
	if origin == "" {
		return nil
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("dbadminer: origin is not allowed")
	}
	if !sameHost(hostHeader, u.Host) || !isLoopbackAddr(u.Host) {
		return errors.New("dbadminer: origin is not allowed")
	}
	return nil
}

func sameHost(a, b string) bool {
	ah, ap, aerr := splitHostPortLoose(a)
	bh, bp, berr := splitHostPortLoose(b)
	if aerr != nil || berr != nil {
		return false
	}
	return strings.EqualFold(ah, bh) && ap == bp
}

func splitHostPortLoose(addr string) (string, string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", "", err
	}
	return strings.Trim(host, "[]"), port, nil
}

// ReadOnlySQL reports whether sql is a single read-only statement.
func ReadOnlySQL(sql string) (bool, error) { return isReadOnlySQL(sql) }

// Validate reports whether sql is a single statement.
func Validate(sql string) error { return validateStatement(sql) }

// ReturnsRows reports whether sql produces a row set.
func ReturnsRows(sql string) bool { return returnsRows(sql) }

// Loopback reports whether addr is a loopback host.
func Loopback(addr string) bool { return isLoopbackAddr(addr) }

// CheckRequestHost rejects a request whose Host or Origin is not loopback.
func CheckRequestHost(hostHeader, origin string) error {
	return checkRequestHost(hostHeader, origin)
}
