package journal

import (
	"bytes"
	"encoding/base64"

	"github.com/relux-works/waggle/record"
	"github.com/relux-works/waggle/verifier"
)

// SnapshotFacts carries caller-protected availability, age and rollback facts.
// They cannot be inferred from the received signed snapshot itself.
type SnapshotFacts struct {
	Bytes                   []byte
	Available, RollbackSafe bool
	MaxAge                  int64
}

// Publication includes the prepared checkpoint body and oldest-first suffix,
// normally anchor, new record, reissued snapshot. Missing links are not filled.
type Publication struct {
	Entries                         [][]byte
	CheckpointBody, PriorCheckpoint []byte
	Snapshot                        SnapshotFacts
	Witness                         Projection
}

// Prepared retains exact VerifyJournal inputs. It is empty on every refusal.
// Signed bytes are private candidates until the later publication transaction.
type Prepared struct {
	Entries             [][]byte
	Candidate, Snapshot []byte
	IDJournal           map[string][]byte
	IDJournalAvailable  bool
	Context             verifier.Context
}

func copyEntries(in [][]byte) [][]byte {
	out := make([][]byte, len(in))
	for i, b := range in {
		out[i] = bytes.Clone(b)
	}
	return out
}

func BuildPublication(c verifier.Context, s Signer, p Publication) (Prepared, verifier.Result) {
	refuse := func(code string) (Prepared, verifier.Result) { return Prepared{}, result(code) }
	// Size/count/depth parsing precedes every signature or external signer call.
	if len(p.Entries) > MaxEntries {
		return refuse(string(Malformed))
	}
	total := 0
	inputs := append(append([][]byte{}, p.Entries...), p.CheckpointBody, p.Snapshot.Bytes)
	if len(p.PriorCheckpoint) > 0 {
		inputs = append(inputs, p.PriorCheckpoint)
	}
	for _, b := range inputs {
		if len(b) > MaxRecordBytes {
			return refuse(string(Malformed))
		}
		total += len(b)
		if total > MaxCallBytes {
			return refuse(string(Malformed))
		}
		if _, e := parse(b); e != nil {
			return refuse(string(Malformed))
		}
	}
	if len(p.Witness.raw) > MaxRecordBytes {
		return refuse(string(Malformed))
	}
	total += len(p.Witness.raw)
	if !p.Witness.Valid() {
		return refuse("journal_unavailable")
	}
	body, e := parse(p.CheckpointBody)
	if e != nil || !checkpointBody(body) {
		return refuse(string(Malformed))
	}
	// Charge the complete candidate envelope, not an estimated overhead.
	id, e := record.ContentID("checkpoint/1", body)
	if e != nil {
		return refuse(string(Malformed))
	}
	placeholder := map[string]any{"version": "swarm-envelope/2", "body": body, "id": id, "signatures": []any{map[string]any{"role": "author", "key": body["registry"], "sig": base64.RawURLEncoding.EncodeToString(make([]byte, 64))}}}
	encoded, e := encode(placeholder)
	if e != nil || total-len(p.CheckpointBody)+len(encoded) > MaxCallBytes {
		return refuse(string(Malformed))
	}
	candidate, e := BuildCheckpoint(p.CheckpointBody, "", s)
	if e != nil {
		if typed, ok := e.(*Error); ok {
			switch typed.Code {
			case Malformed, ProfileRefused, SignerUnavailable, SignerInvalid, Rollback:
				return refuse(string(typed.Code))
			}
		}
		return refuse(string(SignerUnavailable))
	}
	cp, e := envelope(candidate, true)
	if e != nil {
		return refuse("checkpoint_invalid")
	}
	signedBody, e := record.Canonical(cp["body"])
	if e != nil {
		return refuse("checkpoint_invalid")
	}
	expectedBody, e := record.Canonical(body)
	if e != nil || !bytes.Equal(signedBody, expectedBody) {
		return refuse("checkpoint_invalid")
	}
	root := text(body["registry"])
	availability := "unavailable"
	if p.Snapshot.Available {
		availability = "available"
	}
	snapshots := map[string]any{root: map[string]any{"snapshot": p.Snapshot.Bytes, "availability": availability, "max_age": p.Snapshot.MaxAge, "rollback_safe": p.Snapshot.RollbackSafe}}
	c.SignedSnapshots, e = verifier.Present(snapshots)
	if e != nil {
		return refuse("journal_unavailable")
	}
	c.JournalWitness = p.Witness.Fact()
	priorID := text(p.Witness.fields["checkpoint_id"])
	seen := map[string]any{}
	journal := map[string][]byte{}
	if len(p.PriorCheckpoint) > 0 {
		seen[priorID] = p.PriorCheckpoint
		journal[priorID] = bytes.Clone(p.PriorCheckpoint)
	}
	c.IDJournalAvailable, _ = verifier.Present(true)
	c.IDJournal, e = verifier.Present(seen)
	if e != nil {
		return refuse("journal_unavailable")
	}
	r := Verify(c, p.Entries, candidate, nil)
	if r.Code != "accept" {
		return Prepared{}, r
	}
	prior, _ := parse(p.PriorCheckpoint)
	if object(prior["body"])["snapshot"] == body["snapshot"] {
		return refuse("checkpoint_snapshot_invalid")
	}
	// Journal admission authenticates references; enforce this builder's exact
	// three-entry publication layout as an additional application constraint.
	if len(p.Entries) != 3 {
		return refuse("journal_chain_broken")
	}
	last, _ := parse(p.Entries[2])
	lb := object(last["body"])
	if lb["op"] != "add" || lb["record_type"] != "revocation/1" || lb["record"] != body["snapshot"] {
		return refuse("journal_chain_broken")
	}
	return Prepared{copyEntries(p.Entries), bytes.Clone(candidate), bytes.Clone(p.Snapshot.Bytes), journal, true, c}, r
}
