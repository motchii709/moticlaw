import asyncio
import os
from dotenv import set_key
import database
import sys

async def onboard():
    print("========================================")
    print("Welcome to Moticlaw Onboarding!")
    print("This script will set up the bare minimum requirements.")
    print("Detailed configuration can be done via Discord chat later.")
    print("========================================\n")

    # 1. Discord Setup
    env_file = ".env"
    if not os.path.exists(env_file):
        open(env_file, 'a').close()
        
    print("[1/2] Discord Bot Token")
    print("Enter your Discord Bot Token (or press Enter to skip if already set):")
    token = input("> ").strip()
    if token:
        set_key(env_file, "DISCORD_TOKEN", token)
        print("✅ DISCORD_TOKEN saved to .env")
    else:
        print("Skipped.")

    # Initialize DB for API keys
    await database.init_db()

    # 2. Minimum API Key
    print("\n[2/2] Primary AI Provider API Key")
    print("Moticlaw needs at least one API key to start.")
    print("Supported providers: openai, anthropic, groq, gemini")
    
    provider = ""
    while provider not in ["openai", "anthropic", "groq", "gemini", "skip"]:
        print("Enter provider name (or 'skip'):")
        provider = input("> ").strip().lower()
        
    if provider != "skip":
        print(f"Enter your {provider.capitalize()} API Key:")
        api_key = input("> ").strip()
        if api_key:
            await database.set_api_key(provider, api_key)
            print(f"✅ API Key for {provider} saved to database.")
            
    print("\n========================================")
    print("🎉 Onboarding Complete!")
    print("Next steps:")
    print("1. Run `python main.py` to start the bot.")
    print("2. Go to your Discord server and mention the bot.")
    print("3. You can chat with the AI and configure other settings (like admin channels or extra models) directly in Discord!")
    print("========================================")

if __name__ == "__main__":
    if len(sys.argv) > 1 and sys.argv[1] == "onboard":
        asyncio.run(onboard())
    else:
        print("Usage: python onboard.py onboard")
