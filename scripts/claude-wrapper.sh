#!/bin/sh
# Wrapper script for Claude Code that pre-approves API key before launching.

CLAUDE_JSON="/root/.claude.json"

# If ANTHROPIC_API_KEY is set, pre-approve it in claude.json
if [ -n "$ANTHROPIC_API_KEY" ]; then
    # Extract last 20 characters of the API key (the suffix Claude uses for approval)
    KEY_SUFFIX=$(echo "$ANTHROPIC_API_KEY" | tail -c 21)

    # Check if jq is available, otherwise use sed
    if command -v jq >/dev/null 2>&1; then
        # Use jq to add the key suffix to approved list
        TMP_FILE=$(mktemp)
        jq --arg suffix "$KEY_SUFFIX" '.customApiKeyResponses.approved = (.customApiKeyResponses.approved // []) + [$suffix] | .customApiKeyResponses.approved |= unique' "$CLAUDE_JSON" > "$TMP_FILE" && mv "$TMP_FILE" "$CLAUDE_JSON"
    else
        # Fallback: create the structure manually if file is simple
        if grep -q '"customApiKeyResponses"' "$CLAUDE_JSON" 2>/dev/null; then
            # Already has the key, try to add to it with sed (basic)
            sed -i "s/\"approved\":\s*\[\]/\"approved\":[\"$KEY_SUFFIX\"]/" "$CLAUDE_JSON"
        else
            # Add the whole block - read current content and rebuild
            CURRENT=$(cat "$CLAUDE_JSON" | tr -d '\n' | sed 's/}$//')
            echo "${CURRENT},\"customApiKeyResponses\":{\"approved\":[\"$KEY_SUFFIX\"],\"rejected\":[]}}" > "$CLAUDE_JSON"
        fi
    fi
fi

# Execute claude with all arguments
exec /usr/local/bin/claude "$@"
