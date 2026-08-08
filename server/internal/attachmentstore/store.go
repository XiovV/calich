// Package attachmentstore is the filesystem half of an Attachment (#132,
// ADR-0040): the database row is the truth, this is where its bytes live.
// There is no transaction spanning SQLite and the filesystem, so every
// method here is written to make the survivable failure the likely one —
// see Save and Delete's doc comments.
package attachmentstore

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Store reads and writes Attachment bytes under
// baseDir/<first two hex chars of id>/<id> (ADR-0040) — the id is the
// Attachment's filename, never a name derived from user input, so there is
// no traversal, collision, or unicode-normalization handling to write.
type Store struct {
	baseDir string
}

func New(dataDir string) *Store {
	return &Store{baseDir: filepath.Join(dataDir, "attachments")}
}

// shardDir is baseDir/<first two hex chars of id> — a flat 256-way split so
// no single directory ends up holding every Attachment this instance has
// ever stored.
func (s *Store) shardDir(id string) string {
	shard := id
	if len(shard) > 2 {
		shard = shard[:2]
	}
	return filepath.Join(s.baseDir, shard)
}

func (s *Store) path(id string) string {
	return filepath.Join(s.shardDir(id), id)
}

// Save streams r to id's file: write to a temp file in the same directory,
// then rename into place. A crash between the two leaves a temp file, never
// a partially written Attachment at its real path — id's file only ever
// exists complete. Returns the number of bytes written.
func (s *Store) Save(id string, r io.Reader) (int64, error) {
	dir := s.shardDir(id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, fmt.Errorf("create shard dir: %w", err)
	}

	tmp, err := os.CreateTemp(dir, id+".tmp-*")
	if err != nil {
		return 0, fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	// Best-effort: gone once the rename below succeeds, and irrelevant if it
	// doesn't, since the caller is discarding the upload either way.
	defer os.Remove(tmpName)

	size, err := io.Copy(tmp, r)
	if err != nil {
		tmp.Close()
		return 0, fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return 0, fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tmpName, s.path(id)); err != nil {
		return 0, fmt.Errorf("rename into place: %w", err)
	}

	return size, nil
}

// Open returns id's bytes for reading. The caller must Close it.
func (s *Store) Open(id string) (*os.File, error) {
	f, err := os.Open(s.path(id))
	if err != nil {
		return nil, fmt.Errorf("open attachment: %w", err)
	}
	return f, nil
}

// Delete removes id's file, best-effort — called only after the row
// deletion it follows has already committed (ADR-0040), so a failure here
// (including the file already being gone) just leaves it for Sweep to
// reclaim later rather than being treated as this call's own error.
func (s *Store) Delete(id string) {
	os.Remove(s.path(id))
}

// Sweep removes every file under baseDir whose id is not in known,
// reclaiming bytes left behind by a crash between Save and its row commit,
// or by a cascade delete in SQLite that no Go code observed file-by-file
// (calendar deletion, series deletion, account deletion — ADR-0040). Worst
// case without a Sweep is wasted disk, which is recoverable; a missing file
// for a live row is not, so this only ever deletes what known says nothing
// references.
func (s *Store) Sweep(known map[string]bool) error {
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read attachments dir: %w", err)
	}

	for _, shard := range entries {
		if !shard.IsDir() {
			continue
		}
		shardPath := filepath.Join(s.baseDir, shard.Name())
		files, err := os.ReadDir(shardPath)
		if err != nil {
			return fmt.Errorf("read shard dir: %w", err)
		}
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			id := f.Name()
			if known[id] {
				continue
			}
			os.Remove(filepath.Join(shardPath, id))
		}
	}

	return nil
}
