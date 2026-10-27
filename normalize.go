package dbadminer

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const maxSafeInteger int64 = 1 << 53

func normalize(v any, typeName string) any {
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case int64:
		return normInt(t)
	case int32:
		return int64(t)
	case int:
		return normInt(int64(t))
	case int16:
		return int64(t)
	case int8:
		return int64(t)
	case uint64:
		if t > uint64(maxSafeInteger) {
			return strconv.FormatUint(t, 10)
		}
		return t
	case uint32:
		return uint64(t)
	case uint:
		return normUint(uint64(t))
	case float64:
		return normFloat(t)
	case float32:
		return normFloat(float64(t))
	case bool:
		return t
	case string:
		if elem, ok := pgArrayElementType(typeName); ok {
			if parsed, ok := parsePGArray(t); ok {
				return coercePGArray(parsed, elem)
			}
		}
		if isFloatType(typeName) && isNonFiniteText(t) {
			return nil
		}
		return t
	case time.Time:
		return t.Format(time.RFC3339Nano)
	case []byte:
		copied := append([]byte(nil), t...)
		if elem, ok := pgArrayElementType(typeName); ok && utf8.Valid(copied) {
			if parsed, ok := parsePGArray(string(copied)); ok {
				return coercePGArray(parsed, elem)
			}
		}
		return normBytes(copied, typeName)
	case json.RawMessage:
		return append(json.RawMessage(nil), t...)
	default:
		return normReflect(v, typeName)
	}
}

func normInt(v int64) any {
	if v > maxSafeInteger || v < -maxSafeInteger {
		return strconv.FormatInt(v, 10)
	}
	return v
}

func normUint(v uint64) any {
	if v > uint64(maxSafeInteger) {
		return strconv.FormatUint(v, 10)
	}
	return v
}

func normFloat(v float64) any {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return v
}

func isFloatType(typeName string) bool {
	switch strings.ToUpper(strings.TrimSpace(typeName)) {
	case "FLOAT", "FLOAT4", "FLOAT8", "DOUBLE", "DOUBLE PRECISION", "REAL":
		return true
	default:
		return false
	}
}

func isNonFiniteText(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "nan", "inf", "+inf", "-inf", "infinity", "+infinity", "-infinity":
		return true
	default:
		return false
	}
}

type typeKind int

const (
	kindUnknown typeKind = iota
	kindText
	kindJSON
	kindBinary
)

func classifyType(typeName string) typeKind {
	switch strings.ToUpper(strings.TrimSpace(typeName)) {
	case "JSON", "JSONB":
		return kindJSON
	case "BYTEA", "BLOB", "TINYBLOB", "MEDIUMBLOB", "LONGBLOB", "BINARY", "VARBINARY", "BIT", "RAW":
		return kindBinary
	case "":
		return kindUnknown
	default:
		return kindText
	}
}

func normBytes(b []byte, typeName string) any {
	switch classifyType(typeName) {
	case kindJSON:
		if json.Valid(b) {
			return json.RawMessage(b)
		}
		if utf8.Valid(b) {
			return string(b)
		}
		return base64.StdEncoding.EncodeToString(b)
	case kindBinary:
		return base64.StdEncoding.EncodeToString(b)
	default:
		if utf8.Valid(b) {
			return string(b)
		}
		return base64.StdEncoding.EncodeToString(b)
	}
}

func normReflect(v any, typeName string) any {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return nil
	}
	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface:
		if rv.IsNil() {
			return nil
		}
		return normalize(rv.Elem().Interface(), typeName)
	case reflect.Array:
		if rv.Type().Elem().Kind() == reflect.Uint8 && rv.Len() == 16 && (typeName == "" || strings.Contains(strings.ToUpper(typeName), "UUID")) {
			var b [16]byte
			reflect.Copy(reflect.ValueOf(b[:]), rv)
			return formatUUID(b)
		}
		return normSequence(rv)
	case reflect.Slice:
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			buf := make([]byte, rv.Len())
			reflect.Copy(reflect.ValueOf(buf), rv)
			return normBytes(buf, typeName)
		}
		return normSequence(rv)
	default:
		if s, ok := v.(fmt.Stringer); ok {
			return s.String()
		}
		return fmt.Sprint(v)
	}
}

func normSequence(rv reflect.Value) any {
	out := make([]any, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		out[i] = normalize(rv.Index(i).Interface(), "")
	}
	return out
}

func pgArrayElementType(typeName string) (string, bool) {
	u := strings.ToUpper(strings.TrimSpace(typeName))
	switch {
	case strings.HasPrefix(u, "_") && len(u) > 1:
		return u[1:], true
	case strings.HasSuffix(u, "[]") && len(u) > 2:
		return strings.TrimSpace(strings.TrimSuffix(u, "[]")), true
	default:
		return "", false
	}
}

func coercePGArray(v any, elemType string) any {
	items, ok := v.([]any)
	if !ok {
		return coerceArrayElement(v, elemType)
	}
	out := make([]any, len(items))
	for i, item := range items {
		out[i] = coercePGArray(item, elemType)
	}
	return out
}

func coerceArrayElement(v any, elemType string) any {
	if v == nil {
		return nil
	}
	s, ok := v.(string)
	if !ok {
		return normalize(v, elemType)
	}
	switch strings.ToUpper(strings.TrimSpace(elemType)) {
	case "INT2", "INT4", "INT8", "INTEGER", "BIGINT", "SMALLINT", "SERIAL", "BIGSERIAL", "SMALLSERIAL":
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return s
		}
		return normInt(n)
	case "FLOAT", "FLOAT4", "FLOAT8", "DOUBLE", "DOUBLE PRECISION", "REAL":
		n, err := strconv.ParseFloat(s, 64)
		if err != nil {
			if isNonFiniteText(s) {
				return nil
			}
			return s
		}
		return normFloat(n)
	case "BOOL", "BOOLEAN":
		switch strings.ToLower(s) {
		case "t", "true":
			return true
		case "f", "false":
			return false
		default:
			return s
		}
	case "JSON", "JSONB":
		if json.Valid([]byte(s)) {
			return json.RawMessage(s)
		}
		return s
	default:
		return s
	}
}

type pgArrayParser struct {
	s string
	i int
}

func parsePGArray(s string) (any, bool) {
	p := pgArrayParser{s: s}
	v, ok := p.parseValue()
	if !ok || p.i != len(p.s) {
		return nil, false
	}
	if _, ok := v.([]any); !ok {
		return nil, false
	}
	return v, true
}

func (p *pgArrayParser) parseValue() (any, bool) {
	if !p.consume('{') {
		return nil, false
	}
	if p.consume('}') {
		return []any{}, true
	}
	items := []any{}
	for {
		var item any
		var ok bool
		if p.peek() == '{' {
			item, ok = p.parseValue()
		} else {
			item, ok = p.element()
		}
		if !ok {
			return nil, false
		}
		items = append(items, item)
		if p.consume('}') {
			return items, true
		}
		if !p.consume(',') {
			return nil, false
		}
	}
}

func (p *pgArrayParser) element() (any, bool) {
	if p.peek() == '"' {
		return p.quoted()
	}
	start := p.i
	for p.i < len(p.s) {
		switch p.s[p.i] {
		case ',', '}':
			tok := p.s[start:p.i]
			if tok == "NULL" {
				return nil, true
			}
			return tok, true
		default:
			p.i++
		}
	}
	return nil, false
}

func (p *pgArrayParser) quoted() (string, bool) {
	if !p.consume('"') {
		return "", false
	}
	var b strings.Builder
	for p.i < len(p.s) {
		c := p.s[p.i]
		p.i++
		switch c {
		case '\\':
			if p.i >= len(p.s) {
				return "", false
			}
			b.WriteByte(p.s[p.i])
			p.i++
		case '"':
			return b.String(), true
		default:
			b.WriteByte(c)
		}
	}
	return "", false
}

func (p *pgArrayParser) peek() byte {
	if p.i >= len(p.s) {
		return 0
	}
	return p.s[p.i]
}

func (p *pgArrayParser) consume(c byte) bool {
	if p.peek() != c {
		return false
	}
	p.i++
	return true
}

func formatUUID(b [16]byte) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, 36)
	n := 0
	for i, c := range b {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			out[n] = '-'
			n++
		}
		out[n] = hexdigits[c>>4]
		out[n+1] = hexdigits[c&0x0f]
		n += 2
	}
	return string(out)
}

func estimateValue(v any) int64 {
	switch t := v.(type) {
	case nil:
		return 4
	case string:
		return int64(len(t))
	case json.RawMessage:
		return int64(len(t))
	case []any:
		var n int64
		for _, item := range t {
			n += estimateValue(item)
		}
		return n
	default:
		return 32
	}
}
