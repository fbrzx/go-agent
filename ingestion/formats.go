// Package ingestion handles document parsing, chunking, and persistence to vector and graph databases.
package ingestion

import (
	"path/filepath"
	"strings"
)

// DocumentFormat enumerates supported document payload formats.
type DocumentFormat string

const (
	// FormatUnknown represents an unsupported or undetected format.
	FormatUnknown DocumentFormat = ""
	// FormatMarkdown represents Markdown documents.
	FormatMarkdown DocumentFormat = "markdown"
	// FormatPDF represents PDF documents.
	FormatPDF DocumentFormat = "pdf"
	// FormatCSV represents comma separated values documents.
	FormatCSV DocumentFormat = "csv"
)

// DetectFormat infers a document format from the provided path's extension.
func DetectFormat(path string) DocumentFormat {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".md", ".markdown":
		return FormatMarkdown
	case ".pdf":
		return FormatPDF
	case ".csv":
		return FormatCSV
	default:
		return FormatUnknown
	}
}
