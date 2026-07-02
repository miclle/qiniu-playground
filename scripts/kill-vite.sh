#!/usr/bin/env bash
# Kill any lingering Vite dev server process for the website workspace.
# Safe to call when nothing is running — exits 0 either way.
set -euo pipefail

ps aux | grep "[n]pm run dev" | grep "website" | awk '{print $2}' | xargs kill -9 2>/dev/null || true
sleep 1
