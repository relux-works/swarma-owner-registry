package intake_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/relux-works/swarma-owner-registry/internal/intake"
	"github.com/relux-works/swarma-owner-registry/internal/pins"
	"github.com/relux-works/waggle/record"
)

const (
	modulePath    = "github.com/relux-works/waggle"
	moduleVersion = "v0.0.0-20261025015329-5f4e7435d756"
)

type fixtureModule struct {
	Path, Version, Dir string
	Replace            *fixtureModule
}

func pinnedFixtureDirectory(m fixtureModule) (string, error) {
	if m.Path != modulePath || m.Version != moduleVersion || m.Dir == "" || m.Replace != nil {
		return "", fmt.Errorf("fixture module identity differs from reviewed pin or has no directory")
	}
	return m.Dir, nil
}

var fixtureLocation struct {
	sync.Once
	dir string
	err error
}

func fixtureDirectory(t *testing.T) string {
	t.Helper()
	fixtureLocation.Do(func() {
		// Inherit Go's selected environment, including GOENV-only module caches.
		// Every test command and this subprocess run inside the scheduling wrapper.
		cmd := exec.Command("go", "list", "-m", "-json", modulePath)
		cmd.Dir = "../.."
		raw, err := cmd.Output()
		if err != nil {
			fixtureLocation.err = fmt.Errorf("locate pinned fixture module: %w", err)
			return
		}
		var m fixtureModule
		if err = json.Unmarshal(raw, &m); err != nil {
			fixtureLocation.err = err
			return
		}
		fixtureLocation.dir, fixtureLocation.err = pinnedFixtureDirectory(m)
	})
	if fixtureLocation.err != nil {
		t.Fatal(fixtureLocation.err)
	}
	return fixtureLocation.dir
}

// Fixture discovery accepts the reviewed module directory and refuses changed path/version, replacements or absent directories.
func TestFixtureModuleIdentityRefused(t *testing.T) {
	valid := fixtureModule{Path: modulePath, Version: moduleVersion, Dir: fixtureDirectory(t)}
	if dir, err := pinnedFixtureDirectory(valid); err != nil || dir != valid.Dir {
		t.Fatalf("valid fixture directory refused: %v", err)
	}
	for _, name := range []string{"path", "version", "directory", "replacement"} {
		t.Run(name, func(t *testing.T) {
			m := valid
			switch name {
			case "path":
				m.Path = "example.org/other"
			case "version":
				m.Version = "v0.0.0"
			case "directory":
				m.Dir = ""
			case "replacement":
				m.Replace = &valid
			}
			if dir, err := pinnedFixtureDirectory(m); dir != "" || err == nil {
				t.Fatal("changed module identity admitted")
			}
		})
	}
}

type sample struct {
	kind     string
	config   *pins.Config
	document map[string]any
	facts    intake.Facts
	raw      []byte
}

func object(v any) map[string]any { m, _ := v.(map[string]any); return m }
func text(v any) string           { s, _ := v.(string); return s }
func parse(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	v, e := record.Parse(raw, 512*1024, 16)
	if e != nil {
		t.Fatal(e)
	}
	return object(v)
}
func wire(t *testing.T, v any) []byte {
	t.Helper()
	b, e := record.Canonical(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func decode(t *testing.T, v any) []byte {
	t.Helper()
	b, e := record.DecodeBase64(text(v), -1)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func publicKey(n byte) (string, ed25519.PrivateKey) {
	// RFC 8032 section 7.1 published test seed, separated by test role.
	seed, _ := hex.DecodeString("9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60")
	d := sha256.Sum256(append(seed, n))
	k := ed25519.NewKeyFromSeed(d[:])
	return "ed25519:" + base64.RawURLEncoding.EncodeToString(k.Public().(ed25519.PublicKey)), k
}
func config(t *testing.T, d map[string]any) *pins.Config {
	t.Helper()
	c, e := pins.Parse(wire(t, d))
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func load(t *testing.T, name string) sample {
	t.Helper()
	folder := "0-negative"
	if strings.HasPrefix(name, "P-") {
		folder = "1-positive"
	}
	raw, e := os.ReadFile(filepath.Join(fixtureDirectory(t), "vectors/swarm-envelope-2", folder, name+".json"))
	if e != nil {
		t.Fatal(e)
	}
	v := parse(t, raw)
	f := object(v["fixture"])
	s := sample{kind: "grant/1"}
	fs := &s.facts
	fs.Now = f["now"]
	fs.Time = f["time"]
	fs.Watermark = f["watermark"]
	fs.WatermarkAvailable = f["watermark_available"]
	fs.Audience = f["audience"]
	fs.Generations = f["active_generations"]
	fs.AgentAttestationIDs = f["agent_attestation_ids"]
	fs.SignerFacts = f["signer_facts"]
	fs.Assurance = f["assurance"]
	fs.Request = f["request"]
	collections := []struct {
		name string
		out  *map[string][]byte
	}{{"attestations", &fs.Attestations}, {"operator_attestations", &fs.Devices}, {"service_attestations", &fs.Services}, {"seen", &fs.Retained}}
	for _, c := range collections {
		if m, ok := f[c.name].(map[string]any); ok {
			*c.out = map[string][]byte{}
			for k, v := range m {
				(*c.out)[k] = decode(t, v)
			}
		}
	}
	fs.RetainedAvailable = f["id_journal_available"] == true
	fs.Snapshots = map[string]intake.Snapshot{}
	for k, v := range object(f["revocations"]) {
		r := object(v)
		fs.Snapshots[k] = intake.Snapshot{Bytes: decode(t, r["snapshot"]), Available: r["availability"] == "available", MaxAge: r["max_age"].(int64), RollbackSafe: r["rollback_safe"] == true}
	}
	chain := object(v["input"])["chain"].([]any)
	s.raw = decode(t, chain[0])
	for _, r := range chain[1:] {
		fs.Parents = append(fs.Parents, decode(t, r))
	}
	keys := map[pins.Role]string{pins.Purpose5Root: text(object(f["service_root"])["key"]), pins.RegistryOwner: text(object(f["agent_revocation_root"])["key"]), pins.OperatorRoot: text(f["operator_root"]), pins.GrantRoot: text(f["root"])}
	keys[pins.CommitSigner], _ = publicKey(70)
	keys[pins.EnrollmentPublisher], _ = publicKey(71)
	for k := range fs.Services {
		keys[pins.GrantService] = k
		break
	}
	roles := map[string]any{}
	purposes := map[pins.Role]string{pins.RegistryOwner: "registry-revocation", pins.CommitSigner: "git-commit", pins.EnrollmentPublisher: "enrollment/1", pins.Purpose5Root: "purpose-5", pins.OperatorRoot: "operator", pins.GrantRoot: "grant", pins.GrantService: "grant-service"}
	for r, k := range keys {
		scope := "owner-a"
		if r == pins.Purpose5Root {
			scope = "purpose-5"
		}
		if r == pins.RegistryOwner {
			scope = keys[pins.Purpose5Root]
		}
		roles[string(r)] = map[string]any{"key": k, "purpose": purposes[r], "scope": scope}
	}
	s.document = map[string]any{"revision": "5f4e7435d75697385ff6ce15a7501ac6f31c8ea5", "owner": "owner-a", "roles": roles, "record_types": []any{"attestation/1", "grant/1", "revocation/1", "enrollment/1", "journal-entry/1", "checkpoint/1"}, "key_classes": []any{"agent", "operator-device", "service"}, "service_profile": map[string]any{"key_class": "service", "purpose": "grant-service", "roles": []any{"author"}, "actions": []any{"grant.sign"}, "max_lifetime_seconds": int64(86400)}}
	s.config = config(t, s.document)
	return s
}
func check(t *testing.T, s sample, want string) *intake.Accepted {
	t.Helper()
	before := append([]byte(nil), s.raw...)
	factsBefore := cloneFacts(t, s.facts)
	got, e := intake.Accept(s.kind, s.raw, s.config, s.facts)
	if !bytes.Equal(before, s.raw) || !reflect.DeepEqual(factsBefore, s.facts) {
		t.Fatal("admission mutated caller input")
	}
	if want == "accept" {
		if e != nil || got == nil {
			t.Fatalf("want acceptance, got %v", e)
		}
		en := parse(t, s.raw)
		if got.ID != en["id"] || got.Type != object(en["body"])["type"] || !bytes.Equal(got.Bytes, s.raw) || got.IssuerRoot == "" {
			t.Fatal("accepted binding differs")
		}
		return got
	}
	var refusal *intake.Error
	if got != nil || !errors.As(e, &refusal) || string(refusal.Code) != want || e.Error() != want {
		t.Fatalf("want nil verdict/%s, got accepted=%v error=%v", want, got != nil, e)
	}
	return nil
}

// cloneFacts preserves nil and CCJ types, giving refusal tests a whole-input mutation witness.
func cloneFacts(t *testing.T, f intake.Facts) intake.Facts {
	t.Helper()
	out := f
	copyMap := func(m map[string][]byte) map[string][]byte {
		if m == nil {
			return nil
		}
		n := map[string][]byte{}
		for k, v := range m {
			n[k] = bytes.Clone(v)
		}
		return n
	}
	out.Attestations = copyMap(f.Attestations)
	out.Devices = copyMap(f.Devices)
	out.Services = copyMap(f.Services)
	out.Retained = copyMap(f.Retained)
	if f.Snapshots != nil {
		out.Snapshots = map[string]intake.Snapshot{}
		for k, v := range f.Snapshots {
			v.Bytes = bytes.Clone(v.Bytes)
			out.Snapshots[k] = v
		}
	}
	if f.Parents != nil {
		out.Parents = make([][]byte, len(f.Parents))
		for i, b := range f.Parents {
			out.Parents[i] = bytes.Clone(b)
		}
	}
	slots := []*any{&out.Now, &out.Time, &out.Watermark, &out.WatermarkAvailable, &out.Audience, &out.Generations, &out.AgentAttestationIDs, &out.SignerFacts, &out.Assurance, &out.Request}
	for _, slot := range slots {
		if *slot != nil {
			v, e := record.Parse(wire(t, *slot), 512*1024, 16)
			if e != nil {
				t.Fatal(e)
			}
			*slot = v
		}
	}
	return out
}
func sign(t *testing.T, b map[string]any, k string, private ed25519.PrivateKey) []byte {
	t.Helper()
	kind := text(b["type"])
	id, e := record.ContentID(kind, b)
	if e != nil {
		t.Fatal(e)
	}
	pre, e := record.SigningPreimage(kind, k, "author", b)
	if e != nil {
		t.Fatal(e)
	}
	return wire(t, map[string]any{"version": "swarm-envelope/2", "body": b, "id": id, "signatures": []any{map[string]any{"key": k, "role": "author", "sig": base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, pre))}}})
}
func syntheticService(t *testing.T) (sample, ed25519.PrivateKey) {
	t.Helper()
	s := load(t, "P-SVC-chain")
	for _, raw := range s.facts.Services {
		s.raw = raw
		break
	}
	s.kind = "attestation/1"
	service, _ := publicKey(82)
	object(object(s.document["roles"])[string(pins.GrantService)])["key"] = service
	root, private := publicKey(80)
	object(object(s.document["roles"])[string(pins.GrantRoot)])["key"] = root
	s.config = config(t, s.document)
	b := object(parse(t, s.raw)["body"])
	b["issuer"] = root
	object(b["subject"])["key_id"] = service
	object(b["subject"])["public_bytes"] = strings.TrimPrefix(service, "ed25519:")
	s.raw = sign(t, b, root, private)
	snap := sign(t, map[string]any{"type": "revocation/1", "issuer": root, "issued_at": s.facts.Now, "complete": true, "entries": []any{}}, root, private)
	s.facts.Snapshots[root] = intake.Snapshot{Bytes: snap, Available: true, MaxAge: 60, RollbackSafe: true}
	s.facts.Parents = nil
	s.facts.Services = map[string][]byte{service: s.raw}
	return s, private
}

// An unpinned issuer cannot substitute its domain for the protected grant authority.
func TestWrongIssuerScopeRefused(t *testing.T) {
	s, _ := syntheticService(t)
	check(t, s, "accept")
	k, _ := publicKey(81)
	object(object(s.document["roles"])[string(pins.GrantRoot)])["key"] = k
	s.config = config(t, s.document)
	check(t, s, "key_binding_invalid")
}

// Changing only the protected pin rejects the previous service attestation, with or without cached service evidence.
func TestProtectedServiceSubjectAttestationRefused(t *testing.T) {
	for _, cached := range []bool{true, false} {
		t.Run(map[bool]string{true: "cached", false: "submitted-only"}[cached], func(t *testing.T) {
			s, _ := syntheticService(t)
			if !cached {
				s.facts.Services = nil
			}
			check(t, s, "accept")
			alternate, _ := publicKey(96)
			object(object(s.document["roles"])[string(pins.GrantService)])["key"] = alternate
			s.config = config(t, s.document)
			check(t, s, "key_binding_invalid")
		})
	}
}

// A valid delegation stops admitting its old service issuer when only the protected service-subject pin changes.
func TestProtectedServiceSubjectDelegationRefused(t *testing.T) {
	s, private := syntheticService(t)
	s = syntheticDelegation(t, s, private)
	check(t, s, "accept")
	alternate, _ := publicKey(96)
	object(object(s.document["roles"])[string(pins.GrantService)])["key"] = alternate
	s.config = config(t, s.document)
	check(t, s, "key_binding_invalid")
}

// Caller service evidence cannot widen the pinned subject set even alongside valid evidence for the pinned key.
func TestUnpinnedServiceEvidenceRefused(t *testing.T) {
	s, _ := syntheticService(t)
	check(t, s, "accept")
	alternate, _ := publicKey(96)
	s.facts.Services[alternate] = s.raw
	check(t, s, "key_binding_invalid")
}

// A forged grant reports signature failure even when its id already has conflicting retained bytes.
func TestInvalidSignatureBeforeConflict(t *testing.T) {
	s := load(t, "P-SVC-chain")
	check(t, s, "accept")
	e := parse(t, s.raw)
	id := text(e["id"])
	s.facts.Retained[id] = []byte("conflict")
	check(t, s, "envelope_conflict")
	object(e["signatures"].([]any)[0])["sig"] = base64.RawURLEncoding.EncodeToString(make([]byte, 64))
	s.raw = wire(t, e)
	check(t, s, "envelope_signature_invalid")
}

// Duplicate names, including escaped aliases in otherwise complete documents, fail before signature verification.
func TestDuplicateCCJMemberRefused(t *testing.T) {
	s := load(t, "P-SVC-chain")
	check(t, s, "accept")
	for _, member := range []string{`"version"`, `"\u0076ersion"`} {
		n := s
		n.raw = append(bytes.Clone(s.raw[:len(s.raw)-1]), []byte(`,`+member+`:"swarm-envelope/2"}`)...)
		check(t, n, "envelope_malformed")
	}
}

// Over-limit malformed bytes are rejected before decoding without touching caller bytes or evidence.
func TestOversizeNoMutation(t *testing.T) {
	s := load(t, "P-SVC-chain")
	check(t, s, "accept")
	s.raw = bytes.Repeat([]byte("!"), 65537)
	check(t, s, "request_too_large")
	s, _ = syntheticService(t)
	check(t, s, "accept")
	s.raw = append(s.raw, bytes.Repeat([]byte(" "), 8193-len(s.raw))...)
	check(t, s, "request_too_large")
}

// Unpublished classes stay closed even when their bytes fit an accepted attestation shape.
func TestUnknownKeyClassRefused(t *testing.T) {
	s, k := syntheticService(t)
	check(t, s, "accept")
	b := object(parse(t, s.raw)["body"])
	b["key_class"] = "robot"
	s.raw = sign(t, b, text(b["issuer"]), k)
	check(t, s, "key_class_unsupported")
}

// A correctly signed service attestation cannot add an approver role.
func TestServiceForbiddenRoleRefused(t *testing.T) {
	s, k := syntheticService(t)
	check(t, s, "accept")
	b := object(parse(t, s.raw)["body"])
	b["roles"] = []any{"author", "approver"}
	s.raw = sign(t, b, text(b["issuer"]), k)
	check(t, s, "key_binding_invalid")
}

// Service identity alone never supplies a root grant or a missing parent.
func TestServiceMissingParentRefused(t *testing.T) {
	check(t, load(t, "P-SVC-chain"), "accept")
	check(t, load(t, "N-SVC-no-parent"), "grant_root_unknown")
}

// A parent at depth zero cannot authorize a service child.
func TestServiceDepthZeroRefused(t *testing.T) {
	check(t, load(t, "P-SVC-chain"), "accept")
	check(t, load(t, "N-SVC-parent-depth-zero"), "grant_not_narrowing")
}

// A current, correctly signed service leaf is refused when its parent id is revoked.
func TestServiceRevokedParentRefused(t *testing.T) {
	check(t, load(t, "P-SVC-chain"), "accept")
	check(t, load(t, "N-SVC-parent-revoked"), "revoked")
}

// A revoked service-attestation id invalidates both attestation intake and dependent delegation.
func TestServiceRevokedAttestationRefused(t *testing.T) {
	s, k := syntheticService(t)
	check(t, s, "accept")
	dependency := syntheticDelegation(t, s, k)
	check(t, dependency, "accept")
	e := parse(t, s.raw)
	root := text(object(e["body"])["issuer"])
	body := map[string]any{"type": "revocation/1", "issuer": root, "issued_at": s.facts.Now, "complete": true, "entries": []any{map[string]any{"kind": "record", "id": e["id"], "from": s.facts.Now}}}
	snap := s.facts.Snapshots[root]
	snap.Bytes = sign(t, body, root, k)
	s.facts.Snapshots[root] = snap
	check(t, s, "revoked")
	check(t, dependency, "revoked")
}
func syntheticDelegation(t *testing.T, s sample, rootPrivate ed25519.PrivateKey) sample {
	t.Helper()
	base := load(t, "P-SVC-chain")
	root, _ := s.config.Pin(pins.GrantRoot)
	service, servicePrivate := publicKey(82)
	parent := object(parse(t, base.facts.Parents[0])["body"])
	parent["issuer"] = root.Key
	parent["subject"] = service
	parentRaw := sign(t, parent, root.Key, rootPrivate)
	leaf := object(parse(t, base.raw)["body"])
	leaf["issuer"] = service
	leaf["parent"] = parse(t, parentRaw)["id"]
	s.kind = "grant/1"
	s.raw = sign(t, leaf, service, servicePrivate)
	s.facts.Parents = [][]byte{parentRaw}
	return s
}

// All three published identity classes, a root grant and each authorized snapshot domain preserve exact submitted bytes.
func TestAuthorizedSignedRecordAccepted(t *testing.T) {
	base := load(t, "P-SVC-chain")
	for name, collection := range map[string]map[string][]byte{"agent": base.facts.Attestations, "device": base.facts.Devices, "service": base.facts.Services} {
		t.Run(name, func(t *testing.T) {
			for _, raw := range collection {
				s := base
				s.raw = raw
				s.kind = "attestation/1"
				check(t, s, "accept")
			}
		})
	}
	for root, snap := range base.facts.Snapshots {
		s := base
		s.raw = snap.Bytes
		s.kind = "revocation/1"
		got := check(t, s, "accept")
		if got.IssuerRoot != root {
			t.Fatal("wrong snapshot issuer")
		}
	}
	base.raw = base.facts.Parents[0]
	base.facts.Parents = nil
	b := object(parse(t, base.raw)["body"])
	base.facts.Request = map[string]any{"key": b["subject"], "action": "agent.spawn", "target": map[string]any{"kind": "station", "id": "team/member"}}
	check(t, base, "accept")
}

// The pinned public positive chain admits the service leaf and returns the protected root, not the delegating service.
func TestServiceDelegationAccepted(t *testing.T) {
	s := load(t, "P-SVC-chain")
	got := check(t, s, "accept")
	p, _ := s.config.Pin(pins.GrantRoot)
	if got.IssuerRoot != p.Key {
		t.Fatal("wrong grant authority")
	}
	got.Bytes[0] = '!'
	check(t, s, "accept")
}

// Every non-intake type remains closed, and the selected profile cannot relabel signed bytes.
func TestUnsupportedIntakeTypeRefused(t *testing.T) {
	s := load(t, "P-SVC-chain")
	check(t, s, "accept")
	for _, kind := range []string{"enrollment/1", "journal-entry/1", "checkpoint/1", "freshness/1", "message/1", "operator-act/1", "contact", ""} {
		t.Run(kind, func(t *testing.T) { n := s; n.kind = kind; check(t, n, "record_type_unsupported") })
	}
	s.kind = "attestation/1"
	check(t, s, "envelope_type_mismatch")
}

// Missing current facts fail closed through the pinned profile; no static caller override exists.
func TestMissingEvidenceRefused(t *testing.T) {
	check(t, load(t, "P-SVC-chain"), "accept")
	cases := []struct {
		name, code string
		change     func(*sample)
	}{
		{"configuration", "verification_unavailable", func(s *sample) { s.config = nil }},
		{"request", "verification_unavailable", func(s *sample) { s.facts.Request = nil }},
		{"time", "time_unavailable", func(s *sample) { s.facts.Time = nil }},
		{"watermark", "watermark_unavailable", func(s *sample) { s.facts.WatermarkAvailable = false }},
		{"generation", "key_binding_unavailable", func(s *sample) { s.facts.Generations = nil }},
		{"snapshot", "key_binding_unavailable", func(s *sample) { s.facts.Snapshots = nil }},
		{"service_identity", "key_binding_unavailable", func(s *sample) { s.facts.Services = nil }},
		{"id_journal", "id_journal_unavailable", func(s *sample) { s.facts.RetainedAvailable = false }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { s := load(t, "P-SVC-chain"); c.change(&s); check(t, s, c.code) })
	}
}

// Altered candidate signatures cannot be hidden by an already available valid attestation or snapshot.
func TestSubmittedSignatureRefused(t *testing.T) {
	base := load(t, "P-SVC-chain")
	for name, collection := range map[string]map[string][]byte{"agent": base.facts.Attestations, "device": base.facts.Devices, "service": base.facts.Services} {
		t.Run(name, func(t *testing.T) {
			for _, raw := range collection {
				s := base
				s.kind = "attestation/1"
				s.raw = raw
				check(t, s, "accept")
				e := parse(t, raw)
				object(e["signatures"].([]any)[0])["sig"] = base64.RawURLEncoding.EncodeToString(make([]byte, 64))
				s.raw = wire(t, e)
				check(t, s, "key_binding_invalid")
			}
		})
	}
	for _, snapshot := range base.facts.Snapshots {
		s := base
		s.kind = "revocation/1"
		s.raw = snapshot.Bytes
		check(t, s, "accept")
		e := parse(t, s.raw)
		object(e["signatures"].([]any)[0])["sig"] = base64.RawURLEncoding.EncodeToString(make([]byte, 64))
		s.raw = wire(t, e)
		check(t, s, "key_binding_invalid")
	}
}

// Byte and aggregate limits reject before signature work; exactly bounded valid submissions remain accepted.
func TestEvidenceLimits(t *testing.T) {
	s := load(t, "P-SVC-chain")
	s.raw = append(s.raw, bytes.Repeat([]byte(" "), 65536-len(s.raw))...)
	check(t, s, "accept")
	identity, _ := syntheticService(t)
	identity.raw = append(identity.raw, bytes.Repeat([]byte(" "), 8192-len(identity.raw))...)
	check(t, identity, "accept")
	s = load(t, "P-SVC-chain")
	s.facts.Parents = append(s.facts.Parents, make([][]byte, 3)...)
	check(t, s, "grant_chain_malformed")
	s = load(t, "P-SVC-chain")
	s.facts.Parents[0] = bytes.Repeat([]byte("!"), 65537)
	check(t, s, "request_too_large")
	s = load(t, "P-SVC-chain")
	s.facts.Attestations = map[string][]byte{}
	for i := 0; i < 65; i++ {
		key, _ := publicKey(byte(i))
		s.facts.Attestations[key] = bytes.Repeat([]byte("!"), 8192)
	}
	check(t, s, "request_too_large")
}

// Exact retries retain bytes; a whitespace-only alternate under the same id conflicts after valid profile verification.
func TestExactByteBinding(t *testing.T) {
	s, _ := syntheticService(t)
	check(t, s, "accept")
	id := text(parse(t, s.raw)["id"])
	s.facts.Retained[id] = bytes.Clone(s.raw)
	check(t, s, "accept")
	s.raw = append([]byte(" \n"), s.raw...)
	check(t, s, "envelope_conflict")
}

// The current pinned profile refuses stale and rollback-unsafe evidence without a historical-issuance bypass.
func TestSnapshotCurrencyRefused(t *testing.T) {
	s, _ := syntheticService(t)
	check(t, s, "accept")
	s.facts.Now = "2026-01-01T00:02:01Z"
	s.facts.Watermark = s.facts.Now
	check(t, s, "key_binding_unavailable")
	s, _ = syntheticService(t)
	root := text(object(parse(t, s.raw)["body"])["issuer"])
	snap := s.facts.Snapshots[root]
	snap.RollbackSafe = false
	s.facts.Snapshots[root] = snap
	check(t, s, "key_binding_unavailable")
}

// Additional unchanged public service negatives retain their independently published expected refusal codes.
func TestPublishedServiceRefusals(t *testing.T) {
	check(t, load(t, "P-SVC-chain"), "accept")
	cases := map[string]string{"N-SVC-key-revoked": "revoked", "N-SVC-actions": "key_binding_invalid", "N-SVC-subject": "key_binding_invalid", "N-SVC-parent-expired": "expired", "N-SVC-wider-action": "grant_not_narrowing", "N-SVC-owner": "key_binding_invalid", "N-SVC-attestation-expired": "key_binding_invalid", "N-SVC-agent-profile": "key_binding_invalid", "N-SVC-agent-evidence": "key_binding_invalid"}
	for name, code := range cases {
		t.Run(name, func(t *testing.T) { check(t, load(t, name), code) })
	}
}

// A configured service identity stays protected from agent admission even when no current service attestation is supplied.
func TestPinnedServiceRoleCannotBecomeAgent(t *testing.T) {
	base := load(t, "P-SVC-chain")
	p5, private := publicKey(83)
	object(object(base.document["roles"])[string(pins.Purpose5Root)])["key"] = p5
	object(object(base.document["roles"])[string(pins.RegistryOwner)])["scope"] = p5
	base.config = config(t, base.document)
	var original []byte
	for _, raw := range base.facts.Attestations {
		original = raw
		break
	}
	base.facts.Services = nil
	for _, service := range []bool{false, true} {
		subject, _ := publicKey(84)
		if service {
			p, _ := base.config.Pin(pins.GrantService)
			subject = p.Key
		}
		b := object(parse(t, original)["body"])
		old := text(object(b["subject"])["key_id"])
		b["issuer"] = p5
		object(b["subject"])["key_id"] = subject
		object(b["subject"])["public_bytes"] = strings.TrimPrefix(subject, "ed25519:")
		s := base
		s.kind = "attestation/1"
		s.raw = sign(t, b, p5, private)
		oldGen := object(base.facts.Generations)[old]
		s.facts.Generations = map[string]any{subject: oldGen}
		if service {
			check(t, s, "key_binding_invalid")
		} else {
			check(t, s, "accept")
		}
	}
}
