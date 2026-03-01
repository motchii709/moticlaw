import aiohttp
import asyncio

async def call(api_key: str, model_name: str, messages: list[dict], timeout: float = 30.0) -> str:
    # Simple Replicate API call for Llama models
    url = "https://api.replicate.com/v1/predictions"
    headers = {
        "Authorization": f"Token {api_key}",
        "Content-Type": "application/json"
    }
    
    # Format prompt from messages
    prompt = ""
    for msg in messages:
        prompt += f"{msg['role']}: {msg['content']}\n"
    prompt += "assistant: "

    payload = {
        "version": "a0a1497236dc9a263c9c9e8fb17a417fd9e96e5781a74dced48c77be9b3621b1", # Example hash for Llama 3.1 405b
        "input": {
            "prompt": prompt,
            "max_new_tokens": 500
        }
    }
    
    async with aiohttp.ClientSession() as session:
        try:
            async with session.post(url, headers=headers, json=payload, timeout=timeout) as response:
                if response.status != 201:
                    text = await response.text()
                    raise Exception(f"Replicate Error: {response.status} - {text}")
                
                prediction = await response.json()
                get_url = prediction["urls"]["get"]
                
                # Poll for result
                while True:
                    async with session.get(get_url, headers=headers) as poll_resp:
                        poll_result = await poll_resp.json()
                        if poll_result["status"] == "succeeded":
                            return "".join(poll_result["output"])
                        if poll_result["status"] == "failed":
                            raise Exception("Replicate prediction failed")
                        await asyncio.sleep(1)
        except Exception as e:
            raise Exception(f"Replicate API Error: {e}")
