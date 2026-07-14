package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"axiom/internal/backup"
)

func TestPostgresToolArgumentsContainNoPassword(t *testing.T) {
	configuration := settings{
		host: "postgres", port: 5432, database: "axiom", user: "axiom_backup", password: "secret-canary",
	}
	for _, arguments := range [][]string{
		databaseArguments(configuration), restoreArguments(configuration),
	} {
		joined := strings.Join(arguments, " ")
		if strings.Contains(joined, configuration.password) || strings.Contains(strings.ToLower(joined), "password") {
			t.Fatalf("credential-like argument found: %s", joined)
		}
	}
}

func TestPSQLArgumentsSelectConfiguredDatabase(t *testing.T) {
	configuration := settings{host: "postgres", port: 5432, database: "axiom", user: "axiom_backup"}
	arguments := psqlArguments(configuration, "SELECT 1")
	if !containsArgument(arguments, "--dbname=axiom") {
		t.Fatalf("psql arguments omit configured database: %v", arguments)
	}
}

func TestRestoreIsAtomic(t *testing.T) {
	configuration := settings{host: "postgres", port: 5432, database: "axiom", user: "axiom_migrator"}
	arguments := restoreArguments(configuration)
	if !containsArgument(arguments, "--single-transaction") {
		t.Fatalf("restore arguments are not atomic: %v", arguments)
	}
}

func containsArgument(arguments []string, wanted string) bool {
	for _, argument := range arguments {
		if argument == wanted {
			return true
		}
	}
	return false
}

func TestWALBoundaryIsCanonicalHexLSN(t *testing.T) {
	for _, valid := range []string{"0/16B6A50", "FFFFFFFF/FFFFFFFF", "a/b"} {
		if !validWALBoundary(valid) {
			t.Fatalf("valid WAL boundary rejected: %s", valid)
		}
	}
	for _, invalid := range []string{"", "0", "/1", "1/", "1/2/3", "GG/1", "123456789/1"} {
		if validWALBoundary(invalid) {
			t.Fatalf("invalid WAL boundary accepted: %s", invalid)
		}
	}
}

func TestTemporaryPassfileIsOwnerOnlyAndRemovable(t *testing.T) {
	configuration := settings{
		host: "postgres", port: 5432, database: "axiom", user: "axiom_backup", password: "fixture-value",
	}
	path, cleanup, err := createPassfile(configuration)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("passfile mode = %v, %v", info.Mode().Perm(), err)
	}
	cleanup()
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("temporary passfile survived cleanup")
	}
}

func TestMigrationVersionIsCanonical(t *testing.T) {
	for _, valid := range []string{"000001", "999999"} {
		if !regexpMigrationVersion(valid) {
			t.Fatalf("valid version rejected: %s", valid)
		}
	}
	for _, invalid := range []string{"1", "00000x", "0000001", ""} {
		if regexpMigrationVersion(invalid) {
			t.Fatalf("invalid version accepted: %s", invalid)
		}
	}
}

func TestPGPassFieldsAreEscapedAndNewlinesRejected(t *testing.T) {
	if actual := pgpassEscape(`value:with\separator`); actual != `value\:with\\separator` {
		t.Fatalf("escaped field = %q", actual)
	}
	for _, value := range []string{"", "line\nbreak", "line\rbreak", "nul\x00byte"} {
		if safePGPassField(value) {
			t.Fatalf("unsafe field accepted: %q", value)
		}
	}
}

func TestArchiveValidationStreamsAuthenticatedPlaintextToLister(t *testing.T) {
	root := t.TempDir()
	key := [32]byte{1, 2, 3}
	spec := backup.ArtifactSpec{
		Name: "axiom-validation", Database: "axiom", SchemaVersion: "000003",
		ToolVersion: "pg_dump (PostgreSQL) 18.4", ValidatorVersion: "pg_restore (PostgreSQL) 18.4",
		WALBoundary: "0/16B6A50", StartedAt: time.Now().Add(-time.Second).UTC(),
	}
	manifest, err := backup.CreateArtifact(root, spec, bytes.NewReader([]byte("archive-fixture")), key)
	if err != nil {
		t.Fatal(err)
	}
	if err = validateArchiveWithCommand(root, manifest, key, exec.CommandContext(context.Background(), "/bin/sh", "-c", "cat >/dev/null")); err != nil {
		t.Fatal(err)
	}
	if err = validateArchiveWithCommand(root, manifest, key, exec.CommandContext(context.Background(), "/bin/sh", "-c", "cat >/dev/null; exit 1")); err == nil {
		t.Fatal("failed archive lister accepted")
	}
	wrongKey := key
	wrongKey[0]++
	if err = validateArchiveWithCommand(root, manifest, wrongKey, exec.CommandContext(context.Background(), "/bin/sh", "-c", "cat >/dev/null")); err == nil {
		t.Fatal("unauthenticated archive accepted")
	}
	if _, err = os.Stat(filepath.Join(root, manifest.Path)); err != nil {
		t.Fatal("validation unexpectedly modified artifact")
	}
}

func TestRestoreVerificationQueriesCoverCleanTargetAndAccountingTruth(t *testing.T) {
	for _, required := range []string{"pg_namespace", "pg_class", "pg_proc", "pg_type"} {
		if !strings.Contains(targetCleanQuery, required) {
			t.Fatalf("clean-target query omits %s", required)
		}
	}
	for _, required := range []string{
		"journal_transactions", "ledger_entries", "virtual_balances", "positions",
		"reservations", "'active','quarantined'", "full outer join",
	} {
		if !strings.Contains(strings.ToLower(restoredIntegrityQuery), required) {
			t.Fatalf("restore-integrity query omits %s", required)
		}
	}
}
