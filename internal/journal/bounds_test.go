package journal

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/relux-works/waggle/record"
	"github.com/relux-works/waggle/verifier"
)

// Every entry field is closed and typed, including safe integers, UTF-8 byte
// length, prev/genesis, exact calendar time, operation and record/hash grammar.
// Equal-time continuation and the exact safe maximum remain valid controls.
func TestMalformedEntryFieldsRefused(t *testing.T) {
	f := loadFixture(t, "P-JRN-publish-reissue")
	_, es, _ := fixtureProof(t, f)
	base := object(parseObject(t, es[1])["body"])
	for _, tc := range []struct {
		name, key string
		value     any
	}{{"extra", "extra", true}, {"type", "type", "checkpoint/1"}, {"registry", "registry", "invalid"}, {"owner-empty", "owner", ""}, {"owner-bytes", "owner", strings.Repeat("é", 128)}, {"owner-utf8", "owner", string([]byte{255})}, {"zero", "seq", int64(0)}, {"overflow", "seq", MaxSequence + 1}, {"float", "seq", float64(2.5)}, {"null-prev", "prev", nil}, {"bad-prev", "prev", "sha256:X"}, {"bad-date", "at", "2026-02-30T00:00:00Z"}, {"offset", "at", "2026-01-01T00:01:10+00:00"}, {"fraction", "at", "2026-01-01T00:01:10.0Z"}, {"old-year", "at", "1969-12-31T23:59:59Z"}, {"leap-second", "at", "2026-01-01T00:01:60Z"}, {"operation", "op", "replace"}, {"record-type", "record_type", "freshness/1"}, {"record-id", "record", "sha256:" + strings.Repeat("A", 64)}} {
		t.Run(tc.name, func(t *testing.T) {
			m := map[string]any{}
			for k, v := range base {
				m[k] = v
			}
			m[tc.key] = tc.value
			raw, e := record.Canonical(m)
			if e != nil {
				switch tc.name {
				case "owner-utf8":
					raw = bytes.Replace(wire(t, base), []byte(`"owner-a"`), []byte{34, 255, 34}, 1)
				case "overflow":
					raw = bytes.Replace(wire(t, base), []byte(`"seq":2`), []byte(`"seq":9007199254740992`), 1)
				case "float":
					raw = bytes.Replace(wire(t, base), []byte(`"seq":2`), []byte(`"seq":2.5`), 1)
				default:
					t.Fatal(e)
				}
			}
			out, e := BuildEntry(raw, "2026-01-01T00:00:00Z")
			if out != nil {
				t.Fatal("malformed field produced entry")
			}
			errorCode(t, e, Malformed)
		})
	}
	equal := text(base["at"])
	if _, e := BuildEntry(wire(t, base), equal); e != nil {
		t.Fatal(e)
	}
	if out, e := BuildEntry(wire(t, base), "2026-01-01T00:01:11Z"); out != nil || e == nil {
		t.Fatal("backward entry accepted")
	}
	base["seq"] = MaxSequence
	base["owner"] = strings.Repeat("x", 255)
	if _, e := BuildEntry(wire(t, base), equal); e != nil {
		t.Fatal(e)
	}
	genesis := object(parseObject(t, es[0])["body"])
	genesis["prev"] = base["record"]
	out, e := BuildEntry(wire(t, genesis), "")
	if out != nil {
		t.Fatal("genesis predecessor accepted")
	}
	errorCode(t, e, Malformed)
}

// The signer gates every non-profile or foreign-scope body before external
// custody. Valid revocation/checkpoint/freshness use the published preimage;
// upstream diagnostics and invalid returned signatures never escape as bytes.
func TestSignerProfilesRefused(t *testing.T) {
	seed := sha256.Sum256([]byte("waggle public test registry revocation seed"))
	private := ed25519.NewKeyFromSeed(seed[:])
	registry := "ed25519:" + base64.RawURLEncoding.EncodeToString(private.Public().(ed25519.PublicKey))
	calls := 0
	s, e := NewSigner(registry, "owner-a", serviceFunc(func(pre []byte) ([]byte, error) {
		calls++
		if !bytes.HasPrefix(pre, []byte("swarma-")) {
			t.Fatal("unpublished domain")
		}
		return ed25519.Sign(private, pre), nil
	}))
	if e != nil {
		t.Fatal(e)
	}
	c, p, _ := publicationFixture(t)
	_ = c
	cp := parseObject(t, p.CheckpointBody)
	fresh := map[string]any{"type": "freshness/1", "registry": registry, "owner": "owner-a", "publication_seq": int64(1), "prev": nil, "issued_at": cp["at"], "snapshot": cp["snapshot"], "registry_seq": cp["seq"], "checkpoint_id": record.Digest([]byte("checkpoint"))}
	snap := object(parseObject(t, p.Snapshot.Bytes)["body"])
	for _, b := range []map[string]any{snap, cp, fresh} {
		out, e := s.Sign(wire(t, b))
		if e != nil || len(out) == 0 {
			t.Fatal("allowed profile refused", e)
		}
	}
	if calls != 3 {
		t.Fatal("profile control missed service")
	}
	for _, kind := range []string{"grant/1", "attestation/1", "journal-entry/1", "message/1", "enrollment/1", "operator-act/1", "new/1"} {
		out, e := s.Sign(wire(t, map[string]any{"type": kind}))
		if out != nil {
			t.Fatal("non-profile signature returned")
		}
		errorCode(t, e, ProfileRefused)
	}
	cp["owner"] = "owner-b"
	out, e := s.Sign(wire(t, cp))
	if out != nil {
		t.Fatal("foreign owner accepted")
	}
	errorCode(t, e, ProfileRefused)
	if calls != 3 {
		t.Fatal("refused profile reached external service")
	}
	serviceError, _ := NewSigner(registry, "owner-a", serviceFunc(func([]byte) ([]byte, error) { return nil, errors.New("private backend diagnostic") }))
	out, e = serviceError.Sign(wire(t, snap))
	if out != nil {
		t.Fatal("error produced signature")
	}
	errorCode(t, e, SignerUnavailable)
	invalid, _ := NewSigner(registry, "owner-a", serviceFunc(func([]byte) ([]byte, error) { return make([]byte, 64), nil }))
	out, e = invalid.Sign(wire(t, snap))
	if out != nil {
		t.Fatal("invalid signature returned")
	}
	errorCode(t, e, SignerInvalid)
}

// Bounds and duplicate/deep parsing run before any signer call. Each failure
// returns an empty Prepared and verifier plan; a valid transaction signs once.
func TestPublicationBoundsBeforeSignature(t *testing.T) {
	c, p, _ := publicationFixture(t)
	calls := 0
	s := signerFunc(func(b []byte) ([]byte, error) { calls++; return fixtureSigner(t).Sign(b) })
	prepared, r := BuildPublication(c, s, p)
	accepted(t, r)
	if len(prepared.Candidate) == 0 || calls != 1 {
		t.Fatal("positive signer control")
	}
	for _, mode := range []string{"entries65", "entry8193", "snapshot8193", "prior8193", "depth17", "duplicate", "total"} {
		t.Run(mode, func(t *testing.T) {
			_, n, _ := publicationFixture(t)
			switch mode {
			case "entries65":
				n.Entries = make([][]byte, 65)
			case "entry8193":
				n.Entries[0] = bytes.Repeat([]byte(" "), 8193)
			case "snapshot8193":
				n.Snapshot.Bytes = bytes.Repeat([]byte(" "), 8193)
			case "prior8193":
				n.PriorCheckpoint = bytes.Repeat([]byte(" "), 8193)
			case "depth17":
				n.Entries[0] = []byte(strings.Repeat("[", 17) + "0" + strings.Repeat("]", 17))
			case "duplicate":
				n.Entries[0] = []byte(`{"a":1,"a":2}`)
			case "total":
				n.Entries = make([][]byte, 64)
				for i := range n.Entries {
					n.Entries[i] = append(bytes.Repeat([]byte(" "), 8190), []byte("{}")...)
				}
				n.PriorCheckpoint = append(bytes.Repeat([]byte(" "), 8190), []byte("{}")...)
				n.Snapshot.Bytes = bytes.Clone(n.PriorCheckpoint)
			}
			before := calls
			prepared, r := BuildPublication(c, s, n)
			assertPreparedEmpty(t, prepared)
			refusedResult(t, r, "journal_malformed")
			if calls != before {
				t.Fatal("bound violation reached signer")
			}
		})
	}
}

type signerFunc func([]byte) ([]byte, error)

func (f signerFunc) Sign(b []byte) ([]byte, error) { return f(b) }

// Checkpoint fields have a closed shape; independently retained checkpoint
// time permits equality but rejects a backward clock before signer work.
func TestCheckpointFieldsAndTimeRefused(t *testing.T) {
	_, p, _ := publicationFixture(t)
	b := parseObject(t, p.CheckpointBody)
	s := fixtureSigner(t)
	for _, k := range []string{"type", "registry", "owner", "seq", "head", "at", "snapshot", "extra"} {
		t.Run(k, func(t *testing.T) {
			m := map[string]any{}
			for k, v := range b {
				m[k] = v
			}
			m[k] = nil
			out, e := BuildCheckpoint(wire(t, m), "", s)
			if out != nil {
				t.Fatal("malformed checkpoint returned")
			}
			errorCode(t, e, Malformed)
		})
	}
	if _, e := BuildCheckpoint(p.CheckpointBody, text(b["at"]), s); e != nil {
		t.Fatal(e)
	}
	out, e := BuildCheckpoint(p.CheckpointBody, "2026-01-01T00:01:21Z", s)
	if out != nil {
		t.Fatal("backward checkpoint returned")
	}
	errorCode(t, e, Rollback)
}

// Every projection binding is checked against independently supplied originals;
// valid retransmission passes, same-head checkpoint substitution forks and an
// authenticated lower sequence rolls back without producing a derived fact.
func TestProjectionBindingsRefused(t *testing.T) {
	raw, o, old := projectionFixture(t)
	m := parseObject(t, raw)
	for _, k := range []string{"registry_id", "owner", "registry_seq", "registry_head", "checkpoint_id", "checkpoint_time", "candidate_digest", "previous_checkpoint_id", "extra"} {
		t.Run(k, func(t *testing.T) {
			n := map[string]any{}
			for k, v := range m {
				n[k] = v
			}
			n[k] = "invalid"
			got, e := ReduceProjection(wire(t, n), o, nil)
			if got.Valid() {
				t.Fatal("bad binding produced projection")
			}
			errorCode(t, e, ProjectionInvalid)
		})
	}
	if _, e := ReduceProjection(raw, o, &old); e != nil {
		t.Fatal(e)
	}
	cp := parseObject(t, o.Checkpoint)
	body := object(cp["body"])
	body["at"] = "2026-01-01T00:00:01Z"
	o.Checkpoint = signFixture(t, body)
	changed := parseObject(t, raw)
	changed["checkpoint_time"] = body["at"]
	changed["checkpoint_id"] = parseObject(t, o.Checkpoint)["id"]
	got, e := ReduceProjection(wire(t, changed), o, &old)
	if got.Valid() {
		t.Fatal("fork produced fact")
	}
	errorCode(t, e, Fork)
	highBody := object(parseObject(t, o.Checkpoint)["body"])
	highBody["seq"] = int64(2)
	highCP := signFixture(t, highBody)
	highRaw := parseObject(t, wire(t, changed))
	highRaw["registry_seq"] = int64(2)
	highRaw["checkpoint_id"] = parseObject(t, highCP)["id"]
	highOriginals := o
	highOriginals.Checkpoint = highCP
	higher, e := ReduceProjection(wire(t, highRaw), highOriginals, nil)
	if e != nil {
		t.Fatal(e)
	}

	_, o, _ = projectionFixture(t)
	got, e = ReduceProjection(raw, o, &higher)
	if got.Valid() {
		t.Fatal("rollback produced fact")
	}
	errorCode(t, e, Rollback)
}

// The exact 64-entry/two-checkpoint/540672-byte boundary is admitted. One extra
// entry or byte refuses at J1 even if later protected policy is unavailable.
func TestProofBoundsRefused(t *testing.T) {
	f := loadFixture(t, "P-JRN-entries64")
	c, es, cp := fixtureProof(t, f)
	accepted(t, Verify(c, es, cp, nil))
	pad := func(b []byte) []byte {
		return append(bytes.Clone(b), bytes.Repeat([]byte(" "), MaxRecordBytes-len(b))...)
	}
	for i := range es {
		es[i] = pad(es[i])
	}
	cp = pad(cp)
	accepted(t, Verify(c, es, cp, cp))
	refusedResult(t, Verify(verifier.Context{}, append(es, es[0]), cp, cp), "journal_malformed")
	refusedResult(t, Verify(verifier.Context{}, es, append(bytes.Clone(cp), ' '), cp), "journal_malformed")
}

// External custody receives an owned preimage; changing it cannot make a
// signature for different bytes pass the closed wrapper's original preimage.
func TestSignerPreimageMutationRefused(t *testing.T) {
	seed := sha256.Sum256([]byte("waggle public test registry revocation seed"))
	private := ed25519.NewKeyFromSeed(seed[:])
	registry := "ed25519:" + base64.RawURLEncoding.EncodeToString(private.Public().(ed25519.PublicKey))
	_, p, _ := publicationFixture(t)
	snap := object(parseObject(t, p.Snapshot.Bytes)["body"])
	if _, e := fixtureSigner(t).Sign(wire(t, snap)); e != nil {
		t.Fatal(e)
	}
	s, e := NewSigner(registry, "owner-a", serviceFunc(func(pre []byte) ([]byte, error) { pre[0] = 'x'; return ed25519.Sign(private, pre), nil }))
	if e != nil {
		t.Fatal(e)
	}
	out, e := s.Sign(wire(t, snap))
	if out != nil {
		t.Fatal("mutated preimage produced envelope")
	}
	errorCode(t, e, SignerInvalid)
}

// A valid journal reference chain alone cannot satisfy the application's
// reissue binding. The snapshot entry must add the candidate's exact snapshot.
func TestPublicationReissueLinkRefused(t *testing.T) {
	c, p, _ := publicationFixture(t)
	_, r := BuildPublication(c, fixtureSigner(t), p)
	accepted(t, r)
	last := object(parseObject(t, p.Entries[2])["body"])
	last["record"] = object(parseObject(t, p.Entries[0])["body"])["record"]
	changed, e := BuildEntry(wire(t, last), text(last["at"]))
	if e != nil {
		t.Fatal(e)
	}
	p.Entries[2] = changed
	cp := parseObject(t, p.CheckpointBody)
	cp["head"] = parseObject(t, changed)["id"]
	p.CheckpointBody = wire(t, cp)
	// VerifyJournal deliberately checks references rather than content authority.
	candidate := signFixture(t, cp)
	accepted(t, Verify(c, p.Entries, candidate, nil))
	prepared, r := BuildPublication(c, fixtureSigner(t), p)
	assertPreparedEmpty(t, prepared)
	refusedResult(t, r, "journal_chain_broken")
}

// Golden entry bytes bind the exact journal-entry tag and canonical body.
// The separate transaction test checks the signed checkpoint tag.
func TestEntryOriginalBytesAccepted(t *testing.T) {
	f := loadFixture(t, "P-JRN-publish-reissue")
	_, es, _ := fixtureProof(t, f)
	previous := ""
	for _, raw := range es {
		body := object(parseObject(t, raw)["body"])
		built, e := BuildEntry(wire(t, body), previous)
		if e != nil || !bytes.Equal(built, raw) {
			t.Fatal("entry differs from published original", e)
		}
		previous = text(body["at"])
	}
}

// The snapshot signer profile rejects unknown members, incomplete snapshots,
// malformed revocation entries and foreign roots.
func TestSignerProfileShapesRefused(t *testing.T) {
	s := fixtureSigner(t)
	_, p, _ := publicationFixture(t)
	snap := object(parseObject(t, p.Snapshot.Bytes)["body"])
	if _, e := s.Sign(wire(t, snap)); e != nil {
		t.Fatal(e)
	}
	for _, mode := range []string{"extra", "incomplete", "issuer", "entry-kind", "entry-id", "entry-time"} {
		t.Run(mode, func(t *testing.T) {
			b := parseObject(t, wire(t, snap))
			switch mode {
			case "extra":
				b["extra"] = true
			case "incomplete":
				b["complete"] = false
			case "issuer":
				b["issuer"] = "invalid"
			default:
				r := map[string]any{"kind": "record", "id": record.Digest([]byte("item")), "from": b["issued_at"]}
				switch mode {
				case "entry-kind":
					r["kind"] = "unknown"
				case "entry-id":
					r["id"] = "invalid"
				case "entry-time":
					r["from"] = "2026-02-30T00:00:00Z"
				}
				b["entries"] = []any{r}
			}
			out, e := s.Sign(wire(t, b))
			if out != nil {
				t.Fatal("malformed profile signed")
			}
			errorCode(t, e, ProfileRefused)
		})
	}
	// Entry construction also accepts both calendar limits and a valid leap day.
	for _, at := range []string{"1970-01-01T00:00:00Z", "2000-02-29T00:00:00Z", "9999-12-31T23:59:59Z"} {
		f := loadFixture(t, "P-JRN-publish-reissue")
		_, es, _ := fixtureProof(t, f)
		b := object(parseObject(t, es[1])["body"])
		b["at"] = at
		if _, e := BuildEntry(wire(t, b), at); e != nil {
			t.Fatal("calendar boundary refused", e)
		}
	}
}

// Provider errors cannot smuggle diagnostics through an arbitrary typed code;
// valid provider output passes first, and every bad provider leaves no plan.
func TestPublicationSignerFailureEmpty(t *testing.T) {
	c, p, _ := publicationFixture(t)
	_, r := BuildPublication(c, fixtureSigner(t), p)
	accepted(t, r)
	for _, mode := range []string{"error", "unknown-code", "wrong-body"} {
		t.Run(mode, func(t *testing.T) {
			s := signerFunc(func([]byte) ([]byte, error) {
				switch mode {
				case "error":
					return nil, errors.New("private provider diagnostic")
				case "unknown-code":
					return nil, &Error{Code: "private provider diagnostic"}
				default:
					cp := parseObject(t, p.CheckpointBody)
					cp["at"] = "2026-01-01T00:01:21Z"
					return signFixture(t, cp), nil
				}
			})
			prepared, r := BuildPublication(c, s, p)
			assertPreparedEmpty(t, prepared)
			want := "signer_unavailable"
			if mode == "wrong-body" {
				want = "checkpoint_invalid"
			}
			refusedResult(t, r, want)
		})
	}
}
