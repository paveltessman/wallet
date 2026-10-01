package main

import (
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"strconv"
	"time"
)

type config struct {
	HTTPPort         int
	PostgresHost     string
	PostgresPort     int
	PostgresUser     string
	PostgresPassword string
	PostgresDB       string
	DBMaxConns       int32
	DBAcquireTimeout time.Duration
}

func loadConfig(getenv func(string) string) (config, error) {
	r := envReader{getenv: getenv}

	cfg := config{
		HTTPPort:         r.port("HTTP_PORT"),
		PostgresHost:     r.str("POSTGRES_HOST"),
		PostgresPort:     r.port("POSTGRES_PORT"),
		PostgresUser:     r.str("POSTGRES_USER"),
		PostgresPassword: r.str("POSTGRES_PASSWORD"),
		PostgresDB:       r.str("POSTGRES_DB"),
		DBMaxConns:       r.positiveInt32("DB_MAX_CONNS"),
		DBAcquireTimeout: r.positiveDuration("DB_ACQUIRE_TIMEOUT"),
	}

	if err := errors.Join(r.errs...); err != nil {
		return config{}, fmt.Errorf("read the config: %w", err)
	}
	return cfg, nil
}

func (c config) databaseURL() string {
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(c.PostgresUser, c.PostgresPassword),
		Host:   net.JoinHostPort(c.PostgresHost, strconv.Itoa(c.PostgresPort)),
		Path:   "/" + c.PostgresDB,
		// The database is on the Compose network or on localhost.
		RawQuery: "sslmode=disable",
	}
	return u.String()
}

// envReader collects the errors, so that one start reports all the bad variables.
type envReader struct {
	getenv func(string) string
	errs   []error
}

func (r *envReader) str(name string) string {
	v := r.getenv(name)
	if v == "" {
		r.errs = append(r.errs, fmt.Errorf("%s is not set", name))
	}
	return v
}

func (r *envReader) port(name string) int {
	v := r.str(name)
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > math.MaxUint16 {
		r.errs = append(r.errs, fmt.Errorf("%s must be a port from 1 to %d, got %q", name, math.MaxUint16, v))
		return 0
	}
	return n
}

func (r *envReader) positiveInt32(name string) int32 {
	v := r.str(name)
	if v == "" {
		return 0
	}
	n, err := strconv.ParseInt(v, 10, 32)
	if err != nil || n < 1 {
		r.errs = append(r.errs, fmt.Errorf("%s must be an integer from 1 to %d, got %q", name, math.MaxInt32, v))
		return 0
	}
	return int32(n)
}

func (r *envReader) positiveDuration(name string) time.Duration {
	v := r.str(name)
	if v == "" {
		return 0
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		r.errs = append(r.errs, fmt.Errorf("%s must be a positive duration such as 2s, got %q", name, v))
		return 0
	}
	return d
}
