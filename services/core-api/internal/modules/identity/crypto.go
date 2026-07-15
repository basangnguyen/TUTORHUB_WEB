package identity

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"strings"
)

const (
	randomTokenBytes  = 32
	pkceVerifierBytes = 48
	ciphertextVersion = byte(1)
)

type Crypto struct {
	hmacKey []byte
	aead    cipher.AEAD
}

func NewCrypto(key []byte) (*Crypto, error) {
	if len(key) < 32 {
		return nil, fmt.Errorf("identity crypto key must contain at least 32 bytes")
	}

	derivedKey := sha256.Sum256(append([]byte("tutorhub-identity-aead-v1:"), key...))
	block, err := aes.NewCipher(derivedKey[:])
	if err != nil {
		return nil, fmt.Errorf("create identity cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create identity AEAD: %w", err)
	}

	return &Crypto{hmacKey: append([]byte(nil), key...), aead: aead}, nil
}

func (crypto *Crypto) RandomToken() (string, error) {
	return randomBase64URL(randomTokenBytes)
}

func (crypto *Crypto) PKCEVerifier() (string, error) {
	return randomBase64URL(pkceVerifierBytes)
}

func (crypto *Crypto) Digest(purpose string, value string) []byte {
	mac := hmac.New(sha256.New, crypto.hmacKey)
	_, _ = mac.Write([]byte(purpose))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(value))
	return mac.Sum(nil)
}

func (crypto *Crypto) EqualDigest(expected []byte, purpose string, value string) bool {
	actual := crypto.Digest(purpose, value)
	return subtle.ConstantTimeCompare(expected, actual) == 1
}

func (crypto *Crypto) Encrypt(plaintext string) ([]byte, error) {
	nonce := make([]byte, crypto.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate identity encryption nonce: %w", err)
	}

	ciphertext := make([]byte, 1, 1+len(nonce)+len(plaintext)+crypto.aead.Overhead())
	ciphertext[0] = ciphertextVersion
	ciphertext = append(ciphertext, nonce...)
	ciphertext = crypto.aead.Seal(ciphertext, nonce, []byte(plaintext), nil)
	return ciphertext, nil
}

func (crypto *Crypto) Decrypt(ciphertext []byte) (string, error) {
	minimumLength := 1 + crypto.aead.NonceSize() + crypto.aead.Overhead()
	if len(ciphertext) < minimumLength || ciphertext[0] != ciphertextVersion {
		return "", fmt.Errorf("invalid encrypted identity value")
	}

	nonceEnd := 1 + crypto.aead.NonceSize()
	nonce := ciphertext[1:nonceEnd]
	plaintext, err := crypto.aead.Open(nil, nonce, ciphertext[nonceEnd:], nil)
	if err != nil {
		return "", fmt.Errorf("decrypt identity value: %w", err)
	}

	return string(plaintext), nil
}

func PKCEChallenge(verifier string) string {
	digest := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func NormalizeReturnTo(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "/app/home", nil
	}
	if !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.Contains(value, `\`) {
		return "", ErrInvalidReturnTo
	}

	parsed, err := url.Parse(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" {
		return "", ErrInvalidReturnTo
	}

	return parsed.RequestURI(), nil
}

func IPPrefix(remoteAddress string) string {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err != nil {
		host = remoteAddress
	}
	ip := net.ParseIP(strings.TrimSpace(host))
	if ip == nil {
		return ""
	}
	if ipv4 := ip.To4(); ipv4 != nil {
		return (&net.IPNet{IP: ipv4.Mask(net.CIDRMask(24, 32)), Mask: net.CIDRMask(24, 32)}).String()
	}

	return (&net.IPNet{IP: ip.Mask(net.CIDRMask(56, 128)), Mask: net.CIDRMask(56, 128)}).String()
}

func randomBase64URL(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate secure random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}
