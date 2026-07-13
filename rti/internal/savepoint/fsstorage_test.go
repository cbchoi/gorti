package savepoint

import (
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"testing"
)

func TestFSStorage_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFSStorage(dir)
	if err != nil {
		t.Fatalf("NewFSStorage: %v", err)
	}

	if store.Exists("fed", "lbl") {
		t.Errorf("Exists pre-write returned true; expected false")
	}

	w, err := store.Writer("fed", "lbl")
	if err != nil {
		t.Fatalf("Writer: %v", err)
	}
	if _, err := w.Write([]byte("hello")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if !store.Exists("fed", "lbl") {
		t.Errorf("Exists post-write returned false; expected true")
	}

	r, err := store.Reader("fed", "lbl")
	if err != nil {
		t.Fatalf("Reader: %v", err)
	}
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	_ = r.Close()
	if !bytes.Equal(got, []byte("hello")) {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestFSStorage_DuplicateWriteRejected(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFSStorage(dir)
	if err != nil {
		t.Fatalf("NewFSStorage: %v", err)
	}
	w, err := store.Writer("fed", "lbl")
	if err != nil {
		t.Fatalf("first Writer: %v", err)
	}
	_ = w.Close()

	_, err = store.Writer("fed", "lbl")
	if !errors.Is(err, ErrSaveBundleExists) {
		t.Errorf("second Writer err = %v, want ErrSaveBundleExists", err)
	}
}

func TestFSStorage_ReadMissingBundle(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFSStorage(dir)
	if err != nil {
		t.Fatalf("NewFSStorage: %v", err)
	}
	_, err = store.Reader("fed", "no-such")
	if !errors.Is(err, ErrSaveBundleNotFound) {
		t.Errorf("err = %v, want ErrSaveBundleNotFound", err)
	}
}

func TestFSStorage_FilenameEscape(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFSStorage(dir)
	if err != nil {
		t.Fatalf("NewFSStorage: %v", err)
	}
	// A federation name with chars unsafe on Windows must round-trip
	// through the bundle layer by surviving filenameEscape.
	w, err := store.Writer("fed/with:slashes", "label?with*stars")
	if err != nil {
		t.Fatalf("Writer with unsafe chars: %v", err)
	}
	_, _ = w.Write([]byte("ok"))
	_ = w.Close()
	if !store.Exists("fed/with:slashes", "label?with*stars") {
		t.Errorf("Exists with unsafe chars returned false")
	}
	// And the on-disk filename actually contains escape sequences.
	got := store.bundlePath("fed/with:slashes", "label?with*stars")
	want := filepath.Join(dir, "fed%2Fwith%3Aslashes__label%3Fwith%2Astars.bundle")
	if got != want {
		t.Errorf("bundlePath = %q, want %q", got, want)
	}
}

func TestFSStorage_GenerationPathAndRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFSStorage(dir)
	if err != nil {
		t.Fatalf("NewFSStorage: %v", err)
	}
	key := StorageKey{Federation: "fed", Generation: 42, Label: "lbl"}
	w, err := store.WriterFor(key)
	if err != nil {
		t.Fatalf("WriterFor: %v", err)
	}
	if _, err := w.Write([]byte("v2-data")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if store.ExistsFor(key) {
		t.Fatal("bundle became visible before Close")
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	wantPath := filepath.Join(dir, "v2", "666564", "000000000000002a", "6c626c.bundle")
	if got := store.bundlePathFor(key); got != wantPath {
		t.Fatalf("bundlePathFor = %q, want %q", got, wantPath)
	}
	r, err := store.ReaderFor(key)
	if err != nil {
		t.Fatalf("ReaderFor: %v", err)
	}
	got, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != "v2-data" {
		t.Fatalf("data = %q, want v2-data", got)
	}
}

func TestFSStorage_SameLabelCoexistsAcrossGenerations(t *testing.T) {
	store, err := NewFSStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewFSStorage: %v", err)
	}
	for generation, data := range map[uint64]string{1: "one", 2: "two"} {
		key := StorageKey{Federation: "fed", Generation: generation, Label: "same"}
		w, err := store.WriterFor(key)
		if err != nil {
			t.Fatalf("WriterFor generation %d: %v", generation, err)
		}
		_, _ = w.Write([]byte(data))
		if err := w.Close(); err != nil {
			t.Fatalf("Close generation %d: %v", generation, err)
		}
	}
	for generation, want := range map[uint64]string{1: "one", 2: "two"} {
		r, err := store.ReaderFor(StorageKey{Federation: "fed", Generation: generation, Label: "same"})
		if err != nil {
			t.Fatalf("ReaderFor generation %d: %v", generation, err)
		}
		got, _ := io.ReadAll(r)
		_ = r.Close()
		if string(got) != want {
			t.Fatalf("generation %d data = %q, want %q", generation, got, want)
		}
	}
}

func TestFSStorage_EscapingIsInjective(t *testing.T) {
	if filenameEscape("/") == filenameEscape("%2F") {
		t.Fatal("legacy escaping aliases slash with literal percent escape")
	}
	if hexComponent("/") == hexComponent("%2F") {
		t.Fatal("v2 encoding aliases distinct labels")
	}
	store, err := NewFSStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewFSStorage: %v", err)
	}
	left := store.bundlePathFor(StorageKey{Federation: "a/b", Generation: 1, Label: "c"})
	right := store.bundlePathFor(StorageKey{Federation: "a", Generation: 1, Label: "b/c"})
	if left == right {
		t.Fatalf("v2 paths alias distinct federation/label pairs: %q", left)
	}
}

func TestFSStorage_FailedPublicationIsInvisible(t *testing.T) {
	store, err := NewFSStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewFSStorage: %v", err)
	}
	key := StorageKey{Federation: "fed", Generation: 5, Label: "failed"}
	w, err := store.WriterFor(key)
	if err != nil {
		t.Fatalf("WriterFor: %v", err)
	}
	_, _ = w.Write([]byte("partial"))
	fsWriter := w.(*fsBundleWriter)
	if err := fsWriter.file.Close(); err != nil {
		t.Fatalf("force-close temp file: %v", err)
	}
	if err := w.Close(); err == nil {
		t.Fatal("Close succeeded after forced sync failure")
	}
	if store.ExistsFor(key) {
		t.Fatal("failed publication left a visible final bundle")
	}
	if _, err := store.ReaderFor(key); !errors.Is(err, ErrSaveBundleNotFound) {
		t.Fatalf("ReaderFor failed bundle error = %v, want ErrSaveBundleNotFound", err)
	}
}

func TestFSStorage_EmptyDir(t *testing.T) {
	_, err := NewFSStorage("")
	if err == nil {
		t.Errorf("NewFSStorage(\"\") succeeded; expected error")
	}
}
