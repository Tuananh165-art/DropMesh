package protocol

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

const (
	Version         = 2
	MaxControlFrame = 64 << 10
	ChunkSize       = 1 << 20
	ChunkHeaderSize = 8 + 8 + 4 + sha256.Size
)

type Message struct {
	Version      int         `json:"version"`
	Type         string      `json:"type"`
	Session      string      `json:"session,omitempty"`
	Secret       string      `json:"secret,omitempty"`
	Descriptor   *Descriptor `json:"descriptor,omitempty"`
	ResumeBitmap []byte      `json:"resume_bitmap"`
	Reason       string      `json:"reason,omitempty"`
}

type Descriptor struct {
	TransferID string            `json:"transfer_id"`
	Name       string            `json:"name"`
	Size       uint64            `json:"size"`
	ChunkSize  uint32            `json:"chunk_size"`
	SHA256     [sha256.Size]byte `json:"sha256"`
}

func WriteMessage(w io.Writer, m Message) error {
	b, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("encode control message: %w", err)
	}
	if len(b) > MaxControlFrame {
		return fmt.Errorf("control message exceeds %d bytes", MaxControlFrame)
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(b)))
	if err = writeFull(w, header[:]); err != nil {
		return fmt.Errorf("write control length: %w", err)
	}
	if err = writeFull(w, b); err != nil {
		return fmt.Errorf("write control message: %w", err)
	}
	return nil
}

func ReadMessage(r *bufio.Reader) (Message, error) {
	var m Message
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return m, fmt.Errorf("read control length: %w", err)
	}
	n := binary.BigEndian.Uint32(header[:])
	if n == 0 || n > MaxControlFrame {
		return m, fmt.Errorf("invalid control frame size %d", n)
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return m, fmt.Errorf("read control message: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return m, fmt.Errorf("decode control message: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return m, fmt.Errorf("control message has trailing data")
	}
	if m.Version != Version {
		return m, fmt.Errorf("unsupported protocol version %d", m.Version)
	}
	return m, nil
}

func WriteChunkHeader(w io.Writer, index, offset uint64, length uint32, digest [sha256.Size]byte) error {
	var b [ChunkHeaderSize]byte
	binary.BigEndian.PutUint64(b[0:8], index)
	binary.BigEndian.PutUint64(b[8:16], offset)
	binary.BigEndian.PutUint32(b[16:20], length)
	copy(b[20:], digest[:])
	return writeFull(w, b[:])
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

func ReadChunkHeader(r io.Reader) (index, offset uint64, length uint32, digest [sha256.Size]byte, err error) {
	var b [ChunkHeaderSize]byte
	if _, err = io.ReadFull(r, b[:]); err != nil {
		return
	}
	index = binary.BigEndian.Uint64(b[0:8])
	offset = binary.BigEndian.Uint64(b[8:16])
	length = binary.BigEndian.Uint32(b[16:20])
	copy(digest[:], b[20:])
	return
}
