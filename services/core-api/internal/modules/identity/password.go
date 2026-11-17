package identity

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
	"golang.org/x/text/unicode/norm"
)

const (
	passwordMemoryKiB   = uint32(19456)
	passwordIterations  = uint32(2)
	passwordParallelism = uint8(1)
	passwordSaltBytes   = 16
	passwordKeyBytes    = 32
	passwordHashWait    = 2 * time.Second
	// This synthetic PHC is fixed and contains no user's password. DummyVerify
	// performs the same current Argon2id work but never authenticates its input.
	dummyPasswordPHC = "$argon2id$v=19$m=19456,t=2,p=1$AAECAwQFBgcICQoLDA0ODw$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
)

var (
	ErrPasswordPolicy = errors.New("password does not meet the password policy")
	ErrPasswordBusy   = errors.New("password verification capacity is temporarily unavailable")
	ErrPasswordHash   = errors.New("password hash operation failed")
)

type PasswordOptions struct {
	MinimumLength  int
	MaxConcurrency int
	BreachMode     string
	Logger         *slog.Logger
}

type PasswordService struct {
	minimumLength int
	breach        *passwordBreachChecker
	limiter       *passwordWorkLimiter
}

// All service instances share a process-wide limiter. Configure once during
// startup; changing its limit also constrains work started by existing services.
var globalPasswordWork = newPasswordWorkLimiter(2)

func NewPasswordService(options PasswordOptions) (*PasswordService, error) {
	if options.MinimumLength == 0 {
		options.MinimumLength = 8
	}
	if options.MaxConcurrency == 0 {
		options.MaxConcurrency = 2
	}
	if options.BreachMode == "" {
		options.BreachMode = "offline"
	}
	if options.MinimumLength < 8 || options.MinimumLength > 64 || options.MaxConcurrency < 1 || options.MaxConcurrency > 32 ||
		(options.BreachMode != "offline" && options.BreachMode != "hibp" && options.BreachMode != "off") {
		return nil, errors.New("invalid password service configuration")
	}
	globalPasswordWork.configure(options.MaxConcurrency)
	return &PasswordService{minimumLength: options.MinimumLength, breach: newPasswordBreachChecker(options.BreachMode, options.Logger), limiter: globalPasswordWork}, nil
}

func (service *PasswordService) ValidateAndHash(ctx context.Context, password, email, displayName string) (string, error) {
	normalized, err := normalizePassword(password, service.minimumLength)
	if err != nil {
		return "", err
	}
	if err := service.breach.check(ctx, normalized, email, displayName); err != nil {
		return "", err
	}
	return service.hashNormalized(ctx, normalized)
}

// Hash is also used for rehash-on-login. It enforces the safety bounds and NFKC,
// but not a newly raised enrollment minimum or a changed breach policy.
func (service *PasswordService) Hash(ctx context.Context, password string) (string, error) {
	normalized, err := normalizePassword(password, 1)
	if err != nil {
		return "", err
	}
	return service.hashNormalized(ctx, normalized)
}

func (service *PasswordService) hashNormalized(ctx context.Context, password string) (string, error) {
	if err := service.limiter.acquire(ctx); err != nil {
		return "", err
	}
	defer service.limiter.release()
	salt := make([]byte, passwordSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", ErrPasswordHash
	}
	key := argon2.IDKey([]byte(password), salt, passwordIterations, passwordMemoryKiB, passwordParallelism, passwordKeyBytes)
	defer clear(key)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", passwordMemoryKiB, passwordIterations, passwordParallelism,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func (service *PasswordService) Verify(ctx context.Context, password, phc string) (bool, bool, error) {
	parameters, salt, expected, err := parsePasswordPHC(phc)
	if err != nil {
		return false, false, err
	}
	normalized, policyErr := normalizePassword(password, 1)
	if policyErr != nil {
		// Even malformed candidate input takes one bounded Argon2id operation.
		if err := service.DummyVerify(ctx, password); err != nil {
			return false, false, err
		}
		return false, false, nil
	}
	if err := service.limiter.acquire(ctx); err != nil {
		return false, false, err
	}
	defer service.limiter.release()
	actual := argon2.IDKey([]byte(normalized), salt, parameters.iterations, parameters.memory, parameters.parallelism, uint32(len(expected)))
	defer clear(actual)
	matched := subtle.ConstantTimeCompare(actual, expected) == 1
	rehash := matched && (parameters.memory != passwordMemoryKiB || parameters.iterations != passwordIterations ||
		parameters.parallelism != passwordParallelism || len(salt) != passwordSaltBytes || len(expected) != passwordKeyBytes)
	return matched, rehash, nil
}

func (service *PasswordService) DummyVerify(ctx context.Context, password string) error {
	normalized, err := normalizePassword(password, 1)
	if err != nil {
		normalized = "invalid-password-input"
	}
	parameters, salt, expected, _ := parsePasswordPHC(dummyPasswordPHC)
	if err := service.limiter.acquire(ctx); err != nil {
		return err
	}
	defer service.limiter.release()
	actual := argon2.IDKey([]byte(normalized), salt, parameters.iterations, parameters.memory, parameters.parallelism, uint32(len(expected)))
	_ = subtle.ConstantTimeCompare(actual, expected)
	clear(actual)
	return nil
}

func (service *PasswordService) BreachFailureCount() uint64 { return service.breach.failures.Load() }

func normalizePassword(password string, minimum int) (string, error) {
	if !utf8.ValidString(password) || len(password) > 512 {
		return "", ErrPasswordPolicy
	}
	normalized := norm.NFKC.String(password)
	length := utf8.RuneCountInString(normalized)
	if len(normalized) > 512 || length < minimum || length > 128 {
		return "", ErrPasswordPolicy
	}
	for _, character := range normalized {
		if !unicode.IsPrint(character) {
			return "", ErrPasswordPolicy
		}
	}
	return normalized, nil
}

type passwordParameters struct {
	memory, iterations uint32
	parallelism        uint8
}

// Parse before allocating Argon memory. The bounded legacy envelope allows
// upgrades from existing parameters without accepting attacker-controlled OOM.
func parsePasswordPHC(phc string) (passwordParameters, []byte, []byte, error) {
	invalid := func() (passwordParameters, []byte, []byte, error) {
		return passwordParameters{}, nil, nil, ErrPasswordHash
	}
	if len(phc) > 256 {
		return invalid()
	}
	parts := strings.Split(phc, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" {
		return invalid()
	}
	fields := strings.Split(parts[3], ",")
	if len(fields) != 3 {
		return invalid()
	}
	values := [3]uint64{}
	for index, prefix := range []string{"m=", "t=", "p="} {
		if !strings.HasPrefix(fields[index], prefix) {
			return invalid()
		}
		raw := strings.TrimPrefix(fields[index], prefix)
		value, err := strconv.ParseUint(raw, 10, 32)
		if err != nil || strconv.FormatUint(value, 10) != raw {
			return invalid()
		}
		values[index] = value
	}
	if values[0] < 8192 || values[0] > 65536 || values[1] < 1 || values[1] > 4 || values[2] < 1 || values[2] > 4 {
		return invalid()
	}
	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil || len(salt) < 16 || len(salt) > 32 {
		return invalid()
	}
	key, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil || len(key) < 16 || len(key) > 64 {
		return invalid()
	}
	return passwordParameters{uint32(values[0]), uint32(values[1]), uint8(values[2])}, salt, key, nil
}

type passwordWorkLimiter struct {
	mu            sync.Mutex
	limit, active int
	changed       chan struct{}
}

func newPasswordWorkLimiter(limit int) *passwordWorkLimiter {
	return &passwordWorkLimiter{limit: limit, changed: make(chan struct{})}
}

func (limiter *passwordWorkLimiter) configure(limit int) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	limiter.limit = limit
	close(limiter.changed)
	limiter.changed = make(chan struct{})
}

func (limiter *passwordWorkLimiter) acquire(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, passwordHashWait)
	defer cancel()
	for {
		if ctx.Err() != nil {
			return ErrPasswordBusy
		}
		limiter.mu.Lock()
		if limiter.active < limiter.limit {
			limiter.active++
			limiter.mu.Unlock()
			return nil
		}
		changed := limiter.changed
		limiter.mu.Unlock()
		select {
		case <-ctx.Done():
			return ErrPasswordBusy
		case <-changed:
		}
	}
}

func (limiter *passwordWorkLimiter) release() {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	limiter.active--
	close(limiter.changed)
	limiter.changed = make(chan struct{})
}
