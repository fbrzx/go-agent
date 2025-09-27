package knowledge

import (
	"context"
	"fmt"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

type Document struct {
	ID       string
	Path     string
	Title    string
	SHA      string
	Folder   string
	Chunks   []Chunk
	Sections []Section
	Topics   []Topic
}

type Chunk struct {
	ID        string
	Index     int
	Text      string
	SectionID string
}

type Section struct {
	ID    string
	Title string
	Level int
	Order int
}

type Topic struct {
	Name string
}

func SyncDocument(ctx context.Context, driver neo4j.DriverWithContext, doc Document) error {
	if driver == nil {
		return fmt.Errorf("neo4j driver is nil")
	}

	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	params := map[string]any{
		"id":     doc.ID,
		"path":   doc.Path,
		"title":  doc.Title,
		"sha":    doc.SHA,
		"folder": doc.Folder,
	}

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		if _, err := tx.Run(ctx, `
			MERGE (d:Document {id: $id})
			SET d.path = $path,
			    d.title = $title,
			    d.sha256 = $sha,
			    d.updated_at = datetime()
		`, params); err != nil {
			return nil, fmt.Errorf("upsert document node: %w", err)
		}

		if doc.Folder != "" {
			if _, err := tx.Run(ctx, `
				MATCH (d:Document {id: $id})-[r:IN_FOLDER]->(:Folder)
				DELETE r
			`, params); err != nil {
				return nil, fmt.Errorf("remove stale folder relation: %w", err)
			}
			if _, err := tx.Run(ctx, `
				MATCH (d:Document {id: $id})
				MERGE (f:Folder {name: $folder})
				MERGE (d)-[:IN_FOLDER]->(f)
			`, params); err != nil {
				return nil, fmt.Errorf("upsert folder relation: %w", err)
			}
		} else {
			if _, err := tx.Run(ctx, `
				MATCH (d:Document {id: $id})-[r:IN_FOLDER]->(f:Folder)
				DELETE r
				WITH f
				WHERE NOT (f)<-[:IN_FOLDER]-(:Document)
				DETACH DELETE f
			`, params); err != nil {
				return nil, fmt.Errorf("cleanup folder relation: %w", err)
			}
		}

		if _, err := tx.Run(ctx, `
			MATCH (d:Document {id: $id})-[:HAS_SECTION]->(s:Section)
			DETACH DELETE s
		`, map[string]any{"id": doc.ID}); err != nil {
			return nil, fmt.Errorf("clear existing sections: %w", err)
		}

		if _, err := tx.Run(ctx, `
			MATCH (d:Document {id: $id})-[r:HAS_TOPIC]->(t:Topic)
			DELETE r
		`, map[string]any{"id": doc.ID}); err != nil {
			return nil, fmt.Errorf("clear existing topics: %w", err)
		}

		for _, section := range doc.Sections {
			if _, err := tx.Run(ctx, `
				MATCH (d:Document {id: $doc_id})
				MERGE (s:Section {id: $section_id})
				SET s.title = $section_title,
				    s.level = $section_level,
				    s.order = $section_order
				MERGE (d)-[:HAS_SECTION {order: $section_order}]->(s)
			`, map[string]any{
				"doc_id":        doc.ID,
				"section_id":    section.ID,
				"section_title": section.Title,
				"section_level": section.Level,
				"section_order": section.Order,
			}); err != nil {
				return nil, fmt.Errorf("upsert section: %w", err)
			}
		}

		for _, topic := range doc.Topics {
			if topic.Name == "" {
				continue
			}
			if _, err := tx.Run(ctx, `
				MATCH (d:Document {id: $doc_id})
				MERGE (t:Topic {name: $topic_name})
				MERGE (d)-[:HAS_TOPIC]->(t)
			`, map[string]any{
				"doc_id":     doc.ID,
				"topic_name": topic.Name,
			}); err != nil {
				return nil, fmt.Errorf("upsert topic: %w", err)
			}
		}

		if _, err := tx.Run(ctx, `
			MATCH (d:Document {id: $id})-[:HAS_CHUNK]->(c:Chunk)
			DETACH DELETE c
		`, map[string]any{"id": doc.ID}); err != nil {
			return nil, fmt.Errorf("clear existing chunk nodes: %w", err)
		}

		for _, chunk := range doc.Chunks {
			if _, err := tx.Run(ctx, `
				MATCH (d:Document {id: $doc_id})
				MERGE (c:Chunk {id: $chunk_id})
				SET c.index = $chunk_index,
				    c.text = $chunk_text
				MERGE (d)-[:HAS_CHUNK {order: $chunk_index}]->(c)
			`, map[string]any{
				"doc_id":      doc.ID,
				"chunk_id":    chunk.ID,
				"chunk_index": chunk.Index,
				"chunk_text":  chunk.Text,
			}); err != nil {
				return nil, fmt.Errorf("upsert chunk node: %w", err)
			}

			if chunk.SectionID != "" {
				if _, err := tx.Run(ctx, `
					MATCH (s:Section {id: $section_id}), (c:Chunk {id: $chunk_id})
					MERGE (s)-[:HAS_CHUNK {order: $chunk_index}]->(c)
				`, map[string]any{
					"section_id":  chunk.SectionID,
					"chunk_id":    chunk.ID,
					"chunk_index": chunk.Index,
				}); err != nil {
					return nil, fmt.Errorf("link chunk to section: %w", err)
				}
			}
		}

		return nil, nil
	})

	if err == nil {
		if _, cleanupErr := session.Run(ctx, `
			MATCH (t:Topic)
			WHERE NOT (t)<-[:HAS_TOPIC]-(:Document)
			DELETE t
		`, nil); cleanupErr != nil && err == nil {
			err = cleanupErr
		}
	}

	return err
}
