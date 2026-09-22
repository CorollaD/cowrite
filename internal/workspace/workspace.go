package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ErrConflict is returned when a save is based on a version of the file that
// is no longer what is on disk, which means something else edited it.
var ErrConflict = errors.New("file changed on disk since it was loaded")

type ConflictError struct {
	Path     string
	DiskHash string
	BaseHash string
	DiskBody string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("conflict at %s: base %s, disk %s", e.Path, short(e.BaseHash), short(e.DiskHash))
}
func (e *ConflictError) Unwrap() error { return ErrConflict }

func short(h string) string {
	if len(h) > 8 {
		return h[:8]
	}
	return h
}

// Workspace is the markdown tree on disk. It is the source of truth; the
// SQLite index is derived from it.
type Workspace struct {
	Root string

	// selfWritten records hashes this process just wrote, so the file
	// watcher can tell its own writes apart from external edits and does
	// not feed them back as change notifications.
	mu          sync.Mutex
	selfWritten map[string]string
}

func New(root string) (*Workspace, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(abs, "posts"), 0o755); err != nil {
		return nil, fmt.Errorf("create workspace: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(abs, ".cowrite"), 0o755); err != nil {
		return nil, fmt.Errorf("create workspace metadata dir: %w", err)
	}
	return &Workspace{Root: abs, selfWritten: make(map[string]string)}, nil
}

func (w *Workspace) PostsDir() string { return filepath.Join(w.Root, "posts") }
func (w *Workspace) MetaDir() string  { return filepath.Join(w.Root, ".cowrite") }

// Entry is one post file found on disk.
type Entry struct {
	Path    string // absolute
	RelPath string // relative to workspace root, the index key
	Post    *Post
	Hash    string
	Size    int64
	MTime   time.Time
}

// Scan walks the posts tree and returns every post file.
func (w *Workspace) Scan() ([]Entry, error) {
	var entries []Entry
	err := filepath.WalkDir(w.PostsDir(), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		e, err := w.Load(path)
		if err != nil {
			// One unreadable file must not abort the whole scan.
			return nil
		}
		entries = append(entries, *e)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan workspace: %w", err)
	}
	return entries, nil
}

// Load reads and parses a single post file.
func (w *Workspace) Load(path string) (*Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	post, err := ParsePost(data)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	rel, err := filepath.Rel(w.Root, path)
	if err != nil {
		rel = path
	}
	return &Entry{
		Path:    path,
		RelPath: rel,
		Post:    post,
		Hash:    HashContent(data),
		Size:    info.Size(),
		MTime:   info.ModTime(),
	}, nil
}

// Save writes a post, refusing the write if the file changed underneath.
//
// baseHash is the hash the editor had when it loaded the file. An empty
// baseHash means "create", and the write fails if the file already exists.
func (w *Workspace) Save(path string, post *Post, baseHash string) (*Entry, error) {
	existing, err := os.ReadFile(path)
	switch {
	case err == nil:
		diskHash := HashContent(existing)
		if baseHash == "" {
			return nil, fmt.Errorf("refusing to overwrite existing file %s", path)
		}
		if diskHash != baseHash {
			disk, _ := ParsePost(existing)
			body := ""
			if disk != nil {
				body = disk.Body
			}
			return nil, &ConflictError{
				Path: path, DiskHash: diskHash, BaseHash: baseHash, DiskBody: body,
			}
		}
	case errors.Is(err, os.ErrNotExist):
		// Creating; nothing to compare against.
	default:
		return nil, fmt.Errorf("read %s before save: %w", path, err)
	}

	data, err := post.Bytes()
	if err != nil {
		return nil, err
	}
	if err := WriteAtomic(path, data, 0o644); err != nil {
		return nil, err
	}

	hash := HashContent(data)
	w.markSelfWritten(path, hash)

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat after save: %w", err)
	}
	rel, err := filepath.Rel(w.Root, path)
	if err != nil {
		rel = path
	}
	return &Entry{
		Path: path, RelPath: rel, Post: post,
		Hash: hash, Size: info.Size(), MTime: info.ModTime(),
	}, nil
}

func (w *Workspace) markSelfWritten(path, hash string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.selfWritten[path] = hash
}

// WasSelfWritten reports whether the given path+hash is a write this process
// just made, consuming the record so a later external edit is still seen.
//
// Without this the watcher would react to our own saves, notify the editor,
// and drive a save loop.
func (w *Workspace) WasSelfWritten(path, hash string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if got, ok := w.selfWritten[path]; ok && got == hash {
		delete(w.selfWritten, path)
		return true
	}
	return false
}

var nonSlug = regexp.MustCompile(`[^\p{L}\p{N}]+`)

// Slugify builds a filesystem-safe name, keeping CJK characters since a
// Chinese title would otherwise slug down to nothing.
func Slugify(title string) string {
	s := nonSlug.ReplaceAllString(strings.ToLower(strings.TrimSpace(title)), "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "untitled"
	}
	r := []rune(s)
	if len(r) > 60 {
		s = strings.Trim(string(r[:60]), "-")
	}
	return s
}

// NewPostPath allocates a path for a new post, dated so the tree stays
// browsable, and suffixed if that slug is already taken.
func (w *Workspace) NewPostPath(title string, now time.Time) string {
	slug := Slugify(title)
	dir := filepath.Join(w.PostsDir(), now.Format("2006"), now.Format("01"))
	candidate := filepath.Join(dir, slug+".md")
	for i := 2; ; i++ {
		if _, err := os.Stat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate
		}
		candidate = filepath.Join(dir, fmt.Sprintf("%s-%d.md", slug, i))
	}
}
