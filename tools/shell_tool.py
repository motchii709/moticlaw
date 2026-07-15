import asyncio
import logging
from .base import BaseTool
from typing import Any, Dict

logger = logging.getLogger(__name__)

class ShellTool(BaseTool):
    @property
    def name(self) -> str:
        return "shell"

    @property
    def description(self) -> str:
        return "Executes a shell command on the host system."

    @property
    def parameters_schema(self) -> Dict[str, Any]:
        return {
            "type": "object",
            "properties": {
                "command": {
                    "type": "string",
                    "description": "The command to execute."
                }
            },
            "required": ["command"]
        }

    async def execute(self, command: str) -> str:
        logger.info(f"Executing shell command: {command}")
        try:
            process = await asyncio.create_subprocess_shell(
                command,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE
            )
            stdout, stderr = await process.communicate()
            result = stdout.decode().strip() or stderr.decode().strip()
            return result if result else "Success (no output)"
        except Exception as e:
            return f"Error: {str(e)}"
