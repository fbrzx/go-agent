package unit

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/fabfab/go-agent/ingestion"
)

func TestChunkMarkdownRespectsOverlap(t *testing.T) {
	text := "# Title\n\n" +
		"Paragraph one." +
		"\n\n" +
		"Paragraph two is quite a bit longer than the first paragraph and should trigger a split." +
		"\n\n" +
		"Paragraph three." +
		"\n\n" +
		"Paragraph four."

	chunks := ingestion.ChunkMarkdown(text, 50, 10)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}

	if chunks[0] == chunks[1] {
		t.Fatalf("expected overlapping but not identical chunks")
	}
}

func TestExtractTitle(t *testing.T) {
	content := "Some intro\n# Heading One\nMore text"
	title := ingestion.ExtractTitle(content, "fallback")
	if title != "Heading One" {
		t.Fatalf("expected title 'Heading One', got %q", title)
	}
}

func TestIngestDirectoryMissingEmbedder(t *testing.T) {
	svc := ingestion.NewService((*pgxpool.Pool)(nil), nil, nil, nil, 128)
	if err := svc.IngestDirectory(context.Background(), "./does-not-matter"); err == nil {
		t.Fatal("expected error when embedder is nil")
	}
}

func TestChunkMarkdownHandlesEmpty(t *testing.T) {
	chunks := ingestion.ChunkMarkdown("\n\n", 100, 20)
	if len(chunks) != 0 {
		t.Fatalf("expected no chunks for empty content, got %d", len(chunks))
	}
}
