package journal

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
	"reflect"
	"strings"
	"sync"
	"testing"

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
		if m.Path != "github.com/relux-works/waggle" || m.Version != "v0.0.0-20261025015329-5f4e7435d756" || m.Dir == "" || m.Replace != nil {
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
func refusedResult(t *testing.T, r verifier.Result, want string) {
	t.Helper()
	if r.Code != want || !reflect.DeepEqual(r.Plan, verifier.Plan{}) || r.FreshnessReceipt != nil {
		t.Fatalf("want empty %s, got %+v", want, r)
	}
}
func errorCode(t *testing.T, e error, want Code) {
	t.Helper()
	var typed *Error
	if !errors.As(e, &typed) || typed == nil || typed.Code != want || e.Error() != string(want) {
		t.Fatalf("want %s, got %v", want, e)
	}
}

type serviceFunc func([]byte) ([]byte, error)

func (f serviceFunc) Sign(p []byte) ([]byte, error) { return f(p) }
func fixtureSigner(t *testing.T) Signer {
	t.Helper()
	seed := sha256.Sum256([]byte("waggle public test registry revocation seed"))
	private := ed25519.NewKeyFromSeed(seed[:])
	key := "ed25519:" + base64.RawURLEncoding.EncodeToString(private.Public().(ed25519.PublicKey))
	s, e := NewSigner(key, "owner-a", serviceFunc(func(p []byte) ([]byte, error) { return ed25519.Sign(private, p), nil }))
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
func projectionFixture(t *testing.T) ([]byte, Originals, Projection) {
	t.Helper()
	f := loadFixture(t, "P-JRN-publish-reissue")
	facts := object(f["fixture"])
	w := object(facts["journal_witness"])
	cp := decode(t, object(facts["seen"])[text(w["checkpoint_id"])])
	o := Originals{Registry: text(w["registry_id"]), Owner: text(w["owner"]), Checkpoint: cp, CandidateDigest: record.Digest([]byte("approved candidate bytes"))}
	if w["previous_checkpoint_id"] != nil {
		s := text(w["previous_checkpoint_id"])
		o.PreviousCheckpointID = &s
	}
	m := map[string]any{}
	for k, v := range w {
		m[k] = v
	}
	m["checkpoint_time"] = object(parseObject(t, cp)["body"])["at"]
	m["candidate_digest"] = o.CandidateDigest
	raw := wire(t, m)
	p, e := ReduceProjection(raw, o, nil)
	if e != nil {
		t.Fatal(e)
	}
	return raw, o, p
}
func publicationFixture(t *testing.T) (verifier.Context, Publication, []byte) {
	t.Helper()
	f := loadFixture(t, "P-JRN-publish-reissue")
	c, entries, cp := fixtureProof(t, f)
	_, o, w := projectionFixture(t)
	facts := object(f["fixture"])
	root := text(w.fields["registry_id"])
	row := object(object(facts["revocations"])[root])
	snap := SnapshotFacts{Bytes: decode(t, row["snapshot"]), Available: row["availability"] == "available", RollbackSafe: row["rollback_safe"] == true, MaxAge: number(row["max_age"])}
	return c, Publication{Entries: entries, CheckpointBody: wire(t, parseObject(t, cp)["body"]), PriorCheckpoint: o.Checkpoint, Snapshot: snap, Witness: w}, cp
}
func assertPreparedEmpty(t *testing.T, p Prepared) {
	t.Helper()
	if !reflect.DeepEqual(p, Prepared{}) {
		t.Fatal("refusal retained a prepared candidate")
	}
}
func exactCandidate(t *testing.T, p Prepared, want []byte) {
	t.Helper()
	if !bytes.Equal(p.Candidate, want) {
		t.Fatal("constructed candidate differs from shared original")
	}
}
