package identity

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	ErrInvalidPasskey  = errors.New("invalid passkey")
	ErrPasskeyUnknown  = errors.New("unknown passkey credential")
	ErrPasskeyClone    = errors.New("passkey clone warning")
	ErrPasskeyConflict = errors.New("credential already registered")
	ErrPasskeyNotFound = errors.New("passkey not found")
)

// Passkey is the user's settings view. Credential IDs, handles and public keys
// stay on the server; signalUnknownCredential uses the browser's attempted ID.
type Passkey struct {
	ID             uuid.UUID  `json:"id"`
	Label          string     `json:"label"`
	Transports     []string   `json:"transports"`
	BackupEligible bool       `json:"backup_eligible"`
	BackupState    bool       `json:"backup_state"`
	CreatedAt      time.Time  `json:"created_at"`
	LastUsedAt     *time.Time `json:"last_used_at"`
}

const passkeySummaryColumns = "id,label,transports,backup_eligible,backup_state,created_at,last_used_at"

func readPasskey(row pgx.Row) (Passkey, error) {
	var result Passkey
	err := row.Scan(&result.ID, &result.Label, &result.Transports, &result.BackupEligible, &result.BackupState, &result.CreatedAt, &result.LastUsedAt)
	return result, err
}

func validatePasskeyLabel(label string) error {
	if !utf8.ValidString(label) || strings.TrimSpace(label) == "" || strings.ContainsRune(label, 0) || utf8.RuneCountInString(label) > 80 {
		return ErrAuthFoundationInput
	}
	return nil
}

// lockPasskeyUser serializes discoverable login with credential revocation and
// all other account mutations. Only acquire credential locks after the user.
func lockPasskeyUser(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	err := lockActiveTenantUser(ctx, tx, userID)
	if errors.Is(err, ErrSessionNotFound) {
		return ErrInvalidPasskey
	}
	if err != nil {
		return ErrAuthFoundationStorage
	}
	return nil
}

func passkeyUserHandle(ctx context.Context, tx pgx.Tx, userID uuid.UUID, create bool) ([]byte, error) {
	var handle []byte
	if err := tx.QueryRow(ctx, `SELECT webauthn_user_handle FROM tutorhub.users WHERE id=$1 AND status='active'`, userID).Scan(&handle); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInvalidPasskey
		}
		return nil, ErrAuthFoundationStorage
	}
	if handle != nil || !create {
		return handle, nil
	}
	handle = make([]byte, 64)
	if _, err := rand.Read(handle); err != nil {
		return nil, ErrAuthFoundationStorage
	}
	cmd, err := tx.Exec(ctx, `UPDATE tutorhub.users SET webauthn_user_handle=$2 WHERE id=$1 AND webauthn_user_handle IS NULL`, userID, handle)
	if err != nil || cmd.RowsAffected() != 1 {
		return nil, ErrAuthFoundationStorage
	}
	return handle, nil
}

func lookupPasskeyOwner(ctx context.Context, tx pgx.Tx, rawID []byte) (uuid.UUID, error) {
	var owner uuid.UUID
	err := tx.QueryRow(ctx, `SELECT user_id FROM tutorhub.webauthn_credentials WHERE credential_id=$1`, rawID).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrPasskeyUnknown
	}
	if err != nil {
		return uuid.Nil, ErrAuthFoundationStorage
	}
	return owner, nil
}

func activePasskeyCredentials(ctx context.Context, tx pgx.Tx, userID uuid.UUID) ([]webauthn.Credential, error) {
	rows, err := tx.Query(ctx, `SELECT credential_id,public_key,attestation_type,aaguid,sign_count,clone_warning,transports,backup_eligible,backup_state
		FROM tutorhub.webauthn_credentials WHERE user_id=$1 AND status='active' ORDER BY created_at,id`, userID)
	if err != nil {
		return nil, ErrAuthFoundationStorage
	}
	defer rows.Close()
	result := []webauthn.Credential{}
	for rows.Next() {
		var c webauthn.Credential
		var transports []string
		var be, bs bool
		if err := rows.Scan(&c.ID, &c.PublicKey, &c.AttestationType, &c.Authenticator.AAGUID, &c.Authenticator.SignCount, &c.Authenticator.CloneWarning, &transports, &be, &bs); err != nil {
			return nil, ErrAuthFoundationStorage
		}
		var flags protocol.AuthenticatorFlags
		if be {
			flags |= protocol.FlagBackupEligible
		}
		if bs {
			flags |= protocol.FlagBackupState
		}
		c.Flags = webauthn.NewCredentialFlags(flags)
		for _, t := range transports {
			c.Transport = append(c.Transport, protocol.AuthenticatorTransport(t))
		}
		result = append(result, c)
	}
	if rows.Err() != nil {
		return nil, ErrAuthFoundationStorage
	}
	return result, nil
}

func saveRegisteredPasskey(ctx context.Context, tx pgx.Tx, userID uuid.UUID, c *webauthn.Credential, label string, now time.Time) (Passkey, error) {
	if err := validatePasskeyLabel(label); err != nil {
		return Passkey{}, err
	}
	if c == nil || len(c.ID) < 1 || len(c.ID) > 1024 {
		return Passkey{}, ErrInvalidPasskey
	}
	transports := make([]string, 0, len(c.Transport))
	for _, transport := range c.Transport {
		transports = append(transports, string(transport))
	}
	item, err := readPasskey(tx.QueryRow(ctx, `INSERT INTO tutorhub.webauthn_credentials
		(user_id,credential_id,public_key,attestation_type,aaguid,sign_count,transports,backup_eligible,backup_state,label,created_at,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$11) ON CONFLICT(credential_id) DO NOTHING RETURNING `+passkeySummaryColumns,
		userID, c.ID, c.PublicKey, c.AttestationType, c.Authenticator.AAGUID, c.Authenticator.SignCount, transports, c.Flags.BackupEligible, c.Flags.BackupState, label, now))
	if errors.Is(err, pgx.ErrNoRows) {
		return Passkey{}, ErrPasskeyConflict
	}
	if err != nil {
		return Passkey{}, ErrAuthFoundationStorage
	}
	if err := insertIdentityEvent(ctx, tx, uuid.Nil, userID, "passkey_added", item.ID, now); err != nil {
		return Passkey{}, ErrAuthFoundationStorage
	}
	return item, nil
}

// The caller holds the user lock from credential lookup through proof verification
// and must commit the warning and consumed challenge before returning ErrPasskeyClone.
func recordPasskeyAssertion(ctx context.Context, tx pgx.Tx, userID uuid.UUID, c *webauthn.Credential, now time.Time) error {
	if c.Authenticator.CloneWarning {
		var id uuid.UUID
		err := tx.QueryRow(ctx, `UPDATE tutorhub.webauthn_credentials SET clone_warning=true,updated_at=GREATEST(updated_at,$3)
			WHERE user_id=$1 AND credential_id=$2 AND status='active' AND NOT clone_warning RETURNING id`, userID, c.ID, now).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrPasskeyClone
		}
		if err != nil {
			return ErrAuthFoundationStorage
		}
		if err := insertIdentityEvent(ctx, tx, uuid.Nil, userID, "passkey_clone_detected", id, now); err != nil {
			return ErrAuthFoundationStorage
		}
		return ErrPasskeyClone
	}
	cmd, err := tx.Exec(ctx, `UPDATE tutorhub.webauthn_credentials SET sign_count=$3,backup_state=$4,last_used_at=GREATEST(created_at,COALESCE(last_used_at,$5),$5),updated_at=GREATEST(updated_at,COALESCE(last_used_at,$5),$5)
		WHERE user_id=$1 AND credential_id=$2 AND status='active' AND NOT clone_warning`, userID, c.ID, c.Authenticator.SignCount, c.Flags.BackupState, now)
	if err != nil {
		return ErrAuthFoundationStorage
	}
	if cmd.RowsAffected() != 1 {
		return ErrInvalidPasskey
	}
	return nil
}

// Both unlink paths hold the same user row lock before this check.
func requireAnotherLoginMethod(ctx context.Context, tx pgx.Tx, userID, removingIdentity, removingPasskey uuid.UUID, passkeysEnabled bool) error {
	var available bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM tutorhub.identities WHERE user_id=$1 AND status='active' AND id IS DISTINCT FROM $2::uuid)
		OR EXISTS (SELECT 1 FROM tutorhub.user_password_credentials WHERE user_id=$1 AND status='active')
		OR ($4 AND EXISTS (SELECT 1 FROM tutorhub.webauthn_credentials WHERE user_id=$1 AND status='active' AND NOT clone_warning AND id IS DISTINCT FROM $3::uuid))`,
		userID, nullUUID(removingIdentity), nullUUID(removingPasskey), passkeysEnabled).Scan(&available); err != nil {
		return ErrAuthFoundationStorage
	}
	if !available {
		return ErrLastIdentity
	}
	return nil
}

func (service *NativeAuthService) ListMyPasskeys(ctx context.Context, p Principal) ([]Passkey, error) {
	if service.webauthn == nil {
		return nil, ErrNativeMethodDisabled
	}
	ctx, cancel := context.WithTimeout(ctx, service.queryTimeout())
	defer cancel()
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return nil, ErrAuthFoundationStorage
	}
	defer rollback(tx)
	if err := service.validateSecuritySession(ctx, tx, p); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT `+passkeySummaryColumns+` FROM tutorhub.webauthn_credentials WHERE user_id=$1 AND status='active' ORDER BY created_at,id`, p.User.ID)
	if err != nil {
		return nil, ErrAuthFoundationStorage
	}
	defer rows.Close()
	items := []Passkey{}
	for rows.Next() {
		item, err := readPasskey(rows)
		if err != nil {
			return nil, ErrAuthFoundationStorage
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, ErrAuthFoundationStorage
	}
	return items, nil
}

func (service *NativeAuthService) lockRecentPasskeySession(ctx context.Context, tx pgx.Tx, p Principal, changesFactor bool) (lockedSecuritySession, error) {
	locked, err := service.lockNativeSecuritySession(ctx, tx, p)
	if err != nil {
		return locked, err
	}
	if err = nativePasswordRecentAuthentication(locked.AuthenticatedAt, service.now().UTC()); err == nil && changesFactor {
		err = service.requireRecoveryFactorAssurance(ctx, tx, p.User.ID, locked.AssuranceLevel)
	}
	return locked, err
}

func (service *NativeAuthService) RenameMyPasskey(ctx context.Context, p Principal, id uuid.UUID, label string) (Passkey, error) {
	if service.webauthn == nil {
		return Passkey{}, ErrNativeMethodDisabled
	}
	if err := validatePasskeyLabel(label); err != nil {
		return Passkey{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, service.queryTimeout())
	defer cancel()
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return Passkey{}, ErrAuthFoundationStorage
	}
	defer rollback(tx)
	if _, err := service.lockRecentPasskeySession(ctx, tx, p, false); err != nil {
		return Passkey{}, err
	}
	item, err := readPasskey(tx.QueryRow(ctx, `UPDATE tutorhub.webauthn_credentials SET label=$3,updated_at=GREATEST(updated_at,$4) WHERE id=$1 AND user_id=$2 AND status='active' RETURNING `+passkeySummaryColumns, id, p.User.ID, label, service.now().UTC()))
	if errors.Is(err, pgx.ErrNoRows) {
		return Passkey{}, ErrPasskeyNotFound
	}
	if err != nil || tx.Commit(ctx) != nil {
		return Passkey{}, ErrAuthFoundationStorage
	}
	return item, nil
}

func (service *NativeAuthService) RevokeMyPasskey(ctx context.Context, p Principal, id uuid.UUID) error {
	if service.webauthn == nil {
		return ErrNativeMethodDisabled
	}
	ctx, cancel := context.WithTimeout(ctx, service.queryTimeout())
	defer cancel()
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return ErrAuthFoundationStorage
	}
	defer rollback(tx)
	if _, err := service.lockRecentPasskeySession(ctx, tx, p, true); err != nil {
		return err
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tutorhub.webauthn_credentials WHERE id=$1 AND user_id=$2 AND status='active')`, id, p.User.ID).Scan(&exists); err != nil {
		return ErrAuthFoundationStorage
	}
	if !exists {
		return ErrPasskeyNotFound
	}
	if err := requireAnotherLoginMethod(ctx, tx, p.User.ID, uuid.Nil, id, true); err != nil {
		return err
	}
	now := service.now().UTC()
	cmd, err := tx.Exec(ctx, `UPDATE tutorhub.webauthn_credentials SET status='revoked',revoked_at=GREATEST(created_at,COALESCE(last_used_at,$3),$3),updated_at=GREATEST(updated_at,COALESCE(last_used_at,$3),$3) WHERE id=$1 AND user_id=$2 AND status='active'`, id, p.User.ID, now)
	if err != nil || cmd.RowsAffected() != 1 {
		return ErrAuthFoundationStorage
	}
	if err := insertIdentityEvent(ctx, tx, uuid.Nil, p.User.ID, "passkey_removed", id, now); err != nil {
		return ErrAuthFoundationStorage
	}
	if err := tx.Commit(ctx); err != nil {
		return ErrAuthFoundationStorage
	}
	return nil
}
