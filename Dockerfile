FROM golang:1.25-alpine AS go-builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git

# Copy go mod files
COPY go.mod go.sum* ./
RUN go mod download

# Copy source
COPY . .

# Build
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /catty-exec-runtime ./cmd/catty-exec-runtime

# Claude installer stage
FROM node:22-alpine AS claude-builder

RUN npm install -g @anthropic-ai/claude-code

# Runtime image
FROM node:22-alpine

# Install runtime dependencies
RUN apk add --no-cache ca-certificates git jq

# Copy claude from builder (npm global bin location)
COPY --from=claude-builder /usr/local/bin/claude /usr/local/bin/claude
COPY --from=claude-builder /usr/local/lib/node_modules/@anthropic-ai /usr/local/lib/node_modules/@anthropic-ai

# Pre-create Claude Code config directories and settings
RUN mkdir -p /root/.claude/projects /root/.claude/todos /root/.claude/statsig

# Pre-populate claude.json to skip first-run onboarding prompts and pre-approve settings
# - hasCompletedOnboarding: skip onboarding wizard
# - projects["/"]: pre-trust the root directory
# - customApiKeyResponses.approved: empty array, will be populated at runtime via wrapper
RUN echo '{"numStartups":1,"installMethod":"npm","autoUpdates":false,"hasCompletedOnboarding":true,"lastOnboardingVersion":"1.0.0","projects":{"/":{"allowedTools":[],"hasTrustDialogAccepted":true,"hasClaudeMdExternalIncludesApproved":true}}}' > /root/.claude.json

# Wrapper script that pre-approves API key before launching claude
COPY scripts/claude-wrapper.sh /usr/local/bin/claude-wrapper
RUN chmod +x /usr/local/bin/claude-wrapper

# Copy catty binary
COPY --from=go-builder /catty-exec-runtime /usr/local/bin/

EXPOSE 8080

CMD ["catty-exec-runtime"]
