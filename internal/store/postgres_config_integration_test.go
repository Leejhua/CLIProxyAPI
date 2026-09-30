package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// These tests require an explicitly supplied disposable PostgreSQL database.
func newConfigTestStore(t *testing.T) *PostgresStore {
	t.Helper()
	dsn := os.Getenv("CLIPROXY_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("set CLIPROXY_TEST_PG_DSN to run PostgreSQL integration tests")
	}
	s, err := NewPostgresStore(context.Background(), PostgresStoreConfig{
		DSN: dsn, Schema: fmt.Sprintf("config_test_%d", time.Now().UnixNano()), SpoolDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := s.db.ExecContext(context.Background(), "DROP SCHEMA "+quoteIdentifier(s.cfg.Schema)+" CASCADE"); err != nil {
			t.Error(err)
		}
		_ = s.Close()
	})
	if err = s.EnsureSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPostgresConfigHistory(t *testing.T) {
	s := newConfigTestStore(t)
	ctx := context.Background()
	const original = "port: 8318\nopenai-compatibility:\n  - name: ollama\n    base-url: https://example.invalid/v1\n"
	if err := s.persistConfig(ctx, []byte(original)); err != nil {
		t.Fatal(err)
	}
	// Idempotent schema installation must not erase history or duplicate triggers.
	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{original, "replacement", "replacement"} {
		if err := s.persistConfig(ctx, []byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	history := s.fullTableName(s.cfg.ConfigTable + "_history")
	var count int
	var old string
	if err := s.db.QueryRowContext(ctx, "SELECT count(*), min(content) FROM "+history).Scan(&count, &old); err != nil {
		t.Fatal(err)
	}
	if count != 1 || old != original {
		t.Fatal("history must contain exactly the original configuration")
	}
	// Direct SQL simulates a rollback to an older binary unaware of history.
	if _, err := s.db.ExecContext(ctx, "DELETE FROM "+s.fullTableName(s.cfg.ConfigTable)); err != nil {
		t.Fatal(err)
	}
	var operation string
	if err := s.db.QueryRowContext(ctx, "SELECT content, operation FROM "+history+" ORDER BY revision DESC LIMIT 1").Scan(&old, &operation); err != nil {
		t.Fatal(err)
	}
	if old != "replacement" || operation != "DELETE" {
		t.Fatal("delete did not archive its old content")
	}
	if err := s.persistConfig(ctx, []byte(original)); err != nil {
		t.Fatal(err)
	}
	// Force archive failure; both UPDATE and DELETE must leave live data intact.
	if _, err := s.db.ExecContext(ctx, "ALTER TABLE "+history+" ADD CONSTRAINT reject_archive CHECK (false) NOT VALID"); err != nil {
		t.Fatal(err)
	}
	if err := s.persistConfig(ctx, []byte("lost")); err == nil {
		t.Fatal("write succeeded without a backup")
	}
	if err := s.deleteConfigRecord(ctx); err == nil {
		t.Fatal("delete succeeded without a backup")
	}
	if err := s.db.QueryRowContext(ctx, "SELECT content FROM "+s.fullTableName(s.cfg.ConfigTable)).Scan(&old); err != nil || old != original {
		t.Fatal("failed backup changed the authoritative configuration")
	}
}

func TestPostgresBootstrapPreservesDatabase(t *testing.T) {
	s := newConfigTestStore(t)
	ctx := context.Background()
	const original = "complete configuration with upstream credentials"
	if err := s.persistConfig(ctx, []byte(original)); err != nil {
		t.Fatal(err)
	}
	if err := s.persistAuth(ctx, "oauth.json", []byte(`{"type":"codex","refresh_token":"dummy-test-token"}`)); err != nil {
		t.Fatal(err)
	}
	// Neither a stale local mirror nor an invalid/missing seed may replace DB data.
	if err := os.WriteFile(s.ConfigPath(), []byte("stale minimal config"), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := s.BootstrapWithConfigTransform(ctx, "missing-seed.yaml", func(data []byte) ([]byte, error) {
			if string(data) != original {
				t.Fatal("transform did not receive database content")
			}
			return data, nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(s.ConfigPath())
	if err != nil || string(data) != original {
		t.Fatal("local mirror did not retain stored content")
	}
	if _, err = os.Stat(filepath.Join(s.AuthDir(), "oauth.json")); err != nil {
		t.Fatal("OAuth credential was not mirrored")
	}
	if err = s.BootstrapWithConfigTransform(ctx, "", func([]byte) ([]byte, error) {
		return nil, errors.New("invalid override")
	}); err == nil {
		t.Fatal("invalid override accepted")
	}
	var current string
	if err = s.db.QueryRowContext(ctx, "SELECT content FROM "+s.fullTableName(s.cfg.ConfigTable)).Scan(&current); err != nil || current != original {
		t.Fatal("failed transform changed stored content")
	}
	var count int
	if err = s.db.QueryRowContext(ctx, "SELECT count(*) FROM "+s.fullTableName(s.cfg.ConfigTable+"_history")).Scan(&count); err != nil || count != 0 {
		t.Fatal("unchanged restarts must not generate history")
	}
}

func TestPostgresBootstrapInvalidSeedRollsBack(t *testing.T) {
	s := newConfigTestStore(t)
	seed := filepath.Join(t.TempDir(), "seed.yaml")
	if err := os.WriteFile(seed, []byte("invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := s.BootstrapWithConfigTransform(context.Background(), seed, func([]byte) ([]byte, error) {
		return nil, errors.New("invalid seed")
	})
	if err == nil {
		t.Fatal("invalid seed accepted")
	}
	var count int
	if err = s.db.QueryRow("SELECT count(*) FROM " + s.fullTableName(s.cfg.ConfigTable)).Scan(&count); err != nil || count != 0 {
		t.Fatal("invalid seed was committed")
	}
	if err = os.WriteFile(seed, []byte("corrected"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = s.Bootstrap(context.Background(), seed); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(s.ConfigPath())
	if err != nil || string(data) != "corrected" {
		t.Fatal("corrected seed was not used")
	}
}

func TestPostgresConcurrentInitialization(t *testing.T) {
	s := newConfigTestStore(t)
	seed := filepath.Join(t.TempDir(), "seed.yaml")
	if err := os.WriteFile(seed, []byte("seed"), 0o600); err != nil {
		t.Fatal(err)
	}
	const workers = 4
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		cfg := s.cfg
		cfg.SpoolDir = t.TempDir()
		peer, err := NewPostgresStore(context.Background(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer peer.Close()
			<-start
			// The row lock serializes all four read/modify/write operations.
			if err := peer.syncConfigFromDatabase(context.Background(), seed, func(data []byte) ([]byte, error) {
				return append(data, '+'), nil
			}); err != nil {
				t.Error(err)
			}
		}()
	}
	close(start)
	wg.Wait()
	var current string
	if err := s.db.QueryRow("SELECT content FROM " + s.fullTableName(s.cfg.ConfigTable)).Scan(&current); err != nil || current != "seed++++" {
		t.Fatal("concurrent initialization overwrote a committed configuration")
	}
}
