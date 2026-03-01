import logging
import database
import model_router

logger = logging.getLogger(__name__)

async def run_health_check(bot=None, admin_channel_id=None):
    """
    Checks all configured models by sending a simple 'ALIVE' probe.
    Logs success/failure and auto-disables models after 3 consecutive failures.
    """
    await database.resurrect_models() # Try to revive models disabled > 30 mins
    
    models = await database.get_active_models()
    report = []
    
    for model_info in models:
        provider = model_info['provider']
        model_name = model_info['model_name']
        
        api_key = await database.get_api_key(provider)
        if not api_key:
            report.append(f"⚠️ `{provider}`: APIキー未設定")
            continue
            
        try:
            await model_router.chat_completion(
                [{"role": "user", "content": "Return 'ALIVE'. Do not say anything else."}],
                preferred_provider=provider
            )
            report.append(f"✅ `{provider}/{model_name}`: 正常")
        except Exception as e:
            report.append(f"❌ `{provider}/{model_name}`: 失敗 ({e})")
            
    # Send report to discord if configured
    if bot and admin_channel_id:
        try:
            channel = bot.get_channel(int(admin_channel_id))
            if channel:
                active_count = sum("✅" in r for r in report)
                total = len(models)
                message = f"**🔧 ヘルスチェック [正常: {active_count}/{total}]**\n" + "\n".join(report)
                await channel.send(message)
        except Exception as e:
            logger.error(f"Failed to send health check to Discord: {e}")
    
    return report
