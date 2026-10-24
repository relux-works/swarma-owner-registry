// Package pins parses caller-protected configuration. No request, environment,
// repository metadata or signed record may supply this trust policy.
package pins

import (
	"strings"
	"unicode/utf8"

	"github.com/relux-works/swarma-owner-registry/internal/contracts"
	"github.com/relux-works/waggle/record"
	"github.com/relux-works/waggle/verifier"
)

const MaxConfigBytes = 16 * 1024

type Code string

const (
	ConfigMalformed     Code = "config_malformed"
	ConfigTooLarge      Code = "config_too_large"
	ConfigUnavailable   Code = "config_unavailable"
	RevisionUnsupported Code = "revision_unsupported"
	ProfileUnsupported  Code = "profile_unsupported"
	KeyClassUnsupported Code = "key_class_unsupported"
	RoleMissing         Code = "role_missing"
	KeyInvalid          Code = "key_invalid"
	CrossRoleKey        Code = "cross_role_key"
	RoleScope           Code = "role_scope_invalid"
	Purpose5Scope       Code = "purpose5_scope_invalid"
	GateClosed          Code = "gate_closed"
	GateUnknown         Code = "gate_unknown"
)

// Error contains only a stable local code, never caller bytes or nested errors.
type Error struct{ Code Code }

func (e *Error) Error() string { return string(e.Code) }
func refused(code Code) error  { return &Error{Code: code} }

type Role string

const (
	RegistryOwner       Role = "registry_owner"
	CommitSigner        Role = "commit_signer"
	EnrollmentPublisher Role = "enrollment_publisher"
	Purpose5Root        Role = "purpose5_root"
	OperatorRoot        Role = "operator_root"
	GrantRoot           Role = "grant_root"
	GrantService        Role = "grant_service"
)

func Roles() []Role {
	return []Role{RegistryOwner, CommitSigner, EnrollmentPublisher, Purpose5Root, OperatorRoot, GrantRoot, GrantService}
}

type Gate string

const (
	Freshness         Gate = "s4_freshness"
	Purpose5Runtime   Gate = "purpose5_runtime"
	Custody           Gate = "custody"
	Transport         Gate = "transport"
	Time              Gate = "time"
	Witness           Gate = "witness"
	Storage           Gate = "storage"
	FreshnessHistory  Gate = "freshness_history"
	ConsumerMigration Gate = "consumer_migration"
)

func Gates() []Gate {
	return []Gate{Freshness, Purpose5Runtime, Custody, Transport, Time, Witness, Storage, FreshnessHistory, ConsumerMigration}
}

// Pin uses local policy labels; purpose-5 becomes integer purpose 5 in the
// verifier context. Scope is an exact root id or the exact protected owner.
type Pin struct{ Key, Purpose, Scope string }

// Config is immutable after Parse. Its zero value and nil pointer are closed.
type Config struct {
	valid bool
	owner string
	roles map[Role]Pin
	gates map[Gate]bool
}

// Parse accepts bounded duplicate-free CCJ-1 through the reviewed record reader.
// Noncanonical whitespace is allowed; shapes and all members are closed.
// A refusal returns nil and never changes input bytes or external state.
func Parse(raw []byte) (*Config, error) {
	if len(raw) > MaxConfigBytes {
		return nil, refused(ConfigTooLarge)
	}
	value, err := record.Parse(raw, MaxConfigBytes, 8)
	if err != nil {
		return nil, refused(ConfigMalformed)
	}
	m, ok := value.(map[string]any)
	if !ok || !shape(m, "revision owner roles record_types key_classes service_profile", "activation") {
		return nil, refused(ConfigMalformed)
	}
	if m["revision"] != contracts.Revision {
		return nil, refused(RevisionUnsupported)
	}
	owner, ok := m["owner"].(string)
	if !ok || owner == "" || len(owner) > 255 || !utf8.ValidString(owner) {
		return nil, refused(ConfigMalformed)
	}
	if err = closedSet(m["record_types"], contracts.RecordTypes(), ProfileUnsupported); err != nil {
		return nil, err
	}
	if err = closedSet(m["key_classes"], contracts.KeyClasses(), KeyClassUnsupported); err != nil {
		return nil, err
	}
	if !serviceProfile(m["service_profile"]) {
		return nil, refused(ProfileUnsupported)
	}
	roleMap, ok := m["roles"].(map[string]any)
	if !ok {
		return nil, refused(ConfigMalformed)
	}
	if len(roleMap) > len(Roles()) {
		return nil, refused(ConfigMalformed)
	}
	for name := range roleMap {
		if !knownRole(Role(name)) {
			return nil, refused(ConfigMalformed)
		}
	}
	c := &Config{owner: owner, roles: make(map[Role]Pin), gates: make(map[Gate]bool)}
	seen := map[string]bool{}
	for _, r := range Roles() {
		value, exists := roleMap[string(r)]
		if !exists {
			return nil, refused(RoleMissing)
		}
		row, ok := value.(map[string]any)
		if !ok || !shape(row, "key purpose scope", "") {
			return nil, refused(ConfigMalformed)
		}
		key, k := row["key"].(string)
		purpose, p := row["purpose"].(string)
		scope, s := row["scope"].(string)
		if !k || !p || !s {
			return nil, refused(ConfigMalformed)
		}
		if record.ValidateKey(key) != nil {
			return nil, refused(KeyInvalid)
		}
		if (r == Purpose5Root || r == EnrollmentPublisher || r == GrantService) && !strings.HasPrefix(key, "ed25519:") {
			return nil, refused(KeyInvalid)
		}
		if seen[key] {
			return nil, refused(CrossRoleKey)
		}
		seen[key] = true
		c.roles[r] = Pin{key, purpose, scope}
	}
	p5 := c.roles[Purpose5Root]
	if p5.Purpose != "purpose-5" || p5.Scope != "purpose-5" || c.roles[RegistryOwner].Scope != p5.Key {
		return nil, refused(Purpose5Scope)
	}
	purposes := map[Role]string{RegistryOwner: "registry-revocation", CommitSigner: "git-commit", EnrollmentPublisher: "enrollment/1", OperatorRoot: "operator", GrantRoot: "grant", GrantService: "grant-service"}
	for r, purpose := range purposes {
		pin := c.roles[r]
		if pin.Purpose != purpose || (r != RegistryOwner && pin.Scope != owner) {
			return nil, refused(RoleScope)
		}
	}
	if value, exists := m["activation"]; exists {
		activation, ok := value.(map[string]any)
		if !ok {
			return nil, refused(ConfigMalformed)
		}
		for name, value := range activation {
			gate := Gate(name)
			open, ok := value.(bool)
			if !knownGate(gate) || !ok {
				return nil, refused(ConfigMalformed)
			}
			c.gates[gate] = open
		}
	}
	c.valid = true
	return c, nil
}
func shape(m map[string]any, required, optional string) bool {
	allowed := map[string]bool{}
	for _, k := range strings.Fields(required) {
		if _, ok := m[k]; !ok {
			return false
		}
		allowed[k] = true
	}
	for _, k := range strings.Fields(optional) {
		allowed[k] = true
	}
	for k := range m {
		if !allowed[k] {
			return false
		}
	}
	return true
}
func closedSet(value any, expected []string, unsupported Code) error {
	items, ok := value.([]any)
	if !ok {
		return refused(ConfigMalformed)
	}
	allowed := map[string]bool{}
	for _, s := range expected {
		allowed[s] = true
	}
	seen := map[string]bool{}
	for _, item := range items {
		s, ok := item.(string)
		if !ok {
			return refused(ConfigMalformed)
		}
		if !allowed[s] {
			return refused(unsupported)
		}
		if seen[s] {
			return refused(ConfigMalformed)
		}
		seen[s] = true
	}
	if len(seen) != len(allowed) {
		return refused(ConfigMalformed)
	}
	return nil
}
func serviceProfile(value any) bool {
	m, ok := value.(map[string]any)
	if !ok || !shape(m, "key_class purpose roles actions max_lifetime_seconds", "") {
		return false
	}
	p := contracts.GrantServiceProfile()
	return m["key_class"] == p.KeyClass && m["purpose"] == p.Purpose &&
		closedSet(m["roles"], p.Roles, ProfileUnsupported) == nil && closedSet(m["actions"], p.Actions, ProfileUnsupported) == nil &&
		m["max_lifetime_seconds"] == p.MaxLifetimeSeconds
}
func knownRole(r Role) bool {
	for _, v := range Roles() {
		if r == v {
			return true
		}
	}
	return false
}
func knownGate(g Gate) bool {
	for _, v := range Gates() {
		if g == v {
			return true
		}
	}
	return false
}

func (c *Config) Pin(role Role) (Pin, bool) {
	if c == nil || !c.valid {
		return Pin{}, false
	}
	p, ok := c.roles[role]
	return p, ok
}
func (c *Config) IsOpen(gate Gate) bool {
	return c != nil && c.valid && knownGate(gate) && c.gates[gate]
}
func (c *Config) Require(gate Gate) error {
	if c == nil || !c.valid {
		return refused(ConfigUnavailable)
	}
	if !knownGate(gate) {
		return refused(GateUnknown)
	}
	if !c.IsOpen(gate) {
		return refused(GateClosed)
	}
	return nil
}

// CheckProfile checks registry membership only, not a record's identity,
// signature, allowed type/class combination or activation dependencies.
func (c *Config) CheckProfile(kind, class string) error {
	if c == nil || !c.valid {
		return refused(ConfigUnavailable)
	}
	if !contracts.HasRecordType(kind) {
		return refused(ProfileUnsupported)
	}
	if !contracts.HasKeyClass(class) {
		return refused(KeyClassUnsupported)
	}
	return nil
}
func (c *Config) CheckPurpose5(key string, purpose int64) error {
	if c == nil || !c.valid {
		return refused(ConfigUnavailable)
	}
	if purpose != 5 || key != c.roles[Purpose5Root].Key {
		return refused(Purpose5Scope)
	}
	return nil
}

// VerifierContext copies static pins only. All signed/current/time/witness facts
// stay unavailable. Constructing this context neither opens gates nor admits a
// record; consumers must check their dependencies before calling a verifier.
func (c *Config) VerifierContext() (verifier.Context, error) {
	if c == nil || !c.valid {
		return verifier.Context{}, refused(ConfigUnavailable)
	}
	p5 := c.roles[Purpose5Root].Key
	registry := c.roles[RegistryOwner].Key
	grant := c.roles[GrantRoot].Key
	values := []any{
		map[string]any{"key": p5, "purpose": int64(5)},
		map[string]any{"key": registry, "purpose": "registry-revocation", "attestation_root": p5},
		c.roles[CommitSigner].Key, c.roles[EnrollmentPublisher].Key, grant, c.roles[OperatorRoot].Key, c.owner,
		map[string]any{"key": grant, "purpose": "grant-service", "owner": c.owner},
	}
	var out verifier.Context
	fields := []*verifier.Evidence{&out.ServiceRoot, &out.AgentRevocationRoot, &out.CommitSigningKey, &out.EnrollmentPublisher, &out.GrantRoot, &out.OperatorRoot, &out.RegistryOwner, &out.ServiceAuthority}
	for i, value := range values {
		e, err := verifier.Present(value)
		if err != nil {
			return verifier.Context{}, refused(ConfigUnavailable)
		}
		*fields[i] = e
	}
	out.OperatorOwner = out.RegistryOwner
	return out, nil
}
