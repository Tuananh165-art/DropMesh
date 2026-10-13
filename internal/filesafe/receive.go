package filesafe

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"dropmesh/internal/protocol"
)

func ValidateName(name string) error {
	if name == "" || name == "." || name == ".." || len(name) > 255 || !utf8.ValidString(name) {
		return fmt.Errorf("unsafe file name")
	}
	if strings.ContainsAny(name, "/\\:<>\"|?*") || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return fmt.Errorf("unsafe file name")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return fmt.Errorf("unsafe file name")
		}
	}
	base := strings.ToUpper(name)
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	if reserved(base) {
		return fmt.Errorf("unsafe reserved file name")
	}
	return nil
}

func reserved(s string) bool {
	if s == "CON" || s == "PRN" || s == "AUX" || s == "NUL" || s == "CONIN$" || s == "CONOUT$" {
		return true
	}
	for _, p := range []string{"COM", "LPT"} {
		if len(s) == 4 && strings.HasPrefix(s, p) && s[3] >= '1' && s[3] <= '9' {
			return true
		}
		runes := []rune(s)
		if len(runes) == 4 && strings.HasPrefix(s, p) && (runes[3] == '¹' || runes[3] == '²' || runes[3] == '³') {
			return true
		}
	}
	return false
}

type StagedFile struct {
	root      *os.Root
	temp      string
	file      *os.File
	final     string
	published bool
}

func NewStagedFile(dir, name string) (*StagedFile, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("open destination directory: %w", err)
	}
	for i := 0; i < 4; i++ {
		var b [12]byte
		if _, err = rand.Read(b[:]); err != nil {
			root.Close()
			return nil, fmt.Errorf("create temporary name: %w", err)
		}
		temp := ".dropmesh-" + hex.EncodeToString(b[:]) + ".partial"
		f, e := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e == nil {
			return &StagedFile{root: root, temp: temp, file: f, final: name}, nil
		}
		if !os.IsExist(e) {
			root.Close()
			return nil, fmt.Errorf("create temporary output: %w", e)
		}
	}
	root.Close()
	return nil, fmt.Errorf("could not reserve a temporary output name")
}

func (s *StagedFile) Writer() io.Writer { return s.file }
func (s *StagedFile) SyncAndPublish() error {
	if err := s.file.Sync(); err != nil {
		return fmt.Errorf("sync received file: %w", err)
	}
	if err := s.file.Close(); err != nil {
		s.file = nil
		return fmt.Errorf("close received file: %w", err)
	}
	s.file = nil
	if err := s.root.Link(s.temp, s.final); err != nil {
		return fmt.Errorf("cannot safely publish received file without replacing an existing path (destination filesystem may not support hard links): %w", err)
	}
	if err := s.root.Remove(s.temp); err != nil {
		return fmt.Errorf("remove temporary link: %w", err)
	}
	s.published = true
	return nil
}
func (s *StagedFile) Close() error {
	var result error
	if s.file != nil {
		if err := s.file.Close(); err != nil {
			result = err
		}
		s.file = nil
	}
	if !s.published {
		if err := s.root.Remove(s.temp); err != nil && !os.IsNotExist(err) && result == nil {
			result = err
		}
	}
	if err := s.root.Close(); err != nil && result == nil {
		result = err
	}
	return result
}

func ValidateDescriptor(d protocol.Descriptor, maxBytes uint64) error {
	if err := ValidateName(d.Name); err != nil {
		return err
	}
	if d.TransferID == "" || len(d.TransferID) > 64 {
		return fmt.Errorf("invalid transfer identifier")
	}
	if d.ChunkSize != protocol.ChunkSize {
		return fmt.Errorf("unsupported chunk size")
	}
	if d.Size > maxBytes {
		return fmt.Errorf("transfer exceeds configured size limit")
	}
	return nil
}
