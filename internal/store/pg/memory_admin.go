package pg

import (
	"context"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ListAllDocumentsGlobal returns all documents across all agents (for admin overview).
func (s *PGMemoryStore) ListAllDocumentsGlobal(ctx context.Context) ([]store.DocumentInfo, error) {
	var rows []documentInfoRow
	if err := pkgSqlxDB.SelectContext(ctx, &rows,
		`SELECT agent_id, path, hash, user_id, updated_at
		 FROM memory_documents
		 ORDER BY updated_at DESC`); err != nil {
		return nil, err
	}
	result := make([]store.DocumentInfo, len(rows))
	for i := range rows {
		result[i] = rows[i].toDocumentInfo()
	}
	return result, nil
}

// ListAllDocuments returns all documents for an agent across all users (global + personal).
func (s *PGMemoryStore) ListAllDocuments(ctx context.Context, agentID string) ([]store.DocumentInfo, error) {
	aid, err := parseUUID(agentID)
	if err != nil {
		return nil, fmt.Errorf("memory list all documents: %w", err)
	}

	var rows []documentInfoRow
	if err := pkgSqlxDB.SelectContext(ctx, &rows,
		`SELECT agent_id, path, hash, user_id, updated_at
		 FROM memory_documents WHERE agent_id = $1
		 ORDER BY updated_at DESC`, aid); err != nil {
		return nil, err
	}
	result := make([]store.DocumentInfo, len(rows))
	for i := range rows {
		result[i] = rows[i].toDocumentInfo()
	}
	return result, nil
}

// GetDocumentDetail returns full document info with chunk and embedding counts.
func (s *PGMemoryStore) GetDocumentDetail(ctx context.Context, agentID, userID, path string) (*store.DocumentDetail, error) {
	aid, err := parseUUID(agentID)
	if err != nil {
		return nil, fmt.Errorf("memory get document detail: %w", err)
	}

	var q string
	var args []any
	if userID == "" {
		q = `SELECT d.path, d.content, d.hash, d.user_id, d.created_at, d.updated_at,
				COUNT(c.id) AS chunk_count,
				COUNT(c.embedding) AS embedded_count
			 FROM memory_documents d
			 LEFT JOIN memory_chunks c ON c.document_id = d.id
			 WHERE d.agent_id = $1 AND d.path = $2 AND d.user_id IS NULL
			 GROUP BY d.id`
		args = []any{aid, path}
	} else {
		q = `SELECT d.path, d.content, d.hash, d.user_id, d.created_at, d.updated_at,
				COUNT(c.id) AS chunk_count,
				COUNT(c.embedding) AS embedded_count
			 FROM memory_documents d
			 LEFT JOIN memory_chunks c ON c.document_id = d.id
			 WHERE d.agent_id = $1 AND d.path = $2 AND d.user_id = $3
			 GROUP BY d.id`
		args = []any{aid, path, userID}
	}

	var row documentDetailRow
	if err := pkgSqlxDB.GetContext(ctx, &row, q, args...); err != nil {
		return nil, err
	}
	detail := row.toDocumentDetail()
	return &detail, nil
}

// ListChunks returns chunks for a document identified by agent, user, and path.
func (s *PGMemoryStore) ListChunks(ctx context.Context, agentID, userID, path string) ([]store.ChunkInfo, error) {
	aid, err := parseUUID(agentID)
	if err != nil {
		return nil, fmt.Errorf("memory list chunks: %w", err)
	}

	var q string
	var args []any
	if userID == "" {
		q = `SELECT c.id, c.start_line, c.end_line,
				c.text AS text_preview,
				(c.embedding IS NOT NULL) AS has_embedding
			 FROM memory_chunks c
			 JOIN memory_documents d ON c.document_id = d.id
			 WHERE d.agent_id = $1 AND d.path = $2 AND d.user_id IS NULL
			 ORDER BY c.start_line`
		args = []any{aid, path}
	} else {
		q = `SELECT c.id, c.start_line, c.end_line,
				c.text AS text_preview,
				(c.embedding IS NOT NULL) AS has_embedding
			 FROM memory_chunks c
			 JOIN memory_documents d ON c.document_id = d.id
			 WHERE d.agent_id = $1 AND d.path = $2 AND d.user_id = $3
			 ORDER BY c.start_line`
		args = []any{aid, path, userID}
	}

	var rows []chunkInfoRow
	if err := pkgSqlxDB.SelectContext(ctx, &rows, q, args...); err != nil {
		return nil, err
	}
	result := make([]store.ChunkInfo, len(rows))
	for i := range rows {
		result[i] = rows[i].toChunkInfo()
	}
	return result, nil
}
