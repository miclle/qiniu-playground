# Playground sandbox template
# Bundles Amp, Claude Code, Codex, opencode, GitHub Copilot, and code-server
# for the default playground sandbox template. Provider credentials are injected
# at runtime through environment variables such as ANTHROPIC_API_KEY,
# OPENAI_API_KEY, AMP_API_KEY, and GH_TOKEN. The image never embeds secrets.
#
# qshell should build this Dockerfile on top of the configured base template.
# Some qshell rebuild versions still resolve the Dockerfile FROM image directly,
# so keep this fallback image buildable on its own.
# Reference: https://developer.qiniu.com/las/13294/sandbox-template
FROM crp-internal.qiniucs.com/registry-1.docker.io/library/ubuntu:22.04

# ---------- Base tools layer ----------
# Node.js 22 powers agent CLIs, the code-server extension host, and frontend tools.
# git / ripgrep are required for agent search and version-control workflows.
# gh is used by the playground backend when creating pull requests in sandboxes.
# vim / less / tree / jq / zip / unzip cover common editing, inspection, and archive tasks.
# procps / lsof / nc / dig / ping support process, port, and network debugging.
# locales avoids LC_ALL warnings in code-server on some vendor base images.
# python3 / sudo / user setup make the fallback Docker base behave like the base template.
# The base template may ship Node.js 20 plus npm/pnpm/yarn under /usr/local.
# Because /usr/local/bin wins over /usr/bin in PATH, remove the old binaries
# before installing Node.js 22 so the runtime matches the installed package
# version.
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        git \
        ripgrep \
        vim \
        jq \
        less \
        tree \
        unzip \
        zip \
        procps \
        lsof \
        netcat-openbsd \
        dnsutils \
        iputils-ping \
        ca-certificates \
        curl \
        gzip \
        gnupg \
        locales \
        python3 \
        python3-pip \
        sudo \
    && mkdir -p -m 755 /etc/apt/keyrings \
    && rm -f /usr/local/bin/node \
             /usr/local/bin/nodejs \
             /usr/local/bin/npm \
             /usr/local/bin/npx \
             /usr/local/bin/corepack \
             /usr/local/bin/pnpm \
             /usr/local/bin/pnpx \
             /usr/local/bin/yarn \
             /usr/local/bin/yarnpkg \
    && rm -rf /usr/local/lib/node_modules \
              /usr/local/include/node \
    && curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
    && curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
        | gpg --dearmor -o /etc/apt/keyrings/githubcli-archive-keyring.gpg \
    && chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
        > /etc/apt/sources.list.d/github-cli.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends \
        nodejs \
        gh \
    && if ! id -u user >/dev/null 2>&1; then useradd -m -s /bin/bash user; fi \
    && echo 'user ALL=(ALL) NOPASSWD:ALL' > /etc/sudoers.d/user \
    && chmod 0440 /etc/sudoers.d/user \
    && rm -rf /var/lib/apt/lists/* \
    && locale-gen en_US.UTF-8

# ---------- Agent CLI layer ----------
# Install Amp, Claude Code, Codex, opencode, and common frontend tooling at
# @latest so each template rebuild picks up current upstream releases.
RUN npm install -g \
        @sourcegraph/amp@latest \
        @anthropic-ai/claude-code@latest \
        @openai/codex@latest \
        opencode-ai@latest \
        pnpm@latest \
        tsx@latest \
        vite@latest \
    && npm cache clean --force

# ---------- code-server layer ----------
# Install the official standalone release at build time. This keeps code-server
# preinstalled while avoiding npm postinstall instability in remote builders.
RUN set -eux; \
    version="$(curl --http1.1 --connect-timeout 20 --max-time 120 --retry 5 --retry-delay 3 --retry-all-errors -fsSL https://api.github.com/repos/coder/code-server/releases/latest | jq -r '.tag_name | ltrimstr("v")')"; \
    arch="$(dpkg --print-architecture)"; \
    case "$arch" in \
        amd64|arm64) code_server_arch="$arch" ;; \
        *) echo "unsupported architecture: $arch" >&2; exit 1 ;; \
    esac; \
    tmp="$(mktemp -d)"; \
    curl --http1.1 --connect-timeout 20 --max-time 300 --speed-time 60 --speed-limit 1024 --retry 5 --retry-delay 3 --retry-all-errors \
        -fsSLo "$tmp/code-server.tgz" \
        "https://github.com/coder/code-server/releases/download/v${version}/code-server-${version}-linux-${code_server_arch}.tar.gz"; \
    rm -rf /usr/local/lib/code-server; \
    tar -C "$tmp" -xzf "$tmp/code-server.tgz"; \
    mv "$tmp/code-server-${version}-linux-${code_server_arch}" /usr/local/lib/code-server; \
    ln -sf /usr/local/lib/code-server/bin/code-server /usr/local/bin/code-server; \
    rm -rf "$tmp"; \
    code-server --version

# Preinstall MCP servers maintained by Anthropic so Claude Code and opencode can
# consume them without per-sandbox setup.
RUN npm install -g \
        @modelcontextprotocol/server-filesystem@latest \
        @modelcontextprotocol/server-github@latest \
        @modelcontextprotocol/server-memory@latest \
        @modelcontextprotocol/server-sequential-thinking@latest \
    && npm cache clean --force

# Claude Code checks official service connectivity before the first interactive
# launch. Preseed local onboarding state so third-party Anthropic-compatible
# gateway setups do not get blocked by that check. No API keys or base URLs are
# written here; credentials must still be injected at runtime.
RUN mkdir -p /home/user \
    && printf '%s\n' \
        '{' \
        '  "theme": "dark",' \
        '  "hasCompletedOnboarding": true,' \
        '  "lastOnboardingVersion": "2.1.198",' \
        '  "hasTrustDialogAccepted": true' \
        '}' \
        > /home/user/.claude.json \
    && chown user:user /home/user/.claude.json

# GitHub Copilot CLI is installed as a gh extension and authenticates with
# GH_TOKEN at runtime. gh extension install needs a writable home, so run it as
# user. Do not fail the image build if Copilot is unavailable for the account or
# region used by the builder.
RUN sudo -u user bash -lc 'gh extension install github/gh-copilot' || true

# ---------- Language runtime layer ----------
# The target users may open any GitHub repository, so use a layered runtime strategy:
#
#   1. Bake in base languages: Node 22, Python from the base template, and Go.
#      The Go toolchain is around 150 MB and gives Go repositories a useful
#      out-of-the-box editing and build baseline.
#   2. Let mise install the rest. It is asdf-compatible and a small standalone
#      binary. Repositories with .mise.toml, .tool-versions, or .nvmrc can pull
#      their declared runtime versions without baking Rust, JDK, or .NET into
#      every sandbox image.
#
# Do not preinstall every mise-managed runtime. Developer projects use different
# versions, so a global preinstall would often mismatch repository declarations,
# waste image space, and still require a second install.
RUN set -eux; \
    GO_VERSION=1.23.4; \
    arch="$(dpkg --print-architecture)"; \
    case "$arch" in \
        amd64|arm64) go_arch="$arch" ;; \
        *) echo "unsupported architecture: $arch" >&2; exit 1 ;; \
    esac; \
    curl --retry 5 --retry-delay 3 --retry-all-errors -fsSLo /tmp/go.tgz \
        "https://dl.google.com/go/go${GO_VERSION}.linux-${go_arch}.tar.gz"; \
    rm -rf /usr/local/go; \
    tar -C /usr/local -xzf /tmp/go.tgz; \
    rm -f /tmp/go.tgz; \
    ln -sf /usr/local/go/bin/go /usr/local/bin/go; \
    ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt; \
    go version

# mise activates repository-declared runtimes from .mise.toml / .tool-versions
# when users cd into a repo. Install it globally; user shells can eval the hook
# when needed.
RUN (curl --http1.1 --connect-timeout 20 --max-time 120 --retry 5 --retry-delay 3 --retry-all-errors -fsSLo /tmp/mise-install.sh https://mise.run \
        && MISE_INSTALL_PATH=/usr/local/bin/mise \
            MISE_INSTALL_FROM_GITHUB=1 \
            MISE_INSTALL_EXT=tar.gz \
            sh /tmp/mise-install.sh \
        && rm -f /tmp/mise-install.sh \
        && mise --version) \
    || (rm -f /tmp/mise-install.sh && echo "warning: mise install failed; continuing without mise")

# ---------- Finalization ----------
# Keep the default working directory aligned with the base image. The playground
# backend clones repositories into /home/user/repo.
WORKDIR /home/user

# Build-time smoke checks. Do not fail for CLIs that require runtime credentials
# before they can start cleanly.
RUN (amp --version || true) \
    && (claude --version || true) \
    && (codex --version || true) \
    && (opencode --version || true) \
    && code-server --version \
    && gh --version \
    && vim --version | head -n 1 \
    && jq --version \
    && rg --version \
    && node --version \
    && python3 --version \
    && go version \
    && (mise --version || true)
