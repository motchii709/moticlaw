import aiosqlite
import os

DB_FILE = "moticlaw.db"

async def init_db():
    async with aiosqlite.connect(DB_FILE) as db:
        await db.execute('''
            CREATE TABLE IF NOT EXISTS api_keys (
                provider TEXT PRIMARY KEY,
                api_key TEXT NOT NULL
            )
        ''')
        await db.execute('''
            CREATE TABLE IF NOT EXISTS models (
                provider TEXT,
                model_name TEXT,
                score INTEGER DEFAULT 100,
                failure_count INTEGER DEFAULT 0,
                is_disabled BOOLEAN DEFAULT 0,
                last_checked TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                PRIMARY KEY (provider, model_name)
            )
        ''')
        await db.execute('''
            CREATE TABLE IF NOT EXISTS messages (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                channel_id TEXT NOT NULL,
                role TEXT NOT NULL,
                content TEXT NOT NULL,
                timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP
            )
        ''')
        
        # default models
        models = [
            ('openai', 'gpt-4o'),
            ('anthropic', 'claude-3-5-sonnet-20240620'),
            ('groq', 'llama-3.1-8b-instant'),
            ('gemini', 'gemini-1.5-flash'),
            ('nvidia', 'meta/llama-3.1-405b-instruct'),
            ('cerebras', 'llama3.1-70b'),
            ('sambanova', 'Meta-Llama-3.1-70B-Instruct'),
            ('openrouter', 'meta-llama/llama-3.1-8b-instruct:free'),
            ('deepinfra', 'meta-llama/Meta-Llama-3.1-70B-Instruct'),
            ('mistral', 'codestral-latest'),
            ('hyperbolic', 'meta-llama/Meta-Llama-3.1-70B-Instruct'),
            ('scaleway', 'llama-3.1-70b-instruct'),
            ('siliconflow', 'vendor/meta-llama/Meta-Llama-3.1-70B-Instruct'),
            ('together', 'meta-llama/Meta-Llama-3.1-70B-Instruct'),
            ('huggingface', 'meta-llama/Meta-Llama-3.1-70B-Instruct'),
            ('replicate', 'meta/meta-llama-3.1-405b-instruct')
        ]
        
        await db.executemany('''
            INSERT OR IGNORE INTO models (provider, model_name) VALUES (?, ?)
        ''', models)
        
        await db.commit()

async def get_api_key(provider: str) -> str | None:
    async with aiosqlite.connect(DB_FILE) as db:
        async with db.execute('SELECT api_key FROM api_keys WHERE provider = ?', (provider,)) as cursor:
            row = await cursor.fetchone()
            return row[0] if row else None

async def set_api_key(provider: str, api_key: str):
    async with aiosqlite.connect(DB_FILE) as db:
        await db.execute('''
            INSERT INTO api_keys (provider, api_key)
            VALUES (?, ?)
            ON CONFLICT(provider) DO UPDATE SET api_key=excluded.api_key
        ''', (provider, api_key))
        await db.commit()

async def add_message(channel_id: str, role: str, content: str):
    async with aiosqlite.connect(DB_FILE) as db:
        await db.execute('INSERT INTO messages (channel_id, role, content) VALUES (?, ?, ?)', (channel_id, role, content))
        # Keep only last 20 messages per channel
        await db.execute('''
            DELETE FROM messages WHERE id IN (
                SELECT id FROM messages WHERE channel_id = ? ORDER BY timestamp DESC LIMIT -1 OFFSET 20
            )
        ''', (channel_id,))
        await db.commit()

async def get_messages(channel_id: str):
    async with aiosqlite.connect(DB_FILE) as db:
        db.row_factory = aiosqlite.Row
        async with db.execute('SELECT role, content FROM messages WHERE channel_id = ? ORDER BY timestamp ASC', (channel_id,)) as cursor:
            rows = await cursor.fetchall()
            return [{"role": r["role"], "content": r["content"]} for r in rows]

async def clear_messages(channel_id: str):
    async with aiosqlite.connect(DB_FILE) as db:
        await db.execute('DELETE FROM messages WHERE channel_id = ?', (channel_id,))
        await db.commit()

async def get_active_models():
    async with aiosqlite.connect(DB_FILE) as db:
        db.row_factory = aiosqlite.Row
        async with db.execute('SELECT * FROM models WHERE is_disabled = 0 ORDER BY score DESC') as cursor:
            return await cursor.fetchall()

async def update_model_health(provider: str, model_name: str, success: bool):
    async with aiosqlite.connect(DB_FILE) as db:
        if success:
            await db.execute('''
                INSERT INTO models (provider, model_name, score, failure_count, is_disabled, last_checked)
                VALUES (?, ?, 100, 0, 0, CURRENT_TIMESTAMP)
                ON CONFLICT(provider, model_name) DO UPDATE SET
                    score = MIN(score + 5, 100),
                    failure_count = 0,
                    is_disabled = 0,
                    last_checked = CURRENT_TIMESTAMP
            ''', (provider, model_name))
        else:
            await db.execute('''
                INSERT INTO models (provider, model_name, score, failure_count, is_disabled, last_checked)
                VALUES (?, ?, 95, 1, 0, CURRENT_TIMESTAMP)
                ON CONFLICT(provider, model_name) DO UPDATE SET
                    score = MAX(score - 10, 0),
                    failure_count = failure_count + 1,
                    is_disabled = CASE WHEN failure_count >= 2 THEN 1 ELSE 0 END,
                    last_checked = CURRENT_TIMESTAMP
            ''', (provider, model_name))
        await db.commit()

async def resurrect_models():
    async with aiosqlite.connect(DB_FILE) as db:
        await db.execute('''
            UPDATE models 
            SET is_disabled = 0, failure_count = 0, score = 50
            WHERE is_disabled = 1 
            AND strftime('%s', 'now') - strftime('%s', last_checked) > 1800
        ''')
        await db.commit()
