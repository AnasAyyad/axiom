package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"axiom/internal/backup"
	"axiom/internal/security"
)

const commandTimeout = 4 * time.Hour

func main() {
	if err := run(os.Args[1:]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	if len(arguments) != 1 || (arguments[0] != "create" && arguments[0] != "restore") {
		return fmt.Errorf("backup_command_invalid")
	}
	settings, err := loadSettings()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	passfile, cleanup, err := createPassfile(settings)
	if err != nil {
		return err
	}
	defer cleanup()
	if arguments[0] == "create" {
		return create(ctx, settings, passfile)
	}
	return restore(ctx, settings, passfile)
}

type settings struct {
	host        string
	port        uint16
	database    string
	user        string
	password    string
	destination string
	key         [32]byte
	manifest    string
	retain      int
}

func loadSettings() (settings, error) {
	port, err := strconv.ParseUint(environment("DB_PORT", "5432"), 10, 16)
	if err != nil || port == 0 {
		return settings{}, fmt.Errorf("backup_database_invalid")
	}
	retention, err := strconv.Atoi(environment("BACKUP_RETENTION_GENERATIONS", "14"))
	if err != nil || retention < backup.MinimumRetainedGenerations {
		return settings{}, fmt.Errorf("backup_retention_invalid")
	}
	password, err := security.ReadSecretFile(environment("DB_PASSWORD_FILE", "/run/secrets/postgres_backup_password"))
	if err != nil {
		return settings{}, err
	}
	keyText, err := security.ReadSecretFile(environment("BACKUP_ENCRYPTION_KEY_FILE", "/run/secrets/backup_encryption_key"))
	if err != nil {
		return settings{}, err
	}
	key, err := backup.DecodeKey(keyText)
	if err != nil {
		return settings{}, err
	}
	value := settings{
		host: environment("DB_HOST", "postgres"), port: uint16(port),
		database: environment("DB_NAME", "axiom"), user: environment("DB_USER", "axiom_backup"),
		password: password, destination: environment("BACKUP_DESTINATION", "/backups"), key: key,
		manifest: os.Getenv("BACKUP_RESTORE_MANIFEST"), retain: retention,
	}
	if !safePGPassField(value.host) || !safePGPassField(value.database) || !safePGPassField(value.user) ||
		!safePGPassField(value.password) || !filepath.IsAbs(value.destination) {
		return settings{}, fmt.Errorf("backup_configuration_invalid")
	}
	return value, nil
}

func create(ctx context.Context, settings settings, passfile string) error {
	started := time.Now().UTC()
	spec, err := artifactSpec(ctx, settings, passfile, started)
	if err != nil {
		return err
	}
	command := exec.CommandContext(ctx, "pg_dump", databaseArguments(settings)...)
	command.Env = append(os.Environ(), "PGPASSFILE="+passfile)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return fmt.Errorf("backup_dump_unavailable")
	}
	command.Stderr = io.Discard
	if err = command.Start(); err != nil {
		return fmt.Errorf("backup_dump_unavailable")
	}
	reader := &processReader{reader: stdout, command: command}
	manifest, err := backup.CreateArtifact(settings.destination, spec, reader, settings.key)
	if err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return err
	}
	if err = validateArchive(ctx, settings.destination, manifest, settings.key); err != nil {
		if quarantineErr := backup.QuarantineArtifact(settings.destination, manifest, settings.key); quarantineErr != nil {
			return fmt.Errorf("backup_archive_validation_and_quarantine_failed")
		}
		return err
	}
	removed, err := backup.PruneArtifacts(settings.destination, settings.key, settings.retain)
	if err != nil {
		return fmt.Errorf("backup_complete_retention_failed")
	}
	_, _ = fmt.Fprintf(os.Stdout, "backup_complete name=%s sha256=%s size=%d pruned=%d\n", manifest.Spec.Name, manifest.SHA256, manifest.Size, len(removed))
	return nil
}

func artifactSpec(ctx context.Context, settings settings, passfile string, started time.Time) (backup.ArtifactSpec, error) {
	schema, err := schemaVersion(ctx, settings, passfile)
	if err != nil {
		return backup.ArtifactSpec{}, err
	}
	dumpVersion, err := postgresToolVersion(ctx, "pg_dump")
	if err != nil {
		return backup.ArtifactSpec{}, err
	}
	restoreVersion, err := postgresToolVersion(ctx, "pg_restore")
	if err != nil {
		return backup.ArtifactSpec{}, err
	}
	walBoundary, err := currentWALBoundary(ctx, settings, passfile)
	if err != nil {
		return backup.ArtifactSpec{}, err
	}
	return backup.ArtifactSpec{
		Name: "axiom-" + started.Format("20060102t150405z"), Database: settings.database,
		SchemaVersion: schema, ToolVersion: dumpVersion, ValidatorVersion: restoreVersion,
		WALBoundary: walBoundary, StartedAt: started,
	}, nil
}

func validateArchive(ctx context.Context, root string, manifest backup.ArtifactManifest, key [32]byte) error {
	command := exec.CommandContext(ctx, "pg_restore", "--list")
	return validateArchiveWithCommand(root, manifest, key, command)
}

func validateArchiveWithCommand(root string, manifest backup.ArtifactManifest, key [32]byte, command *exec.Cmd) error {
	if command == nil {
		return fmt.Errorf("backup_archive_validation_unavailable")
	}
	command.Stdout, command.Stderr = io.Discard, io.Discard
	stdin, err := command.StdinPipe()
	if err != nil {
		return fmt.Errorf("backup_archive_validation_unavailable")
	}
	if err = command.Start(); err != nil {
		return fmt.Errorf("backup_archive_validation_unavailable")
	}
	decryptErr := backup.RestoreArtifact(root, manifest, stdin, key)
	closeErr := stdin.Close()
	waitErr := command.Wait()
	if decryptErr != nil || closeErr != nil || waitErr != nil {
		return fmt.Errorf("backup_archive_validation_failed")
	}
	return nil
}

func restore(ctx context.Context, settings settings, passfile string) error {
	if !filepath.IsAbs(settings.manifest) {
		return fmt.Errorf("restore_manifest_invalid")
	}
	manifest, err := backup.ReadArtifactManifest(settings.manifest)
	if err != nil || manifest.Spec.Database != settings.database {
		return fmt.Errorf("restore_manifest_invalid")
	}
	root := filepath.Dir(settings.manifest)
	if err = backup.RestoreArtifact(root, manifest, io.Discard, settings.key); err != nil {
		return err
	}
	if err = validateArchive(ctx, root, manifest, settings.key); err != nil {
		return err
	}
	empty, err := targetIsEmpty(ctx, settings, passfile)
	if err != nil || !empty {
		return fmt.Errorf("restore_target_not_clean")
	}
	command := exec.CommandContext(ctx, "pg_restore", restoreArguments(settings)...)
	command.Env = append(os.Environ(), "PGPASSFILE="+passfile)
	stdin, err := command.StdinPipe()
	if err != nil {
		return fmt.Errorf("restore_process_unavailable")
	}
	command.Stdout, command.Stderr = io.Discard, io.Discard
	if err = command.Start(); err != nil {
		return fmt.Errorf("restore_process_unavailable")
	}
	decryptErr := backup.RestoreArtifact(root, manifest, stdin, settings.key)
	closeErr := stdin.Close()
	waitErr := command.Wait()
	if decryptErr != nil || closeErr != nil || waitErr != nil {
		return fmt.Errorf("restore_failed")
	}
	if err = verifyRestoredDatabase(ctx, settings, passfile, manifest); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(os.Stdout, "restore_complete name=%s schema=%s\n", manifest.Spec.Name, manifest.Spec.SchemaVersion)
	return nil
}

type processReader struct {
	reader  io.Reader
	command *exec.Cmd
	waited  bool
}

// Read converts a failed pg_dump exit into a streaming read failure.
func (reader *processReader) Read(buffer []byte) (int, error) {
	count, err := reader.reader.Read(buffer)
	if err != io.EOF || reader.waited {
		return count, err
	}
	reader.waited = true
	if waitErr := reader.command.Wait(); waitErr != nil {
		return count, fmt.Errorf("backup_dump_failed")
	}
	return count, io.EOF
}

func schemaVersion(ctx context.Context, settings settings, passfile string) (string, error) {
	output, err := runPSQL(ctx, settings, passfile, "SELECT max(version) FROM schema_migrations")
	if err != nil || !regexpMigrationVersion(output) {
		return "", fmt.Errorf("backup_schema_unavailable")
	}
	return output, nil
}

func targetIsEmpty(ctx context.Context, settings settings, passfile string) (bool, error) {
	output, err := runPSQL(ctx, settings, passfile, targetCleanQuery)
	return output == "0", err
}

func verifyRestoredDatabase(
	ctx context.Context,
	settings settings,
	passfile string,
	manifest backup.ArtifactManifest,
) error {
	version, err := schemaVersion(ctx, settings, passfile)
	if err != nil || version != manifest.Spec.SchemaVersion {
		return fmt.Errorf("restore_schema_verification_failed")
	}
	violations, err := runPSQL(ctx, settings, passfile, restoredIntegrityQuery)
	if err != nil || violations != "0" {
		return fmt.Errorf("restore_integrity_verification_failed")
	}
	return nil
}

func currentWALBoundary(ctx context.Context, settings settings, passfile string) (string, error) {
	value, err := runPSQL(ctx, settings, passfile, "SELECT pg_current_wal_lsn()::text")
	if err != nil || !validWALBoundary(value) {
		return "", fmt.Errorf("backup_wal_boundary_unavailable")
	}
	return value, nil
}

func postgresToolVersion(ctx context.Context, tool string) (string, error) {
	if tool != "pg_dump" && tool != "pg_restore" {
		return "", fmt.Errorf("backup_tool_invalid")
	}
	command := exec.CommandContext(ctx, tool, "--version")
	command.Stderr = io.Discard
	var output bytes.Buffer
	command.Stdout = &output
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("backup_tool_unavailable")
	}
	value := strings.TrimSpace(output.String())
	if len(value) > 128 || !strings.HasPrefix(value, tool+" (PostgreSQL) ") || strings.ContainsAny(value, "\r\n\x00") {
		return "", fmt.Errorf("backup_tool_version_invalid")
	}
	return value, nil
}

func runPSQL(ctx context.Context, settings settings, passfile, query string) (string, error) {
	arguments := psqlArguments(settings, query)
	command := exec.CommandContext(ctx, "psql", arguments...)
	command.Env = append(os.Environ(), "PGPASSFILE="+passfile)
	command.Stderr = io.Discard
	var output bytes.Buffer
	command.Stdout = &output
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("backup_database_unavailable")
	}
	return strings.TrimSpace(output.String()), nil
}

func psqlArguments(settings settings, query string) []string {
	arguments := connectionArguments(settings)
	return append(arguments, "--dbname="+settings.database, "--tuples-only", "--no-align", "--command", query)
}

func databaseArguments(settings settings) []string {
	arguments := connectionArguments(settings)
	return append(arguments,
		"--format=custom", "--compress=0", "--no-owner", "--no-privileges",
		"--dbname="+settings.database, "--lock-wait-timeout=30s")
}

func restoreArguments(settings settings) []string {
	arguments := connectionArguments(settings)
	return append(arguments, "--single-transaction", "--no-owner", "--no-privileges", "--dbname="+settings.database)
}

func connectionArguments(settings settings) []string {
	return []string{"--host=" + settings.host, "--port=" + strconv.Itoa(int(settings.port)), "--username=" + settings.user}
}

func createPassfile(settings settings) (string, func(), error) {
	file, err := os.CreateTemp("", "axiom-pgpass-*")
	if err != nil {
		return "", func() {}, fmt.Errorf("backup_passfile_unavailable")
	}
	cleanup := func() { _ = os.Remove(file.Name()) }
	if err = file.Chmod(0o600); err == nil {
		_, err = fmt.Fprintf(file, "%s:%d:%s:%s:%s\n",
			pgpassEscape(settings.host), settings.port, pgpassEscape(settings.database),
			pgpassEscape(settings.user), pgpassEscape(settings.password))
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("backup_passfile_unavailable")
	}
	return file.Name(), cleanup, nil
}

func safePGPassField(value string) bool {
	return value != "" && !strings.ContainsAny(value, "\r\n\x00")
}

func pgpassEscape(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(value, ":", `\:`)
}

func regexpMigrationVersion(value string) bool {
	if len(value) != 6 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func validWALBoundary(value string) bool {
	left, right, found := strings.Cut(value, "/")
	if !found || left == "" || right == "" || len(left) > 8 || len(right) > 8 {
		return false
	}
	for _, part := range []string{left, right} {
		for _, character := range part {
			if !((character >= '0' && character <= '9') || (character >= 'A' && character <= 'F') ||
				(character >= 'a' && character <= 'f')) {
				return false
			}
		}
	}
	_, leftErr := strconv.ParseUint(left, 16, 32)
	_, rightErr := strconv.ParseUint(right, 16, 32)
	return leftErr == nil && rightErr == nil
}

func environment(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
