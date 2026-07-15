import google.generativeai as genai
import asyncio

async def call(api_key: str, model_name: str, messages: list[dict], timeout: float = 30.0) -> str:
    genai.configure(api_key=api_key)
    # Convert role names slightly
    gemini_messages = []
    system_instruction = None
    
    for msg in messages:
        if msg["role"] == "system":
            system_instruction = msg["content"]
        elif msg["role"] == "user":
            gemini_messages.append({"role": "user", "parts": [msg["content"]]})
        else:
            gemini_messages.append({"role": "model", "parts": [msg["content"]]})
            
    try:
        model = genai.GenerativeModel(model_name=model_name, system_instruction=system_instruction)
        
        # We wrap in asyncio.to_thread because gemini call is synchronous
        response = await asyncio.wait_for(
            asyncio.to_thread(model.generate_content, gemini_messages),
            timeout=timeout
        )
        return response.text
    except Exception as e:
        raise Exception(f"Gemini API Error: {e}")
