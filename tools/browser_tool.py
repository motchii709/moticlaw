import logging
import database
from .base import BaseTool
from typing import Any, Dict
from browser_use import Agent, Browser, BrowserConfig
from langchain_openai import ChatOpenAI
from langchain_google_genai import ChatGoogleGenerativeAI

logger = logging.getLogger(__name__)

class BrowserUseTool(BaseTool):
    @property
    def name(self) -> str:
        return "browser_use"

    @property
    def description(self) -> str:
        return "Advanced autonomous browser navigation. Use this for complex web tasks, scraping, or multi-step interactions."

    @property
    def parameters_schema(self) -> Dict[str, Any]:
        return {
            "type": "object",
            "properties": {
                "task": {
                    "type": "string",
                    "description": "The natural language description of the task to perform in the browser."
                }
            },
            "required": ["task"]
        }

    async def execute(self, task: str) -> str:
        logger.info(f"Executing Browser-use task: {task}")
        
        # Determine the best LLM to power the browser agent
        # Browser-use works well with GPT-4o or Gemini 1.5 Pro
        api_key_openai = await database.get_api_key("openai")
        api_key_gemini = await database.get_api_key("gemini")
        
        llm = None
        if api_key_openai:
            llm = ChatOpenAI(model="gpt-4o", api_key=api_key_openai)
        elif api_key_gemini:
            llm = ChatGoogleGenerativeAI(model="gemini-1.5-flash", google_api_key=api_key_gemini)
        else:
            return "Error: browser_use requires an OpenAI or Gemini API key to power the sub-agent."

        try:
            # We use a headless browser by default for Moticlaw
            agent = Agent(
                task=task,
                llm=llm
            )
            result = await agent.run()
            
            # Extract the final result from the AgentHistoryList
            return str(result.final_result()) if result else "Browser task completed with no specific output."
            
        except Exception as e:
            logger.error(f"Browser-use execution failed: {e}")
            return f"Error: {str(e)}"
