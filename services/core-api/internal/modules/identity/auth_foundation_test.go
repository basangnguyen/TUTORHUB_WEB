package identity

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// This locked transactional double exercises service sequencing under goroutine
// races. The companion integration test verifies actual PostgreSQL row locks.
type foundationTestDB struct {
	mu           sync.Mutex
	transactions map[string]AuthTransaction
	challenges   map[uuid.UUID]*foundationTestChallenge
	deliveries   map[uuid.UUID][]any
	failCommit   bool
	commits      int
}

type foundationTestChallenge struct {
	tx                    uuid.UUID
	hash, destination     []byte
	deliverable           bool
	attempts, maximum     int
	created, expires      time.Time
	consumed, invalidated *time.Time
}

type foundationTestTx struct {
	pgx.Tx
	db     *foundationTestDB
	closed bool
}
type foundationTestRow struct {
	values []any
	err    error
}

func newFoundationTestDB() *foundationTestDB {
	return &foundationTestDB{transactions: map[string]AuthTransaction{}, challenges: map[uuid.UUID]*foundationTestChallenge{}, deliveries: map[uuid.UUID][]any{}}
}
func (db *foundationTestDB) Begin(context.Context) (pgx.Tx, error) {
	db.mu.Lock()
	return &foundationTestTx{db: db}, nil
}
func (tx *foundationTestTx) Commit(context.Context) error {
	if tx.closed {
		return pgx.ErrTxClosed
	}
	tx.closed = true
	defer tx.db.mu.Unlock()
	if tx.db.failCommit {
		tx.db.failCommit = false
		return errors.New("fixture commit failure")
	}
	tx.db.commits++
	return nil
}
func (tx *foundationTestTx) Rollback(context.Context) error {
	if !tx.closed {
		tx.closed = true
		tx.db.mu.Unlock()
	}
	return nil
}
func (row foundationTestRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(row.values) != len(dest) {
		return errors.New("fixture scan mismatch")
	}
	for i, value := range row.values {
		target := reflect.ValueOf(dest[i]).Elem()
		if value == nil {
			target.SetZero()
		} else {
			target.Set(reflect.ValueOf(value))
		}
	}
	return nil
}
func (tx *foundationTestTx) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	switch {
	case strings.HasPrefix(query, "SELECT id,kind,state"):
		r, ok := tx.db.transactions[hex.EncodeToString(args[0].([]byte))]
		if !ok || r.State != "pending" || !r.ExpiresAt.After(args[1].(time.Time)) {
			return foundationTestRow{err: pgx.ErrNoRows}
		}
		return foundationTestRow{values: []any{r.ID, r.Kind, r.State, r.CreatedAt, r.ExpiresAt, r.CompletedAt}}
	case strings.HasPrefix(query, "SELECT count(*)"):
		count := 0
		var last *time.Time
		for _, c := range tx.db.challenges {
			if c.tx == args[0].(uuid.UUID) {
				count++
				if last == nil || c.created.After(*last) {
					v := c.created
					last = &v
				}
			}
		}
		return foundationTestRow{values: []any{count, last}}
	case strings.HasPrefix(query, "SELECT code_hash"):
		c, ok := tx.db.challenges[args[0].(uuid.UUID)]
		if !ok || c.tx != args[1].(uuid.UUID) {
			return foundationTestRow{err: pgx.ErrNoRows}
		}
		return foundationTestRow{values: []any{c.hash, c.deliverable, c.attempts, c.maximum, c.expires, c.consumed, c.invalidated}}
	case strings.HasPrefix(query, "SELECT EXISTS"):
		c, ok := tx.db.challenges[args[0].(uuid.UUID)]
		return foundationTestRow{values: []any{ok && c.consumed == nil && c.invalidated == nil}}
	default:
		return foundationTestRow{err: errors.New("unexpected fixture query")}
	}
}
func (tx *foundationTestTx) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	switch {
	case strings.HasPrefix(query, "INSERT INTO tutorhub.auth_transactions"):
		tx.db.transactions[hex.EncodeToString(args[1].([]byte))] = AuthTransaction{ID: args[0].(uuid.UUID), Kind: args[2].(string), State: "pending", CreatedAt: args[7].(time.Time), ExpiresAt: args[8].(time.Time)}
	case strings.HasPrefix(query, "UPDATE tutorhub.auth_transactions"):
		for key, r := range tx.db.transactions {
			if r.ID == args[0].(uuid.UUID) {
				now := args[1].(time.Time)
				r.State = "completed"
				r.CompletedAt = &now
				tx.db.transactions[key] = r
			}
		}
	case strings.HasPrefix(query, "UPDATE tutorhub.auth_challenges SET invalidated"):
		for _, c := range tx.db.challenges {
			if c.tx == args[0].(uuid.UUID) && c.consumed == nil && c.invalidated == nil {
				now := args[1].(time.Time)
				c.invalidated = &now
			}
		}
	case strings.HasPrefix(query, "INSERT INTO tutorhub.auth_challenges"):
		tx.db.challenges[args[0].(uuid.UUID)] = &foundationTestChallenge{tx: args[1].(uuid.UUID), destination: args[4].([]byte), hash: args[5].([]byte), deliverable: args[6].(bool), maximum: 5, created: args[7].(time.Time), expires: args[8].(time.Time)}
	case strings.HasPrefix(query, "UPDATE tutorhub.auth_challenges SET attempts"):
		c := tx.db.challenges[args[0].(uuid.UUID)]
		c.attempts = args[1].(int)
		c.consumed = args[2].(*time.Time)
		c.invalidated = args[3].(*time.Time)
	case strings.HasPrefix(query, "INSERT INTO tutorhub.auth_challenge_deliveries"):
		tx.db.deliveries[args[0].(uuid.UUID)] = []any{"queued", nil, "", nil}
	case strings.HasPrefix(query, "UPDATE tutorhub.auth_challenge_deliveries"):
		tx.db.deliveries[args[0].(uuid.UUID)] = append([]any(nil), args[1:]...)
	default:
		return pgconn.CommandTag{}, errors.New("unexpected fixture statement")
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func foundationFixture(t *testing.T, kind string, secure bool) (*foundationTestDB, *Crypto, *TransactionService, *ChallengeService, *http.Cookie) {
	t.Helper()
	crypto, err := NewCrypto(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	db := newFoundationTestDB()
	transactions := NewTransactionService(db, crypto, secure)
	_, cookie, err := transactions.Create(context.Background(), TransactionInput{Kind: kind})
	if err != nil {
		t.Fatal(err)
	}
	challenges := NewChallengeService(db, crypto, nil)
	challenges.code = func() (string, error) { return "420381", nil }
	return db, crypto, transactions, challenges, cookie
}

func TestAuthTransactionBindingCookieAndOneTimeConsume(t *testing.T) {
	for _, secure := range []bool{false, true} {
		db, crypto, transactions, _, cookie := foundationFixture(t, "login", secure)
		if cookie.Name != AuthTransactionCookieName(secure) || !cookie.HttpOnly || cookie.Secure != secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || cookie.MaxAge != 900 {
			t.Fatal("invalid transaction cookie")
		}
		if _, ok := db.transactions[hex.EncodeToString(crypto.Digest("auth-tx-binding", cookie.Value))]; !ok {
			t.Fatal("binding not domain-separated")
		}
		if _, err := transactions.Lookup(context.Background(), strings.Repeat("x", 43)); !errors.Is(err, ErrAuthTransactionUnavailable) {
			t.Fatal("foreign binding accepted")
		}
		if _, err := transactions.Consume(context.Background(), cookie.Value); err != nil {
			t.Fatal(err)
		}
		if _, err := transactions.Consume(context.Background(), cookie.Value); !errors.Is(err, ErrAuthTransactionUnavailable) {
			t.Fatal("transaction replay accepted")
		}
	}
}

func TestAuthChallengeConcurrentVerifyAndHashPrivacy(t *testing.T) {
	db, crypto, _, challenges, cookie := foundationFixture(t, "login", true)
	challenge, err := challenges.Issue(context.Background(), cookie.Value, ChallengeInput{Purpose: "login_code", Channel: DeliveryEmail, Destination: "Private@Example.test", Deliverable: true})
	if err != nil {
		t.Fatal(err)
	}
	stored := db.challenges[challenge.ID]
	if !bytes.Equal(stored.hash, crypto.Digest("auth-otp-v1", challenge.ID.String()+":420381")) || !bytes.Equal(stored.destination, crypto.Digest("auth-dest-v1", "email:private@example.test")) {
		t.Fatal("incorrect challenge hashes")
	}
	var winners atomic.Int32
	var group sync.WaitGroup
	for range 32 {
		group.Add(1)
		go func() {
			defer group.Done()
			ok, err := challenges.Verify(context.Background(), cookie.Value, challenge.ID, "420381")
			if err != nil {
				t.Error(err)
			}
			if ok {
				winners.Add(1)
			}
		}()
	}
	group.Wait()
	if winners.Load() != 1 {
		t.Fatalf("concurrent verification winners=%d", winners.Load())
	}
}

func TestAuthChallengeAttemptsDummyResendAndExpiry(t *testing.T) {
	for _, deliverable := range []bool{false, true} {
		db, _, _, challenges, cookie := foundationFixture(t, "login", false)
		now := time.Now().UTC()
		challenges.now = func() time.Time { return now }
		input := ChallengeInput{Purpose: "login_code", Channel: DeliveryEmail, Destination: "private@example.test", Deliverable: deliverable}
		first, err := challenges.Issue(context.Background(), cookie.Value, input)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := challenges.Issue(context.Background(), cookie.Value, input); !errors.Is(err, ErrAuthChallengeThrottled) {
			t.Fatal("cooldown bypassed")
		}
		for range 5 {
			if ok, err := challenges.Verify(context.Background(), cookie.Value, first.ID, "111111"); ok || err != nil {
				t.Fatal("invalid code accepted")
			}
		}
		if db.challenges[first.ID].attempts != 5 || db.challenges[first.ID].invalidated == nil {
			t.Fatal("attempt limit not persisted")
		}
		if ok, _ := challenges.Verify(context.Background(), cookie.Value, first.ID, "420381"); ok {
			t.Fatal("locked challenge accepted")
		}
		for i := 1; i < 5; i++ {
			now = now.Add(ChallengeCooldown)
			current, err := challenges.Issue(context.Background(), cookie.Value, input)
			if err != nil {
				t.Fatal(err)
			}
			if i == 1 && !deliverable {
				if ok, err := challenges.Verify(context.Background(), cookie.Value, current.ID, "420381"); ok || err != nil {
					t.Fatal("dummy challenge accepted")
				}
				if db.challenges[current.ID].attempts != 1 {
					t.Fatal("dummy bypassed write path")
				}
			}
			if i == 2 {
				if ok, _ := challenges.Verify(context.Background(), cookie.Value, first.ID, "420381"); ok {
					t.Fatal("resend did not invalidate old code")
				}
			}
		}
		now = now.Add(ChallengeCooldown)
		if _, err := challenges.Issue(context.Background(), cookie.Value, input); !errors.Is(err, ErrAuthChallengeThrottled) {
			t.Fatal("send limit bypassed")
		}
		now = now.Add(20 * time.Minute)
		if ok, err := challenges.Verify(context.Background(), cookie.Value, first.ID, "420381"); ok || !errors.Is(err, ErrAuthTransactionUnavailable) {
			t.Fatal("expired transaction accepted")
		}
	}
}

type privacyTestSender struct {
	calls atomic.Int32
	fail  bool
}

type foundationTestBudget bool

func (budget foundationTestBudget) AllowDelivery(context.Context, DeliveryChannel) bool {
	return bool(budget)
}

func (sender *privacyTestSender) SendChallenge(context.Context, ChallengeMessage) (DeliveryReceipt, error) {
	sender.calls.Add(1)
	if sender.fail {
		return DeliveryReceipt{}, errors.New("private@example.test +84901234567 420381")
	}
	return DeliveryReceipt{ProviderMessageRef: "private@example.test +84901234567 420381"}, nil
}

func TestAuthDeliveryPostCommitAndNoSensitiveLogs(t *testing.T) {
	for _, fail := range []bool{false, true} {
		db, crypto, _, challenges, cookie := foundationFixture(t, "login", false)
		sender := &privacyTestSender{fail: fail}
		dispatcher, err := NewDeliveryDispatcher(db, crypto, sender, nil, 2, foundationTestBudget(true))
		if err != nil {
			t.Fatal(err)
		}
		challenges.dispatcher = dispatcher
		var logs bytes.Buffer
		previous := slog.Default()
		slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
		defer slog.SetDefault(previous)
		if _, err := challenges.Issue(context.Background(), cookie.Value, ChallengeInput{Purpose: "login_code", Channel: DeliveryEmail, Destination: "private@example.test", Deliverable: true}); err != nil {
			t.Fatal(err)
		}
		if err := dispatcher.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
		if sender.calls.Load() != 1 {
			t.Fatal("delivery was not dispatched")
		}
		slog.Info("defensive redaction", "message", ChallengeMessage{Destination: "private@example.test", Code: "420381"}, "input", ChallengeInput{Destination: "+84901234567"}, "receipt", DeliveryReceipt{ProviderMessageRef: "private@example.test"})
		for _, secret := range []string{"private@example.test", "+84901234567", "420381"} {
			if strings.Contains(logs.String(), secret) {
				t.Fatal("structured log leaked sensitive data")
			}
		}
		for _, receipt := range db.deliveries {
			if fail && receipt[0] != "failed" {
				t.Fatal("failure receipt missing")
			}
			if !fail {
				ref := receipt[1].(*string)
				if len(*ref) != 64 || strings.Contains(*ref, "private") {
					t.Fatal("provider reference was not redacted")
				}
			}
		}
	}
	db, crypto, _, challenges, cookie := foundationFixture(t, "login", false)
	sender := &privacyTestSender{}
	dispatcher, _ := NewDeliveryDispatcher(db, crypto, sender, nil, 2, foundationTestBudget(true))
	challenges.dispatcher = dispatcher
	db.failCommit = true
	if _, err := challenges.Issue(context.Background(), cookie.Value, ChallengeInput{Purpose: "login_code", Channel: DeliveryEmail, Destination: "private@example.test", Deliverable: true}); err == nil {
		t.Fatal("commit failure ignored")
	}
	_ = dispatcher.Close(context.Background())
	if sender.calls.Load() != 0 {
		t.Fatal("provider called before successful commit")
	}
}

func TestAuthOTPShapeAndSMTPEnvironment(t *testing.T) {
	for range 100 {
		code, err := newOTP()
		if err != nil || !validOTP(code) {
			t.Fatal("OTP must contain exactly six digits")
		}
	}
	for _, test := range []struct{ env, addr string }{{"production", "127.0.0.1:1025"}, {"staging", "[::1]:1025"}, {"test", "192.0.2.1:1025"}, {"test", "mail.example:1025"}} {
		if _, err := NewSMTPSender(test.env, test.addr); err == nil {
			t.Fatal("unsafe SMTP accepted")
		}
	}
	if _, err := NewSMTPSender("test", "127.0.0.1:1025"); err != nil {
		t.Fatal(err)
	}
}

func TestAuthChallengeResendInvalidatesLiveCodeAndPhoneExpiry(t *testing.T) {
	db, _, _, challenges, cookie := foundationFixture(t, "phone", false)
	now := time.Now().UTC()
	challenges.now = func() time.Time { return now }
	input := ChallengeInput{Purpose: "phone_login", Channel: DeliverySMS, Destination: "+84901234567", Deliverable: true}
	first, err := challenges.Issue(context.Background(), cookie.Value, input)
	if err != nil {
		t.Fatal(err)
	}
	if first.ExpiresAt.Sub(db.challenges[first.ID].created) != PhoneChallengeTTL {
		t.Fatal("phone TTL must be five minutes")
	}
	now = now.Add(ChallengeCooldown)
	second, err := challenges.Issue(context.Background(), cookie.Value, input)
	if err != nil {
		t.Fatal(err)
	}
	if db.challenges[first.ID].invalidated == nil {
		t.Fatal("resend did not invalidate live challenge")
	}
	if ok, err := challenges.Verify(context.Background(), cookie.Value, first.ID, "420381"); ok || err != nil {
		t.Fatal("live prior code survived resend")
	}
	now = second.ExpiresAt
	if ok, err := challenges.Verify(context.Background(), cookie.Value, second.ID, "420381"); ok || err != nil {
		t.Fatal("expired phone code verified")
	}
}

func TestAuthChallengeRechecksExpiryAfterLockWait(t *testing.T) {
	_, _, _, challenges, cookie := foundationFixture(t, "login", false)
	challenge, err := challenges.Issue(context.Background(), cookie.Value, ChallengeInput{Purpose: "login_code", Channel: DeliveryEmail, Destination: "private@example.test", Deliverable: true})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	challenges.now = func() time.Time {
		calls++
		if calls == 1 {
			return challenge.ExpiresAt.Add(-time.Second)
		}
		return challenge.ExpiresAt.Add(time.Second)
	}
	if ok, err := challenges.Verify(context.Background(), cookie.Value, challenge.ID, "420381"); ok || err != nil {
		t.Fatal("code expired during lock wait was accepted")
	}
}

func TestAuthDeliveryBudgetFailsClosedAndDummyNeverSends(t *testing.T) {
	for _, test := range []struct {
		name        string
		budget      DeliveryBudget
		deliverable bool
	}{
		{"missing budget", nil, true}, {"quota denied", foundationTestBudget(false), true}, {"dummy", foundationTestBudget(true), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, crypto, _, challenges, cookie := foundationFixture(t, "login", false)
			sender := &privacyTestSender{}
			dispatcher, err := NewDeliveryDispatcher(db, crypto, sender, nil, 1, test.budget)
			if err != nil {
				t.Fatal(err)
			}
			challenges.dispatcher = dispatcher
			if _, err := challenges.Issue(context.Background(), cookie.Value, ChallengeInput{Purpose: "login_code", Channel: DeliveryEmail, Destination: "private@example.test", Deliverable: test.deliverable}); err != nil {
				t.Fatal(err)
			}
			if err := dispatcher.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			if sender.calls.Load() != 0 {
				t.Fatal("sender invoked without deliverable quota reservation")
			}
			for _, receipt := range db.deliveries {
				if receipt[0] != "skipped" {
					t.Fatal("missing skipped delivery receipt")
				}
			}
		})
	}
}

func TestAuthDeliveryQueueIsBoundedAndRejectsAfterClose(t *testing.T) {
	dispatcher := &DeliveryDispatcher{queue: make(chan deliveryJob, 1)}
	if !dispatcher.enqueueCommitted(deliveryJob{}) || dispatcher.enqueueCommitted(deliveryJob{}) {
		t.Fatal("queue bound not enforced")
	}
	<-dispatcher.queue
	dispatcher.closed = true
	if dispatcher.enqueueCommitted(deliveryJob{}) {
		t.Fatal("closed dispatcher admitted OTP")
	}
}
