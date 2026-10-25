package freshness_test

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	f "github.com/relux-works/swarma-owner-registry/internal/freshness"
	"github.com/relux-works/swarma-owner-registry/internal/journal"
	"github.com/relux-works/swarma-owner-registry/internal/qtime"
	"github.com/relux-works/waggle/record"
	"github.com/relux-works/waggle/verifier"
)

func object(v any) map[string]any { m, _ := v.(map[string]any); return m }
func text(v any) string           { s, _ := v.(string); return s }
func wire(t *testing.T, v any) []byte {
	t.Helper()
	b, e := record.Canonical(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func parsed(t *testing.T, b []byte) map[string]any {
	t.Helper()
	v, e := record.Parse(b, 1<<20, 16)
	if e != nil {
		t.Fatal(e)
	}
	return object(v)
}
func fact(t *testing.T, v any) verifier.Evidence {
	t.Helper()
	e, err := verifier.Present(v)
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func decode(t *testing.T, v any) []byte {
	t.Helper()
	b, e := record.DecodeBase64(text(v), -1)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

var module struct {
	sync.Once
	dir string
	err error
}

func load(t *testing.T, name string) map[string]any {
	t.Helper()
	module.Do(func() {
		b, e := exec.Command("go", "list", "-m", "-json", "github.com/relux-works/waggle").Output()
		if e != nil {
			module.err = e
			return
		}
		var m struct {
			Path, Version, Dir string
			Replace            any
		}
		if e = json.Unmarshal(b, &m); e != nil {
			module.err = e
			return
		}
		if m.Path != "github.com/relux-works/waggle" || m.Version != "v0.0.0-20261025015329-5f4e7435d756" || m.Dir == "" || m.Replace != nil {
			module.err = errors.New("unreviewed fixture module")
			return
		}
		module.dir = m.Dir
	})
	if module.err != nil {
		t.Fatal(module.err)
	}
	dir := "0-negative"
	if strings.HasPrefix(name, "P-") {
		dir = "1-positive"
	}
	b, e := os.ReadFile(filepath.Join(module.dir, "vectors/swarm-envelope-2", dir, name+".json"))
	if e != nil {
		t.Fatal(e)
	}
	return parsed(t, b)
}
func proof(t *testing.T, name string) (verifier.Context, [][]byte, []byte, map[string]any) {
	t.Helper()
	d := load(t, name)
	v := object(d["fixture"])
	var c verifier.Context
	slots := map[string]*verifier.Evidence{"registry_owner": &c.RegistryOwner, "journal_witness": &c.JournalWitness, "freshness_anchor": &c.FreshnessAnchor, "witness_read_at": &c.WitnessReadAt, "agent_revocation_root": &c.AgentRevocationRoot, "service_root": &c.ServiceRoot, "operator_root": &c.OperatorRoot, "root": &c.GrantRoot, "commit_signing_key": &c.CommitSigningKey, "enrollment_publisher": &c.EnrollmentPublisher, "now": &c.Now, "time": &c.Time, "watermark": &c.Watermark, "watermark_available": &c.WatermarkAvailable, "attestations": &c.Attestations, "agent_attestation_ids": &c.AgentAttestationIDs, "active_generations": &c.CurrentGenerations, "operator_attestations": &c.DeviceAttestations, "known_keys": &c.AdmittedKeys}
	for k, s := range slots {
		if x, ok := v[k]; ok {
			*s = fact(t, x)
		}
	}
	attest := map[string]any{}
	for id, x := range object(v["attestations"]) {
		attest[id] = decode(t, x)
	}
	c.Attestations = fact(t, attest)
	snapshots := map[string]any{}
	for root, x := range object(v["revocations"]) {
		row := object(x)
		copy := map[string]any{}
		for k, x := range row {
			copy[k] = x
		}
		copy["snapshot"] = decode(t, row["snapshot"])
		snapshots[root] = copy
	}
	c.SignedSnapshots = fact(t, snapshots)
	in := object(d["input"])
	var chain [][]byte
	for _, x := range in["chain"].([]any) {
		chain = append(chain, decode(t, x))
	}
	return c, chain, decode(t, in["checkpoint"]), d
}
func stringsOf(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, text(x))
	}
	return out
}
func fixtureVerdict(t *testing.T, name, want string) {
	t.Helper()
	c, chain, cp, d := proof(t, name)
	in := object(d["input"])
	got := f.Verify(c, chain, cp, stringsOf(in["ids"]), stringsOf(in["keys"]))
	if got.Code != want {
		t.Fatalf("%s: want %s got %+v", name, want, got)
	}
	if want != "accept" && (!reflect.DeepEqual(got.Plan, verifier.Plan{}) || got.FreshnessReceipt != nil) {
		t.Fatal("refusal exposed plan or receipt")
	}
	encoded, err := json.Marshal(got.Plan)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(parsed(t, encoded), object(d["expected"])["plan"]) {
		t.Fatal("plan differs from published expected result")
	}
}

type service func([]byte) ([]byte, error)

func (s service) Sign(b []byte) ([]byte, error) { return s(b) }

// Published waggle vector seed; no production secret material is used.
func signer(t *testing.T) journal.Signer {
	t.Helper()
	seed := sha256.Sum256([]byte("waggle public test registry revocation seed"))
	key := ed25519.NewKeyFromSeed(seed[:])
	root := "ed25519:" + base64.RawURLEncoding.EncodeToString(key.Public().(ed25519.PublicKey))
	s, e := journal.NewSigner(root, "owner-a", service(func(b []byte) ([]byte, error) { return ed25519.Sign(key, b), nil }))
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func anchor(t *testing.T, m map[string]any) f.Anchor {
	t.Helper()
	var a f.Anchor
	if e := json.Unmarshal(wire(t, m), &a); e != nil {
		t.Fatal(e)
	}
	return a
}
func initial(t *testing.T) f.State {
	t.Helper()
	d := load(t, "P-FR-genesis")
	a := anchor(t, object(object(d["fixture"])["freshness_anchor"]))
	return f.State{Genesis: a, Anchor: a, Watermark: "2026-01-01T00:03:00Z", DecisionFloor: "-1000000000", Boot: "boot-a"}
}
func input(t *testing.T) f.Input { t.Helper(); return inputNamed(t, "P-FR-genesis") }
func inputNamed(t *testing.T, name string) f.Input {
	t.Helper()
	c, _, cp, d := proof(t, name)
	v := object(d["fixture"])
	a := object(v["freshness_anchor"])
	row := object(object(v["revocations"])[text(a["registry_id"])])
	request := object(d["input"])
	return f.Input{IDs: stringsOf(request["ids"]), Keys: stringsOf(request["keys"]), Context: c, Time: intervalAt(t, text(v["now"])), Checkpoint: cp, Snapshot: journal.SnapshotFacts{Bytes: decode(t, row["snapshot"]), Available: true, RollbackSafe: true, MaxAge: 60}, Witness: f.WitnessRead{Fact: c.JournalWitness, ReadAt: text(v["witness_read_at"]), Original: wire(t, v["journal_witness"])}}
}
func at(t *testing.T, in f.Input, when string) f.Input {
	t.Helper()
	in.Time = intervalAt(t, when)
	in.Context.Now = fact(t, when)
	in.Context.Time = fact(t, map[string]any{"trusted": true, "fresh": true, "authority_until": "2026-01-02T00:00:00Z"})
	in.Witness.ReadAt = when
	return in
}

type persistence struct {
	calls   int
	updates []f.Update
	fail    bool
	wrong   bool
}

func (p *persistence) Commit(u f.Update) (string, error) {
	p.calls++
	p.updates = append(p.updates, u.Clone())
	if p.fail {
		return "", errors.New("uncertain write")
	}
	if p.wrong {
		return "sha256:" + strings.Repeat("0", 64), nil
	}
	return f.StateID(u.After)
}
func publisher(t *testing.T, state f.State, p *persistence) *f.Publisher {
	t.Helper()
	return publisherWithSigner(t, state, p, signer(t))
}
func publisherWithSigner(t *testing.T, state f.State, p *persistence, s journal.Signer) *f.Publisher {
	t.Helper()
	id, e := f.StateID(state)
	if e != nil {
		t.Fatal(e)
	}
	x, e := f.New(f.Trust{Registry: state.Genesis.RegistryID, Owner: state.Genesis.Owner, Signer: s, Retained: state, Persistence: p, Recovery: f.Recovery{StateID: id, Reconciled: true, GenesisAuthorized: state.Anchor.PublicationSeq == 0}})
	if e != nil {
		t.Fatal(e)
	}
	return x
}
func publish(t *testing.T, p *f.Publisher, in f.Input) ([]byte, f.Outcome) {
	t.Helper()
	b, r := p.Publish(in)
	if r.Code != "accept" || len(b) == 0 || p.Fenced() {
		t.Fatalf("publication refused: %+v", r)
	}
	return b, r
}
func advanced(t *testing.T, in f.Input) f.Input {
	t.Helper()
	s := signer(t)
	snap := object(parsed(t, in.Snapshot.Bytes)["body"])
	snap["issued_at"] = "2026-01-01T00:03:01Z"
	b, e := s.Sign(wire(t, snap))
	if e != nil {
		t.Fatal(e)
	}
	in.Snapshot.Bytes = b
	cp := object(parsed(t, in.Checkpoint)["body"])
	cp["seq"] = int64(3)
	cp["at"] = "2026-01-01T00:03:01Z"
	cp["head"] = record.Digest([]byte("next journal head"))
	cp["snapshot"] = parsed(t, b)["id"]
	prior := parsed(t, in.Checkpoint)["id"]
	in.Checkpoint, e = s.Sign(wire(t, cp))
	if e != nil {
		t.Fatal(e)
	}
	witness := map[string]any{"registry_id": cp["registry"], "owner": cp["owner"], "registry_seq": cp["seq"], "registry_head": cp["head"], "checkpoint_id": parsed(t, in.Checkpoint)["id"], "previous_checkpoint_id": prior}
	in.Witness.Fact = fact(t, witness)
	in.Witness.Original = wire(t, witness)
	return at(t, in, "2026-01-01T00:03:20Z")
}
func unchanged(t *testing.T, before, after f.State) {
	t.Helper()
	if !reflect.DeepEqual(before, after) {
		t.Fatal("refusal advanced retained state")
	}
}
func noCandidate(t *testing.T, b []byte, r f.Outcome, want string) {
	t.Helper()
	if len(b) != 0 || r.Code != want || !reflect.DeepEqual(r.Plan, verifier.Plan{}) {
		t.Fatalf("want empty %s got bytes=%d %+v", want, len(b), r)
	}
}

type countingSigner struct {
	inner journal.Signer
	calls int
}

func (s *countingSigner) Sign(b []byte) ([]byte, error) { s.calls++; return s.inner.Sign(b) }
func stateRefused(t *testing.T, p *f.Publisher, err error) {
	t.Helper()
	var e *f.Error
	if p != nil || !p.Fenced() || !errors.As(err, &e) || e.Code != "freshness_state_unavailable" || err.Error() != e.Code {
		t.Fatalf("want nil fenced/state_unavailable, got publisher=%v error=%v", p != nil, err)
	}
}

type signerFunc func([]byte) ([]byte, error)

func (s signerFunc) Sign(b []byte) ([]byte, error) { return s(b) }

// Synthetic explicit interval, not a qualified provider receipt. Each later
// second imports the preceding upper floor and uses a distinct sample identity.
func intervalAt(t *testing.T, when string) qtime.Interval {
	t.Helper()
	at, e := time.Parse(time.RFC3339, when)
	if e != nil {
		t.Fatal(e)
	}
	n := strconv.FormatInt(at.UnixNano(), 10)
	floor := strconv.FormatInt(at.Add(-time.Second).UnixNano(), 10)
	return qtime.Interval{Observed: n, Lower: n, Upper: n, AuthorityUntil: strconv.FormatInt(at.Add(300*time.Second).UnixNano(), 10), PriorHighWater: floor, HighWater: n, JournalFloor: floor, Boot: "boot-a", SampleID: "sample-" + n}
}
func issueAt(t *testing.T, in f.Input) string {
	t.Helper()
	v, e := in.Time.Project("1970-01-01T00:00:00Z", in.Time.JournalFloor)
	if e != nil {
		t.Fatal(e)
	}
	return v.IssuedAt
}
