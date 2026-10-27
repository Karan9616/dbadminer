package dbadminer

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"testing"
	"time"
)

func TestNormalize(t *testing.T) {
	if got := normalize(int64(maxSafeInteger), ""); got != int64(maxSafeInteger) {
		t.Fatalf("boundary int = %#v", got)
	}
	if got := normalize(int64(maxSafeInteger)+1, ""); got != "9007199254740993" {
		t.Fatalf("big int = %#v", got)
	}
	if got := normalize(int64(-maxSafeInteger)-1, ""); got != "-9007199254740993" {
		t.Fatalf("big negative = %#v", got)
	}
	if got := normalize(math.NaN(), ""); got != nil {
		t.Fatalf("nan = %#v", got)
	}
	if got := normalize(math.Inf(1), ""); got != nil {
		t.Fatalf("inf = %#v", got)
	}
	if got := normalize("NaN", "TEXT"); got != "NaN" {
		t.Fatalf("text nan = %#v", got)
	}
	if got := normalize("NaN", "FLOAT8"); got != nil {
		t.Fatalf("float text nan = %#v", got)
	}
	stamp := time.Date(2024, 6, 1, 12, 30, 0, 0, time.FixedZone("IST", 5*3600+30*60))
	if got := normalize(stamp, ""); got != stamp.Format(time.RFC3339Nano) {
		t.Fatalf("time = %#v", got)
	}
	raw := []byte(`{"ok":true}`)
	got := normalize(raw, "JSONB")
	msg, ok := got.(json.RawMessage)
	if !ok || string(msg) != string(raw) {
		t.Fatalf("json = %#v", got)
	}
	blob := []byte{0xde, 0xad, 0xbe, 0xef}
	if got := normalize(blob, "BYTEA"); got != base64.StdEncoding.EncodeToString(blob) {
		t.Fatalf("bytea = %#v", got)
	}
	if got := normalize([]byte("alpha"), "TEXT"); got != "alpha" {
		t.Fatalf("text bytes = %#v", got)
	}
	var uuid [16]byte
	uuid[0] = 0x12
	uuid[15] = 0xab
	if got := normalize(uuid, "UUID"); got != "12000000-0000-0000-0000-0000000000ab" {
		t.Fatalf("uuid = %#v", got)
	}
	arr := normalize([]int64{1, maxSafeInteger + 1}, "")
	items, ok := arr.([]any)
	if !ok || len(items) != 2 || items[0] != int64(1) || items[1] != "9007199254740993" {
		t.Fatalf("array = %#v", arr)
	}
	tags := normalize("{a,b}", "_TEXT")
	tagItems, ok := tags.([]any)
	if !ok || len(tagItems) != 2 || tagItems[0] != "a" || tagItems[1] != "b" {
		t.Fatalf("pg text array = %#v", tags)
	}
	if got := normalize("{a,b}", "TEXT"); got != "{a,b}" {
		t.Fatalf("text braces = %#v", got)
	}
	quoted := normalize(`{"a,b","c\"d",NULL}`, "_TEXT")
	quotedItems, ok := quoted.([]any)
	if !ok || len(quotedItems) != 3 || quotedItems[0] != "a,b" || quotedItems[1] != `c"d` || quotedItems[2] != nil {
		t.Fatalf("quoted array = %#v", quoted)
	}
	nums := normalize("{{1,2},{3,4}}", "_INT4")
	outer, ok := nums.([]any)
	if !ok || len(outer) != 2 {
		t.Fatalf("nested array = %#v", nums)
	}
	inner, ok := outer[0].([]any)
	if !ok || len(inner) != 2 || inner[0] != int64(1) || inner[1] != int64(2) {
		t.Fatalf("nested row = %#v", outer[0])
	}
	if got := normalize("{NaN,1}", "_FLOAT8"); !reflectDeep(got, []any{nil, float64(1)}) {
		t.Fatalf("float array = %#v", got)
	}
	if got := normalize("{}", "TEXT[]"); !reflectDeep(got, []any{}) {
		t.Fatalf("empty array = %#v", got)
	}
}

func reflectDeep(got, want any) bool {
	gb, err1 := json.Marshal(got)
	wb, err2 := json.Marshal(want)
	return err1 == nil && err2 == nil && string(gb) == string(wb)
}
