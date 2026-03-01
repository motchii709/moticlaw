import json
import os
import aiofiles

CONFIG_FILE = "config.json"

DEFAULT_CONFIG = {
    "heartbeat_interval_minutes": 30,
    "admin_channel_id": None
}

async def load_config() -> dict:
    if not os.path.exists(CONFIG_FILE):
        await save_config(DEFAULT_CONFIG)
        return DEFAULT_CONFIG
    try:
        async with aiofiles.open(CONFIG_FILE, mode='r', encoding='utf-8') as f:
            content = await f.read()
            return json.loads(content)
    except Exception as e:
        print(f"Error loading config: {e}")
        return DEFAULT_CONFIG

async def save_config(config_data: dict):
    async with aiofiles.open(CONFIG_FILE, mode='w', encoding='utf-8') as f:
        await f.write(json.dumps(config_data, indent=4, ensure_ascii=False))

def get_env_or_die(var_name: str) -> str:
    val = os.getenv(var_name)
    if not val:
        raise ValueError(f"Environment variable {var_name} not set")
    return val
