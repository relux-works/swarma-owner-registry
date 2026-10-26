package pins_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/relux-works/swarma-owner-registry/internal/contracts"
	"github.com/relux-works/swarma-owner-registry/internal/pins"
	"github.com/relux-works/swarma-registrar/ccj"
	"github.com/relux-works/waggle/verifier"
)

func testKey(n byte) string {
	// RFC 8032 section 7.1 public test seed, domain-separated for distinct test roles.
	seed, _ := hex.DecodeString("9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60")
	digest := sha256.Sum256(append(seed, n))
	pub := ed25519.NewKeyFromSeed(digest[:]).Public().(ed25519.PublicKey)
	return "ed25519:" + base64.RawURLEncoding.EncodeToString(pub)
}

var requiredRoles = []pins.Role{pins.RegistryOwner, pins.CommitSigner, pins.EnrollmentPublisher, pins.Purpose5Root, pins.OperatorRoot, pins.GrantRoot, pins.GrantService}

func validDocument() map[string]any {
	roles := map[string]any{}
	purposes := []string{"registry-revocation", "git-commit", "enrollment/1", "purpose-5", "operator", "grant", "grant-service"}
	for i, role := range requiredRoles {
		scope := "owner-a"
		if role == pins.Purpose5Root {
			scope = "purpose-5"
		}
		if role == pins.RegistryOwner {
			scope = testKey(4)
		}
		roles[string(role)] = map[string]any{"key": testKey(byte(i + 1)), "purpose": purposes[i], "scope": scope}
	}
	return map[string]any{
		"revision": "b63d627b19c4178d34b7cff58f8f6068efeca1a0", "owner": "owner-a", "object_format": "sha1", "roles": roles,
		"record_types":    []any{"attestation/1", "grant/1", "revocation/1", "enrollment/1", "journal-entry/1", "checkpoint/1"},
		"key_classes":     []any{"agent", "operator-device", "service"},
		"service_profile": map[string]any{"key_class": "service", "purpose": "grant-service", "roles": []any{"author"}, "actions": []any{"grant.sign"}, "max_lifetime_seconds": int64(86400)},
	}
}
func wire(t *testing.T, d map[string]any) []byte {
	t.Helper()
	b, err := ccj.Marshal(d, ccj.RegistrationMalformed)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func role(d map[string]any, r pins.Role) map[string]any {
	return d["roles"].(map[string]any)[string(r)].(map[string]any)
}
func accepted(t *testing.T, d map[string]any) *pins.Config {
	t.Helper()
	c, err := pins.Parse(wire(t, d))
	if err != nil || c == nil {
		t.Fatalf("valid configuration refused: %v", err)
	}
	return c
}
func refused(t *testing.T, raw []byte, code pins.Code) {
	t.Helper()
	before := append([]byte(nil), raw...)
	c, err := pins.Parse(raw)
	var e *pins.Error
	if c != nil || !errors.As(err, &e) || e.Code != code {
		t.Fatalf("want nil configuration/%s, got config=%v error=%v", code, c != nil, err)
	}
	if err.Error() != string(code) {
		t.Fatal("error contains more than its non-secret code")
	}
	if !bytes.Equal(before, raw) {
		t.Fatal("parser changed caller bytes")
	}
}
func codeIs(t *testing.T, err error, code pins.Code) {
	t.Helper()
	var e *pins.Error
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}

// Unknown records and altered D33 profiles cannot widen the accepted revision, even with an open freshness gate.
func TestUnpublishedProfileRefused(t *testing.T) {
	accepted(t, validDocument())
	for _, kind := range []string{"freshness/1", "contact", "message/1", "operator-act/1"} {
		t.Run(kind, func(t *testing.T) {
			d := validDocument()
			d["record_types"] = append(d["record_types"].([]any), kind)
			refused(t, wire(t, d), pins.ProfileUnsupported)
		})
	}
	d := validDocument()
	d["service_profile"].(map[string]any)["actions"] = []any{"message.sign"}
	refused(t, wire(t, d), pins.ProfileUnsupported)
	d = validDocument()
	d["activation"] = map[string]any{string(pins.Freshness): true}
	c := accepted(t, d)
	codeIs(t, c.CheckProfile("freshness/1", "agent"), pins.ProfileUnsupported)
}

// All 21 protected role pairs must reject one shared public key, before returning any configuration.
func TestCrossRoleRootRefused(t *testing.T) {
	accepted(t, validDocument())
	roles := requiredRoles
	for i, a := range roles {
		for _, b := range roles[i+1:] {
			t.Run(string(a)+"/"+string(b), func(t *testing.T) {
				d := validDocument()
				role(d, b)["key"] = role(d, a)["key"]
				refused(t, wire(t, d), pins.CrossRoleKey)
			})
		}
	}
}

// Registry scope, purpose-5 domain and requested root must match the protected pin exactly.
func TestForeignPurpose5DomainRefused(t *testing.T) {
	c := accepted(t, validDocument())
	p, _ := c.Pin(pins.Purpose5Root)
	if err := c.CheckPurpose5(p.Key, 5); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(map[string]any){
		func(d map[string]any) { role(d, pins.RegistryOwner)["scope"] = testKey(40) },
		func(d map[string]any) { role(d, pins.Purpose5Root)["key"] = testKey(40) },
		func(d map[string]any) { role(d, pins.Purpose5Root)["scope"] = "purpose-4" },
		func(d map[string]any) { role(d, pins.Purpose5Root)["purpose"] = "purpose-4" },
	} {
		d := validDocument()
		change(d)
		refused(t, wire(t, d), pins.Purpose5Scope)
	}
	codeIs(t, c.CheckPurpose5(testKey(40), 5), pins.Purpose5Scope)
	codeIs(t, c.CheckPurpose5(p.Key, 4), pins.Purpose5Scope)
}

// Unknown classes and revisions are rejected beside a valid pinned/class control.
func TestUnknownClassAndRevisionRefused(t *testing.T) {
	c := accepted(t, validDocument())
	if err := c.CheckProfile("attestation/1", "service"); err != nil {
		t.Fatal(err)
	}
	d := validDocument()
	d["key_classes"] = append(d["key_classes"].([]any), "robot")
	refused(t, wire(t, d), pins.KeyClassUnsupported)
	codeIs(t, c.CheckProfile("attestation/1", "robot"), pins.KeyClassUnsupported)
	for _, revision := range []any{"", strings.Repeat("0", 40), int64(3)} {
		d = validDocument()
		d["revision"] = revision
		refused(t, wire(t, d), pins.RevisionUnsupported)
	}
}

// Every required role must exist with a valid canonical public key and its fixed purpose/scope.
func TestMissingOrInvalidRoleRefused(t *testing.T) {
	accepted(t, validDocument())
	for _, r := range requiredRoles {
		t.Run(string(r), func(t *testing.T) {
			d := validDocument()
			delete(d["roles"].(map[string]any), string(r))
			refused(t, wire(t, d), pins.RoleMissing)
			d = validDocument()
			role(d, r)["key"] = "untrusted-input"
			refused(t, wire(t, d), pins.KeyInvalid)
			d = validDocument()
			role(d, r)["extra"] = "untrusted-input"
			refused(t, wire(t, d), pins.ConfigMalformed)
		})
	}
	d := validDocument()
	role(d, pins.GrantService)["purpose"] = "message"
	refused(t, wire(t, d), pins.RoleScope)
	d = validDocument()
	role(d, pins.GrantRoot)["scope"] = "owner-b"
	refused(t, wire(t, d), pins.RoleScope)
	d = validDocument()
	d["owner"] = ""
	refused(t, wire(t, d), pins.ConfigMalformed)
}

// Unknown/duplicate members, malformed shapes, oversized bytes and deep input yield typed refusals without partial state.
func TestStrictBoundedCCJRefused(t *testing.T) {
	good := wire(t, validDocument())
	if _, err := pins.Parse(append([]byte(" \n"), good...)); err != nil {
		t.Fatal(err)
	}
	for _, raw := range [][]byte{
		append(append([]byte{}, good[:len(good)-1]...), []byte(`,"owner":"owner-a"}`)...),
		append(append([]byte{}, good[:len(good)-1]...), []byte(`,"\u006fwner":"owner-a"}`)...),
		[]byte(`{"owner":1.0}`), []byte(`null`), []byte(`[]`), append(good, []byte(` {}`)...),
		[]byte(strings.Repeat("[", 17) + strings.Repeat("]", 17)),
	} {
		refused(t, raw, pins.ConfigMalformed)
	}
	d := validDocument()
	d["unexpected"] = "untrusted-input"
	refused(t, wire(t, d), pins.ConfigMalformed)
	d = validDocument()
	delete(d, "owner")
	refused(t, wire(t, d), pins.ConfigMalformed)
	d = validDocument()
	d["record_types"] = append(d["record_types"].([]any), "grant/1")
	refused(t, wire(t, d), pins.ConfigMalformed)
	d = validDocument()
	d["record_types"] = []any{"grant/1"}
	refused(t, wire(t, d), pins.ConfigMalformed)
	padded := append(append([]byte{}, good...), bytes.Repeat([]byte(" "), pins.MaxConfigBytes-len(good))...)
	if _, err := pins.Parse(padded); err != nil {
		t.Fatalf("at byte limit: %v", err)
	}
	refused(t, append(padded, ' '), pins.ConfigTooLarge)
}

// Missing gates and explicit false never authorize use; only a protected true opens that one gate.
func TestClosedGateRefused(t *testing.T) {
	c := accepted(t, validDocument())
	for _, g := range pins.Gates() {
		if c.IsOpen(g) {
			t.Fatal(g)
		}
		codeIs(t, c.Require(g), pins.GateClosed)
	}
	d := validDocument()
	d["activation"] = map[string]any{string(pins.Custody): true, string(pins.Purpose5Runtime): false}
	c = accepted(t, d)
	if !c.IsOpen(pins.Custody) || c.Require(pins.Custody) != nil {
		t.Fatal("explicit true closed")
	}
	codeIs(t, c.Require(pins.Purpose5Runtime), pins.GateClosed)
	codeIs(t, c.Require(pins.Gate("unknown")), pins.GateUnknown)
	for _, value := range []any{nil, "true", int64(1), []any{}} {
		d = validDocument()
		d["activation"] = map[string]any{string(pins.Custody): value}
		refused(t, wire(t, d), pins.ConfigMalformed)
	}
	d = validDocument()
	d["activation"] = map[string]any{"unknown": true}
	refused(t, wire(t, d), pins.ConfigMalformed)
	d = validDocument()
	d["activation"] = nil
	refused(t, wire(t, d), pins.ConfigMalformed)
}

// The original S3 service surface stays accepted under the current pin, with every omitted gate closed.
func TestPinnedS3ProfileAccepted(t *testing.T) {
	if !reflect.DeepEqual(pins.Roles(), requiredRoles) {
		t.Fatal("required role registry changed")
	}
	c := accepted(t, validDocument())
	for _, kind := range contracts.RecordTypes() {
		for _, class := range contracts.KeyClasses() {
			if err := c.CheckProfile(kind, class); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, r := range requiredRoles {
		if p, ok := c.Pin(r); !ok || p.Key == "" {
			t.Fatal(r)
		}
	}
	for _, g := range pins.Gates() {
		if c.IsOpen(g) {
			t.Fatal("gate defaulted open")
		}
	}
}

// Returned values and verifier pin snapshots cannot mutate the configuration; no current or signed evidence is manufactured.
func TestImmutableVerifierPins(t *testing.T) {
	d := validDocument()
	raw := wire(t, d)
	c, err := pins.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	clear(raw)
	p, _ := c.Pin(pins.RegistryOwner)
	p.Key = testKey(50)
	original, _ := c.Pin(pins.RegistryOwner)
	if original.Key == p.Key {
		t.Fatal("mutable pin")
	}
	a, err := c.VerifierContext()
	if err != nil {
		t.Fatal(err)
	}
	if !a.RegistryOwner.IsAvailable() || !a.ServiceAuthority.IsAvailable() || a.SignedSnapshots.IsAvailable() || a.Time.IsAvailable() || a.ServiceAttestations.IsAvailable() {
		t.Fatal("wrong evidence availability")
	}
	present := func(value any) verifier.Evidence {
		t.Helper()
		e, err := verifier.Present(value)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	want := verifier.Context{
		ServiceRoot:         present(map[string]any{"key": testKey(4), "purpose": int64(5)}),
		AgentRevocationRoot: present(map[string]any{"key": testKey(1), "purpose": "registry-revocation", "attestation_root": testKey(4)}),
		CommitSigningKey:    present(testKey(2)), EnrollmentPublisher: present(testKey(3)),
		GrantRoot: present(testKey(6)), OperatorRoot: present(testKey(5)),
		RegistryOwner: present("owner-a"), OperatorOwner: present("owner-a"),
		ServiceAuthority: present(map[string]any{"key": testKey(6), "purpose": "grant-service", "owner": "owner-a"}),
	}
	if !reflect.DeepEqual(a, want) {
		t.Fatal("static verifier facts differ from exact protected pins")
	}
	a.RegistryOwner = a.Time
	b, err := c.VerifierContext()
	if err != nil || !b.RegistryOwner.IsAvailable() {
		t.Fatal("context mutated config")
	}
	var zero pins.Config
	codeIs(t, zero.Require(pins.Custody), pins.ConfigUnavailable)
	codeIs(t, zero.CheckProfile("grant/1", "service"), pins.ConfigUnavailable)
	codeIs(t, zero.CheckPurpose5(testKey(4), 5), pins.ConfigUnavailable)
	if _, err := zero.VerifierContext(); err == nil {
		t.Fatal("zero context accepted")
	}
	if zero.IsOpen(pins.Custody) {
		t.Fatal("zero gate open")
	}
	if _, ok := zero.Pin(pins.RegistryOwner); ok {
		t.Fatal("zero pin available")
	}
	var absent *pins.Config
	codeIs(t, absent.Require(pins.Custody), pins.ConfigUnavailable)
	if absent.IsOpen(pins.Custody) {
		t.Fatal("nil gate open")
	}
	if reflect.DeepEqual(a, b) {
		t.Fatal("test did not change returned context")
	}
}

// D33's exact service language and lifetime ceiling cannot be expanded or replaced by a local profile.
func TestServiceProfileRefused(t *testing.T) {
	accepted(t, validDocument())
	for name, value := range map[string]any{
		"key_class": "agent", "purpose": "message", "roles": []any{"author", "approver"},
		"actions": []any{"grant.sign", "message.sign"}, "max_lifetime_seconds": int64(86401),
	} {
		t.Run(name, func(t *testing.T) {
			d := validDocument()
			d["service_profile"].(map[string]any)[name] = value
			refused(t, wire(t, d), pins.ProfileUnsupported)
		})
	}
}

// Noncanonical and identity public-key encodings, unknown roles and overlong owners never install partial pins.
func TestInvalidPolicyEncodingRefused(t *testing.T) {
	accepted(t, validDocument())
	for _, key := range []string{testKey(1) + "=", "ed25519:" + base64.RawURLEncoding.EncodeToString(make([]byte, 32)), "rsa:invalid"} {
		d := validDocument()
		role(d, pins.RegistryOwner)["key"] = key
		refused(t, wire(t, d), pins.KeyInvalid)
	}
	d := validDocument()
	d["roles"].(map[string]any)["unknown"] = role(d, pins.RegistryOwner)
	refused(t, wire(t, d), pins.ConfigMalformed)
	d = validDocument()
	d["owner"] = strings.Repeat("a", 256)
	refused(t, wire(t, d), pins.ConfigMalformed)
}

// A complete old S3 configuration is refused rather than running one revision while declaring another.
func TestS3PinCannotAdmitFreshness(t *testing.T) {
	d := validDocument()
	d["revision"] = "9b42338b8a57d12a21da4db4375b269deacdd6bf"
	refused(t, wire(t, d), pins.RevisionUnsupported)
	d["revision"] = "b63d627b19c4178d34b7cff58f8f6068efeca1a0"
	accepted(t, d)
}

// The original S4 profile test retains the same service surface at the current pin; explicit booleans open only the selected S4 dependency.
func TestPinnedS4ProfileAccepted(t *testing.T) {
	d := validDocument()
	d["revision"] = "b63d627b19c4178d34b7cff58f8f6068efeca1a0"
	c := accepted(t, d)
	for _, g := range pins.Gates() {
		if c.IsOpen(g) {
			t.Fatalf("default gate open: %s", g)
		}
		codeIs(t, c.Require(g), pins.GateClosed)
	}
	for _, selected := range []pins.Gate{pins.Freshness, pins.Gate("s4_verify_freshness"), pins.Gate("s4_reanchor")} {
		for _, enabled := range []bool{false, true} {
			d["activation"] = map[string]any{string(selected): enabled}
			c = accepted(t, d)
			for _, g := range pins.Gates() {
				want := g == selected && enabled
				if c.IsOpen(g) != want {
					t.Fatalf("gate %s widened by %s", g, selected)
				}
				if want {
					if err := c.Require(g); err != nil {
						t.Fatal(err)
					}
				} else {
					codeIs(t, c.Require(g), pins.GateClosed)
				}
			}
		}
		for _, invalid := range []any{"true", int64(1), nil} {
			d["activation"] = map[string]any{string(selected): invalid}
			refused(t, wire(t, d), pins.ConfigMalformed)
		}
	}
}
