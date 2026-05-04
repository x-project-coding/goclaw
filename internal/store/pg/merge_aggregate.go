package pg

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// MergeUserAggregate atomically merges SourceUserIDs' data into TargetUserID
// across channel_contacts, agent_sessions, user_context_files, and memory_documents.
// All UPDATEs share one *sql.Tx — atomic merge guarantee.
//
// Pre-checks inside the TX (concurrency-safe via SELECT FOR UPDATE):
//   - source contacts must have merged_id IS NULL (no user→user merge)
//   - target user must exist (FK guard)
//   - target user must not have been merged elsewhere (depth cap = 1)
//
// On success, the in-memory contact-resolve cache is invalidated.
func (s *PGContactStore) MergeUserAggregate(ctx context.Context, req store.MergeUserAggregateRequest) error {
	if len(req.ContactIDs) == 0 {
		return fmt.Errorf("merge: contact_ids required")
	}
	if req.TargetUserID == uuid.Nil {
		return fmt.Errorf("merge: target_user_id required")
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("merge: begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := pgMergeAssertTargetExists(ctx, tx, req.TargetUserID); err != nil {
		return err
	}
	if err := pgMergeAssertSourceUnmerged(ctx, tx, req.ContactIDs); err != nil {
		return err
	}
	if err := pgMergeAssertTargetUnmerged(ctx, tx, req.TargetUserID); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE channel_contacts
		    SET merged_id = $1, merge_audit = COALESCE($2::jsonb, '{}'::jsonb)
		  WHERE id = ANY($3)`,
		req.TargetUserID, jsonbOrNil(req.MergeAudit), pq.Array(req.ContactIDs),
	); err != nil {
		return fmt.Errorf("merge: update channel_contacts: %w", err)
	}

	if len(req.SourceUserIDs) > 0 {
		sourceArr := pq.Array(req.SourceUserIDs)
		if _, err := tx.ExecContext(ctx,
			`UPDATE agent_sessions SET user_id = $1 WHERE user_id = ANY($2)`,
			req.TargetUserID, sourceArr,
		); err != nil {
			return fmt.Errorf("merge: update agent_sessions: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE user_context_files SET user_id = $1 WHERE user_id = ANY($2)`,
			req.TargetUserID, sourceArr,
		); err != nil {
			return fmt.Errorf("merge: update user_context_files: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE memory_documents SET user_id = $1 WHERE user_id = ANY($2)`,
			req.TargetUserID, sourceArr,
		); err != nil {
			return fmt.Errorf("merge: update memory_documents: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("merge: commit: %w", err)
	}
	committed = true
	s.InvalidateContactResolveCache()
	return nil
}

// pgMergeAssertTargetExists verifies the target user row is present and not
// soft-deleted. Without the deleted_at filter an admin could resurrect a tombstoned
// account by merging contacts into it.
func pgMergeAssertTargetExists(ctx context.Context, tx *sql.Tx, target uuid.UUID) error {
	var ok bool
	err := tx.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM users WHERE id = $1 AND deleted_at IS NULL)`, target,
	).Scan(&ok)
	if err != nil {
		return fmt.Errorf("merge: check target user: %w", err)
	}
	if !ok {
		return store.ErrMergeTargetUserNotFound
	}
	return nil
}

// pgMergeAssertSourceUnmerged ensures every source contact has merged_id IS NULL.
// SELECT FOR UPDATE locks all addressed rows unconditionally so concurrent
// merge attempts queue behind the first winner — without that, two TXes both
// see merged_id IS NULL and both succeed (last-write-wins), violating the
// atomic-merge contract.
func pgMergeAssertSourceUnmerged(ctx context.Context, tx *sql.Tx, contactIDs []uuid.UUID) error {
	rows, err := tx.QueryContext(ctx,
		`SELECT merged_id FROM channel_contacts
		  WHERE id = ANY($1)
		  FOR UPDATE`,
		pq.Array(contactIDs),
	)
	if err != nil {
		return fmt.Errorf("merge: check source contacts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var merged sql.NullString
		if scanErr := rows.Scan(&merged); scanErr != nil {
			return fmt.Errorf("merge: scan source merged_id: %w", scanErr)
		}
		if merged.Valid && merged.String != "" {
			return store.ErrMergeSourceAlreadyMerged
		}
	}
	return rows.Err()
}

// pgMergeAssertTargetUnmerged blocks chained merges (target user must not have
// any contacts whose merged_id points to a different user).
//
// Locks all target-owned contacts FOR UPDATE so a concurrent merge cannot stage
// a chained-merge precondition between this check and our COMMIT (READ COMMITTED
// alone leaves the EXISTS check vulnerable to phantom rows committed after the
// SELECT but before our UPDATE).
func pgMergeAssertTargetUnmerged(ctx context.Context, tx *sql.Tx, target uuid.UUID) error {
	rows, err := tx.QueryContext(ctx,
		`SELECT merged_id FROM channel_contacts
		  WHERE user_id = $1
		  FOR UPDATE`,
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

// jsonbOrNil returns the input unchanged or "{}" when nil/empty so the
// COALESCE in the UPDATE always casts a real JSONB value.
func jsonbOrNil(b []byte) []byte {
	if len(b) == 0 {
		return []byte(`{}`)
	}
	return b
}
