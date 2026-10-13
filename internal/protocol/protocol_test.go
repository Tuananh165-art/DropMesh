package protocol

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"reflect"
	"testing"
)

func TestMessageRoundTrip(t *testing.T) {
	want := Message{Version: Version, Type: "accept", Session: "session", ResumeBitmap: []byte{1, 2}}
	var b bytes.Buffer
	if err := WriteMessage(&b, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadMessage(bufio.NewReader(&b))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestReadMessageRejectsInvalidFrames(t *testing.T) {
	for _, size := range []uint32{0, MaxControlFrame + 1} {
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], size)
		if _, err := ReadMessage(bufio.NewReader(bytes.NewReader(b[:]))); err == nil {
			t.Fatalf("size %d accepted", size)
		}
	}
	var b bytes.Buffer
	if err := WriteMessage(&b, Message{Version: Version + 1, Type: "hello"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadMessage(bufio.NewReader(&b)); err == nil {
		t.Fatal("unsupported version accepted")
	}
	var trailing bytes.Buffer
	_, _ = trailing.Write([]byte{0, 0, 0, 20})
	_, _ = trailing.Write([]byte(`{"version":1,"type":"ok"} {}`))
	if _, err := ReadMessage(bufio.NewReader(&trailing)); err == nil {
		t.Fatal("trailing JSON accepted")
	}
}

func TestMaximumResumeBitmapFitsControlFrame(t *testing.T) {
	message := Message{Version: Version, Type: "accept", Session: "session", ResumeBitmap: make([]byte, (100<<30)/(1<<20)/8)}
	var b bytes.Buffer
	if err := WriteMessage(&b, message); err != nil {
		t.Fatalf("maximum supported resume bitmap exceeded control-frame bound: %v", err)
	}
	if b.Len() > MaxControlFrame+4 {
		t.Fatalf("encoded frame has %d bytes", b.Len())
	}
}

func TestChunkHeaderRoundTrip(t *testing.T) {
	want := sha256.Sum256([]byte("chunk"))
	var b bytes.Buffer
	if err := WriteChunkHeader(&b, 7, 700, 5, want); err != nil {
		t.Fatal(err)
	}
	i, o, n, d, err := ReadChunkHeader(&b)
	if err != nil {
		t.Fatal(err)
	}
	if i != 7 || o != 700 || n != 5 || d != want {
		t.Fatalf("chunk header mismatch")
	}
}

func FuzzReadMessage(f *testing.F) {
	var b bytes.Buffer
	_ = WriteMessage(&b, Message{Version: Version, Type: "cancel"})
	f.Add(b.Bytes())
	f.Add([]byte{0, 0, 0, 255})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > MaxControlFrame+4 {
			return
		}
		_, _ = ReadMessage(bufio.NewReader(bytes.NewReader(data)))
	})
}
