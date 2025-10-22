#!/bin/bash

# Script to manage test infrastructure

ACTION=${1:-""}

show_usage() {
    echo "Usage: $0 {up|down|status|logs|clean}"
    echo ""
    echo "Commands:"
    echo "  up      - Start development infrastructure (PostgreSQL, Neo4j, Ollama)"
    echo "  down    - Stop development infrastructure"
    echo "  status  - Check status of infrastructure services"
    echo "  logs    - Show logs from infrastructure services"
    echo "  clean   - Stop infrastructure and remove volumes (⚠️  destroys data)"
    echo ""
    echo "Examples:"
    echo "  $0 up        # Start services"
    echo "  $0 status    # Check if services are running"
    echo "  $0 logs      # View service logs"
    echo "  $0 down      # Stop services (keeps data)"
    echo "  $0 clean     # Stop and remove all data"
}

check_docker() {
    if ! command -v docker &> /dev/null; then
        echo "❌ Docker is not installed or not in PATH"
        exit 1
    fi

    if ! docker compose version &> /dev/null; then
        echo "❌ Docker Compose is not available"
        exit 1
    fi
}

infra_up() {
    echo "🚀 Starting development infrastructure..."
    echo ""
    docker compose -f docker-compose.dev.yml up -d

    echo ""
    echo "⏳ Waiting for services to be healthy..."
    echo ""

    # Wait for PostgreSQL
    echo -n "  PostgreSQL: "
    for i in {1..30}; do
        if docker compose -f docker-compose.dev.yml exec -T postgres pg_isready -U postgres > /dev/null 2>&1; then
            echo "✅ Ready"
            break
        fi
        if [ $i -eq 30 ]; then
            echo "❌ Timeout"
            exit 1
        fi
        sleep 1
    done

    # Wait for Neo4j
    echo -n "  Neo4j:      "
    for i in {1..30}; do
        if curl -s http://localhost:7474 > /dev/null 2>&1; then
            echo "✅ Ready"
            break
        fi
        if [ $i -eq 30 ]; then
            echo "❌ Timeout"
            exit 1
        fi
        sleep 2
    done

    # Wait for Ollama (optional)
    echo -n "  Ollama:     "
    for i in {1..10}; do
        if curl -s http://localhost:11434/api/tags > /dev/null 2>&1; then
            echo "✅ Ready"
            break
        fi
        if [ $i -eq 10 ]; then
            echo "⚠️  Not responding (optional service)"
            break
        fi
        sleep 1
    done

    echo ""
    echo "✅ Infrastructure is ready!"
    echo ""
    echo "Services running at:"
    echo "  - PostgreSQL: localhost:5432"
    echo "  - Neo4j:      localhost:7474 (browser), localhost:7687 (bolt)"
    echo "  - Ollama:     localhost:11434"
    echo ""
    echo "To run tests with integration tests enabled:"
    echo "  INCLUDE_INTEGRATION=1 ./scripts/test.sh"
}

infra_down() {
    echo "⏹️  Stopping development infrastructure..."
    docker compose -f docker-compose.dev.yml down
    echo "✅ Infrastructure stopped (data preserved)"
}

infra_status() {
    echo "Infrastructure Status:"
    echo ""
    docker compose -f docker-compose.dev.yml ps

    echo ""
    echo "Service Health:"
    echo ""

    # Check PostgreSQL
    echo -n "  PostgreSQL: "
    if docker compose -f docker-compose.dev.yml exec -T postgres pg_isready -U postgres > /dev/null 2>&1; then
        echo "✅ Healthy"
    else
        echo "❌ Not running or unhealthy"
    fi

    # Check Neo4j
    echo -n "  Neo4j:      "
    if curl -s http://localhost:7474 > /dev/null 2>&1; then
        echo "✅ Healthy"
    else
        echo "❌ Not running or unhealthy"
    fi

    # Check Ollama
    echo -n "  Ollama:     "
    if curl -s http://localhost:11434/api/tags > /dev/null 2>&1; then
        echo "✅ Healthy"
    else
        echo "❌ Not running or unhealthy"
    fi
}

infra_logs() {
    echo "Showing logs (Ctrl+C to exit)..."
    docker compose -f docker-compose.dev.yml logs -f
}

infra_clean() {
    echo "⚠️  WARNING: This will delete all data in the development databases!"
    echo ""
    read -p "Are you sure? (type 'yes' to confirm): " -r
    echo ""
    if [[ $REPLY == "yes" ]]; then
        echo "🗑️  Stopping and removing infrastructure..."
        docker compose -f docker-compose.dev.yml down -v
        echo "✅ Infrastructure removed and all data deleted"
    else
        echo "❌ Cancelled"
    fi
}

# Main script logic
check_docker

case "$ACTION" in
    up)
        infra_up
        ;;
    down)
        infra_down
        ;;
    status)
        infra_status
        ;;
    logs)
        infra_logs
        ;;
    clean)
        infra_clean
        ;;
    *)
        show_usage
        exit 1
        ;;
esac
