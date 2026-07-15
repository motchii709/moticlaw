import asyncio
import os
import sys
import subprocess
from dotenv import set_key

def check_dependencies():
    print("Checking dependencies...")
    try:
        import aiosqlite
        import discord
        import dotenv
        print("✅ Dependencies found.")
    except ImportError:
        print("❌ Missing dependencies. Installing from requirements.txt...")
        try:
            subprocess.check_call([sys.executable, "-m", "pip", "install", "-r", "requirements.txt"])
            print("✅ Dependencies installed successfully.")
        except Exception as e:
            print(f"❌ Failed to install dependencies: {e}")
            print("Please run 'python -m pip install -r requirements.txt' manually.")
            sys.exit(1)

async def onboard():
    import database # Import here after ensuring dependencies are met
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
    supported_providers = [
        "openai", "anthropic", "groq", "gemini", "nvidia", "cerebras", 
        "sambanova", "openrouter", "deepinfra", "mistral", "hyperbolic", 
        "scaleway", "siliconflow", "together", "huggingface", "replicate",
        "tavily"
    ]
    print(f"Supported providers: {', '.join(supported_providers)}")
    
    provider = ""
    while provider not in supported_providers + ["skip"]:
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
        check_dependencies()
        asyncio.run(onboard())
    else:
        print("Usage: python onboard.py onboard")
