package unit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

func TestDetectFormat(t *testing.T) {
	cases := map[string]ingestion.DocumentFormat{
		"document.md":    ingestion.FormatMarkdown,
		"notes.MARKDOWN": ingestion.FormatMarkdown,
		"report.pdf":     ingestion.FormatPDF,
		"data.csv":       ingestion.FormatCSV,
		"unknown.txt":    ingestion.FormatUnknown,
	}

	for path, want := range cases {
		if got := ingestion.DetectFormat(path); got != want {
			t.Fatalf("detect format for %s: want %s, got %s", path, want, got)
		}
	}
}

func TestIngestDocumentFromBytes(t *testing.T) {
	t.Parallel()

	content := "# Doc Title\n\n## Topic One\n\nParagraph one.\n\nParagraph two that should stay in the same chunk." +
		"\n\n## Topic Two\n\nMore content here."

	embed := &mockEmbedder{}
	svc := ingestion.NewService(nil, nil, embed, nil, 1)

	res, err := svc.IngestDocument(context.Background(), ingestion.DocumentPayload{
		Path: "memory/doc.md",
		Data: []byte(content),
	})
	if err != nil {
		t.Fatalf("ingest document: %v", err)
	}

	if res == nil {
		t.Fatal("expected non-nil result")
	}

	if want := "Doc Title"; res.Title != want {
		t.Fatalf("unexpected title: want %q, got %q", want, res.Title)
	}

	if want := "memory/doc.md"; res.RelPath != want {
		t.Fatalf("unexpected rel path: want %q, got %q", want, res.RelPath)
	}

	expectedFragments, expectedSections, expectedTopics := ingestion.ChunkMarkdown(content, 1000, 200)
	if len(res.Fragments) != len(expectedFragments) {
		t.Fatalf("unexpected fragment count: want %d, got %d", len(expectedFragments), len(res.Fragments))
	}

	for i, fragment := range res.Fragments {
		if fragment.Text != expectedFragments[i].Text {
			t.Fatalf("fragment %d text mismatch: want %q, got %q", i, expectedFragments[i].Text, fragment.Text)
		}
		if fragment.Section != expectedFragments[i].Section {
			t.Fatalf("fragment %d section mismatch: want %+v, got %+v", i, expectedFragments[i].Section, fragment.Section)
		}
	}

	if len(res.Sections) != len(expectedSections) {
		t.Fatalf("unexpected section count: want %d, got %d", len(expectedSections), len(res.Sections))
	}

	for i, section := range res.Sections {
		if section != expectedSections[i] {
			t.Fatalf("section %d mismatch: want %+v, got %+v", i, expectedSections[i], section)
		}
	}

	if len(res.Topics) != len(expectedTopics) {
		t.Fatalf("unexpected topic count: want %d, got %d", len(expectedTopics), len(res.Topics))
	}

	for i, topic := range res.Topics {
		if topic != expectedTopics[i] {
			t.Fatalf("topic %d mismatch: want %+v, got %+v", i, expectedTopics[i], topic)
		}
	}

	if embed.calls != 1 {
		t.Fatalf("expected embedder to be invoked once, got %d", embed.calls)
	}

	for i, text := range embed.lastTexts {
		if text != expectedFragments[i].Text {
			t.Fatalf("embed input %d mismatch: want %q, got %q", i, expectedFragments[i].Text, text)
		}
		if len(res.Embeddings[i]) != 1 {
			t.Fatalf("embedding %d length mismatch: want 1, got %d", i, len(res.Embeddings[i]))
		}
		if got := res.Embeddings[i][0]; got != float32(len(text)) {
			t.Fatalf("embedding %d value mismatch: want %f, got %f", i, float32(len(text)), got)
		}
	}
}

func TestIngestDocumentMatchesDiskIngestion(t *testing.T) {
	t.Parallel()

	content := "# Disk Title\n\n## Disk Topic\n\nFirst paragraph.\n\nSecond paragraph that should overlap across chunks for the test." +
		"\n\n## Another Topic\n\nClosing paragraph."

	directEmbed := &mockEmbedder{}
	directSvc := ingestion.NewService(nil, nil, directEmbed, nil, 1)

	directRes, err := directSvc.IngestDocument(context.Background(), ingestion.DocumentPayload{
		Path: "virtual/file.md",
		Data: []byte(content),
	})
	if err != nil {
		t.Fatalf("direct ingest: %v", err)
	}

	tmpDir := t.TempDir()
	diskDir := filepath.Join(tmpDir, "docs")
	if err := os.MkdirAll(diskDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	diskPath := filepath.Join(diskDir, "file.md")
	if err := os.WriteFile(diskPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	bytesFromDisk, err := os.ReadFile(diskPath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	diskEmbed := &mockEmbedder{}
	diskSvc := ingestion.NewService(nil, nil, diskEmbed, nil, 1)

	diskRes, err := diskSvc.IngestDocument(context.Background(), ingestion.DocumentPayload{
		Root: tmpDir,
		Path: diskPath,
		Data: bytesFromDisk,
	})
	if err != nil {
		t.Fatalf("disk ingest: %v", err)
	}

	if want := "docs/file.md"; diskRes.RelPath != want {
		t.Fatalf("unexpected rel path: want %q, got %q", want, diskRes.RelPath)
	}

	if want := "docs"; diskRes.Folder != want {
		t.Fatalf("unexpected folder: want %q, got %q", want, diskRes.Folder)
	}

	if diskRes.Title != directRes.Title {
		t.Fatalf("title mismatch: direct %q, disk %q", directRes.Title, diskRes.Title)
	}

	if diskRes.Hash != directRes.Hash {
		t.Fatalf("hash mismatch: direct %q, disk %q", directRes.Hash, diskRes.Hash)
	}

	if len(diskRes.Fragments) != len(directRes.Fragments) {
		t.Fatalf("fragment count mismatch: direct %d, disk %d", len(directRes.Fragments), len(diskRes.Fragments))
	}

	for i, fragment := range diskRes.Fragments {
		if fragment.Text != directRes.Fragments[i].Text {
			t.Fatalf("fragment %d text mismatch: direct %q, disk %q", i, directRes.Fragments[i].Text, fragment.Text)
		}
		if fragment.Section != directRes.Fragments[i].Section {
			t.Fatalf("fragment %d section mismatch: direct %+v, disk %+v", i, directRes.Fragments[i].Section, fragment.Section)
		}
	}

	if len(diskRes.Topics) != len(directRes.Topics) {
		t.Fatalf("topic count mismatch: direct %d, disk %d", len(directRes.Topics), len(diskRes.Topics))
	}

	for i, topic := range diskRes.Topics {
		if topic != directRes.Topics[i] {
			t.Fatalf("topic %d mismatch: direct %+v, disk %+v", i, directRes.Topics[i], topic)
		}
	}

	if len(diskRes.Sections) != len(directRes.Sections) {
		t.Fatalf("section count mismatch: direct %d, disk %d", len(directRes.Sections), len(diskRes.Sections))
	}

	for i, section := range diskRes.Sections {
		if section != directRes.Sections[i] {
			t.Fatalf("section %d mismatch: direct %+v, disk %+v", i, directRes.Sections[i], section)
		}
	}

	if len(diskEmbed.lastTexts) != len(directEmbed.lastTexts) {
		t.Fatalf("embed texts length mismatch: direct %d, disk %d", len(directEmbed.lastTexts), len(diskEmbed.lastTexts))
	}

	for i, text := range diskEmbed.lastTexts {
		if text != directEmbed.lastTexts[i] {
			t.Fatalf("embed text %d mismatch: direct %q, disk %q", i, directEmbed.lastTexts[i], text)
		}
	}
}

func TestChunkPlainText(t *testing.T) {
	content := "First paragraph line one.\nline two.\n\nSecond paragraph."
	fragments, sections := ingestion.ChunkPlainText(content, "Plain", 30, 5)

	if len(sections) != 1 {
		t.Fatalf("expected one section, got %d", len(sections))
	}

	if sections[0].Title != "Plain" {
		t.Fatalf("unexpected section title: %q", sections[0].Title)
	}

	if len(fragments) == 0 {
		t.Fatal("expected at least one fragment for plain text")
	}

	if !strings.Contains(fragments[0].Text, "First paragraph") {
		t.Fatalf("fragment does not contain expected text: %q", fragments[0].Text)
	}
}

func TestIngestDocumentCSV(t *testing.T) {
	t.Parallel()

	csvContent := "title,category\nHello,World\nAnother,Row"
	embed := &mockEmbedder{}
	svc := ingestion.NewService(nil, nil, embed, nil, 1)

	res, err := svc.IngestDocument(context.Background(), ingestion.DocumentPayload{
		Path: "memory/data.csv",
		Data: []byte(csvContent),
	})
	if err != nil {
		t.Fatalf("ingest csv: %v", err)
	}

	if embed.calls != 1 {
		t.Fatalf("expected embedder to be called once, got %d", embed.calls)
	}

	if res.Title != "title" {
		t.Fatalf("unexpected title: want 'title', got %q", res.Title)
	}

	if len(res.Sections) != 1 || res.Sections[0].Title != "Rows" {
		t.Fatalf("unexpected sections: %#v", res.Sections)
	}

	if len(res.Topics) != 2 {
		t.Fatalf("expected two topics, got %d", len(res.Topics))
	}

	if res.Topics[0].Name != "title" || res.Topics[1].Name != "category" {
		t.Fatalf("unexpected topics: %#v", res.Topics)
	}

	if len(res.Fragments) != 1 {
		t.Fatalf("expected single fragment, got %d", len(res.Fragments))
	}

	fragment := res.Fragments[0].Text
	if !strings.Contains(fragment, "Row 1") || !strings.Contains(fragment, "Row 2") {
		t.Fatalf("fragment missing row labels: %q", fragment)
	}
	if !strings.Contains(fragment, "Hello") || !strings.Contains(fragment, "World") {
		t.Fatalf("fragment missing csv data: %q", fragment)
	}
}

func TestIngestDocumentUnsupportedFormat(t *testing.T) {
	embed := &mockEmbedder{}
	svc := ingestion.NewService(nil, nil, embed, nil, 1)

	_, err := svc.IngestDocument(context.Background(), ingestion.DocumentPayload{
		Path: "memory/data.txt",
		Data: []byte("plain text"),
	})
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
}

type mockEmbedder struct {
	calls     int
	lastTexts []string
}

func (m *mockEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	m.calls++
	m.lastTexts = append([]string(nil), texts...)
	embeddings := make([][]float32, len(texts))
	for i, text := range texts {
		embeddings[i] = []float32{float32(len(text))}
	}
	return embeddings, nil
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
