# Discord Emote Keeper

A Discord bot built with Arikawa that tracks emoji and sticker usage across your server.

## Features

- **Track Custom Emojis**: Monitors server-uploaded custom emojis (both static and animated)
  - From message content
  - From message reactions (add/remove)
- **Track Stickers**: Records sticker usage from messages
- **SQLite Database**: Stores all usage data persistently with BIGINT IDs
- **Event Handling**: Monitors multiple event types:
  - `MessageCreateEvent` - emojis and stickers in messages
  - `InteractionCreateEvent` - emojis and stickers in interactions
  - `MessageReactionAddEvent` - emoji reactions added
  - `MessageReactionRemoveEvent` - emoji reactions removed
- **Per-Server Tracking**: Separate statistics for each Discord server
- **Slash Commands**: Moderator commands to view statistics and manage data

## Setup

1. **Install Dependencies**:
   ```bash
   go mod download
   ```

2. **Set Discord Bot Token**:
   
   Set your Discord bot token as an environment variable:
   
   **Windows (PowerShell)**:
   ```powershell
   $env:DISCORD_TOKEN="your_bot_token_here"
   ```
   
   **Windows (CMD)**:
   ```cmd
   set DISCORD_TOKEN=your_bot_token_here
   ```
   
   **Linux/Mac**:
   ```bash
   export DISCORD_TOKEN="your_bot_token_here"
   ```

3. **Enable Required Intents**:
   
   Make sure your bot has these intents enabled in the [Discord Developer Portal](https://discord.com/developers/applications):
   - `GUILDS`
   - `GUILD_MESSAGES`
   - `MESSAGE_CONTENT` (Privileged Intent - requires verification for large bots)
   - `GUILD_MESSAGE_REACTIONS`

## Running the Bot

```bash
go run main.go
```

The bot will:
- Create a SQLite database file `emote_tracker.db` in the current directory
- Connect to Discord and start monitoring messages
- Register slash commands automatically
- Log all tracked emojis and stickers to the console

## Slash Commands

The bot provides the following moderator-only commands (requires **Manage Server** permission):

### `/listemojis`
Displays a paginated list of custom emoji usage statistics for the current server.
- **Format**: `- <emoji>: count`
- **25 emojis per page**
- **Navigation**: Use `<<`, `<`, `>`, `>>` buttons to navigate pages

### `/liststickers`
Displays a paginated list of sticker usage statistics for the current server.
- **Format**: Sticker image URL followed by count
- **5 stickers per page**
- **Navigation**: Use `<<`, `<`, `>`, `>>` buttons to navigate pages
- Stickers are displayed as: `https://media.discordapp.net/stickers/[id].webp?size=96&quality=lossless`

### `/resetcount`
Resets all emoji and sticker usage counts for the current server.
- **Warning**: This action is irreversible!
- Deletes all tracking data for the server from the database

## Database Schema

### Emojis Table
- `server_id`: Discord Guild ID (BIGINT)
- `emote_id`: Discord custom emoji ID (BIGINT)
- `emote_name`: Name of the custom emoji
- `usage_count`: Number of times used
- `first_used`: First usage timestamp
- `last_used`: Last usage timestamp
- Primary Key: `(server_id, emote_id)`

### Stickers Table
- `server_id`: Discord Guild ID (BIGINT)
- `sticker_id`: Discord sticker ID (BIGINT)
- `sticker_name`: Sticker name
- `usage_count`: Number of times used
- `first_used`: First usage timestamp
- `last_used`: Last usage timestamp
- Primary Key: `(server_id, sticker_id)`

## Querying Usage Data

You can query the database using any SQLite client. A `queries.sql` file is provided with useful pre-written queries.

**Example queries:**

```sql
-- Top 10 most used custom emojis
SELECT emote_name, emote_id, server_id, usage_count 
FROM emojis 
ORDER BY usage_count DESC 
LIMIT 10;

-- Top 10 most used stickers
SELECT sticker_name, sticker_id, server_id, usage_count 
FROM stickers 
ORDER BY usage_count DESC 
LIMIT 10;

-- Most popular emojis for a specific server
SELECT emote_name, usage_count 
FROM emojis 
WHERE server_id = 123456789
ORDER BY usage_count DESC;
```

## Building

To build a standalone executable:

```bash
go build -o emote_keeper.exe
```

## Notes

- The bot only tracks **custom emojis** (server-uploaded) and **stickers**, not standard Unicode emojis
- The bot ignores messages from other bots to prevent counting bot-generated emojis
- The bot only tracks messages and interactions from guild/server channels (DMs are ignored)
- **Reaction tracking**:
  - When a custom emoji reaction is added, the count increases
  - When a custom emoji reaction is removed, the count decreases (minimum 0)
  - Only custom emoji reactions are tracked, not Unicode emoji reactions
- The database uses UPSERT operations to efficiently update counts
- All timestamps are in UTC
- IDs are stored as BIGINT (int64) to match Discord's snowflake ID format
