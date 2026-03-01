import asyncio
import os
import json
import logging
import aiofiles
import datetime
import importlib
from model_router import chat_completion
from tools.registry import registry

logger = logging.getLogger(__name__)

# Initialize tools on module load
registry.load_tools("tools")

async def read_file(path: str) -> str:
    try:
        async with aiofiles.open(path, 'r', encoding='utf-8') as f:
            return await f.read()
    except Exception as e:
        logger.error(f"Failed to read {path}: {e}")
        return ""

async def write_file(path: str, content: str):
    try:
        os.makedirs(os.path.dirname(path), exist_ok=True)
        async with aiofiles.open(path, 'w', encoding='utf-8') as f:
            await f.write(content)
    except Exception as e:
        logger.error(f"Failed to write {path}: {e}")

async def execute_heartbeat(bot, admin_channel_id: int, next_interval_minutes: int):
    """Executes the core cognitive heartbeat cycle with Tool Support."""
    logger.info("Executing Heartbeat Cycle...")
    
    soul_content = await read_file("SOUL.md")
    heartbeat_content = await read_file("HEARTBEAT.md")
    
    # Gather tools metadata for the AI
    tools_metadata = registry.get_all_tools_metadata()
    
    recent_context = "No new unread messages in the last cycle."
    
    system_prompt = f"""
You are Moticlaw. This is your scheduled Heartbeat.
Follow the architecture defined in your files.

--- SOUL.md ---
{soul_content}

--- HEARTBEAT.md ---
{heartbeat_content}

--- Available Tools ---
{json.dumps(tools_metadata, indent=2)}

--- Current Context ---
Time: {datetime.datetime.now().isoformat()}
Recent Activity: {recent_context}
"""

    messages = [
        {"role": "system", "content": system_prompt},
        {"role": "user", "content": "Execute your heartbeat cycle and return the required JSON payload with tool_calls for any actions."}
    ]
    
    try:
        response_text = await chat_completion(messages)
        
        if "```json" in response_text:
            json_str = response_text.split("```json")[1].split("```")[0].strip()
        else:
            json_str = response_text.strip()
            
        decision = json.loads(json_str)
        
        status_summary = decision.get("status_summary", "思考完了")
        improvement_memory = decision.get("improvement_memory", "特になし")
        
        # Unified Tool Execution
        tool_calls = decision.get("tool_calls", [])
        tool_results = []
        
        for call in tool_calls:
            tool_name = call.get("name")
            params = call.get("parameters", {})
            
            tool = registry.get_tool(tool_name)
            if tool:
                logger.info(f"Executing tool '{tool_name}' with params {params}")
                result = await tool.execute(**params)
                tool_results.append({"tool": tool_name, "result": result})
            else:
                logger.error(f"Tool '{tool_name}' not found.")
                tool_results.append({"tool": tool_name, "result": "Error: Tool not found"})

        # (Special handling for extension creation if not moved to a tool yet)
        sys_actions = decision.get("system_actions", [])
        for action in sys_actions:
            if action.get("type") == "create_extension":
                filename = action.get("filename")
                code = action.get("code")
                if filename and code:
                    if not filename.startswith("extensions/"):
                        filename = f"extensions/{filename}"
                    if not filename.endswith(".py"):
                        filename += ".py"
                    await write_file(filename, code)
                    logger.info(f"Created new extension: {filename}")
                    try:
                        module_name = filename.replace("/", ".").replace(".py", "")
                        if module_name in bot.extensions:
                            await bot.reload_extension(module_name)
                        else:
                            await bot.load_extension(module_name)
                    except Exception as e:
                        logger.error(f"Failed to load extension {module_name}: {e}")

        # Post the heartbeat report to Discord
        if admin_channel_id:
            channel = bot.get_channel(admin_channel_id)
            if channel:
                import database
                models = await database.get_active_models()
                total = len(models)
                healthy = sum(1 for m in models if m['score'] == 100)
                
                tool_summary = f"\nツール実行: {len(tool_results)}件" if tool_results else ""
                report = f"""🫀 [HEARTBEAT {datetime.datetime.now().strftime('%Y-%m-%d %H:%M')}] moticlaw is THINKING ✅
健康モデル: {healthy}/{total}
今回のターン: {status_summary}{tool_summary}
改善メモリ更新: 1件 ({improvement_memory[:20]}...)
次Heartbeat: {next_interval_minutes}分後
"""
                await channel.send(report)
                
        # Execute discord actions
        discord_actions = decision.get("discord_actions", [])
        for action in discord_actions:
            if action.get("type") == "send_message":
                cid = action.get("channel_id")
                content = action.get("content")
                if cid and content:
                    ch = bot.get_channel(int(cid))
                    if ch:
                        await ch.send(content)

    except Exception as e:
        logger.error(f"Heartbeat execution failed: {e}")
        if admin_channel_id:
            channel = bot.get_channel(admin_channel_id)
            if channel:
                await channel.send(f"❌ Heartbeat execution failed: {e}")
