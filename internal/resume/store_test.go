package resume

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"dropmesh/internal/protocol"
)

const testTransferID = "0123456789abcdef0123456789abcdef"

func testDescriptor(payload []byte) protocol.Descriptor {
	return protocol.Descriptor{
		TransferID: testTransferID,
		Name:       "received.bin",
		Size:       uint64(len(payload)),
		ChunkSize:  protocol.ChunkSize,
		SHA256:     sha256.Sum256(payload),
	}
}

func openTestStore(t *testing.T, out string, descriptor protocol.Descriptor) *Store {
	t.Helper()
	store, err := Open(out, descriptor, uint64(protocol.ChunkSize))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestStoreCommitRecoverAndPublish(t *testing.T) {
	out := t.TempDir()
	payload := []byte("durable chunk bytes")
	d := testDescriptor(payload)
	store := openTestStore(t, out, d)
	if err := store.CommitChunk(0, payload); err != nil {
		t.Fatal(err)
	}
	if got := store.Bitmap(); !bytes.Equal(got, []byte{1}) {
		t.Fatalf("bitmap = %08b, want first chunk committed", got)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store = openTestStore(t, out, d)
	if store.CommittedChunks() != 1 {
		t.Fatalf("recovered %d chunks, want 1", store.CommittedChunks())
	}
	if _, err := store.VerifyAndPublish(d.Name); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(out, d.Name))
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("published file = %q, err = %v", got, err)
	}
	if _, err = os.Stat(filepath.Join(out, StateDirectory, d.TransferID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("resume state remains after publication: %v", err)
	}
}

func TestStoreDropsTornJournalTail(t *testing.T) {
	out := t.TempDir()
	payload := []byte("committed")
	d := testDescriptor(payload)
	store := openTestStore(t, out, d)
	if err := store.CommitChunk(0, payload); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(out, StateDirectory, d.TransferID, JournalFile)
	f, err := os.OpenFile(journalPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write([]byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}

	store = openTestStore(t, out, d)
	if store.CommittedChunks() != 1 {
		t.Fatalf("recovered %d chunks after torn tail, want 1", store.CommittedChunks())
	}
	stat, err := store.journal.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if stat.Size() != store.journalEnd {
		t.Fatalf("journal size = %d, recovered end = %d", stat.Size(), store.journalEnd)
	}
}

func TestStoreRevalidatesCommittedData(t *testing.T) {
	out := t.TempDir()
	payload := []byte("original bytes")
	d := testDescriptor(payload)
	store := openTestStore(t, out, d)
	if err := store.CommitChunk(0, payload); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	dataPath := filepath.Join(out, StateDirectory, d.TransferID, DataFile)
	if err := os.WriteFile(dataPath, []byte("tampered bytes"), 0600); err != nil {
		t.Fatal(err)
	}

	store = openTestStore(t, out, d)
	if store.CommittedChunks() != 0 {
		t.Fatalf("recovered %d chunks from tampered state, want 0", store.CommittedChunks())
	}
	if err := store.CommitChunk(0, payload); err != nil {
		t.Fatal(err)
	}
}

func TestStoreResetsChunksAfterWholeFileMismatch(t *testing.T) {
	out := t.TempDir()
	expected := []byte("original chunk")
	d := testDescriptor(expected)
	store := openTestStore(t, out, d)
	if err := store.CommitChunk(0, []byte("modified chunk")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.VerifyAndPublish(d.Name); !errors.Is(err, ErrIntegrity) {
		t.Fatalf("VerifyAndPublish error = %v, want ErrIntegrity", err)
	}
	if store.CommittedChunks() != 0 {
		t.Fatalf("inconsistent data left %d chunks committed", store.CommittedChunks())
	}
	if err := store.CommitChunk(0, expected); err != nil {
		t.Fatal(err)
	}
	if _, err := store.VerifyAndPublish(d.Name); err != nil {
		t.Fatal(err)
	}
}

func TestStoreRejectsDescriptorMismatch(t *testing.T) {
	out := t.TempDir()
	d := testDescriptor([]byte("same size"))
	store := openTestStore(t, out, d)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	d.SHA256 = sha256.Sum256([]byte("other data"))
	_, err := Open(out, d, uint64(protocol.ChunkSize))
	if !errors.Is(err, ErrStateMismatch) {
		t.Fatalf("Open error = %v, want ErrStateMismatch", err)
	}
}

func TestStoreSerializesAndLimitsOutputState(t *testing.T) {
	out := t.TempDir()
	d := testDescriptor([]byte("payload"))
	first := openTestStore(t, out, d)
	if _, err := Open(out, d, uint64(protocol.ChunkSize)); !errors.Is(err, ErrActive) {
		t.Fatalf("concurrent Open error = %v, want ErrActive", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	other := d
	other.TransferID = "abcdef0123456789abcdef0123456789"
	if _, err := Open(out, other, uint64(protocol.ChunkSize)); !errors.Is(err, ErrOtherState) {
		t.Fatalf("second transfer Open error = %v, want ErrOtherState", err)
	}
}

func TestStoreExpiresAndDeletesState(t *testing.T) {
	out := t.TempDir()
	payload := []byte("payload")
	d := testDescriptor(payload)
	store := openTestStore(t, out, d)
	if err := store.CommitChunk(0, payload); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-ResumeLifetime - time.Minute)
	setFirstCommitTimestamp(t, out, d, payload, old)
	statePath := filepath.Join(out, StateDirectory, d.TransferID)
	now := time.Now()
	if err := os.Chtimes(statePath, now, now); err != nil {
		t.Fatal(err)
	}
	other := d
	other.TransferID = "abcdef0123456789abcdef0123456789"
	second := openTestStore(t, out, other)
	if _, err := os.Stat(filepath.Join(out, StateDirectory, d.TransferID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired state remains: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	if err := Delete(out, other.TransferID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, StateDirectory, other.TransferID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted state remains: %v", err)
	}
}

func TestOpenExpiresCurrentTransferFromLastCommittedChunkTime(t *testing.T) {
	out := t.TempDir()
	payload := []byte("payload")
	d := testDescriptor(payload)
	store := openTestStore(t, out, d)
	if err := store.CommitChunk(0, payload); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	setFirstCommitTimestamp(t, out, d, payload, time.Now().Add(-ResumeLifetime-time.Minute))
	statePath := filepath.Join(out, StateDirectory, d.TransferID)
	now := time.Now()
	if err := os.Chtimes(statePath, now, now); err != nil {
		t.Fatal(err)
	}
	store = openTestStore(t, out, d)
	if store.CommittedChunks() != 0 {
		t.Fatalf("expired current state recovered %d chunks", store.CommittedChunks())
	}
}

func setFirstCommitTimestamp(t *testing.T, out string, d protocol.Descriptor, payload []byte, at time.Time) {
	t.Helper()
	journalPath := filepath.Join(out, StateDirectory, d.TransferID, JournalFile)
	journal, err := os.OpenFile(journalPath, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	stat, err := journal.Stat()
	if err != nil {
		t.Fatal(err)
	}
	_, _, headerEnd, err := readHeader(journal, stat.Size())
	if err != nil {
		t.Fatal(err)
	}
	record := encodeRecord(0, 0, uint32(len(payload)), sha256.Sum256(payload), at)
	if _, err = journal.WriteAt(record, headerEnd); err != nil {
		t.Fatal(err)
	}
	if err = journal.Sync(); err != nil {
		t.Fatal(err)
	}
	if err = journal.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestStoreNeverReplacesDestination(t *testing.T) {
	out := t.TempDir()
	payload := []byte("new payload")
	d := testDescriptor(payload)
	if err := os.WriteFile(filepath.Join(out, d.Name), []byte("keep existing"), 0600); err != nil {
		t.Fatal(err)
	}
	store := openTestStore(t, out, d)
	if err := store.CommitChunk(0, payload); err != nil {
		t.Fatal(err)
	}
	if _, err := store.VerifyAndPublish(d.Name); err == nil {
		t.Fatal("different existing destination was accepted")
	}
	got, err := os.ReadFile(filepath.Join(out, d.Name))
	if err != nil || string(got) != "keep existing" {
		t.Fatalf("existing destination changed to %q, err = %v", got, err)
	}
}

func TestStoreAcceptsMatchingExistingDestinationIdempotently(t *testing.T) {
	out := t.TempDir()
	payload := []byte("same verified payload")
	d := testDescriptor(payload)
	if err := os.WriteFile(filepath.Join(out, d.Name), payload, 0600); err != nil {
		t.Fatal(err)
	}
	store := openTestStore(t, out, d)
	if err := store.CommitChunk(0, payload); err != nil {
		t.Fatal(err)
	}
	if _, err := store.VerifyAndPublish(d.Name); err != nil {
		t.Fatal(err)
	}
}

func TestStorePublishesEmptyFile(t *testing.T) {
	out := t.TempDir()
	d := testDescriptor(nil)
	d.Name = "empty.bin"
	store := openTestStore(t, out, d)
	if _, err := store.VerifyAndPublish(d.Name); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(out, d.Name))
	if err != nil || info.Size() != 0 {
		t.Fatalf("published empty file: info=%v err=%v", info, err)
	}
}

func TestStoreTruncatesRecordsBeyondDeclaredChunkCount(t *testing.T) {
	out := t.TempDir()
	d := testDescriptor(nil)
	store := openTestStore(t, out, d)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(out, StateDirectory, d.TransferID, JournalFile)
	f, err := os.OpenFile(journalPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	emptyDigest := sha256.Sum256(nil)
	if _, err = f.Write(encodeRecord(0, 0, 0, emptyDigest, time.Now())); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	store = openTestStore(t, out, d)
	if store.CommittedChunks() != 0 || len(store.Bitmap()) != 0 {
		t.Fatalf("empty descriptor recovered chunks=%d bitmap=%v", store.CommittedChunks(), store.Bitmap())
	}
}

func TestStoreRecoversAfterInjectedWriteAndSyncFaults(t *testing.T) {
	failure := errors.New("injected storage failure")
	diskFull := errors.New("no space left on device")
	payload := []byte("fault-tested chunk")
	cases := []struct {
		name string
		set  func(*Store)
		want error
	}{
		{name: "short data write", set: func(s *Store) {
			s.writeDataAtFn = func(p []byte, off int64) (int, error) { return len(p) / 2, nil }
		}, want: io.ErrShortWrite},
		{name: "data sync failure", set: func(s *Store) { s.syncDataFn = func() error { return failure } }, want: failure},
		{name: "disk exhaustion", set: func(s *Store) {
			s.writeDataAtFn = func([]byte, int64) (int, error) { return 0, diskFull }
		}, want: diskFull},
		{name: "short journal write", set: func(s *Store) {
			s.writeJournalFn = func(p []byte, off int64) (int, error) { return len(p) / 2, nil }
		}, want: io.ErrShortWrite},
		{name: "journal sync failure", set: func(s *Store) { s.syncJournalFn = func() error { return failure } }, want: failure},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			out := t.TempDir()
			d := testDescriptor(payload)
			store := openTestStore(t, out, d)
			test.set(store)
			if err := store.CommitChunk(0, payload); err == nil {
				t.Fatal("injected storage fault was ignored")
			} else if !errors.Is(err, test.want) {
				t.Fatalf("CommitChunk error = %v, want %v", err, test.want)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store = openTestStore(t, out, d)
			if store.CommittedChunks() > 1 {
				t.Fatalf("recovered impossible committed count %d", store.CommittedChunks())
			}
			if store.CommittedChunks() == 0 {
				if err := store.CommitChunk(0, payload); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := store.VerifyAndPublish(d.Name); err != nil {
				t.Fatalf("could not publish after recovery: %v", err)
			}
		})
	}
}

func BenchmarkDurableChunkCommit(b *testing.B) {
	payload := bytes.Repeat([]byte("x"), protocol.ChunkSize)
	d := testDescriptor(payload)
	d.Size = protocol.ChunkSize
	store, err := Open(b.TempDir(), d, protocol.ChunkSize)
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close()
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i != 0 {
			if err = store.ResetCommittedChunks(); err != nil {
				b.Fatal(err)
			}
		}
		if err = store.CommitChunk(0, payload); err != nil {
			b.Fatal(err)
		}
	}
}

func TestDeleteRejectsTransferSymlink(t *testing.T) {
	out := t.TempDir()
	stateRoot := filepath.Join(out, StateDirectory)
	if err := os.Mkdir(stateRoot, 0700); err != nil {
		t.Fatal(err)
	}
	targetID := "abcdef0123456789abcdef0123456789"
	linkID := "fedcba9876543210fedcba9876543210"
	target := filepath.Join(stateRoot, targetID)
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(targetID, filepath.Join(stateRoot, linkID)); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	if err := Delete(out, linkID); err == nil {
		t.Fatal("Delete followed a transfer-state symlink")
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("symlink target was affected: %v", err)
	}
}

func FuzzReadJournalHeader(f *testing.F) {
	d := testDescriptor([]byte("seed"))
	encoded, _ := json.Marshal(d)
	f.Add(encodeHeader(encoded))
	f.Add([]byte("DMRSM2"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > headerMax+16 {
			return
		}
		_, _, _, _ = readHeader(bytes.NewReader(data), int64(len(data)))
	})
}

func FuzzDecodeJournalRecord(f *testing.F) {
	f.Add(encodeRecord(0, 0, 4, sha256.Sum256([]byte("seed")), time.Now()))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > recordSize {
			return
		}
		_, _, _, _, _, _ = decodeRecord(data)
	})
}

func TestResumeCrashWorker(t *testing.T) {
	crashCase := os.Getenv("DROPMESH_TEST_CRASH_CASE")
	if crashCase == "" {
		return
	}
	out := os.Getenv("DROPMESH_TEST_CRASH_OUT")
	payload := []byte("crash recovery chunk")
	d := testDescriptor(payload)
	d.Name = "crash.bin"
	store, err := Open(out, d, uint64(protocol.ChunkSize))
	if err != nil {
		t.Fatal(err)
	}
	switch crashCase {
	case "after-data-write":
		if _, err = store.data.WriteAt(payload, 0); err != nil {
			t.Fatal(err)
		}
	case "after-data-sync":
		if _, err = store.data.WriteAt(payload, 0); err != nil {
			t.Fatal(err)
		}
		if err = store.data.Sync(); err != nil {
			t.Fatal(err)
		}
	case "after-record-write":
		if _, err = store.data.WriteAt(payload, 0); err != nil {
			t.Fatal(err)
		}
		if err = store.data.Sync(); err != nil {
			t.Fatal(err)
		}
		record := encodeRecord(0, 0, uint32(len(payload)), sha256.Sum256(payload), time.Now())
		if _, err = store.journal.WriteAt(record, store.journalEnd); err != nil {
			t.Fatal(err)
		}
	case "after-journal-sync", "after-publication":
		if err = store.CommitChunk(0, payload); err != nil {
			t.Fatal(err)
		}
		if crashCase == "after-publication" {
			name := filepath.Join(StateDirectory, d.TransferID, DataFile)
			if err = store.outputRoot.Link(name, d.Name); err != nil {
				t.Fatal(err)
			}
		}
	default:
		t.Fatalf("unknown crash case %q", crashCase)
	}
	os.Exit(0)
}

func TestRecoveryAfterAbruptProcessExit(t *testing.T) {
	for _, crashCase := range []string{"after-data-write", "after-data-sync", "after-record-write", "after-journal-sync", "after-publication"} {
		t.Run(crashCase, func(t *testing.T) {
			out := t.TempDir()
			cmd := exec.Command(os.Args[0], "-test.run=^TestResumeCrashWorker$")
			cmd.Env = append(os.Environ(), "DROPMESH_TEST_CRASH_CASE="+crashCase, "DROPMESH_TEST_CRASH_OUT="+out)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("crash worker failed: %v\n%s", err, output)
			}
			payload := []byte("crash recovery chunk")
			d := testDescriptor(payload)
			d.Name = "crash.bin"
			store, err := Open(out, d, uint64(protocol.ChunkSize))
			if err != nil {
				t.Fatal(err)
			}
			recovered := store.CommittedChunks()
			t.Logf("recovered committed chunks after process exit: %d", recovered)
			if recovered == 0 {
				if err = store.CommitChunk(0, payload); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = store.VerifyAndPublish(d.Name); err != nil {
				t.Fatalf("recovery publication failed: %v", err)
			}
			if err = store.Close(); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(filepath.Join(out, d.Name))
			if err != nil || !bytes.Equal(got, payload) {
				t.Fatalf("recovered output = %q err=%v", got, err)
			}
		})
	}
}
