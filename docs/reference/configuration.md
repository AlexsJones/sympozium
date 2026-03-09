# Configuration

## Environment Variables

| Variable | Component | Description |
|----------|-----------|-------------|
| `EVENT_BUS_URL` | All | NATS server URL |
| `DATABASE_URL` | API Server | PostgreSQL connection string |
| `INSTANCE_NAME` | Channels | Owning SympoziumInstance name |
| `MEMORY_ENABLED` | Agent Runner | Whether persistent memory is active |
| `TELEGRAM_BOT_TOKEN` | Telegram | Bot API token |
| `SLACK_BOT_TOKEN` | Slack | Bot OAuth token |
| `SLACK_APP_TOKEN` | Slack | App-level token for Socket Mode |
| `DISCORD_BOT_TOKEN` | Discord | Bot token |
| `WHATSAPP_ACCESS_TOKEN` | WhatsApp | Cloud API access token |

## LLM Providers

Sympozium supports any GenAI provider with an OpenAI-compatible API:

| Provider | Base URL | API Key Variable |
|----------|----------|-----------------|
| OpenAI | (default) | `OPENAI_API_KEY` |
| Anthropic | (default) | `ANTHROPIC_API_KEY` |
| Azure OpenAI | your endpoint | `AZURE_OPENAI_API_KEY` |
| Ollama | `http://ollama:11434/v1` | none |
| Any OpenAI-compatible | custom URL | custom |

See the [Ollama guide](../guides/ollama.md) for detailed local LLM setup.
