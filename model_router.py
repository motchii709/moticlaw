import database
import importlib
import logging

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

# Map providers to their endpoints if they are OpenAI-compatible
OPENAI_COMPATIBLE_ENDPOINTS = {
    "nvidia": "https://integrate.api.nvidia.com/v1",
    "cerebras": "https://api.cerebras.ai/v1",
    "sambanova": "https://api.sambanova.ai/v1",
    "openrouter": "https://openrouter.ai/api/v1",
    "deepinfra": "https://api.deepinfra.com/v1",
    "mistral": "https://codestral.mistral.ai/v1",
    "hyperbolic": "https://api.hyperbolic.xyz/v1",
    "scaleway": "https://api.scaleway.com/v1/regions/fr-par/inference", # Example region
    "siliconflow": "https://api.siliconflow.cn/v1",
    "together": "https://api.together.xyz/v1"
}

async def chat_completion(messages: list[dict], preferred_provider: str = None) -> str:
    """Routes the request to the best available model, fallback on failure."""
    active_models = await database.get_active_models()
    if not active_models:
        raise Exception("No active models available. Check health or register API keys.")

    models_to_try = []
    if preferred_provider:
        preferred = [m for m in active_models if m['provider'] == preferred_provider]
        others = [m for m in active_models if m['provider'] != preferred_provider]
        models_to_try = preferred + others
    else:
        models_to_try = active_models

    for model_info in models_to_try:
        provider = model_info['provider']
        model_name = model_info['model_name']
        api_key = await database.get_api_key(provider)
        
        if not api_key:
            continue

        try:
            if provider in OPENAI_COMPATIBLE_ENDPOINTS:
                # Use the generic OpenAI-compatible wrapper
                from providers import openai_compatible
                base_url = OPENAI_COMPATIBLE_ENDPOINTS[provider]
                logger.info(f"Routing to OpenAI-Compatible: {provider}/{model_name}")
                response = await openai_compatible.call(api_key, model_name, messages, base_url=base_url)
            else:
                # Dynamic import of specific provider module
                provider_module = importlib.import_module(f"providers.{provider}")
                logger.info(f"Routing to specialized provider: {provider}/{model_name}")
                response = await provider_module.call(api_key, model_name, messages)
            
            await database.update_model_health(provider, model_name, success=True)
            return response
            
        except Exception as e:
            logger.error(f"Failed with {provider}/{model_name}: {e}")
            await database.update_model_health(provider, model_name, success=False)
            continue
            
    raise Exception("All models failed to process the request.")
