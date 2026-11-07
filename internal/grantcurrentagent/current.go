// Package grantcurrentagent contains a bounded local CurrentAgent read core.
// It is not a protected provider: no source authenticator, authoritative D1
// transition reader, independent recovery floor or final-use exclusion is
// implemented. Reader has no admission constructor, and its zero value refuses.
// A local store read or a caller-supplied callback cannot install authority.
package grantcurrentagent

import (
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/relux-works/swarma-envelope/record"
)

// Agent preserves the existing consumer's LinuxRegistryAgent field shape.
// A later composition must copy these fields without changing the consumer port.
type Agent struct {
	Key, MXID                            string
	UID, RegistrationNumber              int64
	Generation, MasterEpoch, Owner, Path string
	Active                               bool
}

// ErrUnavailable deliberately discloses no identity, binding or source metadata.
// It is a local component error, not an added platform refusal code.
var ErrUnavailable = errors.New("current agent unavailable")

// Reader is a local projection only. There is deliberately no exported way to
// populate it before an owning-source connector and its admission contract exist.
// It opens no store, socket, file, clock authority or network connection.
type Reader struct {
	mu    sync.Mutex
	state *readState
}

// mapping comes from independently protected installation, never a response.
type mapping struct {
	key, mxid         string
	uid, registration int64
}

// position is an opaque, source-owned identity, not an ordered revision scheme.
// This core cannot prove ancestry or advance a floor. It only compares an exact
// already-admitted current position with its independently reconciled floor.
type position struct {
	lineage, head string
	binding       Agent
}

// readState is process-local staged state, never serializable evidence. A future
// connector must authenticate and authorize each read, reconcile complete source
// history with an independent floor, retain retirement, and exclude old instances
// before installation. No production code in this package installs readState.
type readState struct {
	installed      mapping
	current, floor position
	origin         *originAdmission
	disclosure     *readAdmission
	// deadline is an original finite local operation budget, not qualified time
	// or proof of source freshness. Reads never renew it.
	deadline time.Time
}

// These process-local handles express necessary binding and invalidation checks;
// their existence does not authenticate anything. Their only construction today
// is in synthetic tests. A real connector still needs source-owner qualification.
type originAdmission struct {
	reader         *Reader
	state          *readState
	current, floor position  // exact immutable premise, not just a pointer identity
	deadline       time.Time // original operation budget, never renewable by a read
}

type readAdmission struct {
	reader     *Reader
	state      *readState
	principal  string // transport/session-authenticated read principal
	key, owner string // authorized exact subject and disclosure domain
}

// CurrentAgent makes one nonblocking, allocation-bounded local read. Contention,
// absent admission, refenced handles, expired operation budgets and any identity
// conflict return the same error and a completely empty Agent. It performs no
// retries, remote work, persistence, reservations or effects.
//
// The mutex excludes only local state replacement while copying. It does not
// exclude an authoritative update after return or hold a consumer's final effect
// boundary. A caller must not treat this method as qualified final currentness.
func (r *Reader) CurrentAgent(key string) (Agent, error) {
	if r == nil || len(key) != 51 || !strings.HasPrefix(key, "ed25519:") || record.ValidateKey(key) != nil {
		return Agent{}, ErrUnavailable
	}
	if !r.mu.TryLock() {
		return Agent{}, ErrUnavailable
	}
	defer r.mu.Unlock()
	s := r.state
	if s == nil || s.origin == nil || s.disclosure == nil || s.deadline.IsZero() || !time.Now().Before(s.deadline) {
		return Agent{}, ErrUnavailable
	}
	if s.origin.reader != r || s.origin.state != s || s.origin.deadline != s.deadline || s.disclosure.reader != r || s.disclosure.state != s {
		return Agent{}, ErrUnavailable
	}
	a, pin, grant := s.current.binding, s.installed, s.disclosure
	// Bound all strings before any comparison, parse or disclosure. Limits are
	// local component ceilings; they are not admitted transport/proof budgets.
	if !bounded(pin.mxid) || !bounded(grant.principal) || !bounded(grant.owner) ||
		!bounded(s.current.lineage) || !bounded(s.current.head) ||
		!validAgent(a) || !validAgent(s.floor.binding) ||
		!bounded(s.floor.lineage) || !bounded(s.floor.head) {
		return Agent{}, ErrUnavailable
	}
	if pin.key != key || a.Key != key || grant.key != key || grant.owner != a.Owner ||
		a.MXID != pin.mxid || a.UID != pin.uid || a.RegistrationNumber != pin.registration ||
		s.origin.current != s.current || s.origin.floor != s.floor ||
		s.current != s.floor || !a.Active {
		return Agent{}, ErrUnavailable
	}
	// The budget is checked again at the last disclosure point. This wall/monotonic
	// check is solely operational; it supplies no clock/currentness qualification.
	if !time.Now().Before(s.deadline) {
		return Agent{}, ErrUnavailable
	}
	return a, nil
}

func bounded(s string) bool {
	return len(s) > 0 && len(s) <= 255 && utf8.ValidString(s)
}

func validAgent(a Agent) bool {
	return len(a.Key) == 51 && strings.HasPrefix(a.Key, "ed25519:") && record.ValidateKey(a.Key) == nil &&
		bounded(a.MXID) && a.UID >= 0 && a.UID <= 4294967294 &&
		a.RegistrationNumber >= 1 && a.RegistrationNumber <= 9007199254740991 &&
		bounded(a.Generation) && bounded(a.MasterEpoch) && bounded(a.Owner) && bounded(a.Path)
}
