package snapshot

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/relux-works/swarma-owner-registry/internal/journal"
	"github.com/relux-works/swarma-owner-registry/internal/qtime"
	"github.com/relux-works/waggle/record"
	"github.com/relux-works/waggle/verifier"
)

var moduleLocation struct {
	sync.Once
	dir string
	err error
}

func fixtureDir(t *testing.T) string {
	t.Helper()
	moduleLocation.Do(func() {
		cmd := exec.Command("go", "list", "-m", "-json", "github.com/relux-works/waggle")
		cmd.Dir = "../.."
		raw, e := cmd.Output()
		if e != nil {
			moduleLocation.err = e
			return
		}
		var m struct {
			Path, Version, Dir string
			Replace            any
		}
		if e = json.Unmarshal(raw, &m); e != nil {
			moduleLocation.err = e
			return
		}
		if m.Path != "github.com/relux-works/waggle" || m.Version != "v0.0.0-20261026034605-b63d627b19c4" || m.Dir == "" || m.Replace != nil {
			moduleLocation.err = errors.New("fixture module differs from pinned identity")
			return
		}
		moduleLocation.dir = m.Dir
	})
	if moduleLocation.err != nil {
		t.Fatal(moduleLocation.err)
	}
	return moduleLocation.dir
}
func loadFixture(t *testing.T, name string) map[string]any {
	t.Helper()
	folder := "0-negative"
	if strings.HasPrefix(name, "P-") {
		folder = "1-positive"
	}
	raw, e := os.ReadFile(filepath.Join(fixtureDir(t), "vectors/swarm-envelope-2", folder, name+".json"))
	if e != nil {
		t.Fatal(e)
	}
	v, e := record.Parse(raw, 1<<20, 16)
	if e != nil {
		t.Fatal(e)
	}
	return v.(map[string]any)
}
func wire(t *testing.T, v any) []byte {
	t.Helper()
	b, e := record.Canonical(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func parseObject(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	v, e := record.Parse(raw, 1<<20, 16)
	if e != nil {
		t.Fatal(e)
	}
	return v.(map[string]any)
}
func decode(t *testing.T, v any) []byte {
	t.Helper()
	b, e := record.DecodeBase64(text(v), -1)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func present(t *testing.T, v any) verifier.Evidence {
	t.Helper()
	e, err := verifier.Present(v)
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func fixtureProof(t *testing.T, f map[string]any) (verifier.Context, [][]byte, []byte) {
	t.Helper()
	facts := object(f["fixture"])
	var c verifier.Context
	slots := map[string]*verifier.Evidence{"registry_owner": &c.RegistryOwner, "journal_witness": &c.JournalWitness, "agent_revocation_root": &c.AgentRevocationRoot, "service_root": &c.ServiceRoot, "operator_root": &c.OperatorRoot, "root": &c.GrantRoot, "commit_signing_key": &c.CommitSigningKey, "enrollment_publisher": &c.EnrollmentPublisher, "now": &c.Now, "time": &c.Time, "watermark": &c.Watermark, "watermark_available": &c.WatermarkAvailable, "id_journal_available": &c.IDJournalAvailable, "active_generations": &c.CurrentGenerations, "operator_attestations": &c.DeviceAttestations}
	for k, s := range slots {
		if v, ok := facts[k]; ok {
			*s = present(t, v)
		}
	}
	snapshots := map[string]any{}
	for k, v := range object(facts["revocations"]) {
		r := object(v)
		row := map[string]any{}
		for n, x := range r {
			row[n] = x
		}
		row["snapshot"] = decode(t, r["snapshot"])
		snapshots[k] = row
	}
	c.SignedSnapshots = present(t, snapshots)
	seen := map[string]any{}
	for k, v := range object(facts["seen"]) {
		seen[k] = decode(t, v)
	}
	c.IDJournal = present(t, seen)
	input := object(f["input"])
	var entries [][]byte
	for _, v := range input["entries"].([]any) {
		entries = append(entries, decode(t, v))
	}
	return c, entries, decode(t, input["checkpoint"])
}
func accepted(t *testing.T, r verifier.Result) {
	t.Helper()
	if r.Code != "accept" || len(r.Plan.IDs) != 1 || r.Plan.Watermark == "" {
		t.Fatalf("positive control refused: %+v", r)
	}
}
func errorCode(t *testing.T, e error, want Code) {
	t.Helper()
	var typed *Error
	if !errors.As(e, &typed) || typed.Code != want || e.Error() != string(want) {
		t.Fatalf("want %s, got %v", want, e)
	}
}

type serviceFunc func([]byte) ([]byte, error)

func (f serviceFunc) Sign(p []byte) ([]byte, error) { return f(p) }
func fixtureSigner(t *testing.T) journal.Signer {
	t.Helper()
	seed := sha256.Sum256([]byte("waggle public test registry revocation seed"))
	private := ed25519.NewKeyFromSeed(seed[:])
	key := "ed25519:" + base64.RawURLEncoding.EncodeToString(private.Public().(ed25519.PublicKey))
	s, e := journal.NewSigner(key, "owner-a", serviceFunc(func(p []byte) ([]byte, error) { return ed25519.Sign(private, p), nil }))
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func signFixture(t *testing.T, b map[string]any) []byte {
	t.Helper()
	raw, e := fixtureSigner(t).Sign(wire(t, b))
	if e != nil {
		t.Fatal(e)
	}
	return raw
}

func text(v any) string           { s, _ := v.(string); return s }
func number(v any) int64          { n, _ := v.(int64); return n }
func object(v any) map[string]any { m, _ := v.(map[string]any); return m }

func publicationFixture(t *testing.T, name string) (verifier.Context, journal.Publication, Retained, qtime.Interval, []byte) {
	t.Helper()
	f := loadFixture(t, name)
	c, entries, cp := fixtureProof(t, f)
	positive := loadFixture(t, "P-JRN-publish-reissue")
	facts := object(positive["fixture"])
	w := object(facts["journal_witness"])
	prior := decode(t, object(facts["seen"])[text(w["checkpoint_id"])])
	o := journal.Originals{Registry: text(w["registry_id"]), Owner: text(w["owner"]), Checkpoint: prior, CandidateDigest: record.Digest([]byte("approved candidate bytes"))}
	m := map[string]any{}
	for k, v := range w {
		m[k] = v
	}
	m["checkpoint_time"] = object(parseObject(t, prior)["body"])["at"]
	m["candidate_digest"] = o.CandidateDigest
	projection, e := journal.ReduceProjection(wire(t, m), o, nil)
	if e != nil {
		t.Fatal(e)
	}
	old := signFixture(t, map[string]any{"type": "revocation/1", "issuer": o.Registry, "issued_at": "2026-01-01T00:01:00Z", "complete": true, "entries": []any{}})
	if parseObject(t, old)["id"] != object(parseObject(t, prior)["body"])["snapshot"] {
		t.Fatal("prior snapshot differs from protected checkpoint")
	}
	state := Retained{Available: true, RollbackSafe: true, Registry: o.Registry, Owner: o.Owner, Snapshot: old, Checkpoint: prior, Watermark: "2026-01-01T00:01:00Z", DecisionFloor: "0"}
	facts = object(f["fixture"])
	row := object(object(facts["revocations"])[o.Registry])
	prior = nil
	if v, ok := object(facts["seen"])[text(w["checkpoint_id"])]; ok {
		prior = decode(t, v)
	}
	p := journal.Publication{Entries: entries, CheckpointBody: wire(t, parseObject(t, cp)["body"]), PriorCheckpoint: prior, Witness: projection, Snapshot: journal.SnapshotFacts{Bytes: decode(t, row["snapshot"]), Available: row["availability"] == "available", RollbackSafe: row["rollback_safe"] == true, MaxAge: number(row["max_age"])}}
	sample := intervalAt(t, text(facts["now"]))
	return c, p, state, sample, cp
}
func prepareRefused(t *testing.T, c verifier.Context, p journal.Publication, s Retained, clock qtime.Interval, want string) {
	t.Helper()
	beforeS := wire(t, map[string]any{"snapshot": base64.RawURLEncoding.EncodeToString(s.Snapshot), "checkpoint": base64.RawURLEncoding.EncodeToString(s.Checkpoint)})
	got, e := Prepare(c, fixtureSigner(t), s, clock, p)
	errorCode(t, e, Code(want))
	if got != nil {
		t.Fatal("refusal returned candidate")
	}
	if !bytes.Equal(beforeS, wire(t, map[string]any{"snapshot": base64.RawURLEncoding.EncodeToString(s.Snapshot), "checkpoint": base64.RawURLEncoding.EncodeToString(s.Checkpoint)})) {
		t.Fatal("refusal changed retained originals")
	}
}

// Explicit synthetic interval; qualification and boot continuity are caller contracts.
func intervalAt(t *testing.T, when string) qtime.Interval {
	t.Helper()
	at, e := time.Parse(time.RFC3339, when)
	if e != nil {
		t.Fatal(e)
	}
	n := strconv.FormatInt(at.UnixNano(), 10)
	return qtime.Interval{Observed: n, Lower: n, Upper: n, AuthorityUntil: strconv.FormatInt(at.Add(300*time.Second).UnixNano(), 10), PriorHighWater: "0", HighWater: n, JournalFloor: "0", Boot: "boot-a", SampleID: "sample-a"}
}
