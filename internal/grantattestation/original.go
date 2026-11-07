package grantattestation

import (
	"encoding/base64"
	"time"
	"unicode/utf8"

	"github.com/relux-works/swarma-envelope/record"
)

func text(s string) bool { return s != "" && len(s) <= 255 && utf8.ValidString(s) }

func object(v any) map[string]any { m, _ := v.(map[string]any); return m }
func stringOf(v any) string       { s, _ := v.(string); return s }
func only(m map[string]any, keys ...string) bool {
	if len(m) != len(keys) {
		return false
	}
	for _, key := range keys {
		if _, ok := m[key]; !ok {
			return false
		}
	}
	return true
}
func singleton(v any, wanted string) bool {
	a, ok := v.([]any)
	return ok && len(a) == 1 && a[0] == wanted
}
func stamp(v any) (time.Time, bool) {
	s, ok := v.(string)
	t, err := time.Parse("2006-01-02T15:04:05Z", s)
	return t, ok && err == nil && len(s) == 20 && t.Year() >= 1970 && t.Year() <= 9999 && t.Format("2006-01-02T15:04:05Z") == s
}

// original verifies the existing closed purpose-5 agent profile and original
// issuer signature, not current-use eligibility or the registry's custody proof.
// It never reserializes bytes returned by Read. Pinned codec primitives provide
// canonical content-ID/signing-preimage construction; no new wire is defined.
func original(wire []byte, p Provenance) bool {
	v, err := record.Parse(wire, MaxWireBytes, 16)
	if err != nil {
		return false
	}
	e := object(v)
	if !only(e, "version", "id", "body", "signatures") || e["version"] != "swarm-envelope/2" || e["id"] != p.ContentID {
		return false
	}
	b := object(e["body"])
	if !only(b, "type", "issuer", "subject", "key_class", "purpose", "roles", "actions", "iat", "exp", "binding") ||
		b["type"] != "attestation/1" || b["issuer"] != p.Issuer || b["key_class"] != "agent" || b["purpose"] != "swarma-msg" ||
		!singleton(b["roles"], "author") || !singleton(b["actions"], "message.sign") {
		return false
	}
	id, err := record.ContentID("attestation/1", b)
	if err != nil || id != p.ContentID {
		return false
	}
	subject := object(b["subject"])
	algorithm, public, err := record.KeyBytes(p.Subject)
	if err != nil || algorithm != "ed25519" || p.Subject == p.Issuer || !only(subject, "algorithm", "key_id", "public_bytes") ||
		subject["algorithm"] != "Ed25519" || subject["key_id"] != p.Subject || subject["public_bytes"] != base64.RawURLEncoding.EncodeToString(public) {
		return false
	}
	binding := object(b["binding"])
	if !only(binding, "uid", "generation", "registration_number", "master_epoch", "owner", "path") || binding["owner"] != p.Owner {
		return false
	}
	uid, uidOK := binding["uid"].(int64)
	number, numberOK := binding["registration_number"].(int64)
	if !uidOK || uid < 0 || uid > 4294967294 || !numberOK || number < 1 || number > 9007199254740991 {
		return false
	}
	for _, field := range []string{"generation", "master_epoch", "owner", "path"} {
		if !text(stringOf(binding[field])) {
			return false
		}
	}
	iat, iatOK := stamp(b["iat"])
	exp, expOK := stamp(b["exp"])
	if !iatOK || !expOK || !exp.After(iat) || exp.Sub(iat) > 86400*time.Second {
		return false
	}
	signatures, ok := e["signatures"].([]any)
	if !ok || len(signatures) != 1 {
		return false
	}
	sig := object(signatures[0])
	if !only(sig, "role", "key", "sig") || sig["role"] != "author" || sig["key"] != p.Issuer {
		return false
	}
	signature, err := record.DecodeBase64(stringOf(sig["sig"]), 64)
	if err != nil {
		return false
	}
	preimage, err := record.SigningPreimage("attestation/1", p.Issuer, "author", b)
	return err == nil && record.VerifySignature(p.Issuer, signature, preimage) == nil
}
