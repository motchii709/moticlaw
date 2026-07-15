import importlib
import os
import logging
from typing import Dict, Type
from .base import BaseTool

logger = logging.getLogger(__name__)

class ToolRegistry:
    def __init__(self):
        self.tools: Dict[str, BaseTool] = {}

    def register_tool(self, tool: BaseTool):
        self.tools[tool.name] = tool
        logger.info(f"Registered tool: {tool.name}")

    def load_tools(self, tools_dir: str = "tools"):
        """Automatically loads tools from the specified directory."""
        # Convert path to package notation if inside current working dir
        # For simplicity, we assume 'tools' is a submodule
        for filename in os.listdir(tools_dir):
            if filename.endswith(".py") and filename not in ["__init__.py", "base.py", "registry.py"]:
                module_name = f"{tools_dir.replace('/', '.')}.{filename[:-3]}"
                try:
                    module = importlib.import_module(module_name)
                    # Look for classes that inherit from BaseTool
                    for attr_name in dir(module):
                        attr = getattr(module, attr_name)
                        if (isinstance(attr, type) and 
                            issubclass(attr, BaseTool) and 
                            attr is not BaseTool):
                            self.register_tool(attr())
                except Exception as e:
                    logger.error(f"Failed to load tool from {module_name}: {e}")

    def get_tool(self, name: str) -> BaseTool | None:
        return self.tools.get(name)

    def get_all_tools_metadata(self) -> list:
        return [
            {
                "name": t.name,
                "description": t.description,
                "parameters": t.parameters_schema
            } for t in self.tools.values()
        ]

# Global registry instance
registry = ToolRegistry()
