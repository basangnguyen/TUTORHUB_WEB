//go:build integration

package main

import (
	"bytes"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
)

func TestMaintenanceCLIBatchesOnDisposableDatabase(t *testing.T) {
	raw := os.Getenv("AUTH_TEST_DATABASE_URL")
	if raw == "" || os.Getenv("AUTH_TEST_DISPOSABLE") != "true" {
		t.Skip("requires the owner of an isolated disposable database")
	}
	ctx := t.Context()
	if err := migrationrunner.Up(ctx, raw); err != nil {
		t.Fatal("apply migrations")
	}
	owner, err := pgx.Connect(ctx, raw)
	if err != nil {
		t.Fatal("connect disposable owner")
	}
	defer owner.Close(ctx)
	roleName := "auth13_job_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	role := pgx.Identifier{roleName}.Sanitize()
	password := uuid.NewString()
	if _, err = owner.Exec(ctx, "CREATE ROLE "+role+" LOGIN PASSWORD '"+password+"'"); err != nil {
		t.Fatal("create disposable login")
	}
	defer func() {
		if _, err := owner.Exec(ctx, "DROP OWNED BY "+role+"; DROP ROLE "+role); err != nil {
			t.Error("clean up disposable job role")
		}
	}()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal("parse disposable URL")
	}
	u.User = url.UserPassword(roleName, password)
	var denied bytes.Buffer
	if code := run(ctx, nil, u.String(), &denied); code != 1 || strings.Contains(denied.String(), password) ||
		strings.Contains(denied.String(), roleName) || !strings.Contains(denied.String(), "auth maintenance batch failed") {
		t.Fatal("unprovisioned job must redact its database error")
	}
	if err = identity.ProvisionAuthMaintenanceACL(ctx, owner, roleName); err != nil {
		t.Fatal("provision disposable maintenance role", err)
	}
	for i := 0; i < 3; i++ {
		id := uuid.New()
		_, err = owner.Exec(ctx, `INSERT INTO tutorhub.auth_transactions(id,binding_hash,kind,state,created_at,expires_at)
			VALUES ($1, $2, 'login', 'pending', now()-interval '2 days 5 minutes', now()-interval '2 days')`, id, []byte(strings.ReplaceAll(id.String(), "-", "")))
		if err != nil {
			t.Fatal("seed expired transaction")
		}
		defer func() {
			if _, err := owner.Exec(ctx, "DELETE FROM tutorhub.auth_transactions WHERE id=$1", id); err != nil {
				t.Error("clean disposable transaction")
			}
		}()
	}
	for _, invocation := range []struct {
		args []string
		want []int
	}{
		{[]string{"--batch-size", "1", "--max-batches", "1"}, []int{1}},
		{[]string{"--batch-size", "1"}, []int{1, 1, 0}},
		{nil, []int{0}},
	} {
		var out bytes.Buffer
		if code := run(ctx, invocation.args, u.String(), &out); code != 0 || strings.Contains(out.String(), password) || strings.Contains(out.String(), roleName) {
			t.Fatal("maintenance invocation failed or logged its credentials")
		}
		lines := strings.Split(strings.TrimSpace(out.String()), "\n")
		if len(lines) != len(invocation.want) {
			t.Fatal("unexpected number of batches")
		}
		for i, line := range lines {
			var record map[string]any
			if json.Unmarshal([]byte(line), &record) != nil || record["msg"] != "auth maintenance batch" || len(record) != len(retentionTables)+3 {
				t.Fatal("expected JSON log with only standard metadata and fixed table counts")
			}
			for _, table := range retentionTables {
				want := 0
				if table == "auth_transactions" {
					want = invocation.want[i]
				}
				if record[table] != float64(want) {
					t.Fatal("unexpected retention count", table)
				}
			}
		}
	}
}
