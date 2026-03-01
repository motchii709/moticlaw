from openai import AsyncOpenAI
import asyncio
import logging

logger = logging.getLogger(__name__)

async def call(api_key: str, model_name: str, messages: list[dict], base_url: str, timeout: float = 30.0) -> str:
    client = AsyncOpenAI(api_key=api_key, base_url=base_url)
    try:
        response = await asyncio.wait_for(
            client.chat.completions.create(
                model=model_name,
                messages=messages,
            ),
            timeout=timeout
        )
        return response.choices[0].message.content
    except Exception as e:
        raise Exception(f"OpenAI-Compatible API Error ({base_url}): {e}")
