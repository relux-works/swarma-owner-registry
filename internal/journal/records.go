// Package journal constructs bounded local publication candidates. Acceptance
// is a persistence plan, never proof of witness resolution or publication.
package journal

import (
	"bytes"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/relux-works/swarma-envelope/record"
	"github.com/relux-works/swarma-envelope/verifier"
)

const (
	MaxRecordBytes       = 8192
	MaxEntries           = 64
	MaxCallBytes         = 540672
	MaxSequence    int64 = 9007199254740991
)

type Code string

const (
	Malformed         Code = "journal_malformed"
	ProfileRefused    Code = "signer_profile_refused"
	SignerUnavailable Code = "signer_unavailable"
	SignerInvalid     Code = "signer_signature_invalid"
	ProjectionInvalid Code = "journal_projection_invalid"
	Fork              Code = "journal_fork"
	Rollback          Code = "journal_rollback"
)

// Error never includes caller data, signer diagnostics or key material.
type Error struct{ Code Code }

func (e *Error) Error() string           { return string(e.Code) }
func fail(c Code) error                  { return &Error{Code: c} }
func result(code string) verifier.Result { return verifier.Result{Code: code} }
func text(v any) string                  { s, _ := v.(string); return s }
func number(v any) int64                 { n, _ := v.(int64); return n }
func object(v any) map[string]any        { m, _ := v.(map[string]any); return m }
func shape(m map[string]any, names string) bool {
	keys := strings.Fields(names)
	if len(m) != len(keys) {
		return false
	}
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			return false
		}
	}
	return true
}
func validText(v any) bool {
	s, ok := v.(string)
	return ok && len(s) > 0 && len(s) <= 255 && utf8.ValidString(s)
}
func sequence(v any) bool { n, ok := v.(int64); return ok && n >= 1 && n <= MaxSequence }
func timestamp(v any) bool {
	s, ok := v.(string)
	if !ok || len(s) != 20 {
		return false
	}
	t, e := time.Parse("2006-01-02T15:04:05Z", s)
	return e == nil && t.Year() >= 1970 && t.Year() <= 9999 && t.Format("2006-01-02T15:04:05Z") == s
}
func hash(v any) bool { s, ok := v.(string); return ok && record.IsContentID(s) }
func key(v any) bool {
	s, ok := v.(string)
	if !ok || len(s) > 128 {
		return false
	}
	_, _, e := record.KeyBytes(s)
	return e == nil
}
func common(b map[string]any) bool {
	return key(b["registry"]) && validText(b["owner"]) && sequence(b["seq"]) && timestamp(b["at"])
}
func entryBody(b map[string]any) bool {
	if !shape(b, "type registry owner seq prev at op record_type record") || b["type"] != "journal-entry/1" || !common(b) {
		return false
	}
	if (number(b["seq"]) == 1) != (b["prev"] == nil) || b["prev"] != nil && !hash(b["prev"]) {
		return false
	}
	switch b["record_type"] {
	case "attestation/1", "grant/1", "revocation/1", "enrollment/1":
	default:
		return false
	}
	return (b["op"] == "add" || b["op"] == "revoke") && hash(b["record"])
}
func checkpointBody(b map[string]any) bool {
	return shape(b, "type registry owner seq head at snapshot") && b["type"] == "checkpoint/1" && common(b) && hash(b["head"]) && hash(b["snapshot"])
}
func parse(raw []byte) (map[string]any, error) {
	v, e := record.Parse(raw, MaxRecordBytes, 16)
	if e != nil {
		return nil, fail(Malformed)
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fail(Malformed)
	}
	return m, nil
}
func envelope(raw []byte, checkpoint bool) (map[string]any, error) {
	e, err := parse(raw)
	if err != nil {
		return nil, err
	}
	names := "version body id"
	if checkpoint {
		names += " signatures"
	}
	b := object(e["body"])
	valid := entryBody(b)
	if checkpoint {
		valid = checkpointBody(b)
	}
	if !shape(e, names) || e["version"] != "swarm-envelope/2" || !hash(e["id"]) || !valid {
		return nil, fail(Malformed)
	}
	id, err := record.ContentID(text(b["type"]), b)
	if err != nil || e["id"] != id {
		return nil, fail(Malformed)
	}
	if checkpoint {
		s, ok := e["signatures"].([]any)
		if !ok || len(s) != 1 {
			return nil, fail(Malformed)
		}
		sig := object(s[0])
		if !shape(sig, "role key sig") || sig["role"] != "author" || sig["key"] != b["registry"] {
			return nil, fail(Malformed)
		}
		if _, err = record.DecodeBase64(text(sig["sig"]), 64); err != nil {
			return nil, fail(Malformed)
		}
	}
	return e, nil
}
func encode(v any) ([]byte, error) {
	raw, e := record.Canonical(v)
	if e != nil || len(raw) > MaxRecordBytes {
		return nil, fail(Malformed)
	}
	if _, e = record.Parse(raw, MaxRecordBytes, 16); e != nil {
		return nil, fail(Malformed)
	}
	return raw, nil
}

// BuildEntry accepts a closed body, retaining equality at the time boundary.
// previousAt is separately retained authenticated entry time; empty is genesis.
func BuildEntry(raw []byte, previousAt string) ([]byte, error) {
	b, e := parse(raw)
	if e != nil || !entryBody(b) {
		return nil, fail(Malformed)
	}
	if number(b["seq"]) > 1 && (!timestamp(previousAt) || text(b["at"]) < previousAt) || number(b["seq"]) == 1 && previousAt != "" {
		return nil, fail(Malformed)
	}
	id, e := record.ContentID("journal-entry/1", b)
	if e != nil {
		return nil, fail(Malformed)
	}
	return encode(map[string]any{"version": "swarm-envelope/2", "body": b, "id": id})
}

// BuildCheckpoint keeps prior checkpoint time distinct from entry time. It
// calls only the closed-profile interface; it does not hold a private key.
func BuildCheckpoint(raw []byte, previousAt string, s Signer) ([]byte, error) {
	b, e := parse(raw)
	if e != nil || !checkpointBody(b) {
		return nil, fail(Malformed)
	}
	if previousAt != "" && !timestamp(previousAt) {
		return nil, fail(Malformed)
	}
	if previousAt != "" && text(b["at"]) < previousAt {
		return nil, fail(Rollback)
	}
	if s == nil {
		return nil, fail(SignerUnavailable)
	}
	return s.Sign(bytes.Clone(raw))
}

// Verify forwards the pinned journal verifier's code and zero-on-refusal plan.
// Context is already authenticated protected evidence, never received policy.
func Verify(c verifier.Context, entries [][]byte, candidate, conflict []byte) verifier.Result {
	return verifier.New(c).VerifyJournal(entries, candidate, conflict)
}
