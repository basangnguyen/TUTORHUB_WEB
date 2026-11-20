//go:build integration

package identity

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPhonePostgresLegacyUnlinkAndLastMethodAvailability(t *testing.T) {
	f := newSettingsIntegrationFixture(t, true)
	p := f.principal
	number, err := NormalizePhoneNumber("0912 345 678", "VN", []string{"VN"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.attachPhone(f.ctx, f.tx, p, number, f.service.crypto.phoneLookupHash(number.E164), f.now); err != nil {
		t.Fatal(err)
	}
	var phoneIdentity uuid.UUID
	if err = f.tx.QueryRow(f.ctx, `SELECT identity_id FROM tutorhub.user_phone_numbers WHERE user_id=$1 AND status='active'`, p.User.ID).Scan(&phoneIdentity); err != nil {
		t.Fatal(err)
	}
	repository := NewPostgresRepository(f.tx, 20*time.Second, nil)
	if err = repository.UnlinkIdentity(f.ctx, p.User.ID, p.SessionID, phoneIdentity, f.now); !errors.Is(err, ErrIdentityNotFound) {
		t.Fatal("legacy unlink must require the phone management ceremony", err)
	}
	for _, phoneEnabled := range []bool{false, true} {
		repository.SetLoginMethodAvailability(phoneEnabled, false, false, 60*24*time.Hour)
		err = repository.UnlinkIdentity(f.ctx, p.User.ID, p.SessionID, p.IdentityID, f.now)
		if phoneEnabled && err != nil {
			t.Fatal("active phone should count", err)
		}
		if !phoneEnabled && !errors.Is(err, ErrLastIdentity) {
			t.Fatal("disabled phone counted as an alternate", err)
		}
	}
	if _, err = ensureNativeEmailIdentity(f.ctx, f.tx, p.User.ID, p.User.Email, f.now); err != nil {
		t.Fatal(err)
	}
	check := func(want error) {
		t.Helper()
		got := requireAnotherLoginMethod(f.ctx, f.tx, p.User.ID, phoneIdentity, uuid.Nil, f.service.availableLoginMethods(), f.now)
		if !errors.Is(got, want) {
			t.Fatalf("remaining login method decision = %v, want %v", got, want)
		}
	}
	check(ErrLastIdentity)
	f.service.config.EmailCodeAvailable = true
	check(ErrLastIdentity)
	f.service.config.EmailCodeLoginEnabled = true
	check(nil)
	f.service.config.EmailCodeLoginEnabled = false
	f.service.config.PasswordEnabled = true
	check(ErrLastIdentity)
	hash, err := f.passwords.Hash(f.ctx, nativeIntegrationPassword)
	if err != nil {
		t.Fatal(err)
	}
	if err = upsertNativePassword(f.ctx, f.tx, p.User.ID, hash, f.now); err != nil {
		t.Fatal(err)
	}
	check(nil)
	f.service.config.PasswordEnabled = false
	check(ErrLastIdentity)
}

func TestPhonePostgresStalePhoneCannotBeLastRemainingMethod(t *testing.T) {
	for _, kind := range []string{"federated", "passkey"} {
		t.Run(kind, func(t *testing.T) {
			f := newSettingsIntegrationFixture(t, true)
			p := f.principal
			now := f.now
			number, err := NormalizePhoneNumber("0912345678", "VN", []string{"VN"})
			if err != nil {
				t.Fatal(err)
			}
			if err = f.service.attachPhone(f.ctx, f.tx, p, number, f.service.crypto.phoneLookupHash(number.E164), now.Add(-61*24*time.Hour)); err != nil {
				t.Fatal(err)
			}
			// A verified canonical email trips the stale guard even when
			// email-code/password login has been disabled by deployment policy.
			if _, err = f.tx.Exec(f.ctx, `UPDATE tutorhub.users SET email_verified_at=$2 WHERE id=$1`, p.User.ID, now); err != nil {
				t.Fatal(err)
			}
			var remove func() error
			if kind == "federated" {
				repo := NewPostgresRepository(f.tx, 20*time.Second, nil)
				repo.SetLoginMethodAvailability(true, false, false, 60*24*time.Hour)
				remove = func() error { return repo.UnlinkIdentity(f.ctx, p.User.ID, p.SessionID, p.IdentityID, now) }
			} else {
				if _, err := f.tx.Exec(f.ctx, `UPDATE tutorhub.identities SET status='unlinked',unlinked_at=$2 WHERE id=$1`, p.IdentityID, now); err != nil {
					t.Fatal(err)
				}
				passkey := uuid.New()
				if _, err := f.tx.Exec(f.ctx, `INSERT INTO tutorhub.webauthn_credentials(id,user_id,credential_id,public_key,attestation_type,label,created_at,updated_at)
					VALUES($1,$2,$3,$3,'none','fixture',$4,$4)`, passkey, p.User.ID, passkey[:], now); err != nil {
					t.Fatal(err)
				}
				f.service.webauthn = &PasskeyProtocol{}
				f.service.config.PhoneEnabled = true
				f.service.config.PhoneStaleAfter = 60 * 24 * time.Hour
				remove = func() error { return f.service.RevokeMyPasskey(f.ctx, p, passkey) }
			}
			if err = remove(); !errors.Is(err, ErrLastIdentity) {
				t.Fatal("stale phone permitted removal of last usable method", err)
			}
			var id uuid.UUID
			if err = f.tx.QueryRow(f.ctx, `SELECT identity_id FROM tutorhub.user_phone_numbers WHERE user_id=$1 AND status='active'`, p.User.ID).Scan(&id); err != nil {
				t.Fatal(err)
			}
			if err = markPhoneUsed(f.ctx, f.tx, p.User.ID, id, now); err != nil {
				t.Fatal(err)
			}
			if err = remove(); err != nil {
				t.Fatal("reverified phone must be usable after removing last other method", err)
			}
		})
	}
}
