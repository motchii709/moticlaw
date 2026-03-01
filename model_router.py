import database
import importlib
import logging

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

async def chat_completion(messages: list[dict], preferred_provider: str = None) -> str:
    """Routes the request to the best available model, fallback on failure."""
    active_models = await database.get_active_models()
    if not active_models:
        raise Exception("No active models available. Check health or register API keys.")

    models_to_try = []
    if preferred_provider:
        # Put preferred first if it exists
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
            logger.warning(f"No API key for {provider}, skipping.")
            continue

        try:
            # Dynamic import of the provider module
            provider_module = importlib.import_module(f"providers.{provider}")
            
            logger.info(f"Routing to {provider}/{model_name}")
            response = await provider_module.call(api_key, model_name, messages)
            
            # Update health on success
            await database.update_model_health(provider, model_name, success=True)
            return response
            
        except Exception as e:
            logger.error(f"Failed with {provider}/{model_name}: {e}")
            # Update health on failure
            await database.update_model_health(provider, model_name, success=False)
            continue
            
    raise Exception("All models failed to process the request.")
