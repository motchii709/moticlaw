import aiohttp
import asyncio

async def call(api_key: str, model_name: str, messages: list[dict], timeout: float = 30.0) -> str:
    # Use Inference API
    url = f"https://api-inference.huggingface.co/models/{model_name}/v1/chat/completions"
    headers = {"Authorization": f"Bearer {api_key}"}
    payload = {
        "model": model_name,
        "messages": messages,
        "max_tokens": 500
    }
    
    async with aiohttp.ClientSession() as session:
        try:
            async with session.post(url, headers=headers, json=payload, timeout=timeout) as response:
                if response.status != 200:
                    text = await response.text()
                    raise Exception(f"HF Error: {response.status} - {text}")
                result = await response.json()
                return result["choices"][0]["message"]["content"]
        except Exception as e:
            raise Exception(f"HuggingFace API Error: {e}")
