package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/tutorhub-v2/core-api/internal/platform/devinbox"
)

type accountNoticeBudget struct {
	allowed bool
	calls   int
}

func (budget *accountNoticeBudget) AllowDelivery(_ context.Context, channel DeliveryChannel) bool {
	budget.calls++
	return budget.allowed && channel == DeliveryEmail
}

func TestPasswordChangedNoticeUsesBudgetWithoutOTPOrDatabase(t *testing.T) {
	for _, allowed := range []bool{false, true} {
		inbox, err := devinbox.New("test", 10, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		budget := &accountNoticeBudget{allowed: allowed}
		// No database is configured: a consumed recovery OTP must not gate a notice.
		dispatcher := &DeliveryDispatcher{email: NewDevinboxSender(inbox, DeliveryEmail), budget: budget}
		dispatcher.deliver(deliveryJob{notice: &AccountNotice{Kind: "password_changed", Destination: "private@example.test", ExpiresAt: time.Now().Add(time.Minute)}})
		items := inbox.List()
		if budget.calls != 1 || allowed && len(items) != 1 || !allowed && len(items) != 0 {
			t.Fatal("security notice bypassed quota or was incorrectly challenge-gated")
		}
		if allowed {
			encoded, err := json.Marshal(items[0])
			if err != nil || items[0].NoticeKind != "password_changed" || items[0].Code != "" || items[0].ChallengeID != "" || strings.Contains(string(encoded), `"code":`) {
				t.Fatal("password notice must not contain a verification code")
			}
		}
		dispatcher.deliver(deliveryJob{notice: &AccountNotice{Kind: "password_changed", ExpiresAt: time.Now().Add(-time.Second)}})
		if budget.calls != 1 {
			t.Fatal("expired notice reserved delivery budget")
		}
		inbox.Close()
	}
}

func TestNativeAccountInputsAndRotationResultRedactLogs(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	current := "secret-current-password"
	logger.Info("auth", "verify", EmailCodeVerifyInput{Code: "420381"}, "recovery", PasswordRecoveryCompleteInput{Code: "420381", NewPassword: "secret-new-password"},
		"update", UpdateMyPasswordInput{CurrentPassword: &current, NewPassword: "secret-new-password"}, "result", UpdateMyPasswordResult{SessionToken: "private-session-token", CSRFToken: "private-csrf-token"})
	for _, value := range []string{current, "secret-new-password", "420381", "private-session-token", "private-csrf-token"} {
		if strings.Contains(output.String(), value) {
			t.Fatal("native account operation leaked sensitive fields to logs")
		}
	}
}
