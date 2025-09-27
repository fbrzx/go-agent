package integration_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"

	"github.com/fabfab/go-agent/chat"
	"github.com/fabfab/go-agent/config"
	"github.com/fabfab/go-agent/database"
)

func TestVectorSearchRanking(t *testing.T) {
	if os.Getenv("RUN_DB_INTEGRATION_TESTS") != "1" {
		t.Skip("set RUN_DB_INTEGRATION_TESTS=1 to run database connectivity checks")
	}

	cfg := config.Load()
	ctx := context.Background()

	pool, err := database.NewPostgresPool(ctx, cfg.PostgresDSN)
	if err != nil {
		t.Fatalf("postgres connection: %v", err)
	}
	defer pool.Close()

	dim := cfg.Embeddings.Dimension
	if dim <= 0 {
		t.Fatalf("invalid embedding dimension: %d", dim)
	}

	if err := database.EnsureRAGSchema(ctx, pool, dim); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	docA := uuid.New()
	docB := uuid.New()
	chunkA := uuid.New()
	chunkB := uuid.New()

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM rag_documents WHERE id = ANY($1)", []uuid.UUID{docA, docB})
	})

	if _, err := pool.Exec(ctx, `
        INSERT INTO rag_documents (id, source_path, title, sha256, created_at, updated_at)
        VALUES ($1, $2, $3, $4, NOW(), NOW()),
               ($5, $6, $7, $8, NOW(), NOW())
    `, docA, "test/docA.md", "Doc A", "hash-a", docB, "test/docB.md", "Doc B", "hash-b"); err != nil {
		t.Fatalf("insert documents: %v", err)
	}

	makeVector := func(weight float32) []float32 {
		vec := make([]float32, dim)
		vec[0] = weight
		return vec
	}

	if _, err := pool.Exec(ctx, `
	        INSERT INTO rag_chunks (id, document_id, chunk_index, section_order, section_level, section_title, content, embedding, created_at, updated_at)
	        VALUES ($1, $2, 0, 1, 2, $3, $4, $5, NOW(), NOW()),
	               ($6, $7, 0, 2, 2, $8, $9, $10, NOW(), NOW())
	`,
		chunkA, docA, "Section A", "Chunk A", pgvector.NewVector(makeVector(1.0)),
		chunkB, docB, "Section B", "Chunk B", pgvector.NewVector(makeVector(0.4)),
	); err != nil {
		t.Fatalf("insert chunks: %v", err)
	}

	store := chat.NewPostgresVectorStore(pool)

	results, err := store.SimilarChunks(ctx, makeVector(0.9), 2)
	if err != nil {
		t.Fatalf("vector search: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results[0].ChunkID != chunkA.String() {
		t.Fatalf("expected first result chunk %s, got %s", chunkA, results[0].ChunkID)
	}

	if results[0].Score <= results[1].Score {
		t.Fatalf("expected first score to be higher, got %f <= %f", results[0].Score, results[1].Score)
	}
}
