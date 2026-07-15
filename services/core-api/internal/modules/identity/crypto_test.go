package identity

import (
	"bytes"
	"testing"
)

func TestCryptoRoundTripAndPurposeSeparatedDigests(t *testing.T) {
	t.Parallel()

	crypto, err := NewCrypto(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("create crypto: %v", err)
	}

	verifier, err := crypto.PKCEVerifier()
	if err != nil {
		t.Fatalf("generate verifier: %v", err)
	}
	if len(verifier) < 43 || len(PKCEChallenge(verifier)) != 43 {
		t.Fatalf("unexpected PKCE values: verifier=%d challenge=%d", len(verifier), len(PKCEChallenge(verifier)))
	}

	ciphertext, err := crypto.Encrypt(verifier)
	if err != nil {
		t.Fatalf("encrypt verifier: %v", err)
	}
	plaintext, err := crypto.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("decrypt verifier: %v", err)
	}
	if plaintext != verifier {
		t.Fatal("decrypted verifier does not match")
	}

	digest := crypto.Digest("session", "token")
	if !crypto.EqualDigest(digest, "session", "token") {
		t.Fatal("expected matching digest")
	}
	if crypto.EqualDigest(digest, "csrf", "token") {
		t.Fatal("digest purposes must be separated")
	}
}

func TestNormalizeReturnToRejectsExternalOrAmbiguousTargets(t *testing.T) {
	t.Parallel()

	valid, err := NormalizeReturnTo("/app/classes?tab=active")
	if err != nil || valid != "/app/classes?tab=active" {
		t.Fatalf("unexpected valid return target: %q, %v", valid, err)
	}

	for _, value := range []string{"https://evil.example", "//evil.example", `/\\evil`, "app/home"} {
		if _, err := NormalizeReturnTo(value); err == nil {
			t.Fatalf("expected return target %q to be rejected", value)
		}
	}
}

func TestIPPrefixMinimizesAddressPrecision(t *testing.T) {
	t.Parallel()

	if got := IPPrefix("192.0.2.129:443"); got != "192.0.2.0/24" {
		t.Fatalf("unexpected IPv4 prefix: %q", got)
	}
	if got := IPPrefix("[2001:db8:1234:5678::1]:443"); got != "2001:db8:1234:5600::/56" {
		t.Fatalf("unexpected IPv6 prefix: %q", got)
	}
}
