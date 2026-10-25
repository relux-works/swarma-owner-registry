//go:build linux

package store

import "errors"

// Local reference encodings preserve nil versus empty bytes/slices. A blank
// reference denotes nil; an empty (nonnull) byte slice has its SHA-256 blob.
// Arguments retains only scalar/string metadata and record type/id fields.
// Every byte payload, including bundle and result, is stored independently once.
type packedRequest struct {
	Arguments                          Request
	Records                            []string
	Journal, Checkpoint, Result        string
	Entries, Publications, Checkpoints []string
	Bundle, Wrapper                    string
}
type packedOutcome struct {
	Sequence        uint64
	Head, Watermark string
	Result          string
}

func bytesRef(b []byte) string {
	if b == nil {
		return ""
	}
	return byteID(b)
}
func outcomeRef(o Outcome) packedOutcome {
	return packedOutcome{o.Sequence, o.Head, o.Watermark, bytesRef(o.Result)}
}
func packRequest(r Request) (packedRequest, map[string][]byte) {
	blobs := map[string][]byte{}
	add := func(b []byte) string {
		if b == nil {
			return ""
		}
		id := byteID(b)
		blobs[id] = b
		return id
	}
	list := func(raw [][]byte) []string {
		if raw == nil {
			return nil
		}
		out := make([]string, len(raw))
		for i, b := range raw {
			out[i] = add(b)
		}
		return out
	}
	p := packedRequest{Arguments: r, Journal: add(r.Journal), Checkpoint: add(r.Checkpoint), Result: add(r.Result), Entries: list(r.Proof.Entries), Publications: list(r.Proof.Publications), Checkpoints: list(r.Proof.Checkpoints), Bundle: add(r.Proof.Bundle), Wrapper: add(r.Proof.Wrapper)}
	// Clone only the record descriptors before clearing their byte fields; never
	// mutate the caller's record slice or any other caller-owned input.
	p.Arguments.Records = append([]Record{}, r.Records...)
	p.Records = make([]string, len(r.Records))
	for i, v := range r.Records {
		p.Records[i] = add(v.Bytes)
		p.Arguments.Records[i].Bytes = nil
	}
	p.Arguments.Journal, p.Arguments.Checkpoint, p.Arguments.Result = nil, nil, nil
	p.Arguments.Proof.Entries, p.Arguments.Proof.Publications, p.Arguments.Proof.Checkpoints = nil, nil, nil
	p.Arguments.Proof.Bundle, p.Arguments.Proof.Wrapper = nil, nil
	return p, blobs
}
func requestDigest(r Request) string { p, _ := packRequest(r); return byteID(canonical(p)) }
func (s *Store) unpackRequest(raw []byte) (Request, error) {
	return expandRequest(raw, s.readBlobLimit)
}
func expandRequest(raw []byte, lookup func(string, int) ([]byte, error)) (Request, error) {
	var p packedRequest
	if !decode(raw, &p) || len(p.Records) != len(p.Arguments.Records) {
		return Request{}, errors.New("invalid retained request")
	}
	if len(p.Records) > 64 || len(p.Entries) > 64 || len(p.Publications) > 64 || len(p.Checkpoints) > 2 || len(p.Arguments.Proof.QueryIDs) > 64 || len(p.Arguments.Proof.QueryKeys) > 64 {
		return Request{}, errors.New("retained reference bounds")
	}
	r := p.Arguments
	get := func(id string, limit int) ([]byte, error) {
		if id == "" {
			return nil, nil
		}
		return lookup(id, limit)
	}
	list := func(ids []string) ([][]byte, error) {
		if ids == nil {
			return nil, nil
		}
		out := make([][]byte, len(ids))
		for i, id := range ids {
			b, e := get(id, 8192)
			if e != nil {
				return nil, e
			}
			out[i] = b
		}
		return out, nil
	}
	for i, id := range p.Records {
		limit := 8192
		if r.Records[i].Type == "grant/1" || r.Records[i].Type == "enrollment/1" {
			limit = 65536
		}
		b, e := get(id, limit)
		if e != nil {
			return Request{}, e
		}
		r.Records[i].Bytes = b
	}
	var e error
	if r.Journal, e = get(p.Journal, 8192); e != nil {
		return Request{}, e
	}
	if r.Checkpoint, e = get(p.Checkpoint, 8192); e != nil {
		return Request{}, e
	}
	if r.Result, e = get(p.Result, 65536); e != nil {
		return Request{}, e
	}
	if r.Proof.Bundle, e = get(p.Bundle, MaxBundleBytes); e != nil {
		return Request{}, e
	}
	if r.Proof.Wrapper, e = get(p.Wrapper, 8192); e != nil {
		return Request{}, e
	}
	if r.Proof.Entries, e = list(p.Entries); e != nil {
		return Request{}, e
	}
	if r.Proof.Publications, e = list(p.Publications); e != nil {
		return Request{}, e
	}
	if r.Proof.Checkpoints, e = list(p.Checkpoints); e != nil {
		return Request{}, e
	}
	// Reject hidden/noncanonical data in byte slots that should contain only nil.
	// Repacking must reproduce every exact scalar, slice and digest reference.
	repacked, _ := packRequest(r)
	if string(canonical(repacked)) != string(raw) {
		return Request{}, errors.New("noncanonical retained references")
	}
	return r, nil
}
