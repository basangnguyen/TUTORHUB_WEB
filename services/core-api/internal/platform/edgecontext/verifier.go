package edgecontext

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	HeaderVersion      = "X-TutorHub-Edge-Version"
	HeaderTimestamp    = "X-TutorHub-Edge-Timestamp"
	HeaderClientPrefix = "X-TutorHub-Client-Prefix"
	HeaderSignature    = "X-TutorHub-Edge-Signature"
	Version            = "v1"
	defaultMaxSkew     = 2 * time.Minute
)

var ErrInvalidKey = errors.New("edge context key must contain at least 32 bytes")

// Verifier authenticates the privacy-reduced client prefix asserted by the
// Cloudflare proxy. Invalid or absent assertions deliberately fall back to the
// direct peer address; they never make an untrusted forwarding header authoritative.
type Verifier struct {
	key     []byte
	clock   func() time.Time
	maxSkew time.Duration
}

type Config struct {
	Clock   func() time.Time
	MaxSkew time.Duration
}

func New(key []byte, config Config) (*Verifier, error) {
	if len(key) < 32 {
		return nil, ErrInvalidKey
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	if config.MaxSkew <= 0 {
		config.MaxSkew = defaultMaxSkew
	}
	return &Verifier{
		key:     append([]byte(nil), key...),
		clock:   config.Clock,
		maxSkew: config.MaxSkew,
	}, nil
}

// ResolveRemoteAddress returns a network address suitable for the existing
// privacy-prefix normalizers. It never returns the signed header verbatim.
func (verifier *Verifier) ResolveRemoteAddress(request *http.Request) string {
	if verifier == nil || request == nil {
		if request == nil {
			return ""
		}
		return request.RemoteAddr
	}
	prefix, ok := verifier.verify(request)
	if !ok {
		return request.RemoteAddr
	}
	_, network, err := net.ParseCIDR(prefix)
	if err != nil {
		return request.RemoteAddr
	}
	return network.IP.String()
}

func (verifier *Verifier) verify(request *http.Request) (string, bool) {
	version := strings.TrimSpace(request.Header.Get(HeaderVersion))
	timestampValue := strings.TrimSpace(request.Header.Get(HeaderTimestamp))
	prefix := strings.TrimSpace(request.Header.Get(HeaderClientPrefix))
	signatureValue := strings.TrimSpace(request.Header.Get(HeaderSignature))
	if version != Version || timestampValue == "" || prefix == "" || signatureValue == "" {
		return "", false
	}
	timestampUnix, err := strconv.ParseInt(timestampValue, 10, 64)
	if err != nil {
		return "", false
	}
	timestamp := time.Unix(timestampUnix, 0)
	delta := verifier.clock().UTC().Sub(timestamp.UTC())
	if delta < 0 {
		delta = -delta
	}
	if delta > verifier.maxSkew || !validPrivacyPrefix(prefix) {
		return "", false
	}
	signature, err := base64.RawURLEncoding.DecodeString(signatureValue)
	if err != nil || len(signature) != sha256.Size {
		return "", false
	}
	mac := hmac.New(sha256.New, verifier.key)
	_, _ = mac.Write([]byte(canonical(
		version,
		timestampValue,
		request.Method,
		request.URL.RequestURI(),
		prefix,
	)))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return "", false
	}
	return prefix, true
}

func validPrivacyPrefix(value string) bool {
	ip, network, err := net.ParseCIDR(value)
	if err != nil || ip == nil || network == nil || !ip.Equal(network.IP) {
		return false
	}
	ones, bits := network.Mask.Size()
	return (bits == 32 && ones == 24) || (bits == 128 && ones == 56)
}

func canonical(version, timestamp, method, requestURI, prefix string) string {
	return fmt.Sprintf(
		"%s\n%s\n%s\n%s\n%s",
		version,
		timestamp,
		strings.ToUpper(strings.TrimSpace(method)),
		requestURI,
		prefix,
	)
}
