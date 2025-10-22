package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/fabfab/go-agent/chat"
	"github.com/fabfab/go-agent/config"
	"github.com/fabfab/go-agent/database"
	"github.com/fabfab/go-agent/embeddings"
	"github.com/fabfab/go-agent/ingestion"
	"github.com/fabfab/go-agent/llm"
)

const (
	defaultChatLimit = 5
	maxUploadSize    = 16 << 20 // 16 MiB
)

// Server exposes HTTP handlers for the core go-agent workflows.
type Server struct {
	cfg         config.Config
	logger      *log.Logger
	handler     http.Handler
	pgPool      *pgxpool.Pool
	neo4jDriver neo4j.DriverWithContext
	embedder    embeddings.Embedder
	llmClient   llm.StreamClient
}

// CleanupFunc is a function that cleans up server resources
type CleanupFunc func()

type messageResponse struct {
	Message string `json:"message"`
}

type errorResponse struct {
	Error string `json:"error"`
}

type ingestUploadResponse struct {
	Message  string           `json:"message"`
	Document uploadedDocument `json:"document"`
}

type uploadedDocument struct {
	Title  string `json:"title"`
	Path   string `json:"path"`
	Format string `json:"format"`
	Chunks int    `json:"chunks"`
}

type ingestRequest struct {
	Dir string `json:"dir"`
}

type clearRequest struct {
	Confirm bool `json:"confirm"`
}

type chatRequest struct {
	Question string           `json:"question"`
	Limit    int              `json:"limit"`
	Sections []string         `json:"sections"`
	Topics   []string         `json:"topics"`
	History  []messagePayload `json:"history"`
}

type chatResponse struct {
	Answer  string           `json:"answer"`
	Sources []chatSource     `json:"sources"`
	History []messagePayload `json:"history,omitempty"`
}

type messagePayload struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatSource struct {
	DocumentID string              `json:"documentId"`
	Title      string              `json:"title"`
	Path       string              `json:"path"`
	Snippet    string              `json:"snippet"`
	Score      float64             `json:"score"`
	Insight    chatDocumentInsight `json:"insight"`
}

type chatDocumentInsight struct {
	ChunkCount       int                   `json:"chunkCount"`
	Folders          []string              `json:"folders"`
	Sections         []chatSectionInfo     `json:"sections"`
	Topics           []string              `json:"topics"`
	RelatedDocuments []chatRelatedDocument `json:"relatedDocuments"`
}

type chatSectionInfo struct {
	Title string `json:"title"`
	Level int    `json:"level"`
	Order int    `json:"order"`
}

type chatRelatedDocument struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Path       string  `json:"path"`
	Weight     float64 `json:"weight"`
	Similarity float64 `json:"similarity"`
	Reason     string  `json:"reason"`
}

type chatStreamChunk struct {
	Content string `json:"content"`
}

type chatStreamFinal struct {
	Answer  string           `json:"answer"`
	Sources []chatSource     `json:"sources"`
	History []messagePayload `json:"history"`
}

// New constructs a Server that serves the HTTP API using the provided configuration.
// It initializes database connections that are reused across requests for better performance.
// Returns the server and a cleanup function that should be called when shutting down.
func New(cfg config.Config, logger *log.Logger) (*Server, CleanupFunc, error) {
	if logger == nil {
		logger = log.Default()
	}

	ctx := context.Background()

	// Initialize PostgreSQL connection pool
	pgPool, err := database.NewPostgresPool(ctx, cfg.PostgresDSN)
	if err != nil {
		return nil, nil, fmt.Errorf("postgres connection: %w", err)
	}

	// Initialize Neo4j driver
	neo4jDriver, err := database.NewNeo4jDriver(ctx, cfg.Neo4jURI, cfg.Neo4jUser, cfg.Neo4jPass)
	if err != nil {
		pgPool.Close()
		return nil, nil, fmt.Errorf("neo4j connection: %w", err)
	}

	// Initialize embedder
	embedder, err := embeddings.NewEmbedder(cfg)
	if err != nil {
		neo4jDriver.Close(ctx)
		pgPool.Close()
		return nil, nil, fmt.Errorf("embedder setup: %w", err)
	}

	// Initialize LLM client
	llmClient, err := llm.NewClient(cfg)
	if err != nil {
		neo4jDriver.Close(ctx)
		pgPool.Close()
		return nil, nil, fmt.Errorf("llm setup: %w", err)
	}

	s := &Server{
		cfg:         cfg,
		logger:      logger,
		pgPool:      pgPool,
		neo4jDriver: neo4jDriver,
		embedder:    embedder,
		llmClient:   llmClient,
	}
	s.handler = s.routes()

	cleanup := func() {
		if neo4jDriver != nil {
			neo4jDriver.Close(ctx)
		}
		if pgPool != nil {
			pgPool.Close()
		}
	}

	return s, cleanup, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

func (s *Server) Handler() http.Handler {
	return s.handler
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/openapi.yaml", s.handleOpenAPI)
	mux.HandleFunc("/v1/ingest", s.handleIngest)
	mux.HandleFunc("/v1/ingest/upload", s.handleIngestUpload)
	mux.HandleFunc("/v1/chat", s.handleChat)
	mux.HandleFunc("/v1/chat/stream", s.handleChatStream)
	mux.HandleFunc("/v1/clear", s.handleClear)
	mux.HandleFunc("/", s.handleRoot)
	mux.Handle("/assets/", s.staticHandler())
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.methodNotAllowed(w, http.MethodGet)
		return
	}

	s.writeJSON(w, http.StatusOK, messageResponse{Message: "ok"})
}

func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.methodNotAllowed(w, http.MethodGet)
		return
	}

	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	w.Header().Set("Content-Disposition", "inline; filename=\"openapi.yaml\"")
	_, _ = w.Write(openAPISpecYAML)
}

func (s *Server) handleIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.methodNotAllowed(w, http.MethodPost)
		return
	}

	var req ingestRequest
	if err := decodeJSON(r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
		return
	}

	dir := strings.TrimSpace(req.Dir)
	if dir == "" {
		dir = s.cfg.DataDir
	}

	ctx := r.Context()

	svc, cleanup, err := s.buildIngestionService(ctx)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer cleanup()

	s.logger.Printf("ingesting documents from %s using %s/%s embeddings", dir, strings.ToUpper(s.cfg.Embeddings.Provider), s.cfg.Embeddings.Model)

	if err := svc.IngestDirectory(ctx, dir); err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Errorf("ingestion failed: %w", err))
		return
	}

	s.writeJSON(w, http.StatusOK, messageResponse{Message: "ingestion complete"})
}

func (s *Server) handleIngestUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.methodNotAllowed(w, http.MethodPost)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Errorf("parse multipart form: %w", err))
		return
	}

	file, header, err := r.FormFile("document")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Errorf("document field is required: %w", err))
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Errorf("read upload: %w", err))
		return
	}
	if len(data) == 0 {
		s.writeError(w, http.StatusBadRequest, fmt.Errorf("uploaded file is empty"))
		return
	}

	fileName := sanitizeUploadName(header.Filename)
	format := ingestion.DetectFormat(fileName)
	if format == ingestion.FormatUnknown {
		s.writeError(w, http.StatusBadRequest, fmt.Errorf("unsupported document format: %s", fileName))
		return
	}

	timestamp := time.Now().UTC().Format("20060102-150405")
	relativePath := filepath.ToSlash(filepath.Join("uploads", fmt.Sprintf("%s-%s", timestamp, fileName)))
	payload := ingestion.DocumentPayload{Path: relativePath, Data: data, Format: format}

	ctx := r.Context()

	svc, cleanup, err := s.buildIngestionService(ctx)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer cleanup()

	result, err := svc.IngestDocument(ctx, payload)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ingestion.ErrNoChunks) {
			status = http.StatusBadRequest
		}
		s.writeError(w, status, fmt.Errorf("ingest document: %w", err))
		return
	}

	chunks, err := svc.PersistDocument(ctx, result, format)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Errorf("persist document: %w", err))
		return
	}

	message := fmt.Sprintf("ingested %s", result.Title)
	if chunks == 0 {
		message = fmt.Sprintf("no updates required for %s", result.Title)
	}

	s.writeJSON(w, http.StatusOK, ingestUploadResponse{
		Message: message,
		Document: uploadedDocument{
			Title:  result.Title,
			Path:   result.RelPath,
			Format: string(format),
			Chunks: chunks,
		},
	})
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.methodNotAllowed(w, http.MethodPost)
		return
	}

	var req chatRequest
	if err := decodeJSON(r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
		return
	}

	req.Question = strings.TrimSpace(req.Question)
	if req.Question == "" {
		s.writeError(w, http.StatusBadRequest, fmt.Errorf("question is required"))
		return
	}

	ctx := r.Context()

	history, err := parseHistory(req.History)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}

	svc, cleanup, err := s.buildChatService(ctx)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer cleanup()

	resp, updatedHistory, err := svc.ChatStream(ctx, req.Question, chat.Config{
		SimilarityLimit: s.resolveLimit(req.Limit),
		SectionFilters:  req.Sections,
		TopicFilters:    req.Topics,
	}, history, nil)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Errorf("chat failed: %w", err))
		return
	}

	s.writeJSON(w, http.StatusOK, buildChatResponse(resp, updatedHistory))
}

func (s *Server) handleChatStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.methodNotAllowed(w, http.MethodPost)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		s.writeError(w, http.StatusInternalServerError, fmt.Errorf("streaming not supported"))
		return
	}

	var req chatRequest
	if err := decodeJSON(r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
		return
	}

	req.Question = strings.TrimSpace(req.Question)
	if req.Question == "" {
		s.writeError(w, http.StatusBadRequest, fmt.Errorf("question is required"))
		return
	}

	history, err := parseHistory(req.History)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}

	ctx := r.Context()
	svc, cleanup, err := s.buildChatService(ctx)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer cleanup()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	resp, updatedHistory, err := svc.ChatStream(ctx, req.Question, chat.Config{
		SimilarityLimit: s.resolveLimit(req.Limit),
		SectionFilters:  req.Sections,
		TopicFilters:    req.Topics,
	}, history, func(chunk string) error {
		return s.sendSSE(w, flusher, "chunk", chatStreamChunk{Content: chunk})
	})
	if err != nil {
		_ = s.sendSSE(w, flusher, "error", errorResponse{Error: err.Error()})
		return
	}

	final := buildChatResponse(resp, updatedHistory)
	_ = s.sendSSE(w, flusher, "final", chatStreamFinal{
		Answer:  final.Answer,
		Sources: final.Sources,
		History: final.History,
	})
	_ = s.sendSSE(w, flusher, "done", messageResponse{Message: "complete"})
}

func (s *Server) handleClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.methodNotAllowed(w, http.MethodPost)
		return
	}

	var req clearRequest
	if err := decodeJSON(r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
		return
	}

	if !req.Confirm {
		s.writeError(w, http.StatusBadRequest, fmt.Errorf("confirm must be true to clear data"))
		return
	}

	ctx := r.Context()

	// Use existing connection pool
	if _, err := s.pgPool.Exec(ctx, "TRUNCATE rag_chunks, rag_documents"); err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Errorf("truncate postgres tables: %w", err))
		return
	}
	s.logger.Println("cleared Postgres rag_documents and rag_chunks")

	// Use existing Neo4j driver
	session := s.neo4jDriver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	if err := purgeNeo4j(ctx, session); err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Errorf("clear neo4j: %w", err))
		return
	}

	s.logger.Println("Neo4j documents and chunks cleared")
	s.logger.Println("RAG data removed")

	s.writeJSON(w, http.StatusOK, messageResponse{Message: "rag data cleared"})
}

func (s *Server) methodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed)
	s.writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed, use %s", allowed))
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		s.logger.Printf("encode response: %v", err)
	}
}

func (s *Server) writeError(w http.ResponseWriter, status int, err error) {
	s.logger.Printf("api error (%d): %v", status, err)
	s.writeJSON(w, status, errorResponse{Error: err.Error()})
}

func decodeJSON(r *http.Request, dst any) error {
	if r.Body == nil {
		return nil
	}
	defer r.Body.Close()

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if err == io.EOF {
			return nil
		}
		return err
	}

	if dec.More() {
		return fmt.Errorf("request body must contain a single JSON object")
	}

	return nil
}

func (s *Server) resolveLimit(limit int) int {
	if limit <= 0 {
		return defaultChatLimit
	}
	return limit
}

func (s *Server) buildIngestionService(ctx context.Context) (*ingestion.Service, func(), error) {
	// Reuse existing connections from the server
	svc := ingestion.NewService(s.pgPool, s.neo4jDriver, s.embedder, s.logger, s.cfg.Embeddings.Dimension)

	// No cleanup needed as connections are managed by the server
	cleanup := func() {}

	return svc, cleanup, nil
}

func (s *Server) buildChatService(ctx context.Context) (*chat.Service, func(), error) {
	// Reuse existing connections from the server
	vectorStore := chat.NewPostgresVectorStore(s.pgPool)
	graphStore := chat.NewNeo4jGraphStore(s.neo4jDriver)
	svc := chat.NewService(vectorStore, graphStore, s.embedder, s.llmClient, s.logger)

	// No cleanup needed as connections are managed by the server
	cleanup := func() {}

	return svc, cleanup, nil
}

func parseHistory(payloads []messagePayload) ([]llm.Message, error) {
	if len(payloads) == 0 {
		return nil, nil
	}
	messages := make([]llm.Message, 0, len(payloads))
	for _, payload := range payloads {
		role := strings.TrimSpace(payload.Role)
		content := strings.TrimSpace(payload.Content)
		if role == "" || content == "" {
			continue
		}
		switch role {
		case llm.RoleUser, llm.RoleAssistant, llm.RoleSystem:
			messages = append(messages, llm.Message{Role: role, Content: content})
		default:
			return nil, fmt.Errorf("unsupported history role: %s", role)
		}
	}
	return messages, nil
}

func toMessagePayloads(messages []llm.Message) []messagePayload {
	if len(messages) == 0 {
		return nil
	}
	converted := make([]messagePayload, 0, len(messages))
	for _, msg := range messages {
		converted = append(converted, messagePayload{Role: msg.Role, Content: msg.Content})
	}
	return converted
}

func buildChatResponse(resp chat.Response, history []llm.Message) chatResponse {
	converted := chatResponse{Answer: resp.Answer}
	converted.Sources = buildSources(resp.Sources)
	if len(history) > 0 {
		converted.History = toMessagePayloads(history)
	}
	return converted
}

func buildSources(sources []chat.Source) []chatSource {
	if len(sources) == 0 {
		return nil
	}
	converted := make([]chatSource, len(sources))
	for i, src := range sources {
		converted[i] = chatSource{
			DocumentID: src.DocumentID,
			Title:      src.Title,
			Path:       src.Path,
			Snippet:    src.Snippet,
			Score:      src.Score,
			Insight:    transformInsight(src.Insight),
		}
	}
	return converted
}

func sanitizeUploadName(name string) string {
	base := filepath.Base(name)
	base = strings.TrimSpace(base)
	if base == "" || base == "." {
		base = "document"
	}
	sanitized := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		switch r {
		case '.', '-', '_':
			return r
		}
		return '_'
	}, base)
	if sanitized == "" {
		return "document"
	}
	return sanitized
}

func transformInsight(insight chat.DocumentInsight) chatDocumentInsight {
	sections := make([]chatSectionInfo, len(insight.Sections))
	for i, section := range insight.Sections {
		sections[i] = chatSectionInfo{
			Title: section.Title,
			Level: section.Level,
			Order: section.Order,
		}
	}

	related := make([]chatRelatedDocument, len(insight.RelatedDocuments))
	for i, doc := range insight.RelatedDocuments {
		related[i] = chatRelatedDocument{
			ID:         doc.ID,
			Title:      doc.Title,
			Path:       doc.Path,
			Weight:     doc.Weight,
			Similarity: doc.Similarity,
			Reason:     doc.Reason,
		}
	}

	return chatDocumentInsight{
		ChunkCount:       insight.ChunkCount,
		Folders:          append([]string(nil), insight.Folders...),
		Sections:         sections,
		Topics:           append([]string(nil), insight.Topics...),
		RelatedDocuments: related,
	}
}

func (s *Server) sendSSE(w http.ResponseWriter, flusher http.Flusher, event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		s.logger.Printf("marshal sse payload: %v", err)
		return err
	}
	if event != "" {
		if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func purgeNeo4j(ctx context.Context, session neo4j.SessionWithContext) error {
	queries := []string{
		"MATCH (d:Document) DETACH DELETE d",
		"MATCH (c:Chunk) DETACH DELETE c",
		"MATCH (f:Folder) DETACH DELETE f",
	}

	for _, query := range queries {
		result, err := session.Run(ctx, query, nil)
		if err != nil {
			return err
		}
		if _, err := result.Consume(ctx); err != nil {
			return err
		}
	}

	return nil
}
