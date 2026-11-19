package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Use the normal encrypted payload, challenge engine, expiry checks and owner
// selection. The database double supplies only the rows for this registration.
type registrationTestDB struct {
	*foundationTestDB
	row                             AuthTransaction
	bindingHash, ciphertext, digest []byte
	owner                           uuid.UUID
	linkOwner                       uuid.UUID
	existing                        bool
}

type registrationTestTx struct {
	pgx.Tx
	reg *registrationTestDB
}

func (db *registrationTestDB) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := db.foundationTestDB.Begin(ctx)
	return &registrationTestTx{Tx: tx, reg: db}, err
}

func (tx *registrationTestTx) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	db := tx.reg
	switch {
	case strings.HasPrefix(query, "SELECT id,kind,created_at"):
		r := db.row
		if r.State != "pending" || !bytes.Equal(args[0].([]byte), db.bindingHash) || !r.ExpiresAt.After(args[1].(time.Time)) {
			return foundationTestRow{err: pgx.ErrNoRows}
		}
		return foundationTestRow{values: []any{r.ID, r.Kind, r.CreatedAt, r.ExpiresAt, db.ciphertext, db.digest}}
	case strings.Contains(query, "AND destination_hash=$3"):
		c := db.challenges[args[0].(uuid.UUID)]
		return foundationTestRow{values: []any{c != nil && c.tx == args[1].(uuid.UUID) && bytes.Equal(c.destination, args[2].([]byte))}}
	case strings.HasPrefix(query, "SELECT id,user_id,status"), strings.HasPrefix(query, "SELECT m.tenant_id"):
		return foundationTestRow{err: pgx.ErrNoRows}
	case strings.HasPrefix(query, "INSERT INTO tutorhub.users"):
		if db.existing {
			return foundationTestRow{err: pgx.ErrNoRows}
		}
		return foundationTestRow{values: []any{db.owner}}
	case strings.HasPrefix(query, "SELECT id FROM tutorhub.users"):
		if !db.existing {
			return foundationTestRow{err: pgx.ErrNoRows}
		}
		return foundationTestRow{values: []any{db.owner}}
	case strings.HasPrefix(query, "INSERT INTO tutorhub.identities"):
		return foundationTestRow{values: []any{uuid.New()}}
	default:
		return tx.Tx.QueryRow(ctx, query, args...)
	}
}

func (tx *registrationTestTx) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	switch {
	case strings.HasPrefix(query, "UPDATE tutorhub.auth_transactions SET payload_ciphertext"):
		tx.reg.ciphertext = args[1].([]byte)
	case strings.HasPrefix(query, "UPDATE tutorhub.auth_transactions SET state='completed'"):
		tx.reg.row.State = "completed"
	case strings.Contains(query, "VALUES($1,'link_intent','pending'"):
		tx.reg.linkOwner = args[1].(uuid.UUID)
	case strings.HasPrefix(query, "SELECT pg_advisory_xact_lock"),
		strings.HasPrefix(query, "UPDATE tutorhub.users SET email_verified_at"),
		strings.HasPrefix(query, "INSERT INTO tutorhub.user_legal_acceptances"),
		strings.HasPrefix(query, "INSERT INTO tutorhub.sessions"),
		strings.HasPrefix(query, "UPDATE tutorhub.identities SET last_authenticated_at"):
	default:
		return tx.Tx.Exec(ctx, query, args...)
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func TestPendingRegistrationProofAndOwnerSelection(t *testing.T) {
	for _, scenario := range []string{"new user", "existing owner", "wrong code", "expired code", "foreign challenge", "destination throttled", "signup disabled", "resend", "Microsoft new user", "Microsoft existing owner", "Microsoft disabled provider", "Microsoft blocked tenant"} {
		t.Run(scenario, func(t *testing.T) {
			service, _, _ := nativeUnitFixture(t)
			now := time.Now().UTC()
			ctx := context.Background()
			service.now = func() time.Time { return now }
			const binding = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			p := FederationIdentityPayload{ProviderSlug: "facebook", Subject: "12345", Issuer: "https://facebook.test", ReturnTo: "/app", Email: "hint@example.test"}
			if strings.HasPrefix(scenario, "Microsoft ") {
				p.ProviderSlug, p.ProviderOrgID, p.ProviderStableID = "microsoft", uuid.NewString(), uuid.NewString()
				p.Issuer = microsoftIssuer(p.ProviderOrgID)
				service.ConfigureRegistrationProviders(microsoftTestRegistry(t, &MicrosoftProvider{tenantMode: "common"}))
			}
			raw, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			ciphertext, err := service.crypto.Encrypt(string(raw))
			if err != nil {
				t.Fatal(err)
			}
			db := &registrationTestDB{foundationTestDB: newFoundationTestDB(), owner: uuid.New(), existing: strings.HasSuffix(scenario, "existing owner"),
				row:        AuthTransaction{ID: uuid.New(), Kind: registrationKind, State: "pending", CreatedAt: now, ExpiresAt: now.Add(AuthTransactionTTL)},
				ciphertext: ciphertext, bindingHash: service.crypto.Digest(registrationBindingPurpose, binding), digest: service.crypto.ProviderSubjectDigest(p.Issuer, p.Subject)}
			service.database = db
			service.challenges = NewChallengeService(db, service.crypto, nil)
			service.challenges.now = service.now
			service.challenges.code = func() (string, error) { return "420381", nil }
			view, err := service.PreviewRegistration(ctx, binding)
			if err != nil || view.Provider != p.ProviderSlug || view.MaskedEmail != "h***@example.test" {
				t.Fatal("registration preview lost provider provenance", err)
			}
			const email = "chosen@example.test"
			start, err := service.StartRegistration(ctx, binding, SignupStartInput{Email: email, DestinationAllowed: scenario != "destination throttled"})
			if err != nil {
				t.Fatal(err)
			}
			input := RegistrationCompleteInput{TermsVersion: service.config.LegalTermsVersion, Code: "420381", ChallengeID: start.ChallengeID,
				SignupEnabled: scenario != "signup disabled" && !db.existing, KeepSignedIn: true}
			var wantErr error
			switch scenario {
			case "Microsoft disabled provider":
				service.ConfigureRegistrationProviders(nil)
				wantErr = ErrAuthTransactionUnavailable
			case "Microsoft blocked tenant":
				service.ConfigureRegistrationProviders(microsoftTestRegistry(t, &MicrosoftProvider{tenantMode: "common", blockedTenantIDs: []string{p.ProviderOrgID}}))
				wantErr = ErrAuthTransactionUnavailable
			case "wrong code":
				input.Code = "000000"
				wantErr = ErrNativeInvalidCode
			case "expired code":
				now = now.Add(EmailChallengeTTL)
				wantErr = ErrNativeInvalidCode
			case "foreign challenge":
				input.ChallengeID = uuid.New()
				wantErr = ErrNativeInvalidCode
			case "destination throttled":
				wantErr = ErrNativeInvalidCode
			case "signup disabled":
				wantErr = ErrFederationSignupDisabled
			case "resend":
				allowed := func(_ context.Context, stored string) (bool, error) { return stored == email, nil }
				if _, err := service.ResendRegistration(ctx, binding, allowed); !errors.Is(err, ErrAuthChallengeThrottled) {
					t.Fatal("cooldown", err)
				}
				now = now.Add(ChallengeCooldown)
				next, err := service.ResendRegistration(ctx, binding, allowed)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := service.CompleteRegistration(ctx, binding, input); !errors.Is(err, ErrNativeInvalidCode) {
					t.Fatal("superseded code", err)
				}
				input.ChallengeID = next.ChallengeID
			}
			result, err := service.CompleteRegistration(ctx, binding, input)
			if !errors.Is(err, wantErr) {
				t.Fatalf("complete error = %v, want %v", err, wantErr)
			}
			if err != nil {
				if result.Session.SessionToken != "" || result.LinkBinding != "" || db.row.State != "pending" {
					t.Fatal("failed proof advanced registration")
				}
				return
			}
			if db.existing {
				if result.LinkBinding == "" || result.Session.SessionToken != "" || db.linkOwner != db.owner {
					t.Fatal("collision must create only the owner's link intent")
				}
			} else if result.LinkBinding != "" || result.Session.SessionToken == "" || !result.Session.Persistent {
				t.Fatal("new registration did not create the requested session")
			}
			if _, err := service.CompleteRegistration(ctx, binding, input); !errors.Is(err, ErrAuthTransactionUnavailable) {
				t.Fatal("registration replay", err)
			}
		})
	}
}
