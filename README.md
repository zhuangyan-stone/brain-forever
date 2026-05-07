# BrainForever

> A loyal historian sits between you and the AI, quietly getting to know you — your personality, habits, interests, profession, thinking style, cultural taste, and even your food preferences — so the AI can serve you better over time.

## Why I Built This

I started this project because I miss my father, who passed away over a decade ago. I can no longer talk to him. But I realized — one day, I too will be gone. And when that day comes, I want my children to be able to connect to a server, whenever they miss me, and have a conversation with an AI agent that carries the memories of half my lifetime.

This is BrainForever.

---

BrainForever is an AI chat companion that remembers who you are. Unlike ordinary chatbots that treat every conversation as a fresh start, BrainForever places a discreet "historian" between you and the LLM. As you chat naturally, this historian silently observes and builds a multi-dimensional profile of your character — your communication style, your values, your sense of humor, your expertise, your aesthetic preferences, and more. The more you talk, the better it understands you, and the more personalized and thoughtful the AI's responses become.

## Why BrainForever?

Most AI chats are **memoryless** — each session starts from scratch, and the AI has no idea who you are or what you care about. BrainForever changes that.

- **It learns you, not just your words.** It picks up on your personality traits, your decision-making patterns, your cultural references, and even your taste in food. Over time, it builds a **personal trait library** that captures who you truly are.
- **It gets better over time.** The more conversations you have, the richer your personal profile becomes, and the more the AI's responses feel like they're coming from someone who truly knows you.
- **It's subtle and natural.** You don't need to fill out forms or answer questionnaires. Just talk, and the historian does the rest.
- **Your data stays yours.** Everything is stored locally — no cloud, no surveillance, no third-party profiling.

## Features

- **Personalized AI Conversations** — The AI adapts to your unique personality, communication style, and preferences as it gets to know you through natural conversation.
- **Streaming Responses** — Real-time token-by-token streaming via Server-Sent Events (SSE) for a smooth, natural chat experience.
- **Web Search (Optional)** — When you need fresh information, BrainForever can search the web to supplement its knowledge.
- **Session Management** — Your conversations are organized by session, with automatic cleanup of idle sessions.
- **Dark/Light Theme** — Switch between themes for comfortable reading day or night.
- **Message Management** — Delete individual messages or entire conversation turns as needed.
- **Graceful Shutdown** — Cleanly shuts down, preserving your data.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Frontend (HTML/CSS/JS)                    │
│  ┌───────────────────────────────────────────────────────┐  │
│  │              Chat Interface (SSE stream)               │  │
│  └──────────────────────┬────────────────────────────────┘  │
│                         │ POST /api/chat                     │
│                         │ ← SSE (text/event-stream)          │
└─────────────────────────┼───────────────────────────────────┘
                          │
┌─────────────────────────▼───────────────────────────────────┐
│                   BrainForever Server (Go)                    │
│                                                               │
│  ┌───────────────────────────────────────────────────────┐   │
│  │  ChatHandler                                           │   │
│  │  ① Receives your message                               │   │
│  │  ② The historian reviews your profile & conversation   │   │
│  │  ③ (Optional) Searches the web for fresh info          │   │
│  │  ④ Crafts a personalized prompt → calls the AI         │   │
│  │  ⑤ Streams the AI's response back to you               │   │
│  └───────────────────────────────────────────────────────┘   │
│                                                               │
│  ┌───────────────────────────────────────────────────────┐   │
│  │  Your Personal Profile (local storage)                 │   │
│  │  - Conversation history                                │   │
│  │  - Personal trait library (evolving over time)         │   │
│  └───────────────────────────────────────────────────────┘   │
│                                                               │
│  ┌───────────────────────────────────────────────────────┐   │
│  │  AI Client (DeepSeek / OpenAI-compatible)              │   │
│  │  - Streaming chat completions                          │   │
│  │  - Token usage tracking                                │   │
│  └───────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
```

## Prerequisites

- Go 1.25.1 or later
- CGO enabled
- GCC (e.g., MinGW on Windows, or gcc on Linux/macOS)
- API keys for the services you intend to use

## Quick Start

### 1. Clone the repository

```bash
git clone https://github.com/yourusername/BrainForever.git
cd BrainForever
```

### 2. Set up environment variables

| Variable | Required | Description |
|---|---|---|
| `DASHSCOPE_API_KEY` | Yes (default) | API key for text embedding |
| `ZHIPUAI_API_KEY` | Alternative | Alternative embedding provider (set `EMBEDDER_PROVIDER=zhipu`) |
| `DEEPSEEK_API_KEY` | Yes | API key for the AI chat model |
| `BOCHA_API_KEY` | No | API key for optional web search |
| `PROXY_ADDR` | No | Server address (default: `:8080`) |
| `EMBEDDER_PROVIDER` | No | Embedding provider: `ali` (default) or `zhipu` |

### 3. Build and run

**Windows:**

```batch
b.bat
brain.exe
```

**Linux/macOS:**

```bash
CGO_ENABLED=1 go build -o brain .
./brain
```

### 4. Open the frontend

Navigate to [http://localhost:8080](http://localhost:8080) in your browser.

## API Endpoints

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/chat` | Send a message and receive a streaming AI response |
| `GET` | `/api/session` | Restore your current conversation history |
| `POST` | `/api/history` | Delete a message and its associated AI reply |
| `GET` | `/api/health` | Health check |

## Project Structure

```
BrainForever/
├── main.go                          # Entry point, HTTP server setup
├── b.bat                            # Windows build script
├── go.mod / go.sum                  # Go module dependencies
├── frontend/
│   ├── index.html                   # Chat UI
│   └── static/                      # Frontend assets (CSS, JS, images)
├── internal/
│   ├── agent/
│   │   ├── chat.go                  # Chat handler (core logic)
│   │   └── typedef.go               # Type definitions & session management
│   └── store/
│       ├── vector.go                # Knowledge store
│       ├── users.go                 # User management
│       └── roles.go                 # Role management
├── infra/
│   ├── 3rdapi/
│   │   ├── embedder/                # Text embedding providers
│   │   ├── llm/                     # AI chat client
│   │   └── search/                  # Web search client
│   ├── httpx/                       # HTTP client with DNS fallback
│   └── sse/                         # SSE encoder/decoder
└── toolset/
    └── rune_tl.go                   # Text processing utilities
```

## License

BrainForever is **dual-licensed** under the following terms:

- **Open Source**: Licensed under the [GNU Affero General Public License v3.0 (AGPL v3)](LICENSE) — for personal, non-commercial, and open-source use.
- **Commercial**: A commercial license is available for organizations that wish to use BrainForever in proprietary, closed-source environments without the obligations of the AGPL v3. See [COMMERCIAL-LICENSE.md](COMMERCIAL-LICENSE.md) for details.
