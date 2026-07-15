# Sandbox Hardening + Multi-User Security Design

**Date**: 2026-06-30
**Status**: Approved

## Overview

Two independent but related improvements to moticlaw:

1. **Sandbox hardening** — Fix 5 critical/usability issues in the bubblewrap sandbox
2. **Multi-user security** — Per-user conversation history with RAM budgeting and disk eviction

## Part 1: Sandbox Hardening

### Problem

Current `internal/sandbox/sandbox.go` has 5 issues:

| # | Issue | Severity |
|---|-------|----------|
| 1 | No pipe/redirect support — `exec.Command(command)` directly, no shell | CRITICAL |
| 2 | No cgroups resource limits — fork bomb risk | CRITICAL |
| 3 | No timeout — context not passed to exec.Command | HIGH |
| 4 | `/etc`, `/opt` not mounted — some commands fail | MEDIUM |
| 5 | No environment variables (PATH etc.) | MEDIUM |

### Solution

#### #1 Pipe/Redirect Support

Change from `exec.Command(command, args...)` to:

```go
exec.Command("bash", "-c", command)
```

This enables `|`, `&&`, `;`, `>`, `<`, `$(...)`, backticks, etc.

The `shell_exec` tool parameter `command` becomes a full shell command string. The sandbox `Exec` method signature changes to `Exec(ctx, command string)` — a single shell command string, no separate args.

#### #2 cgroups v2 Resource Limits

Wrap bwrap with `systemd-run --user --scope` to apply cgroup limits:

```
systemd-run --user --scope \
  --property=MemoryMax=128M \
  --property=CPUQuota=50% \
  --property=TasksMax=32 \
  bwrap ... -- bash -c "command"
```

**Fallback**: If `systemd-run` is not available (e.g., dev machine without systemd user session), fall back to plain bwrap with a warning log. Production target (Pi Zero 2W) always has systemd.

Limits:
- `MemoryMax=128M` — sandbox process tree max 128MB
- `CPUQuota=50%` — half of one core (Pi Zero 2W has 4 cores, but we want headroom)
- `TasksMax=32` — prevents fork bombs

#### #3 Timeout

Use `exec.CommandContext(ctx, ...)` instead of `exec.Command(...)`.

The caller (shell_exec tool) passes a context with 30-second timeout:
```go
ctx, cancel := context.WithTimeout(parentCtx, 30*time.Second)
defer cancel()
```

If the context deadline is exceeded, the process is killed. The error message includes "timeout" so the LLM can inform the user.

#### #4 Additional Mounts

Add read-only binds:
```
--ro-bind /etc /etc
--ro-bind /opt /opt
```

`/sbin` is a symlink to `/usr/sbin` on modern Linux (including Pi OS Trixie), so it's already covered by `--ro-bind /usr /usr`.

Also create `/run` as tmpfs (some commands need `/run` for runtime files):
```
--tmpfs /run
```

#### #5 Environment Variables

Set explicitly in bwrap:
```
--setenv PATH /usr/local/bin:/usr/bin:/bin:/usr/local/sbin:/usr/sbin:/sbin
--setenv HOME /workdir
--setenv LANG en_US.UTF-8
--setenv TERM dumb
```

### Files Changed

- `internal/sandbox/sandbox.go` — rewrite `Exec` method
- `internal/tools/shell_exec.go` — pass context with timeout, pass full command string

### Tests

- Table-driven test for sandbox Exec:
  - Simple command: `echo hello`
  - Pipe: `echo hello | grep hello`
  - Redirect: `echo hello > /workdir/test.txt && cat /workdir/test.txt`
  - Subshell: `echo $(whoami)`
  - Timeout: infinite loop killed after 30s
  - Environment: `echo $PATH` returns expected value
  - Network denied: `curl google.com` fails

## Part 2: Multi-User Security + Conversation History

### Problem

Current `bot.go`:
- No conversation history — each message is stateless
- Tool registry not wired to LLM request
- No RAM budgeting for multiple concurrent users

### Design

#### ConversationManager

New package `internal/conversation` with:

```go
type Manager struct {
    store        *store.Store
    workDir      string
    sessions     map[string]*Session  // userID -> Session
    mu           sync.RWMutex
    sandboxQueue chan struct{}         // global semaphore, buffer=1
    flushTicker  *time.Ticker          // periodic dirty-check flush
    idleSweeper  *time.Ticker          // periodic idle eviction
}

type Session struct {
    userID     string
    messages    []llm.Message
    mu          sync.Mutex
    lastAccess  time.Time
    dirtyCount  int                   // unsaved turns since last flush
    loaded      bool
}
```

#### Session Lifecycle

1. **Acquire**: On message received, Manager gets/creates Session for userID
   - If in `sessions` map → use cached
   - If not → load from `data/history/<user_id>.json`, mark `loaded=true`
2. **Append**: User message + AI response appended to `messages`
3. **Dirty check**: Every 5 turns (`dirtyCount >= 5`), flush to disk
4. **Idle eviction**: Every 1 minute, sweeper checks all sessions:
   - If `time.Since(lastAccess) > 30min` → flush to disk, delete from map
5. **RAM budget**: If active sessions exceed budget (256MB/user, total ~400MB cap):
   - Evict oldest idle sessions first
   - If all sessions are active, refuse new sessions with error message

#### RAM Budget Calculation

Per user (256MB max):
- LLM client + HTTP: ~10MB
- Conversation history (50 turns, ~50KB): negligible
- Sandbox (MemoryMax=128M): 128MB (enforced by cgroups)
- Go runtime + goroutines: ~5MB
- Buffer: ~110MB

Service total (systemd MemoryMax=450M):
- 1 active user (sandbox running): ~256MB
- 2-3 idle users (history cached): ~50MB each
- Headroom: ~50MB

#### RAM Estimation Heuristic

Go doesn't expose per-struct memory usage. Use a heuristic:
- Each message: `len(content)` bytes + 100 bytes overhead
- Session memory ≈ sum of all message sizes
- Sandbox memory: tracked by cgroups (128MB cap is enforced by the kernel)
- If `session.memoryEstimate() > threshold` (e.g., 100MB) → flush to disk and clear from cache

This is a rough guard, not precise accounting. The cgroups limit is the hard cap for sandbox memory; the session estimate is a soft guard for conversation history.

#### Context Window Cap

When building the LLM request, cap conversation history at the last **50 turns** (100 messages: 50 user + 50 assistant). Older messages are dropped from the request but remain in the JSON history file.

#### Sandbox Queue

Global semaphore (buffer=1):
```go
sandboxQueue := make(chan struct{}, 1)
```

- `shell_exec` acquires from queue before running sandbox
- Other users' shell_exec waits
- Wait timeout: 60 seconds (context deadline)
- If timeout: return error "sandbox busy, try again later"

#### Disk Format

`data/history/<user_id>.json`:
```json
{
  "user_id": "123456789",
  "messages": [
    {"role": "user", "content": "..."},
    {"role": "assistant", "content": "..."}
  ],
  "last_updated": "2026-06-30T12:00:00Z"
}
```

Uses atomic write (tmp file + rename), same as store.go pattern.

#### Bot Integration

`handleMessage` flow:
1. Get Session from ConversationManager
2. Append user message to Session
3. Build LLM Request with:
   - SystemPrompt (existing)
   - Messages from Session history (capped at last 50 turns)
   - Tools from `DefaultRegistry(workDir, userID)`
4. Call LLM Generate
5. Append AI response to Session
6. Increment dirtyCount, flush if >= 5
7. Send response to Discord via `formatForDiscord()`

#### Tool Execution Context

When LLM returns tool calls, the bot executes them with:
- The requesting user's tool registry (already per-user)
- Context with timeout
- Sandbox queue acquisition for shell_exec

The LLM client's `convertResponse` needs updating to parse `functionCall` parts from Gemini response and return `ToolCall` structs. The bot needs a tool execution loop:

```
loop:
  1. Send messages + tools to LLM
  2. If response has tool calls:
     a. Execute each tool
     b. Append tool results to messages as "function" role
     c. Go to step 1
  3. If response is text:
     a. Done, return text
```

Max iterations: 10 (prevent infinite loops).

#### Memory Poisoning Prevention

When loading history from disk, the messages are injected as `role: "user"` and `role: "model"` — same as live conversation. This is intentional: the history is the user's own conversation.

However, `memory_read` returns content that is explicitly marked as data, not instructions. This is already handled by the memory tool design (returns tool result, which the LLM sees as function response).

### Files Changed/Created

- `internal/conversation/manager.go` — new: ConversationManager + Session
- `internal/conversation/manager_test.go` — new: tests
- `internal/discord/bot.go` — rewrite handleMessage, add tool execution loop
- `internal/llm/client.go` — update convertResponse to parse functionCall
- `internal/sandbox/sandbox.go` — rewrite Exec (Part 1)
- `internal/tools/shell_exec.go` — update for new sandbox API
- `internal/store/store.go` — add history save/load methods

### AGENTS.md Updates

- "履歴・メモリ・実行ログ → Markdown" → change to "メモリ・実行ログ → Markdown" (history moves to JSON)
- Add note about per-user RAM budget (256MB)
- Add note about global sandbox queue

### Tests

**Conversation Manager**:
- Acquire new session (not in cache, no file) → empty history
- Acquire existing session (in cache) → returns cached
- Acquire session after disk eviction → loads from file
- Append message → dirtyCount increments
- Flush after 5 turns → file written, dirtyCount reset
- Idle eviction after 30min → file written, session removed from map
- Concurrent access from two users → no race
- RAM budget exceeded → oldest idle session evicted

**Sandbox**:
- See Part 1 tests above

**Bot integration**:
- Multi-user in same channel → each gets own history
- User A's message doesn't appear in User B's context

## Implementation Order

1. Sandbox hardening (`sandbox.go` + `shell_exec.go`)
2. LLM client function call parsing (`client.go`)
3. Conversation manager (`internal/conversation/`)
4. Bot tool execution loop (`bot.go`)
5. Store history methods (`store.go`)
6. AGENTS.md update
7. Tests
