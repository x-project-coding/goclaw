//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// MergeUserAggregate atomically migrates SourceUserIDs' data into TargetUserID
// across channel_contacts, agent_sessions, user_context_files, memory_documents.
// All UPDATEs share one *sql.Tx for atomicity. SQLite has no array type; lists
// expand to placeholder strings at query build time.
func (s *SQLiteContactStore) MergeUserAggregate(ctx context.Context, req store.MergeUserAggregateRequest) error {
	if len(req.ContactIDs) == 0 {
		return fmt.Errorf("merge: contact_ids required")
	}
	if req.TargetUserID == uuid.Nil {
		return fmt.Errorf("merge: target_user_id required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("merge: begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := sqliteMergeAssertTargetExists(ctx, tx, req.TargetUserID); err != nil {
		return err
	}
	if err := sqliteMergeAssertSourceUnmerged(ctx, tx, req.ContactIDs); err != nil {
		return err
	}
	if err := sqliteMergeAssertTargetUnmerged(ctx, tx, req.TargetUserID); err != nil {
		return err
	}

	auditBlob := req.MergeAudit
	if len(auditBlob) == 0 {
		auditBlob = []byte(`{}`)
	}

	contactQ, contactArgs := buildInListQuery(
		"UPDATE channel_contacts SET merged_id = ?, merge_audit = ? WHERE id IN (",
		req.ContactIDs,
		req.TargetUserID, string(auditBlob),
	)
	if _, err := tx.ExecContext(ctx, contactQ, contactArgs...); err != nil {
		return fmt.Errorf("merge: update channel_contacts: %w", err)
	}

	if len(req.SourceUserIDs) > 0 {
		for _, table := range []string{"agent_sessions", "user_context_files", "memory_documents"} {
			q, args := buildInListQuery(
				"UPDATE "+table+" SET user_id = ? WHERE user_id IN (",
				req.SourceUserIDs,
				req.TargetUserID,
			)
			if _, err := tx.ExecContext(ctx, q, args...); err != nil {
				return fmt.Errorf("merge: update %s: %w", table, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("merge: commit: %w", err)
	}
	committed = true
	return nil
}

func sqliteMergeAssertTargetExists(ctx context.Context, tx *sql.Tx, target uuid.UUID) error {
	var ok int
	err := tx.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM users WHERE id = ? AND deleted_at IS NULL)`, target,
	).Scan(&ok)
	if err != nil {
		return fmt.Errorf("merge: check target: %w", err)
	}
	if ok == 0 {
		return store.ErrMergeTargetUserNotFound
	}
	return nil
}

func sqliteMergeAssertSourceUnmerged(ctx context.Context, tx *sql.Tx, contactIDs []uuid.UUID) error {
	q, args := buildInListQuery(
		"SELECT 1 FROM channel_contacts WHERE merged_id IS NOT NULL AND id IN (",
		contactIDs,
	)
	q += " LIMIT 1"
	row := tx.QueryRowContext(ctx, q, args...)
	var x int
	err := row.Scan(&x)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("merge: check source: %w", err)
	}
	return store.ErrMergeSourceAlreadyMerged
}

// sqliteMergeAssertTargetUnmerged blocks chained merges. SQLite serializes
// writes via its journal lock (BEGIN takes implicit write intent), so we don't
// need FOR UPDATE — any concurrent merge waits on the journal.
func sqliteMergeAssertTargetUnmerged(ctx context.Context, tx *sql.Tx, target uuid.UUID) error {
	rows, err := tx.QueryContext(ctx,
		`SELECT merged_id FROM channel_contacts WHERE user_id = ?`,
		target,
	)
	if err != nil {
		return fmt.Errorf("merge: check target chain: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var merged sql.NullString
		if scanErr := rows.Scan(&merged); scanErr != nil {
			return fmt.Errorf("merge: scan target chain: %w", scanErr)
		}
		if merged.Valid && merged.String != "" && merged.String != target.String() {
			return store.ErrMergeTargetAlreadyMerged
		}
	}
	return rows.Err()
}

// buildInListQuery expands an UUID slice into ?,?,? placeholders and prepends
// any leading positional args before the IN list. Final query gets a closing ")".
func buildInListQuery(prefix string, ids []uuid.UUID, leadArgs ...any) (string, []any) {
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(leadArgs)+len(ids))
	args = append(args, leadArgs...)
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	return prefix + strings.Join(placeholders, ",") + ")", args
}
