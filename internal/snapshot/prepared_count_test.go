package snapshot

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/relux-works/swarma-envelope/record"
	"github.com/relux-works/swarma-owner-registry/internal/journal"
)

// Independently signed, byte-bounded 64/65-key snapshots distinguish count
// exhaustion from byte exhaustion at the final public preparation boundary.
func TestPreparedCountCapacity(t *testing.T) {
	for _, n := range []int{64, 65} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			c, p, s, sample, _ := publicationFixture(t, "P-JRN-publish-reissue")
			beforeS, beforeC := bytes.Clone(s.Snapshot), bytes.Clone(s.Checkpoint)
			rows := make([]Entry, n)
			for i := range rows {
				seed := sha256.Sum256([]byte(fmt.Sprintf("public snapshot count test %d", i)))
				pub := ed25519.NewKeyFromSeed(seed[:]).Public().(ed25519.PublicKey)
				rows[i] = Entry{"key", "ed25519:" + base64.RawURLEncoding.EncodeToString(pub), "2026-01-02T00:00:00Z"}
			}
			body := map[string]any{"type": "revocation/1", "issuer": s.Registry, "issued_at": "2026-01-01T00:01:05Z", "complete": true, "entries": entryValues(rows)}
			id, e := record.ContentID("revocation/1", body)
			if e != nil {
				t.Fatal(e)
			}
			preimage, e := record.SigningPreimage("revocation/1", s.Registry, "author", body)
			if e != nil {
				t.Fatal(e)
			}
			// This is the already-public deterministic fixture key, never production custody.
			seed := sha256.Sum256([]byte("waggle public test registry revocation seed"))
			sig := ed25519.Sign(ed25519.NewKeyFromSeed(seed[:]), preimage)
			p.Snapshot.Bytes = wire(t, map[string]any{"version": "swarm-envelope/2", "body": body, "id": id, "signatures": []any{map[string]any{"role": "author", "key": s.Registry, "sig": base64.RawURLEncoding.EncodeToString(sig)}}})
			if len(p.Snapshot.Bytes) > MaxBytes {
				t.Fatalf("count witness exceeds byte bound: %d", len(p.Snapshot.Bytes))
			}
			reissue := object(parseObject(t, p.Entries[2])["body"])
			reissue["record"] = id
			p.Entries[2], e = journal.BuildEntry(wire(t, reissue), text(reissue["at"]))
			if e != nil {
				t.Fatal(e)
			}
			cp := parseObject(t, p.CheckpointBody)
			cp["snapshot"] = id
			cp["head"] = parseObject(t, p.Entries[2])["id"]
			p.CheckpointBody = wire(t, cp)
			signer := &countSigner{Signer: fixtureSigner(t)}
			got, e := Prepare(c, signer, s, sample, p)
			signal := false
			if typed, ok := e.(*Error); ok {
				signal = typed.Signal
			}
			t.Logf("entries=%d bytes=%d error=%v signal=%t signer_calls=%d", n, len(p.Snapshot.Bytes), e, signal, signer.calls)
			if !bytes.Equal(beforeS, s.Snapshot) || !bytes.Equal(beforeC, s.Checkpoint) {
				t.Fatal("retained state changed")
			}
			if n == 64 {
				if e != nil {
					t.Fatal(e)
				}
				accepted(t, got.Result)
				return
			}
			if got != nil || signer.calls != 0 {
				t.Fatal("capacity reached custody or returned candidate")
			}
			errorCode(t, e, CapacityExhausted)
			if !e.(*Error).Signal {
				t.Fatal("capacity did not signal")
			}
		})
	}
}
