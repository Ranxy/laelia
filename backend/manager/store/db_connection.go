package store

import (
	"context"
	"database/sql"

	_ "github.com/jackc/pgx/v5" // pgx driver
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/pkg/errors"
)

type DBConnectionManager struct {
	db          *sql.DB
	pgURL       string
	stopWatcher chan struct{}
}

func NewDBConnectionManager(pgURL string) *DBConnectionManager {
	return &DBConnectionManager{
		pgURL:       pgURL,
		stopWatcher: make(chan struct{}),
	}
}

func (m *DBConnectionManager) Initialize(ctx context.Context) error {
	if m.pgURL == "" {
		return errors.New("database URL is not provided")
	}

	var err error
	db, err := createConnection(ctx, m.pgURL)
	if err != nil {
		return err
	}

	m.db = db
	return nil
}

// GetDB returns the current database connection.
func (m *DBConnectionManager) GetDB() *sql.DB {
	return m.db
}

func (m *DBConnectionManager) Close() error {
	if m.db != nil {
		return m.db.Close()
	}
	return nil
}

func createConnection(ctx context.Context, pgURL string) (*sql.DB, error) {
	db, err := sql.Open("pgx", pgURL)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open database connection")
	}

	// Validate connection
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, errors.Wrap(err, "failed to ping database")
	}

	// Configure connection pool
	var maxConns, reservedConns int
	if err := db.QueryRowContext(ctx, `SHOW max_connections`).Scan(&maxConns); err != nil {
		_ = db.Close()
		return nil, errors.Wrap(err, "failed to get max_connections")
	}
	if err := db.QueryRowContext(ctx, `SHOW superuser_reserved_connections`).Scan(&reservedConns); err != nil {
		_ = db.Close()
		return nil, errors.Wrap(err, "failed to get superuser_reserved_connections")
	}

	maxOpenConns := maxConns - reservedConns
	if maxOpenConns > 50 {
		maxOpenConns = 50
	}
	db.SetMaxOpenConns(maxOpenConns)

	return db, nil
}
