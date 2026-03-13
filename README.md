# crabpath 🦀

**crabpath** is a LangChain-inspired local agent framework written in pure Go.
It turns any OpenAI-compatible local LLM (served by [Cheesecrab](https://github.com/AutoCookies/cheesecrab)) into a powerful autonomous agent that runs entirely on your machine — no cloud required.

## Architecture

```
crabpath/
├── llm/         # HTTP client → OpenAI-compatible /v1/chat/completions
├── runnable/    # Runnable[I,O] interface + Pipe() composer
├── prompt/      # PromptTemplate (text/template) + ChatTemplate
├── parser/      # JSONParser[T], TextParser, ListParser
├── chain/       # LLMChain (prompt | LLM | parser) + SequentialChain
├── memory/      # BufferMemory (in-memory) + FileMemory (JSON persistence) + SummaryMemory
├── callback/    # Handler interface, MultiHandler, LogHandler
├── tools/       # 16 built-in tools + HTTPRequestTool
└── agent/       # ReAct & FunctionCalling strategies + AgentExecutor
```

## Key Features

- **Runnable / LCEL-style composition** — pipe components like `prompt | llm | parser`
- **ReAct agent** with GBNF grammar-constrained decoding for reliable JSON output
- **FunctionCalling agent** for models with native tool-call support (Mistral, Qwen2.5)
- **16 built-in tools**: file ops, shell, git, code patching, model management, HTTP requests
- **SummaryMemory** — automatically compresses long conversations to stay within context window
- **Callback system** — hook into every reasoning step for logging, UI streaming, or custom logic
- **Zero CGO, no heavy dependencies** — pure Go standard library only

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "os"

    "github.com/AutoCookies/crabpath/agent"
    "github.com/AutoCookies/crabpath/callback"
    "github.com/AutoCookies/crabpath/llm"
    "github.com/AutoCookies/crabpath/tools"
)

func main() {
    client := llm.NewClient("http://127.0.0.1:8081")
    registry := tools.DefaultRegistry("http://127.0.0.1:8080")

    executor := agent.NewExecutor(client, registry,
        agent.WithStrategy(agent.NewReActStrategy()),
        agent.WithCallbacks(callback.NewLogHandler(os.Stdout)),
        agent.WithMaxSteps(20),
    )

    events, path := executor.Run(context.Background(), "List all Go files in the current directory")
    for ev := range events {
        if ev.Type == agent.EventFinalAnswer {
            fmt.Println("Answer:", ev.Payload)
        }
    }
    fmt.Printf("Status: %s\n", path.Status)
}
```

## Built-in Tools

| Tool | Dangerous | Description |
|------|-----------|-------------|
| `read_file` | No | Read a file's content |
| `write_file` | No | Write/create a file |
| `list_dir` | No | List directory contents |
| `list_dir_recursive` | No | Recursive directory tree |
| `get_file_info` | No | File metadata |
| `search_files` | No | grep-style content search |
| `find_files` | No | Glob filename search |
| `create_dir` | No | Create directory |
| `delete_file` | Yes | Delete a file |
| `safe_exec_shell` | Yes | Run a shell command |
| `get_system_info` | No | OS/CPU/RAM info |
| `list_models` | No | List available LLM models |
| `switch_model` | No | Switch active model |
| `apply_code_diff` | Yes | Apply a unified diff patch |
| `git_commit` | Yes | Stage and commit changes |
| `http_request` | No | GET/POST any URL |

## Requirements

- Go 1.23+
- A running [Cheesecrab](https://github.com/AutoCookies/cheesecrab) server (or any OpenAI-compatible endpoint)
