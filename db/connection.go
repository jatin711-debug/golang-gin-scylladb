package db

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/gocql/gocql"
	"github.com/scylladb/gocqlx/v3"
)

type ScyllaDB struct {
	Session gocqlx.Session
	config  *Config
}

type Config struct {
	Hosts              []string
	Keyspace           string
	Consistency        gocql.Consistency
	Timeout            time.Duration
	ConnectTimeout     time.Duration
	MaxRetries         int
	RetryDelay         time.Duration
	NumConnections     int
	MaxWaitTime        time.Duration
	ReconnectInterval  time.Duration
	IgnorePeerAddr     bool
	DisableInitialHost bool
}

func DefaultConfig() *Config {
	return &Config{
		Consistency:        gocql.Quorum,
		Timeout:            10 * time.Second,
		ConnectTimeout:     10 * time.Second,
		MaxRetries:         3,
		RetryDelay:         2 * time.Second,
		NumConnections:     2,
		MaxWaitTime:        30 * time.Second,
		ReconnectInterval:  60 * time.Second,
		IgnorePeerAddr:     true,
		DisableInitialHost: true,
	}
}

func (c *Config) Validate() error {
	if len(c.Hosts) == 0 {
		return fmt.Errorf("at least one host must be specified")
	}
	if c.Keyspace == "" {
		return fmt.Errorf("keyspace must be specified")
	}
	if c.Timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}
	if c.ConnectTimeout <= 0 {
		return fmt.Errorf("connect timeout must be positive")
	}
	if c.NumConnections <= 0 {
		return fmt.Errorf("number of connections must be positive")
	}
	return nil
}

func Connect(hosts []string, keyspace string) (*ScyllaDB, error) {
	config := DefaultConfig()
	config.Hosts = hosts
	config.Keyspace = keyspace
	return ConnectWithConfig(config)
}

type connectObserver struct{}

func (c *connectObserver) ObserveConnect(o gocql.ObservedConnect) {
	if o.Err != nil {
		log.Printf("⚠️ Connection attempt to %s failed: %v", o.Host.HostID(), o.Err)
	} else {
		log.Printf("✅ Successfully connected to %s", o.Host.HostID())
	}
}

func ConnectWithConfig(config *Config) (*ScyllaDB, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	cluster := gocql.NewCluster(config.Hosts...)
	cluster.Keyspace = config.Keyspace
	cluster.Consistency = config.Consistency
	cluster.Timeout = config.Timeout
	cluster.ConnectTimeout = config.ConnectTimeout
	cluster.NumConns = config.NumConnections
	cluster.ReconnectInterval = config.ReconnectInterval
	cluster.IgnorePeerAddr = config.IgnorePeerAddr
	cluster.DisableInitialHostLookup = config.DisableInitialHost

	// Token-aware load balancing with round-robin fallback
	cluster.PoolConfig.HostSelectionPolicy = gocql.TokenAwareHostPolicy(
		gocql.RoundRobinHostPolicy(),
	)

	// Retry policy for transient failures
	cluster.RetryPolicy = &gocql.ExponentialBackoffRetryPolicy{
		NumRetries: config.MaxRetries,
		Min:        config.RetryDelay,
		Max:        config.MaxWaitTime,
	}

	// Connection observer for monitoring
	cluster.ConnectObserver = &connectObserver{}

	var session *gocql.Session
	var err error

	// Retry connection with exponential backoff
	for attempt := 1; attempt <= config.MaxRetries; attempt++ {
		session, err = cluster.CreateSession()
		if err == nil {
			break
		}

		if attempt < config.MaxRetries {
			waitTime := config.RetryDelay * time.Duration(attempt)
			log.Printf("⚠️ Connection attempt %d/%d failed: %v. Retrying in %v...",
				attempt, config.MaxRetries, err, waitTime)
			time.Sleep(waitTime)
		}
	}

	if err != nil {
		return nil, fmt.Errorf("failed to connect to ScyllaDB after %d attempts: %w",
			config.MaxRetries, err)
	}

	gocqlxSession := gocqlx.NewSession(session)

	db := &ScyllaDB{
		Session: gocqlxSession,
		config:  config,
	}

	log.Printf("✅ ScyllaDB connection established to keyspace '%s'", config.Keyspace)

	// Perform initial health check
	if err := db.Health(); err != nil {
		db.Close()
		return nil, fmt.Errorf("initial health check failed: %w", err)
	}

	return db, nil
}

func (db *ScyllaDB) Close() {
	if db.Session.Session != nil {
		db.Session.Close()
		log.Println("✅ ScyllaDB session closed gracefully")
	}
}

func (db *ScyllaDB) Health() error {
	return db.HealthWithContext(context.Background())
}

func (db *ScyllaDB) HealthWithContext(ctx context.Context) error {
	type result struct {
		t   time.Time
		err error
	}

	resultCh := make(chan result, 1)

	go func() {
		query := db.Session.Query("SELECT now() FROM system.local", nil)
		defer query.Release()

		var t time.Time
		err := query.Get(&t)
		resultCh <- result{t: t, err: err}
	}()

	select {
	case <-ctx.Done():
		return fmt.Errorf("health check cancelled: %w", ctx.Err())
	case res := <-resultCh:
		if res.err != nil {
			return fmt.Errorf("health check failed: %w", res.err)
		}
		log.Printf("✅ Database health check passed at %v", res.t)
		return nil
	}
}

func (db *ScyllaDB) Ping() error {
	return db.Health()
}

func (db *ScyllaDB) GetConfig() *Config {
	return db.config
}
