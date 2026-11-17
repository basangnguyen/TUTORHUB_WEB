package identity

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/devinbox"
)

type DeliveryChannel string

const (
	DeliveryEmail DeliveryChannel = "email"
	DeliverySMS   DeliveryChannel = "sms"
)

var (
	ErrAuthDeliveryDisabled = errors.New("authentication delivery is disabled")
	ErrAuthDeliveryFailed   = errors.New("authentication delivery failed")
)

// ChallengeMessage is deliberately ephemeral: it is never serialized to the DB,
// outbox or logs. The development inbox is the only opt-in plaintext projection.
type ChallengeMessage struct {
	ChallengeID uuid.UUID
	Purpose     string
	Channel     DeliveryChannel
	Destination string
	Code        string
	Locale      string
	ExpiresAt   time.Time
}

func (ChallengeMessage) LogValue() slog.Value { return slog.StringValue("[redacted auth message]") }
func (ChallengeMessage) String() string       { return "[redacted auth message]" }

type DeliveryReceipt struct {
	ProviderMessageRef string
	AcceptedAt         time.Time
}

func (DeliveryReceipt) LogValue() slog.Value { return slog.StringValue("[redacted delivery receipt]") }
func (DeliveryReceipt) String() string       { return "[redacted delivery receipt]" }

type EmailSender interface {
	SendChallenge(context.Context, ChallengeMessage) (DeliveryReceipt, error)
	SendNotice(context.Context, AccountNotice) (DeliveryReceipt, error)
}

// AccountNotice contains no code. It shares the same bounded post-commit queue
// and email budget; its destination exists only in memory and the local inbox.
type AccountNotice struct {
	ChallengeID uuid.UUID
	Kind        string
	Destination string
	ExpiresAt   time.Time
}

func (AccountNotice) String() string       { return "[redacted account notice]" }
func (AccountNotice) LogValue() slog.Value { return slog.StringValue("[redacted account notice]") }

type PhoneChallengeSender interface {
	Channel() DeliveryChannel
	Supports(countryCallingCode int) bool
	SendChallenge(context.Context, ChallengeMessage) (DeliveryReceipt, error)
}

type DisabledSender struct{}

func (DisabledSender) SendChallenge(context.Context, ChallengeMessage) (DeliveryReceipt, error) {
	return DeliveryReceipt{}, ErrAuthDeliveryDisabled
}
func (DisabledSender) SendNotice(context.Context, AccountNotice) (DeliveryReceipt, error) {
	return DeliveryReceipt{}, ErrAuthDeliveryDisabled
}
func (DisabledSender) Channel() DeliveryChannel { return DeliverySMS }
func (DisabledSender) Supports(int) bool        { return false }

type DevinboxSender struct {
	inbox   *devinbox.Inbox
	channel DeliveryChannel
}

func NewDevinboxSender(inbox *devinbox.Inbox, channel DeliveryChannel) *DevinboxSender {
	return &DevinboxSender{inbox: inbox, channel: channel}
}

func (sender *DevinboxSender) Channel() DeliveryChannel { return sender.channel }
func (sender *DevinboxSender) Supports(code int) bool   { return code == 84 }
func (sender *DevinboxSender) SendChallenge(ctx context.Context, message ChallengeMessage) (DeliveryReceipt, error) {
	if ctx.Err() != nil || sender.inbox == nil || message.Channel != sender.channel {
		return DeliveryReceipt{}, ErrAuthDeliveryFailed
	}
	if err := sender.inbox.Add(devinbox.Message{Kind: "challenge", ChallengeID: message.ChallengeID.String(), Purpose: message.Purpose,
		Channel: string(message.Channel), Destination: message.Destination, Code: message.Code, ExpiresAt: message.ExpiresAt}); err != nil {
		return DeliveryReceipt{}, ErrAuthDeliveryFailed
	}
	return DeliveryReceipt{ProviderMessageRef: uuid.NewString(), AcceptedAt: time.Now().UTC()}, nil
}

func (sender *DevinboxSender) SendNotice(ctx context.Context, notice AccountNotice) (DeliveryReceipt, error) {
	if ctx.Err() != nil || sender.inbox == nil || sender.channel != DeliveryEmail || (notice.Kind != "account_exists" && notice.Kind != "password_changed") {
		return DeliveryReceipt{}, ErrAuthDeliveryFailed
	}
	purpose, challengeID := "signup_email", notice.ChallengeID.String()
	if notice.Kind == "password_changed" {
		purpose, challengeID = "password_changed", ""
	}
	if err := sender.inbox.Add(devinbox.Message{Kind: "notice", NoticeKind: notice.Kind, ChallengeID: challengeID, Purpose: purpose, Channel: "email", Destination: notice.Destination, ExpiresAt: notice.ExpiresAt}); err != nil {
		return DeliveryReceipt{}, ErrAuthDeliveryFailed
	}
	return DeliveryReceipt{ProviderMessageRef: uuid.NewString(), AcceptedAt: time.Now().UTC()}, nil
}

// SMTPSender only supports unauthenticated Mailpit-style loopback delivery.
// Paid or production providers must supply separate adapters in later slices.
type SMTPSender struct{ address, host string }

func NewSMTPSender(environment, address string) (*SMTPSender, error) {
	host, port, err := net.SplitHostPort(address)
	if host == "localhost" {
		host = "127.0.0.1"
		address = net.JoinHostPort(host, port)
	}
	ip := net.ParseIP(host)
	if (environment != "development" && environment != "test") || err != nil || port == "" || ip == nil || !ip.IsLoopback() {
		return nil, ErrAuthFoundationInput
	}
	return &SMTPSender{address: address, host: host}, nil
}

func (sender *SMTPSender) SendChallenge(ctx context.Context, message ChallengeMessage) (DeliveryReceipt, error) {
	if message.Channel != DeliveryEmail || !validOTP(message.Code) || !message.ExpiresAt.After(time.Now()) {
		return DeliveryReceipt{}, ErrAuthDeliveryFailed
	}
	return sender.send(ctx, message.Destination, "TutorHub verification code", "Your verification code: "+message.Code)
}

func (sender *SMTPSender) SendNotice(ctx context.Context, notice AccountNotice) (DeliveryReceipt, error) {
	if !notice.ExpiresAt.After(time.Now()) {
		return DeliveryReceipt{}, ErrAuthDeliveryFailed
	}
	switch notice.Kind {
	case "account_exists":
		return sender.send(ctx, notice.Destination, "TutorHub account notice", "An account already exists for this email address. Please sign in using your existing sign-in method.")
	case "password_changed":
		return sender.send(ctx, notice.Destination, "TutorHub password changed", "Your TutorHub password has been changed. If you did not make this change, recover your account and contact support.")
	default:
		return DeliveryReceipt{}, ErrAuthDeliveryFailed
	}
}

func (sender *SMTPSender) send(ctx context.Context, destination, subject, body string) (DeliveryReceipt, error) {
	address, err := mail.ParseAddress(destination)
	if err != nil || address.Address != destination || strings.ContainsAny(destination, "\r\n") {
		return DeliveryReceipt{}, ErrAuthDeliveryFailed
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	connection, err := (&net.Dialer{}).DialContext(ctx, "tcp", sender.address)
	if err != nil {
		return DeliveryReceipt{}, ErrAuthDeliveryFailed
	}
	defer func() { _ = connection.Close() }()
	deadline, _ := ctx.Deadline()
	if connection.SetDeadline(deadline) != nil {
		return DeliveryReceipt{}, ErrAuthDeliveryFailed
	}
	client, err := smtp.NewClient(connection, sender.host)
	if err != nil {
		return DeliveryReceipt{}, ErrAuthDeliveryFailed
	}
	defer func() { _ = client.Close() }()
	if client.Mail("auth@tutorhub.local") != nil || client.Rcpt(address.Address) != nil {
		return DeliveryReceipt{}, ErrAuthDeliveryFailed
	}
	writer, err := client.Data()
	if err != nil {
		return DeliveryReceipt{}, ErrAuthDeliveryFailed
	}
	_, err = fmt.Fprintf(writer, "From: TutorHub <auth@tutorhub.local>\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s\r\n", address.Address, subject, body)
	closeErr := writer.Close()
	if err != nil || closeErr != nil || client.Quit() != nil {
		return DeliveryReceipt{}, ErrAuthDeliveryFailed
	}
	return DeliveryReceipt{ProviderMessageRef: uuid.NewString(), AcceptedAt: time.Now().UTC()}, nil
}

type deliveryJob struct {
	id          uuid.UUID
	message     ChallengeMessage
	deliverable bool
	notice      *AccountNotice
}

// DeliveryBudget reserves a daily slot in shared storage. Implementations must
// return false on storage failure; no sender is invoked without a reservation.
type DeliveryBudget interface {
	AllowDelivery(context.Context, DeliveryChannel) bool
}

// DeliveryDispatcher owns a bounded, non-durable queue and one worker. The only
// enqueue calls occur only after the authentication mutation has committed.
type DeliveryDispatcher struct {
	database Database
	crypto   *Crypto
	email    EmailSender
	phone    PhoneChallengeSender
	budget   DeliveryBudget
	queue    chan deliveryJob
	mu       sync.Mutex
	closed   bool
	done     chan struct{}
}

func NewDeliveryDispatcher(database Database, crypto *Crypto, email EmailSender, phone PhoneChallengeSender, capacity int, budgets ...DeliveryBudget) (*DeliveryDispatcher, error) {
	if database == nil || crypto == nil || capacity < 1 || capacity > 10000 {
		return nil, ErrAuthFoundationInput
	}
	if email == nil {
		email = DisabledSender{}
	}
	if phone == nil {
		phone = DisabledSender{}
	}
	result := &DeliveryDispatcher{database: database, crypto: crypto, email: email, phone: phone,
		queue: make(chan deliveryJob, capacity), done: make(chan struct{})}
	if len(budgets) > 0 {
		result.budget = budgets[0]
	}
	go result.run()
	return result, nil
}

func (dispatcher *DeliveryDispatcher) enqueueCommitted(job deliveryJob) bool {
	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()
	if dispatcher.closed {
		return false
	}
	select {
	case dispatcher.queue <- job:
		return true
	default:
		return false
	}
}

func (dispatcher *DeliveryDispatcher) Close(ctx context.Context) error {
	dispatcher.mu.Lock()
	if !dispatcher.closed {
		dispatcher.closed = true
		close(dispatcher.queue)
	}
	dispatcher.mu.Unlock()
	select {
	case <-dispatcher.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (dispatcher *DeliveryDispatcher) run() {
	defer close(dispatcher.done)
	for job := range dispatcher.queue {
		dispatcher.deliver(job)
		job = deliveryJob{}
	}
}

func (dispatcher *DeliveryDispatcher) provider(channel DeliveryChannel) string {
	var sender any = dispatcher.email
	if channel != DeliveryEmail {
		sender = dispatcher.phone
	}
	switch sender.(type) {
	case DisabledSender, *DisabledSender:
		return "disabled"
	case *DevinboxSender:
		return "devinbox"
	case *SMTPSender:
		return "smtp"
	default:
		// Injected test adapters use the disabled provider label, never a raw
		// provider-controlled string. Future paid adapters extend this switch.
		return "disabled"
	}
}

func (dispatcher *DeliveryDispatcher) deliver(job deliveryJob) {
	// A security notice has no challenge/delivery row. Keep it independent from
	// OTP validity: recovery has already consumed its challenge at this point.
	if job.id == uuid.Nil && job.notice != nil && job.notice.Kind == "password_changed" && job.notice.ChallengeID == uuid.Nil {
		if !job.notice.ExpiresAt.After(time.Now()) {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if dispatcher.budget != nil && dispatcher.budget.AllowDelivery(ctx, DeliveryEmail) {
			_, _ = dispatcher.email.SendNotice(ctx, *job.notice)
		}
		return
	}
	status, code := "skipped", "not_deliverable"
	var receipt DeliveryReceipt
	if (job.deliverable || job.notice != nil) && job.message.ExpiresAt.After(time.Now()) && dispatcher.challengeActive(job.message.ChallengeID) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if dispatcher.budget == nil || !dispatcher.budget.AllowDelivery(ctx, job.message.Channel) {
			cancel()
			dispatcher.record(job.id, "skipped", "delivery_quota_denied", DeliveryReceipt{})
			return
		}
		var err error
		if job.notice != nil {
			receipt, err = dispatcher.email.SendNotice(ctx, *job.notice)
		} else if job.message.Channel == DeliveryEmail {
			receipt, err = dispatcher.email.SendChallenge(ctx, job.message)
		} else if job.message.Channel == dispatcher.phone.Channel() {
			receipt, err = dispatcher.phone.SendChallenge(ctx, job.message)
		} else {
			err = ErrAuthDeliveryDisabled
		}
		cancel()
		switch {
		case errors.Is(err, ErrAuthDeliveryDisabled):
			status, code = "disabled", "sender_disabled"
		case err != nil:
			status, code = "failed", "send_failed"
		default:
			status, code = "accepted", ""
		}
	} else if job.deliverable {
		code = "challenge_unavailable"
	}
	dispatcher.record(job.id, status, code, receipt)
}

func (dispatcher *DeliveryDispatcher) challengeActive(id uuid.UUID) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := dispatcher.database.Begin(ctx)
	if err != nil {
		return false
	}
	defer rollback(tx)
	var active bool
	err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM tutorhub.auth_challenges c
		JOIN tutorhub.auth_transactions t ON t.id=c.transaction_id
		WHERE c.id=$1 AND c.consumed_at IS NULL AND c.invalidated_at IS NULL
		AND c.expires_at>now() AND t.state='pending' AND t.expires_at>now())`, id).Scan(&active)
	return err == nil && active
}

func (dispatcher *DeliveryDispatcher) record(id uuid.UUID, status, code string, receipt DeliveryReceipt) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := dispatcher.database.Begin(ctx)
	if err != nil {
		return
	}
	defer rollback(tx)
	var reference *string
	var acceptedAt *time.Time
	if status == "accepted" {
		// Never trust even an apparently opaque provider reference to be PII-free.
		redacted := hex.EncodeToString(dispatcher.crypto.Digest("auth-delivery-ref-v1", receipt.ProviderMessageRef))
		reference = &redacted
		now := time.Now().UTC()
		acceptedAt = &now
	}
	_, err = tx.Exec(ctx, `UPDATE tutorhub.auth_challenge_deliveries
		SET status=$2,provider_message_ref=$3,error_code=NULLIF($4,''),delivered_at=$5 WHERE id=$1 AND status='queued'`,
		id, status, reference, code, acceptedAt)
	if err == nil {
		_ = tx.Commit(ctx)
	}
}
