//go:build linux

package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func decode(raw []byte, v any) bool {
	return json.Unmarshal(raw, v) == nil && bytes.Equal(raw, canonical(v))
}
func fold(base, delta image) image {
	for p, id := range delta.Files {
		base.Files[p] = id
	}
	for k, ref := range delta.Requests {
		base.Requests[k] = ref
	}
	base.Sequence, base.Head, base.Watermark = delta.Sequence, delta.Head, delta.Watermark
	return base
}
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// delta retains only this request's payloads. Later deltas contain no old bytes.
func delta(base string, next image, r Request) (manifest, map[string][]byte) {
	files := map[string]string{}
	packed, blobs := packRequest(r)
	add := func(raw []byte) string { id := byteID(raw); blobs[id] = raw; return id }
	for _, v := range r.Records {
		files[recordPath(v)] = add(v.Bytes)
	}
	files[fmt.Sprintf("journal/%016d.json", next.Sequence)] = add(r.Journal)
	files[fmt.Sprintf("checkpoints/%016d.json", next.Sequence)] = add(r.Checkpoint)
	out := Outcome{Sequence: next.Sequence, Head: r.Head, Watermark: r.Watermark, Result: append([]byte{}, r.Result...)}
	add(out.Result)
	ref := retained{Request: add(canonical(packed)), Outcome: add(canonical(outcomeRef(out))), Sequence: next.Sequence}
	return manifest{Base: base, Sequence: next.Sequence, Head: next.Head, Watermark: next.Watermark, Files: files, Key: requestKey(r), Retained: ref}, blobs
}

// Capacity charges every existing regular byte (including retained fragments),
// all missing blobs/materialized files, the delta and the replacement CURRENT.
// Counting both CURRENT copies reserves the maximum staging overlap. No IO that
// can write, intent marker or mkdir occurs before this conservative check.
func (s *Store) capacity(m manifest, blobs map[string][]byte) error {
	used := s.disk.totalBytes
	add := uint64(len(canonical(m)) + 72)
	charge := func(n uint64) bool {
		if n > s.maxBytes || used > s.maxBytes-n {
			return false
		}
		used += n
		return true
	}
	if !charge(add) {
		return refuse(CapacityExhausted)
	}
	for id, b := range blobs {
		if !s.blobs[id] && !charge(uint64(len(b))) {
			return refuse(CapacityExhausted)
		}
	}
	for p, id := range m.Files {
		if _, ok := s.active.Files[p]; !ok && !charge(uint64(len(blobs[id]))) {
			return refuse(CapacityExhausted)
		}
	}
	return nil
}
func (s *Store) materialize(files map[string]string, stateID string) error {
	for _, p := range sortedKeys(files) {
		raw, e := s.disk.read("blobs/" + files[p] + ".blob")
		if e != nil || byteID(raw) != files[p] {
			return errors.New("missing exact blob")
		}
		if e = s.disk.ensureDir(filepath.Dir(p)); e != nil {
			return e
		}
		if e = s.disk.atomic(p, raw, stateID, false); e != nil {
			return e
		}
	}
	return nil
}
func allowedDirectory(p string) bool {
	for _, v := range layoutDirs() {
		if p == v+"/" {
			return true
		}
	}
	return false
}
func temporaryOwner(path string) (string, bool) {
	name := filepath.Base(path)
	if !strings.HasPrefix(name, ".tmp-") || len(name) < 71 || name[69] != '-' {
		return "", false
	}
	id := "sha256:" + name[5:69]
	if !validID(id) {
		return "", false
	}
	for _, c := range name[70:] {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9') {
			return "", false
		}
	}
	return id, true
}
func (s *Store) inventory(paths []string, files map[string]string, manifests, blobs map[string]bool, anchorID string) error {
	for _, p := range paths {
		if strings.HasSuffix(p, "/") {
			if !allowedDirectory(p) {
				return errors.New("unknown directory")
			}
			continue
		}
		if p == "anchor.json" || p == "CURRENT" {
			continue
		}
		if owner, ok := temporaryOwner(p); ok {
			if !manifests[owner] && owner != anchorID {
				return errors.New("incomplete candidate manifest")
			}
			if parent := filepath.Dir(p); parent != "." && !allowedDirectory(parent+"/") {
				return errors.New("unknown staging directory")
			}
			continue
		}
		if strings.HasPrefix(p, "transactions/") && strings.HasSuffix(p, ".json") {
			id := strings.TrimSuffix(strings.TrimPrefix(p, "transactions/"), ".json")
			if validID(id) && manifests[id] {
				continue
			}
		}
		if strings.HasPrefix(p, "blobs/") && strings.HasSuffix(p, ".blob") {
			id := strings.TrimSuffix(strings.TrimPrefix(p, "blobs/"), ".blob")
			if validID(id) && blobs[id] {
				continue
			}
		}
		if _, ok := files[p]; !ok {
			return errors.New("unreferenced local evidence")
		}
	}
	return nil
}
func (s *Store) audit() error {
	paths, e := s.disk.scan()
	if e != nil {
		return e
	}
	if e = s.inventory(paths, s.active.Files, s.manifests, s.blobs, digest(genesis())); e != nil {
		return e
	}
	expected := map[string]string{"anchor.json": byteID(canonical(genesis())), "CURRENT": byteID([]byte(s.stateID + "\n"))}
	for p, id := range s.active.Files {
		expected[p] = id
	}
	for id := range s.manifests {
		expected["transactions/"+id+".json"] = id
	}
	for id := range s.blobs {
		expected["blobs/"+id+".blob"] = id
	}
	for p, id := range expected {
		if e = s.disk.verify(p, id); e != nil {
			return e
		}
	}
	return nil
}
func (s *Store) readBlob(id string) ([]byte, error) { return s.readBlobLimit(id, MaxLocalFileBytes) }
func (s *Store) readBlobLimit(id string, limit int) ([]byte, error) {
	if !validID(id) {
		return nil, errors.New("invalid blob id")
	}
	raw, e := s.disk.readCap("blobs/"+id+".blob", limit)
	if e != nil || byteID(raw) != id {
		return nil, errors.New("blob unavailable")
	}
	return raw, nil
}

// Reconcile folds verified deltas from the immutable anchor. Its index contains
// paths, digest references and positions, never full historical manifests or
// request payloads. One bounded delta/request/outcome is decoded at a time.
// No durability operation starts before the entire exact lineage is validated.
func (s *Store) Reconcile(expectedStateID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fenced = true
	fail := func() error { return refuse(StateUnavailable) }
	if s.closed || !validID(expectedStateID) || (s.pendingID != "" && s.pendingID != expectedStateID) {
		return fail()
	}
	paths, e := s.disk.scan()
	if e != nil {
		return fail()
	}
	raw, e := s.disk.read("anchor.json")
	if e != nil || !bytes.Equal(raw, canonical(genesis())) {
		return fail()
	}
	byBase := map[string]string{}
	ids := map[string]bool{}
	blobs := map[string]bool{}
	for _, p := range paths {
		if strings.HasPrefix(p, "blobs/") && strings.HasSuffix(p, ".blob") {
			id := strings.TrimSuffix(strings.TrimPrefix(p, "blobs/"), ".blob")
			if !validID(id) || s.disk.verify(p, id) != nil {
				return fail()
			}
			blobs[id] = true
		}
		if !strings.HasPrefix(p, "transactions/") || !strings.HasSuffix(p, ".json") {
			continue
		}
		id := strings.TrimSuffix(strings.TrimPrefix(p, "transactions/"), ".json")
		raw, e = s.disk.read(p)
		var m manifest
		if e != nil || len(raw) > 65536 || !decode(raw, &m) || !validID(id) || byteID(raw) != id || !validID(m.Base) {
			return fail()
		}
		if _, ok := byBase[m.Base]; ok {
			return fail()
		}
		byBase[m.Base] = id
		ids[id] = true
	}
	next := genesis()
	stateID := digest(next)
	visited := map[string]bool{stateID: true}
	for {
		id, ok := byBase[stateID]
		if !ok {
			break
		}
		raw, e = s.disk.read("transactions/" + id + ".json")
		var m manifest
		if e != nil || !decode(raw, &m) || byteID(raw) != id || m.Base != stateID {
			return fail()
		}
		requestBytes, e := s.readBlob(m.Retained.Request)
		r, unpackErr := s.unpackRequest(requestBytes)
		if e != nil || unpackErr != nil || validate(r) != nil {
			return fail()
		}
		expected, out, err := candidate(next, r)
		if err != nil || expected.Files == nil {
			return fail()
		}
		want, _ := delta(stateID, expected, r)
		if !bytes.Equal(canonical(want), canonical(m)) {
			return fail()
		}
		outcomeBytes, e := s.readBlob(m.Retained.Outcome)
		if e != nil || !bytes.Equal(outcomeBytes, canonical(outcomeRef(out))) {
			return fail()
		}
		for _, blob := range m.Files {
			if !blobs[blob] {
				return fail()
			}
		}
		// Transfer only the folded digest indexes; the bounded payloads are released.
		next = fold(next, expected)
		stateID = id
		if visited[id] {
			return fail()
		}
		visited[id] = true
	}
	if stateID != expectedStateID || len(visited) != len(ids)+1 {
		return fail()
	}
	if e = s.inventory(paths, next.Files, ids, blobs, digest(genesis())); e != nil {
		return fail()
	}
	current, e := s.disk.read("CURRENT")
	if e == nil {
		old := strings.TrimSuffix(string(current), "\n")
		if !bytes.Equal(current, []byte(old+"\n")) || !visited[old] {
			return fail()
		}
	} else if !errors.Is(e, os.ErrNotExist) || len(ids) == 0 {
		return fail()
	}
	// Existing exact materialized bytes must not be repaired over corruption.
	// Only missing referenced paths may be reconstructed from verified originals.
	for p, id := range next.Files {
		if raw, e = s.disk.read(p); e == nil {
			if byteID(raw) != id {
				return fail()
			}
		} else if !errors.Is(e, os.ErrNotExist) {
			return fail()
		}
	}
	for id := range ids {
		if e = s.disk.resync("transactions/" + id + ".json"); e != nil {
			return fail()
		}
	}
	for id := range blobs {
		if e = s.disk.resync("blobs/" + id + ".blob"); e != nil {
			return fail()
		}
	}
	if e = s.materialize(next.Files, stateID); e != nil {
		return fail()
	}
	for p := range next.Files {
		if e = s.disk.resync(p); e != nil {
			return fail()
		}
	}
	// Re-establish mkdir durability even if a process crash left its name visible.
	for _, p := range paths {
		if strings.HasSuffix(p, "/") {
			if e = s.disk.syncDir(filepath.Dir(strings.TrimSuffix(p, "/"))); e != nil {
				return fail()
			}
		}
	}
	if e = s.disk.atomic("CURRENT", []byte(stateID+"\n"), stateID, true); e != nil {
		return fail()
	}
	if e = s.disk.resync("CURRENT"); e != nil {
		return fail()
	}
	s.active, s.stateID, s.manifests, s.blobs = next, stateID, ids, blobs
	s.pendingID = ""
	s.fenced = false
	return nil
}
