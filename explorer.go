// Package dbadminer serves a read-only database explorer over HTTP.
// The root package imports only the standard library. The cmd/dbadminer
// module opens a database from the usual environment variables.
package dbadminer

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/dbadminer/dbadminer/internal/auth"
	"github.com/dbadminer/dbadminer/internal/guard"
)

//go:embed all:ui
var uiFiles embed.FS

// Version is reported by GET /api/info.
var Version = "0.0.0-dev"

const (
	defaultAddr            = "127.0.0.1:8080"
	defaultMaxRows         = 200
	ceilingMaxRows         = 5000
	defaultPageLimit       = 50
	defaultQueryTimeout    = 15 * time.Second
	defaultMaxBodyBytes    = 1 << 20
	defaultMaxResponseSize = 8 << 20
	maxOffset              = 1_000_000
	maxColumns             = 512

	// AccessReadOnly rejects statements that change data.
	AccessReadOnly = "readonly"
	// AccessFull runs statements with the database user's privileges.
	AccessFull = "full"
)

// Config controls one explorer. Use DefaultConfig to start from the safe
// defaults. A zero Config passed to New is read-only. Set Access to
// AccessReadOnly or AccessFull to choose the mode. Setting ReadOnly to false
// on a value returned by DefaultConfig or Flags still selects full access.
type Config struct {
	// Dialect overrides automatic detection when set.
	Dialect Dialect
	// Access is AccessReadOnly or AccessFull. Empty means read-only, unless
	// ReadOnly was set false on a configured Config.
	Access string
	// ReadOnly rejects statements that change data. New keeps it aligned with Access.
	ReadOnly bool
	// Addr is the loopback listen address used by ListenAndServe.
	Addr string
	// User and Password enable HTTP basic auth when either is set.
	// They are never filled from database credentials.
	User     string
	Password string
	// MaxRows caps a page and a query result. Values above 5000 are clamped.
	MaxRows int
	// QueryTimeout bounds each statement.
	QueryTimeout time.Duration
	// MaxBodyBytes caps a query request body.
	MaxBodyBytes int64
	// MaxResponseBytes caps an encoded JSON payload.
	MaxResponseBytes int64
	// Audit, when set, is called for each statement the explorer executes.
	Audit func(ctx context.Context, query string, err error)
	// Schema, when set, hides every other schema. The command sets it from
	// --schema or DB_SCHEMA. On PostgreSQL that same value is search_path.
	Schema string

	configured bool
}

// DefaultConfig returns a read-only explorer listening on 127.0.0.1:8080.
func DefaultConfig() Config {
	return Config{
		ReadOnly:         true,
		Addr:             defaultAddr,
		MaxRows:          defaultMaxRows,
		QueryTimeout:     defaultQueryTimeout,
		MaxBodyBytes:     defaultMaxBodyBytes,
		MaxResponseBytes: defaultMaxResponseSize,
		configured:       true,
	}
}

func (c Config) applyDefaults() Config {
	if !c.configured {
		access := c.Access
		d := DefaultConfig()
		d.Access = access
		if c.Dialect != nil {
			d.Dialect = c.Dialect
		}
		if c.Addr != "" {
			d.Addr = c.Addr
		}
		if c.User != "" {
			d.User = c.User
		}
		if c.Password != "" {
			d.Password = c.Password
		}
		if c.MaxRows > 0 {
			d.MaxRows = c.MaxRows
		}
		if c.QueryTimeout > 0 {
			d.QueryTimeout = c.QueryTimeout
		}
		if c.MaxBodyBytes > 0 {
			d.MaxBodyBytes = c.MaxBodyBytes
		}
		if c.MaxResponseBytes > 0 {
			d.MaxResponseBytes = c.MaxResponseBytes
		}
		if c.Audit != nil {
			d.Audit = c.Audit
		}
		if c.Schema != "" {
			d.Schema = c.Schema
		}
		c = d
	}
	if c.Addr == "" {
		c.Addr = defaultAddr
	}
	if c.MaxRows <= 0 {
		c.MaxRows = defaultMaxRows
	}
	if c.MaxRows > ceilingMaxRows {
		c.MaxRows = ceilingMaxRows
	}
	if c.QueryTimeout <= 0 {
		c.QueryTimeout = defaultQueryTimeout
	}
	if c.MaxBodyBytes <= 0 {
		c.MaxBodyBytes = defaultMaxBodyBytes
	}
	if c.MaxResponseBytes <= 0 {
		c.MaxResponseBytes = defaultMaxResponseSize
	}
	return c
}

// Explorer serves the UI and the JSON API for one database.
type Explorer struct {
	db      *sql.DB
	cfg     Config
	dialect Dialect
	auth    *auth.Authenticator
}

// New detects the database dialect, unless cfg.Dialect is set, and returns an
// explorer. db must be non-nil. New does not take ownership of the pool.
func New(db *sql.DB, cfg Config) (*Explorer, error) {
	if db == nil {
		return nil, errors.New("dbadminer: db is nil")
	}
	cfg = cfg.applyDefaults()
	if err := cfg.normalizeAccess(); err != nil {
		return nil, err
	}
	d := cfg.Dialect
	if d == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		detected, err := detectDialect(ctx, db)
		if err != nil {
			return nil, err
		}
		d = detected
	}
	if d.Name() == "" {
		return nil, errors.New("dbadminer: dialect name is empty")
	}
	return &Explorer{
		db:      db,
		cfg:     cfg,
		dialect: d,
		auth:    auth.New(cfg.User, cfg.Password),
	}, nil
}

// Flags registers explorer settings on a new flag set. cfg must be non-nil.
// A zero Config is replaced with DefaultConfig, so access defaults to read-only.
// Pass --access=readonly or --access=full. --readonly=false is the same as
// --access=full. The set does not include a database DSN; the cmd/dbadminer
// program accepts --dsn.
func Flags(cfg *Config) *flag.FlagSet {
	if cfg == nil {
		panic("dbadminer: Flags cfg is nil")
	}
	if !cfg.configured {
		*cfg = DefaultConfig()
	}
	applied := cfg.applyDefaults()
	*cfg = applied
	accessDefault := applied.Access
	if accessDefault == "" {
		if applied.configured && !applied.ReadOnly {
			accessDefault = AccessFull
		} else {
			accessDefault = AccessReadOnly
		}
	}
	fs := flag.NewFlagSet("dbadminer", flag.ContinueOnError)
	fs.StringVar(&cfg.Addr, "addr", cfg.Addr, "listen address: loopback, 0.0.0.0, or ::")
	fs.StringVar(&cfg.Access, "access", accessDefault, `access mode: "readonly" or "full"`)
	fs.BoolVar(&cfg.ReadOnly, "readonly", accessDefault != AccessFull, "read-only access; false selects full access")
	fs.StringVar(&cfg.User, "user", cfg.User, "basic auth user for the explorer")
	fs.StringVar(&cfg.Password, "password", cfg.Password, "basic auth password for the explorer")
	fs.IntVar(&cfg.MaxRows, "max-rows", cfg.MaxRows, "maximum rows in one response")
	fs.DurationVar(&cfg.QueryTimeout, "query-timeout", cfg.QueryTimeout, "per-statement timeout")
	fs.Int64Var(&cfg.MaxBodyBytes, "max-body", cfg.MaxBodyBytes, "maximum request body in bytes")
	fs.Int64Var(&cfg.MaxResponseBytes, "max-response", cfg.MaxResponseBytes, "maximum JSON response in bytes")
	return fs
}

// ApplyAccess makes Access and ReadOnly agree after flags are parsed.
// An explicit --access or --readonly wins. Otherwise env, when non-empty,
// selects the mode. The empty default is read-only.
func ApplyAccess(cfg *Config, set *flag.FlagSet, env string) error {
	if cfg == nil {
		return errors.New("dbadminer: nil config")
	}
	accessSet := false
	readonlySet := false
	if set != nil {
		set.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "access":
				accessSet = true
			case "readonly":
				readonlySet = true
			}
		})
	}
	if accessSet && readonlySet {
		return errors.New("dbadminer: set only one of --access and --readonly")
	}
	if accessSet {
		return cfg.setAccess(cfg.Access)
	}
	if readonlySet {
		if cfg.ReadOnly {
			cfg.Access = AccessReadOnly
		} else {
			cfg.Access = AccessFull
		}
		return nil
	}
	if strings.TrimSpace(env) != "" {
		return cfg.setAccess(env)
	}
	return cfg.normalizeAccess()
}

func (c *Config) setAccess(mode string) error {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case AccessReadOnly:
		c.Access = AccessReadOnly
		c.ReadOnly = true
	case AccessFull:
		c.Access = AccessFull
		c.ReadOnly = false
	default:
		return fmt.Errorf("dbadminer: access %q must be %q or %q", mode, AccessReadOnly, AccessFull)
	}
	return nil
}

func (c *Config) normalizeAccess() error {
	if strings.TrimSpace(c.Access) == "" {
		if c.configured && !c.ReadOnly {
			c.Access = AccessFull
			return nil
		}
		c.Access = AccessReadOnly
		c.ReadOnly = true
		return nil
	}
	return c.setAccess(c.Access)
}

// Run parses args, serves the explorer, and returns when ctx is cancelled.
// args are the process arguments after the program name. db is the caller's pool.
func Run(ctx context.Context, db *sql.DB, args []string) error {
	if ctx == nil {
		return errors.New("dbadminer: nil context")
	}
	cfg := DefaultConfig()
	fs := Flags(&cfg)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := ApplyAccess(&cfg, fs, ""); err != nil {
		return err
	}
	ex, err := New(db, cfg)
	if err != nil {
		return err
	}
	return ex.ListenAndServe(ctx)
}

// ListenAndServe binds cfg.Addr and returns after ctx is cancelled and the
// server has shut down. The address must include a port and be loopback,
// 0.0.0.0, or ::. A specific public address is refused.
func (e *Explorer) ListenAndServe(ctx context.Context) error {
	return Serve(ctx, e.cfg, e.Handler())
}

// Serve binds cfg.Addr and serves handler until ctx is cancelled. The address
// rules match ListenAndServe.
func Serve(ctx context.Context, cfg Config, handler http.Handler) error {
	if ctx == nil {
		return errors.New("dbadminer: nil context")
	}
	if handler == nil {
		return errors.New("dbadminer: nil handler")
	}
	cfg = cfg.applyDefaults()
	if err := requireLoopbackAddr(cfg.Addr); err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       cfg.QueryTimeout + 10*time.Second,
		WriteTimeout:      cfg.QueryTimeout + 10*time.Second,
		IdleTimeout:       time.Minute,
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		shut, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		shutErr := srv.Shutdown(shut)
		serveErr := <-errCh
		if shutErr != nil {
			return shutErr
		}
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			return serveErr
		}
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func requireLoopbackAddr(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return fmt.Errorf("dbadminer: listen address %q needs a host and port", addr)
	}
	if guard.Loopback(host) || unspecifiedHost(host) {
		return nil
	}
	return fmt.Errorf("dbadminer: listen address %q is not loopback", addr)
}

func unspecifiedHost(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.IsUnspecified()
}
