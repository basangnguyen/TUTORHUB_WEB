//go:build integration

package httpapi

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
)

func TestPhonePostgresCountryQuotaAndBreaker(t *testing.T) {
	url := os.Getenv("AUTH_TEST_DATABASE_URL")
	if url == "" || os.Getenv("AUTH_TEST_DISPOSABLE") != "true" {
		t.Skip("requires disposable auth database")
	}
	ctx := t.Context()
	if err := migrationrunner.Up(ctx, url); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	h := testPhoneGuard()
	now := time.Now().UTC()
	h.auth.clock = func() time.Time { return now }
	vn := identity.NormalizedPhoneNumber{E164: "+84912345678", CountryCode: "VN"}
	us := identity.NormalizedPhoneNumber{E164: "+12025550124", CountryCode: "US"}
	_, err = tx.Exec(ctx, `WITH p AS (INSERT INTO tutorhub.auth_transactions(binding_hash,kind,created_at,expires_at)
		VALUES (decode(repeat('3c',32),'hex'),'phone',$1::timestamptz,$1::timestamptz+interval '5 minutes') RETURNING id)
		INSERT INTO tutorhub.auth_challenges(transaction_id,purpose,channel,destination_hash,code_hash,deliverable,phone_country_code,created_at,expires_at,consumed_at)
		SELECT p.id,'phone_login','sms',decode(repeat('01',32),'hex'),decode(repeat('02',32),'hex'),true,'VN',$1::timestamptz,$1::timestamptz+interval '5 minutes',
		CASE WHEN n<=14 THEN $1::timestamptz END FROM p CROSS JOIN generate_series(1,50) n`, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.phoneSendGuard(ctx, tx, vn, false); !errors.Is(err, identity.ErrPhoneDeliveryUnavailable) {
		t.Fatal("VN breaker", err)
	}
	h.auth.config.Authentication.PhoneCountryHourlyQuota = 1
	if ok, err := h.phoneSendGuard(ctx, tx, us, false); !ok || err != nil {
		t.Fatal("isolated country denied", err)
	}
	if _, err = h.phoneSendGuard(ctx, tx, us, false); !errors.Is(err, identity.ErrPhoneDeliveryUnavailable) {
		t.Fatal("country hourly quota", err)
	}
	_, err = tx.Exec(ctx, `UPDATE tutorhub.auth_challenges SET consumed_at=$1 WHERE id=(SELECT id FROM tutorhub.auth_challenges WHERE phone_country_code='VN' AND consumed_at IS NULL LIMIT 1)`, now)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := h.phoneSendGuard(ctx, tx, vn, false); !ok || err != nil {
		t.Fatal("30 percent recovery", err)
	}
	h.auth.config.Authentication.PhoneOTPDailyQuota = 50
	if _, err = h.phoneSendGuard(ctx, tx, us, false); !errors.Is(err, identity.ErrPhoneDeliveryUnavailable) {
		t.Fatal("global daily reservation", err)
	}
}
