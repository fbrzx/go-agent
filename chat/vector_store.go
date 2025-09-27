package chat

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
)

type VectorStore interface {
	SimilarChunks(ctx context.Context, embedding []float32, limit int) ([]ChunkResult, error)
}

type PostgresVectorStore struct {
	pool *pgxpool.Pool
}

func NewPostgresVectorStore(pool *pgxpool.Pool) *PostgresVectorStore {
	return &PostgresVectorStore{pool: pool}
}

func (s *PostgresVectorStore) SimilarChunks(ctx context.Context, embedding []float32, limit int) ([]ChunkResult, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("postgres pool is nil")
	}
	if len(embedding) == 0 {
		return nil, fmt.Errorf("embedding is empty")
	}
	if limit <= 0 {
		limit = 5
	}

	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	probes := limit * 10
	if probes < 10 {
		probes = 10
	}
	if _, err := conn.Exec(ctx, fmt.Sprintf("SET ivfflat.probes = %d", probes)); err != nil {
		return nil, fmt.Errorf("set ivfflat probes: %w", err)
	}

	rows, err := conn.Query(ctx, `
        SELECT
            rc.id,
            rc.document_id,
            rd.title,
            rd.source_path,
            rc.content,
            (rc.embedding <-> $1::vector) AS distance
        FROM rag_chunks rc
        JOIN rag_documents rd ON rd.id = rc.document_id
        ORDER BY rc.embedding <-> $1::vector
        LIMIT $2
    `, pgvector.NewVector(embedding), limit)
	if err != nil {
		return nil, fmt.Errorf("query similar chunks: %w", err)
	}
	defer rows.Close()

	results := make([]ChunkResult, 0)
	for rows.Next() {
		var item ChunkResult
		var distance float64
		if scanErr := rows.Scan(&item.ChunkID, &item.DocumentID, &item.Title, &item.Path, &item.Content, &distance); scanErr != nil {
			return nil, fmt.Errorf("scan similar chunk: %w", scanErr)
		}
		item.Score = 1 / (1 + distance)
		results = append(results, item)
	}

	if rows.Err() != nil {
		return nil, rows.Err()
	}

	return results, nil
}

var _ VectorStore = (*PostgresVectorStore)(nil)
