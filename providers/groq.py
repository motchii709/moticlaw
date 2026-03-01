from groq import AsyncGroq
import asyncio

async def call(api_key: str, model_name: str, messages: list[dict], timeout: float = 8.0) -> str:
    client = AsyncGroq(api_key=api_key)
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
        raise Exception(f"Groq API Error: {e}")
