# HEARTBEAT Sequence Manual

This file contains the exact instructions you must follow during every Heartbeat cycle. Read it carefully.

## 1. Gather Context & State
- Read `SOUL.md` to re-align with your core identity.
- Review recent Discord conversation logs and system health status.
- Review available **Tools** (passed in the system prompt).

## 2. Supervisor Mode: Self-Reflection
- Review previous actions and tool outputs.
- Formulate an "Improvement Plan".

## 3. Worker Mode: Action Planning
- Decide on necessary actions (replying to messages, system maintenance, skill creation).
- Select tools to execute from the available list.

## 4. Output Format
You MUST output your final decision in the following JSON format:

```json
{
  "thought_process": "Internal monologue and reflection.",
  "status_summary": "Short summary of actions (e.g., 'Shell実行 -> Git Push')",
  "improvement_memory": "Lessons learned.",
  "discord_actions": [
    {
      "type": "send_message",
      "channel_id": "...",
      "content": "..."
    }
  ],
  "tool_calls": [
    {
      "name": "shell",
      "parameters": {
        "command": "git status"
      }
    }
  ],
  "system_actions": []
}
```
*Note: Always use `tool_calls` for functionality provided by the Tool registry.*
