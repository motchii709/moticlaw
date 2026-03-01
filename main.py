import asyncio
import os
import logging
from dotenv import load_dotenv
from apscheduler.schedulers.asyncio import AsyncIOScheduler

import database
import config
import health_checker
import heartbeat_core
from channels.discord_channel import bot

logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(name)s - %(levelname)s - %(message)s')
logger = logging.getLogger(__name__)

async def run_periodic_health_check():
    logger.info("Running periodic health check...")
    cfg = await config.load_config()
    await health_checker.run_health_check(bot, cfg.get("admin_channel_id"))

async def run_periodic_heartbeat():
    logger.info("Running periodic heartbeat...")
    cfg = await config.load_config()
    # Execute heartbeat with stored admin channel
    await heartbeat_core.execute_heartbeat(bot, cfg.get("admin_channel_id"), cfg.get("heartbeat_interval_minutes", 30))

async def main():
    load_dotenv()
    discord_token = os.getenv("DISCORD_TOKEN")
    
    if not discord_token:
        logger.error("DISCORD_TOKEN environment variable not set.")
        logger.error("Please set it in a .env file or export it.")
        return

    # 1. Init Database
    logger.info("Initializing database...")
    await database.init_db()
    
    # 2. Load Config
    cfg = await config.load_config()
    
    # 3. Setup Scheduler
    logger.info("Starting background schedulers...")
    scheduler = AsyncIOScheduler()
    
    # Health check every 3 minutes
    scheduler.add_job(run_periodic_health_check, 'interval', minutes=3)
    
    # Heartbeat interval from config
    hb_interval = cfg.get("heartbeat_interval_minutes", 30)
    scheduler.add_job(run_periodic_heartbeat, 'interval', minutes=hb_interval)
    
    scheduler.start()
    
    # Run a heartbeat on startup (async)
    logger.info("Triggering initial startup Heartbeat...")
    asyncio.create_task(run_periodic_heartbeat())

    # 4. Start Discord Bot
    logger.info("Starting Discord bot...")
    
    # Load extensions dynamically
    extensions_dir = "extensions"
    if os.path.exists(extensions_dir):
        for filename in os.listdir(extensions_dir):
            if filename.endswith(".py") and filename != "__init__.py":
                module_name = f"extensions.{filename[:-3]}"
                try:
                    await bot.load_extension(module_name)
                    logger.info(f"Loaded extension: {module_name}")
                except Exception as e:
                    logger.error(f"Failed to load extension {module_name}: {e}")

    # Use asyncio.gather or just await bot.start()
    try:
        await bot.start(discord_token)
    except KeyboardInterrupt:
        logger.info("Shutting down...")
    finally:
        scheduler.shutdown()
        await bot.close()

if __name__ == "__main__":
    asyncio.run(main())
