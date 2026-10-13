package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"strings"
	"time"
)

const (
	InvitePrefix   = "dropmesh:v2:"
	MaxInvite      = 16 << 10
	InviteLifetime = 15 * time.Minute
	ServerName     = "dropmesh.local"
)

type Invite struct {
	Version     int       `json:"version"`
	Endpoint    string    `json:"endpoint"`
	Session     string    `json:"session"`
	Secret      string    `json:"secret"`
	Expires     time.Time `json:"expires"`
	Certificate string    `json:"certificate"`
}

func New(endpoint string) (Invite, tls.Certificate, error) {
	var inv Invite
	key, err := ecdsaKey()
	if err != nil {
		return inv, tls.Certificate{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return inv, tls.Certificate{}, err
	}
	now := time.Now().UTC()
	tmpl := x509.Certificate{SerialNumber: serial, Subject: pkixName(), NotBefore: now.Add(-time.Minute), NotAfter: now.Add(24 * time.Hour), DNSNames: []string{ServerName}, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true, IsCA: true}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, key.Public(), key)
	if err != nil {
		return inv, tls.Certificate{}, fmt.Errorf("create invite certificate: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM, err := privateKeyPEM(key)
	if err != nil {
		return inv, tls.Certificate{}, err
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return inv, tls.Certificate{}, fmt.Errorf("load invite certificate: %w", err)
	}
	var session [16]byte
	var secret [32]byte
	if _, err = rand.Read(session[:]); err != nil {
		return inv, tls.Certificate{}, err
	}
	if _, err = rand.Read(secret[:]); err != nil {
		return inv, tls.Certificate{}, err
	}
	inv = Invite{Version: 2, Endpoint: endpoint, Session: base64.RawURLEncoding.EncodeToString(session[:]), Secret: base64.RawURLEncoding.EncodeToString(secret[:]), Expires: now.Add(InviteLifetime), Certificate: base64.RawURLEncoding.EncodeToString(der)}
	return inv, pair, nil
}

func Encode(inv Invite) (string, error) {
	b, err := json.Marshal(inv)
	if err != nil {
		return "", err
	}
	token := InvitePrefix + base64.RawURLEncoding.EncodeToString(b)
	if len(token) > MaxInvite {
		return "", fmt.Errorf("invite exceeds %d bytes", MaxInvite)
	}
	return token, nil
}

func Decode(raw string) (Invite, error) {
	var inv Invite
	raw = strings.TrimSpace(raw)
	if len(raw) > MaxInvite || !strings.HasPrefix(raw, InvitePrefix) {
		return inv, fmt.Errorf("invalid invite format")
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(raw, InvitePrefix))
	if err != nil {
		return inv, fmt.Errorf("invalid invite encoding")
	}
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&inv); err != nil {
		return inv, fmt.Errorf("invalid invite data")
	}
	var extra any
	if err = dec.Decode(&extra); err != io.EOF {
		return inv, fmt.Errorf("trailing invite data")
	}
	if inv.Version != 2 {
		return inv, fmt.Errorf("unsupported invite version")
	}
	host, port, err := net.SplitHostPort(inv.Endpoint)
	if err != nil || host == "" || port == "" {
		return inv, fmt.Errorf("invalid invite endpoint")
	}
	if _, err = base64.RawURLEncoding.DecodeString(inv.Session); err != nil {
		return inv, fmt.Errorf("invalid invite session")
	}
	if v, _ := base64.RawURLEncoding.DecodeString(inv.Session); len(v) != 16 {
		return inv, fmt.Errorf("invalid invite session")
	}
	if v, e := base64.RawURLEncoding.DecodeString(inv.Secret); e != nil || len(v) != 32 {
		return inv, fmt.Errorf("invalid invite secret")
	}
	der, err := base64.RawURLEncoding.DecodeString(inv.Certificate)
	if err != nil {
		return inv, fmt.Errorf("invalid invite certificate")
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return inv, fmt.Errorf("invalid invite certificate")
	}
	if !cert.IsCA || len(cert.DNSNames) != 1 || cert.DNSNames[0] != ServerName {
		return inv, fmt.Errorf("invalid invite certificate identity")
	}
	if time.Now().After(inv.Expires) {
		return inv, fmt.Errorf("invite expired")
	}
	if inv.Expires.After(time.Now().Add(InviteLifetime + time.Minute)) {
		return inv, fmt.Errorf("invalid invite expiry")
	}
	return inv, nil
}

func ClientTLS(inv Invite) (*tls.Config, error) {
	der, err := base64.RawURLEncoding.DecodeString(inv.Certificate)
	if err != nil {
		return nil, fmt.Errorf("invalid invite certificate")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})) {
		return nil, fmt.Errorf("load invite trust root")
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, ServerName: ServerName, RootCAs: roots, NextProtos: []string{"dropmesh/2"}}, nil
}

func VerifySecret(expected, actual string) bool {
	a, e1 := base64.RawURLEncoding.DecodeString(expected)
	b, e2 := base64.RawURLEncoding.DecodeString(actual)
	if e1 != nil || e2 != nil || len(a) != 32 || len(b) != 32 {
		return false
	}
	return subtle.ConstantTimeCompare(a, b) == 1
}

func Fingerprint(inv Invite) (string, error) {
	der, e := base64.RawURLEncoding.DecodeString(inv.Certificate)
	if e != nil {
		return "", e
	}
	c, e := x509.ParseCertificate(der)
	if e != nil {
		return "", e
	}
	sum := sha256.Sum256(c.RawSubjectPublicKeyInfo)
	return fmt.Sprintf("%x", sum[:]), nil
}

func ecdsaKey() (*ecdsa.PrivateKey, error) { return ecdsa.GenerateKey(elliptic.P256(), rand.Reader) }
func pkixName() pkix.Name {
	return pkix.Name{CommonName: ServerName, Organization: []string{"DropMesh ephemeral transfer"}}
}
func privateKeyPEM(key *ecdsa.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("encode invite private key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}
