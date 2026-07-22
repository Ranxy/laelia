package migration

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/blang/semver/v4"
	_ "github.com/jackc/pgx/v5"        // register the pgx driver for database/sql.
	_ "github.com/jackc/pgx/v5/stdlib" // register the "pgx" stdlib driver.
)

func TestGetVersionFromPath(t *testing.T) {
	tests := []struct {
		path    string
		want    string
		wantErr bool
	}{
		{"migration/1.0/0000##init.sql", "1.0.0", false},
		{"migration/1.0/0001##add_col.sql", "1.0.1", false},
		{"migration/2.13/0021##migrate_users.sql", "2.13.21", false},
		{"migration/LATEST.sql", "", true},        // not a versioned migration
		{"migration/1.0/add_col.sql", "", true},   // missing ## separator
		{"migration/1.0.sql", "", true},           // wrong depth
		{"migration/1.0/00ab##bad.sql", "", true}, // non-numeric patch
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got, err := getVersionFromPath(tt.path)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.String() != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestGetSortedVersionedFiles(t *testing.T) {
	fsys := fstest.MapFS{
		"migration/LATEST.sql":           {Data: []byte("-- baseline")},
		"migration/1.0/0002##b.sql":      {Data: []byte("-- b")},
		"migration/1.0/0001##a.sql":      {Data: []byte("-- a")},
		"migration/1.0/0000##seed.sql":   {Data: []byte("-- seed")},
		"migration/0.9/0005##legacy.sql": {Data: []byte("-- legacy")},
	}

	files, err := getSortedVersionedFiles(fsys)
	if err != nil {
		t.Fatalf("getSortedVersionedFiles: %v", err)
	}

	want := []string{"0.9.5", "1.0.0", "1.0.1", "1.0.2"}
	if len(files) != len(want) {
		t.Fatalf("got %d files, want %d (%v)", len(files), len(want), files)
	}
	seen := make(map[string]bool)
	for i, f := range files {
		if f.version.String() != want[i] {
			t.Errorf("files[%d] = %s, want %s", i, f.version, want[i])
		}
		if seen[f.version.String()] {
			t.Errorf("duplicate version %s", f.version)
		}
		seen[f.version.String()] = true
	}
}

func TestGetSortedVersionedFilesExcludesLATEST(t *testing.T) {
	fsys := fstest.MapFS{
		"migration/LATEST.sql": {Data: []byte("-- baseline only")},
	}
	files, err := getSortedVersionedFiles(fsys)
	if err != nil {
		t.Fatalf("getSortedVersionedFiles: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("expected no versioned files, got %v", files)
	}
}

func TestComputeLatestVersion(t *testing.T) {
	t.Run("empty falls back to baseline", func(t *testing.T) {
		got := computeLatestVersion(nil)
		if got.String() != baselineVersion {
			t.Fatalf("got %s, want %s", got, baselineVersion)
		}
	})

	t.Run("max of files", func(t *testing.T) {
		files := []versionedFile{
			{version: ptrVersion("1.0.0")},
			{version: ptrVersion("1.0.3")},
			{version: ptrVersion("1.0.2")},
		}
		got := computeLatestVersion(files)
		if got.String() != "1.0.3" {
			t.Fatalf("got %s, want 1.0.3", got)
		}
	})

	t.Run("files below baseline keep baseline", func(t *testing.T) {
		files := []versionedFile{
			{version: ptrVersion("0.9.5")},
		}
		got := computeLatestVersion(files)
		if got.String() != baselineVersion {
			t.Fatalf("got %s, want %s", got, baselineVersion)
		}
	})
}

func TestMigratorEmbedContainsLATEST(t *testing.T) {
	if _, err := migrationFS.ReadFile(latestSchemaFileName); err != nil {
		t.Fatalf("embedded %q not readable: %v", latestSchemaFileName, err)
	}
}

func ptrVersion(v string) *semver.Version {
	s := semver.MustParse(v)
	return &s
}

// --- Integration tests (gated) ---
//
// These exercise the migrator against a real Postgres. They are skipped unless
// LAELIA_RUN_MIGRATION_TESTS=1 is set, mirroring the LAELIA_RUN_OPENCODE_ACP_TESTS
// pattern.
//
// Two flavors:
//   - TestExecuteMigration_MultiStatementDollarQuote: runs in a unique temp
//     SCHEMA inside the LAELIA_TEST_PG_URL database (only needs CREATE-schema
//     privilege, which the DB owner has). This is the verification of the key
//     pgx risk: that a multi-statement string — including the actual `DO $$ ...
//     END $$` dollar-quoted block from LATEST.sql — executes in a single
//     ExecContext within one transaction, with atomic version recording and
//     rollback on failure. (The full cumulative LATEST.sql cannot run in a
//     shared database: pinning search_path hides pg_trgm, while adding public
//     shadows the real tables — so the full-file path is covered by the
//     CREATEDB-gated test below against a clean throwaway database.)
//   - TestMigrateSchema_{FreshInstall,Upgrade}: end-to-end MigrateSchema flows
//     that need a clean throwaway DATABASE per subtest, so they require the test
//     user to have CREATEDB. They skip when the user lacks CREATEDB.

func requireMigrationTests(t *testing.T) string {
	t.Helper()
	if os.Getenv("LAELIA_RUN_MIGRATION_TESTS") != "1" {
		t.Skip("set LAELIA_RUN_MIGRATION_TESTS=1 to run migration integration tests")
	}
	rootURL := os.Getenv("LAELIA_TEST_PG_URL")
	if rootURL == "" {
		t.Skip("set LAELIA_TEST_PG_URL to a Postgres URL for migration integration tests")
	}
	return rootURL
}

// integrationSchema opens a connection to the LAELIA_TEST_PG_URL database and
// creates a uniquely named throwaway schema, returning a *sql.DB and the schema
// name. The schema is dropped on cleanup. Only needs CREATE-schema privilege.
func integrationSchema(t *testing.T) (*sql.DB, string) {
	t.Helper()
	rootURL := requireMigrationTests(t)

	db, err := sql.Open("pgx", rootURL)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	schema := fmt.Sprintf("laelia_migtest_%d_%d", os.Getpid(), atomic.AddInt64(&testDBCounter, 1))
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`CREATE SCHEMA "%s"`, schema)); err != nil {
		t.Fatalf("create schema %s: %v", schema, err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf(`DROP SCHEMA IF EXISTS "%s" CASCADE`, schema))
	})
	return db, schema
}

// integrationDB creates a uniquely named throwaway DATABASE and returns a
// connection to it. Requires CREATEDB; skips when the test user lacks it.
func integrationDB(t *testing.T) *sql.DB {
	t.Helper()
	rootURL := requireMigrationTests(t)

	root, err := sql.Open("pgx", rootURL)
	if err != nil {
		t.Fatalf("open root: %v", err)
	}
	t.Cleanup(func() { _ = root.Close() })

	// CREATEDB is required to isolate each subtest in its own database. Skip
	// cleanly when the test user lacks it rather than failing noisily.
	var canCreateDB bool
	if err := root.QueryRowContext(context.Background(),
		"SELECT rolcreatedb FROM pg_roles WHERE rolname = current_user").Scan(&canCreateDB); err != nil {
		t.Fatalf("check createdb: %v", err)
	}
	if !canCreateDB {
		t.Skip("LAELIA_TEST_PG_URL user lacks CREATEDB; skipping end-to-end MigrateSchema tests (run TestExecuteMigration_MultiStatementDollarQuote for the pgx multi-statement check)")
	}

	name := fmt.Sprintf("laelia_migtest_%d_%d", os.Getpid(), atomic.AddInt64(&testDBCounter, 1))
	if _, err := root.ExecContext(context.Background(),
		fmt.Sprintf(`CREATE DATABASE "%s"`, name)); err != nil {
		t.Fatalf("create db %s: %v", name, err)
	}
	t.Cleanup(func() {
		_, _ = root.ExecContext(context.Background(),
			fmt.Sprintf(`DROP DATABASE IF EXISTS "%s" WITH (FORCE)`, name))
	})

	testURL := replaceDatabase(rootURL, name)
	db, err := sql.Open("pgx", testURL)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

var testDBCounter int64

// TestExecuteMigration_MultiStatementDollarQuote verifies the key pgx
// assumption: that a multi-statement SQL string containing a `DO $$ ... END $$`
// dollar-quoted plpgsql block executes in a single ExecContext inside one
// transaction, and that the version is recorded atomically with it.
//
// It uses the actual DO $$ block from LATEST.sql (the conversation agent-DM
// order CHECK constraint) plus the table and index it depends on. Running the
// full LATEST.sql here is not possible in a shared database: pinning search_path
// to a temp schema hides the pg_trgm extension (installed in public), while
// adding public to search_path would shadow the real laelia tables and mutate
// them. The full cumulative file is exercised end-to-end by the CREATEDB-gated
// TestMigrateSchema_FreshInstall against a clean throwaway database. This test
// covers the same execution mechanism (multi-statement + dollar-quote in one
// ExecContext/txn) with a self-contained slice that needs only CREATE-schema
// privilege.
func TestExecuteMigration_MultiStatementDollarQuote(t *testing.T) {
	db, schema := integrationSchema(t)
	ctx := context.Background()

	// Dedicated connection with search_path pinned to the temp schema so all
	// unqualified DDL and the version-table INSERT land there.
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("acquire conn: %v", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, fmt.Sprintf(`SET search_path = "%s"`, schema)); err != nil {
		t.Fatalf("set search_path: %v", err)
	}

	// executeMigration records the version, so the history table must exist first.
	if _, err := conn.ExecContext(ctx, fmt.Sprintf(`
CREATE TABLE %s (id bigserial PRIMARY KEY, version text NOT NULL);
CREATE UNIQUE INDEX idx_test_history_unique ON %s (version);`, schemaMigrationHistory, schemaMigrationHistory)); err != nil {
		t.Fatalf("create history table: %v", err)
	}

	// The actual DO $$ block from LATEST.sql, with the table + index it depends
	// on. Sent as one multi-statement string in a single ExecContext.
	stmt := `
CREATE TABLE IF NOT EXISTS conversation (
    id serial PRIMARY KEY,
    agent_dm_a integer,
    agent_dm_b integer,
    type integer
);
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'conversation_agent_dm_order_check') THEN
        ALTER TABLE conversation ADD CONSTRAINT conversation_agent_dm_order_check
            CHECK (agent_dm_a IS NULL OR agent_dm_b IS NULL OR agent_dm_a < agent_dm_b);
    END IF;
END $$;
CREATE INDEX IF NOT EXISTS idx_conversation_agent_dm ON conversation (agent_dm_a, agent_dm_b);`

	if err := executeMigration(ctx, conn, stmt, baselineVersion); err != nil {
		t.Fatalf("executeMigration failed (multi-statement/dollar-quote not supported by pgx?): %v", err)
	}

	// The table was created in the temp schema.
	var hasTable bool
	if err := conn.QueryRowContext(ctx,
		"SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = $1 AND table_name = 'conversation')",
		schema).Scan(&hasTable); err != nil {
		t.Fatalf("check conversation table: %v", err)
	}
	if !hasTable {
		t.Fatal("conversation table not created in temp schema")
	}

	// The DO $$ block added the constraint; its presence proves the
	// dollar-quoted block executed within the single ExecContext.
	var hasConstraint bool
	if err := conn.QueryRowContext(ctx,
		"SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'conversation_agent_dm_order_check')").Scan(&hasConstraint); err != nil {
		t.Fatalf("check constraint: %v", err)
	}
	if !hasConstraint {
		t.Fatal("DO $$ block did not execute (constraint missing)")
	}

	// The version was recorded atomically with the schema change.
	var version string
	if err := conn.QueryRowContext(ctx,
		fmt.Sprintf("SELECT version FROM %s ORDER BY id DESC LIMIT 1", schemaMigrationHistory)).Scan(&version); err != nil {
		t.Fatalf("query version: %v", err)
	}
	if version != baselineVersion {
		t.Fatalf("recorded version = %s, want %s", version, baselineVersion)
	}

	// A failed migration must not record its version. Re-run with a bad
	// statement under a new version and confirm neither the change nor the
	// version row landed (the transaction rolled back).
	badStmt := `CREATE TABLE IF NOT EXISTS should_rollback (id int); BOGUS SYNTAX HERE;`
	if err := executeMigration(ctx, conn, badStmt, "1.0.1"); err == nil {
		t.Fatal("expected executeMigration to fail on bad SQL")
	}
	var hasRollbackTable bool
	_ = conn.QueryRowContext(ctx,
		"SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = $1 AND table_name = 'should_rollback')",
		schema).Scan(&hasRollbackTable)
	if hasRollbackTable {
		t.Fatal("failed migration leaked should_rollback table (transaction not rolled back)")
	}
	assertHistoryVersionsConn(t, conn, baselineVersion)
}

func TestMigrateSchema_FreshInstall(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()

	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("MigrateSchema (fresh): %v", err)
	}

	if !tableExistsQ(t, db, "principal") {
		t.Fatal("principal table missing after fresh install")
	}
	if !tableExistsQ(t, db, schemaMigrationHistory) {
		t.Fatal("schema_migration_history missing after fresh install")
	}
	assertHistoryVersions(t, db, "1.0.0")

	// Second run is a no-op: still one row, no error.
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("MigrateSchema (idempotent re-run): %v", err)
	}
	assertHistoryVersions(t, db, "1.0.0")
}

func TestMigrateSchema_Upgrade(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()

	// Fresh install with the real embedded tree (no incrementals yet).
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("MigrateSchema (fresh): %v", err)
	}
	assertHistoryVersions(t, db, "1.0.0")

	// Inject a fake newer incremental migration via a test FS. The upgrade path
	// does not re-read LATEST.sql (schema already exists), so the test FS only
	// needs the incremental file.
	latestBuf, err := fs.ReadFile(migrationFS, latestSchemaFileName)
	if err != nil {
		t.Fatalf("read embedded LATEST.sql: %v", err)
	}
	testFS := fstest.MapFS{
		"migration/LATEST.sql":         {Data: latestBuf},
		"migration/1.0/0001##test.sql": {Data: []byte("CREATE TABLE IF NOT EXISTS test_upgrade_marker (id int);")},
	}

	if err := migrateSchemaFS(ctx, db, testFS); err != nil {
		t.Fatalf("migrateSchemaFS (upgrade): %v", err)
	}

	if !tableExistsQ(t, db, "test_upgrade_marker") {
		t.Fatal("upgrade migration did not create test_upgrade_marker")
	}
	assertHistoryVersions(t, db, "1.0.0", "1.0.1")
}

// --- helpers ---

func tableExistsQ(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var ok bool
	if err := db.QueryRowContext(context.Background(),
		"SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)",
		table).Scan(&ok); err != nil {
		t.Fatalf("tableExists %s: %v", table, err)
	}
	return ok
}

func assertHistoryVersions(t *testing.T, db *sql.DB, want ...string) {
	t.Helper()
	rows, err := db.QueryContext(context.Background(),
		fmt.Sprintf("SELECT version FROM %s ORDER BY id ASC", schemaMigrationHistory))
	if err != nil {
		t.Fatalf("query history: %v", err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows err: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("history versions = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("history versions = %v, want %v", got, want)
		}
	}
}

// replaceDatabase swaps the database name in a Postgres URL.
func replaceDatabase(pgURL, db string) string {
	if i := strings.LastIndex(pgURL, "/"); i != -1 {
		base := pgURL[:i+1]
		rest := pgURL[i+1:]
		if j := strings.IndexAny(rest, "?"); j != -1 {
			return base + db + rest[j:]
		}
		return base + db
	}
	return pgURL
}

// assertHistoryVersionsConn is the *sql.Conn variant of assertHistoryVersions
// for tests that pin search_path on a dedicated connection.
func assertHistoryVersionsConn(t *testing.T, conn *sql.Conn, want ...string) {
	t.Helper()
	rows, err := conn.QueryContext(context.Background(),
		fmt.Sprintf("SELECT version FROM %s ORDER BY id ASC", schemaMigrationHistory))
	if err != nil {
		t.Fatalf("query history: %v", err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows err: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("history versions = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("history versions = %v, want %v", got, want)
		}
	}
}
