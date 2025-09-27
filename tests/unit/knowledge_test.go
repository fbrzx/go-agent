package unit

import (
	"context"
	"testing"

	"github.com/fabfab/go-agent/knowledge"
)

func TestSyncDocumentNilDriver(t *testing.T) {
	doc := knowledge.Document{}
	if err := knowledge.SyncDocument(context.Background(), nil, doc); err == nil {
		t.Fatal("expected error when driver is nil")
	}
}
