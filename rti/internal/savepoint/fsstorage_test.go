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

func TestFSStorage_EmptyDir(t *testing.T) {
	_, err := NewFSStorage("")
	if err == nil {
		t.Errorf("NewFSStorage(\"\") succeeded; expected error")
	}
}
