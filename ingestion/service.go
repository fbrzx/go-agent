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
	parsers   map[DocumentFormat]DocumentParser
}

// DocumentPayload represents the data required to ingest a document.
type DocumentPayload struct {
	// Root is the directory that should be treated as the ingestion root. It is
	// used to compute the document's relative path. When empty, the document
	// path is used as-is.
	Root string
	// Path is the absolute or virtual path associated with the document.
	Path string
	// Data contains the raw bytes of the document.
	Data []byte
	// Format optionally overrides the detected document format.
	Format DocumentFormat
}

// DocumentResult contains the parsed metadata and embeddings generated during
// ingestion. It is the in-memory representation of the document before it is
// persisted to storage backends.
type DocumentResult struct {
	RelPath    string
	Folder     string
	Title      string
	Hash       string
	Fragments  []ChunkFragment
	Sections   []SectionMeta
	Topics     []TopicMeta
	Embeddings [][]float32
}

// ErrNoChunks signals that parsing produced no chunkable content.
var ErrNoChunks = errors.New("document produced no chunks")

type ChunkFragment struct {
	Text    string
	Section SectionMeta
}

type SectionMeta struct {
	Title string
	Level int
	Order int
}

type TopicMeta struct {
	Name string
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
		parsers: map[DocumentFormat]DocumentParser{
			FormatMarkdown: markdownParser{},
			FormatPDF:      pdfParser{},
			FormatCSV:      csvParser{},
		},
	}
}

// IngestDocument chunks the provided payload, generates embeddings for each
// chunk, and returns the in-memory representation that would be persisted by
// the service. It does not perform any database or knowledge graph writes,
// making it suitable for unit testing and in-memory ingestion flows.
func (s *Service) IngestDocument(ctx context.Context, payload DocumentPayload) (*DocumentResult, error) {
	if s.embedder == nil {
		return nil, fmt.Errorf("embedder not configured")
	}
	if payload.Path == "" {
		return nil, fmt.Errorf("document path is required")
	}

	format := payload.Format
	if format == FormatUnknown {
		format = ""
	}
	if format == "" {
		format = DetectFormat(payload.Path)
	}
	if format == FormatUnknown {
		return nil, fmt.Errorf("unsupported document format: %s", payload.Path)
	}

	parser, ok := s.parsers[format]
	if !ok {
		return nil, fmt.Errorf("no parser registered for format %s", format)
	}

	relPath := payload.Path
	if payload.Root != "" {
		if candidate, err := filepath.Rel(payload.Root, payload.Path); err == nil {
			relPath = candidate
		}
	}
	relPath = filepath.ToSlash(relPath)

	folder := stdpath.Dir(relPath)
	if folder == "." || folder == "/" {
		folder = ""
	}

	payload.Format = format
	parsed, err := parser.Parse(ctx, payload)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", format, err)
	}

	if parsed == nil || len(parsed.Fragments) == 0 {
		return nil, ErrNoChunks
	}

	title := parsed.Title
	if title == "" {
		title = filepath.Base(payload.Path)
	}

	hash := sha256.Sum256(payload.Data)
	hashHex := hex.EncodeToString(hash[:])

	texts := make([]string, len(parsed.Fragments))
	for i, fragment := range parsed.Fragments {
		texts[i] = fragment.Text
	}

	embeddings, err := s.embedder.Embed(ctx, texts)
	if err != nil {
		return nil, fmt.Errorf("generate embeddings: %w", err)
	}

	if len(embeddings) != len(parsed.Fragments) {
		return nil, fmt.Errorf("embedding count mismatch: have %d chunks, %d embeddings", len(parsed.Fragments), len(embeddings))
	}

	return &DocumentResult{
		RelPath:    relPath,
		Folder:     folder,
		Title:      title,
		Hash:       hashHex,
		Fragments:  parsed.Fragments,
		Sections:   parsed.Sections,
		Topics:     parsed.Topics,
		Embeddings: embeddings,
	}, nil
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
		if format := DetectFormat(path); format != FormatUnknown {
			entries = append(entries, path)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("walk data directory: %w", err)
	}

	if len(entries) == 0 {
		s.logger.Printf("no supported documents found in %s", dir)
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
	format := DetectFormat(path)
	if format == FormatUnknown {
		s.logger.Printf("skip unsupported format for %s", path)
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	result, err := s.IngestDocument(ctx, DocumentPayload{Root: root, Path: path, Data: data, Format: format})
	if err != nil {
		if errors.Is(err, ErrNoChunks) {
			s.logger.Printf("skip empty document %s", path)
			return nil
		}
		return err
	}

	_, err = s.PersistDocument(ctx, result, format)
	return err
}

func (s *Service) PersistDocument(ctx context.Context, result *DocumentResult, format DocumentFormat) (count int, err error) {
	if result == nil {
		return 0, fmt.Errorf("document result is nil")
	}

	if err := database.EnsureRAGSchema(ctx, s.pool, s.dimension); err != nil {
		return 0, fmt.Errorf("ensure schema: %w", err)
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}

	defer func() {
		if err != nil {
			if rbErr := tx.Rollback(ctx); rbErr != nil {
				s.logger.Printf("rollback error: %v", rbErr)
			}
		}
	}()

	docID, changed, err := upsertDocument(ctx, tx, result.RelPath, result.Title, result.Hash)
	if err != nil {
		return 0, err
	}

	sectionIDs := map[int]string{}
	sections := make([]knowledge.Section, 0, len(result.Sections))
	for _, sectionMeta := range result.Sections {
		id := uuid.New().String()
		sections = append(sections, knowledge.Section{
			ID:    id,
			Title: sectionMeta.Title,
			Level: sectionMeta.Level,
			Order: sectionMeta.Order,
		})
		sectionIDs[sectionMeta.Order] = id
	}

	topics := make([]knowledge.Topic, 0, len(result.Topics))
	for _, topicMeta := range result.Topics {
		if topicMeta.Name == "" {
			continue
		}
		topics = append(topics, knowledge.Topic{Name: topicMeta.Name})
	}

	chunkNodes := make([]knowledge.Chunk, 0, len(result.Fragments))

	if changed {
		if _, err = tx.Exec(ctx, "DELETE FROM rag_chunks WHERE document_id = $1", docID); err != nil {
			return 0, fmt.Errorf("clear existing chunks: %w", err)
		}

		for idx, fragment := range result.Fragments {
			chunkID := uuid.New()
			chunkNodes = append(chunkNodes, knowledge.Chunk{
				ID:        chunkID.String(),
				Index:     idx,
				Text:      fragment.Text,
				SectionID: sectionIDs[fragment.Section.Order],
			})

			vec := pgvector.NewVector(result.Embeddings[idx])
			if _, err := tx.Exec(ctx, `
                                INSERT INTO rag_chunks (id, document_id, chunk_index, section_order, section_level, section_title, content, embedding, created_at, updated_at)
                                VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
                        `, chunkID, docID, idx, fragment.Section.Order, fragment.Section.Level, fragment.Section.Title, fragment.Text, vec); err != nil {
				return 0, fmt.Errorf("insert chunk %d: %w", idx, err)
			}
		}
	}

	if commitErr := tx.Commit(ctx); commitErr != nil {
		return 0, fmt.Errorf("commit transaction: %w", commitErr)
	}

	if len(chunkNodes) == 0 {
		s.logger.Printf("no updates required for %s", result.RelPath)
		return 0, nil
	}

	doc := knowledge.Document{
		ID:       docID.String(),
		Path:     result.RelPath,
		Title:    result.Title,
		SHA:      result.Hash,
		Folder:   result.Folder,
		Chunks:   chunkNodes,
		Sections: sections,
		Topics:   topics,
	}

	if err := knowledge.SyncDocument(ctx, s.driver, doc); err != nil {
		return 0, fmt.Errorf("sync knowledge graph: %w", err)
	}

	s.logger.Printf("ingested %s [%s] (%d chunks)", result.RelPath, format, len(chunkNodes))
	return len(chunkNodes), nil
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

func ChunkMarkdown(content string, target, overlap int) ([]ChunkFragment, []SectionMeta, []TopicMeta) {
	clean := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(clean, "\n")

	introSection := SectionMeta{Title: "Introduction", Level: 1, Order: 0}
	currentSection := introSection
	sectionOrder := 0
	introUsed := false

	sections := make([]SectionMeta, 0)
	paragraphs := make([]paragraphWithSection, 0)
	topicsSet := make(map[string]struct{})
	topics := make([]TopicMeta, 0)

	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "#") {
			headingLevel := headingLevel(line)
			title := strings.TrimSpace(line[headingLevel:])
			if title == "" {
				continue
			}

			if headingLevel <= 1 {
				introSection.Title = title
				currentSection = SectionMeta{Title: title, Level: 1, Order: 0}
			} else {
				sectionOrder++
				currentSection = SectionMeta{Title: title, Level: headingLevel, Order: sectionOrder}
				sections = append(sections, currentSection)
				if headingLevel == 2 {
					if _, seen := topicsSet[title]; !seen {
						topicsSet[title] = struct{}{}
						topics = append(topics, TopicMeta{Name: title})
					}
				}
			}

			paragraphs = append(paragraphs, paragraphWithSection{Text: title, Section: currentSection})
			if currentSection.Order == 0 {
				introUsed = true
			}
			continue
		}

		paragraphs = append(paragraphs, paragraphWithSection{Text: line, Section: currentSection})
		if currentSection.Order == 0 {
			introUsed = true
		}
	}

	if introUsed {
		top := SectionMeta{Title: introSection.Title, Level: introSection.Level, Order: introSection.Order}
		sections = append([]SectionMeta{top}, sections...)
	}

	fragments := chunkParagraphs(paragraphs, target, overlap)
	return fragments, sections, topics
}

func ChunkPlainText(content, title string, target, overlap int) ([]ChunkFragment, []SectionMeta) {
	clean := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(clean, "\n")
	section := SectionMeta{Title: title, Level: 1, Order: 0}

	paragraphs := make([]paragraphWithSection, 0)
	current := make([]string, 0)

	addParagraph := func() {
		if len(current) == 0 {
			return
		}
		paragraph := strings.Join(current, "\n")
		paragraphs = append(paragraphs, paragraphWithSection{Text: paragraph, Section: section})
		current = current[:0]
	}

	for _, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			addParagraph()
			continue
		}
		current = append(current, trimmed)
	}

	addParagraph()

	fragments := chunkParagraphs(paragraphs, target, overlap)
	sections := []SectionMeta{section}
	return fragments, sections
}

type paragraphWithSection struct {
	Text    string
	Section SectionMeta
}

func chunkParagraphs(paragraphs []paragraphWithSection, target, overlap int) []ChunkFragment {
	fragments := make([]ChunkFragment, 0)
	if target <= 0 {
		target = defaultChunkSize
	}
	if overlap < 0 {
		overlap = 0
	}

	current := make([]paragraphWithSection, 0)
	currentLen := 0

	for _, paragraph := range paragraphs {
		pLen := len(paragraph.Text)
		if currentLen+pLen > target && len(current) > 0 {
			fragments = append(fragments, buildChunkFragment(current))
			if overlap > 0 && len(current) > 0 {
				last := current[len(current)-1]
				current = []paragraphWithSection{last}
				currentLen = len(last.Text)
			} else {
				current = current[:0]
				currentLen = 0
			}
		}

		current = append(current, paragraph)
		currentLen += pLen
	}

	if len(current) > 0 {
		fragments = append(fragments, buildChunkFragment(current))
	}

	return fragments
}

func headingLevel(line string) int {
	level := 0
	for _, ch := range line {
		if ch == '#' {
			level++
			continue
		}
		break
	}
	if level == 0 {
		return 1
	}
	if level > 6 {
		return 6
	}
	return level
}

func buildChunkFragment(paragraphs []paragraphWithSection) ChunkFragment {
	texts := make([]string, len(paragraphs))
	section := SectionMeta{Title: "Introduction", Level: 1, Order: 0}
	for i, paragraph := range paragraphs {
		texts[i] = paragraph.Text
		if paragraph.Section.Order > section.Order || (section.Order == 0 && paragraph.Section.Title != "") {
			section = paragraph.Section
		}
	}

	return ChunkFragment{
		Text:    strings.Join(texts, "\n\n"),
		Section: section,
	}
}
