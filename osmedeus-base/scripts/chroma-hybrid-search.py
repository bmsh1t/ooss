#!/usr/bin/env python3
"""
Chroma 混合检索脚本
支持向量搜索 + BM25 关键词搜索 + RRF 融合排名
"""

import json
import sys
import os
import argparse
from pathlib import Path

try:
    import chromadb
    from chromadb.config import Settings
except ImportError:
    print(json.dumps({"error": "chromadb not installed. Run: pip install chromadb"}))
    sys.exit(1)


def parse_args():
    parser = argparse.ArgumentParser(description="Chroma Hybrid Search")
    parser.add_argument("--action", required=True, choices=["create", "search", "reset"])
    parser.add_argument("--collection", required=True, help="Collection name")
    parser.add_argument("--chroma-path", default="./chroma_data", help="Chroma data path")
    parser.add_argument("--persist", action="store_true", help="Enable persistence")
    
    # For create action
    parser.add_argument("--input-file", help="Input file with content to index")
    parser.add_argument("--batch-size", type=int, default=100, help="Batch size for indexing")
    
    # For search action
    parser.add_argument("--query", help="Search query")
    parser.add_argument("--query-embedding", help="Pre-computed query embedding (JSON)")
    parser.add_argument("--n-results", type=int, default=10, help="Number of results")
    parser.add_argument("--alpha", type=float, default=0.7, help="RRF weight: alpha for vector, (1-alpha) for BM25")
    parser.add_argument("--min-score", type=float, default=0.0, help="Minimum score threshold")
    parser.add_argument("--output", help="Output file for results")
    
    # Embeddings
    parser.add_argument("--embeddings-api", default="jina", choices=["jina", "openai", "ollama"])
    parser.add_argument("--embeddings-model", default="jina-embeddings-v5-small-eu")
    parser.add_argument("--embeddings-api-key", help="API key for embeddings")
    parser.add_argument("--embeddings-url", default="https://api.jina.ai/v1/embeddings")
    
    return parser.parse_args()


def get_embedding(text, args):
    """Get embedding for text using specified API"""
    import urllib.request
    import urllib.error
    
    if not args.embeddings_api_key:
        return None
    
    payload = {
        "model": args.embeddings_model,
        "input": [text]
    }
    
    req = urllib.request.Request(
        args.embeddings_url,
        data=json.dumps(payload).encode("utf-8"),
        headers={
            "Content-Type": "application/json",
            "Authorization": f"Bearer {args.embeddings_api_key}"
        },
        method="POST"
    )
    
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            result = json.loads(resp.read().decode("utf-8"))
            return result["data"][0]["embedding"]
    except Exception as e:
        print(f"Embedding error: {e}", file=sys.stderr)
        return None


def get_embeddings_batch(texts, args):
    """Get embeddings for multiple texts"""
    import urllib.request
    
    if not args.embeddings_api_key:
        return None
    
    payload = {
        "model": args.embeddings_model,
        "input": texts
    }
    
    req = urllib.request.Request(
        args.embeddings_url,
        data=json.dumps(payload).encode("utf-8"),
        headers={
            "Content-Type": "application/json",
            "Authorization": f"Bearer {args.embeddings_api_key}"
        },
        method="POST"
    )
    
    try:
        with urllib.request.urlopen(req, timeout=60) as resp:
            result = json.loads(resp.read().decode("utf-8"))
            return [item["embedding"] for item in sorted(result["data"], key=lambda x: x["index"])]
    except Exception as e:
        print(f"Batch embedding error: {e}", file=sys.stderr)
        return None


def bm25_score(query, document, k1=1.5, b=0.75):
    """Simple BM25 implementation"""
    import math
    
    def tokenize(text):
        return text.lower().split()
    
    def term_freq(term, doc_tokens):
        return doc_tokens.count(term) / len(doc_tokens) if doc_tokens else 0
    
    def avg_doc_len(docs):
        return sum(len(d) for d in docs) / len(docs) if docs else 1
    
    query_tokens = tokenize(query)
    doc_tokens = tokenize(document)
    
    if not query_tokens or not doc_tokens:
        return 0.0
    
    doc_len = len(doc_tokens)
    avg_len = doc_len  # Simplified
    score = 0.0
    
    for term in query_tokens:
        tf = term_freq(term, doc_tokens)
        if tf > 0:
            # IDF (simplified - use constant)
            idf = math.log((len([1]) + 0.5) / (0.5 + 0.0001))
            score += idf * (tf * (k1 + 1)) / (tf + k1 * (1 - b + b * doc_len / avg_len))
    
    return score


def rrf_fusion(vector_results, bm25_results, k=60, alpha=0.7):
    """Reciprocal Rank Fusion for combining results"""
    scores = {}
    
    # Vector search scores (normalized)
    for i, (doc_id, metadata, vector_score) in enumerate(vector_results):
        rrf_score = 1.0 / (k + i + 1)
        scores[doc_id] = scores.get(doc_id, 0) + alpha * rrf_score
        if doc_id not in [r[0] for r in vector_results[:i]]:
            scores[doc_id + "_vec"] = vector_score
            scores[doc_id + "_meta"] = metadata
    
    # BM25 scores (normalized)
    max_bm25 = max((s for _, s in bm25_results), default=1.0)
    for i, (doc_id, bm25_score_val) in enumerate(bm25_results):
        rrf_score = 1.0 / (k + i + 1)
        scores[doc_id] = scores.get(doc_id, 0) + (1 - alpha) * rrf_score
        scores[doc_id + "_bm25"] = bm25_score_val / max_bm25 if max_bm25 > 0 else 0
    
    return scores


def create_collection(args):
    """Create or reset a collection"""
    client = chromadb.PersistentClient(path=args.chroma_path) if args.persist else chromadb.Client()
    
    if args.action == "reset":
        try:
            client.delete_collection(args.collection)
            print(json.dumps({"status": "deleted", "collection": args.collection}))
        except:
            pass
    
    try:
        collection = client.get_or_create_collection(args.collection)
        
        if args.input_file and os.path.exists(args.input_file):
            # Read and index content
            with open(args.input_file, "r", encoding="utf-8", errors="ignore") as f:
                content = f.read()
            
            # Split into chunks (by lines or paragraphs)
            lines = [l.strip() for l in content.split("\n") if l.strip()]
            
            if not lines:
                print(json.dumps({"error": "No content to index"}))
                return
            
            # Get embeddings in batch
            embeddings = get_embeddings_batch(lines[:500], args)  # Limit to 500 lines
            
            if embeddings:
                ids = [f"doc_{i}" for i in range(len(lines[:500]))]
                collection.add(
                    ids=ids,
                    documents=lines[:500],
                    embeddings=embeddings
                )
                print(json.dumps({
                    "status": "created",
                    "collection": args.collection,
                    "indexed": len(lines[:500])
                }))
            else:
                # Fallback: add without embeddings (will use query as raw)
                ids = [f"doc_{i}" for i in range(len(lines))]
                collection.add(
                    ids=ids,
                    documents=lines
                )
                print(json.dumps({
                    "status": "created_no_embeddings",
                    "collection": args.collection,
                    "indexed": len(lines)
                }))
        else:
            print(json.dumps({"status": "ready", "collection": args.collection}))
            
    except Exception as e:
        print(json.dumps({"error": str(e)}))


def search_collection(args):
    """Search the collection with hybrid search"""
    client = chromadb.PersistentClient(path=args.chroma_path) if args.persist else chromadb.Client()
    
    try:
        collection = client.get_or_create_collection(args.collection)
    except Exception as e:
        print(json.dumps({"error": f"Collection not found: {str(e)}"}))
        return
    
    results = {
        "query": args.query,
        "collection": args.collection,
        "alpha": args.alpha,
        "results": []
    }
    
    # Get query embedding
    query_embedding = None
    if args.query_embedding:
        try:
            query_embedding = json.loads(args.query_embedding)["embedding"]
        except:
            pass
    
    if not query_embedding and args.query:
        query_embedding = get_embedding(args.query, args)
    
    # Vector search
    vector_results = []
    if query_embedding:
        try:
            vector_results_raw = collection.query(
                query_embeddings=[query_embedding],
                n_results=args.n_results
            )
            
            if vector_results_raw and "documents" in vector_results_raw:
                for i, doc in enumerate(vector_results_raw["documents"][0]):
                    doc_id = vector_results_raw["ids"][0][i] if vector_results_raw["ids"] else f"vec_{i}"
                    distance = vector_results_raw["distances"][0][i] if vector_results_raw.get("distances") else 0
                    # Convert distance to similarity score
                    similarity = 1.0 - distance if distance else 0.5
                    metadata = vector_results_raw.get("metadatas", [[{}]])[0][i] if vector_results_raw.get("metadatas") else {}
                    vector_results.append((doc_id, metadata, similarity))
        except Exception as e:
            print(f"Vector search error: {e}", file=sys.stderr)
    
    # BM25 search
    bm25_results = []
    if args.query:
        try:
            all_docs = collection.get(include=["documents"])
            if all_docs and "documents" in all_docs:
                docs = all_docs["documents"]
                for i, doc in enumerate(docs):
                    score = bm25_score(args.query, doc)
                    if score > 0:
                        bm25_results.append((f"bm25_{i}", score))
                # Sort by score
                bm25_results.sort(key=lambda x: x[1], reverse=True)
                bm25_results = bm25_results[:args.n_results]
        except Exception as e:
            print(f"BM25 search error: {e}", file=sys.stderr)
    
    # RRF Fusion
    if vector_results or bm25_results:
        fused = rrf_fusion(vector_results, bm25_results, alpha=args.alpha)
        
        # Sort by fused score
        sorted_results = sorted(fused.items(), key=lambda x: x[1], reverse=True)
        
        # Build final results
        seen = set()
        for key, score in sorted_results:
            if key.startswith("doc_") or key.startswith("bm25_"):
                doc_id = key.replace("_vec", "").replace("_bm25", "")
                if doc_id not in seen:
                    seen.add(doc_id)
                    
                    # Get document content
                    try:
                        doc_data = collection.get(ids=[doc_id])
                        if doc_data and "documents" in doc_data:
                            doc_content = doc_data["documents"][0] if doc_data["documents"] else ""
                        else:
                            doc_content = ""
                    except:
                        doc_content = ""
                    
                    vec_score = fused.get(doc_id + "_vec", 0)
                    bm25_score_val = fused.get(doc_id + "_bm25", 0)
                    
                    # Filter by min score
                    if score >= args.min_score:
                        results["results"].append({
                            "id": doc_id,
                            "content": doc_content[:500],  # Truncate
                            "score": round(score, 4),
                            "vector_score": round(vec_score, 4),
                            "bm25_score": round(bm25_score_val, 4)
                        })
        
        results["results"] = results["results"][:args.n_results]
        results["total"] = len(results["results"])
        results["status"] = "success"
    else:
        results["status"] = "no_results"
        results["message"] = "No query or embedding provided"
    
    # Output
    if args.output:
        with open(args.output, "w", encoding="utf-8") as f:
            json.dump(results, f, ensure_ascii=False, indent=2)
        print(json.dumps({"status": "saved", "output": args.output, "count": results["total"]}))
    else:
        print(json.dumps(results, ensure_ascii=False))


def main():
    args = parse_args()
    
    if args.action == "create":
        create_collection(args)
    elif args.action == "search":
        search_collection(args)
    elif args.action == "reset":
        args.action = "reset"
        create_collection(args)


if __name__ == "__main__":
    main()
