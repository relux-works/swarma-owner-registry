// Package contracts freezes the reviewed S4 service surface, not runtime admission.
package contracts

const Revision = "f76a400ebeb599b95a9bf9b2c59b522bef0392d8"

// RecordTypes returns a copy of the service's closed record types. Message/act
// intake and registry-internal freshness publication are outside this list.
func RecordTypes() []string {
	return []string{"attestation/1", "grant/1", "revocation/1", "enrollment/1", "journal-entry/1", "checkpoint/1"}
}

func KeyClasses() []string { return []string{"agent", "operator-device", "service"} }

func HasRecordType(kind string) bool {
	for _, candidate := range RecordTypes() {
		if kind == candidate {
			return true
		}
	}
	return false
}
func HasKeyClass(class string) bool {
	for _, candidate := range KeyClasses() {
		if class == candidate {
			return true
		}
	}
	return false
}

// Profile describes D33 identity constraints; it grants no parent or effect authority.
type Profile struct {
	KeyClass, Purpose  string
	Roles, Actions     []string
	MaxLifetimeSeconds int64
}

func GrantServiceProfile() Profile {
	return Profile{"service", "grant-service", []string{"author"}, []string{"grant.sign"}, 86400}
}
