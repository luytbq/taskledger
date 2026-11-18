// Package jsonx writes JSON in the one format taskledger uses everywhere:
// two-space indent, raw UTF-8, no HTML escaping, trailing newline.
package jsonx

import (
	"bytes"
	"encoding/json"
	"io"
)

func Marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := Write(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func Write(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// RequireKeys reports the first key missing from a JSON object. Every field of a
// taskledger schema must be present, so a missing key is a protocol violation even
// when Go would happily decode it as a zero value.
func RequireKeys(raw []byte, keys ...string) (string, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", err
	}
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			return k, nil
		}
	}
	return "", nil
}
