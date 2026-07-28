#!/usr/bin/env sh
set -eu

if [ -z "${BREVO_API_KEY:-}" ]; then
  echo "BREVO_API_KEY is required"
  exit 1
fi

if [ -z "${WEBHOOK_BASE_URL:-}" ]; then
  echo "WEBHOOK_BASE_URL is required (example: https://api.example.com)"
  exit 1
fi

BREVO_WEBHOOK_TOKEN="${BREVO_WEBHOOK_TOKEN:-}"
WEBHOOK_URL="${WEBHOOK_BASE_URL%/}/webhooks/brevo/events"
HEADERS_JSON="[]"
if [ -n "$BREVO_WEBHOOK_TOKEN" ]; then
  HEADERS_JSON='[{"key":"X-Webhook-Token","value":"'"$BREVO_WEBHOOK_TOKEN"'"}]'
fi

cat <<EOF
Creating Brevo transactional webhook with events:
- sent
- delivered
- opened
- uniqueOpened
- click
- hardBounce
- softBounce
- blocked
- unsubscribed
Target URL: $WEBHOOK_URL
EOF

curl -sS -X POST "https://api.brevo.com/v3/webhooks" \
  -H "accept: application/json" \
  -H "api-key: $BREVO_API_KEY" \
  -H "content-type: application/json" \
  -d "{\"url\":\"$WEBHOOK_URL\",\"description\":\"Wello Resto outbound tracking\",\"events\":[\"sent\",\"delivered\",\"opened\",\"uniqueOpened\",\"click\",\"hardBounce\",\"softBounce\",\"blocked\",\"unsubscribed\"],\"headers\":$HEADERS_JSON,\"type\":\"transactional\"}"

echo
