//go:build integration

package identity

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAuthFoundationPostgresChallengeLifecycleAndConcurrentVerify(t *testing.T) {
	ctx, pool, _ := authSchemaTestDatabase(t)
	crypto, err := NewCrypto(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	transactions := NewTransactionService(pool, crypto, true)
	challenges := NewChallengeService(pool, crypto, nil)
	challenges.code = func() (string, error) { return "420381", nil }
	var transactionIDs []uuid.UUID
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		// Fixture cleanup uses the explicitly disposable owner, never runtime ACL.
		if _, err := pool.Exec(cleanup, deleteFixtureDeliveries+"(SELECT unnest($1::uuid[]))", transactionIDs); err != nil {
			t.Error("clean fixture deliveries", err)
			return
		}
		for _, id := range transactionIDs {
			if _, err := pool.Exec(cleanup, "DELETE FROM tutorhub.auth_transactions WHERE id=$1", id); err != nil {
				t.Error("clean up disposable authentication transaction")
			}
		}
	})
	create := func(kind string) string {
		t.Helper()
		transaction, cookie, err := transactions.Create(ctx, TransactionInput{Kind: kind})
		if err != nil {
			t.Fatal(err)
		}
		transactionIDs = append(transactionIDs, transaction.ID)
		return cookie.Value
	}
	input := ChallengeInput{Purpose: "login_code", Channel: DeliveryEmail, Destination: "fixture@example.test", Deliverable: true}
	binding := create("login")
	challenge, err := challenges.Issue(ctx, binding, input)
	if err != nil {
		t.Fatal(err)
	}
	var codeLength, destinationLength int
	var ttl time.Duration
	var ttlSeconds float64
	if err := pool.QueryRow(ctx, `SELECT octet_length(code_hash),octet_length(destination_hash),extract(epoch FROM expires_at-created_at)::float8
		FROM tutorhub.auth_challenges WHERE id=$1`, challenge.ID).Scan(&codeLength, &destinationLength, &ttlSeconds); err != nil {
		t.Fatal("read challenge hash lengths")
	}
	ttl = time.Duration(ttlSeconds) * time.Second
	if codeLength != 32 || destinationLength != 32 || ttl != EmailChallengeTTL {
		t.Fatal("challenge hash/TTL constraints differ from policy")
	}
	if _, err := challenges.Issue(ctx, binding, input); !errors.Is(err, ErrAuthChallengeThrottled) {
		t.Fatal("PostgreSQL cooldown was bypassed")
	}
	otherBinding := create("login")
	if ok, err := challenges.Verify(ctx, otherBinding, challenge.ID, "420381"); ok || err != nil {
		t.Fatal("challenge was not bound to its browser transaction")
	}
	var winners atomic.Int32
	var group sync.WaitGroup
	start := make(chan struct{})
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			ok, err := challenges.Verify(ctx, binding, challenge.ID, "420381")
			if err != nil {
				t.Error(err)
			}
			if ok {
				winners.Add(1)
			}
		}()
	}
	close(start)
	group.Wait()
	if winners.Load() != 1 {
		t.Fatalf("PostgreSQL concurrent verification winners=%d", winners.Load())
	}
	if _, err := transactions.Consume(ctx, binding); err != nil {
		t.Fatal(err)
	}
	if _, err := transactions.Consume(ctx, binding); !errors.Is(err, ErrAuthTransactionUnavailable) {
		t.Fatal("completed transaction was replayable")
	}

	for _, deliverable := range []bool{true, false} {
		binding := create("login")
		input.Deliverable = deliverable
		challenge, err := challenges.Issue(ctx, binding, input)
		if err != nil {
			t.Fatal(err)
		}
		code := "000000"
		if !deliverable {
			code = "420381"
		}
		for range 5 {
			if ok, err := challenges.Verify(ctx, binding, challenge.ID, code); ok || err != nil {
				t.Fatal("invalid/dummy challenge verified")
			}
		}
		var attempts int
		var invalidated bool
		if err := pool.QueryRow(ctx, `SELECT attempts,invalidated_at IS NOT NULL FROM tutorhub.auth_challenges WHERE id=$1`, challenge.ID).Scan(&attempts, &invalidated); err != nil || attempts != 5 || !invalidated {
			t.Fatal("PostgreSQL attempt cap was not persisted")
		}
	}

	phoneBinding := create("phone")
	phone, err := challenges.Issue(ctx, phoneBinding, ChallengeInput{Purpose: "phone_login", Channel: DeliverySMS, Destination: "+84901234567", Deliverable: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT extract(epoch FROM expires_at-created_at)::float8 FROM tutorhub.auth_challenges WHERE id=$1`, phone.ID).Scan(&ttlSeconds); err != nil || ttlSeconds != PhoneChallengeTTL.Seconds() {
		t.Fatal("phone TTL differs from policy")
	}
}

func TestPhonePostgresDeliveryRecordsActualFallback(t *testing.T) {
	ctx, pool, _ := authSchemaTestDatabase(t)
	c, err := NewCrypto(bytes.Repeat([]byte{8}, 32))
	if err != nil {
		t.Fatal(err)
	}
	router, err := NewPhoneSenderRouter(map[int][]PhoneRoute{84: {
		{Provider: "zns", Sender: &phoneTestSender{channel: "zalo_zns", err: ErrAuthDeliveryFailed}},
		{Provider: "sms_brandname", Sender: &phoneTestSender{channel: DeliverySMS}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	dispatcher, err := NewDeliveryDispatcher(pool, c, nil, router, 2, foundationTestBudget(true))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dispatcher.Close(context.Background()) })
	tx, cookie, err := NewTransactionService(pool, c, false).Create(ctx, TransactionInput{Kind: "phone"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, deleteFixtureDeliveries+"($1)", tx.ID); err != nil {
			t.Error("clean fixture phone deliveries", err)
			return
		}
		if _, err := pool.Exec(context.Background(), "DELETE FROM tutorhub.auth_transactions WHERE id=$1", tx.ID); err != nil {
			t.Error("clean up disposable phone transaction")
		}
	})
	challenge, err := NewChallengeService(pool, c, dispatcher).Issue(ctx, cookie.Value, ChallengeInput{
		Purpose: "phone_login", Channel: DeliverySMS, Destination: "+84912345678", Deliverable: true, PhoneCountryCode: "VN",
	})
	if err != nil || dispatcher.Close(ctx) != nil {
		t.Fatal("could not issue and drain phone challenge")
	}
	var total, failed, accepted int
	err = pool.QueryRow(ctx, `SELECT count(*),
		count(*) FILTER (WHERE channel='zalo_zns' AND provider='zns' AND status='failed' AND error_code='send_failed'),
		count(*) FILTER (WHERE channel='sms' AND provider='sms_brandname' AND status='accepted' AND length(provider_message_ref)=64 AND delivered_at IS NOT NULL)
		FROM tutorhub.auth_challenge_deliveries WHERE challenge_id=$1`, challenge.ID).Scan(&total, &failed, &accepted)
	if err != nil || total != 2 || failed != 1 || accepted != 1 {
		t.Fatal("database receipts did not match transport attempts")
	}
}
