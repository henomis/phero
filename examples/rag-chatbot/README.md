# rag-chatbot example

A tiny terminal chatbot that loads a local text file, splits it into chunks, indexes it in **Qdrant**, and answers questions using an agent with a RAG tool.

This example uses Qdrant over **gRPC**.

## Start Qdrant

If you don't already have Qdrant running, one quick way is Docker:

```bash
docker run --rm -p 6333:6333 -p 6334:6334 qdrant/qdrant
```

## Configure the LLM + embeddings

The example uses OpenAI-compatible endpoints via environment variables:

- `OPENAI_API_KEY` (optional for local OpenAI-compatible servers)
- `OPENAI_BASE_URL` (optional; if unset and no key is provided, it defaults to an Ollama-compatible base URL)
- `OPENAI_MODEL` (optional)

### Option A: Local Ollama (OpenAI-compatible)

```bash
export OPENAI_BASE_URL=http://localhost:11434/v1
export OPENAI_MODEL=gpt-oss:20b-cloud
# OPENAI_API_KEY can be empty for Ollama
```

### Option B: OpenAI

```bash
export OPENAI_API_KEY=...your_key...
export OPENAI_MODEL=gpt-4o-mini

# optional
# export OPENAI_BASE_URL=https://api.openai.com/v1
```

## Run

From the repo root:

```bash
go run ./examples/rag-chatbot -file /path/to/your/file.txt
```

Type your questions and press Enter. Type `exit` to quit.

### Useful flags

- `-chunk-size` / `-chunk-overlap`: controls the splitter
- `-topk`: how many chunks to retrieve per tool call
- `-qdrant-host` / `-qdrant-port`: Qdrant connection (defaults to `localhost:6334`)
- `-qdrant-collection`: Qdrant collection name (defaults to `rag_chatbot`)
- `-qdrant-api-key`: optional API key
- `-qdrant-tls`: enable TLS
