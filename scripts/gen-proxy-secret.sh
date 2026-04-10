#!/bin/bash
SECRET=$(openssl rand -hex 16)
echo "TG_WS_PROXY_SECRET=$SECRET"
echo ""
echo "Add this to your .env file"
echo ""
echo "To test proxy connection:"
echo "docker run --rm ghcr.io/audetv/tg-ws-proxy:latest echo 'Secret: $SECRET'"