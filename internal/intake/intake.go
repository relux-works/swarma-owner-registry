// Package intake makes pure, current signed-record admission decisions using
// the pinned verifier. It never publishes, executes a plan or opens resources.
package intake

import (
	"bytes"
	"errors"

	"github.com/relux-works/swarma-envelope/record"
	"github.com/relux-works/swarma-envelope/verifier"
	"github.com/relux-works/swarma-owner-registry/internal/pins"
)

const (
	MaxRecordBytes   = 65536
	MaxIdentityBytes = 8192
	MaxEvidenceBytes = 512 * 1024
)

type Code string

const (
	RequestTooLarge         Code = "request_too_large"
	RequestInvalid          Code = "request_invalid"
	RecordTypeUnsupported   Code = "record_type_unsupported"
	VerificationUnavailable Code = "verification_unavailable"
	KeyClassUnsupported     Code = "key_class_unsupported"
)

// Error exposes only a local or unchanged pinned-verifier refusal code.
type Error struct{ Code Code }

func (e *Error) Error() string { return string(e.Code) }
func refuse(code string) error { return &Error{Code: Code(code)} }
func upstream(err error) error {
	var e *record.Error
	if errors.As(err, &e) {
		return refuse(e.Code)
	}
	return refuse(string(VerificationUnavailable))
}

// Snapshot metadata is protected current caller evidence, never inferred from
// its signature. RollbackSafe must come from qualified continuity evidence.
type Snapshot struct {
	Bytes        []byte
	Available    bool
	MaxAge       int64
	RollbackSafe bool
}

// Facts contains only dynamic evidence. Nil collections/values are unavailable;
// nonnil empty collections are available and empty. Plain values use the pinned
// CCJ shape (including int64 numbers). No caller field can replace static pins.
// Parents is leaf-first after the submitted grant; Request is the protected
// concrete {key,action,target} required by Authorize, never a synthetic probe.
// Retained is the caller's exact-byte id journal, not a mutable output store.
type Facts struct {
	Now, Time, Watermark, WatermarkAvailable                                    any
	Audience, Generations, AgentAttestationIDs, SignerFacts, Assurance, Request any
	Attestations, Devices, Services                                             map[string][]byte
	Snapshots                                                                   map[string]Snapshot
	Parents                                                                     [][]byte
	Retained                                                                    map[string][]byte
	RetainedAvailable                                                           bool
}

// Accepted binds a verified type/content id to the authority domain and an
// owned copy of the exact submission. It is not a publication receipt, active
// deployment, cumulative-history proof or permission to execute an effect.
type Accepted struct {
	ID, Type, IssuerRoot string
	Bytes                []byte
}

// Accept checks byte limits and closed routing before invoking published
// profile verification. All refusals return nil; input and facts are unchanged.
// A caller must authenticate the submitter and revalidate current evidence at
// its later serialized publication boundary. Activation gates remain external.
func Accept(kind string, raw []byte, config *pins.Config, facts Facts) (*Accepted, error) {
	limit := MaxRecordBytes
	if kind == "attestation/1" || kind == "revocation/1" {
		limit = MaxIdentityBytes
	}
	if len(raw) > limit {
		return nil, refuse(string(RequestTooLarge))
	}
	switch kind {
	case "attestation/1", "grant/1", "revocation/1":
	default:
		return nil, refuse(string(RecordTypeUnsupported))
	}
	value, err := record.Parse(raw, limit, 16)
	if err != nil {
		return nil, upstream(err)
	}
	envelope, ok := value.(map[string]any)
	if !ok {
		return nil, refuse(string(RequestInvalid))
	}
	body, ok := envelope["body"].(map[string]any)
	if !ok {
		return nil, refuse(string(RequestInvalid))
	}
	if body["type"] != kind {
		return nil, refuse("envelope_type_mismatch")
	}
	id, ok := envelope["id"].(string)
	if !ok {
		return nil, refuse(string(RequestInvalid))
	}
	context, err := config.VerifierContext()
	if err != nil {
		return nil, refuse(string(VerificationUnavailable))
	}
	class, _ := body["key_class"].(string)
	if kind == "attestation/1" {
		if err = config.CheckProfile(kind, class); err != nil {
			return nil, refuse(string(KeyClassUnsupported))
		}
	}
	if err = facts.preflight(len(raw)); err != nil {
		return nil, err
	}
	if err = facts.fill(&context); err != nil {
		return nil, err
	}
	servicePin, _ := config.Pin(pins.GrantService)
	context.ServiceAttestations, err = serviceRecords(facts.Services, servicePin.Key, "", nil)
	if err != nil {
		return nil, err
	}
	issuer, _ := body["issuer"].(string)
	root := issuer
	switch kind {
	case "attestation/1":
		subject, _ := body["subject"].(map[string]any)
		key, _ := subject["key_id"].(string)
		switch class {
		case "agent":
			context.Attestations, err = withRecord(facts.Attestations, id, raw)
		case "operator-device":
			context.DeviceAttestations, err = withRecord(facts.Devices, key, raw)
		case "service":
			context.ServiceAttestations, err = serviceRecords(facts.Services, servicePin.Key, key, raw)
		}
		if err != nil {
			return nil, err
		}
		v := verifier.New(context)
		if class == "service" {
			_, err = v.VerifyServiceAttestation(key)
		} else {
			_, err = v.VerifyAttestation(key, id, class == "operator-device")
		}
		if err != nil {
			return nil, upstream(err)
		}
	case "grant/1":
		chain := make([][]byte, 0, 1+len(facts.Parents))
		chain = append(chain, raw)
		chain = append(chain, facts.Parents...)
		request, err := verifier.Present(facts.Request)
		if facts.Request == nil || err != nil {
			return nil, refuse(string(VerificationUnavailable))
		}
		result := verifier.New(context).Authorize(chain, request)
		if result.Code != "accept" {
			return nil, refuse(result.Code)
		}
		pin, _ := config.Pin(pins.GrantRoot)
		root = pin.Key
	case "revocation/1":
		// Verify the submission itself, not an older snapshot found under its key.
		// Currency/rollback metadata must already exist for this exact issuer domain.
		snapshot, ok := facts.Snapshots[issuer]
		if !ok {
			return nil, refuse(string(VerificationUnavailable))
		}
		snapshots := snapshotRecords(facts.Snapshots)
		snapshot.Bytes = raw
		snapshots[issuer] = snapshot.value()
		context.SignedSnapshots, err = verifier.Present(snapshots)
		if err != nil {
			return nil, refuse(string(VerificationUnavailable))
		}
		state, err := verifier.New(context).CheckRevocation(issuer, nil, nil)
		if err != nil {
			return nil, upstream(err)
		}
		if state != verifier.NotRevoked {
			return nil, refuse("key_binding_unavailable")
		}
	}
	// This check stays after cryptographic/profile verification. Grant E8 checks
	// also run in their original place inside Authorize for every chain record.
	if err = facts.conflict(id, raw); err != nil {
		return nil, err
	}
	return &Accepted{ID: id, Type: kind, IssuerRoot: root, Bytes: bytes.Clone(raw)}, nil
}

func (f Facts) conflict(id string, raw []byte) error {
	if !f.RetainedAvailable || f.Retained == nil {
		return refuse("id_journal_unavailable")
	}
	if previous, ok := f.Retained[id]; ok && !bytes.Equal(previous, raw) {
		return refuse("envelope_conflict")
	}
	return nil
}
func rawRecords(records map[string][]byte) map[string]any {
	if records == nil {
		return nil
	}
	out := make(map[string]any, len(records))
	for key, raw := range records {
		out[key] = raw
	}
	return out
}
func withRecord(records map[string][]byte, key string, raw []byte) (verifier.Evidence, error) {
	out := rawRecords(records)
	if out == nil {
		out = map[string]any{}
	}
	out[key] = raw
	e, err := verifier.Present(out)
	if err != nil {
		return verifier.Evidence{}, refuse(string(VerificationUnavailable))
	}
	return e, nil
}

// Only the configured service subject may enter the protected service collection.
// It keeps its role even without current evidence; nil is not identity evidence.
func serviceRecords(records map[string][]byte, pinned, key string, raw []byte) (verifier.Evidence, error) {
	if key != "" && key != pinned {
		return verifier.Evidence{}, refuse("key_binding_invalid")
	}
	for subject := range records {
		if subject != pinned {
			return verifier.Evidence{}, refuse("key_binding_invalid")
		}
	}
	out := rawRecords(records)
	if out == nil {
		out = map[string]any{}
	}
	if _, exists := out[pinned]; !exists {
		out[pinned] = nil
	}
	if key != "" {
		out[key] = raw
	}
	e, err := verifier.Present(out)
	if err != nil {
		return verifier.Evidence{}, refuse(string(VerificationUnavailable))
	}
	return e, nil
}
func (s Snapshot) value() map[string]any {
	availability := "unavailable"
	if s.Available {
		availability = "available"
	}
	return map[string]any{"snapshot": s.Bytes, "availability": availability, "max_age": s.MaxAge, "rollback_safe": s.RollbackSafe}
}
func snapshotRecords(records map[string]Snapshot) map[string]any {
	if records == nil {
		return nil
	}
	out := make(map[string]any, len(records))
	for key, snapshot := range records {
		out[key] = snapshot.value()
	}
	return out
}
func (f Facts) fill(c *verifier.Context) error {
	slots := []struct {
		value  any
		target *verifier.Evidence
	}{
		{f.Now, &c.Now}, {f.Time, &c.Time}, {f.Watermark, &c.Watermark}, {f.WatermarkAvailable, &c.WatermarkAvailable},
		{f.Audience, &c.Audience}, {f.Generations, &c.CurrentGenerations}, {f.AgentAttestationIDs, &c.AgentAttestationIDs}, {f.SignerFacts, &c.SignerFacts}, {f.Assurance, &c.Assurance},
	}
	for _, slot := range slots {
		if slot.value != nil {
			e, err := verifier.Present(slot.value)
			if err != nil {
				return refuse(string(VerificationUnavailable))
			}
			*slot.target = e
		}
	}
	collections := []struct {
		value  map[string]any
		target *verifier.Evidence
	}{
		{rawRecords(f.Attestations), &c.Attestations}, {rawRecords(f.Devices), &c.DeviceAttestations}, {rawRecords(f.Services), &c.ServiceAttestations},
		{snapshotRecords(f.Snapshots), &c.SignedSnapshots}, {rawRecords(f.Retained), &c.IDJournal},
	}
	for _, slot := range collections {
		if slot.value != nil {
			e, err := verifier.Present(slot.value)
			if err != nil {
				return refuse(string(VerificationUnavailable))
			}
			*slot.target = e
		}
	}
	var err error
	c.IDJournalAvailable, err = verifier.Present(f.RetainedAvailable)
	if err != nil {
		return refuse(string(VerificationUnavailable))
	}
	return nil
}

// preflight charges all supplied raw records plus keys, ordinary CCJ facts and
// snapshot metadata before Present copies evidence or any signature work. This
// is a local decoded-accounting policy, not new wire limits: snapshot metadata
// costs 128 bytes, Retained records allow 65,536 bytes, and ordinary facts use
// canonical encoded size plus depth16/string8192/array64 bounds. Encoding those
// facts can allocate before the budget is checked; transport gates are external.
func (f Facts) preflight(total int) error {
	charge := func(n int) error {
		if n > MaxEvidenceBytes-total {
			return refuse(string(RequestTooLarge))
		}
		total += n
		return nil
	}
	recordBytes := func(raw []byte, limit int) error {
		if len(raw) > limit {
			return refuse(string(RequestTooLarge))
		}
		return charge(len(raw))
	}
	if len(f.Parents) > 3 {
		return refuse("grant_chain_malformed")
	}
	for _, raw := range f.Parents {
		if err := recordBytes(raw, MaxRecordBytes); err != nil {
			return err
		}
	}
	collections := []struct {
		records map[string][]byte
		limit   int
	}{{f.Attestations, MaxIdentityBytes}, {f.Devices, MaxIdentityBytes}, {f.Services, MaxIdentityBytes}, {f.Retained, MaxRecordBytes}}
	for _, collection := range collections {
		for key, raw := range collection.records {
			if err := charge(len(key)); err != nil {
				return err
			}
			if err := recordBytes(raw, collection.limit); err != nil {
				return err
			}
		}
	}
	for key, snapshot := range f.Snapshots {
		if err := charge(len(key) + 128); err != nil {
			return err
		}
		if err := recordBytes(snapshot.Bytes, MaxIdentityBytes); err != nil {
			return err
		}
	}
	for _, value := range []any{f.Now, f.Time, f.Watermark, f.WatermarkAvailable, f.Audience, f.Generations, f.AgentAttestationIDs, f.SignerFacts, f.Assurance, f.Request} {
		if value == nil {
			continue
		}
		encoded, err := record.Canonical(value)
		if err != nil {
			return refuse(string(VerificationUnavailable))
		}
		if err = charge(len(encoded)); err != nil {
			return err
		}
		if _, err = record.Parse(encoded, MaxEvidenceBytes, 16); err != nil {
			return refuse(string(VerificationUnavailable))
		}
	}
	return nil
}
