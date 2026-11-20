//go:build integration

package identity

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
	"github.com/tutorhub-v2/core-api/migrations"
)

func TestPasswordMigrationRoundTripBackfillAndPreflight(t *testing.T) {
	ctx, pool, url := authSchemaTestDatabase(t)
	if err := migrationrunner.Down(ctx, url, 7); err != nil {
		t.Fatal("prepare disposable ledger 43")
	}
	version, err := migrationrunner.CurrentVersion(ctx, url)
	if err != nil || version.Number != 43 || version.Dirty {
		t.Fatal("password migration requires clean ledger 43")
	}
	userID := uuid.New()
	email := userID.String() + "@example.test"
	if _, err := pool.Exec(ctx, `INSERT INTO tutorhub.users(id,email,display_name) VALUES($1,$2,'Migration fixture')`, userID, email); err != nil {
		t.Fatal("create backfill fixture")
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, `DELETE FROM tutorhub.users WHERE id=$1`, userID); err != nil {
			t.Error("clean disposable backfill fixture")
		}
	})
	now := time.Now().UTC().Truncate(time.Microsecond)
	verifiedAt := now.Add(-time.Hour)
	for index, item := range []struct {
		mail, status string
		verified     bool
		created      time.Time
	}{
		{strings.ToUpper(email), "active", true, verifiedAt},
		{email, "active", true, now},
		{email, "active", false, now.Add(-2 * time.Hour)},
		{"unrelated@example.test", "active", true, now.Add(-3 * time.Hour)},
		{email, "unlinked", true, now.Add(-4 * time.Hour)},
	} {
		if _, err := pool.Exec(ctx, `INSERT INTO tutorhub.identities(user_id,provider,subject,email_at_provider,email_verified,status,created_at)
            VALUES($1,'urn:fixture:backfill',$2,$3,$4,$5,$6)`, userID, uuid.NewString(), item.mail, item.verified, item.status, item.created); err != nil {
			t.Fatalf("insert backfill fixture %d", index)
		}
	}
	for cycle := range 2 {
		if err := migrationrunner.Up(ctx, url); err != nil {
			t.Fatal("apply password migration")
		}
		version, err := migrationrunner.CurrentVersion(ctx, url)
		if err != nil || version.Number != 50 || version.Dirty {
			t.Fatal("password migration must finish at clean ledger 50")
		}
		var actual time.Time
		if err := pool.QueryRow(ctx, `SELECT email_verified_at FROM tutorhub.users WHERE id=$1`, userID).Scan(&actual); err != nil || !actual.Equal(verifiedAt) {
			t.Fatal("verification backfill must use earliest matching active verified identity")
		}
		if cycle == 0 {
			if err := migrationrunner.Down(ctx, url, 7); err != nil {
				t.Fatal("empty password migration rollback")
			}
		}
	}
	down, err := migrations.Files.ReadFile("000044_password_credentials.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []string{
		`INSERT INTO tutorhub.user_password_credentials(user_id,password_hash,created_at,updated_at) VALUES($1,'$argon2id$v=19$fixture',now(),now())`,
		`INSERT INTO tutorhub.user_legal_acceptances(user_id,document_kind,version,accepted_at) VALUES($1,'privacy_policy','fixture',now())`,
	} {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal("begin password rollback fixture")
		}
		if _, err := tx.Exec(ctx, fixture, userID); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatal("create password rollback fixture")
		}
		_, err = tx.Exec(ctx, string(down))
		_ = tx.Rollback(ctx)
		if err == nil || !strings.Contains(err.Error(), "AUTH-02A rollback blocked") {
			t.Fatal("password rollback must preserve credentials and legal acceptances")
		}
	}
}
