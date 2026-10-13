package filesafe

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"dropmesh/internal/protocol"
)

func TestValidatePortableNames(t *testing.T) {
	good := []string{"file.txt", "résumé.bin", "archive.tar.gz"}
	for _, name := range good {
		if err := ValidateName(name); err != nil {
			t.Errorf("%q rejected: %v", name, err)
		}
	}
	bad := []string{"", ".", "..", "../escape", "dir\\file", "C:stream", "COM1.txt", "COM1.part.txt", "COM¹.txt", "CONIN$", "nul", "name.", "name ", "a?b", "a<b", `a"b`, "bad\x00name", "bad\nname", "\\server\\file"}
	for _, name := range bad {
		if err := ValidateName(name); err == nil {
			t.Errorf("unsafe name accepted: %q", name)
		}
	}
}

func TestStagedFileDoesNotFollowDestinationSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated Windows privileges")
	}
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "victim.txt")
	if err := os.WriteFile(outside, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link.txt")); err != nil {
		t.Fatal(err)
	}
	s, err := NewStagedFile(dir, "link.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, _ = s.Writer().Write([]byte("attacker"))
	if err = s.SyncAndPublish(); err == nil {
		t.Fatal("symlink destination was replaced")
	}
	got, err := os.ReadFile(outside)
	if err != nil || string(got) != "original" {
		t.Fatalf("outside target changed: %q %v", got, err)
	}
}

func TestStagedFilePublishesWithoutOverwrite(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStagedFile(dir, "received.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Writer().Write([]byte("verified")); err != nil {
		t.Fatal(err)
	}
	if err = s.SyncAndPublish(); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "received.bin"))
	if err != nil || string(got) != "verified" {
		t.Fatalf("published bytes %q, %v", got, err)
	}
	s2, err := NewStagedFile(dir, "received.bin")
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	_, _ = s2.Writer().Write([]byte("overwrite"))
	if err = s2.SyncAndPublish(); err == nil {
		t.Fatal("existing file overwritten")
	}
	got, _ = os.ReadFile(filepath.Join(dir, "received.bin"))
	if string(got) != "verified" {
		t.Fatalf("existing file changed: %q", got)
	}
}

func TestStagedFileSyncFailureDoesNotPublish(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStagedFile(dir, "failed.bin")
	if err != nil {
		t.Fatal(err)
	}
	tempName := s.temp
	if _, err = s.Writer().Write([]byte("incomplete")); err != nil {
		t.Fatal(err)
	}
	if err = s.file.Close(); err != nil {
		t.Fatal(err)
	}
	if err = s.SyncAndPublish(); err == nil {
		t.Fatal("sync failure was accepted")
	}
	s.file = nil // Simulate an OS-level close failure after the sync error.
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"failed.bin", tempName} {
		if _, err = os.Lstat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("failure left %s behind: %v", name, err)
		}
	}
}

func TestValidateDescriptorLimits(t *testing.T) {
	d := protocol.Descriptor{TransferID: "transfer", Name: "x", Size: 9, ChunkSize: protocol.ChunkSize, SHA256: sha256.Sum256(nil)}
	if err := ValidateDescriptor(d, 9); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDescriptor(d, 8); err == nil {
		t.Fatal("oversize accepted")
	}
	d.Name = "../x"
	if err := ValidateDescriptor(d, 100); err == nil {
		t.Fatal("unsafe descriptor accepted")
	}
}

func FuzzValidateName(f *testing.F) {
	f.Add("file.txt")
	f.Add("../escape")
	f.Add("NUL.txt")
	f.Fuzz(func(t *testing.T, name string) {
		if len(name) > 4096 {
			return
		}
		_ = ValidateName(name)
	})
}
