# AI Summary Setup

To enable the AI summarization feature, you need to configure the Anthropic Claude API key.

## Setup Instructions

1. Get an API key from [Anthropic Console](https://console.anthropic.com/)

2. Set the environment variable:
   ```bash
   export ANTHROPIC_API_KEY="your-api-key-here"
   ```

3. Restart the multichat server:
   ```bash
   systemctl --user restart multichat-server
   ```

## Usage

1. Go to the Inbox page
2. Select any conversation from the left panel
3. Click the "✨ Summarize" button in the conversation header
4. The AI will analyze the last 50 messages and provide:
   - Summary of the conversation
   - Key topics discussed
   - Overall sentiment (positive/negative/neutral)

## API Endpoint

```bash
curl -X POST -H "Content-Type: application/json" \
  -d '{"room_id": "!your-room-id:chat.kayushkin.com"}' \
  http://localhost:8402/api/ai/summarize
```

Response:
```json
{
  "summary": "Recent conversation about...",
  "topics": ["topic1", "topic2"],
  "sentiment": "positive"
}
```

## Note

Without the API key, the endpoint will return:
```json
{"error": "ANTHROPIC_API_KEY environment variable not set"}
```