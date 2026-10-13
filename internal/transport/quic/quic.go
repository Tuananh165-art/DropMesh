package quictransport

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"time"

	"github.com/quic-go/quic-go"
)

const ALPN = "dropmesh/2"

const (
	ErrorAuthentication uint64 = 1
	ErrorInvalidSession uint64 = 2
	ErrorInviteConsumed uint64 = 3
)

func config() *quic.Config {
	// M1 uses one bidirectional control stream and one sender-to-receiver data
	// stream. QUIC's peer stream limits reject any additional inbound streams.
	return &quic.Config{HandshakeIdleTimeout: 10 * time.Second, MaxIdleTimeout: 60 * time.Second, KeepAlivePeriod: 20 * time.Second, MaxIncomingStreams: 1, MaxIncomingUniStreams: 1, InitialStreamReceiveWindow: 2 << 20, MaxStreamReceiveWindow: 8 << 20, InitialConnectionReceiveWindow: 4 << 20, MaxConnectionReceiveWindow: 16 << 20}
}

type Listener struct{ inner *quic.Listener }
type Conn struct{ inner *quic.Conn }
type ControlStream struct{ inner *quic.Stream }
type DataSendStream struct{ inner *quic.SendStream }
type DataReceiveStream struct{ inner *quic.ReceiveStream }

func Listen(address string, certificate tls.Certificate) (*Listener, error) {
	inner, err := quic.ListenAddr(address, &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, NextProtos: []string{ALPN}, Certificates: []tls.Certificate{certificate}}, config())
	if err != nil {
		return nil, err
	}
	return &Listener{inner: inner}, nil
}

func (l *Listener) Addr() net.Addr { return l.inner.Addr() }
func (l *Listener) Close() error   { return l.inner.Close() }
func (l *Listener) Accept(ctx context.Context) (*Conn, error) {
	inner, err := l.inner.Accept(ctx)
	if err != nil {
		return nil, err
	}
	return &Conn{inner: inner}, nil
}

func Dial(ctx context.Context, address string, tlsConfig *tls.Config) (*Conn, error) {
	inner, err := quic.DialAddr(ctx, address, tlsConfig, config())
	if err != nil {
		return nil, err
	}
	return &Conn{inner: inner}, nil
}

func (c *Conn) OpenControl(ctx context.Context) (*ControlStream, error) {
	inner, err := c.inner.OpenStreamSync(ctx)
	if err != nil {
		return nil, err
	}
	return &ControlStream{inner: inner}, nil
}
func (c *Conn) AcceptControl(ctx context.Context) (*ControlStream, error) {
	inner, err := c.inner.AcceptStream(ctx)
	if err != nil {
		return nil, err
	}
	return &ControlStream{inner: inner}, nil
}
func (c *Conn) OpenData(ctx context.Context) (*DataSendStream, error) {
	inner, err := c.inner.OpenUniStreamSync(ctx)
	if err != nil {
		return nil, err
	}
	return &DataSendStream{inner: inner}, nil
}
func (c *Conn) AcceptData(ctx context.Context) (*DataReceiveStream, error) {
	inner, err := c.inner.AcceptUniStream(ctx)
	if err != nil {
		return nil, err
	}
	return &DataReceiveStream{inner: inner}, nil
}
func (c *Conn) CloseWithError(code uint64, reason string) error {
	return c.inner.CloseWithError(quic.ApplicationErrorCode(code), reason)
}

func IsAuthenticationFailure(err error) bool {
	var app *quic.ApplicationError
	return errors.As(err, &app) && uint64(app.ErrorCode) == ErrorAuthentication
}

func IsGracefulClose(err error) bool {
	var app *quic.ApplicationError
	return errors.As(err, &app) && app.Remote && app.ErrorCode == 0
}

func IsTimeout(err error) bool {
	var handshake *quic.HandshakeTimeoutError
	var idle *quic.IdleTimeoutError
	var network net.Error
	return errors.As(err, &handshake) || errors.As(err, &idle) || (errors.As(err, &network) && network.Timeout())
}

func (s *ControlStream) Read(p []byte) (int, error)        { return s.inner.Read(p) }
func (s *ControlStream) Write(p []byte) (int, error)       { return s.inner.Write(p) }
func (s *ControlStream) Close() error                      { return s.inner.Close() }
func (s *ControlStream) CancelRead(code uint64)            { s.inner.CancelRead(quic.StreamErrorCode(code)) }
func (s *ControlStream) CancelWrite(code uint64)           { s.inner.CancelWrite(quic.StreamErrorCode(code)) }
func (s *ControlStream) SetReadDeadline(t time.Time) error { return s.inner.SetReadDeadline(t) }

func (s *DataSendStream) Write(p []byte) (int, error) { return s.inner.Write(p) }
func (s *DataSendStream) Close() error                { return s.inner.Close() }
func (s *DataSendStream) CancelWrite(code uint64)     { s.inner.CancelWrite(quic.StreamErrorCode(code)) }

func (s *DataReceiveStream) Read(p []byte) (int, error) { return s.inner.Read(p) }
func (s *DataReceiveStream) CancelRead(code uint64)     { s.inner.CancelRead(quic.StreamErrorCode(code)) }

var _ io.ReadWriteCloser = (*ControlStream)(nil)
var _ io.WriteCloser = (*DataSendStream)(nil)
var _ io.Reader = (*DataReceiveStream)(nil)
