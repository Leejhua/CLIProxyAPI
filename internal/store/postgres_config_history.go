package store

import (
	"context"
	"fmt"
)

// ensureConfigHistory installs a database-side archive so configuration writes
// from management, startup, and older application versions all retain the old
// content. Archiving and mutation share a transaction: backup failures abort the
// write. History contains credentials and requires the same protection as config.
func (s *PostgresStore) ensureConfigHistory(ctx context.Context) error {
	history := s.fullTableName(s.cfg.ConfigTable + "_history")
	function := s.fullTableName(s.cfg.ConfigTable + "_archive")
	table := s.fullTableName(s.cfg.ConfigTable)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("postgres store: begin config history schema: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	queries := []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			revision BIGSERIAL PRIMARY KEY,
			config_id TEXT NOT NULL,
			content TEXT NOT NULL,
			source_created_at TIMESTAMPTZ NOT NULL,
			source_updated_at TIMESTAMPTZ NOT NULL,
			archived_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
			operation TEXT NOT NULL
		)`, history),
		fmt.Sprintf(`CREATE OR REPLACE FUNCTION %s() RETURNS trigger
		LANGUAGE plpgsql AS $archive$
		BEGIN
			IF TG_OP = 'DELETE' OR OLD.content IS DISTINCT FROM NEW.content THEN
				INSERT INTO %s (config_id, content, source_created_at, source_updated_at, operation)
				VALUES (OLD.id, OLD.content, OLD.created_at, OLD.updated_at, TG_OP);
			END IF;
			IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
			RETURN NEW;
		END;
		$archive$`, function, history),
		// Drop and create in one transaction supports PostgreSQL before v14 too.
		fmt.Sprintf(`DROP TRIGGER IF EXISTS cpa_config_archive ON %s`, table),
		fmt.Sprintf(`CREATE TRIGGER cpa_config_archive BEFORE UPDATE OR DELETE ON %s
		FOR EACH ROW EXECUTE FUNCTION %s()`, table, function),
	}
	for _, query := range queries {
		if _, err = tx.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("postgres store: install config history: %w", err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("postgres store: commit config history schema: %w", err)
	}
	return nil
}
