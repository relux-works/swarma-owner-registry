package contracts_test

import (
	"bytes"
	"encoding/json"
	"io"
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
	want := map[string]string{"github.com/relux-works/swarma-envelope": "v0.0.0-20261026200550-5636986d0de5", "github.com/relux-works/swarma-registrar": "v0.0.0-20261025040811-264660e5dabb"}
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

// The dependency gate rejects old identities, changed versions, extra/missing
// requirements and replacements beside the actual selected positive control.
func TestDependencyPolicyRejectsChanges(t *testing.T) {
	if !allowed(currentPolicy(t)) {
		t.Fatal("positive control invalid")
	}
	for _, mutate := range []func(*modulePolicy){
		func(p *modulePolicy) { p.Require = append(p.Require, requirement{"example.org/extra", "v1.0.0"}) },
		func(p *modulePolicy) { p.Require[0].Version = "v1.0.0" },
		func(p *modulePolicy) { p.Require = p.Require[:1] },
		func(p *modulePolicy) { p.Replace = []any{true} },
		func(p *modulePolicy) {
			for i := range p.Require {
				if p.Require[i].Path == "github.com/relux-works/swarma-envelope" {
					p.Require[i] = requirement{"github.com/relux-works/waggle", "v0.0.0-20261026034605-b63d627b19c4"}
				}
			}
		},
		func(p *modulePolicy) {
			for i := range p.Require {
				if p.Require[i].Path == "github.com/relux-works/swarma-envelope" {
					p.Require[i].Version = "v0.0.0-20261026034605-b63d627b19c4"
				}
			}
		},
	} {
		p := currentPolicy(t)
		mutate(&p)
		if allowed(p) {
			t.Fatal("policy change accepted")
		}
	}
}

// The actual selected graph contains only the main module and both exact public
// dependencies: an old transitive identity or any replacement is also forbidden.
func TestPublishedModuleGraph(t *testing.T) {
	cmd := exec.Command("go", "list", "-m", "-json", "all")
	cmd.Dir = "../.."
	raw, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"github.com/relux-works/swarma-owner-registry": "",
		"github.com/relux-works/swarma-envelope":       "v0.0.0-20261026200550-5636986d0de5",
		"github.com/relux-works/swarma-registrar":      "v0.0.0-20261025040811-264660e5dabb",
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	for {
		var m struct {
			Path, Version string
			Replace       any
		}
		err = d.Decode(&m)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		v, ok := want[m.Path]
		if !ok || v != m.Version || m.Replace != nil {
			t.Fatalf("unapproved selected module: %s@%s", m.Path, m.Version)
		}
		delete(want, m.Path)
	}
	if len(want) != 0 {
		t.Fatal("selected module missing", want)
	}
}

// Returned registries are copies; service-supported types and D33 profile remain closed.
func TestClosedRegistries(t *testing.T) {
	if contracts.Revision != "5636986d0de5d00c8c744ca2105648d60a48b814" {
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
