package integration_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/fabfab/go-agent/chat"
	"github.com/fabfab/go-agent/config"
	"github.com/fabfab/go-agent/knowledge"
)

func TestGraphInsightsIncludesFoldersAndRelatedDocs(t *testing.T) {
	if os.Getenv("RUN_DB_INTEGRATION_TESTS") != "1" {
		t.Skip("set RUN_DB_INTEGRATION_TESTS=1 to run database connectivity checks")
	}

	cfg := config.Load()
	ctx := context.Background()

	driver, err := neo4j.NewDriverWithContext(cfg.Neo4jURI, neo4j.BasicAuth(cfg.Neo4jUser, cfg.Neo4jPass, ""))
	if err != nil {
		t.Fatalf("neo4j connection: %v", err)
	}
	defer driver.Close(ctx)

	docA := uuid.New().String()
	docB := uuid.New().String()
	folder := "integration/tests"
	sectionA := knowledge.Section{ID: uuid.New().String(), Title: "Overview", Level: 2, Order: 1}
	sectionB := knowledge.Section{ID: uuid.New().String(), Title: "Details", Level: 2, Order: 1}

	cleanup := func() {
		session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
		defer session.Close(ctx)
		_, _ = session.Run(ctx, "MATCH (d:Document) WHERE d.id IN $ids DETACH DELETE d", map[string]any{"ids": []string{docA, docB}})
		_, _ = session.Run(ctx, "MATCH (f:Folder {name: $name}) DETACH DELETE f", map[string]any{"name": folder})
	}

	cleanup()
	t.Cleanup(cleanup)

	if err := knowledge.SyncDocument(ctx, driver, knowledge.Document{
		ID:       docA,
		Path:     "integration/docA.md",
		Title:    "Doc A",
		SHA:      "sha-a",
		Folder:   folder,
		Sections: []knowledge.Section{sectionA},
		Topics:   []knowledge.Topic{{Name: "Integration"}},
		Chunks: []knowledge.Chunk{
			{ID: uuid.New().String(), Index: 0, Text: "chunk a1", SectionID: sectionA.ID},
			{ID: uuid.New().String(), Index: 1, Text: "chunk a2", SectionID: sectionA.ID},
		},
	}); err != nil {
		t.Fatalf("sync doc A: %v", err)
	}

	if err := knowledge.SyncDocument(ctx, driver, knowledge.Document{
		ID:       docB,
		Path:     "integration/docB.md",
		Title:    "Doc B",
		SHA:      "sha-b",
		Folder:   folder,
		Sections: []knowledge.Section{sectionB},
		Topics:   []knowledge.Topic{{Name: "Integration"}},
		Chunks:   []knowledge.Chunk{{ID: uuid.New().String(), Index: 0, Text: "chunk b1", SectionID: sectionB.ID}},
	}); err != nil {
		t.Fatalf("sync doc B: %v", err)
	}

	store := chat.NewNeo4jGraphStore(driver)
	insights, err := store.DocumentInsights(ctx, []string{docA})
	if err != nil {
		t.Fatalf("graph insights: %v", err)
	}

	info, ok := insights[docA]
	if !ok {
		t.Fatalf("missing insights for doc %s", docA)
	}

	if info.ChunkCount != 2 {
		t.Fatalf("expected chunk count 2, got %d", info.ChunkCount)
	}

	if len(info.Folders) != 1 || info.Folders[0] != folder {
		t.Fatalf("expected folder %s, got %#v", folder, info.Folders)
	}

	if len(info.RelatedDocuments) == 0 || info.RelatedDocuments[0].ID != docB {
		t.Fatalf("expected related document %s, got %#v", docB, info.RelatedDocuments)
	}
	if info.RelatedDocuments[0].Weight <= 0 {
		t.Fatalf("expected related weight > 0, got %f", info.RelatedDocuments[0].Weight)
	}
	if info.RelatedDocuments[0].Reason == "" {
		t.Fatalf("expected related reason, got empty")
	}

	if len(info.Sections) == 0 || info.Sections[0].Title != sectionA.Title {
		t.Fatalf("expected section %s, got %#v", sectionA.Title, info.Sections)
	}

	if len(info.Topics) == 0 || info.Topics[0] != "Integration" {
		t.Fatalf("expected topic Integration, got %#v", info.Topics)
	}
}
