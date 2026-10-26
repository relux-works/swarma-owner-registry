// Package contracts freezes the reviewed S8 dependency and explicit publication profiles, not runtime admission.
package contracts

const Revision = "5636986d0de5d00c8c744ca2105648d60a48b814"

// CurrencyVersion is an explicit protected selection, never wire negotiation.
const CurrencyVersion = 2
const CurrencyCheckpoint = "checkpoint/2"
const CurrencyFreshness = "freshness/2"

// RecordTypes returns a copy of the service's closed record types. Message/act
// intake and registry-internal freshness publication are outside this list.
func RecordTypes() []string { return RecordTypesForCurrency(1) }

// RecordTypesForCurrency preserves /1 and selects /2 only for protected version2.
// Internal freshness publication is not a received intake profile.
func RecordTypesForCurrency(version int) []string {
	checkpoint := "checkpoint/1"
	if version == CurrencyVersion {
		checkpoint = CurrencyCheckpoint
	}
	return []string{"attestation/1", "grant/1", "revocation/1", "enrollment/1", "journal-entry/1", checkpoint}
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
