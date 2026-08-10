package antedom

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFingerprintDetectsChanges(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(file, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}

	base := fingerprint(dir)
	if again := fingerprint(dir); again != base {
		t.Errorf("unchanged tree: fingerprint %d then %d", base, again)
	}

	if err := os.WriteFile(file, []byte("longer content"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed := fingerprint(dir)
	if changed == base {
		t.Error("fingerprint unchanged after rewriting a file")
	}

	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	if added := fingerprint(dir); added == changed {
		t.Error("fingerprint unchanged after adding a file")
	}
}

func TestFingerprintFileRoot(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "antedom.js")
	if err := os.WriteFile(file, []byte("antedom.apiVersion(1)"), 0o644); err != nil {
		t.Fatal(err)
	}

	base := fingerprint(file)
	if err := os.WriteFile(file, []byte("antedom.apiVersion(1) // edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	if changed := fingerprint(file); changed == base {
		t.Error("fingerprint unchanged after rewriting a file root")
	}
}

func TestFingerprintMissingRoot(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "nope")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A missing root is skipped, not fatal: a project may have no
	// antedom.js or data/ at all.
	if with, without := fingerprint(dir, missing), fingerprint(dir); with != without {
		t.Errorf("missing root altered fingerprint: %d vs %d", with, without)
	}
}
