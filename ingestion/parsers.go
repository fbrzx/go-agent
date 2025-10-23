package ingestion

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	pdf "github.com/dslipak/pdf"
)

type DocumentParser interface {
	Parse(ctx context.Context, payload DocumentPayload) (*ParsedDocument, error)
}

type ParsedDocument struct {
	Title     string
	Fragments []ChunkFragment
	Sections  []SectionMeta
	Topics    []TopicMeta
}

type markdownParser struct{}

func (markdownParser) Parse(_ context.Context, payload DocumentPayload) (*ParsedDocument, error) {
	content := string(payload.Data)
	title := ExtractTitle(content, filepath.Base(payload.Path))

	fragments, sections, topics := ChunkMarkdown(content, defaultChunkSize, defaultChunkOverlap)

	return &ParsedDocument{
		Title:     title,
		Fragments: fragments,
		Sections:  sections,
		Topics:    topics,
	}, nil
}

type pdfParser struct{}

func (pdfParser) Parse(_ context.Context, payload DocumentPayload) (*ParsedDocument, error) {
	reader := bytes.NewReader(payload.Data)
	doc, err := pdf.NewReader(reader, int64(len(payload.Data)))
	if err != nil {
		return nil, fmt.Errorf("open pdf: %w", err)
	}

	plain, err := doc.GetPlainText()
	if err != nil {
		return nil, fmt.Errorf("extract pdf text: %w", err)
	}

	buf := &bytes.Buffer{}
	if _, err := io.Copy(buf, plain); err != nil {
		return nil, fmt.Errorf("read pdf text: %w", err)
	}

	content := normalizePlainText(buf.String())
	title := firstNonEmptyLine(content)
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(payload.Path), filepath.Ext(payload.Path))
	}

	fragments, sections := ChunkPlainText(content, title, defaultChunkSize, defaultChunkOverlap)

	return &ParsedDocument{
		Title:     title,
		Fragments: fragments,
		Sections:  sections,
		Topics:    nil,
	}, nil
}

type csvParser struct{}

func (csvParser) Parse(_ context.Context, payload DocumentPayload) (*ParsedDocument, error) {
	reader := csv.NewReader(bytes.NewReader(payload.Data))
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse csv: %w", err)
	}

	if len(records) == 0 {
		return &ParsedDocument{
			Title:     strings.TrimSuffix(filepath.Base(payload.Path), filepath.Ext(payload.Path)),
			Fragments: nil,
			Sections:  nil,
			Topics:    nil,
		}, nil
	}

	headers := records[0]
	rows := records[1:]

	title := strings.TrimSuffix(filepath.Base(payload.Path), filepath.Ext(payload.Path))
	if headerTitle := firstNonEmpty(headers); headerTitle != "" {
		title = headerTitle
	}

	section := SectionMeta{Title: "Rows", Level: 1, Order: 0}
	sections := []SectionMeta{section}

	topics := make([]TopicMeta, 0, len(headers))
	for _, header := range headers {
		header = strings.TrimSpace(header)
		if header == "" {
			continue
		}
		topics = append(topics, TopicMeta{Name: header})
	}

	paragraphs := make([]paragraphWithSection, 0, len(rows))
	for idx, row := range rows {
		paragraphs = append(paragraphs, paragraphWithSection{
			Text:    formatCSVRow(headers, row, idx),
			Section: section,
		})
	}

	fragments := chunkParagraphs(paragraphs, defaultChunkSize, defaultChunkOverlap)

	return &ParsedDocument{
		Title:     title,
		Fragments: fragments,
		Sections:  sections,
		Topics:    topics,
	}, nil
}

func firstNonEmpty(values []string) string {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return ""
}

func normalizePlainText(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	return strings.Join(lines, "\n")
}

func firstNonEmptyLine(content string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func formatCSVRow(headers, row []string, idx int) string {
	builder := &strings.Builder{}
	builder.WriteString(fmt.Sprintf("Row %d", idx+1))
	if len(headers) > 0 {
		builder.WriteString("\n")
	}

	limit := len(headers)
	if len(row) < limit {
		limit = len(row)
	}

	for i := 0; i < limit; i++ {
		header := strings.TrimSpace(headers[i])
		value := strings.TrimSpace(row[i])
		if header == "" {
			header = fmt.Sprintf("Column %d", i+1)
		}
		builder.WriteString(header)
		builder.WriteString(": ")
		builder.WriteString(value)
		if i < limit-1 {
			builder.WriteString("\n")
		}
	}

	// Append any remaining values beyond the headers count.
	if len(row) > len(headers) {
		for i := len(headers); i < len(row); i++ {
			builder.WriteString("\n")
			builder.WriteString(fmt.Sprintf("Extra %d: %s", i+1, strings.TrimSpace(row[i])))
		}
	}

	return builder.String()
}
