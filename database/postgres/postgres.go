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

func NewConnManager(host string, port int, dbName, user, password string, options ...Option) (ConnManager, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s dbname=%s password=%s sslmode=disable TimeZone=UTC",
		host, port, user, dbName, password,
	)

	db, err := sqlx.Connect("postgres", dsn)
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

type Option func(*defaultConnManager)

func WithMaxOpenConns(n int) Option {
	return Option(func(cm *defaultConnManager) {
		if n <= 0 {
			n = DefaultMaxOpenConns
		}
		cm.dbConn.SetMaxOpenConns(n)
	})
}

func WithMaxIdleConns(n int) Option {
	return Option(func(cm *defaultConnManager) {
		if n <= 0 {
			n = DefaultMaxIdleConns
		}
		cm.dbConn.SetMaxIdleConns(n)
	})
}

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
