package identity

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1" // HIBP range lookup only; never used for password storage.
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	hibpTimeout          = 1500 * time.Millisecond
	hibpMaxResponseBytes = 1 << 20
	hibpRangeURL         = "https://api.pwnedpasswords.com/range/"
)

type passwordBreachChecker struct {
	mode     string
	client   *http.Client
	rangeURL string
	logger   *slog.Logger
	failures atomic.Uint64
}

func newPasswordBreachChecker(mode string, logger *slog.Logger) *passwordBreachChecker {
	if logger == nil {
		logger = slog.Default()
	}
	return &passwordBreachChecker{mode: mode, rangeURL: hibpRangeURL, logger: logger,
		client: &http.Client{Timeout: hibpTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (checker *passwordBreachChecker) check(ctx context.Context, password, email, displayName string) error {
	if checker.mode == "off" {
		return nil
	}
	if offlinePasswordBlocked(password, email, displayName) {
		return ErrPasswordPolicy
	}
	if checker.mode != "hibp" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, hibpTimeout)
	defer cancel()
	digest := sha1.Sum([]byte(password))
	encoded := strings.ToUpper(hex.EncodeToString(digest[:]))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, checker.rangeURL+encoded[:5], nil)
	if err != nil {
		return checker.failOpen()
	}
	request.Header.Set("Add-Padding", "true")
	request.Header.Set("User-Agent", "TutorHub-Password-Breach-Check/1.0")
	response, err := checker.client.Do(request)
	if err != nil {
		return checker.failOpen()
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return checker.failOpen()
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, hibpMaxResponseBytes+1))
	if err != nil || len(body) > hibpMaxResponseBytes {
		return checker.failOpen()
	}
	compromised, valid := parseHIBPRange(body, encoded[5:])
	if !valid {
		return checker.failOpen()
	}
	if compromised {
		return ErrPasswordPolicy
	}
	return nil
}

func (checker *passwordBreachChecker) failOpen() error {
	checker.failures.Add(1)
	// Do not log the request URL, digest prefix, provider response, raw error,
	// password or user identity. The counter is exposed by PasswordService.
	checker.logger.Warn("password breach check unavailable", "provider", "hibp", "outcome", "fail_open")
	return nil
}

func parseHIBPRange(body []byte, wantedSuffix string) (compromised, valid bool) {
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 128), 128)
	lines := 0
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		parts := strings.Split(line, ":")
		if len(parts) != 2 || len(parts[0]) != 35 || len(parts[1]) == 0 || len(parts[1]) > 20 {
			return false, false
		}
		for _, character := range parts[0] {
			if !(character >= '0' && character <= '9' || character >= 'A' && character <= 'F' || character >= 'a' && character <= 'f') {
				return false, false
			}
		}
		count, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			return false, false
		}
		// Padding rows have count zero and must not turn an unknown password
		// into a compromised one. Inspect all rows instead of a prefix match.
		if subtle.ConstantTimeCompare([]byte(strings.ToUpper(parts[0])), []byte(wantedSuffix)) == 1 && count > 0 {
			compromised = true
		}
		lines++
	}
	return compromised, scanner.Err() == nil && lines > 0
}

func offlinePasswordBlocked(password, email, displayName string) bool {
	folded := strings.ToLower(strings.TrimSpace(norm.NFKC.String(password)))
	digest := sha256.Sum256([]byte(folded))
	if _, blocked := commonPasswordDigests[hex.EncodeToString(digest[:])]; blocked {
		return true
	}
	compact := passwordContextKey(folded)
	localPart, _, _ := strings.Cut(email, "@")
	contexts := []string{"tutorhub", localPart, displayName}
	contexts = append(contexts, strings.Fields(norm.NFKC.String(displayName))...)
	for _, contextValue := range contexts {
		candidate := passwordContextKey(contextValue)
		// Short initials must not reject unrelated passphrases containing them.
		if utf8.RuneCountInString(candidate) >= 3 && strings.Contains(compact, candidate) {
			return true
		}
	}
	return false
}

func passwordContextKey(value string) string {
	return strings.Map(func(character rune) rune {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			return unicode.ToLower(character)
		}
		return -1
	}, norm.NFKC.String(value))
}

// A deliberately small offline baseline, stored as SHA-256 digests of common
// normalized passwords. HIBP mode supplements this list without disclosing the
// password or full digest to the external provider.
var commonPasswordDigests = map[string]struct{}{
	"ef797c8118f02dfb649607dd5d3f8c7623048c9c063d532cc95c5ed7a898a64f": {},
	"15e2b0d3c33891ebb0f1ef609ec419420c20e320ce94c65fbc8c3312448eb225": {},
	"c775e7b757ede630cd0aa1113bd102661ab38829ca52a6422ab782862f268646": {},
	"ee79976c9380d5e337fc1c095ece8c8f22f91f306ceeb161fa51fecede2c4ba1": {},
	"7e071fd9b023ed8f18458a73613a0834f6220bd5cc50357ba3493c6040a9ea8c": {},
	"5e884898da28047151d0e56f8dc6292773603d0d6aabbdd62a11ef721d1542d8": {},
	"0b14d501a594442a01c6859541bcb3e8164d183d32937b851835442f69d5c94e": {},
	"ef92b778bafe771e89245b89ecbc08a44a4e166c06659911881f383d4473e94f": {},
	"8f0e2f76e22b43e2855189877e7dc1e1e7d98c226c95db247cd1d547928334a9": {},
	"daaad6e5604e8e17bd9f108d91e26afe6281dac8fda0091040a7a6d7bd9b43b5": {},
	"9a900403ac313ba27a1bc81f0932652b8020dac92c234d98fa0b06bf0040ecfd": {},
	"9c56cc51b374c3ba189210d5b6d4bf57790d351c96c47c02190ecf1e430635ab": {},
	"e4ad93ca07acb8d908a3aa41e920ea4f4ef4f26e7f86cf8291c5db289780a5ae": {},
	"fcc3a23fc7232cc89c7cb0f23d8774fefb73d7dc2ab22e6a1b6b8b202b4dcc91": {},
	"240be518fabd2724ddb6f04eeb1da5967448d7e831c08c8fa822809f74c720a9": {},
	"2e844ad651c6b9a69cbe8f887b6a74dd3b9fc489aae777701e6e2d0d523a0cfb": {},
	"057ba03d6c44104863dc7361fe4578965d1887360f90a0895882e58a6248fc86": {},
	"c20cf9879666a76500bb136af3d0cf4d15683064b3ef5bdad5ced4ca083926ba": {},
	"6382deaf1f5dc6e792b76db4a4a7bf2ba468884e000b25e7928e621e27fb23cb": {},
	"e48443929f57ec4cb965d358a825849155b2828873c4ed2929fd711bc9f01347": {},
	"14f8f4bb8c0e79a02670a5fea5682da717a5b3d3dc7b1706f7a4bab9afae18c2": {},
	"fcf730b6d95236ecd3c9fc2d92d7b6b2bb061514961aec041d6c7a7192f592e4": {},
	"72ab994fa2eb426c051ef59cad617750bfe06d7cf6311285ff79c19c32afd236": {},
	"059a00192592d5444bc0caad7203f98b506332e2cf7abb35d684ea9bf7c18f08": {},
	"e7bc2f973afb8dfaf00fadfb19596741108be08ab4a107c6a799c429b684c64a": {},
}
