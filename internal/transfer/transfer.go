package transfer

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"dropmesh/internal/auth"
	"dropmesh/internal/filesafe"
	"dropmesh/internal/protocol"
	"dropmesh/internal/resume"
	quictransport "dropmesh/internal/transport/quic"
)

const DefaultMaxBytes uint64 = 100 << 30

type Progress func(done, total uint64)
type SenderOptions struct {
	File       string
	Listen     string
	Advertise  string
	TransferID string
	Progress   Progress
	Status     io.Writer
}
type ReceiverOptions struct {
	Invite       string
	InviteReader io.Reader
	OutputDir    string
	Yes          bool
	MaxBytes     uint64
	Progress     Progress
	Status       io.Writer
}

func Send(ctx context.Context, o SenderOptions, out io.Writer) error {
	input, err := openRegularFile(o.File)
	if err != nil {
		return fail(FailureFilesystem, err)
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return fail(FailureFilesystem, fmt.Errorf("inspect source file: %w", err))
	}
	transferID := o.TransferID
	if transferID == "" {
		transferID, err = resume.NewID()
		if err != nil {
			return fail(FailureFilesystem, fmt.Errorf("create transfer identifier: %w", err))
		}
	} else if err = resume.ValidateID(transferID); err != nil {
		return fail(FailureProtocol, err)
	}
	if chunkCount(uint64(info.Size())) > resume.MaxChunks {
		return fail(FailureResourceLimit, fmt.Errorf("transfer requires too many resumable chunks"))
	}
	var digest [sha256.Size]byte
	prehash := sha256.New()
	if _, err = io.Copy(prehash, input); err != nil {
		return fail(FailureFilesystem, fmt.Errorf("hash source file: %w", err))
	}
	copy(digest[:], prehash.Sum(nil))
	if _, err = input.Seek(0, io.SeekStart); err != nil {
		return fail(FailureFilesystem, fmt.Errorf("rewind source file: %w", err))
	}
	listen := o.Listen
	if listen == "" {
		listen = "0.0.0.0:0"
	}
	placeholder := net.JoinHostPort("0.0.0.0", "0")
	inv, cert, err := auth.New(placeholder)
	if err != nil {
		return fail(FailureAuthentication, fmt.Errorf("prepare invite: %w", err))
	}
	listener, err := quictransport.Listen(listen, cert)
	if err != nil {
		return fail(FailureTransport, fmt.Errorf("listen for receiver: %w", err))
	}
	defer listener.Close()
	sessionCtx, sessionCancel := context.WithDeadline(ctx, inv.Expires)
	defer sessionCancel()
	addr := listener.Addr().(*net.UDPAddr)
	advertise := o.Advertise
	if advertise == "" {
		advertise, err = chooseAdvertiseIP(listen)
		if err != nil {
			return fail(FailureTransport, err)
		}
	}
	inv.Endpoint = net.JoinHostPort(advertise, strconv.Itoa(addr.Port))
	invite, err := auth.Encode(inv)
	if err != nil {
		return err
	}
	status := o.Status
	if status == nil {
		status = out
	}
	if _, err = fmt.Fprintf(status, "File: %s\nSize: %d bytes\nTransfer ID: %s\n\nWaiting for receiver (invite expires in 15 minutes)...\n", filepath.Base(o.File), info.Size(), transferID); err != nil {
		return fmt.Errorf("display sender status: %w", err)
	}
	if _, err = fmt.Fprintln(out, invite); err != nil {
		return fmt.Errorf("display invite: %w", err)
	}
	conn, ctrl, reader, err := acceptOne(sessionCtx, listener, inv)
	if err != nil {
		return classifyOperation(ctx, err, FailureAuthentication)
	}
	stopOnCancel := context.AfterFunc(ctx, func() { _ = conn.CloseWithError(0, "transfer canceled") })
	defer stopOnCancel()
	defer conn.CloseWithError(0, "transfer finished")
	defer ctrl.CancelRead(0)
	name := filepath.Base(o.File)
	if err = protocol.WriteMessage(ctrl, protocol.Message{Version: protocol.Version, Type: "offer", Descriptor: &protocol.Descriptor{TransferID: transferID, Name: name, Size: uint64(info.Size()), ChunkSize: protocol.ChunkSize, SHA256: digest}}); err != nil {
		return fail(FailureProtocol, err)
	}
	response, err := protocol.ReadMessage(reader)
	if err != nil {
		return classifyOperation(ctx, fmt.Errorf("read receiver decision: %w", err), FailureProtocol)
	}
	if response.Type != "accept" || response.Session != inv.Session {
		if response.Type == "reject" {
			return fail(FailureProtocol, fmt.Errorf("receiver rejected the transfer"))
		}
		return fail(FailureProtocol, fmt.Errorf("invalid receiver decision"))
	}
	committed, err := validateResumeBitmap(response.ResumeBitmap, chunkCount(uint64(info.Size())))
	if err != nil {
		return fail(FailureProtocol, fmt.Errorf("invalid receiver resume state: %w", err))
	}
	data, err := conn.OpenData(ctx)
	if err != nil {
		return classifyOperation(ctx, fmt.Errorf("open data stream: %w", err), FailureTransport)
	}
	started := time.Now()
	copied, actual, err := sendResumeChunks(input, data, uint64(info.Size()), committed, o.Progress)
	if err != nil {
		data.CancelWrite(2)
		return classifyOperation(ctx, err, FailureTransport)
	}
	if err = data.Close(); err != nil {
		return fmt.Errorf("finish data stream: %w", err)
	}
	if subtle.ConstantTimeCompare(actual[:], digest[:]) != 1 {
		return fail(FailureIntegrity, fmt.Errorf("source changed during transfer; receiver will reject the file"))
	}
	complete, err := protocol.ReadMessage(reader)
	if err != nil {
		return classifyOperation(ctx, fmt.Errorf("transfer outcome is uncertain; receiver completion acknowledgement was not received: %w", err), FailureTransport)
	}
	if complete.Type != "complete" || complete.Session != inv.Session {
		return fail(FailureProtocol, fmt.Errorf("invalid completion acknowledgement"))
	}
	if err = ctrl.Close(); err != nil {
		return fail(FailureTransport, fmt.Errorf("close control stream after completion: %w", err))
	}
	if n, e := io.Copy(io.Discard, reader); e != nil || n != 0 {
		if e != nil && !quictransport.IsGracefulClose(e) {
			return fail(FailureTransport, fmt.Errorf("completion acknowledgement outcome is uncertain: %w", e))
		}
		if n != 0 {
			return fail(FailureProtocol, fmt.Errorf("unexpected control data after completion acknowledgement"))
		}
	}
	seconds := time.Since(started).Seconds()
	if seconds <= 0 {
		seconds = 0.000001
	}
	_, err = fmt.Fprintf(status, "\nTransfer completed.\nRoute: DIRECT\nTransport: QUIC\nEncryption: TLS 1.3\nTransferred: %d bytes\nElapsed: %s\nThroughput: %.2f MiB/s\n", copied, time.Since(started).Round(time.Millisecond), float64(copied)/seconds/1048576)
	return err
}

func Receive(ctx context.Context, o ReceiverOptions) (string, error) {
	raw := o.Invite
	if o.InviteReader != nil {
		b, e := io.ReadAll(io.LimitReader(o.InviteReader, auth.MaxInvite+1))
		if e != nil {
			return "", fmt.Errorf("read invite: %w", e)
		}
		if len(b) > auth.MaxInvite {
			return "", fail(FailureResourceLimit, fmt.Errorf("invite exceeds size limit"))
		}
		raw = string(b)
	}
	inv, err := auth.Decode(raw)
	if err != nil {
		return "", fail(FailureAuthentication, err)
	}
	if o.OutputDir == "" {
		o.OutputDir = "."
	}
	if o.MaxBytes == 0 {
		o.MaxBytes = DefaultMaxBytes
	}
	tlsConfig, err := auth.ClientTLS(inv)
	if err != nil {
		return "", fail(FailureAuthentication, err)
	}
	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := quictransport.Dial(connectCtx, inv.Endpoint, tlsConfig)
	if err != nil {
		return "", classifyOperation(ctx, fmt.Errorf("connect to sender: %w", err), FailureTransport)
	}
	stopOnCancel := context.AfterFunc(ctx, func() { _ = conn.CloseWithError(0, "transfer canceled") })
	defer stopOnCancel()
	defer conn.CloseWithError(0, "receive finished")
	ctrl, err := conn.OpenControl(connectCtx)
	if err != nil {
		return "", classifyOperation(ctx, fmt.Errorf("open control stream: %w", err), FailureTransport)
	}
	defer ctrl.CancelRead(0)
	if err = protocol.WriteMessage(ctrl, protocol.Message{Version: protocol.Version, Type: "authenticate", Session: inv.Session, Secret: inv.Secret}); err != nil {
		return "", fail(FailureProtocol, err)
	}
	reader := bufio.NewReader(ctrl)
	offer, err := protocol.ReadMessage(reader)
	if err != nil {
		if quictransport.IsAuthenticationFailure(err) {
			return "", fail(FailureAuthentication, fmt.Errorf("sender rejected authentication"))
		}
		return "", classifyOperation(ctx, fmt.Errorf("read transfer offer: %w", err), FailureProtocol)
	}
	if offer.Type != "offer" || offer.Session != "" || offer.Descriptor == nil {
		return "", fail(FailureProtocol, fmt.Errorf("invalid transfer offer"))
	}
	d := *offer.Descriptor
	if err = resume.ValidateID(d.TransferID); err != nil {
		return "", fail(FailureProtocol, fmt.Errorf("invalid transfer identifier in offer"))
	}
	if d.Size > o.MaxBytes {
		return "", fail(FailureResourceLimit, fmt.Errorf("transfer exceeds configured size limit"))
	}
	if err = filesafe.ValidateDescriptor(d, o.MaxBytes); err != nil {
		_ = protocol.WriteMessage(ctrl, protocol.Message{Version: protocol.Version, Type: "reject"})
		return "", fail(FailureProtocol, err)
	}
	if o.Status != nil {
		if _, err = fmt.Fprintf(o.Status, "Incoming file: %s\nSize: %d bytes\nTransfer ID: %s\n", d.Name, d.Size, d.TransferID); err != nil {
			return "", fmt.Errorf("display incoming file offer: %w", err)
		}
	}
	if !o.Yes {
		ok, e := confirmOffer(d)
		if e != nil {
			return "", classifyOperation(ctx, e, FailureCancellation)
		}
		if !ok {
			_ = protocol.WriteMessage(ctrl, protocol.Message{Version: protocol.Version, Type: "reject"})
			return "", fail(FailureCancellation, context.Canceled)
		}
	}
	store, err := resume.Open(o.OutputDir, d, o.MaxBytes)
	if err != nil {
		_ = protocol.WriteMessage(ctrl, protocol.Message{Version: protocol.Version, Type: "reject"})
		if errors.Is(err, resume.ErrStateMismatch) {
			return "", fail(FailureProtocol, err)
		}
		return "", fail(FailureFilesystem, err)
	}
	defer store.Close()
	if err = protocol.WriteMessage(ctrl, protocol.Message{Version: protocol.Version, Type: "accept", Session: inv.Session, ResumeBitmap: store.Bitmap()}); err != nil {
		return "", fail(FailureProtocol, err)
	}
	stream, err := conn.AcceptData(ctx)
	if err != nil {
		return "", classifyOperation(ctx, fmt.Errorf("accept file stream: %w", err), FailureTransport)
	}
	err = receiveResumeChunks(ctx, stream, store, d, o.Progress)
	if err != nil {
		stream.CancelRead(3)
		if FailureCategory(err) != "" {
			return "", err
		}
		return "", classifyOperation(ctx, err, FailureProtocol)
	}
	path, err := store.VerifyAndPublish(d.Name)
	if err != nil {
		if errors.Is(err, resume.ErrIntegrity) {
			return "", fail(FailureIntegrity, err)
		}
		return "", fail(FailureFilesystem, err)
	}
	if err = protocol.WriteMessage(ctrl, protocol.Message{Version: protocol.Version, Type: "complete", Session: inv.Session}); err != nil {
		return "", fail(FailureProtocol, fmt.Errorf("file was verified and published, but completion acknowledgement failed: %w", err))
	}
	if err = ctrl.Close(); err != nil {
		return "", fail(FailureTransport, fmt.Errorf("file was verified and published, but control stream closure failed: %w", err))
	}
	if _, err = io.Copy(io.Discard, ctrl); err != nil && !quictransport.IsGracefulClose(err) {
		return "", fail(FailureTransport, fmt.Errorf("file was verified and published, but sender shutdown failed: %w", err))
	}
	return filepath.Join(o.OutputDir, path), nil
}

func chunkCount(size uint64) uint64 {
	if size == 0 {
		return 0
	}
	return (size-1)/protocol.ChunkSize + 1
}

func validateResumeBitmap(bitmap []byte, chunks uint64) (uint64, error) {
	want := int((chunks + 7) / 8)
	if len(bitmap) != want {
		return 0, fmt.Errorf("resume bitmap length %d, want %d", len(bitmap), want)
	}
	if chunks%8 != 0 && len(bitmap) > 0 {
		used := uint(chunks % 8)
		if bitmap[len(bitmap)-1]&^byte((1<<used)-1) != 0 {
			return 0, fmt.Errorf("resume bitmap has bits beyond the offered file")
		}
	}
	var committed uint64
	for index := uint64(0); index < chunks; index++ {
		set := bitmap[index/8]&(1<<(index%8)) != 0
		if !set {
			for later := index + 1; later < chunks; later++ {
				if bitmap[later/8]&(1<<(later%8)) != 0 {
					return 0, fmt.Errorf("resume bitmap is not a committed prefix")
				}
			}
			break
		}
		committed++
	}
	return committed, nil
}

func sendResumeChunks(src *os.File, dst *quictransport.DataSendStream, size, committed uint64, progress Progress) (uint64, [sha256.Size]byte, error) {
	whole := sha256.New()
	buf := make([]byte, protocol.ChunkSize)
	var sent uint64
	chunks := chunkCount(size)
	skipBytes := committed * protocol.ChunkSize
	if skipBytes > size {
		skipBytes = size
	}
	totalSend := size - skipBytes
	for index := uint64(0); index < chunks; index++ {
		offset := index * protocol.ChunkSize
		length := uint64(protocol.ChunkSize)
		if remain := size - offset; remain < length {
			length = remain
		}
		chunk := buf[:int(length)]
		if _, err := io.ReadFull(src, chunk); err != nil {
			return sent, [sha256.Size]byte{}, fmt.Errorf("read source chunk: %w", err)
		}
		whole.Write(chunk)
		if index < committed {
			continue
		}
		digest := sha256.Sum256(chunk)
		if err := protocol.WriteChunkHeader(dst, index, offset, uint32(length), digest); err != nil {
			return sent, [sha256.Size]byte{}, fmt.Errorf("write chunk header: %w", err)
		}
		if _, err := dst.Write(chunk); err != nil {
			return sent, [sha256.Size]byte{}, fmt.Errorf("write file chunk: %w", err)
		}
		sent += length
		if progress != nil {
			progress(sent, totalSend)
		}
	}
	var actual [sha256.Size]byte
	copy(actual[:], whole.Sum(nil))
	return sent, actual, nil
}

func receiveResumeChunks(ctx context.Context, src io.Reader, store *resume.Store, d protocol.Descriptor, progress Progress) error {
	count := chunkCount(d.Size)
	index := store.CommittedChunks()
	startOffset := index * protocol.ChunkSize
	if startOffset > d.Size {
		startOffset = d.Size
	}
	totalNew := d.Size - startOffset
	var receivedNew uint64
	buf := make([]byte, protocol.ChunkSize)
	if progress != nil {
		progress(0, totalNew)
	}
	for ; index < count; index++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		gotIndex, offset, length, digest, err := protocol.ReadChunkHeader(src)
		if err != nil {
			return fail(FailureProtocol, fmt.Errorf("read chunk header: %w", err))
		}
		wantOffset := index * protocol.ChunkSize
		wantLength := uint64(protocol.ChunkSize)
		if remaining := d.Size - wantOffset; remaining < wantLength {
			wantLength = remaining
		}
		if gotIndex != index || offset != wantOffset || uint64(length) != wantLength || length == 0 || length > protocol.ChunkSize {
			return fail(FailureProtocol, fmt.Errorf("invalid resume chunk sequence or length"))
		}
		payload := buf[:length]
		if _, err = io.ReadFull(src, payload); err != nil {
			return fail(FailureProtocol, fmt.Errorf("read file chunk: %w", err))
		}
		actual := sha256.Sum256(payload)
		if subtle.ConstantTimeCompare(actual[:], digest[:]) != 1 {
			return fail(FailureIntegrity, fmt.Errorf("chunk integrity check failed"))
		}
		if err = store.CommitChunk(index, payload); err != nil {
			return fail(FailureFilesystem, fmt.Errorf("commit received chunk: %w", err))
		}
		receivedNew += wantLength
		if progress != nil {
			progress(receivedNew, totalNew)
		}
	}
	var trailing [1]byte
	n, err := io.ReadFull(src, trailing[:])
	if n != 0 {
		return fail(FailureProtocol, fmt.Errorf("data stream contains an unexpected chunk"))
	}
	if err != io.EOF {
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		return fail(FailureProtocol, fmt.Errorf("data stream did not finish at a chunk boundary: %w", err))
	}
	return nil
}

func receiveChunks(ctx context.Context, src io.Reader, dst io.Writer, d protocol.Descriptor, progress Progress) ([sha256.Size]byte, error) {
	whole := sha256.New()
	buf := make([]byte, protocol.ChunkSize)
	total := uint64(0)
	index := uint64(0)
	for total < d.Size {
		if err := ctx.Err(); err != nil {
			return [sha256.Size]byte{}, err
		}
		gotIndex, offset, n, digest, err := protocol.ReadChunkHeader(src)
		if err != nil {
			return [sha256.Size]byte{}, fmt.Errorf("read chunk header: %w", err)
		}
		remaining := d.Size - total
		expected := uint64(protocol.ChunkSize)
		if remaining < expected {
			expected = remaining
		}
		if gotIndex != index || offset != total || uint64(n) != expected || n == 0 || n > protocol.ChunkSize {
			return [sha256.Size]byte{}, fmt.Errorf("invalid chunk sequence or length")
		}
		chunk := buf[:n]
		if _, err = io.ReadFull(src, chunk); err != nil {
			return [sha256.Size]byte{}, fmt.Errorf("read file chunk: %w", err)
		}
		actual := sha256.Sum256(chunk)
		if subtle.ConstantTimeCompare(actual[:], digest[:]) != 1 {
			return [sha256.Size]byte{}, fail(FailureIntegrity, fmt.Errorf("chunk integrity check failed"))
		}
		if _, err = dst.Write(chunk); err != nil {
			return [sha256.Size]byte{}, fail(FailureFilesystem, fmt.Errorf("write received chunk: %w", err))
		}
		whole.Write(chunk)
		total += uint64(n)
		index++
		if progress != nil {
			progress(total, d.Size)
		}
	}
	var trailing [1]byte
	n, err := io.ReadFull(src, trailing[:])
	if n != 0 {
		return [sha256.Size]byte{}, fmt.Errorf("data stream exceeds declared file size")
	}
	if err != io.EOF {
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		return [sha256.Size]byte{}, fmt.Errorf("data stream did not finish at declared file size: %w", err)
	}
	var actual [sha256.Size]byte
	copy(actual[:], whole.Sum(nil))
	return actual, nil
}

func openRegularFile(path string) (*os.File, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect source file: %w", err)
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() {
		return nil, fmt.Errorf("source must be a regular file, not a symbolic link or special file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open source file: %w", err)
	}
	after, e := f.Stat()
	if e != nil {
		f.Close()
		return nil, e
	}
	if !os.SameFile(before, after) || !after.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("source file changed while opening")
	}
	return f, nil
}

func chooseAdvertiseIP(listen string) (string, error) {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return "", fmt.Errorf("invalid listen address: %w", err)
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return "127.0.0.1", nil
	}
	ifs, err := net.Interfaces()
	if err != nil {
		return "", fmt.Errorf("enumerate network interfaces: %w", err)
	}
	var candidates []string
	for _, iface := range ifs {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, e := iface.Addrs()
		if e != nil {
			continue
		}
		for _, address := range addresses {
			prefix, _, e := net.ParseCIDR(address.String())
			if e == nil && prefix.To4() != nil && prefix.IsPrivate() && !prefix.IsLoopback() && !prefix.IsLinkLocalUnicast() {
				candidates = append(candidates, prefix.String())
			}
		}
	}
	if len(candidates) != 1 {
		return "", fmt.Errorf("cannot choose one LAN IPv4 address safely (%d candidates); pass --advertise IP", len(candidates))
	}
	return candidates[0], nil
}

func acceptOne(ctx context.Context, l *quictransport.Listener, inv auth.Invite) (*quictransport.Conn, *quictransport.ControlStream, *bufio.Reader, error) {
	acceptCtx, stopAccept := context.WithCancel(ctx)
	defer stopAccept()
	type outcome struct {
		conn   *quictransport.Conn
		stream *quictransport.ControlStream
		reader *bufio.Reader
		err    error
	}
	results := make(chan outcome, 4)
	incoming := make(chan *quictransport.Conn, 4)
	slots := make(chan struct{}, 4)
	acceptDone := make(chan struct{})
	var workers sync.WaitGroup
	defer func() {
		stopAccept()
		<-acceptDone
		workers.Wait()
		for {
			select {
			case pending := <-incoming:
				_ = pending.CloseWithError(1, "session ended")
			default:
				return
			}
		}
	}()
	go func() {
		defer close(acceptDone)
		for {
			c, e := l.Accept(acceptCtx)
			if e != nil {
				return
			}
			select {
			case incoming <- c:
				if acceptCtx.Err() != nil {
					_ = c.CloseWithError(1, "session ended")
					return
				}
			case <-acceptCtx.Done():
				_ = c.CloseWithError(1, "session ended")
				return
			}
		}
	}()
	var failures atomic.Uint32
	var used atomic.Bool
	for {
		select {
		case <-acceptCtx.Done():
			return nil, nil, nil, acceptCtx.Err()
		case result := <-results:
			if result.conn != nil {
				// Closing the listener stops new admissions while quic-go keeps
				// established connections alive for the admitted transfer.
				_ = l.Close()
				return result.conn, result.stream, result.reader, nil
			}
			if result.err != nil {
				return nil, nil, nil, result.err
			}
		case conn := <-incoming:
			select {
			case slots <- struct{}{}:
			default:
				_ = conn.CloseWithError(1, "too many pending receivers")
				continue
			}
			workers.Add(1)
			go func(c *quictransport.Conn) {
				defer workers.Done()
				defer func() { <-slots }()
				handshake, cancel := context.WithTimeout(acceptCtx, 10*time.Second)
				defer cancel()
				stream, e := c.AcceptControl(handshake)
				if e != nil {
					_ = c.CloseWithError(quictransport.ErrorInvalidSession, "invalid session")
					return
				}
				_ = stream.SetReadDeadline(time.Now().Add(10 * time.Second))
				reader := bufio.NewReader(stream)
				msg, e := protocol.ReadMessage(reader)
				if e != nil || msg.Type != "authenticate" || msg.Session != inv.Session || !auth.VerifySecret(inv.Secret, msg.Secret) {
					if failures.Add(1) >= 8 {
						select {
						case results <- outcome{err: fmt.Errorf("authentication attempt limit reached")}:
						case <-ctx.Done():
						}
					}
					_ = c.CloseWithError(quictransport.ErrorAuthentication, "authentication failed")
					return
				}
				if !used.CompareAndSwap(false, true) {
					_ = protocol.WriteMessage(stream, outcomeReject(inv.Session))
					_ = c.CloseWithError(quictransport.ErrorInviteConsumed, "invite already used")
					return
				}
				_ = stream.SetReadDeadline(time.Time{})
				select {
				case results <- outcome{conn: c, stream: stream, reader: reader}:
				case <-acceptCtx.Done():
					_ = c.CloseWithError(1, "session ended")
				}
			}(conn)
		}
	}
}

func outcomeReject(session string) protocol.Message {
	return protocol.Message{Version: protocol.Version, Type: "reject", Session: session, Reason: "invite already used"}
}

func confirmOffer(d protocol.Descriptor) (bool, error) {
	_, err := fmt.Fprint(os.Stderr, "Accept transfer? [y/N] ")
	if err != nil {
		return false, err
	}
	var answer string
	if _, err = fmt.Fscanln(os.Stdin, &answer); err != nil && err != io.EOF {
		return false, fmt.Errorf("read transfer confirmation: %w", err)
	}
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes"), nil
}
