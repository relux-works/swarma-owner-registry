package composition_test

import (
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

	c "github.com/relux-works/swarma-owner-registry/internal/composition"
	"github.com/relux-works/swarma-owner-registry/internal/qtime"
	"github.com/relux-works/waggle/record"
	"github.com/relux-works/waggle/verifier"
)

func object(v any) map[string]any { m, _ := v.(map[string]any); return m }
func text(v any) string           { s, _ := v.(string); return s }
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
		if m.Path != "github.com/relux-works/waggle" || m.Version != "v0.0.0-20261026034605-b63d627b19c4" || m.Dir == "" || m.Replace != nil {
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

// Only public signed vectors are loaded; runtime receipts are always minted by F.
type fixture struct {
	proof                c.Proof
	facts                map[string]any
	input                map[string]any
	root, key, reference string
	reanchor             bool
}

func setup(t *testing.T, name string) fixture {
	t.Helper()
	d := load(t, name)
	f := object(d["fixture"])
	in := object(d["input"])
	mint := object(in["mint"])
	m := object(mint["input"])
	var ctx verifier.Context
	slots := map[string]*verifier.Evidence{"id_journal_available": &ctx.IDJournalAvailable, "registry_owner": &ctx.RegistryOwner, "journal_witness": &ctx.JournalWitness, "freshness_anchor": &ctx.FreshnessAnchor, "witness_read_at": &ctx.WitnessReadAt, "agent_revocation_root": &ctx.AgentRevocationRoot, "service_root": &ctx.ServiceRoot, "operator_root": &ctx.OperatorRoot, "root": &ctx.GrantRoot, "commit_signing_key": &ctx.CommitSigningKey, "enrollment_publisher": &ctx.EnrollmentPublisher, "now": &ctx.Now, "time": &ctx.Time, "watermark": &ctx.Watermark, "watermark_available": &ctx.WatermarkAvailable, "agent_attestation_ids": &ctx.AgentAttestationIDs, "active_generations": &ctx.CurrentGenerations, "known_keys": &ctx.AdmittedKeys, "audience": &ctx.Audience, "required_roles": &ctx.RequiredRoles, "operator_owner": &ctx.OperatorOwner, "replay_available": &ctx.ReplayAvailable, "used_actions": &ctx.UsedActions, "signer_facts": &ctx.SignerFacts, "assurance": &ctx.Assurance, "delivery": &ctx.Delivery, "binding": &ctx.ActBinding, "service_authority": &ctx.ServiceAuthority}
	for k, slot := range slots {
		if v, ok := f[k]; ok {
			*slot = fact(t, v)
		}
	}
	for k, slot := range map[string]*verifier.Evidence{"attestations": &ctx.Attestations, "operator_attestations": &ctx.DeviceAttestations, "service_attestations": &ctx.ServiceAttestations, "seen": &ctx.IDJournal} {
		values := map[string]any{}
		for id, v := range object(f[k]) {
			values[id] = decode(t, v)
		}
		if f[k] != nil {
			*slot = fact(t, values)
		}
	}
	if row := object(f["delivery"]); row != nil {
		m := map[string]any{}
		for k, v := range row {
			m[k] = v
		}
		if v, ok := m["text"]; ok {
			m["text"] = decode(t, v)
		}
		ctx.Delivery = fact(t, m)
	}
	ctx.SignedSnapshots = fact(t, snapshots(t, f))
	if v, ok := f["registry_currency_version"].(int64); ok {
		ctx.RegistryCurrencyVersion = int(v)
	}
	if v := object(f["snapshot_journal"]); v != nil {
		journal := map[string]any{"anchor": v["anchor"]}
		entries := make([]any, 0)
		for _, r := range v["entries"].([]any) {
			entries = append(entries, decode(t, r))
		}
		journal["entries"] = entries
		ctx.SnapshotJournal = fact(t, journal)
	}

	chain := [][]byte{}
	for _, v := range m["chain"].([]any) {
		chain = append(chain, decode(t, v))
	}
	op := object(object(in["consume"])["input"])
	return fixture{proof: c.Proof{Context: ctx, Clock: clockAt(t, text(f["now"]), text(f["watermark"])), Chain: chain, Checkpoint: decode(t, m["checkpoint"]), IDs: stringsOf(m["ids"]), Keys: stringsOf(m["keys"])}, facts: f, input: in, root: text(op["root"]), key: text(op["key"]), reference: text(op["reference"]), reanchor: mint["mode"] == "freshness-reanchor"}
}
func stringsOf(v any) []string {
	var s []string
	for _, x := range v.([]any) {
		s = append(s, text(x))
	}
	return s
}
func snapshots(t *testing.T, f map[string]any) map[string]any {
	t.Helper()
	s := map[string]any{}
	for root, v := range object(f["revocations"]) {
		row := map[string]any{}
		for k, x := range object(v) {
			row[k] = x
		}
		row["snapshot"] = decode(t, row["snapshot"])
		s[root] = row
	}
	return s
}

// Synthetic intervals exercise projection, not provider authentication or durability.
func clockAt(t *testing.T, when, prior string) c.Clock {
	t.Helper()
	at, e := time.Parse(time.RFC3339, when)
	if e != nil {
		t.Fatal(e)
	}
	ns := func(x time.Time) string { return strconv.FormatInt(x.UnixNano(), 10) }
	floor := ns(at.Add(-time.Second))
	n := ns(at)
	return c.Clock{Interval: qtime.Interval{Observed: n, Lower: n, Upper: n, AuthorityUntil: ns(at.Add(300 * time.Second)), PriorHighWater: floor, HighWater: n, JournalFloor: floor, Boot: "boot-a", SampleID: "sample-" + n}, PriorWatermark: prior, DecisionFloor: floor}
}
func admit(t *testing.T, f fixture) (*c.Admission, verifier.Result) {
	t.Helper()
	if f.reanchor {
		return c.VerifyFreshnessReanchor(f.proof)
	}
	return c.VerifyFreshness(f.proof)
}
func acceptedAdmission(t *testing.T, f fixture) *c.Admission {
	t.Helper()
	a, r := admit(t, f)
	if a == nil || r.Code != "accept" || r.FreshnessReceipt == nil {
		t.Fatalf("F control: %s", r.Code)
	}
	return a
}
func acknowledged(a *c.Admission) *c.PersistenceAcknowledgement {
	p := a.Plan()
	q := a.Projection()
	return &c.PersistenceAcknowledgement{Anchor: a.Anchor(), Watermark: p.Watermark, Reanchor: p.Reanchor, DecisionFloor: q.DecisionFloor, Boot: q.Boot, SnapshotJournal: p.SnapshotJournal, IDs: p.IDs}
}
func use(f fixture, a *c.Admission) c.Use {
	var ack *c.PersistenceAcknowledgement
	if a != nil {
		ack = acknowledged(a)
	}
	ctx := f.proof.Context
	ctx.RequireFreshnessReceipt = true
	return c.Use{Context: ctx, Clock: f.proof.Clock, Admission: a, Persisted: ack}
}
func consumer(t *testing.T, in c.Use) *c.Consumer {
	t.Helper()
	v, e := c.New(in)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func attestationCode(t *testing.T, f fixture, in c.Use) string {
	t.Helper()
	v, e := c.New(in)
	if e != nil {
		return code(t, e)
	}
	a, e := v.VerifyAttestation(f.key, f.reference, false)
	if e != nil && a != (verifier.Attestation{}) {
		t.Fatal("identity on refusal")
	}
	if e == nil && (a.Key != f.key || a.ID != f.reference) {
		t.Fatal("accepted identity binding")
	}
	return code(t, e)
}
func code(t *testing.T, e error) string {
	t.Helper()
	if e == nil {
		return "accept"
	}
	var r *record.Error
	if !errors.As(e, &r) || r == nil || e.Error() != r.Code {
		t.Fatalf("untyped refusal: %v", e)
	}
	return r.Code
}
func wantCode(t *testing.T, f fixture, in c.Use, want string) {
	t.Helper()
	if got := attestationCode(t, f, in); got != want {
		t.Fatalf("want %s got %s", want, got)
	}
}
