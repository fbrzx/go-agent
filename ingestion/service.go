package ingestion

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	stdpath "path"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/pgvector/pgvector-go"

	"github.com/fabfab/go-agent/database"
	"github.com/fabfab/go-agent/embeddings"
	"github.com/fabfab/go-agent/knowledge"
)

const (
	defaultChunkSize    = 1000
	defaultChunkOverlap = 200
)

type Service struct {
	pool      *pgxpool.Pool
	driver    neo4j.DriverWithContext
	embedder  embeddings.Embedder
	logger    *log.Logger
	dimension int
}

func NewService(pool *pgxpool.Pool, driver neo4j.DriverWithContext, embedder embeddings.Embedder, logger *log.Logger, dimension int) *Service {
	if logger == nil {
		logger = log.Default()
	}

	return &Service{
		pool:      pool,
		driver:    driver,
		embedder:  embedder,
		logger:    logger,
		dimension: dimension,
	}
}

func (s *Service) IngestDirectory(ctx context.Context, dir string) error {
	if s.embedder == nil {
		return fmt.Errorf("embedder not configured")
	}
	if err := database.EnsureRAGSchema(ctx, s.pool, s.dimension); err != nil {
		return fmt.Errorf("ensure schema: %w", err)
	}

	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("data directory: %w", err)
	}

	entries := make([]string, 0)
	if err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			entries = append(entries, path)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("walk data directory: %w", err)
	}

	if len(entries) == 0 {
		s.logger.Printf("no markdown files found in %s", dir)
		return nil
	}

	for _, path := range entries {
		if err := s.ingestFile(ctx, dir, path); err != nil {
			s.logger.Printf("ingest failed for %s: %v", path, err)
		}
	}

	return nil
}

func (s *Service) ingestFile(ctx context.Context, root, path string) (err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	relPath, relErr := filepath.Rel(root, path)
	if relErr != nil {
		relPath = path
	}
	relPath = filepath.ToSlash(relPath)
	folder := stdpath.Dir(relPath)
	if folder == "." || folder == "/" {
		folder = ""
	}

	content := string(data)
	title := ExtractTitle(content, filepath.Base(path))
	hash := sha256.Sum256(data)
	hashHex := hex.EncodeToString(hash[:])

	chunks := ChunkMarkdown(content, defaultChunkSize, defaultChunkOverlap)
	if len(chunks) == 0 {
		s.logger.Printf("skip empty document %s", path)
		return nil
	}

	embeddings, err := s.embedder.Embed(ctx, chunks)
	if err != nil {
		return fmt.Errorf("generate embeddings: %w", err)
	}

	if len(embeddings) != len(chunks) {
		return fmt.Errorf("embedding count mismatch: have %d chunks, %d embeddings", len(chunks), len(embeddings))
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			if rbErr := tx.Rollback(ctx); rbErr != nil {
				s.logger.Printf("rollback error: %v", rbErr)
			}
		}
	}()

	docID, changed, err := upsertDocument(ctx, tx, relPath, title, hashHex)
	if err != nil {
		return err
	}

	chunkNodes := make([]knowledge.Chunk, 0, len(chunks))

	if changed {
		if _, err = tx.Exec(ctx, "DELETE FROM rag_chunks WHERE document_id = $1", docID); err != nil {
			return fmt.Errorf("clear existing chunks: %w", err)
		}

		for idx, text := range chunks {
			chunkID := uuid.New()
			chunkNodes = append(chunkNodes, knowledge.Chunk{
				ID:    chunkID.String(),
				Index: idx,
				Text:  text,
			})

			vec := pgvector.NewVector(embeddings[idx])
			if _, err := tx.Exec(ctx, `
				INSERT INTO rag_chunks (id, document_id, chunk_index, content, embedding, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
			`, chunkID, docID, idx, text, vec); err != nil {
				return fmt.Errorf("insert chunk %d: %w", idx, err)
			}
		}
	}

	if commitErr := tx.Commit(ctx); commitErr != nil {
		return fmt.Errorf("commit transaction: %w", commitErr)
	}

	if len(chunkNodes) == 0 {
		s.logger.Printf("no updates required for %s", relPath)
		return nil
	}

	doc := knowledge.Document{
		ID:     docID.String(),
		Path:   relPath,
		Title:  title,
		SHA:    hashHex,
		Folder: folder,
		Chunks: chunkNodes,
	}

	if err := knowledge.SyncDocument(ctx, s.driver, doc); err != nil {
		return fmt.Errorf("sync knowledge graph: %w", err)
	}

	s.logger.Printf("ingested %s (%d chunks)", relPath, len(chunkNodes))
	return nil
}

func upsertDocument(ctx context.Context, tx pgx.Tx, path, title, sha string) (uuid.UUID, bool, error) {
	var (
		docID        uuid.UUID
		existingHash string
	)

	err := tx.QueryRow(ctx, "SELECT id, sha256 FROM rag_documents WHERE source_path = $1", path).Scan(&docID, &existingHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			newID := uuid.New()
			_, execErr := tx.Exec(ctx, `
				INSERT INTO rag_documents (id, source_path, title, sha256, created_at, updated_at)
				VALUES ($1, $2, $3, $4, NOW(), NOW())
			`, newID, path, title, sha)
			if execErr != nil {
				return uuid.Nil, false, fmt.Errorf("insert document: %w", execErr)
			}
			return newID, true, nil
		}
		return uuid.Nil, false, fmt.Errorf("query document: %w", err)
	}

	if existingHash == sha {
		return docID, false, nil
	}

	if _, err := tx.Exec(ctx, `
		UPDATE rag_documents
		SET title = $2,
		    sha256 = $3,
		    updated_at = NOW()
		WHERE id = $1
	`, docID, title, sha); err != nil {
		return uuid.Nil, false, fmt.Errorf("update document: %w", err)
	}

	return docID, true, nil
}

func ExtractTitle(content, fallback string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			return strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
		}
	}
	return fallback
}

func ChunkMarkdown(content string, target, overlap int) []string {
	clean := strings.ReplaceAll(content, "\r\n", "\n")
	paragraphs := strings.Split(clean, "\n\n")
	chunks := make([]string, 0)
	current := make([]string, 0)
	currentLen := 0

	for _, paragraph := range paragraphs {
		p := strings.TrimSpace(paragraph)
		if p == "" {
			continue
		}

		paragraphLen := len(p)
		if currentLen+paragraphLen > target && len(current) > 0 {
			chunks = append(chunks, strings.Join(current, "\n\n"))
			if overlap > 0 && len(current) > 0 {
				last := current[len(current)-1]
				current = []string{last}
				currentLen = len(last)
			} else {
				current = current[:0]
				currentLen = 0
			}
		}

		current = append(current, p)
		currentLen += paragraphLen
	}

	if len(current) > 0 {
		chunks = append(chunks, strings.Join(current, "\n\n"))
	}

	return chunks
}
