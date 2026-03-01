from anthropic import AsyncAnthropic
import asyncio

async def call(api_key: str, model_name: str, messages: list[dict], timeout: float = 20.0) -> str:
    client = AsyncAnthropic(api_key=api_key)
    
    # Format messages for Anthropic (if system prompt exists, extract it)
    system = ""
    anthropic_messages = []
    
    for msg in messages:
        if msg["role"] == "system":
            system = msg["content"]
        else:
            anthropic_messages.append({
                "role": "user" if msg["role"] == "user" else "assistant",
                "content": msg["content"]
            })
            
    try:
        response = await asyncio.wait_for(
            client.messages.create(
                model=model_name,
                max_tokens=4090,
                system=system,
                messages=anthropic_messages
            ),
            timeout=timeout
        )
        return response.content[0].text
    except Exception as e:
        raise Exception(f"Anthropic API Error: {e}")
