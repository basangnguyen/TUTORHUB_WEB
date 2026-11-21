package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tutorhub-v2/core-api/cmd/auth-maintenance/internal/directdb"
)

func TestMaintenanceRejectsConfigurationWithoutLoggingInputs(t *testing.T) {
	const canary = "private-config-canary"
	for _, test := range []struct {
		raw  string
		args []string
	}{
		{raw: ""},
		{raw: "postgresql://operator:" + canary + "@example-POOLER.invalid/db"},
		{raw: "postgresql://direct.invalid/db?host=fallback-pooler.invalid"},
		{raw: canary},
		{args: []string{"--batch-size", canary}},
		{args: []string{"--batch-size", "0"}},
		{args: []string{"--batch-size", "5001"}},
		{args: []string{"--max-batches", "0"}},
		{args: []string{"--max-batches", "101"}},
		{args: []string{canary}},
	} {
		var out bytes.Buffer
		if code := run(t.Context(), test.args, test.raw, &out); code != 2 || strings.Contains(out.String(), canary) {
			t.Fatal("setup failure must report a fixed, credential-free configuration error")
		}
	}
	cfg, err := directdb.Config("postgresql://operator@127.0.0.1/test?application_name=user-input", "tutorhub-auth-maintenance")
	if err != nil || cfg.RuntimeParams["application_name"] != "tutorhub-auth-maintenance" {
		t.Fatal("maintenance connections must set the fixed application name")
	}
}
