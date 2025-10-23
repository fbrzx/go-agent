-- PostgreSQL initialization script for go-agent
-- This script is automatically run when the Docker container starts
-- The application will create the full schema including tables and indexes

-- Enable pgvector extension
CREATE EXTENSION IF NOT EXISTS vector;

-- Note: The rag_documents and rag_chunks tables will be created
-- automatically by the Go application when it starts up.
-- This ensures the embedding dimension is correctly set based on
-- the chosen model configuration.
