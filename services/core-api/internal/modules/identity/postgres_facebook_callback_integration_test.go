//go:build integration

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
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
	"github.com/tutorhub-v2/core-api/migrations"
)

// Each transaction, including concurrent callbacks and email login, executes as
// the same dedicated runtime role whose ACL was provisioned by the caller.
type facebookRuntimeDatabase struct {
	pool       *pgxpool.Pool
	quotedRole string
}

func (d facebookRuntimeDatabase) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, "SET LOCAL ROLE "+d.quotedRole); err != nil {
		rollback(tx)
		return nil, err
	}
	return tx, nil
}

func testRuntimeFacebookErasure(t *testing.T, ctx context.Context, pool *pgxpool.Pool, roleName string) {
	t.Helper()
	t.Run("Facebook erasure, callbacks and verified email under runtime role", func(t *testing.T) {
		role := pgx.Identifier{roleName}.Sanitize()
		for _, grant := range []string{"GRANT USAGE ON SCHEMA tutorhub TO " + role,
			"GRANT SELECT ON tutorhub.memberships,tutorhub.tenants TO " + role,
			"GRANT INSERT (tenant_id,aggregate_type,aggregate_id,event_type,payload,occurred_at,available_at) ON tutorhub.outbox_events TO " + role, "GRANT " + role + " TO CURRENT_USER"} {
			if _, err := pool.Exec(ctx, grant); err != nil {
				t.Fatal(err)
			}
		}
		database := facebookRuntimeDatabase{pool, role}
		repo := NewPostgresRepository(database, 20*time.Second, nil)
		var aclOK bool
		err := pool.QueryRow(ctx, `SELECT p.prosecdef
			AND p.proconfig=ARRAY['search_path=pg_catalog, tutorhub']
			AND p.proowner=(SELECT relowner FROM pg_class WHERE oid='tutorhub.identities'::regclass)
			AND NOT EXISTS(SELECT 1 FROM aclexplode(COALESCE(p.proacl,acldefault('f',p.proowner))) a
				WHERE a.grantee NOT IN (p.proowner,(SELECT oid FROM pg_roles WHERE rolname=$1)))
			AND has_function_privilege($1,p.oid,'EXECUTE') AND NOT has_function_privilege($1,p.oid,'EXECUTE WITH GRANT OPTION')
			AND NOT has_function_privilege($1,'tutorhub.clear_finished_federation_payload()','EXECUTE')
			AND NOT has_function_privilege($1,'tutorhub.purge_expired_deletion_receipts()','EXECUTE')
			AND NOT has_table_privilege($1,'tutorhub.identities','DELETE')
			AND NOT has_column_privilege($1,'tutorhub.identities','subject','UPDATE')
			FROM pg_proc p WHERE p.oid='tutorhub.erase_provider_identity(text,text,text,bytea,timestamptz)'::regprocedure`, roleName).Scan(&aclOK)
		if err != nil || !aclOK {
			t.Fatal("incorrect provider-erasure permissions", err)
		}

		crypto, err := NewCrypto(bytes.Repeat([]byte{0x91}, 32))
		if err != nil {
			t.Fatal(err)
		}
		userID, fbID, emailID := uuid.New(), uuid.New(), uuid.New()
		email := userID.String() + "@example.test"
		issuer := "https://facebook.example/" + userID.String()
		event := facebookCallbackEvent{issuer: issuer, subject: "174230981", now: time.Now().UTC(), fresh: true}
		event.subjectDigest = crypto.ProviderSubjectDigest(issuer, event.subject)
		event.eventDigest = crypto.Digest("facebook-signed-payload", uuid.NewString())
		absent := event
		absent.subject = "174230982"
		absent.subjectDigest = crypto.ProviderSubjectDigest(issuer, absent.subject)
		absent.eventDigest = crypto.Digest("facebook-signed-payload", uuid.NewString())
		unlink := event
		unlink.eventDigest = crypto.Digest("facebook-signed-payload", uuid.NewString())
		lastMethod := event
		lastMethod.eventDigest = crypto.Digest("facebook-signed-payload", uuid.NewString())
		var marker uuid.UUID
		copy(marker[:], unlink.eventDigest)
		var lastMarker uuid.UUID
		copy(lastMarker[:], lastMethod.eventDigest)
		exec := func(sql string, args ...any) {
			t.Helper()
			if _, err := pool.Exec(ctx, sql, args...); err != nil {
				t.Fatal(err)
			}
		}
		t.Cleanup(func() {
			for _, digest := range [][]byte{event.eventDigest, absent.eventDigest} {
				_, _ = pool.Exec(ctx, `DELETE FROM tutorhub.outbox_events WHERE aggregate_id IN (SELECT id FROM tutorhub.external_data_deletion_requests WHERE idempotency_key=$1)`, digest)
				_, _ = pool.Exec(ctx, `DELETE FROM tutorhub.external_data_deletion_requests WHERE idempotency_key=$1`, digest)
			}
			_, _ = pool.Exec(ctx, `DELETE FROM tutorhub.outbox_events WHERE aggregate_id IN ($1,$2)`, marker, lastMarker)
			_, _ = pool.Exec(ctx, `DELETE FROM tutorhub.users WHERE id=$1`, userID)
		})
		exec(`INSERT INTO tutorhub.users(id,email,display_name,email_verified_at) VALUES($1,$2,'Fixture',now())`, userID, email)
		exec(`INSERT INTO tutorhub.identities(id,user_id,provider,subject,kind,provider_slug,email_verified,email_at_provider)
			VALUES($1,$2,'urn:tutorhub:email',$3,'email','email',true,$4)`, emailID, userID, userID.String(), email)
		insertFB := func(id uuid.UUID) {
			t.Helper()
			exec(`INSERT INTO tutorhub.identities(id,user_id,provider,subject,kind,provider_slug,email_verified,email_at_provider,provider_org_id,provider_stable_id)
				VALUES($1,$2,$3,$4,'oidc','facebook',false,$5,'fixture-org','fixture-stable')`, id, userID, issuer, event.subject, email)
		}
		insertFB(fbID)
		insertSession := func(id uuid.UUID) uuid.UUID {
			t.Helper()
			session := uuid.New()
			exec(`INSERT INTO tutorhub.sessions(id,user_id,identity_id,token_hash,csrf_token_hash,last_seen_at,expires_at,absolute_expires_at,auth_time)
				VALUES($1,$2,$3,$4,$5,now(),now()+interval '1 hour',now()+interval '24 hours',now())`,
				session, userID, id, crypto.Digest("session", session.String()), crypto.Digest("csrf", session.String()))
			return session
		}
		check := func(sql string, args ...any) {
			t.Helper()
			var ok bool
			if err := pool.QueryRow(ctx, sql, args...).Scan(&ok); err != nil || !ok {
				t.Fatalf("fixture assertion failed: %s (%v)", sql, err)
			}
		}
		check(`SELECT array_agg(column_name::text ORDER BY ordinal_position)=ARRAY[
			'id','provider','idempotency_key','confirmation_code','status','created_at','completed_at']
			FROM information_schema.columns WHERE table_schema='tutorhub' AND table_name='external_data_deletion_requests'`)
		// Deauthorization also works when Facebook is the only active method.
		exec(`UPDATE tutorhub.identities SET status='unlinked',unlinked_at=now() WHERE id=$1`, emailID)
		lastSession := insertSession(fbID)
		if err := repo.deauthorizeFacebook(ctx, lastMethod); err != nil {
			t.Fatal(err)
		}
		check(`SELECT status='unlinked' AND subject=$2 FROM tutorhub.identities WHERE id=$1`, fbID, event.subject)
		check(`SELECT revoked_reason='identity_unlinked' FROM tutorhub.sessions WHERE id=$1`, lastSession)
		verifyFacebookErasureEmailAccess(t, ctx, database, crypto, email)
		unlinkedSession := insertSession(fbID)
		emailSession := insertSession(emailID)
		// A user-initiated unlink can leave its initiating session open.
		exec(`UPDATE tutorhub.identities SET status='unlinked',unlinked_at=now() WHERE id=$1`, fbID)
		if err := repo.deauthorizeFacebook(ctx, unlink); err != nil {
			t.Fatal(err)
		}
		check(`SELECT revoked_reason='identity_unlinked' FROM tutorhub.sessions WHERE id=$1`, unlinkedSession)
		check(`SELECT status='unlinked' AND subject=$2 FROM tutorhub.identities WHERE id=$1`, fbID, event.subject)
		exec(`UPDATE tutorhub.identities SET status='active',unlinked_at=NULL WHERE id=$1`, fbID)
		active := insertSession(fbID)
		unlink.fresh = false
		if err := repo.deauthorizeFacebook(ctx, unlink); err != nil {
			t.Fatal(err)
		}
		check(`SELECT revoked_at IS NULL FROM tutorhub.sessions WHERE id=$1`, active)

		for _, f := range []struct {
			kind   string
			digest []byte
		}{
			{"federated_registration", event.subjectDigest}, {"link_intent", event.subjectDigest}, {"federated_registration", absent.subjectDigest},
		} {
			exec(`INSERT INTO tutorhub.auth_transactions(user_id,binding_hash,kind,state,payload_ciphertext,provider_subject_digest,created_at,expires_at)
				VALUES($1,$2,$3,'pending',$4,$5,now(),now()+interval '10 minutes')`, userID, crypto.Digest("tx", uuid.NewString()), f.kind, []byte("ciphertext fixture"), f.digest)
		}
		// The outbox write is last. Its failure must roll back both erasure and receipt.
		exec("REVOKE INSERT (payload) ON tutorhub.outbox_events FROM " + role)
		if _, err := repo.deleteFacebookData(ctx, event); !errors.Is(err, ErrAuthFoundationStorage) {
			t.Fatal("callback must fail when unable to persist the notification", err)
		}
		check(`SELECT status='active' AND subject=$2 FROM tutorhub.identities WHERE id=$1`, fbID, event.subject)
		check(`SELECT NOT EXISTS(SELECT 1 FROM tutorhub.external_data_deletion_requests WHERE idempotency_key=$1)`, event.eventDigest)
		check(`SELECT count(*)=2 FROM tutorhub.auth_transactions WHERE provider_subject_digest=$1 AND payload_ciphertext IS NOT NULL`, event.subjectDigest)
		exec("GRANT INSERT (payload) ON tutorhub.outbox_events TO " + role)
		type result struct {
			code string
			err  error
		}
		start := make(chan struct{})
		done := make(chan result, 2)
		for i := 0; i < 2; i++ {
			go func() {
				<-start
				code, err := repo.deleteFacebookData(ctx, event)
				done <- result{code, err}
			}()
		}
		close(start)
		first, second := <-done, <-done
		if first.err != nil || second.err != nil || len(first.code) != 32 || first.code != second.code {
			t.Fatalf("duplicate concurrent events returned different receipts (%v, %v)", first.err, second.err)
		}
		check(`SELECT count(*)=1 FROM tutorhub.external_data_deletion_requests WHERE idempotency_key=$1 AND status='completed' AND completed_at IS NOT NULL`, event.eventDigest)
		check(`SELECT count(*)=1 FROM tutorhub.outbox_events WHERE aggregate_id IN (SELECT id FROM tutorhub.external_data_deletion_requests WHERE idempotency_key=$1)`, event.eventDigest)
		check(`SELECT status='erased' AND subject LIKE 'erased:%' AND email_at_provider IS NULL AND provider_org_id IS NULL AND provider_stable_id IS NULL FROM tutorhub.identities WHERE id=$1`, fbID)
		check(`SELECT count(*)=0 FROM tutorhub.identities WHERE provider=$1 AND subject=$2`, issuer, event.subject)
		check(`SELECT revoked_reason='provider_data_deleted' FROM tutorhub.sessions WHERE id=$1`, active)
		check(`SELECT revoked_at IS NULL FROM tutorhub.sessions WHERE id=$1`, emailSession)
		check(`SELECT count(*)=2 FROM tutorhub.auth_transactions WHERE user_id=$1 AND state='cancelled' AND provider_subject_digest IS NULL AND payload_ciphertext IS NULL`, userID)
		check(`SELECT count(*)=1 FROM tutorhub.auth_transactions WHERE user_id=$1 AND state='pending' AND provider_subject_digest=$2`, userID, absent.subjectDigest)
		tx, err := database.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer rollback(tx)
		var rows int
		if err := tx.QueryRow(ctx, `SELECT tutorhub.erase_provider_identity('facebook',$1,$2,$3,now())`, issuer, event.subject, event.subjectDigest).Scan(&rows); err != nil || rows != 0 {
			t.Fatal("repeated erasure must affect zero identities", err)
		}
		_, err = tx.Exec(ctx, `SELECT tutorhub.erase_provider_identity('google',$1,$2,$3,now())`, issuer, event.subject, event.subjectDigest)
		var postgresErr *pgconn.PgError
		if !errors.As(err, &postgresErr) || postgresErr.Code != "22023" {
			t.Fatal("erasure accepted a non-Facebook provider", err)
		}
		rollback(tx)

		if _, err := pool.Exec(ctx, `UPDATE tutorhub.identities SET status='active' WHERE id=$1`, fbID); !errors.As(err, &postgresErr) || postgresErr.Code != "23514" {
			t.Fatal("erased identity unexpectedly reactivated", err)
		}
		service := verifyFacebookErasureEmailAccess(t, ctx, database, crypto, email)
		linkHash := verifyFacebookRegistrationAfterErasure(t, ctx, pool, repo, service, event, userID, email)
		event.fresh = false
		replayed, err := repo.deleteFacebookData(ctx, event)
		if err != nil || replayed != first.code {
			t.Fatal("late retry must return original receipt", err)
		}
		check(`SELECT state='pending' AND provider_subject_digest=$2 FROM tutorhub.auth_transactions WHERE binding_hash=$1`, linkHash, event.subjectDigest)
		code, err := repo.deleteFacebookData(ctx, absent)
		if err != nil || code == first.code {
			t.Fatal("missing identity must receive an independent receipt", err)
		}
		status, err := repo.facebookReceiptStatus(ctx, code, event.now)
		if err != nil || status != "completed" {
			t.Fatal("receipt for missing identity did not complete", err)
		}
		check(`SELECT count(*)=3 FROM tutorhub.auth_transactions WHERE user_id=$1 AND state='cancelled' AND provider_subject_digest IS NULL AND payload_ciphertext IS NULL`, userID)

	})
}

func verifyFacebookErasureEmailAccess(t *testing.T, ctx context.Context, database Database, crypto *Crypto, email string) *NativeAuthService {
	t.Helper()
	passwords, err := NewPasswordService(PasswordOptions{BreachMode: "off"})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewNativeAuthService(database, crypto, passwords, nil, NativeAuthConfig{LegalTermsVersion: "v1", SessionTTL: time.Hour, SessionAbsoluteTTL: 24 * time.Hour, SessionPersistentAbsoluteTTL: 30 * 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	service.challenges.code = func() (string, error) { return "420381", nil }
	input := SignupStartInput{Email: email, DestinationAllowed: true}
	challenge, err := service.StartEmailCodeLogin(ctx, input)
	if err != nil {
		t.Fatal("email login challenge", err)
	}
	session, err := service.VerifyEmailCodeLogin(ctx, challenge.Cookie.Value, EmailCodeVerifyInput{ChallengeID: challenge.ChallengeID, Code: "420381"})
	if err != nil || session.SessionToken == "" {
		t.Fatal("email login after provider erasure", err)
	}
	recovery, err := service.StartPasswordRecovery(ctx, input)
	if err != nil {
		t.Fatal("email recovery challenge", err)
	}
	_, err = service.CompletePasswordRecovery(ctx, recovery.Cookie.Value, PasswordRecoveryCompleteInput{ChallengeID: recovery.ChallengeID, Code: "420381", NewPassword: uuid.NewString()})
	if err != nil {
		t.Fatal("verified email password recovery after provider erasure", err)
	}
	return service
}

func verifyFacebookRegistrationAfterErasure(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo *PostgresRepository, service *NativeAuthService, event facebookCallbackEvent, owner uuid.UUID, email string) []byte {
	t.Helper()
	binding, err := service.crypto.RandomToken()
	if err != nil {
		t.Fatal(err)
	}
	hash := service.crypto.Digest(registrationBindingPurpose, binding)
	raw, err := json.Marshal(FederationIdentityPayload{ProviderSlug: "facebook", Issuer: event.issuer, Subject: event.subject, Email: email, ReturnTo: "/app/home"})
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := service.crypto.Encrypt(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, `DELETE FROM tutorhub.auth_transactions WHERE binding_hash=$1`, hash); err != nil {
			t.Error("remove re-registration fixture", err)
		}
	})
	var sessions int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM tutorhub.sessions WHERE user_id=$1`, owner).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	login, err := repo.CompleteFederatedLogin(ctx, FederationLoginParams{
		ProviderSlug: "facebook", Flow: StoredFlow{Purpose: flowPurposeLogin},
		Claims:                  ProviderClaims{ProviderSlug: "facebook", Issuer: event.issuer, Subject: event.subject, Email: email},
		RegistrationBindingHash: hash, LinkPayloadCiphertext: ciphertext, ProviderSubjectDigest: event.subjectDigest,
		LinkExpiresAt: now.Add(AuthTransactionTTL), Metadata: SessionMetadata{CreatedAt: now},
	})
	if err != nil || !login.RegistrationPending || login.LinkPending || login.Principal.SessionID != uuid.Nil {
		t.Fatal("old ASID must begin a fresh registration", err)
	}
	challenge, err := service.StartRegistration(ctx, binding, SignupStartInput{Email: email, DestinationAllowed: true})
	if err != nil {
		t.Fatal("start OTP after erasure", err)
	}
	result, err := service.CompleteRegistration(ctx, binding, RegistrationCompleteInput{ChallengeID: challenge.ChallengeID, Code: "420381", TermsVersion: service.config.LegalTermsVersion})
	if err != nil || result.LinkBinding == "" || result.Session.SessionToken != "" {
		t.Fatal("OTP from an existing email owner must issue only a link intent", err)
	}
	linkHash := service.crypto.Digest(linkBindingPurpose, result.LinkBinding)
	var pending bool
	err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tutorhub.auth_transactions WHERE binding_hash=$1 AND user_id=$2 AND state='pending'
		AND kind='link_intent' AND provider_subject_digest=$3)
		AND NOT EXISTS(SELECT 1 FROM tutorhub.identities WHERE provider=$4 AND subject=$5)
		AND (SELECT count(*) FROM tutorhub.sessions WHERE user_id=$2)=$6`, linkHash, owner, event.subjectDigest, event.issuer, event.subject, sessions).Scan(&pending)
	if err != nil || !pending {
		t.Fatal("OTP auto-linked or signed in a previously erased identity", err)
	}
	return linkHash
}

func TestFacebookErasureMigrationRoundTripAndPreflight(t *testing.T) {
	ctx, pool, url := authSchemaTestDatabase(t)
	if err := migrationrunner.Down(ctx, url, 3); err != nil {
		t.Fatal("empty 46 down", err)
	}
	version, err := migrationrunner.CurrentVersion(ctx, url)
	if err != nil || version.Number != 45 || version.Dirty {
		t.Fatal("46 down did not restore clean 45", err)
	}
	if err := migrationrunner.Up(ctx, url); err != nil {
		t.Fatal("46 up", err)
	}
	sql, err := migrations.Files.ReadFile("000046_provider_data_erasure.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	body := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(string(sql)), "BEGIN;"), "COMMIT;")
	for _, fixture := range []string{
		`WITH u AS (INSERT INTO tutorhub.users(email,display_name) VALUES('erased-preflight@example.test','Fixture') RETURNING id)
		INSERT INTO tutorhub.identities(user_id,provider,subject,provider_slug,status) SELECT id,'https://facebook.example','erased:'||gen_random_uuid()::text,'facebook','erased' FROM u`,
		`INSERT INTO tutorhub.external_data_deletion_requests(provider,idempotency_key,confirmation_code,status,created_at)
		 VALUES('facebook',decode(repeat('01',32),'hex'),repeat('a',32),'received',now())`,
		`INSERT INTO tutorhub.auth_transactions(binding_hash,kind,state,provider_subject_digest,created_at,expires_at) VALUES
		 (decode(repeat('02',32),'hex'),'federated_registration','pending',decode(repeat('03',32),'hex'),now(),now()+interval '10 minutes')`,
	} {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, fixture); err != nil {
			rollback(tx)
			t.Fatal("preflight fixture", err)
		}
		_, err = tx.Exec(ctx, body)
		rollback(tx)
		if err == nil || !strings.Contains(err.Error(), "AUTH-06 rollback blocked") {
			t.Fatal("retained data did not prevent rollback", err)
		}
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `INSERT INTO tutorhub.external_data_deletion_requests(provider,idempotency_key,confirmation_code,status,created_at)
		VALUES('facebook',decode(repeat('01',32),'hex'),repeat('a',32),'received',now()-interval '1 year'),
		      ('facebook',decode(repeat('02',32),'hex'),repeat('b',32),'received',now()-interval '11 months')`); err != nil {
		t.Fatal(err)
	}
	var purged int
	if err = tx.QueryRow(ctx, `SELECT tutorhub.purge_expired_deletion_receipts()`).Scan(&purged); err != nil || purged != 1 {
		t.Fatal("one-year retention", err)
	}
	var keep bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tutorhub.external_data_deletion_requests WHERE confirmation_code=repeat('b',32))`).Scan(&keep); err != nil || !keep {
		t.Fatal("retention removed an unexpired receipt", err)
	}
}
