# Playground Sandbox Template

Default sandbox template for playground. It builds on the default sandbox base
template and preinstalls agent CLIs, Node.js, Go, common developer tools, and
`code-server`.

The template does not start Jupyter. The playground backend uses the sandbox
Commands API to clone the repository and start `code-server`; it does not depend
on notebook services.

## Image Contents

| Component | Version | Notes |
| --- | --- | --- |
| Base template | `base` | Default sandbox base template without Jupyter |
| Agent CLI | `latest` | Amp、Claude Code、Codex、opencode、GitHub Copilot |
| IDE | standalone latest | Installed during image build |
| Language runtimes | Node.js 22.x、Go 1.23.4、Python | Covers common Web / Go / Python repositories |
| Developer tools | apt / npm latest | `git`、`ripgrep`、`gh`、`pnpm`、`tsx`、`vite`、common debugging tools |
| MCP servers | `latest` | filesystem、github、memory、sequential-thinking |
| mise | best-effort latest | Installs additional runtimes from repository declarations; skipped if GitHub downloads fail during build |

> The image does not embed API keys. Credentials are injected through `envs`
> when creating the sandbox.

> The template preseeds Claude Code's local onboarding state. When
> `ANTHROPIC_BASE_URL` points at a third-party Anthropic-compatible gateway,
> interactive `claude` sessions do not get blocked by the official service
> connectivity check. Model requests are still controlled by runtime
> environment variables.
>
> Anthropic-compatible providers such as DeepSeek may use model names that are
> not built into Claude Code. In that case, sandbox Claude Code must support
> `ANTHROPIC_CUSTOM_MODEL_OPTION`. If local Claude works but sandbox AgentRun
> reports that the selected model is unavailable, rebuild the template with
> `--no-cache`, confirm `claude --version` has reached the version you verified
> locally, and set `ai.anthropic.default_model: deepseek-v4-pro` plus
> `ai.anthropic.small_fast_model: deepseek-v4-flash` in the playground config.

## Build With qshell

Template builds use the `qiniu/qshell` sandbox template command and authenticate
with `QINIU_API_KEY`.

`qshell.sandbox.toml` uses the stable `name = "playground"` by default instead
of committing an environment-specific `template_id`. qshell looks up an existing
template by name, rebuilds it when found, and creates one when it does not exist.

```bash
cd templates/playground

# Build and wait for completion.
./build-template.sh

# Skip cache when upgrading code-server or agent CLIs.
./build-template.sh --no-cache
```

`build-template.sh` passes the resource and Dockerfile parameters explicitly,
while `from_template = "base"` is read from `qshell.sandbox.toml`. Equivalent
command from this directory:

```bash
qshell sandbox template build --name playground --dockerfile ./Dockerfile --path . --cpu 4 --memory 8192 --wait
```

## Connect To The playground Backend

After a successful build, query or record the resulting `template_id`, then set
it in `cmd/app/config.local.yaml`:

```yaml
sandbox:
  provider: aone
  api_key: "<your aone API key>"
  endpoint: "https://cn-yangzhou-1-sandbox.qiniuapi.com"
  template_id: "<template_id from the build>"
```

After that, playground started by `task dev` creates sandboxes from this
template. `code-server`, git clone workflows, and agent CLIs are available
immediately.

## Verify In A Sandbox

```bash
code-server --version
node --version
go version
mise --version
claude --version
codex --version
amp --version
```

## Language Runtime Strategy

The target users may open any GitHub repository, so the image cannot preinstall
every language ecosystem. Use a layered strategy:

| Layer | Contents | Reason |
| --- | --- | --- |
| Baked into image | Node 22、Python from base、Go 1.23 | Common and reasonably small; typical editor and build workflows work out of the box |
| Installed by mise | Rust / JDK / Ruby / .NET / multiple Node-Go-Python versions | Keeps the image small and installs versions declared by each repository |

When the repository root contains `.mise.toml`, `.tool-versions`, `.nvmrc`, or
`.python-version`, users can run `mise install` after `cd repo` to pull declared
runtimes. The first install usually takes 30 seconds to 2 minutes; later starts
are fast when the sandbox volume cache hits.

## Resource Profile

`qshell.sandbox.toml` defaults to `cpu_count = 4 / memory_mb = 8192`.
This is a comfortable default for one active repo IDE session with code-server,
language servers, terminal commands, and occasional agent CLI runs:

- Idle code-server uses around 250 MB, and one language server can push usage
  past 1.5 GB.
- Agent CLIs are mostly idle until invoked, but active Node processes can use
  hundreds of MB.
- 8 GB leaves room for normal IDE, language server, test/build, and agent CLI
  workflows. Use a larger profile such as 6C12G or 8C16G for large monorepos,
  heavier build jobs, or multiple active agents.
