import logging
import database
from .base import BaseTool
from tavily import TavilyClient
from typing import Any, Dict

logger = logging.getLogger(__name__)

class TavilySearchTool(BaseTool):
    @property
    def name(self) -> str:
        return "tavily_search"

    @property
    def description(self) -> str:
        return "Performs a web search using Tavily to get real-time information."

    @property
    def parameters_schema(self) -> Dict[str, Any]:
        return {
            "type": "object",
            "properties": {
                "query": {
                    "type": "string",
                    "description": "The search query."
                },
                "search_depth": {
                    "type": "string",
                    "enum": ["basic", "advanced"],
                    "default": "basic",
                    "description": "The depth of the search."
                }
            },
            "required": ["query"]
        }

    async def execute(self, query: str, search_depth: str = "basic") -> str:
        api_key = await database.get_api_key("tavily")
        if not api_key:
            return "Error: Tavily API key not found. Please register it using /register or 'tavily' onboard."
            
        logger.info(f"Executing Tavily search: {query}")
        try:
            # Tavily SDK call
            client = TavilyClient(api_key=api_key)
            # The search call is blocking, in a production async app you might wrap it in run_in_executor
            # but for this scale it's generally fine.
            response = client.search(query=query, search_depth=search_depth)
            
            results = []
            for result in response.get("results", []):
                results.append(f"Title: {result['title']}\nURL: {result['url']}\nContent: {result['content']}\n")
            
            return "\n---\n".join(results) if results else "No results found."
        except Exception as e:
            logger.error(f"Tavily search failed: {e}")
            return f"Error: {str(e)}"
