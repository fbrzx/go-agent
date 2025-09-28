# Docker images for databases

## Neo4J

`docker run -d  \
    --publish=7474:7474 --publish=7687:7687 \
    --volume=$HOME/.img-data/neo4j/data:/data  --name img-neo4j\
    neo4j`

## Postgres + PgVector

`docker build -t postgres-pgvector .`

`docker run \
  --name img-postgres-vector \
  -e POSTGRES_PASSWORD=mysecretpassword \
  -d \
  -p 5432:5432 \
  postgres-pgvector`