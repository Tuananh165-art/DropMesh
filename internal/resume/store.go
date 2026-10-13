package resume

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"dropmesh/internal/filesafe"
	"dropmesh/internal/protocol"
)

const (
	StateDirectory = ".dropmesh-state"
	DataFile       = "data.partial"
	JournalFile    = "journal"
	lockFileName   = ".active.lock"
	ResumeLifetime = 24 * time.Hour
	MaxChunks      = 200_000
	headerMax      = 64 << 10
	recordSize     = 1 + 8 + 8 + 4 + sha256.Size + 8 + 4
)

var (
	journalMagic     = [8]byte{'D', 'M', 'R', 'S', 'M', '2', 0, 2}
	crcTable         = crc32.MakeTable(crc32.Castagnoli)
	ErrActive        = errors.New("a receiver is already using resumable state in this output directory")
	ErrOtherState    = errors.New("another incomplete transfer exists in this output directory")
	ErrBadTransfer   = errors.New("invalid transfer identifier")
	ErrStateCorrupt  = errors.New("resume state is corrupt")
	ErrStateMismatch = errors.New("resume state does not match the offered transfer")
	ErrIntegrity     = errors.New("whole-file integrity check failed")
	ErrDestination   = errors.New("destination already exists and does not match the verified transfer")
)

type Store struct {
	outputRoot     *os.Root
	stateRoot      *os.Root
	transferRoot   *os.Root
	data           *os.File
	journal        *os.File
	lock           *os.File
	unlock         func() error
	descriptor     protocol.Descriptor
	chunkCount     uint64
	committed      uint64
	journalEnd     int64
	headerEnd      int64
	lastChunkAt    time.Time
	closed         bool
	writeDataAtFn  func([]byte, int64) (int, error)
	writeJournalFn func([]byte, int64) (int, error)
	syncDataFn     func() error
	syncJournalFn  func() error
}

func ValidateID(id string) error {
	if len(id) != 32 || strings.ToLower(id) != id {
		return ErrBadTransfer
	}
	b, err := hex.DecodeString(id)
	if err != nil || len(b) != 16 {
		return ErrBadTransfer
	}
	return nil
}

func Open(outputDir string, d protocol.Descriptor, maxBytes uint64) (*Store, error) {
	if err := ValidateID(d.TransferID); err != nil {
		return nil, err
	}
	if err := filesafe.ValidateDescriptor(d, maxBytes); err != nil {
		return nil, err
	}
	if d.Size > math.MaxInt64 {
		return nil, fmt.Errorf("transfer is too large for local file offsets")
	}
	count := chunkCount(d.Size)
	if count > MaxChunks {
		return nil, fmt.Errorf("transfer requires too many resumable chunks")
	}
	outputRoot, err := os.OpenRoot(outputDir)
	if err != nil {
		return nil, fmt.Errorf("open output directory: %w", err)
	}
	stateRoot, err := openStateRoot(outputRoot)
	if err != nil {
		_ = outputRoot.Close()
		return nil, err
	}
	lock, err := openLockFile(stateRoot)
	if err != nil {
		_ = stateRoot.Close()
		_ = outputRoot.Close()
		return nil, err
	}
	unlock, err := acquireFileLock(lock)
	if err != nil {
		_ = lock.Close()
		_ = stateRoot.Close()
		_ = outputRoot.Close()
		if errors.Is(err, ErrActive) {
			return nil, ErrActive
		}
		return nil, fmt.Errorf("lock resume state: %w", err)
	}
	base := &Store{outputRoot: outputRoot, stateRoot: stateRoot, lock: lock, unlock: unlock, descriptor: d, chunkCount: count}
	if err = base.removeExpired(time.Now(), d.TransferID); err != nil {
		_ = base.Close()
		return nil, err
	}
	if err = base.rejectOtherState(d.TransferID); err != nil {
		_ = base.Close()
		return nil, err
	}
	if err = base.openTransfer(d); err != nil {
		_ = base.Close()
		return nil, err
	}
	return base, nil
}

func openStateRoot(outputRoot *os.Root) (*os.Root, error) {
	info, err := outputRoot.Lstat(StateDirectory)
	if errors.Is(err, os.ErrNotExist) {
		if err = outputRoot.Mkdir(StateDirectory, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("create receiver resume directory: %w", err)
		}
		info, err = outputRoot.Lstat(StateDirectory)
	}
	if err != nil {
		return nil, fmt.Errorf("inspect receiver resume directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("receiver resume path is not a real directory")
	}
	root, err := outputRoot.OpenRoot(StateDirectory)
	if err != nil {
		return nil, fmt.Errorf("open receiver resume directory: %w", err)
	}
	return root, nil
}

func (s *Store) openTransfer(d protocol.Descriptor) error {
	info, err := s.stateRoot.Lstat(d.TransferID)
	isNew := errors.Is(err, os.ErrNotExist)
	if err != nil && !isNew {
		return fmt.Errorf("inspect resume state: %w", err)
	}
	if isNew {
		if err = s.stateRoot.Mkdir(d.TransferID, 0700); err != nil {
			return fmt.Errorf("create transfer resume state: %w", err)
		}
	} else if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("transfer resume state is not a real directory")
	}
	s.transferRoot, err = s.stateRoot.OpenRoot(d.TransferID)
	if err != nil {
		return fmt.Errorf("open transfer resume state: %w", err)
	}
	if isNew {
		if err = s.createFiles(); err != nil {
			_ = s.removeOwnState()
			return err
		}
		return nil
	}
	if err = s.openAndRecover(); err == nil {
		lastActivity := s.lastChunkAt
		if lastActivity.IsZero() {
			lastActivity = info.ModTime()
		}
		if time.Since(lastActivity) >= ResumeLifetime {
			return s.recreateExpiredState(d)
		}
		return nil
	}
	if errors.Is(err, ErrStateMismatch) {
		if time.Since(lastJournalActivity(s.stateRoot, d.TransferID, info.ModTime())) >= ResumeLifetime {
			return s.recreateExpiredState(d)
		}
		return err
	}
	if !errors.Is(err, ErrStateCorrupt) && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("open existing resume state: %w", err)
	}
	return s.recreateExpiredState(d)
}

func (s *Store) recreateExpiredState(d protocol.Descriptor) error {
	_ = s.closeTransferFiles()
	if err := s.removeOwnState(); err != nil {
		return fmt.Errorf("remove expired or unusable resume state: %w", err)
	}
	if err := s.stateRoot.Mkdir(d.TransferID, 0700); err != nil {
		return fmt.Errorf("recreate transfer resume state: %w", err)
	}
	var err error
	s.transferRoot, err = s.stateRoot.OpenRoot(d.TransferID)
	if err != nil {
		return fmt.Errorf("open recreated resume state: %w", err)
	}
	s.committed = 0
	s.journalEnd = 0
	s.headerEnd = 0
	s.lastChunkAt = time.Time{}
	return s.createFiles()
}

func (s *Store) createFiles() error {
	var err error
	s.data, err = s.transferRoot.OpenFile(DataFile, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("create resume data file: %w", err)
	}
	s.journal, err = s.transferRoot.OpenFile(JournalFile, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("create resume journal: %w", err)
	}
	encoded, err := json.Marshal(s.descriptor)
	if err != nil {
		return fmt.Errorf("encode resume descriptor: %w", err)
	}
	if len(encoded) > headerMax-16 {
		return fmt.Errorf("resume descriptor exceeds journal header limit")
	}
	header := encodeHeader(encoded)
	if err = writeFull(s.journal, header); err != nil {
		return fmt.Errorf("write resume journal header: %w", err)
	}
	if err = s.journal.Sync(); err != nil {
		return fmt.Errorf("sync resume journal header: %w", err)
	}
	s.journalEnd = int64(len(header))
	s.headerEnd = s.journalEnd
	return nil
}

func (s *Store) openAndRecover() error {
	var err error
	for _, name := range []string{DataFile, JournalFile} {
		info, statErr := s.transferRoot.Lstat(name)
		if errors.Is(statErr, os.ErrNotExist) {
			return ErrStateCorrupt
		}
		if statErr != nil {
			return statErr
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return ErrStateCorrupt
		}
	}
	s.data, err = s.transferRoot.OpenFile(DataFile, os.O_RDWR, 0600)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrStateCorrupt
		}
		return err
	}
	s.journal, err = s.transferRoot.OpenFile(JournalFile, os.O_RDWR, 0600)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrStateCorrupt
		}
		return err
	}
	return s.recoverJournal()
}

func (s *Store) recoverJournal() error {
	stat, err := s.journal.Stat()
	if err != nil {
		return err
	}
	header, descriptor, end, err := readHeader(s.journal, stat.Size())
	_ = header
	if err != nil {
		return ErrStateCorrupt
	}
	if descriptor != s.descriptor {
		return ErrStateMismatch
	}
	validEnd := end
	s.headerEnd = end
	var committed uint64
	var record [recordSize]byte
	for validEnd+recordSize <= stat.Size() {
		if _, err = s.journal.ReadAt(record[:], validEnd); err != nil {
			break
		}
		index, offset, length, digest, committedAt, ok := decodeRecord(record[:])
		if !ok || committed >= s.chunkCount || index != committed || offset != chunkOffset(index) || length == 0 || uint64(length) != chunkLength(s.descriptor.Size, index) {
			break
		}
		data := make([]byte, length)
		if length != 0 {
			if _, err = s.data.ReadAt(data, int64(offset)); err != nil {
				break
			}
		}
		if sha256.Sum256(data) != digest {
			break
		}
		committed++
		s.lastChunkAt = committedAt
		validEnd += recordSize
	}
	if validEnd != stat.Size() {
		if err = s.journal.Truncate(validEnd); err != nil {
			return fmt.Errorf("truncate incomplete resume journal: %w", err)
		}
		if err = s.syncJournalFile(); err != nil {
			return fmt.Errorf("sync recovered resume journal: %w", err)
		}
	}
	s.committed = committed
	s.journalEnd = validEnd
	return nil
}

func (s *Store) Bitmap() []byte {
	length := (s.chunkCount + 7) / 8
	bitmap := make([]byte, int(length))
	for i := uint64(0); i < s.committed; i++ {
		bitmap[i/8] |= 1 << (i % 8)
	}
	return bitmap
}

func (s *Store) CommittedChunks() uint64 { return s.committed }
func (s *Store) ChunkCount() uint64      { return s.chunkCount }

func (s *Store) CommitChunk(index uint64, payload []byte) error {
	if s.closed || s.data == nil || s.journal == nil {
		return fmt.Errorf("resume state is closed")
	}
	if index != s.committed || index >= s.chunkCount {
		return fmt.Errorf("unexpected resume chunk index")
	}
	expected := chunkLength(s.descriptor.Size, index)
	if uint64(len(payload)) != expected || expected == 0 {
		return fmt.Errorf("invalid resume chunk length")
	}
	offset := chunkOffset(index)
	digest := sha256.Sum256(payload)
	n, err := s.writeDataAt(payload, int64(offset))
	if err != nil {
		return fmt.Errorf("write resume chunk: %w", err)
	}
	if n != len(payload) {
		return fmt.Errorf("write resume chunk: %w", io.ErrShortWrite)
	}
	if err = s.syncDataFile(); err != nil {
		return fmt.Errorf("sync resume chunk: %w", err)
	}
	committedAt := time.Now().UTC()
	record := encodeRecord(index, offset, uint32(len(payload)), digest, committedAt)
	n, err = s.writeJournalRecord(record, s.journalEnd)
	if err != nil {
		return fmt.Errorf("append resume journal: %w", err)
	}
	if n != len(record) {
		return fmt.Errorf("append resume journal: %w", io.ErrShortWrite)
	}
	if err = s.syncJournalFile(); err != nil {
		return fmt.Errorf("sync resume journal: %w", err)
	}
	s.journalEnd += recordSize
	s.committed++
	s.lastChunkAt = committedAt
	return nil
}

func (s *Store) VerifyAndPublish(name string) (string, error) {
	if err := filesafe.ValidateName(name); err != nil {
		return "", err
	}
	if s.closed || s.data == nil || s.journal == nil {
		return "", fmt.Errorf("resume state is closed")
	}
	if s.committed != s.chunkCount {
		return "", fmt.Errorf("resume transfer is incomplete")
	}
	if err := s.syncDataFile(); err != nil {
		return "", fmt.Errorf("sync completed resume data: %w", err)
	}
	whole := sha256.New()
	n, err := io.Copy(whole, io.NewSectionReader(s.data, 0, int64(s.descriptor.Size)))
	if err != nil {
		return "", fmt.Errorf("verify completed resume data: %w", err)
	}
	if uint64(n) != s.descriptor.Size || !bytes.Equal(whole.Sum(nil), s.descriptor.SHA256[:]) {
		if resetErr := s.ResetCommittedChunks(); resetErr != nil {
			return "", fmt.Errorf("%w; discard inconsistent resume data: %v", ErrIntegrity, resetErr)
		}
		return "", ErrIntegrity
	}
	if err = s.journal.Close(); err != nil {
		s.journal = nil
		return "", fmt.Errorf("close resume journal: %w", err)
	}
	s.journal = nil
	if err = s.data.Close(); err != nil {
		s.data = nil
		return "", fmt.Errorf("close resume data: %w", err)
	}
	s.data = nil
	internalDataPath := filepath.Join(StateDirectory, s.descriptor.TransferID, DataFile)
	if err = s.outputRoot.Link(internalDataPath, name); err != nil {
		if !errors.Is(err, os.ErrExist) && !os.IsExist(err) {
			return "", fmt.Errorf("publish verified file without replacement: %w", err)
		}
		if err = s.verifyExisting(name); err != nil {
			return "", err
		}
	}
	if err = s.removeOwnState(); err != nil {
		return "", fmt.Errorf("file was published but resume-state cleanup failed: %w", err)
	}
	return filepath.Join(".", name), nil
}

func (s *Store) ResetCommittedChunks() error {
	if s.closed || s.data == nil || s.journal == nil {
		return fmt.Errorf("resume state is closed")
	}
	if err := s.journal.Truncate(s.headerEnd); err != nil {
		return fmt.Errorf("truncate resume journal: %w", err)
	}
	if err := s.syncJournalFile(); err != nil {
		return fmt.Errorf("sync reset resume journal: %w", err)
	}
	if err := s.data.Truncate(0); err != nil {
		return fmt.Errorf("truncate resume data: %w", err)
	}
	if err := s.syncDataFile(); err != nil {
		return fmt.Errorf("sync reset resume data: %w", err)
	}
	s.committed = 0
	s.lastChunkAt = time.Time{}
	s.journalEnd = s.headerEnd
	return nil
}

func (s *Store) writeDataAt(p []byte, offset int64) (int, error) {
	if s.writeDataAtFn != nil {
		return s.writeDataAtFn(p, offset)
	}
	return s.data.WriteAt(p, offset)
}

func (s *Store) writeJournalRecord(p []byte, offset int64) (int, error) {
	if s.writeJournalFn != nil {
		return s.writeJournalFn(p, offset)
	}
	return s.journal.WriteAt(p, offset)
}

func (s *Store) syncDataFile() error {
	if s.syncDataFn != nil {
		return s.syncDataFn()
	}
	return s.data.Sync()
}

func (s *Store) syncJournalFile() error {
	if s.syncJournalFn != nil {
		return s.syncJournalFn()
	}
	return s.journal.Sync()
}

func (s *Store) verifyExisting(name string) error {
	info, err := s.outputRoot.Lstat(name)
	if err != nil {
		return fmt.Errorf("inspect existing destination: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || uint64(info.Size()) != s.descriptor.Size {
		return ErrDestination
	}
	f, err := s.outputRoot.Open(name)
	if err != nil {
		return fmt.Errorf("open existing destination for verification: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return fmt.Errorf("verify existing destination: %w", err)
	}
	if !bytes.Equal(h.Sum(nil), s.descriptor.SHA256[:]) {
		return ErrDestination
	}
	return nil
}

func (s *Store) closeTransferFiles() error {
	var result error
	if s.journal != nil {
		if err := s.journal.Close(); err != nil && result == nil {
			result = err
		}
		s.journal = nil
	}
	if s.data != nil {
		if err := s.data.Close(); err != nil && result == nil {
			result = err
		}
		s.data = nil
	}
	return result
}

func (s *Store) removeOwnState() error {
	_ = s.closeTransferFiles()
	if s.transferRoot != nil {
		_ = s.transferRoot.Close()
		s.transferRoot = nil
	}
	for _, name := range []string{DataFile, JournalFile} {
		if err := s.stateRoot.Remove(filepath.Join(s.descriptor.TransferID, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := s.stateRoot.Remove(s.descriptor.TransferID); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *Store) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	var result error
	if err := s.closeTransferFiles(); err != nil {
		result = err
	}
	if s.transferRoot != nil {
		if err := s.transferRoot.Close(); err != nil && result == nil {
			result = err
		}
		s.transferRoot = nil
	}
	if s.unlock != nil {
		if err := s.unlock(); err != nil && result == nil {
			result = err
		}
		s.unlock = nil
	}
	if s.lock != nil {
		if err := s.lock.Close(); err != nil && result == nil {
			result = err
		}
		s.lock = nil
	}
	if s.stateRoot != nil {
		if err := s.stateRoot.Close(); err != nil && result == nil {
			result = err
		}
		s.stateRoot = nil
	}
	if s.outputRoot != nil {
		if err := s.outputRoot.Close(); err != nil && result == nil {
			result = err
		}
		s.outputRoot = nil
	}
	return result
}

func Delete(outputDir, id string) error {
	if err := ValidateID(id); err != nil {
		return err
	}
	outputRoot, err := os.OpenRoot(outputDir)
	if err != nil {
		return fmt.Errorf("open output directory: %w", err)
	}
	defer outputRoot.Close()
	if _, err = outputRoot.Lstat(StateDirectory); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect receiver resume directory: %w", err)
	}
	stateRoot, err := openStateRoot(outputRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer stateRoot.Close()
	lock, err := openLockFile(stateRoot)
	if err != nil {
		return err
	}
	defer lock.Close()
	unlock, err := acquireFileLock(lock)
	if err != nil {
		if errors.Is(err, ErrActive) {
			return ErrActive
		}
		return fmt.Errorf("lock resume state: %w", err)
	}
	defer unlock()
	info, err := stateRoot.Lstat(id)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect resume state: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("resume state path is not a real directory")
	}
	s := &Store{stateRoot: stateRoot, descriptor: protocol.Descriptor{TransferID: id}}
	transferRoot, err := stateRoot.OpenRoot(id)
	if err != nil {
		return fmt.Errorf("open transfer resume state: %w", err)
	}
	s.transferRoot = transferRoot
	if err = s.removeOwnState(); err != nil {
		return fmt.Errorf("delete resume state: %w", err)
	}
	return nil
}

func (s *Store) removeExpired(now time.Time, currentID string) error {
	entries, err := readDir(s.stateRoot)
	if err != nil {
		return fmt.Errorf("read receiver resume directory: %w", err)
	}
	for _, entry := range entries {
		id := entry.Name()
		if err = ValidateID(id); err != nil || !entry.IsDir() {
			continue
		}
		if id == currentID {
			continue
		}
		info, e := s.stateRoot.Lstat(id)
		if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		last := lastJournalActivity(s.stateRoot, id, info.ModTime())
		if now.Sub(last) < ResumeLifetime {
			continue
		}
		if err = removeStateAt(s.stateRoot, id); err != nil {
			return fmt.Errorf("remove expired resume state: %w", err)
		}
	}
	return nil
}

func lastJournalActivity(root *os.Root, id string, fallback time.Time) time.Time {
	transferRoot, err := root.OpenRoot(id)
	if err != nil {
		return fallback
	}
	defer transferRoot.Close()
	for _, name := range []string{DataFile, JournalFile} {
		info, statErr := transferRoot.Lstat(name)
		if statErr != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return fallback
		}
	}
	journal, err := transferRoot.Open(JournalFile)
	if err != nil {
		return fallback
	}
	stat, err := journal.Stat()
	if err != nil {
		_ = journal.Close()
		return fallback
	}
	_, d, end, err := readHeader(journal, stat.Size())
	if err != nil || d.TransferID != id || d.Size > math.MaxInt64 || chunkCount(d.Size) > MaxChunks {
		_ = journal.Close()
		return fallback
	}
	var last time.Time
	var count uint64
	var record [recordSize]byte
	for count < chunkCount(d.Size) && end+recordSize <= stat.Size() {
		if _, err = journal.ReadAt(record[:], end); err != nil {
			break
		}
		index, offset, length, _, committedAt, ok := decodeRecord(record[:])
		if !ok || index != count || offset != chunkOffset(index) || length == 0 || uint64(length) != chunkLength(d.Size, index) {
			break
		}
		last = committedAt
		count++
		end += recordSize
	}
	_ = journal.Close()
	if last.IsZero() {
		return fallback
	}
	return last
}

func (s *Store) rejectOtherState(currentID string) error {
	entries, err := readDir(s.stateRoot)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() != currentID && ValidateID(entry.Name()) == nil {
			return ErrOtherState
		}
	}
	return nil
}

func openLockFile(root *os.Root) (*os.File, error) {
	info, err := root.Lstat(lockFileName)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect resume-state lock: %w", err)
	}
	if err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return nil, fmt.Errorf("resume-state lock is not a regular file")
	}
	f, err := root.OpenFile(lockFileName, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open resume-state lock: %w", err)
	}
	info, err = f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("inspect opened resume-state lock: %w", err)
	}
	if !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, fmt.Errorf("resume-state lock is not a regular file")
	}
	return f, nil
}

func readDir(root *os.Root) ([]os.DirEntry, error) {
	dir, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	return dir.ReadDir(-1)
}

func removeStateAt(root *os.Root, id string) error {
	info, err := root.Lstat(id)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("resume state path is not a real directory")
	}
	transferRoot, err := root.OpenRoot(id)
	if err != nil {
		return err
	}
	for _, name := range []string{DataFile, JournalFile} {
		if err = transferRoot.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
			_ = transferRoot.Close()
			return err
		}
	}
	if err = transferRoot.Close(); err != nil {
		return err
	}
	return root.Remove(id)
}

func chunkCount(size uint64) uint64 {
	if size == 0 {
		return 0
	}
	return (size-1)/protocol.ChunkSize + 1
}

func chunkOffset(index uint64) uint64 { return index * protocol.ChunkSize }

func chunkLength(size, index uint64) uint64 {
	offset := chunkOffset(index)
	remaining := size - offset
	if remaining > protocol.ChunkSize {
		return protocol.ChunkSize
	}
	return remaining
}

func encodeHeader(descriptor []byte) []byte {
	b := make([]byte, 12+len(descriptor)+4)
	copy(b[:8], journalMagic[:])
	binary.BigEndian.PutUint32(b[8:12], uint32(len(descriptor)))
	copy(b[12:12+len(descriptor)], descriptor)
	binary.BigEndian.PutUint32(b[12+len(descriptor):], crc32.Checksum(b[:12+len(descriptor)], crcTable))
	return b
}

func readHeader(r io.ReaderAt, fileSize int64) ([]byte, protocol.Descriptor, int64, error) {
	var d protocol.Descriptor
	var fixed [12]byte
	if fileSize < int64(len(fixed)) {
		return nil, d, 0, ErrStateCorrupt
	}
	if _, err := r.ReadAt(fixed[:], 0); err != nil {
		return nil, d, 0, err
	}
	if !bytes.Equal(fixed[:8], journalMagic[:]) {
		return nil, d, 0, ErrStateCorrupt
	}
	n := binary.BigEndian.Uint32(fixed[8:])
	if n == 0 || n > headerMax-16 || fileSize < int64(12+n+4) {
		return nil, d, 0, ErrStateCorrupt
	}
	encoded := make([]byte, n)
	if _, err := r.ReadAt(encoded, 12); err != nil {
		return nil, d, 0, err
	}
	var checksum [4]byte
	if _, err := r.ReadAt(checksum[:], int64(12+n)); err != nil {
		return nil, d, 0, err
	}
	if binary.BigEndian.Uint32(checksum[:]) != crc32.Checksum(append(fixed[:], encoded...), crcTable) {
		return nil, d, 0, ErrStateCorrupt
	}
	dec := json.NewDecoder(bytes.NewReader(encoded))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		return nil, d, 0, ErrStateCorrupt
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return nil, d, 0, ErrStateCorrupt
	}
	if ValidateID(d.TransferID) != nil || filesafe.ValidateName(d.Name) != nil || d.ChunkSize != protocol.ChunkSize {
		return nil, d, 0, ErrStateCorrupt
	}
	return encoded, d, int64(12 + n + 4), nil
}

func encodeRecord(index, offset uint64, length uint32, digest [sha256.Size]byte, committedAt time.Time) []byte {
	b := make([]byte, recordSize)
	b[0] = 1
	binary.BigEndian.PutUint64(b[1:9], index)
	binary.BigEndian.PutUint64(b[9:17], offset)
	binary.BigEndian.PutUint32(b[17:21], length)
	copy(b[21:21+sha256.Size], digest[:])
	binary.BigEndian.PutUint64(b[21+sha256.Size:29+sha256.Size], uint64(committedAt.UnixNano()))
	binary.BigEndian.PutUint32(b[recordSize-4:], crc32.Checksum(b[:recordSize-4], crcTable))
	return b
}

func decodeRecord(b []byte) (uint64, uint64, uint32, [sha256.Size]byte, time.Time, bool) {
	var digest [sha256.Size]byte
	if len(b) != recordSize || b[0] != 1 || binary.BigEndian.Uint32(b[recordSize-4:]) != crc32.Checksum(b[:recordSize-4], crcTable) {
		return 0, 0, 0, digest, time.Time{}, false
	}
	index := binary.BigEndian.Uint64(b[1:9])
	offset := binary.BigEndian.Uint64(b[9:17])
	length := binary.BigEndian.Uint32(b[17:21])
	copy(digest[:], b[21:21+sha256.Size])
	nanos := int64(binary.BigEndian.Uint64(b[21+sha256.Size : 29+sha256.Size]))
	committedAt := time.Unix(0, nanos).UTC()
	if nanos <= 0 || committedAt.After(time.Now().Add(5*time.Minute)) {
		return 0, 0, 0, digest, time.Time{}, false
	}
	return index, offset, length, digest, committedAt, true
}

func writeFull(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}
