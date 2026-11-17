package identity

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/argon2"
)

func passwordTestService(t testing.TB, mode string) *PasswordService {
	t.Helper()
	service, err := NewPasswordService(PasswordOptions{MinimumLength: 8, MaxConcurrency: 2, BreachMode: mode})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestPasswordNormalizationAndBounds(t *testing.T) {
	for _, test := range []struct{ value, want string }{
		{"Ｓｐａｃｅ １２３!", "Space 123!"},
		{"  six river stones  ", "  six river stones  "},
		{"cafe\u0301 island 47", "café island 47"},
		{strings.Repeat("界", 128), strings.Repeat("界", 128)},
		{strings.Repeat("😀", 128), strings.Repeat("😀", 128)},
	} {
		got, err := normalizePassword(test.value, 8)
		if err != nil || got != test.want {
			t.Fatal("valid normalized printable password rejected")
		}
	}
	for _, value := range []string{"short", strings.Repeat("a", 129), strings.Repeat("😀", 129), "safe\nphrase123", "safe\tphrase123", "secret\x00value", "safe\u200bvalue123", "invalid\xffvalue", strings.Repeat("\ufdfa", 32)} {
		if _, err := normalizePassword(value, 8); !errors.Is(err, ErrPasswordPolicy) {
			t.Fatal("invalid or overlong password accepted")
		}
	}
}

func TestPasswordHashVerifyPHCAndSaltUniqueness(t *testing.T) {
	service := passwordTestService(t, "off")
	phc, err := service.Hash(context.Background(), "Ｓｐａｃｅ １２３!")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(phc, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatal("incorrect Argon2id PHC parameters")
	}
	_, salt, key, err := parsePasswordPHC(phc)
	if err != nil || len(salt) != 16 || len(key) != 32 {
		t.Fatal("incorrect salt or key format")
	}
	for _, test := range []struct {
		candidate string
		want      bool
	}{{"Space 123!", true}, {"Ｓｐａｃｅ １２３!", true}, {" Space 123!", false}, {"incorrect", false}, {"invalid\ninput", false}} {
		ok, rehash, err := service.Verify(context.Background(), test.candidate, phc)
		if err != nil || ok != test.want || rehash {
			t.Fatal("password verification or normalization mismatch")
		}
	}
	other, err := service.Hash(context.Background(), "Space 123!")
	if err != nil || phc == other {
		t.Fatal("hash salt is reused")
	}
	if err := service.DummyVerify(context.Background(), "Space 123!"); err != nil {
		t.Fatal(err)
	}
}

func TestPasswordLegacyPHCRequestsRehash(t *testing.T) {
	service := passwordTestService(t, "off")
	password := "a cedar grove with 73 stones"
	salt := []byte("legacy-salt-1234")
	key := argon2.IDKey([]byte(password), salt, 1, 8192, 1, 32)
	phc := fmt.Sprintf("$argon2id$v=19$m=8192,t=1,p=1$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key))
	ok, rehash, err := service.Verify(context.Background(), password, phc)
	if err != nil || !ok || !rehash {
		t.Fatal("legacy bounded parameters did not request rehash")
	}
	service.minimumLength = 64
	ok, rehash, err = service.Verify(context.Background(), password, phc)
	if err != nil || !ok || !rehash {
		t.Fatal("new enrollment minimum blocked existing credential login")
	}
}

func TestPasswordRejectsMalformedOrUnboundedPHCBeforeArgon(t *testing.T) {
	for _, value := range []string{
		"", "not-a-phc", strings.Repeat("x", 257),
		strings.Replace(dummyPasswordPHC, "argon2id", "argon2i", 1),
		strings.Replace(dummyPasswordPHC, "v=19", "v=16", 1),
		strings.Replace(dummyPasswordPHC, "m=19456", "m=4294967295", 1),
		strings.Replace(dummyPasswordPHC, "m=19456", "m=65537", 1),
		strings.Replace(dummyPasswordPHC, "m=19456", "m=0", 1),
		strings.Replace(dummyPasswordPHC, "m=19456", "m=019456", 1),
		strings.Replace(dummyPasswordPHC, "t=2", "t=0", 1),
		strings.Replace(dummyPasswordPHC, "t=2", "t=5", 1),
		strings.Replace(dummyPasswordPHC, "p=1", "p=0", 1),
		strings.Replace(dummyPasswordPHC, "p=1", "p=256", 1),
		strings.Replace(dummyPasswordPHC, "p=1", "p=1,p=2", 1),
		strings.Replace(dummyPasswordPHC, "AAECAwQFBgcICQoLDA0ODw", "YQ", 1),
		strings.Replace(dummyPasswordPHC, "AAECAwQFBgcICQoLDA0ODw", "AAECAwQFBgcICQoLDA0ODw==", 1),
		strings.Replace(dummyPasswordPHC, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "YQ", 1),
	} {
		if _, _, _, err := parsePasswordPHC(value); !errors.Is(err, ErrPasswordHash) {
			t.Fatal("malicious PHC passed bounded parsing")
		}
	}
}

func TestPasswordGlobalCapacityAndTimeout(t *testing.T) {
	first := passwordTestService(t, "off")
	second := passwordTestService(t, "off")
	if first.limiter != second.limiter || first.limiter != globalPasswordWork {
		t.Fatal("password services bypass the process-wide semaphore")
	}
	// Use a private semaphore to exercise the same acquisition logic without
	// changing the process-level settings of parallel native-auth tests.
	limiter := newPasswordWorkLimiter(1)
	if err := limiter.acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := limiter.acquire(context.Background()); !errors.Is(err, ErrPasswordBusy) {
		t.Fatal("saturated hash queue did not fail closed")
	}
	elapsed := time.Since(started)
	if elapsed < 1900*time.Millisecond || elapsed > 3*time.Second {
		t.Fatalf("hash queue did not respect two-second deadline: %s", elapsed)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := limiter.acquire(ctx); !errors.Is(err, ErrPasswordBusy) {
		t.Fatal("cancelled hash queue admitted work")
	}
	limiter.release()
	var running, peak atomic.Int32
	var group sync.WaitGroup
	for range 12 {
		group.Add(1)
		go func() {
			defer group.Done()
			if err := limiter.acquire(context.Background()); err != nil {
				t.Error(err)
				return
			}
			active := running.Add(1)
			for old := peak.Load(); active > old && !peak.CompareAndSwap(old, active); old = peak.Load() {
			}
			time.Sleep(time.Millisecond)
			running.Add(-1)
			limiter.release()
		}()
	}
	group.Wait()
	if peak.Load() != 1 {
		t.Fatal("hash semaphore admitted concurrent work past its cap")
	}
}

func TestPasswordServiceConfigurationAndOfflinePolicy(t *testing.T) {
	for _, options := range []PasswordOptions{{MinimumLength: 7}, {MinimumLength: 65}, {MaxConcurrency: -1}, {MaxConcurrency: 33}, {BreachMode: "unexpected"}} {
		if _, err := NewPasswordService(options); err == nil {
			t.Fatal("unsafe password service configuration accepted")
		}
	}
	service := passwordTestService(t, "offline")
	for _, password := range []string{"password", "12345678", "TUTORHUB-2026", "sample.student-472", "NGUYỄN VĂN AN-72"} {
		if _, err := service.ValidateAndHash(context.Background(), password, "sample.student@example.test", "Nguyễn Văn An"); !errors.Is(err, ErrPasswordPolicy) {
			t.Fatal("offline or contextual blocklist bypassed")
		}
	}
	if _, err := service.ValidateAndHash(context.Background(), "nine lavender rivers 72!", "a@example.test", "Li"); err != nil {
		t.Fatal("short contextual initials overblocked an unrelated passphrase")
	}
}

func BenchmarkArgon2idHashAndVerify(b *testing.B) {
	service := passwordTestService(b, "off")
	const password = "benchmark cedar river 739!"
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		phc, err := service.Hash(context.Background(), password)
		if err != nil {
			b.Fatal(err)
		}
		ok, rehash, err := service.Verify(context.Background(), password, phc)
		if err != nil || !ok || rehash {
			b.Fatal("benchmark verification failed")
		}
	}
}
