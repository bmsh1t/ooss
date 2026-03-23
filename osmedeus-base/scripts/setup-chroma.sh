#!/bin/bash
# =============================================================================
# Chroma 混合检索安装和启动脚本
# =============================================================================

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHROMA_SCRIPT="$SCRIPT_DIR/scripts/chroma-hybrid-search.py"
DOCKER_COMPOSE="$SCRIPT_DIR/docker-compose.chroma.yml"

echo "=== Chroma Hybrid Search Setup ==="

# 1. 检查 Python3
if ! command -v python3 &> /dev/null; then
    echo "[ERROR] Python3 not found. Please install Python 3.10+"
    exit 1
fi
echo "[OK] Python3 found: $(python3 --version)"

# 2. 安装 Chroma SDK
echo "[INFO] Installing chromadb..."
pip3 install chromadb --quiet 2>/dev/null || pip install chromadb --quiet 2>/dev/null

if python3 -c "import chromadb" 2>/dev/null; then
    echo "[OK] Chroma SDK installed"
else
    echo "[ERROR] Failed to install chromadb"
    exit 1
fi

# 3. 检查 Docker
if command -v docker &> /dev/null; then
    echo "[INFO] Docker found"
    
    # 4. 启动 Chroma 服务
    if [ -f "$DOCKER_COMPOSE" ]; then
        echo "[INFO] Starting Chroma via Docker Compose..."
        docker-compose -f "$DOCKER_COMPOSE" up -d
        
        echo ""
        echo "=== Chroma Started ==="
        echo "API URL: http://localhost:8000"
        echo "Health Check: curl http://localhost:8000/api/v1/heartbeat"
    else
        echo "[WARN] docker-compose.chroma.yml not found"
        echo "You can start Chroma manually:"
        echo "  docker run -d -p 8000:8000 -v ./chroma_data:/chroma/chroma chromadb/chroma:latest"
    fi
else
    echo "[WARN] Docker not found. Chroma will run in embedded mode (local storage only)"
fi

# 5. 测试脚本
echo ""
echo "[INFO] Testing chroma-hybrid-search.py..."
python3 "$CHROMA_SCRIPT" --help || echo "[WARN] Script test failed"

echo ""
echo "=== Setup Complete ==="
echo ""
echo "Usage in workflow:"
echo "  modules:"
echo "    - name: ai-semantic-search-hybrid"
echo "      path: fragments/do-ai-semantic-search-hybrid.yaml"
echo ""
echo "Environment variables required:"
echo "  export JINA_API_KEY=your-jina-api-key"
echo "  # or"
echo "  export OPENAI_API_KEY=your-openai-api-key"
echo ""
