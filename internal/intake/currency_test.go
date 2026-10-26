package intake_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/relux-works/swarma-owner-registry/internal/composition"
	"github.com/relux-works/swarma-owner-registry/internal/contracts"
	"github.com/relux-works/swarma-owner-registry/internal/intake"
	"github.com/relux-works/waggle/verifier"
)

func currencyRegistry(t *testing.T, name string) (registryFixture, verifier.Evidence) {
	t.Helper()
	folder := "1-positive"
	if name[0] == 'N' {
		folder = "0-negative"
	}
	raw, e := os.ReadFile(filepath.Join(fixtureDirectory(t), "vectors/swarm-envelope-2", folder, name+".json"))
	if e != nil {
		t.Fatal(e)
	}
	d := parse(t, raw)
	input := object(d["input"])
	facts := object(d["fixture"])
	mint := object(object(input["mint"])["input"])
	consume := object(input["consume"])
	consumerInput := object(consume["input"])
	kind := "grant/1"
	if consume["mode"] == "device" || consume["mode"] == "service" {
		collection := "operator_attestations"
		if consume["mode"] == "service" {
			collection = "service_attestations"
		}
		consumerInput = map[string]any{"chain": []any{object(facts[collection])[text(consumerInput["key"])]}}
		kind = "attestation/1"
	}
	d["input"] = consumerInput
	s := sampleFrom(t, d)
	s.kind = kind
	s.document["registry_currency_version"] = int64(2)
	rows := make([]any, 0)
	for _, kind := range contracts.RecordTypesForCurrency(2) {
		rows = append(rows, kind)
	}
	s.document["record_types"] = rows
	s.config = config(t, s.document)
	ctx, e := s.config.VerifierContext()
	if e != nil {
		t.Fatal(e)
	}
	fact := func(v any) verifier.Evidence {
		x, e := verifier.Present(v)
		if e != nil {
			t.Fatal(e)
		}
		return x
	}
	j := object(facts["snapshot_journal"])
	entries := make([]any, 0)
	for _, b := range j["entries"].([]any) {
		entries = append(entries, decode(t, b))
	}
	journal := fact(map[string]any{"anchor": j["anchor"], "entries": entries})
	seen := map[string]any{}
	for id, b := range s.facts.Retained {
		seen[id] = b
	}
	ctx.IDJournal = fact(seen)
	ctx.IDJournalAvailable = fact(true)
	ctx.FreshnessAnchor = fact(facts["freshness_anchor"])
	ctx.JournalWitness = fact(facts["journal_witness"])
	ctx.WitnessReadAt = fact(facts["witness_read_at"])
	ctx.WatermarkAvailable = fact(true)
	r, e := intake.NewCurrencyRegistry(s.config, s.facts.Snapshots, journal)
	if e != nil {
		t.Fatal(e)
	}
	current := cloneFacts(t, s.facts)
	current.Snapshots = nil
	clock := registryClock(t, text(facts["now"]), text(facts["watermark"]))
	var chain [][]byte
	for _, b := range mint["chain"].([]any) {
		chain = append(chain, decode(t, b))
	}
	return registryFixture{s: s, r: r, current: intake.RegistryCurrent{Facts: current, Clock: clock, JournalWitness: ctx.JournalWitness, WitnessReadAt: ctx.WitnessReadAt}, proof: composition.Proof{Context: ctx, Clock: clock, Chain: chain, Checkpoint: decode(t, mint["checkpoint"])}}, journal
}

// The retaining adapter uses an actual /2 F receipt and actual grant-chain
// verifier, refuses both missing authority and incomplete root coverage, and
// repeats admission before any effect. This supplies no native qualification.
func TestCurrencyRegistryGrantAndNoEffectsOnReplacement(t *testing.T) {
	f, journal := currencyRegistry(t, "P-CU-receipt-grant")
	registryWant(t, f, "freshness_receipt_unavailable")
	registryMint(t, &f)
	candidate := registryWant(t, f, "accept")
	effects := 0
	boundary := func(check func(intake.RegistryCurrent) error) error { return check(f.current) }
	effect := func(*intake.Accepted) error { effects++; return nil }
	// Even re-Present of identical journal bytes is a different immutable premise.
	_, other := currencyRegistry(t, "P-CU-receipt-grant")
	f.r.ReplaceSnapshotJournal(other)
	if e := f.r.Apply(candidate, boundary, effect); e == nil || e.Error() != "freshness_receipt_unavailable" || effects != 0 {
		t.Fatal(e, effects)
	}
	registryWant(t, f, "freshness_receipt_unavailable")
	f.r.ReplaceSnapshotJournal(journal)
	registryWant(t, f, "freshness_receipt_unavailable")
	registryMint(t, &f)
	candidate = registryWant(t, f, "accept")
	if e := f.r.Apply(candidate, boundary, effect); e != nil || effects != 1 {
		t.Fatal(e, effects)
	}
	missing, _ := currencyRegistry(t, "N-CU-coverage-grant-required")
	registryMint(t, &missing)
	registryWant(t, missing, "freshness_receipt_unavailable")
}

// Required external-root coverage also governs operator-device and grant-service
// identities. Each positive consumes a real S8 F receipt; omitted required root
// coverage exposes no Accepted identity. No transport or root install is used.
func TestCurrencyRegistryDeviceAndServiceCoverage(t *testing.T) {
	for _, name := range []string{"P-CU-receipt-device", "P-CU-receipt-service", "N-CU-coverage-device-required", "N-CU-coverage-service-required"} {
		t.Run(name, func(t *testing.T) {
			f, _ := currencyRegistry(t, name)
			registryMint(t, &f)
			want := "accept"
			if name[0] == 'N' {
				want = "freshness_receipt_unavailable"
			}
			registryWant(t, f, want)
		})
	}
}
