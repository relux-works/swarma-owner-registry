package journal

import (
	"bytes"
	"encoding/base64"

	"github.com/relux-works/waggle/record"
)

// Signer accepts a bounded body for one of three closed owner profiles and
// returns an exact envelope. Implementations must enforce the same boundary.
type Signer interface {
	Sign(body []byte) ([]byte, error)
}

// SignatureService is the external custody transport behind the closed gate.
// Its deployment must independently enforce the approved profiles; implementing
// this interface does not qualify custody, process isolation or secret handling.
type SignatureService interface {
	Sign(preimage []byte) ([]byte, error)
}
type ownerSigner struct {
	registry, owner string
	service         SignatureService
	currencyRoots   map[string]string
}

func NewSigner(registry, owner string, service SignatureService) (Signer, error) {
	if !key(registry) || record.ValidateKey(registry) != nil || !validText(owner) || service == nil {
		return nil, fail(SignerUnavailable)
	}
	return &ownerSigner{registry: registry, owner: owner, service: service}, nil
}
func revocationBody(b map[string]any, registry string) bool {
	if !shape(b, "type issuer issued_at complete entries") || b["issuer"] != registry || !timestamp(b["issued_at"]) || b["complete"] != true {
		return false
	}
	rows, ok := b["entries"].([]any)
	if !ok || len(rows) > 64 {
		return false
	}
	for _, v := range rows {
		r := object(v)
		if !shape(r, "kind id from") || !timestamp(r["from"]) {
			return false
		}
		if r["kind"] == "record" {
			if !hash(r["id"]) {
				return false
			}
		} else if r["kind"] == "key" {
			if !key(r["id"]) {
				return false
			}
		} else {
			return false
		}
	}
	return true
}
func freshnessBody(b map[string]any) bool {
	return shape(b, "type registry owner publication_seq prev issued_at snapshot registry_seq checkpoint_id") && sequence(b["publication_seq"]) && sequence(b["registry_seq"]) && timestamp(b["issued_at"]) && (number(b["publication_seq"]) != 1 || b["prev"] == nil) && (b["prev"] == nil || hash(b["prev"])) && hash(b["snapshot"]) && hash(b["checkpoint_id"])
}
func (s *ownerSigner) Sign(raw []byte) ([]byte, error) {
	b, e := parse(raw)
	if e != nil {
		return nil, e
	}
	kind := text(b["type"])
	switch kind {
	case "revocation/1", "checkpoint/1", "freshness/1", "checkpoint/2", "freshness/2":
	default:
		return nil, fail(ProfileRefused)
	}
	if s.currencyRoots != nil && (kind == "checkpoint/1" || kind == "freshness/1") {
		return nil, fail(ProfileRefused)
	}
	valid := false
	switch kind {
	case "checkpoint/2", "freshness/2":
		valid = s.currencyRoots != nil && currencyBody(b, s.currencyRoots) && b["registry"] == s.registry && b["owner"] == s.owner
	case "revocation/1":
		valid = revocationBody(b, s.registry)
	case "checkpoint/1":
		valid = checkpointBody(b) && b["registry"] == s.registry && b["owner"] == s.owner
	case "freshness/1":
		valid = freshnessBody(b) && b["registry"] == s.registry && b["owner"] == s.owner
	}
	if !valid {
		return nil, fail(ProfileRefused)
	}
	id, e := record.ContentID(kind, b)
	if e != nil {
		return nil, fail(ProfileRefused)
	}
	// Reserve the complete envelope size before calling external custody.
	sig := map[string]any{"role": "author", "key": s.registry, "sig": base64.RawURLEncoding.EncodeToString(make([]byte, 64))}
	outer := map[string]any{"version": "swarm-envelope/2", "body": b, "id": id, "signatures": []any{sig}}
	if _, e = encode(outer); e != nil {
		return nil, e
	}
	pre, e := record.SigningPreimage(kind, s.registry, "author", b)
	if e != nil {
		return nil, fail(ProfileRefused)
	}
	signature, e := s.service.Sign(bytes.Clone(pre))
	if e != nil {
		return nil, fail(SignerUnavailable)
	}
	if record.VerifySignature(s.registry, signature, pre) != nil {
		return nil, fail(SignerInvalid)
	}
	sig["sig"] = base64.RawURLEncoding.EncodeToString(signature)
	return encode(outer)
}
