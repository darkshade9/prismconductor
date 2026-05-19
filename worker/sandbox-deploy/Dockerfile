# PrismConductor Sandbox Container Image (Phase 2)
#
# This image is NOT used in the Phase 1 POC. It defines the container
# environment for Phase 2, when the sandbox worker spawns a real container
# to clone the repo and run the claude-code CLI.
#
# Phase 2 integration point: replace the Anthropic API call in
# SandboxSession.runPlan() with a container spawn using this image.
#
# Build:
#   docker build -t prismconductor-sandbox:latest .
#
# Required env vars at container runtime:
#   GITHUB_PAT         — for authenticated git clone
#   ANTHROPIC_API_KEY  — for claude-code CLI

FROM node:20-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    ca-certificates \
  && rm -rf /var/lib/apt/lists/*

RUN npm install -g @anthropic-ai/claude-code@latest

WORKDIR /workspace

ENTRYPOINT ["claude"]
