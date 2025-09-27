package unit

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/fabfab/go-agent/ingestion"
)

func TestChunkMarkdownRespectsOverlap(t *testing.T) {
	text := "# Title\n\n" +
		"## Section One\n\n" +
		"Paragraph one." +
		"\n\n" +
		"Paragraph two is quite a bit longer than the first paragraph and should trigger a split." +
		"\n\n" +
		"Paragraph three." +
		"\n\n" +
		"Paragraph four."

	fragments, sections, topics := ingestion.ChunkMarkdown(text, 50, 10)
	if len(fragments) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(fragments))
	}

	if fragments[0].Text == fragments[1].Text {
		t.Fatalf("expected overlapping but not identical chunks")
	}

	if len(sections) == 0 {
		t.Fatal("expected at least one section metadata entry")
	}

	if len(topics) == 0 {
		t.Fatal("expected at least one topic from level-2 headings")
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
	fragments, sections, topics := ingestion.ChunkMarkdown("\n\n", 100, 20)
	if len(fragments) != 0 {
		t.Fatalf("expected no chunks for empty content, got %d", len(fragments))
	}
	if len(sections) != 0 {
		t.Fatalf("expected no sections for empty content, got %d", len(sections))
	}
	if len(topics) != 0 {
		t.Fatalf("expected no topics for empty content, got %d", len(topics))
	}
}
