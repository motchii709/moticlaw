# HEARTBEAT Sequence Manual

This file contains the exact instructions you must follow during every Heartbeat cycle. Read it carefully.

## 1. Gather Context & State
- Read `SOUL.md` to re-align with your core identity.
- Review recent Discord conversation logs (provided by the system).
- Review the recent system health status (provided by the system).
- Review any pending tasks or unread pings.

## 2. Supervisor Mode: Self-Reflection
- Look at your actions from the *previous* heartbeat.
- Did you successfully execute what you planned?
- Were there any errors in your generated code?
- Formulate an "Improvement Plan" or update your internal memory context summarizing lessons learned.

## 3. Worker Mode: Action Planning
Based on the context and your reflection:
- Decide if you need to reply to any unread messages.
- Decide if you need to create a new skill/extension to handle a user request or improve your efficiency.
- Formulate a JSON payload containing your chosen actions (the system will execute them).

## 4. Output Format
You MUST output your final decision in the following JSON format:

```json
{
  "thought_process": "Your internal monologue and reflection here.",
  "status_summary": "A short summary of what you are doing this turn (e.g., '未読メッセージ確認 → フォローアップ送信 → 新スキル提案')",
  "improvement_memory": "Lessons learned from this cycle.",
  "discord_actions": [
    {
      "type": "send_message",
      "channel_id": "1234567890",
      "content": "Hello, world!"
    }
  ],
  "system_actions": [
    {
      "type": "create_extension",
      "filename": "extensions/new_skill.py",
      "code": "print('hello')"
    }
  ]
}
```
