package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

func EnsureRAGSchema(ctx context.Context, pool *pgxpool.Pool, dimension int) error {
	if dimension <= 0 {
		return fmt.Errorf("embedding dimension must be positive")
	}

	stmts := []string{
		"CREATE EXTENSION IF NOT EXISTS vector",
		`CREATE TABLE IF NOT EXISTS rag_documents (
			id UUID PRIMARY KEY,
			source_path TEXT UNIQUE NOT NULL,
			title TEXT,
			sha256 TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS rag_chunks (
			id UUID PRIMARY KEY,
			document_id UUID NOT NULL REFERENCES rag_documents(id) ON DELETE CASCADE,
			chunk_index INT NOT NULL,
			section_order INT,
			section_level INT,
			section_title TEXT,
			content TEXT NOT NULL,
			embedding VECTOR(%d) NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(document_id, chunk_index)
		)`, dimension),
		"ALTER TABLE rag_chunks ADD COLUMN IF NOT EXISTS section_order INT",
		"ALTER TABLE rag_chunks ADD COLUMN IF NOT EXISTS section_level INT",
		"ALTER TABLE rag_chunks ADD COLUMN IF NOT EXISTS section_title TEXT",
		"CREATE INDEX IF NOT EXISTS idx_rag_chunks_document ON rag_chunks(document_id)",
		"CREATE INDEX IF NOT EXISTS idx_rag_chunks_embedding ON rag_chunks USING ivfflat (embedding vector_l2_ops)",
		"CREATE INDEX IF NOT EXISTS idx_rag_chunks_section ON rag_chunks(document_id, section_order)",
	}

	for _, stmt := range stmts {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("execute schema statement: %w", err)
		}
	}

	return nil
}
