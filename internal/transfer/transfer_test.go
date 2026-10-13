package transfer

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dropmesh/internal/auth"
	"dropmesh/internal/protocol"
	"dropmesh/internal/resume"
	quictransport "dropmesh/internal/transport/quic"
)

func TestDirectQUICTransferEndToEnd(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.bin")
	receiverDir := filepath.Join(dir, "received")
	if err := os.Mkdir(receiverDir, 0700); err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte{0, 1, 2, 255}, 700000)
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	reader, writer := io.Pipe()
	sendDone := make(chan error, 1)
	go func() {
		defer writer.Close()
		sendDone <- Send(ctx, SenderOptions{File: source, Listen: "127.0.0.1:0", Advertise: "127.0.0.1"}, writer)
	}()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024), auth.MaxInvite+1024)
	var invite string
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), auth.InvitePrefix) {
			invite = scanner.Text()
			break
		}
	}
	if invite == "" {
		t.Fatalf("sender failed to print invite: %v", scanner.Err())
	}
	received, err := Receive(ctx, ReceiverOptions{Invite: invite, OutputDir: receiverDir, Yes: true, MaxBytes: DefaultMaxBytes})
	if err != nil {
		t.Fatal(err)
	}
	for scanner.Scan() {
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if err := <-sendDone; err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(received)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(got) != sha256.Sum256(data) {
		t.Fatal("received file digest differs")
	}
}

func TestTransferResumesAfterBothProcessesRestart(t *testing.T) {
	work := t.TempDir()
	source := filepath.Join(work, "resume.bin")
	output := filepath.Join(work, "received")
	if err := os.Mkdir(output, 0700); err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("resume-data-"), 300000)
	if err := os.WriteFile(source, payload, 0600); err != nil {
		t.Fatal(err)
	}
	transferID := "89abcdef0123456789abcdef01234567"
	deadline, stop := context.WithTimeout(context.Background(), 45*time.Second)
	defer stop()

	firstInvite, firstDone, firstPipe := startResumeSender(t, deadline, source, transferID)
	firstCtx, cancelFirst := context.WithCancel(deadline)
	_, firstErr := Receive(firstCtx, ReceiverOptions{
		Invite: firstInvite, OutputDir: output, Yes: true,
		Progress: func(done, _ uint64) {
			if done >= protocol.ChunkSize {
				cancelFirst()
			}
		},
	})
	cancelFirst()
	if FailureCategory(firstErr) != FailureCancellation {
		t.Fatalf("first receiver failure = %q, want cancellation (error: %v)", FailureCategory(firstErr), firstErr)
	}
	if err := <-firstDone; err == nil {
		t.Fatal("first sender reported success after receiver cancellation")
	}
	_ = firstPipe.Close()

	descriptor := protocol.Descriptor{TransferID: transferID, Name: filepath.Base(source), Size: uint64(len(payload)), ChunkSize: protocol.ChunkSize, SHA256: sha256.Sum256(payload)}
	store, err := resume.Open(output, descriptor, uint64(len(payload)))
	if err != nil {
		t.Fatal(err)
	}
	committed := store.CommittedChunks()
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	if committed != 1 {
		t.Fatalf("after interruption committed %d chunks, want 1", committed)
	}

	secondInvite, secondDone, secondPipe := startResumeSender(t, deadline, source, transferID)
	path, err := Receive(deadline, ReceiverOptions{Invite: secondInvite, OutputDir: output, Yes: true})
	if err != nil {
		sendErr := <-secondDone
		t.Fatalf("resumed receiver failed: %v (sender: %v)", err, sendErr)
	}
	if err = <-secondDone; err != nil {
		t.Fatalf("resumed sender failed: %v", err)
	}
	_ = secondPipe.Close()
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("resumed output mismatch: len=%d err=%v", len(got), err)
	}
}

func startResumeSender(t *testing.T, ctx context.Context, source, transferID string) (string, <-chan error, *io.PipeReader) {
	t.Helper()
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() {
		defer writer.Close()
		done <- Send(ctx, SenderOptions{File: source, TransferID: transferID, Listen: "127.0.0.1:0", Advertise: "127.0.0.1"}, writer)
	}()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024), auth.MaxInvite+1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, auth.InvitePrefix) {
			go func() {
				for scanner.Scan() {
				}
			}()
			return line, done, reader
		}
	}
	_ = reader.Close()
	t.Fatalf("sender did not offer a resume invite: %v", scanner.Err())
	return "", nil, nil
}

func TestEmptyAndUnicodeNameTransfers(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{name: "empty.txt"},
		{name: "résumé-猫.txt", data: []byte("unicode name and payload")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, tc.name)
			outDir := filepath.Join(dir, "received")
			if err := os.Mkdir(outDir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(source, tc.data, 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			inviteReader, inviteWriter := io.Pipe()
			sendDone := make(chan error, 1)
			go func() {
				defer inviteWriter.Close()
				sendDone <- Send(ctx, SenderOptions{File: source, Listen: "127.0.0.1:0", Advertise: "127.0.0.1"}, inviteWriter)
			}()
			scanner := bufio.NewScanner(inviteReader)
			scanner.Buffer(make([]byte, 1024), auth.MaxInvite+1024)
			var invite string
			for scanner.Scan() {
				if strings.HasPrefix(scanner.Text(), auth.InvitePrefix) {
					invite = scanner.Text()
					break
				}
			}
			if invite == "" {
				t.Fatalf("sender failed to print invite: %v", scanner.Err())
			}
			path, err := Receive(ctx, ReceiverOptions{Invite: invite, OutputDir: outDir, Yes: true})
			if err != nil {
				t.Fatal(err)
			}
			for scanner.Scan() {
			}
			if err = scanner.Err(); err != nil {
				t.Fatal(err)
			}
			if err = <-sendDone; err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(got, tc.data) {
				t.Fatalf("received %q, %v", got, err)
			}
		})
	}
}

func TestConcurrentReceiversAdmitExactlyOne(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.bin")
	if err := os.WriteFile(source, []byte("one receiver"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pipe, writer := io.Pipe()
	sendDone := make(chan error, 1)
	go func() {
		defer writer.Close()
		sendDone <- Send(ctx, SenderOptions{File: source, Listen: "127.0.0.1:0", Advertise: "127.0.0.1"}, writer)
	}()
	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 1024), auth.MaxInvite+1024)
	var invite string
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), auth.InvitePrefix) {
			invite = scanner.Text()
			break
		}
	}
	if invite == "" {
		t.Fatalf("sender did not offer invite: %v", scanner.Err())
	}
	outDir := filepath.Join(dir, "received")
	if err := os.Mkdir(outDir, 0700); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := Receive(ctx, ReceiverOptions{Invite: invite, OutputDir: outDir, Yes: true})
			results <- err
		}()
	}
	accepted := 0
	for range 2 {
		if err := <-results; err == nil {
			accepted++
		}
	}
	for scanner.Scan() {
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if err := <-sendDone; err != nil {
		t.Fatal(err)
	}
	if accepted != 1 {
		t.Fatalf("accepted %d receivers, want exactly one", accepted)
	}
}

func TestPendingAuthenticationLimit(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.bin")
	if err := os.WriteFile(source, []byte("pending"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	pipe, writer := io.Pipe()
	sendDone := make(chan error, 1)
	go func() {
		defer writer.Close()
		sendDone <- Send(ctx, SenderOptions{File: source, Listen: "127.0.0.1:0", Advertise: "127.0.0.1"}, writer)
	}()
	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 1024), auth.MaxInvite+1024)
	invite := ""
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), auth.InvitePrefix) {
			invite = scanner.Text()
			break
		}
	}
	if invite == "" {
		cancel()
		t.Fatalf("sender did not offer invite: %v", scanner.Err())
	}
	defer func() { cancel(); _ = pipe.Close(); <-sendDone }()
	inv, err := auth.Decode(invite)
	if err != nil {
		t.Fatal(err)
	}
	tlsConfig, err := auth.ClientTLS(inv)
	if err != nil {
		t.Fatal(err)
	}
	type pending struct {
		conn   *quictransport.Conn
		stream *quictransport.ControlStream
	}
	clients := make([]pending, 0, 5)
	for i := 0; i < 4; i++ {
		conn, e := quictransport.Dial(ctx, inv.Endpoint, tlsConfig)
		if e != nil {
			t.Fatalf("connect pending client %d: %v", i, e)
		}
		stream, e := conn.OpenControl(ctx)
		if e != nil {
			t.Fatalf("open pending stream %d: %v", i, e)
		}
		clients = append(clients, pending{conn, stream})
		if _, e = stream.Write([]byte{0, 0}); e != nil {
			t.Fatalf("hold partial frame %d: %v", i, e)
		}
	}
	time.Sleep(100 * time.Millisecond)
	fifth, err := quictransport.Dial(ctx, inv.Endpoint, tlsConfig)
	if err != nil {
		t.Fatalf("fifth QUIC handshake should complete before pending admission: %v", err)
	}
	fifthStream, err := fifth.OpenControl(ctx)
	if err != nil {
		t.Fatalf("open fifth control stream: %v", err)
	}
	_ = fifthStream.SetReadDeadline(time.Now().Add(2 * time.Second))
	var probe [1]byte
	if _, err = fifthStream.Read(probe[:]); err == nil {
		t.Fatal("fifth pending authentication connection remained open")
	}
	_ = fifth.CloseWithError(quictransport.ErrorInvalidSession, "test complete")
	// Complete each held frame with invalid credentials so its bounded worker exits.
	payload := []byte(`{"version":1,"type":"authenticate","session":"invalid","secret":"invalid"}`)
	frame := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(frame[:4], uint32(len(payload)))
	copy(frame[4:], payload)
	for i, client := range clients {
		if _, err = client.stream.Write(frame[2:]); err != nil {
			t.Fatalf("release pending client %d: %v", i, err)
		}
		_ = client.conn.CloseWithError(quictransport.ErrorAuthentication, "test complete")
	}
}

func TestSlowAuthenticationPeersTimeOutAndReleasePendingSlots(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dir := t.TempDir()
	source := filepath.Join(dir, "source.bin")
	if err := os.WriteFile(source, []byte("slot released"), 0600); err != nil {
		t.Fatal(err)
	}
	pipe, writer := io.Pipe()
	sendDone := make(chan error, 1)
	go func() {
		defer writer.Close()
		sendDone <- Send(ctx, SenderOptions{File: source, Listen: "127.0.0.1:0", Advertise: "127.0.0.1"}, writer)
	}()
	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 1024), auth.MaxInvite+1024)
	var token string
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), auth.InvitePrefix) {
			token = scanner.Text()
			break
		}
	}
	if token == "" {
		t.Fatalf("sender did not offer invite: %v", scanner.Err())
	}
	invite, err := auth.Decode(token)
	if err != nil {
		t.Fatal(err)
	}
	tlsConfig, err := auth.ClientTLS(invite)
	if err != nil {
		t.Fatal(err)
	}
	type slowPeer struct {
		conn   *quictransport.Conn
		stream *quictransport.ControlStream
	}
	peers := make([]slowPeer, 0, 4)
	for i := 0; i < 4; i++ {
		conn, e := quictransport.Dial(ctx, invite.Endpoint, tlsConfig)
		if e != nil {
			t.Fatalf("connect slow peer %d: %v", i, e)
		}
		stream, e := conn.OpenControl(ctx)
		if e != nil {
			t.Fatalf("open slow peer %d control stream: %v", i, e)
		}
		if _, e = stream.Write([]byte{0, 0}); e != nil {
			t.Fatalf("write partial authentication frame for peer %d: %v", i, e)
		}
		if e = stream.SetReadDeadline(time.Now().Add(12 * time.Second)); e != nil {
			t.Fatalf("set slow peer %d deadline: %v", i, e)
		}
		peers = append(peers, slowPeer{conn: conn, stream: stream})
	}
	for i, peer := range peers {
		var probe [1]byte
		if _, err = peer.stream.Read(probe[:]); err == nil || quictransport.IsTimeout(err) {
			t.Fatalf("slow peer %d was not closed by the authentication deadline: %v", i, err)
		}
		_ = peer.conn.CloseWithError(quictransport.ErrorInvalidSession, "test complete")
	}
	outDir := filepath.Join(dir, "received")
	if err = os.Mkdir(outDir, 0700); err != nil {
		t.Fatal(err)
	}
	path, err := Receive(ctx, ReceiverOptions{Invite: token, OutputDir: outDir, Yes: true})
	if err != nil {
		t.Fatalf("valid receiver could not proceed after slow peers timed out: %v", err)
	}
	for scanner.Scan() {
	}
	if err = scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if err = <-sendDone; err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "slot released" {
		t.Fatalf("received file = %q, %v", got, err)
	}
}

func TestCanceledSendUnblocksSlowReceiverBackpressure(t *testing.T) {
	watchdog, stopWatchdog := context.WithTimeout(context.Background(), 20*time.Second)
	defer stopWatchdog()
	sendCtx, cancelSend := context.WithCancel(watchdog)
	defer cancelSend()
	dir := t.TempDir()
	source := filepath.Join(dir, "large.bin")
	if err := os.WriteFile(source, bytes.Repeat([]byte("x"), 32<<20), 0600); err != nil {
		t.Fatal(err)
	}
	progress := make(chan uint64, 64)
	pipe, writer := io.Pipe()
	sendDone := make(chan error, 1)
	go func() {
		defer writer.Close()
		sendDone <- Send(sendCtx, SenderOptions{
			File: source, Listen: "127.0.0.1:0", Advertise: "127.0.0.1",
			Progress: func(done, _ uint64) {
				select {
				case progress <- done:
				default:
				}
			},
		}, writer)
	}()
	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 1024), auth.MaxInvite+1024)
	var token string
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), auth.InvitePrefix) {
			token = scanner.Text()
			break
		}
	}
	if token == "" {
		t.Fatalf("sender did not offer invite: %v", scanner.Err())
	}
	invite, err := auth.Decode(token)
	if err != nil {
		t.Fatal(err)
	}
	tlsConfig, err := auth.ClientTLS(invite)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := quictransport.Dial(watchdog, invite.Endpoint, tlsConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseWithError(0, "test complete")
	control, err := conn.OpenControl(watchdog)
	if err != nil {
		t.Fatal(err)
	}
	if err = protocol.WriteMessage(control, protocol.Message{Version: protocol.Version, Type: "authenticate", Session: invite.Session, Secret: invite.Secret}); err != nil {
		t.Fatal(err)
	}
	if message, e := protocol.ReadMessage(bufio.NewReader(control)); e != nil || message.Type != "offer" {
		t.Fatalf("read offer: %#v, %v", message, e)
	}
	if err = protocol.WriteMessage(control, protocol.Message{Version: protocol.Version, Type: "accept", Session: invite.Session, ResumeBitmap: make([]byte, 4)}); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.AcceptData(watchdog); err != nil {
		t.Fatal(err)
	}
	// Leave the receive stream unread to exercise QUIC flow control.
	var lastProgress uint64
	select {
	case lastProgress = <-progress:
		if lastProgress == 0 {
			t.Fatal("sender reported zero progress after data-stream acceptance")
		}
	case <-watchdog.Done():
		t.Fatalf("sender made no progress before backpressure: %v", watchdog.Err())
	}
	lastAdvance := time.Now()
	for {
		select {
		case current := <-progress:
			if current > lastProgress {
				lastProgress = current
				lastAdvance = time.Now()
			}
			if lastProgress == 32<<20 {
				t.Fatal("sender completed while the receiver left the data stream unread")
			}
		case <-time.After(100 * time.Millisecond):
			if time.Since(lastAdvance) >= 500*time.Millisecond {
				goto stalled
			}
		case <-watchdog.Done():
			t.Fatalf("sender did not reach bounded backpressure: %v", watchdog.Err())
		}
	}
stalled:
	cancelSend()
	select {
	case err = <-sendDone:
		if FailureCategory(err) != FailureCancellation {
			t.Fatalf("canceled backpressured sender failure = %q, want cancellation (error: %v)", FailureCategory(err), err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("sender did not release its blocked QUIC write after cancellation")
	}
	for scanner.Scan() {
	}
	if err = scanner.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestReceiverSizeLimitRejectsBeforeStaging(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.bin")
	if err := os.WriteFile(source, []byte("larger than limit"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pipe, writer := io.Pipe()
	sendDone := make(chan error, 1)
	go func() {
		defer writer.Close()
		sendDone <- Send(ctx, SenderOptions{File: source, Listen: "127.0.0.1:0", Advertise: "127.0.0.1"}, writer)
	}()
	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 1024), auth.MaxInvite+1024)
	invite := ""
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), auth.InvitePrefix) {
			invite = scanner.Text()
			break
		}
	}
	if invite == "" {
		t.Fatalf("sender did not offer invite: %v", scanner.Err())
	}
	outDir := filepath.Join(dir, "received")
	if err := os.Mkdir(outDir, 0700); err != nil {
		t.Fatal(err)
	}
	_, receiveErr := Receive(ctx, ReceiverOptions{Invite: invite, OutputDir: outDir, Yes: true, MaxBytes: 1})
	for scanner.Scan() {
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if err := <-sendDone; err == nil {
		t.Fatal("sender reported success after size rejection")
	}
	if FailureCategory(receiveErr) != FailureResourceLimit {
		t.Fatalf("size limit category = %q, error=%v", FailureCategory(receiveErr), receiveErr)
	}
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("size rejection staged output: %v", entries)
	}
}

func TestCanceledTransferRemovesTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.bin")
	outDir := filepath.Join(dir, "received")
	if err := os.Mkdir(outDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, bytes.Repeat([]byte("x"), 8<<20), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pipe, writer := io.Pipe()
	sendDone := make(chan error, 1)
	go func() {
		defer writer.Close()
		sendDone <- Send(ctx, SenderOptions{File: source, Listen: "127.0.0.1:0", Advertise: "127.0.0.1"}, writer)
	}()
	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 1024), auth.MaxInvite+1024)
	invite := ""
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), auth.InvitePrefix) {
			invite = scanner.Text()
			break
		}
	}
	if invite == "" {
		t.Fatalf("sender did not offer invite: %v", scanner.Err())
	}
	_, receiveErr := Receive(ctx, ReceiverOptions{Invite: invite, OutputDir: outDir, Yes: true, Progress: func(done, _ uint64) {
		if done >= protocol.ChunkSize {
			cancel()
		}
	}})
	for scanner.Scan() {
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if err := <-sendDone; err == nil {
		t.Fatal("sender reported success after cancellation")
	}
	if FailureCategory(receiveErr) != FailureCancellation {
		t.Fatalf("cancel category = %q, error=%v", FailureCategory(receiveErr), receiveErr)
	}
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != resume.StateDirectory {
		t.Fatalf("cancellation did not preserve only receiver resume state: %v", entries)
	}
	stateEntries, err := os.ReadDir(filepath.Join(outDir, resume.StateDirectory))
	if err != nil || len(stateEntries) < 2 {
		t.Fatalf("cancellation did not preserve resumable journal and bytes: entries=%v err=%v", stateEntries, err)
	}
}

func TestPeerDisconnectDuringChunkCleansStagingFile(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	dir := t.TempDir()
	outDir := filepath.Join(dir, "received")
	if err := os.Mkdir(outDir, 0700); err != nil {
		t.Fatal(err)
	}
	invite, certificate, err := auth.New("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener, err := quictransport.Listen("127.0.0.1:0", certificate)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	invite.Endpoint = listener.Addr().String()
	token, err := auth.Encode(invite)
	if err != nil {
		t.Fatal(err)
	}
	type receiveResult struct {
		path string
		err  error
	}
	received := make(chan receiveResult, 1)
	go func() {
		path, err := Receive(ctx, ReceiverOptions{Invite: token, OutputDir: outDir, Yes: true})
		received <- receiveResult{path: path, err: err}
	}()
	conn, err := listener.Accept(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseWithError(0, "test complete")
	control, err := conn.AcceptControl(ctx)
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(control)
	message, err := protocol.ReadMessage(reader)
	if err != nil || message.Type != "authenticate" || message.Session != invite.Session {
		t.Fatalf("read authentication message = %#v, %v", message, err)
	}
	payload := []byte("partial payload")
	digest := sha256.Sum256(payload)
	descriptor := protocol.Descriptor{TransferID: "0123456789abcdef0123456789abcdef", Name: "abrupt.bin", Size: uint64(len(payload)), ChunkSize: protocol.ChunkSize, SHA256: digest}
	if err = protocol.WriteMessage(control, protocol.Message{Version: protocol.Version, Type: "offer", Descriptor: &descriptor}); err != nil {
		t.Fatal(err)
	}
	decision, err := protocol.ReadMessage(reader)
	if err != nil || decision.Type != "accept" || decision.Session != invite.Session || len(decision.ResumeBitmap) != 1 {
		t.Fatalf("read acceptance = %#v, %v", decision, err)
	}
	stream, err := conn.OpenData(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = protocol.WriteChunkHeader(stream, 0, 0, uint32(len(payload)), digest); err != nil {
		t.Fatal(err)
	}
	if _, err = stream.Write(payload[:3]); err != nil {
		t.Fatal(err)
	}
	if err = conn.CloseWithError(1, "simulated peer disconnect"); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-received:
		if result.err == nil {
			t.Fatalf("receiver published %s after an incomplete chunk", result.path)
		}
	case <-ctx.Done():
		t.Fatalf("receiver did not stop after peer disconnect: %v", ctx.Err())
	}
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != resume.StateDirectory {
		t.Fatalf("peer disconnect did not preserve only resume state: %v", entries)
	}
}

func TestReceiverRejectsControlFilenameBeforeDisplayingOffer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	outputDir := t.TempDir()
	invite, certificate, err := auth.New("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener, err := quictransport.Listen("127.0.0.1:0", certificate)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	invite.Endpoint = listener.Addr().String()
	token, err := auth.Encode(invite)
	if err != nil {
		t.Fatal(err)
	}
	var status bytes.Buffer
	type receiveResult struct{ err error }
	result := make(chan receiveResult, 1)
	go func() {
		_, receiveErr := Receive(ctx, ReceiverOptions{Invite: token, OutputDir: outputDir, Yes: true, Status: &status})
		result <- receiveResult{err: receiveErr}
	}()

	conn, err := listener.Accept(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseWithError(0, "test complete")
	control, err := conn.AcceptControl(ctx)
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(control)
	message, err := protocol.ReadMessage(reader)
	if err != nil || message.Type != "authenticate" || message.Session != invite.Session || !auth.VerifySecret(invite.Secret, message.Secret) {
		t.Fatalf("read authentication message = %#v, %v", message, err)
	}
	descriptor := protocol.Descriptor{
		TransferID: "0123456789abcdef0123456789abcdef",
		Name:       "bad\x1b[31m.txt",
		Size:       1,
		ChunkSize:  protocol.ChunkSize,
		SHA256:     sha256.Sum256([]byte("x")),
	}
	if err = protocol.WriteMessage(control, protocol.Message{Version: protocol.Version, Type: "offer", Descriptor: &descriptor}); err != nil {
		t.Fatal(err)
	}
	select {
	case received := <-result:
		if received.err == nil || FailureCategory(received.err) != FailureProtocol {
			t.Fatalf("invalid filename result = %v, want protocol rejection", received.err)
		}
	case <-ctx.Done():
		t.Fatalf("receiver did not reject control filename: %v", ctx.Err())
	}
	if status.Len() != 0 {
		t.Fatalf("receiver displayed untrusted filename before rejection: %q", status.String())
	}
}

func TestSendCancellationUnblocksIdleReceiver(t *testing.T) {
	watchdog, stopWatchdog := context.WithTimeout(context.Background(), 15*time.Second)
	defer stopWatchdog()
	sendCtx, cancelSend := context.WithCancel(watchdog)
	defer cancelSend()
	pipe, writer := io.Pipe()
	source := filepath.Join(t.TempDir(), "source.bin")
	if err := os.WriteFile(source, []byte("waiting for receiver"), 0600); err != nil {
		t.Fatal(err)
	}
	sendDone := make(chan error, 1)
	go func() {
		defer writer.Close()
		sendDone <- Send(sendCtx, SenderOptions{File: source, Listen: "127.0.0.1:0", Advertise: "127.0.0.1"}, writer)
	}()
	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 1024), auth.MaxInvite+1024)
	var token string
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), auth.InvitePrefix) {
			token = scanner.Text()
			break
		}
	}
	if token == "" {
		t.Fatalf("sender did not offer invite: %v", scanner.Err())
	}
	invite, err := auth.Decode(token)
	if err != nil {
		t.Fatal(err)
	}
	tlsConfig, err := auth.ClientTLS(invite)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := quictransport.Dial(watchdog, invite.Endpoint, tlsConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseWithError(0, "test complete")
	control, err := conn.OpenControl(watchdog)
	if err != nil {
		t.Fatal(err)
	}
	if err = protocol.WriteMessage(control, protocol.Message{Version: protocol.Version, Type: "authenticate", Session: invite.Session, Secret: invite.Secret}); err != nil {
		t.Fatal(err)
	}
	if message, e := protocol.ReadMessage(bufio.NewReader(control)); e != nil || message.Type != "offer" {
		t.Fatalf("read offer before idling: %#v, %v", message, e)
	}
	cancelSend()
	select {
	case err = <-sendDone:
		if FailureCategory(err) != FailureCancellation {
			t.Fatalf("canceled sender failure = %q, want cancellation (error: %v)", FailureCategory(err), err)
		}
	case <-time.After(2 * time.Second):
		_ = conn.CloseWithError(0, "release stalled sender")
		select {
		case <-sendDone:
		case <-watchdog.Done():
		}
		t.Fatal("sender remained blocked after its context was canceled")
	}
}

func TestReceiveCancellationUnblocksIdleSender(t *testing.T) {
	watchdog, stopWatchdog := context.WithTimeout(context.Background(), 15*time.Second)
	defer stopWatchdog()
	receiveCtx, cancelReceive := context.WithCancel(watchdog)
	defer cancelReceive()
	invite, certificate, err := auth.New("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener, err := quictransport.Listen("127.0.0.1:0", certificate)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	invite.Endpoint = listener.Addr().String()
	token, err := auth.Encode(invite)
	if err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()
	type receiveResult struct{ err error }
	received := make(chan receiveResult, 1)
	go func() {
		_, err := Receive(receiveCtx, ReceiverOptions{Invite: token, OutputDir: outDir, Yes: true})
		received <- receiveResult{err: err}
	}()
	conn, err := listener.Accept(watchdog)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseWithError(0, "test complete")
	control, err := conn.AcceptControl(watchdog)
	if err != nil {
		t.Fatal(err)
	}
	message, err := protocol.ReadMessage(bufio.NewReader(control))
	if err != nil || message.Type != "authenticate" || message.Session != invite.Session {
		t.Fatalf("read authentication message = %#v, %v", message, err)
	}
	cancelReceive()
	select {
	case result := <-received:
		if FailureCategory(result.err) != FailureCancellation {
			t.Fatalf("canceled receiver failure = %q, want cancellation (error: %v)", FailureCategory(result.err), result.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("receiver remained blocked after its context was canceled")
	}
}

func TestReceiveRejectsWrongCapability(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	dir := t.TempDir()
	source := filepath.Join(dir, "source.bin")
	if err := os.WriteFile(source, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	pipe, writer := io.Pipe()
	sendDone := make(chan error, 1)
	go func() {
		defer writer.Close()
		sendDone <- Send(ctx, SenderOptions{File: source, Listen: "127.0.0.1:0", Advertise: "127.0.0.1"}, writer)
	}()
	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 1024), auth.MaxInvite+1024)
	token := ""
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), auth.InvitePrefix) {
			token = scanner.Text()
			break
		}
	}
	if token == "" {
		t.Fatalf("sender did not offer invite: %v", scanner.Err())
	}
	inv, err := auth.Decode(token)
	if err != nil {
		t.Fatal(err)
	}
	secret, _ := base64.RawURLEncoding.DecodeString(inv.Secret)
	secret[0] ^= 1
	inv.Secret = base64.RawURLEncoding.EncodeToString(secret)
	altered, err := auth.Encode(inv)
	if err != nil {
		t.Fatal(err)
	}
	_, receiveErr := Receive(ctx, ReceiverOptions{Invite: altered, OutputDir: filepath.Join(dir, "receiver"), Yes: true})
	cancel()
	_ = pipe.Close()
	<-sendDone
	if receiveErr == nil {
		t.Fatal("wrong capability was accepted")
	} else if FailureCategory(receiveErr) != FailureAuthentication {
		t.Fatalf("wrong capability category = %q, want authentication (%v)", FailureCategory(receiveErr), receiveErr)
	}
}

func TestSourceMutationFailsWithoutPublishing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dir := t.TempDir()
	source := filepath.Join(dir, "source.bin")
	receiverDir := filepath.Join(dir, "received")
	_ = os.Mkdir(receiverDir, 0700)
	original := bytes.Repeat([]byte("a"), 2<<20)
	if err := os.WriteFile(source, original, 0600); err != nil {
		t.Fatal(err)
	}
	pipe, writer := io.Pipe()
	sendDone := make(chan error, 1)
	go func() {
		defer writer.Close()
		sendDone <- Send(ctx, SenderOptions{File: source, Listen: "127.0.0.1:0", Advertise: "127.0.0.1"}, writer)
	}()
	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 1024), auth.MaxInvite+1024)
	token := ""
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), auth.InvitePrefix) {
			token = scanner.Text()
			break
		}
	}
	if token == "" {
		t.Fatalf("sender did not offer invite: %v", scanner.Err())
	}
	changed := bytes.Repeat([]byte("b"), len(original))
	if err := os.WriteFile(source, changed, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Receive(ctx, ReceiverOptions{Invite: token, OutputDir: receiverDir, Yes: true}); err == nil {
		t.Fatal("mutated source passed whole-file verification")
	}
	if _, err := os.Stat(filepath.Join(receiverDir, "source.bin")); !os.IsNotExist(err) {
		t.Fatalf("unverified file was published: %v", err)
	}
	if err := <-sendDone; err == nil || !strings.Contains(err.Error(), "source changed") {
		t.Fatalf("sender did not classify source mutation: %v", err)
	}
}

func TestReceiveRejectsExpiredInviteBeforeDial(t *testing.T) {
	i, _, err := auth.New("127.0.0.1:9")
	if err != nil {
		t.Fatal(err)
	}
	i.Expires = time.Now().Add(-time.Second)
	token, err := auth.Encode(i)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Receive(context.Background(), ReceiverOptions{Invite: token, OutputDir: t.TempDir(), Yes: true}); err == nil {
		t.Fatal("expired invite accepted")
	}
}

func TestReceiveRejectsCertificateSubstitution(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dir := t.TempDir()
	source := filepath.Join(dir, "source.bin")
	if err := os.WriteFile(source, []byte("certificate test"), 0600); err != nil {
		t.Fatal(err)
	}
	pipe, writer := io.Pipe()
	sendDone := make(chan error, 1)
	go func() {
		defer writer.Close()
		sendDone <- Send(ctx, SenderOptions{File: source, Listen: "127.0.0.1:0", Advertise: "127.0.0.1"}, writer)
	}()
	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 1024), auth.MaxInvite+1024)
	token := ""
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), auth.InvitePrefix) {
			token = scanner.Text()
			break
		}
	}
	if token == "" {
		t.Fatalf("sender did not offer invite: %v", scanner.Err())
	}
	inv, err := auth.Decode(token)
	if err != nil {
		t.Fatal(err)
	}
	rogue, _, err := auth.New(inv.Endpoint)
	if err != nil {
		t.Fatal(err)
	}
	inv.Certificate = rogue.Certificate
	altered, err := auth.Encode(inv)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Receive(ctx, ReceiverOptions{Invite: altered, OutputDir: filepath.Join(dir, "receiver"), Yes: true}); err == nil {
		t.Fatal("certificate substitution was accepted")
	}
	cancel()
	_ = pipe.Close()
	<-sendDone
}

func TestReceiveChunksRejectsMalformedAndCorruptData(t *testing.T) {
	data := []byte("abcdef")
	fileDigest := sha256.Sum256(data)
	d := protocol.Descriptor{TransferID: "id", Name: "f", Size: uint64(len(data)), ChunkSize: protocol.ChunkSize, SHA256: fileDigest}
	makeChunk := func(index uint64, payload []byte) []byte {
		var b bytes.Buffer
		chunkDigest := sha256.Sum256(payload)
		if err := protocol.WriteChunkHeader(&b, index, 0, uint32(len(payload)), chunkDigest); err != nil {
			t.Fatal(err)
		}
		b.Write(payload)
		return b.Bytes()
	}
	var out bytes.Buffer
	got, err := receiveChunks(context.Background(), bytes.NewReader(makeChunk(0, data)), &out, d, nil)
	if err != nil || got != fileDigest {
		t.Fatalf("valid chunk rejected: %v", err)
	}
	if _, err = receiveChunks(context.Background(), bytes.NewReader(append(makeChunk(0, data), 0)), &out, d, nil); err == nil {
		t.Fatal("trailing data after declared file length was accepted")
	}
	corrupt := makeChunk(0, data)
	corrupt[len(corrupt)-1] ^= 1
	out.Reset()
	if _, err = receiveChunks(context.Background(), bytes.NewReader(corrupt), &out, d, nil); err == nil {
		t.Fatal("corrupt chunk accepted")
	} else if FailureCategory(err) != FailureIntegrity {
		t.Fatalf("corrupt chunk category = %q, want integrity", FailureCategory(err))
	}
	if _, err = receiveChunks(context.Background(), bytes.NewReader(makeChunk(1, data)), &out, d, nil); err == nil {
		t.Fatal("out-of-order chunk accepted")
	}
	var oversized [protocol.ChunkHeaderSize]byte
	binary.BigEndian.PutUint32(oversized[16:20], protocol.ChunkSize+1)
	if _, err = receiveChunks(context.Background(), bytes.NewReader(oversized[:]), io.Discard, d, nil); err == nil {
		t.Fatal("oversized chunk accepted")
	}
	if _, err = receiveChunks(context.Background(), bytes.NewReader(makeChunk(0, data[:2])), io.Discard, d, nil); err == nil {
		t.Fatal("short chunk accepted")
	}
}

func TestValidateResumeBitmapRequiresBoundedCommittedPrefix(t *testing.T) {
	for _, test := range []struct {
		name   string
		bitmap []byte
		chunks uint64
		valid  bool
		want   uint64
	}{
		{name: "empty file", bitmap: []byte{}, chunks: 0, valid: true},
		{name: "prefix", bitmap: []byte{0b00000111}, chunks: 8, valid: true, want: 3},
		{name: "hole", bitmap: []byte{0b00000101}, chunks: 8},
		{name: "high bit", bitmap: []byte{0b10000000}, chunks: 7},
		{name: "wrong length", bitmap: []byte{1}, chunks: 9},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := validateResumeBitmap(test.bitmap, test.chunks)
			if test.valid && err != nil {
				t.Fatalf("valid bitmap rejected: %v", err)
			}
			if !test.valid && err == nil {
				t.Fatal("invalid bitmap accepted")
			}
			if test.valid && got != test.want {
				t.Fatalf("committed count = %d, want %d", got, test.want)
			}
		})
	}
}

func TestReceiveResumeChunksRejectsMissingDuplicateDescendingAndCorruptChunks(t *testing.T) {
	chunk0 := bytes.Repeat([]byte("a"), protocol.ChunkSize)
	chunk1 := bytes.Repeat([]byte("b"), protocol.ChunkSize)
	complete := append(append([]byte(nil), chunk0...), chunk1...)
	d := protocol.Descriptor{
		TransferID: "fedcba9876543210fedcba9876543210",
		Name:       "resume-stream.bin",
		Size:       uint64(len(complete)),
		ChunkSize:  protocol.ChunkSize,
		SHA256:     sha256.Sum256(complete),
	}
	frame := func(index, offset uint64, payload []byte, digest [sha256.Size]byte) []byte {
		var b bytes.Buffer
		if err := protocol.WriteChunkHeader(&b, index, offset, uint32(len(payload)), digest); err != nil {
			t.Fatal(err)
		}
		_, _ = b.Write(payload)
		return b.Bytes()
	}
	valid0 := frame(0, 0, chunk0, sha256.Sum256(chunk0))
	valid1 := frame(1, protocol.ChunkSize, chunk1, sha256.Sum256(chunk1))
	badDigest := sha256.Sum256([]byte("wrong"))
	cases := []struct {
		name        string
		before      bool
		stream      []byte
		wantFailure FailureKind
	}{
		{name: "missing chunk", stream: nil, wantFailure: FailureProtocol},
		{name: "missing final chunk", stream: valid0, wantFailure: FailureProtocol},
		{name: "duplicate chunk", stream: append(append([]byte(nil), valid0...), valid0...), wantFailure: FailureProtocol},
		{name: "descending chunk", stream: valid1, wantFailure: FailureProtocol},
		{name: "already committed chunk", before: true, stream: valid0, wantFailure: FailureProtocol},
		{name: "corrupt chunk digest", stream: frame(0, 0, chunk0, badDigest), wantFailure: FailureIntegrity},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			store, err := resume.Open(t.TempDir(), d, d.Size)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			if test.before {
				if err = store.CommitChunk(0, chunk0); err != nil {
					t.Fatal(err)
				}
			}
			err = receiveResumeChunks(context.Background(), bytes.NewReader(test.stream), store, d, nil)
			if FailureCategory(err) != test.wantFailure {
				t.Fatalf("failure category = %q, want %q (err=%v)", FailureCategory(err), test.wantFailure, err)
			}
		})
	}
}

type failingChunkWriter struct{}

func (failingChunkWriter) Write([]byte) (int, error) { return 0, io.ErrShortWrite }

func TestReceiveChunksClassifiesDestinationWriteFailure(t *testing.T) {
	data := []byte("disk full")
	digest := sha256.Sum256(data)
	d := protocol.Descriptor{TransferID: "transfer", Name: "file.bin", Size: uint64(len(data)), ChunkSize: protocol.ChunkSize, SHA256: digest}
	var stream bytes.Buffer
	if err := protocol.WriteChunkHeader(&stream, 0, 0, uint32(len(data)), digest); err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Write(data); err != nil {
		t.Fatal(err)
	}
	_, err := receiveChunks(context.Background(), &stream, failingChunkWriter{}, d, nil)
	if FailureCategory(err) != FailureFilesystem {
		t.Fatalf("destination write failure category = %q, want filesystem (error: %v)", FailureCategory(err), err)
	}
}

func BenchmarkHashOneMiB(b *testing.B) {
	data := bytes.Repeat([]byte("x"), 1<<20)
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		_ = sha256.Sum256(data)
	}
}
