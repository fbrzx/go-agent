# Docker Deployment Guide

This guide explains how to run the Go-Agent RAG application using Docker and Docker Compose.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Quick Start](#quick-start)
- [Architecture](#architecture)
- [Configuration](#configuration)
- [Development Setup](#development-setup)
- [Production Deployment](#production-deployment)
- [Service Management](#service-management)
- [Troubleshooting](#troubleshooting)
- [Performance Tuning](#performance-tuning)

## Prerequisites

- Docker Engine 20.10+ ([Install Docker](https://docs.docker.com/get-docker/))
- Docker Compose V2 ([Install Compose](https://docs.docker.com/compose/install/))
- At least 8GB RAM (16GB recommended for running local LLMs)
- 20GB free disk space

## Quick Start

### 1. Clone and Setup

```bash
# Clone the repository
cd go-agent

# Copy the environment file
cp .env.docker .env

# Edit the .env file if needed
nano .env
```

### 2. Start All Services

```bash
# Start all services in detached mode
docker compose up -d

# View logs
docker compose logs -f

# Check service health
docker compose ps
```

### 3. Wait for Services to Initialize

The first startup takes 5-10 minutes because:
- Ollama needs to download LLM models (~4-5GB)
- Databases need to initialize schemas
- Dependencies need to be resolved

Monitor progress:
```bash
# Watch the ollama-setup service download models
docker compose logs -f ollama-setup

# Check if all services are healthy
docker compose ps
```

### 4. Access the Application

- **Web UI**: http://localhost:8080
- **API Docs**: http://localhost:8080/openapi.yaml
- **Neo4j Browser**: http://localhost:7474 (neo4j / mysecretpassword)
- **Health Check**: http://localhost:8080/healthz

### 5. Ingest Documents

```bash
# Place documents in ./documents directory
mkdir -p documents
cp /path/to/your/*.md documents/

# Ingest via API
curl -X POST http://localhost:8080/v1/ingest \
  -H "Content-Type: application/json" \
  -d '{"dir": "/app/documents"}'

# Or upload a single file
curl -X POST http://localhost:8080/v1/ingest/upload \
  -F "document=@documents/example.md"
```

### 6. Query via Chat

```bash
# Non-streaming chat
curl -X POST http://localhost:8080/v1/chat \
  -H "Content-Type: application/json" \
  -d '{
    "question": "What is this project about?",
    "limit": 5
  }'

# Streaming chat
curl -X POST http://localhost:8080/v1/chat/stream \
  -H "Content-Type: application/json" \
  -d '{"question": "Explain the architecture"}' \
  --no-buffer
```

## Architecture

The Docker Compose setup includes 5 services:

```
┌─────────────────┐
│   go-agent      │  Port 8080 - Main application (HTTP API + UI)
│   (Go + React)  │
└────────┬────────┘
         │
    ┌────┴────┬──────────┬──────────┐
    │         │          │          │
┌───▼────┐ ┌──▼──────┐ ┌▼────────┐ ┌▼─────────────┐
│postgres│ │  neo4j  │ │ ollama  │ │ollama-setup  │
│+ vector│ │  graph  │ │  LLM    │ │ (init only)  │
└────────┘ └─────────┘ └─────────┘ └──────────────┘
Port 5432   Ports 7474  Port 11434
            & 7687
```

### Service Details

| Service | Purpose | Image | Resources |
|---------|---------|-------|-----------|
| **postgres** | Vector database with pgvector | Custom (Postgres 16.4 + pgvector) | 512MB RAM |
| **neo4j** | Graph database for relationships | neo4j:5.24-community | 2GB heap + 1GB pagecache |
| **ollama** | Local LLM inference engine | ollama/ollama:latest | 4GB+ RAM, GPU optional |
| **ollama-setup** | Downloads models on first run | ollama/ollama:latest | Runs once, then exits |
| **go-agent** | Main application server | Built from source | 512MB RAM |

## Configuration

### Environment Variables

Edit `.env` file to configure:

```bash
# Database Credentials
POSTGRES_PASSWORD=mysecretpassword
NEO4J_PASSWORD=mysecretpassword

# AI Provider (ollama or openai)
LLM_PROVIDER=ollama
EMBEDDING_PROVIDER=ollama

# Ollama Models
LLM_MODEL=llama3.1:8b
EMBEDDING_MODEL=nomic-embed-text
EMBEDDING_DIMENSION=768

# OpenAI (if using openai provider)
# OPENAI_API_KEY=sk-...
# OPENAI_BASE_URL=https://api.openai.com/v1
# LLM_MODEL=gpt-4-turbo-preview
# EMBEDDING_MODEL=text-embedding-3-small
```

### Switching to OpenAI

To use OpenAI instead of local Ollama:

```bash
# Edit .env
LLM_PROVIDER=openai
EMBEDDING_PROVIDER=openai
OPENAI_API_KEY=sk-your-key-here
LLM_MODEL=gpt-4-turbo-preview
EMBEDDING_MODEL=text-embedding-3-small
EMBEDDING_DIMENSION=1536

# Restart the go-agent service
docker compose up -d go-agent
```

You can keep Ollama running or disable it:
```bash
# Stop ollama if not needed
docker compose stop ollama ollama-setup
```

## Development Setup

For local development (running Go app on host, databases in Docker):

```bash
# Start only infrastructure services
docker compose -f docker-compose.dev.yml up -d

# Run the app locally
cp .env.example .env
nano .env  # Update connection strings to localhost

# Run migrations
make build
./bin/go-agent ingest --dir ./documents

# Run in development mode
make serve
```

### Building the UI Separately

```bash
cd ui
npm install
npm run build  # Builds to ../api/ui/dist
cd ..
go run . serve
```

## Production Deployment

### 1. Optimize for Production

```yaml
# docker-compose.prod.yml
services:
  go-agent:
    deploy:
      resources:
        limits:
          cpus: '2'
          memory: 2G
        reservations:
          cpus: '1'
          memory: 512M
    restart: always
```

### 2. Use External Databases

```yaml
# Override for external databases
services:
  go-agent:
    environment:
      POSTGRES_DSN: postgresql://user:pass@prod-db.example.com:5432/go-agent
      NEO4J_URI: neo4j://prod-neo4j.example.com:7687

  # Remove local postgres and neo4j services
```

### 3. Enable HTTPS with Reverse Proxy

```bash
# Add Nginx or Traefik
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d
```

### 4. Backup Volumes

```bash
# Backup PostgreSQL
docker compose exec postgres pg_dump -U postgres go-agent > backup.sql

# Backup Neo4j
docker compose exec neo4j neo4j-admin database dump neo4j --to-path=/backups

# Backup Ollama models
docker run --rm -v go-agent-ollama-data:/data -v $(pwd):/backup \
  alpine tar czf /backup/ollama-backup.tar.gz /data
```

## Service Management

### Start/Stop Services

```bash
# Start all services
docker compose up -d

# Start specific service
docker compose up -d go-agent

# Stop all services
docker compose down

# Stop and remove volumes (⚠️ deletes data)
docker compose down -v

# Restart a service
docker compose restart go-agent
```

### View Logs

```bash
# All services
docker compose logs -f

# Specific service
docker compose logs -f go-agent

# Last 100 lines
docker compose logs --tail=100 go-agent
```

### Update and Rebuild

```bash
# Rebuild after code changes
docker compose build go-agent
docker compose up -d go-agent

# Pull latest images
docker compose pull

# Rebuild everything
docker compose build --no-cache
docker compose up -d
```

### Execute Commands

```bash
# Run ingest command
docker compose exec go-agent /app/go-agent ingest --dir /app/documents

# Access PostgreSQL
docker compose exec postgres psql -U postgres -d go-agent

# Access Neo4j shell
docker compose exec neo4j cypher-shell -u neo4j -p mysecretpassword

# Pull additional Ollama model
docker compose exec ollama ollama pull codellama:7b
```

## Troubleshooting

### Services Won't Start

```bash
# Check service status
docker compose ps

# View detailed logs
docker compose logs

# Check resource usage
docker stats

# Verify health checks
docker compose exec go-agent curl http://localhost:8080/healthz
```

### Database Connection Issues

```bash
# Verify PostgreSQL is ready
docker compose exec postgres pg_isready -U postgres

# Test Neo4j connectivity
docker compose exec neo4j cypher-shell -u neo4j -p mysecretpassword "RETURN 1;"

# Check network connectivity
docker compose exec go-agent ping postgres
docker compose exec go-agent ping neo4j
```

### Ollama Model Issues

```bash
# List downloaded models
docker compose exec ollama ollama list

# Re-download models
docker compose exec ollama ollama pull llama3.1:8b
docker compose exec ollama ollama pull nomic-embed-text

# Check Ollama logs
docker compose logs ollama
```

### Performance Issues

```bash
# Check resource usage
docker stats

# Increase database memory (edit docker-compose.yml)
# For Neo4j:
NEO4J_server_memory_heap_max__size: 4G
NEO4J_server_memory_pagecache_size: 2G

# For PostgreSQL, increase shared_buffers
# Add to postgres service:
command: postgres -c shared_buffers=256MB -c max_connections=100
```

### Clean Slate Restart

```bash
# Stop everything
docker compose down

# Remove volumes (⚠️ deletes all data)
docker compose down -v

# Remove all images
docker compose down --rmi all

# Start fresh
docker compose up -d
```

## Performance Tuning

### PostgreSQL Optimization

Add to `docker-compose.yml` under postgres service:

```yaml
command:
  - postgres
  - -c shared_buffers=256MB
  - -c effective_cache_size=1GB
  - -c maintenance_work_mem=128MB
  - -c checkpoint_completion_target=0.9
  - -c wal_buffers=16MB
  - -c default_statistics_target=100
  - -c random_page_cost=1.1
  - -c effective_io_concurrency=200
  - -c work_mem=4MB
  - -c min_wal_size=1GB
  - -c max_wal_size=4GB
```

### Neo4j Optimization

```yaml
environment:
  NEO4J_server_memory_heap_initial__size: 1G
  NEO4J_server_memory_heap_max__size: 4G
  NEO4J_server_memory_pagecache_size: 2G
  NEO4J_dbms_memory_transaction_total_max: 1G
```

### Ollama GPU Support

```yaml
services:
  ollama:
    deploy:
      resources:
        reservations:
          devices:
            - driver: nvidia
              count: 1
              capabilities: [gpu]
```

Requires NVIDIA Docker runtime:
```bash
# Install NVIDIA Container Toolkit
distribution=$(. /etc/os-release;echo $ID$VERSION_ID)
curl -s -L https://nvidia.github.io/nvidia-docker/gpgkey | sudo apt-key add -
curl -s -L https://nvidia.github.io/nvidia-docker/$distribution/nvidia-docker.list | \
  sudo tee /etc/apt/sources.list.d/nvidia-docker.list

sudo apt-get update && sudo apt-get install -y nvidia-container-toolkit
sudo systemctl restart docker
```

## Monitoring

### Health Checks

```bash
# Application health
curl http://localhost:8080/healthz

# PostgreSQL
docker compose exec postgres pg_isready

# Neo4j
curl http://localhost:7474

# Ollama
curl http://localhost:11434/api/tags
```

### Resource Monitoring

```bash
# Real-time stats
docker stats

# Disk usage
docker system df

# Volume sizes
docker volume ls -q | xargs docker volume inspect | \
  grep -E 'Name|Mountpoint' | paste - -
```

## Security Best Practices

1. **Change Default Passwords**
   ```bash
   # Generate strong passwords
   openssl rand -base64 32
   ```

2. **Use Docker Secrets** (Swarm mode)
   ```yaml
   secrets:
     postgres_password:
       external: true
   services:
     postgres:
       secrets:
         - postgres_password
   ```

3. **Restrict Network Access**
   ```yaml
   # Don't expose databases to host in production
   # Remove ports: from postgres and neo4j services
   ```

4. **Run as Non-Root**
   - The go-agent container already runs as user `goagent` (UID 1000)

5. **Keep Images Updated**
   ```bash
   docker compose pull
   docker compose up -d
   ```

## Additional Resources

- [Docker Documentation](https://docs.docker.com/)
- [Docker Compose Reference](https://docs.docker.com/compose/compose-file/)
- [pgvector Documentation](https://github.com/pgvector/pgvector)
- [Neo4j Operations Manual](https://neo4j.com/docs/operations-manual/)
- [Ollama Documentation](https://github.com/ollama/ollama)

## Support

For issues:
1. Check logs: `docker compose logs -f`
2. Verify health: `docker compose ps`
3. Review this guide's troubleshooting section
4. Open an issue on GitHub with logs and configuration
