import logging
import database
import config
from .base import BaseTool
from typing import Any, Dict

logger = logging.getLogger(__name__)

class ConfigTool(BaseTool):
    @property
    def name(self) -> str:
        return "manage_config"

    @property
    def description(self) -> str:
        return "Manages bot configuration: set admin channel, heartbeat interval, or register API keys."

    @property
    def parameters_schema(self) -> Dict[str, Any]:
        return {
            "type": "object",
            "properties": {
                "action": {
                    "type": "string",
                    "enum": ["SET_ADMIN_CHANNEL", "SET_HEARTBEAT_INTERVAL", "REGISTER_KEY"],
                    "description": "The configuration action to perform."
                },
                "channel_id": {
                    "type": "integer",
                    "description": "The Discord channel ID (required for SET_ADMIN_CHANNEL)."
                },
                "minutes": {
                    "type": "integer",
                    "description": "The heartbeat interval in minutes (required for SET_HEARTBEAT_INTERVAL)."
                },
                "provider": {
                    "type": "string",
                    "description": "The provider name (required for REGISTER_KEY)."
                },
                "api_key": {
                    "type": "string",
                    "description": "The API key (required for REGISTER_KEY)."
                }
            },
            "required": ["action"]
        }

    async def execute(self, action: str, **kwargs) -> str:
        logger.info(f"ConfigTool executing action: {action}")
        cfg = await config.load_config()
        
        try:
            if action == "SET_ADMIN_CHANNEL":
                channel_id = kwargs.get("channel_id")
                if channel_id:
                    cfg["admin_channel_id"] = int(channel_id)
                    await config.save_config(cfg)
                    return f"Success: Admin channel set to {channel_id}."
                return "Error: Missing 'channel_id' for SET_ADMIN_CHANNEL."

            elif action == "SET_HEARTBEAT_INTERVAL":
                minutes = kwargs.get("minutes")
                if minutes:
                    cfg["heartbeat_interval_minutes"] = int(minutes)
                    await config.save_config(cfg)
                    return f"Success: Heartbeat interval set to {minutes} minutes."
                return "Error: Missing 'minutes' for SET_HEARTBEAT_INTERVAL."

            elif action == "REGISTER_KEY":
                provider = kwargs.get("provider")
                api_key = kwargs.get("api_key")
                if provider and api_key:
                    await database.set_api_key(provider, api_key)
                    return f"Success: API Key for {provider} updated."
                return "Error: Missing 'provider' or 'api_key' for REGISTER_KEY."

            return f"Error: Unknown action '{action}'."
        except Exception as e:
            logger.error(f"ConfigTool failed: {e}")
            return f"Error: {str(e)}"
