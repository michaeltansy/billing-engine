package database

import (
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

const (
	// DefaultMaxOpenConns caps concurrent connections held by the pool.
	DefaultMaxOpenConns = 25
	// DefaultMaxIdleConns keeps a few connections warm to avoid reconnect churn.
	DefaultMaxIdleConns = 5
	// DefaultConnMaxLifetime recycles connections so none live forever.
	DefaultConnMaxLifetime = time.Hour
	// DefaultConnMaxIdleTime reaps connections that sat unused.
	DefaultConnMaxIdleTime = 30 * time.Minute
)

type defaultConnManager struct {
	dbConn *sqlx.DB
}

// GetDB returns the Postgres connection.
func (cm *defaultConnManager) GetDB() *sqlx.DB {
	return cm.dbConn
}

func (cm *defaultConnManager) Close() error {
	return cm.dbConn.Close()
}

// NewConnManager opens a Postgres connection pool and verifies it with a Ping.
//
// *sqlx.DB is itself a pool and is safe for concurrent use by many goroutines:
// each request borrows a connection and returns it when done. The pool bounds
// how many payment requests can hit Postgres at once, while the database
// enforces correctness (see the unique constraints in sql/init.sql).
func NewConnManager(host, dbName, user, password string, options ...Option) (ConnManager, error) {
	db, err := sqlx.Connect("postgres", fmt.Sprintf("host=%s user=%s dbname=%s password=%s sslmode=disable", host, user, dbName, password))
	if err != nil {
		return nil, err
	}

	cm := &defaultConnManager{dbConn: db}

	db.SetMaxOpenConns(DefaultMaxOpenConns)
	db.SetMaxIdleConns(DefaultMaxIdleConns)
	db.SetConnMaxLifetime(DefaultConnMaxLifetime)
	db.SetConnMaxIdleTime(DefaultConnMaxIdleTime)

	for _, opt := range options {
		opt(cm)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}

	return cm, nil
}

// Option customises the connection pool, following the functional options
// pattern used by the HTTP handlers.
type Option func(*defaultConnManager)

// WithMaxOpenConns overrides the maximum number of open connections.
func WithMaxOpenConns(n int) Option {
	return Option(func(cm *defaultConnManager) {
		if n <= 0 {
			n = DefaultMaxOpenConns
		}
		cm.dbConn.SetMaxOpenConns(n)
	})
}

// WithMaxIdleConns overrides the number of idle connections kept warm.
func WithMaxIdleConns(n int) Option {
	return Option(func(cm *defaultConnManager) {
		if n <= 0 {
			n = DefaultMaxIdleConns
		}
		cm.dbConn.SetMaxIdleConns(n)
	})
}

// WithConnMaxLifetime overrides how long a connection may be reused.
func WithConnMaxLifetime(d time.Duration) Option {
	return Option(func(cm *defaultConnManager) {
		if d <= 0 {
			d = DefaultConnMaxLifetime
		}
		cm.dbConn.SetConnMaxLifetime(d)
	})
}

// ConnManager provides mechanism to access Postgres connection.
//
//go:generate mockgen -source=./postgres.go -destination=mockconnmanager/mock_connmanager.go -package=mockconnmanager github.com/michaeltansy/billing-engine/database/postgres ConnManager
type ConnManager interface {
	GetDB() *sqlx.DB
	Close() error
}
