package audit

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestMaskBodyRedactsDeniedKeysAtAnyDepth(t *testing.T) {
	m := NewMasker([]string{"token", "secret", "password"})
	body := []byte(`{"a":1,"secret":"s3cr3t","nested":{"token":"t","keep":"k"},"arr":[{"password":"p","id":7}],"num":12345678901234567890}`)
	got := string(m.MaskBody(body))

	for _, want := range []string{`"a":1`, `"keep":"k"`, `"id":7`, `12345678901234567890`} {
		if !strings.Contains(got, want) {
			t.Errorf("masked = %s, want it to keep %s", got, want)
		}
	}
	for _, leaked := range []string{"s3cr3t", `"t"`, `"p"`} {
		if strings.Contains(got, leaked) {
			t.Errorf("masked body leaked %s: %s", leaked, got)
		}
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(got), &out); err != nil {
		t.Fatalf("masked body is not valid JSON: %v\n%s", err, got)
	}
	redacted := 0
	for _, v := range []any{out["secret"], out["nested"].(map[string]any)["token"], out["arr"].([]any)[0].(map[string]any)["password"]} {
		s := v.(string)
		if !strings.HasPrefix(s, "***:") || len(s) != len("***:")+12 {
			t.Errorf("redaction = %q, want ***:<12 hex>", s)
		}
		redacted++
	}
	if redacted != 3 {
		t.Fatalf("redactions = %d, want 3", redacted)
	}
}

func TestMaskBodyHashCorrelatesSameValues(t *testing.T) {
	m := NewMasker([]string{"token"})
	a := m.MaskBody([]byte(`{"token":"same-secret"}`))
	b := m.MaskBody([]byte(`{"token":"same-secret"}`))
	c := m.MaskBody([]byte(`{"token":"other-secret"}`))
	if string(a) != string(b) {
		t.Fatalf("same value masked differently: %s vs %s", a, b)
	}
	if string(a) == string(c) {
		t.Fatalf("different values masked to the same stub: %s", a)
	}
}

func TestMaskBodyIgnoresInvalidAndNonObject(t *testing.T) {
	m := NewMasker([]string{"secret"})
	for _, body := range []string{`{"secret":"x"`, `"just a string"`, `42`} {
		in := []byte(body)
		if got := m.MaskBody(in); string(got) != body {
			t.Errorf("MaskBody(%s) = %s, want unchanged", body, got)
		}
	}
	// Empty input must pass through untouched (the middleware sends "" when a
	// read has no payload).
	if got := m.MaskBody(nil); len(got) != 0 {
		t.Errorf("MaskBody(nil) = %q, want empty", got)
	}
	// An array body is masked at depth like an object would be.
	if got := string(m.MaskBody([]byte(`[{"secret":"x"}]`))); strings.Contains(got, `"x"`) {
		t.Errorf("array body not masked: %s", got)
	}
}

func TestMaskerNilSafeAndCaseInsensitive(t *testing.T) {
	if m := NewMasker([]string{"", "  "}); m != nil {
		t.Fatalf("blank key list built a masker: %v", m)
	}
	if m := NewMasker(nil); m.Masked() {
		t.Fatal("nil masker reports Masked")
	}
	if got := NewMasker(nil).MaskBody([]byte(`{"TOKEN":"x"}`)); string(got) != `{"TOKEN":"x"}` {
		t.Errorf("nil masker changed the body: %s", got)
	}

	m := NewMasker([]string{"token"})
	if !m.Masked() {
		t.Fatal("masker with a key reports !Masked")
	}
	// Matching is case-insensitive: config documents use every casing.
	got := string(m.MaskBody([]byte(`{"Token":"x"}`)))
	if strings.Contains(got, `"x"`) {
		t.Errorf("case-insensitive match missed: %s", got)
	}
}

func TestMaskQueryRedactsDeniedValues(t *testing.T) {
	m := NewMasker([]string{"token"})
	got := m.MaskQuery("token=abc123&page=2")
	// url.Values.Encode percent-encodes the *** stub; decode to assert.
	vals, err := url.ParseQuery(got)
	if err != nil {
		t.Fatalf("masked query not parseable: %v: %q", err, got)
	}
	if v := vals.Get("token"); !strings.HasPrefix(v, "***:") || len(v) != len("***:")+12 {
		t.Fatalf("token value = %q, want a ***:<12 hex> stub", v)
	}
	if vals.Get("page") != "2" {
		t.Fatalf("page = %q, want the other parameter kept", vals.Get("page"))
	}
	if strings.Contains(got, "abc123") {
		t.Fatalf("query %q still carries the original value", got)
	}
	// No denylisted key: the string comes back byte-identical (no re-encoding).
	if got := m.MaskQuery("a=1&b=2"); got != "a=1&b=2" {
		t.Fatalf("untouched query = %q", got)
	}
	if got := m.MaskQuery(""); got != "" {
		t.Fatalf("empty query = %q", got)
	}
}
