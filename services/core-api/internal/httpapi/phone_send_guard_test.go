package httpapi

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
)

type phoneQuotaRow func(...any) error

func (r phoneQuotaRow) Scan(values ...any) error { return r(values...) }

type phoneQuotaTx struct {
	pgx.Tx
	reserved, dispatched, sent, consumed int64
	denied                               InvitationRateLimitAction
}

func (phoneQuotaTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("SELECT 1"), nil
}
func (tx phoneQuotaTx) QueryRow(_ context.Context, q string, args ...any) pgx.Row {
	return phoneQuotaRow(func(dest ...any) error {
		if len(dest) == 4 {
			for i, v := range []int64{tx.reserved, tx.dispatched, tx.sent, tx.consumed} {
				*dest[i].(*int64) = v
			}
			return nil
		}
		if strings.HasPrefix(q, "INSERT") && args[0] == string(tx.denied) {
			return pgx.ErrNoRows
		}
		*dest[0].(*int64) = 1
		return nil
	})
}

func testPhoneGuard() nativeAuthHandlers {
	var h nativeAuthHandlers
	h.auth.config.Authentication = config.AuthenticationConfig{PhoneSender: "devinbox", PhoneOTPDailyQuota: 100, PhoneCountryHourlyQuota: 100, PhoneBreakerMinSends: 50, PhoneBreakerMinConversionPct: 30}
	h.auth.config.Database.QueryTimeout = time.Second
	h.auth.clock = time.Now
	return h
}

func TestPhoneSendGuardQuotasAndConversion(t *testing.T) {
	number := identity.NormalizedPhoneNumber{E164: "+84912345678", CountryCode: "VN"}
	for _, tc := range []struct {
		name string
		tx   phoneQuotaTx
		want bool
	}{
		{"49 sends", phoneQuotaTx{sent: 49}, true},
		{"below 30 percent", phoneQuotaTx{sent: 50, consumed: 14}, false},
		{"exactly 30 percent", phoneQuotaTx{sent: 50, consumed: 15}, true},
		{"pending daily quota", phoneQuotaTx{reserved: 100}, false},
		{"dispatched daily quota", phoneQuotaTx{dispatched: 100}, false},
		{"country full", phoneQuotaTx{denied: AuthRateLimitOTPSendCountry}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := testPhoneGuard()
			ok, err := h.phoneSendGuard(t.Context(), tc.tx, number, false)
			if ok != tc.want || tc.want && err != nil || !tc.want && !errors.Is(err, identity.ErrPhoneDeliveryUnavailable) {
				t.Fatal("admission", ok, err)
			}
		})
	}
	h := testPhoneGuard()
	tx := phoneQuotaTx{denied: AuthRateLimitOTPSendDestination}
	if send, err := h.phoneSendGuard(t.Context(), tx, number, false); send || err != nil {
		t.Fatal("public destination limit should issue a dummy challenge", err)
	}
	var limited *phoneDestinationLimitError
	if _, err := h.phoneSendGuard(t.Context(), tx, number, true); !errors.As(err, &limited) || limited.RetryAfter <= 0 {
		t.Fatal("settings destination limit", err)
	}
	h.auth.config.Authentication.PhoneOTPDailyQuota = 0
	if _, err := h.phoneSendGuard(t.Context(), nil, number, false); !errors.Is(err, identity.ErrPhoneDeliveryUnavailable) {
		t.Fatal("zero daily quota", err)
	}
	h = testPhoneGuard()
	h.auth.config.Authentication.PhoneSender = "disabled"
	if _, err := h.phoneSendGuard(t.Context(), nil, number, true); !errors.Is(err, identity.ErrPhoneDeliveryUnavailable) {
		t.Fatal("disabled sender", err)
	}
}
