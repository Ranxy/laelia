// Package migration applies and upgrades the manager metadata schema.
//
// This is a port of bytebase's metadata migrator
// (backend/migrator/migrator.go): embedded, forward-only, semver-ordered SQL
// migrations with a single cumulative baseline file for fresh installs.
//
// # Layout
//
// The migration tree is embedded via go:embed:
//
//	migration/
//	  LATEST.sql                  full cumulative schema at the newest version
//	  {MAJOR.MINOR}/
//	    {NNNN}##{desc}.sql        incremental migration, version MAJOR.MINOR.NNNN
//
// # Dual-maintenance model
//
// LATEST.sql is the cumulative schema at the newest version and is applied only
// to fresh installs. Every future schema change must BOTH (a) append the
// idempotent DDL to migration/LATEST.sql AND (b) add an incremental file at
// migration/{MAJOR.MINOR}/{NNNN}##{desc}.sql. The incremental file is what
// existing deployments execute at startup; LATEST.sql is never re-run on an
// existing deployment.
package migration

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"slices"
	"strconv"
	"strings"

	"github.com/blang/semver/v4"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pkg/errors"
)

const (
	// latestSchemaFileName is the cumulative baseline applied to fresh installs.
	latestSchemaFileName = "migration/LATEST.sql"

	// baselineVersion is the version recorded for an existing deployment that
	// already has the full schema but predates the migration framework. It is
	// also the floor for the latest version when there are no incremental files.
	baselineVersion = "1.0.0"

	// advisoryLockKey is an arbitrary fixed int64 used as the pg_advisory_lock
	// key so that in HA deployments only one replica runs migrations.
	advisoryLockKey = 9012345678901234

	// schemaSentinelTable is the table whose existence distinguishes a truly
	// fresh install from an existing deployment. principal is one of the first
	// tables LATEST.sql creates.
	schemaSentinelTable = "principal"

	// schemaMigrationHistory is the version-tracking table name.
	schemaMigrationHistory = "schema_migration_history"
)

//go:embed migration
var migrationFS embed.FS

// GoMigrationFunc performs a data migration in Go code. It receives a context
// and a dedicated connection, and should manage its own transactions (e.g.
// batched updates) for optimal performance.
type GoMigrationFunc func(ctx context.Context, conn *sql.Conn) error

// goMigrations is a registry of version-specific Go migrations that run BEFORE
// the SQL migration of the same version. If a Go migration fails, the version
// is not yet recorded in schema_migration_history, so on the next startup both
// the Go and SQL migrations for that version retry. Useful for large batched
// data transformations that are awkward to express in pure SQL.
var goMigrations = map[string]GoMigrationFunc{}

// MigrateSchema migrates the metadata database schema to the latest embedded
// version. It is safe to call on every server startup: fresh installs apply
// LATEST.sql, and existing deployments apply only pending incrementals. A
// session-level advisory lock serializes migrations across replicas.
func MigrateSchema(ctx context.Context, db *sql.DB) error {
	return migrateSchemaFS(ctx, db, migrationFS)
}

// migrateSchemaFS is the testable core of MigrateSchema; fsys is the migration
// tree to draw LATEST.sql and incremental files from (the embedded tree in
// production, an fstest.MapFS in tests).
func migrateSchemaFS(ctx context.Context, db *sql.DB, fsys fs.FS) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to acquire connection")
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", advisoryLockKey); err != nil {
		return errors.Wrap(err, "failed to acquire migration advisory lock")
	}
	defer func() {
		if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", advisoryLockKey); err != nil {
			slog.Error("Failed to release migration advisory lock", "error", err)
		}
	}()

	files, err := getSortedVersionedFiles(fsys)
	if err != nil {
		return err
	}
	latestVersion := computeLatestVersion(files)

	schemaExists, err := tableExists(ctx, conn, schemaSentinelTable)
	if err != nil {
		return errors.Wrap(err, "failed to check schema sentinel table")
	}

	// E1: truly fresh install — apply the cumulative baseline and record the
	// latest version. LATEST.sql creates schema_migration_history itself.
	if !schemaExists {
		buf, err := fs.ReadFile(fsys, latestSchemaFileName)
		if err != nil {
			return errors.Wrapf(err, "failed to read latest schema %q", latestSchemaFileName)
		}
		if err := executeMigration(ctx, conn, string(buf), latestVersion.String()); err != nil {
			return err
		}
		slog.Info(fmt.Sprintf("Initialized database schema with version %s.", latestVersion))
		return nil
	}

	// E2: existing deployment — apply every incremental migration newer than the
	// recorded version. The version table must already exist (it is created by
	// LATEST.sql on fresh installs); an empty table is a corrupt state.
	recorded, err := getLatestDatabaseVersion(ctx, conn)
	if err != nil {
		return err
	}
	if recorded == nil {
		return errors.New("the latest database version is not found")
	}

	for _, f := range files {
		if f.version.LE(*recorded) {
			continue
		}

		buf, err := fs.ReadFile(fsys, f.path)
		if err != nil {
			return errors.Wrapf(err, "failed to read file %q", f.path)
		}
		version := f.version.String()
		slog.Info(fmt.Sprintf("Migrating %s.", version))

		// Run Go migration FIRST if one exists for this version. On failure the
		// version is not recorded and both migrations retry next startup.
		if goMigration, exists := goMigrations[version]; exists {
			slog.Info(fmt.Sprintf("Running Go migration for %s.", version))
			if err := goMigration(ctx, conn); err != nil {
				return errors.Wrapf(err, "Go migration %s failed", version)
			}
		}

		if err := executeMigration(ctx, conn, string(buf), version); err != nil {
			return err
		}
	}

	slog.Info(fmt.Sprintf("Current schema version: %s", latestVersion))
	return nil
}

type versionedFile struct {
	version *semver.Version
	path    string
}

// getSortedVersionedFiles walks fsys (the embedded migration tree) and returns
// every incremental migration file, sorted ascending by semver version. The
// cumulative LATEST.sql is excluded — it is not a versioned migration.
func getSortedVersionedFiles(fsys fs.FS) ([]versionedFile, error) {
	var files []versionedFile
	if err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if path == latestSchemaFileName {
			return nil
		}

		v, err := getVersionFromPath(path)
		if err != nil {
			return err
		}
		files = append(files, versionedFile{
			version: v,
			path:    path,
		})
		return nil
	}); err != nil {
		return nil, err
	}
	slices.SortFunc(files, func(a, b versionedFile) int {
		if a.version.LT(*b.version) {
			return -1
		} else if a.version.GT(*b.version) {
			return 1
		}
		return 0
	})
	return files, nil
}

// getVersionFromPath parses a migration path of the form
// "migration/{MAJOR.MINOR}/{NNNN}##{desc}.sql" into the semver version
// "{MAJOR.MINOR}.{NNNN}". Malformed paths return an error so a bad filename
// fails loudly at startup rather than being silently skipped.
func getVersionFromPath(path string) (*semver.Version, error) {
	s := strings.TrimPrefix(path, "migration/")
	splits := strings.Split(s, "/")
	if len(splits) != 2 {
		return nil, errors.Errorf("invalid migration path %q", path)
	}
	splits2 := strings.Split(splits[1], "##")
	if len(splits2) != 2 {
		return nil, errors.Errorf("invalid migration path %q", path)
	}
	patch, err := strconv.ParseInt(splits2[0], 10, 64)
	if err != nil {
		return nil, errors.Wrapf(err, "migration filename prefix %q should be four digits integer such as '0000'", splits2[0])
	}

	v := fmt.Sprintf("%s.%d", splits[0], patch)
	version, err := semver.Parse(v)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid version %q", v)
	}
	return &version, nil
}

// computeLatestVersion returns the newest migration version, floored at
// baselineVersion so an empty migration tree (only LATEST.sql) yields a valid
// version instead of panicking on files[len(files)-1].
func computeLatestVersion(files []versionedFile) semver.Version {
	latest := semver.MustParse(baselineVersion)
	for _, f := range files {
		if f.version.GT(latest) {
			latest = *f.version
		}
	}
	return latest
}

// executeMigration runs a migration's SQL and records its version in a single
// transaction, so a migration either fully applies and is recorded or fully
// rolls back and is not recorded. The whole file is sent as one multi-statement
// ExecContext (pgx uses the simple protocol when no args are passed, which lets
// the server parse statement boundaries and dollar-quoting).
func executeMigration(ctx context.Context, conn *sql.Conn, statement string, version string) error {
	var currentUser, currentDatabase string
	_ = conn.QueryRowContext(ctx, "SELECT current_user, current_database()").Scan(&currentUser, &currentDatabase)

	txn, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "failed to begin transaction")
	}
	defer txn.Rollback()

	if _, err := txn.ExecContext(ctx, statement); err != nil {
		var sqlState string
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			sqlState = string(pgErr.Code)
		}
		stmtPreview, truncated := truncateString(statement, 100)
		if truncated {
			stmtPreview += "..."
		}
		return errors.Errorf("migration %s failed\n"+
			"Statement: %s\n"+
			"User: %s\n"+
			"Database: %s\n"+
			"Error: %v\n"+
			"SQLSTATE: %s",
			version, stmtPreview, currentUser, currentDatabase, err, sqlState)
	}
	if _, err := txn.ExecContext(ctx,
		fmt.Sprintf("INSERT INTO %s (version) VALUES ($1)", schemaMigrationHistory),
		version,
	); err != nil {
		return errors.Wrapf(err, "failed to record migration version %s", version)
	}

	return txn.Commit()
}

// getLatestDatabaseVersion returns the most recently applied schema version, or
// nil if the version table is empty.
func getLatestDatabaseVersion(ctx context.Context, conn *sql.Conn) (*semver.Version, error) {
	var v string
	if err := conn.QueryRowContext(ctx,
		fmt.Sprintf("SELECT version FROM %s ORDER BY id DESC", schemaMigrationHistory),
	).Scan(&v); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to query latest database version")
	}

	version, err := semver.Make(v)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid version %q", v)
	}
	return &version, nil
}

// tableExists reports whether a table named table exists in the current
// database.
func tableExists(ctx context.Context, conn *sql.Conn, table string) (bool, error) {
	var ok bool
	if err := conn.QueryRowContext(ctx,
		"SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)",
		table,
	).Scan(&ok); err != nil {
		return false, err
	}
	return ok, nil
}

// truncateString returns the first n bytes of s and whether it was truncated.
func truncateString(s string, n int) (string, bool) {
	if len(s) <= n {
		return s, false
	}
	return s[:n], true
}
