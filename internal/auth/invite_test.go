package auth

import (
	"encoding/base64"
	"testing"
	"time"
)

func TestInviteRoundTripAndTrust(t *testing.T) {
	i, _, err := New("127.0.0.1:4567")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := Encode(i)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if got.Session != i.Session || got.Endpoint != i.Endpoint {
		t.Fatalf("round trip mismatch: %#v", got)
	}
	if _, err = ClientTLS(got); err != nil {
		t.Fatal(err)
	}
	fingerprint, err := Fingerprint(got)
	if err != nil || len(fingerprint) != 64 {
		t.Fatalf("fingerprint: %q %v", fingerprint, err)
	}
}

func TestInviteRejectsTamperingAndExpiry(t *testing.T) {
	i, _, err := New("127.0.0.1:4567")
	if err != nil {
		t.Fatal(err)
	}
	i.Expires = time.Now().Add(-time.Second)
	encoded, _ := Encode(i)
	if _, err = Decode(encoded); err == nil {
		t.Fatal("expired invite accepted")
	}
	i.Expires = time.Now().Add(InviteLifetime)
	encoded, _ = Encode(i)
	i.Certificate = base64.RawURLEncoding.EncodeToString([]byte("not an X.509 certificate"))
	altered, err := Encode(i)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Decode(altered); err == nil {
		t.Fatal("invalid certificate accepted")
	}
	if _, err = Decode(encoded + "!"); err == nil {
		t.Fatal("invalid encoding accepted")
	}
}

func TestInviteRejectsV1Downgrade(t *testing.T) {
	i, _, err := New("127.0.0.1:4567")
	if err != nil {
		t.Fatal(err)
	}
	i.Version = 1
	token, err := Encode(i)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Decode(token); err == nil {
		t.Fatal("v1 invite was accepted by the v2 implementation")
	}
}

func TestVerifySecretConstantContract(t *testing.T) {
	a := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	if !VerifySecret(a, a) {
		t.Fatal("matching secret rejected")
	}
	if VerifySecret(a, base64.RawURLEncoding.EncodeToString(make([]byte, 31))) {
		t.Fatal("short secret accepted")
	}
	b := make([]byte, 32)
	b[31] = 1
	if VerifySecret(a, base64.RawURLEncoding.EncodeToString(b)) {
		t.Fatal("different secret accepted")
	}
}

func FuzzDecodeInvite(f *testing.F) {
	i, _, err := New("127.0.0.1:4567")
	if err == nil {
		if token, e := Encode(i); e == nil {
			f.Add(token)
		}
	}
	f.Add(InvitePrefix)
	f.Add("")
	f.Fuzz(func(t *testing.T, token string) {
		if len(token) > MaxInvite {
			return
		}
		_, _ = Decode(token)
	})
}
