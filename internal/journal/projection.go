package journal

import (
	"bytes"

	"github.com/relux-works/swarma-envelope/verifier"
)

// Originals are supplied only after external authentication of checkpoint,
// scope, candidate inventory, ancestry and explicit genesis where applicable.
// This pure reducer does not authenticate a wire or establish latestness.
type Originals struct {
	Registry, Owner      string
	Checkpoint           []byte
	CandidateDigest      string
	PreviousCheckpointID *string
}

// Projection retains the eight-field projection and original checkpoint. Its
// zero value is unavailable; only ReduceProjection creates a valid value.
type Projection struct {
	raw, fact, checkpoint []byte
	fields                map[string]any
}

func (p Projection) Valid() bool        { return p.fields != nil }
func (p Projection) Bytes() []byte      { return bytes.Clone(p.raw) }
func (p Projection) FactBytes() []byte  { return bytes.Clone(p.fact) }
func (p Projection) Checkpoint() []byte { return bytes.Clone(p.checkpoint) }
func (p Projection) Fact() verifier.Evidence {
	if !p.Valid() {
		return verifier.Evidence{}
	}
	v, _ := parse(p.fact)
	e, _ := verifier.Present(v)
	return e
}

func ReduceProjection(raw []byte, o Originals, previous *Projection) (Projection, error) {
	refuse := func(c Code) (Projection, error) { return Projection{}, fail(c) }
	m, e := parse(raw)
	if e != nil {
		return refuse(ProjectionInvalid)
	}
	cp, e := envelope(o.Checkpoint, true)
	if e != nil {
		return refuse(ProjectionInvalid)
	}
	b := object(cp["body"])
	if !shape(m, "registry_id owner registry_seq registry_head checkpoint_id checkpoint_time candidate_digest previous_checkpoint_id") {
		return refuse(ProjectionInvalid)
	}
	var predecessor any
	if o.PreviousCheckpointID != nil {
		predecessor = *o.PreviousCheckpointID
		if !hash(predecessor) {
			return refuse(ProjectionInvalid)
		}
	}
	if !key(o.Registry) || !validText(o.Owner) || !hash(o.CandidateDigest) || m["registry_id"] != o.Registry || b["registry"] != o.Registry || m["owner"] != o.Owner || b["owner"] != o.Owner || m["registry_seq"] != b["seq"] || m["registry_head"] != b["head"] || m["checkpoint_id"] != cp["id"] || m["checkpoint_time"] != b["at"] || m["candidate_digest"] != o.CandidateDigest || m["previous_checkpoint_id"] != predecessor || predecessor == cp["id"] {
		return refuse(ProjectionInvalid)
	}
	if previous != nil {
		if !previous.Valid() || previous.fields["registry_id"] != m["registry_id"] || previous.fields["owner"] != m["owner"] {
			return refuse(ProjectionInvalid)
		}
		old := previous.fields
		if m["registry_seq"] == old["registry_seq"] && (m["registry_head"] != old["registry_head"] || m["checkpoint_id"] != old["checkpoint_id"]) {
			return refuse(Fork)
		}
		if number(m["registry_seq"]) < number(old["registry_seq"]) {
			return refuse(Rollback)
		}
	}
	fact := map[string]any{}
	for _, k := range []string{"registry_id", "owner", "registry_seq", "registry_head", "checkpoint_id", "previous_checkpoint_id"} {
		fact[k] = m[k]
	}
	fb, e := encode(fact)
	if e != nil {
		return refuse(ProjectionInvalid)
	}
	return Projection{bytes.Clone(raw), fb, bytes.Clone(o.Checkpoint), m}, nil
}
