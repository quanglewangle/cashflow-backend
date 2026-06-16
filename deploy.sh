#!/bin/bash
# Usage: ./deploy.sh user@yourserver.example.com
set -e

SERVER=${1:-peter@fimblefowl.co.uk}

HASH=$(git rev-parse --short HEAD)

echo "Building for linux/amd64 (static, hash=$HASH)..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
  -ldflags="-X main.buildHash=$HASH" \
  -o /tmp/cashflow_deploy .

echo "Copying binary to $SERVER..."
scp /tmp/cashflow_deploy "$SERVER":/tmp/cashflow_new

echo "Installing on $SERVER..."
ssh "$SERVER" "mv /tmp/cashflow_new /home/peter/cashflow && systemctl --user restart cashflow && systemctl --user is-active cashflow"

echo "Done — deployed $HASH to $SERVER"
