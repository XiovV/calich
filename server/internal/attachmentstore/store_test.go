package attachmentstore

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStore_SaveAndOpen(t *testing.T) {
	store := New(t.TempDir())

	id := "aabbccdd-0000-0000-0000-000000000000"
	size, err := store.Save(id, strings.NewReader("hello world"))
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if size != int64(len("hello world")) {
		t.Fatalf("size = %d, want %d", size, len("hello world"))
	}

	f, err := store.Open(id)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "hello world" {
		t.Fatalf("content = %q, want %q", got, "hello world")
	}
}

func TestStore_SaveShardsByIDPrefix(t *testing.T) {
	dataDir := t.TempDir()
	store := New(dataDir)

	id := "aabbccdd-0000-0000-0000-000000000000"
	if _, err := store.Save(id, strings.NewReader("x")); err != nil {
		t.Fatalf("save: %v", err)
	}

	want := filepath.Join(dataDir, "attachments", "aa", id)
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected file at %s: %v", want, err)
	}
}

func TestStore_OpenMissing(t *testing.T) {
	store := New(t.TempDir())
	if _, err := store.Open("does-not-exist"); err == nil {
		t.Fatal("expected an error opening a missing attachment")
	}
}

func TestStore_Delete(t *testing.T) {
	store := New(t.TempDir())

	id := "aabbccdd-0000-0000-0000-000000000000"
	if _, err := store.Save(id, strings.NewReader("x")); err != nil {
		t.Fatalf("save: %v", err)
	}

	store.Delete(id)

	if _, err := store.Open(id); err == nil {
		t.Fatal("expected the file to be gone after Delete")
	}

	// Deleting an id that was never saved must not error/panic (best-effort,
	// ADR-0040) — the sweeper and a delete racing a crash both rely on this.
	store.Delete("never-existed")
}

func TestStore_SweepRemovesOnlyUnknownFiles(t *testing.T) {
	store := New(t.TempDir())

	keep := "aabbccdd-0000-0000-0000-000000000000"
	orphan := "aabb1111-0000-0000-0000-000000000000"
	if _, err := store.Save(keep, strings.NewReader("keep")); err != nil {
		t.Fatalf("save keep: %v", err)
	}
	if _, err := store.Save(orphan, strings.NewReader("orphan")); err != nil {
		t.Fatalf("save orphan: %v", err)
	}

	if err := store.Sweep(map[string]bool{keep: true}); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	if _, err := store.Open(keep); err != nil {
		t.Fatalf("expected keep to survive the sweep: %v", err)
	}
	if _, err := store.Open(orphan); err == nil {
		t.Fatal("expected orphan to be removed by the sweep")
	}
}

func TestStore_SweepOnMissingDir(t *testing.T) {
	store := New(t.TempDir())
	if err := store.Sweep(map[string]bool{}); err != nil {
		t.Fatalf("sweep on a data dir with no attachments/ yet: %v", err)
	}
}
