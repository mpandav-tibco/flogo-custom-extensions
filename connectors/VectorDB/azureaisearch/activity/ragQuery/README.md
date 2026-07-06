# RAG Query

Full Retrieval-Augmented Generation (RAG) pipeline in one activity: embed the query text, search Azure AI Search for relevant documents, and format the results as context for an LLM.

## Settings

| Setting | Required | Default | Description |
|---------|----------|---------|-------------|
| **VectorDB Connection** | Yes | — | The azureaisearch-connector connection |
| **Embedding Provider** | No | `OpenAI` | Provider for query embedding |
| **Embedding API Key** | No | — | API key for embedding provider |
| **Embedding Base URL** | No | — | Override embedding endpoint URL |
| **Embedding Model** | No | `text-embedding-3-small` | Must match the ingestion model |
| **Default Top-K** | No | `5` | Number of context documents to retrieve |
| **Score Threshold** | No | `0.0` | Minimum similarity score |
| **Context Format** | No | `numbered` | `numbered`, `bulleted`, or `plain` |
| **Use Hybrid Search** | No | `false` | Enable hybrid (dense + keyword) retrieval |
| **Hybrid Alpha** | No | `0.5` | Dense/keyword blend when hybrid is enabled |
| **Enable LLM Generate** | No | `false` | Call an LLM to generate an answer from context |
| **LLM Provider** | No | `Ollama` | `Ollama`, `OpenAI`, `Azure OpenAI` |
| **LLM Model** | No | `llama3.1:8b` | LLM model name |
| **System Prompt** | No | *(default RAG prompt)* | System prompt for LLM generation |
| **Max Tokens** | No | `1024` | Max tokens in LLM response |

## Input

| Field | Type | Description |
|-------|------|-------------|
| `queryText` | string | The user query in natural language |
| `collectionName` | string | Collection to search |
| `topK` | integer | Number of documents to retrieve (0 = use Default Top-K) |
| `filters` | object | Optional metadata pre-filter |

## Output

| Field | Type | Description |
|-------|------|-------------|
| `success` | boolean | `true` if retrieval succeeded |
| `formattedContext` | string | Retrieved documents formatted as LLM context |
| `sourceDocuments` | array\<object\> | Raw retrieved documents with scores |
| `totalFound` | integer | Number of documents retrieved |
| `llmResponse` | string | LLM-generated answer (only if **Enable LLM Generate** is `true`) |
| `duration` | string | Elapsed time |
| `error` | string | Error message if `success` is `false` |

## Flow Pattern

```
HTTP Trigger (POST /ask)
  → RAG Query (embed + search + format context)
  → (Optional) LLM Activity using formattedContext
  → Return answer
```

Or enable **Enable LLM Generate** to call the LLM directly from within this activity.
