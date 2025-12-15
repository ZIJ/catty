FROM golang:1.25-bookworm AS go-builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum* ./
RUN go mod download

# Copy source
COPY . .

# Build
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /catty-exec-runtime ./cmd/catty-exec-runtime

# Runtime image - use full Debian with Node.js for a complete environment
FROM node:22-bookworm

# Install useful tools that an AI agent might need
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    ca-certificates \
    curl \
    git \
    jq \
    less \
    man-db \
    openssh-client \
    procps \
    ripgrep \
    sudo \
    tree \
    unzip \
    vim \
    wget \
    zip \
    && rm -rf /var/lib/apt/lists/*

# Install Claude Code globally
RUN npm install -g @anthropic-ai/claude-code

# Pre-create Claude Code config directories and settings
RUN mkdir -p /root/.claude/projects /root/.claude/todos /root/.claude/statsig

# Pre-populate claude.json to skip first-run onboarding prompts and pre-approve settings
# - hasCompletedOnboarding: skip onboarding wizard
# - projects["/"]: pre-trust the root directory
# - projects["/workspace"]: pre-trust the workspace directory (where uploaded files go)
# - customApiKeyResponses.approved: empty array, will be populated at runtime via wrapper
RUN echo '{"numStartups":1,"installMethod":"npm","autoUpdates":false,"hasCompletedOnboarding":true,"lastOnboardingVersion":"1.0.0","projects":{"/":{"allowedTools":[],"hasTrustDialogAccepted":true,"hasClaudeMdExternalIncludesApproved":true},"/workspace":{"allowedTools":[],"hasTrustDialogAccepted":true,"hasClaudeMdExternalIncludesApproved":true}}}' > /root/.claude.json

# Pre-create workspace directory
RUN mkdir -p /workspace

# Wrapper script that pre-approves API key before launching claude
COPY scripts/claude-wrapper.sh /usr/local/bin/claude-wrapper
RUN chmod +x /usr/local/bin/claude-wrapper

# Copy catty binary
COPY --from=go-builder /catty-exec-runtime /usr/local/bin/

EXPOSE 8080

CMD ["catty-exec-runtime"]
