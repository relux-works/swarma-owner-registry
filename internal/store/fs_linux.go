package store

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

// hooks are package-private test seams; production always uses real syscalls.
// after runs after the real boundary, permitting a crash at every such point.
type hooks struct {
	before, after func(op, path string) error
	write         func(*os.File, []byte) (int, error)
}
type disk struct {
	root       *os.File
	hooks      hooks
	totalBytes uint64
	scanned    map[string]stamp
}

func (d *disk) before(op, p string) error {
	if d.hooks.before != nil {
		return d.hooks.before(op, p)
	}
	return nil
}
func (d *disk) after(op, p string) error {
	if d.hooks.after != nil {
		return d.hooks.after(op, p)
	}
	return nil
}

// Every component is opened relative to a held fd with NOFOLLOW; O_NONBLOCK
// avoids blocking on an unexpected FIFO. Root ancestors must also be no-link.
func openDisk(root string) (*disk, error) {
	absolute, e := filepath.Abs(root)
	if e != nil {
		return nil, e
	}
	fd, e := syscall.Open("/", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if e != nil {
		return nil, e
	}
	for _, part := range strings.Split(strings.TrimPrefix(absolute, "/"), "/") {
		if part == "" {
			continue
		}
		next, err := syscall.Openat(fd, part, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		closeErr := syscall.Close(fd)
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			syscall.Close(next)
			return nil, closeErr
		}
		fd = next
	}
	f := os.NewFile(uintptr(fd), "store-root")
	st, e := f.Stat()
	if e != nil || st.Mode().Perm()&0077 != 0 {
		f.Close()
		return nil, errors.New("private root required")
	}
	if e = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		f.Close()
		return nil, e
	}
	return &disk{root: f}, nil
}
func (d *disk) close() error { return d.root.Close() }
func cleanRelative(p string) bool {
	return p != "" && p != "." && !filepath.IsAbs(p) && filepath.Clean(p) == p && !strings.Contains(p, "\\") && p != ".." && !strings.HasPrefix(p, "../")
}
func (d *disk) dir(p string) (*os.File, error) {
	fd, e := syscall.Openat(int(d.root.Fd()), ".", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if e != nil {
		return nil, e
	}
	if p != "." {
		if !cleanRelative(p) {
			syscall.Close(fd)
			return nil, errors.New("invalid local path")
		}
		for _, part := range strings.Split(p, "/") {
			next, err := syscall.Openat(fd, part, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
			closeErr := syscall.Close(fd)
			if err != nil {
				return nil, err
			}
			if closeErr != nil {
				syscall.Close(next)
				return nil, closeErr
			}
			fd = next
		}
	}
	return os.NewFile(uintptr(fd), "store-directory"), nil
}
func (d *disk) read(p string) ([]byte, error) { return d.readCap(p, MaxLocalFileBytes) }
func (d *disk) readCap(p string, limit int) ([]byte, error) {
	if limit < 0 || limit > MaxLocalFileBytes {
		return nil, errors.New("invalid read limit")
	}
	if !cleanRelative(p) {
		return nil, errors.New("invalid local path")
	}
	parent, e := d.dir(filepath.Dir(p))
	if e != nil {
		return nil, e
	}
	defer parent.Close()
	fd, e := syscall.Openat(int(parent.Fd()), filepath.Base(p), syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(fd), "store-file")
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return nil, e
	}
	if !st.Mode().IsRegular() {
		return nil, errors.New("special local file")
	}
	if st.Size() > int64(limit) {
		return nil, errors.New("local file too large")
	}
	raw, e := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if len(raw) > limit {
		return nil, errors.New("local file too large")
	}
	return raw, e
}
func layoutDirs() []string {
	out := []string{"records", "journal", "checkpoints", "transactions", "blobs"}
	for _, v := range contractsTypes() {
		out = append(out, "records/"+v)
	}
	return out
}
func contractsTypes() []string {
	return []string{"attestation-1", "grant-1", "revocation-1", "enrollment-1", "journal-entry-1", "checkpoint-1"}
}
func (d *disk) mkdir(p string) error {
	parent, e := d.dir(filepath.Dir(p))
	if e != nil {
		return e
	}
	defer parent.Close()
	if e = d.before("dir.mkdir", p); e != nil {
		return e
	}
	if e = syscall.Mkdirat(int(parent.Fd()), filepath.Base(p), 0700); e != nil {
		return e
	}
	if e = d.after("dir.mkdir", p); e != nil {
		return e
	}
	return d.syncDir(filepath.Dir(p))
}
func (d *disk) syncDir(p string) error {
	if e := d.before("dir.sync", p); e != nil {
		return e
	}
	f, e := d.dir(p)
	if e != nil {
		return e
	}
	defer f.Close()
	if e = f.Sync(); e != nil {
		return e
	}
	if e = d.after("dir.sync", p); e != nil {
		return e
	}
	if e = d.before("dir.close", p); e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return d.after("dir.close", p)
}

// Recovery must re-establish durability even for a surviving equal file: its
// presence after a process crash does not prove the last sync completed.
func (d *disk) resync(p string) error {
	parent, e := d.dir(filepath.Dir(p))
	if e != nil {
		return e
	}
	defer parent.Close()
	fd, e := syscall.Openat(int(parent.Fd()), filepath.Base(p), syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if e != nil {
		return e
	}
	f := os.NewFile(uintptr(fd), "store-recovery")
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return e
	}
	if !st.Mode().IsRegular() {
		return errors.New("special local file")
	}
	if e = d.before("file.sync", p); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	if e = d.after("file.sync", p); e != nil {
		return e
	}
	if e = d.before("file.close", p); e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if e = d.after("file.close", p); e != nil {
		return e
	}
	return d.syncDir(filepath.Dir(p))
}

func (d *disk) atomic(p string, data []byte, stateID string, replace bool) error {
	existing, e := d.read(p)
	if e == nil {
		if bytes.Equal(existing, data) {
			return nil
		}
		if !replace {
			return errors.New("immutable local bytes differ")
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	parent, e := d.dir(filepath.Dir(p))
	if e != nil {
		return e
	}
	defer parent.Close()
	// Staging evidence is retained on every failure. Random suffixes prevent
	// overwriting a pending fragment during exact-candidate recovery.
	tmp := ".tmp-" + stateID[7:] + "-" + rand.Text()
	label := filepath.Dir(p) + "/" + tmp
	if e = d.before("file.open", p); e != nil {
		return e
	}
	fd, e := syscall.Openat(int(parent.Fd()), tmp, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if e != nil {
		return e
	}
	f := os.NewFile(uintptr(fd), "store-staging")
	defer f.Close()
	if e = d.after("file.open", p); e != nil {
		return e
	}
	if e = d.before("file.write", p); e != nil {
		return e
	}
	write := d.hooks.write
	if write == nil {
		write = func(f *os.File, b []byte) (int, error) { return f.Write(b) }
	}
	n, e := write(f, data)
	if e != nil {
		return e
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	if e = d.after("file.write", p); e != nil {
		return e
	}
	if e = d.before("file.sync", p); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	if e = d.after("file.sync", p); e != nil {
		return e
	}
	if e = d.before("file.close", p); e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if e = d.after("file.close", p); e != nil {
		return e
	}
	if e = d.before("file.rename", p); e != nil {
		return e
	}
	if e = syscall.Renameat(int(parent.Fd()), filepath.Base(label), int(parent.Fd()), filepath.Base(p)); e != nil {
		return e
	}
	if e = d.after("file.rename", p); e != nil {
		return e
	}
	// The rename is not durable until its containing directory is synced.
	if e = d.syncDir(filepath.Dir(p)); e != nil {
		return e
	}
	return nil
}

func (d *disk) scan() ([]string, error) {
	var out []string
	d.totalBytes = 0
	d.scanned = map[string]stamp{}
	var walk func(string) error
	walk = func(p string) error {
		f, e := d.dir(p)
		if e != nil {
			return e
		}
		entries, e := f.ReadDir(-1)
		closeErr := f.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
		for _, entry := range entries {
			path := entry.Name()
			if p != "." {
				path = p + "/" + path
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return errors.New("local link")
			}
			if entry.IsDir() {
				if !allowedDirectory(path + "/") {
					return errors.New("unknown directory")
				}
				out = append(out, path+"/")
				if e = walk(path); e != nil {
					return e
				}
			} else {
				if entry.Type()&os.ModeType != 0 {
					return errors.New("special local file")
				}
				st, e := d.stat(path)
				if e != nil {
					return e
				}
				d.scanned[path] = st
				d.totalBytes += uint64(st.Size)
				out = append(out, path)
			}
		}
		return nil
	}
	if e := walk("."); e != nil {
		return nil, e
	}
	sort.Strings(out)
	return out, nil
}

// MaxLocalFileBytes bounds a local blob before decoding; it is not a wire cap.
// Worst-case valid query escaping plus base64 payloads fit below this bound.
const MaxLocalFileBytes = 16 * 1024 * 1024

type stamp struct {
	Dev, Ino     uint64
	Size         int64
	Mtime, Ctime syscall.Timespec
}

func (d *disk) stat(p string) (stamp, error) {
	parent, e := d.dir(filepath.Dir(p))
	if e != nil {
		return stamp{}, e
	}
	defer parent.Close()
	fd, e := syscall.Openat(int(parent.Fd()), filepath.Base(p), syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if e != nil {
		return stamp{}, e
	}
	defer syscall.Close(fd)
	var st syscall.Stat_t
	if e = syscall.Fstat(fd, &st); e != nil {
		return stamp{}, e
	}
	if st.Mode&syscall.S_IFMT != syscall.S_IFREG || st.Size < 0 || st.Size > MaxLocalFileBytes {
		return stamp{}, errors.New("unsafe local file")
	}
	return stamp{Dev: uint64(st.Dev), Ino: st.Ino, Size: st.Size, Mtime: st.Mtim, Ctime: st.Ctim}, nil
}

// Every audit re-reads and hashes authoritative bytes; metadata is not treated
// as a content identity. Comparing the descriptor-independent stamp also detects
// replacement/content writes observed during that bounded read.
func (d *disk) verify(p, id string) error {
	st, ok := d.scanned[p]
	if !ok {
		return errors.New("missing authoritative file")
	}
	b, e := d.read(p)
	if e != nil || byteID(b) != id {
		return errors.New("authoritative bytes unavailable")
	}
	after, e := d.stat(p)
	if e != nil || after != st {
		return errors.New("file changed during audit")
	}
	return nil
}

func (d *disk) ensureDir(p string) error {
	f, e := d.dir(p)
	if e == nil {
		return f.Close()
	}
	if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	if !allowedDirectory(p + "/") {
		return errors.New("unknown directory")
	}
	if parent := filepath.Dir(p); parent != "." {
		if e = d.ensureDir(parent); e != nil {
			return e
		}
	}
	return d.mkdir(p)
}
