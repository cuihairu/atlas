package audit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

var errUnexpectedToken = errors.New("unexpected JSON token")

// Masker redacts denylisted keys in audit payloads at capture time — before
// an entry reaches the ring, the structured log or a sink — so every surface
// shows the same masked body. The audit surface has no credential-bearing
// endpoints (RBAC keys enter via env), but three payload classes can carry
// operator-sensitive data: cross-server config documents (game configs may
// embed fleet secrets), private tag payloads and raw query strings.
//
// A redacted value becomes "***:<12 hex>" where the hex is the start of the
// SHA-256 of the redacted value's JSON bytes: enough to tell two records that
// carried the same secret apart without ever writing the secret down. The
// shape mirrors httpapi.Actor's key fingerprint on purpose.
type Masker struct {
	deny map[string]struct{}
}

// NewMasker builds a masker from a comma-separated key list; empty keys are
// ignored and an all-empty list yields a nil Masker (no masking).
func NewMasker(keys []string) *Masker {
	deny := map[string]struct{}{}
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k != "" {
			deny[strings.ToLower(k)] = struct{}{}
		}
	}
	if len(deny) == 0 {
		return nil
	}
	return &Masker{deny: deny}
}

// Masked reports whether any key is on the denylist.
func (m *Masker) Masked() bool { return m != nil }

func (m *Masker) denied(key string) bool {
	if m == nil {
		return false
	}
	_, ok := m.deny[strings.ToLower(key)]
	return ok
}

// MaskBody applies the key denylist to a JSON body at any nesting depth.
// Invalid JSON passes through unchanged (the ring wraps invalid bodies as
// strings upstream), and non-object values are returned verbatim.
func (m *Masker) MaskBody(body []byte) []byte {
	if m == nil || len(body) == 0 || !json.Valid(body) {
		return body
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	out := &bytes.Buffer{}
	if err := m.copy(out, dec); err != nil {
		return body
	}
	return out.Bytes()
}

// MaskQuery redacts the values of denylisted query parameters. Parsing fails
// (invalid percent escapes) leave the string untouched; unchanged queries are
// returned byte-identical.
func (m *Masker) MaskQuery(q string) string {
	if m == nil || q == "" {
		return q
	}
	vals, err := url.ParseQuery(q)
	if err != nil {
		return q
	}
	changed := false
	for k := range vals {
		if !m.denied(k) {
			continue
		}
		for i, v := range vals[k] {
			vals[k][i] = redact([]byte(v))
			changed = true
		}
	}
	if !changed {
		return q
	}
	return vals.Encode()
}

// redact renders the hash stub for a redacted value.
func redact(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "***:" + hex.EncodeToString(sum[:])[:12]
}

// copy walks one JSON value from dec into out, redacting the value of every
// denylisted object key. With a nil Masker it is a pure compact re-encoding.
func (m *Masker) copy(out *bytes.Buffer, dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	switch v := tok.(type) {
	case json.Delim:
		switch v {
		case '{':
			out.WriteByte('{')
			first := true
			for dec.More() {
				if !first {
					out.WriteByte(',')
				}
				first = false
				keyTok, err := dec.Token()
				if err != nil {
					return err
				}
				key, ok := keyTok.(string)
				if !ok {
					return errUnexpectedToken
				}
				if err := writeJSON(out, key); err != nil {
					return err
				}
				out.WriteByte(':')
				if m.denied(key) {
					raw := &bytes.Buffer{}
					if err := (*Masker)(nil).copy(raw, dec); err != nil {
						return err
					}
					out.WriteString(`"` + redact(raw.Bytes()) + `"`)
				} else if err := m.copy(out, dec); err != nil {
					return err
				}
			}
			if err := expectDelim(dec, '}'); err != nil {
				return err
			}
			out.WriteByte('}')
		case '[':
			out.WriteByte('[')
			first := true
			for dec.More() {
				if !first {
					out.WriteByte(',')
				}
				first = false
				if err := m.copy(out, dec); err != nil {
					return err
				}
			}
			if err := expectDelim(dec, ']'); err != nil {
				return err
			}
			out.WriteByte(']')
		default:
			return errUnexpectedToken
		}
	case string:
		return writeJSON(out, v)
	case bool:
		out.WriteString(strconv.FormatBool(v))
	case json.Number:
		out.WriteString(v.String())
	case nil:
		out.WriteString("null")
	default:
		return errUnexpectedToken
	}
	return nil
}

func expectDelim(dec *json.Decoder, want json.Delim) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	d, ok := tok.(json.Delim)
	if !ok || d != want {
		return errUnexpectedToken
	}
	return nil
}

func writeJSON(out *bytes.Buffer, s string) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	_, err = out.Write(b)
	return err
}
