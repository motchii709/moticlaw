import discord
from discord.ext import commands
import logging
import database
import config
from model_router import chat_completion
import heartbeat_core
import aiofiles

logger = logging.getLogger(__name__)

class MoticlawDiscord(commands.Bot):
    def __init__(self):
        intents = discord.Intents.default()
        intents.message_content = True
        super().__init__(command_prefix="!", intents=intents)

    async def setup_hook(self):
        await self.tree.sync()
        logger.info("Slash commands synced.")

bot = MoticlawDiscord()

@bot.event
async def on_ready():
    logger.info(f"Logged in as {bot.user} (ID: {bot.user.id})")
    print(f"Logged in as {bot.user}")

@bot.tree.command(name="register", description="Register an API key for a provider")
async def register_key(interaction: discord.Interaction, provider: str, api_key: str):
    # In a real app, check if user is admin.
    supported = ["openai", "anthropic", "groq", "gemini"]
    if provider not in supported:
        await interaction.response.send_message(f"Supported providers: {', '.join(supported)}", ephemeral=True)
        return
        
    await database.set_api_key(provider, api_key)
    await interaction.response.send_message(f"Key for {provider} registered.", ephemeral=True)

@bot.tree.command(name="status", description="Show models ranking and health status")
async def status_check(interaction: discord.Interaction):
    models = await database.get_active_models()
    if not models:
        await interaction.response.send_message("No active models.", ephemeral=True)
        return
        
    lines = ["**Model Ranking & Health**"]
    for m in models:
        status = "✅" if m['score'] == 100 else "⚠️"
        lines.append(f"{status} `{m['provider']}/{m['model_name']}` - Score: {m['score']}, Failures: {m['failure_count']}")
        
    await interaction.response.send_message("\n".join(lines))

@bot.tree.command(name="health", description="Manually trigger model health check")
async def manual_health(interaction: discord.Interaction):
    await interaction.response.defer()
    import health_checker
    
    cfg = await config.load_config()
    report = await health_checker.run_health_check(bot, cfg.get("admin_channel_id"))
    
    if report:
        active_count = sum("✅" in r for r in report)
        reply = f"**🔧 ヘルスチェック [正常: {active_count}/{len(report)}]**\n" + "\n".join(report)
        await interaction.followup.send(reply)
    else:
        await interaction.followup.send("No models to test.")

@bot.tree.command(name="heartbeat_now", description="Force an immediate heartbeat cycle")
async def force_heartbeat(interaction: discord.Interaction):
    await interaction.response.defer()
    cfg = await config.load_config()
    admin_ch = cfg.get("admin_channel_id", interaction.channel_id)
    await interaction.followup.send("Forcing Heartbeat...")
    await heartbeat_core.execute_heartbeat(bot, admin_ch, cfg.get("heartbeat_interval_minutes", 30))

@bot.tree.command(name="edit_heartbeat", description="View or edit the HEARTBEAT.md manual")
async def edit_heartbeat(interaction: discord.Interaction, new_content: str = None):
    if not new_content:
        # Just view
        try:
            async with aiofiles.open("HEARTBEAT.md", 'r', encoding='utf-8') as f:
                content = await f.read()
            # Split if too long
            if len(content) > 1900:
                await interaction.response.send_message(f"```markdown\n{content[:1900]}...\n```\n*(Truncated)*", ephemeral=True)
            else:
                await interaction.response.send_message(f"```markdown\n{content}\n```", ephemeral=True)
        except Exception as e:
            await interaction.response.send_message(f"Error reading file: {e}", ephemeral=True)
    else:
        try:
            async with aiofiles.open("HEARTBEAT.md", 'w', encoding='utf-8') as f:
                await f.write(new_content.replace("\\n", "\n"))
            await interaction.response.send_message("HEARTBEAT.md updated successfully.", ephemeral=True)
        except Exception as e:
            await interaction.response.send_message(f"Error writing file: {e}", ephemeral=True)

@bot.event
async def on_message(message: discord.Message):
    if message.author == bot.user:
        return
        
    # React to mentions or DM
    if bot.user in message.mentions or isinstance(message.channel, discord.DMChannel):
        async with message.channel.typing():
            try:
                # Provide minimal context of the message that was sent
                messages = [
                    {"role": "system", "content": "You are Moticlaw. Respond conversationally to the user."},
                    {"role": "user", "content": message.content}
                ]
                response = await chat_completion(messages)
                
                # Split large messages
                for i in range(0, len(response), 2000):
                    await message.reply(response[i:i+2000])
                    
            except Exception as e:
                await message.reply(f"❌ Error thinking: {e}")

def run_discord_bot(token: str):
    bot.run(token)
