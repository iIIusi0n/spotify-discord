if [ -z "$OAUTH_ACCESS_URL" ]; then
    echo "Error: OAUTH_ACCESS_URL environment variable is not set"
    exit 1
fi

if [ -z "$SPOTIFY_OAUTH_CLIENT_ID" ]; then
    echo "Error: SPOTIFY_OAUTH_CLIENT_ID environment variable is not set" 
    exit 1
fi

if [ -z "$SPOTIFY_OAUTH_CLIENT_SECRET" ]; then
    echo "Error: SPOTIFY_OAUTH_CLIENT_SECRET environment variable is not set" 
    exit 1
fi

if [ -z "$DISCORD_BOT_TOKEN" ]; then
    echo "Error: DISCORD_BOT_TOKEN environment variable is not set" 
    exit 1
fi

if [ -z "$DISCORD_GUILD_ID" ]; then
    echo "Error: DISCORD_GUILD_ID environment variable is not set" 
    exit 1
fi

exec /app/spotify-discord