package contracts_test

import (
	"encoding/json"
	"os/exec"
	"reflect"
	"testing"

	"github.com/relux-works/swarma-owner-registry/internal/contracts"
)

type requirement struct{ Path, Version string }
type modulePolicy struct {
	Module                 struct{ Path string }
	Go                     string
	Require                []requirement
	Replace, Exclude, Tool []any
}

func allowed(p modulePolicy) bool {
	if p.Module.Path != "github.com/relux-works/swarma-owner-registry" || p.Go != "1.26.0" || len(p.Require) != 2 || len(p.Replace)+len(p.Exclude)+len(p.Tool) != 0 {
		return false
	}
	want := map[string]string{"github.com/relux-works/waggle": "v0.0.0-20261025015329-5f4e7435d756", "github.com/relux-works/swarma-registrar": "v0.0.0-20261023164451-5f44ebf3f788"}
	for _, r := range p.Require {
		if want[r.Path] != r.Version {
			return false
		}
		delete(want, r.Path)
	}
	return len(want) == 0
}
func currentPolicy(t *testing.T) modulePolicy {
	t.Helper()
	cmd := exec.Command("go", "mod", "edit", "-json")
	cmd.Dir = "../.."
	b, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var p modulePolicy
	if err = json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

// The actual module must retain precisely the two approved versions and no replacements or extra requirements.
func TestDependencyPolicy(t *testing.T) {
	if !allowed(currentPolicy(t)) {
		t.Fatal("module requirements differ from reviewed pins")
	}
}

// The dependency gate detects an added requirement, changed version, missing requirement and replacement beside its valid control.
func TestDependencyPolicyRejectsChanges(t *testing.T) {
	if !allowed(currentPolicy(t)) {
		t.Fatal("positive control invalid")
	}
	for _, mutate := range []func(*modulePolicy){
		func(p *modulePolicy) { p.Require = append(p.Require, requirement{"example.org/extra", "v1.0.0"}) },
		func(p *modulePolicy) { p.Require[0].Version = "v1.0.0" },
		func(p *modulePolicy) { p.Require = p.Require[:1] },
		func(p *modulePolicy) { p.Replace = []any{true} },
	} {
		p := currentPolicy(t)
		mutate(&p)
		if allowed(p) {
			t.Fatal("policy change accepted")
		}
	}
}

// Returned registries are copies; service-supported types and D33 profile remain closed.
func TestClosedRegistries(t *testing.T) {
	if contracts.Revision != "5f4e7435d75697385ff6ce15a7501ac6f31c8ea5" {
		t.Fatal("reviewed revision changed")
	}
	if !reflect.DeepEqual(contracts.RecordTypes(), []string{"attestation/1", "grant/1", "revocation/1", "enrollment/1", "journal-entry/1", "checkpoint/1"}) {
		t.Fatal("record types changed")
	}
	if !reflect.DeepEqual(contracts.KeyClasses(), []string{"agent", "operator-device", "service"}) {
		t.Fatal("key classes changed")
	}

	types := contracts.RecordTypes()
	types[0] = "freshness/1"
	classes := contracts.KeyClasses()
	classes[0] = "robot"
	if contracts.HasRecordType("freshness/1") || contracts.HasKeyClass("robot") {
		t.Fatal("registry widened")
	}
	p := contracts.GrantServiceProfile()
	p.Roles[0] = "agent"
	p.Actions[0] = "message.sign"
	q := contracts.GrantServiceProfile()
	if q.KeyClass != "service" || q.Purpose != "grant-service" || q.Roles[0] != "author" || q.Actions[0] != "grant.sign" || q.MaxLifetimeSeconds != 86400 {
		t.Fatal("D33 profile changed")
	}
}
