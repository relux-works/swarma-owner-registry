package snapshot

import (
	"bytes"
	"encoding/json"
	"io"
)

// checkEntryCount preserves the entries-specific capacity refusal before
// record.Parse, which publishes only envelope_malformed for its array limit.
// This bounded token scan never admits a snapshot: the unchanged strict record
// parser and profile/signature checks still run afterwards. Strings/escapes are
// tokenized rather than searched; only the root body's entries array is capacity.
func checkEntryCount(raw []byte) error {
	if len(raw) > MaxBytes {
		return fail(CapacityExhausted)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var scan func(int, string) error
	scan = func(depth int, path string) error {
		token, e := d.Token()
		if e != nil {
			return fail(Malformed)
		}
		delimiter, container := token.(json.Delim)
		if !container {
			return nil
		}
		if depth >= 16 {
			return fail(Malformed)
		}
		switch delimiter {
		case '{':
			for d.More() {
				key, e := d.Token()
				if e != nil {
					return fail(Malformed)
				}
				name, ok := key.(string)
				if !ok {
					return fail(Malformed)
				}
				next := ""
				if path == "root" && name == "body" {
					next = "body"
				}
				if path == "body" && name == "entries" {
					next = "entries"
				}
				if e = scan(depth+1, next); e != nil {
					return e
				}
			}
			end, e := d.Token()
			if e != nil || end != json.Delim('}') {
				return fail(Malformed)
			}
		case '[':
			count := 0
			for d.More() {
				count++
				if path == "entries" && count > MaxEntries {
					return fail(CapacityExhausted)
				}
				if count > MaxEntries {
					return fail(Malformed)
				}
				if e := scan(depth+1, ""); e != nil {
					return e
				}
			}
			end, e := d.Token()
			if e != nil || end != json.Delim(']') {
				return fail(Malformed)
			}
		default:
			return fail(Malformed)
		}
		return nil
	}
	if e := scan(0, "root"); e != nil {
		return e
	}
	if _, e := d.Token(); e != io.EOF {
		return fail(Malformed)
	}
	return nil
}
