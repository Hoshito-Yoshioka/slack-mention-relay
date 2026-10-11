#!/usr/bin/env bash
# Required parameters:
# @raycast.schemaVersion 1
# @raycast.title Slack Relay Toggle
# @raycast.mode silent
#
# Optional parameters:
# @raycast.icon 🔔
# @raycast.packageName slack-mention-relay
#
# Thin wrapper: all logic lives in the Go binary.
set -euo pipefail
exec "${HOME}/.slack-relay/bin/slack-mention-relay" toggle
