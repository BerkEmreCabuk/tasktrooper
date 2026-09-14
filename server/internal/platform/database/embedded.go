package database

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	embedded "github.com/fergusstrange/embedded-postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	dbUser     = "local_llm"
	dbPassword = "local_llm_desktop"
	dbName     = "local_llm"
)

type Embedded struct {
	postgres *embedded.EmbeddedPostgres
	port     uint32
	dataDir  string
	owned    bool
}

type EmbeddedConfig struct {
	DataDir string
	Port    uint32
	// RuntimePath isolates the extracted binaries. Two instances sharing the
	// default cache race each other while extracting/initialising, which shows
	// up as a killed initdb; tests that may run alongside another embedded
	// instance set their own path.
	RuntimePath string
}

func StartEmbedded(ctx context.Context, cfg EmbeddedConfig) (*Embedded, error) {
	if err := reconcileDataDir(cfg.DataDir); err != nil {
		return nil, err
	}

	if port, ok := existingPort(ctx, cfg.DataDir); ok {
		e := &Embedded{
			port:    port,
			dataDir: cfg.DataDir,
			owned:   false,
		}
		pool, err := pgxpool.New(ctx, e.DSN())
		if err != nil {
			return nil, fmt.Errorf("connect existing embedded postgres: %w", err)
		}
		defer pool.Close()
		if err := RunMigrations(ctx, pool); err != nil {
			return nil, fmt.Errorf("run migrations on existing postgres: %w", err)
		}
		return e, nil
	}

	port := cfg.Port
	if port == 0 {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, fmt.Errorf("allocate postgres port: %w", err)
		}
		port = uint32(listener.Addr().(*net.TCPAddr).Port)
		_ = listener.Close()
	}

	embeddedCfg := embedded.DefaultConfig().
		Username(dbUser).
		Password(dbPassword).
		Database(dbName).
		Version(embedded.V16).
		Port(port).
		DataPath(cfg.DataDir).
		Logger(nil)
	if cfg.RuntimePath != "" {
		embeddedCfg = embeddedCfg.RuntimePath(cfg.RuntimePath)
	}
	pg := embedded.NewDatabase(embeddedCfg)

	if err := pg.Start(); err != nil {
		return nil, fmt.Errorf("start embedded postgres: %w", err)
	}

	e := &Embedded{
		postgres: pg,
		port:     port,
		dataDir:  cfg.DataDir,
		owned:    true,
	}

	pool, err := pgxpool.New(ctx, e.DSN())
	if err != nil {
		_ = pg.Stop()
		return nil, fmt.Errorf("connect embedded postgres: %w", err)
	}
	defer pool.Close()

	if err := RunMigrations(ctx, pool); err != nil {
		_ = pg.Stop()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return e, nil
}

func reconcileDataDir(dataDir string) error {
	pidFile := filepath.Join(dataDir, "postmaster.pid")
	data, err := os.ReadFile(pidFile)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read postmaster.pid: %w", err)
	}

	pid, port, ok := parsePostmasterPID(string(data))
	if !ok {
		_ = os.Remove(pidFile)
		return nil
	}

	if processAlive(pid) && portReachable(port) {
		return nil
	}

	_ = os.Remove(pidFile)
	return nil
}

func existingPort(ctx context.Context, dataDir string) (uint32, bool) {
	pidFile := filepath.Join(dataDir, "postmaster.pid")
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return 0, false
	}

	pid, port, ok := parsePostmasterPID(string(data))
	if !ok || !processAlive(pid) || !portReachable(port) {
		return 0, false
	}

	dsn := fmt.Sprintf(
		"postgres://%s:%s@127.0.0.1:%d/%s?sslmode=disable",
		dbUser, dbPassword, port, dbName,
	)
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return 0, false
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return 0, false
	}

	if err := RunMigrations(ctx, pool); err != nil {
		return 0, false
	}

	return port, true
}

func parsePostmasterPID(content string) (pid int, port uint32, ok bool) {
	lines := strings.Split(strings.TrimSpace(content), "\n")
	if len(lines) < 4 {
		return 0, 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil {
		return 0, 0, false
	}
	portNum, err := strconv.ParseUint(strings.TrimSpace(lines[3]), 10, 32)
	if err != nil {
		return 0, 0, false
	}
	return pid, uint32(portNum), true
}

func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

func portReachable(port uint32) bool {
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func (e *Embedded) DSN() string {
	return fmt.Sprintf(
		"postgres://%s:%s@127.0.0.1:%d/%s?sslmode=disable",
		dbUser,
		dbPassword,
		e.port,
		dbName,
	)
}

func (e *Embedded) Port() uint32 {
	return e.port
}

func (e *Embedded) Stop() error {
	if e.postgres == nil || !e.owned {
		return nil
	}
	return e.postgres.Stop()
}

func PortString(port uint32) string {
	return strconv.FormatUint(uint64(port), 10)
}
