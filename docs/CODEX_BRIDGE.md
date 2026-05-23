# Codex Bridge

The Codex bridge is the first Sage Nexus path for ChatGPT-plan Codex access.
It lets agents use `codex/gpt-5.5` through a local Codex CLI session
instead of an OpenAI API key.

The manager remains the tool runtime:

- Any configured agent can be switched to provider refs such as `codex/gpt-5.5`.
- Defaults can stay conservative; `AGT-sage` is the initial Codex default.
- Codex-backed agents request tools through the bridge's structured protocol.
- The manager executes approved MCP/local tools and feeds results back to Codex.
- Codex CLI native MCP access is intentionally not used for this path.

## Host Setup

Install and sign in to Codex on the Windows host:

```powershell
codex
```

The first run should prompt for ChatGPT sign-in. The root `startup.ps1` starts
and stops the bridge as part of the normal local Docker lifecycle. To start the
bridge manually:

```powershell
cd C:\Users\matta\code\sage-nexus\services\manager
go run .\cmd\codex-bridge
```

Default bridge settings:

- Listen address: `127.0.0.1:8765`
- Model: `gpt-5.5`
- Codex binary: `codex` from `PATH`, or `CODEX_BIN` if set
- Codex execution uses `--ignore-user-config` so the bridge uses local auth
  without depending on desktop plugin marketplace sync.

Useful overrides:

```powershell
$env:CODEX_BIN="C:\Users\matta\AppData\Roaming\npm\codex.cmd"
$env:CODEX_BRIDGE_LISTEN_ADDR="0.0.0.0:8765"
$env:CODEX_DEFAULT_MODEL="gpt-5.5"
go run .\cmd\codex-bridge
```

## Docker Setup

The manager container reads:

```text
CODEX_BRIDGE_URL=http://host.docker.internal:8765
```

That default is already wired in `docker-compose.yml`. Start the stack after
the host bridge is listening:

```powershell
cd C:\Users\matta\code\sage-nexus
docker compose up --build
```

Check status:

```powershell
Invoke-RestMethod http://localhost:8090/providers/codex/status
```

If `gpt-5.5` is not available to the signed-in Codex CLI session, the bridge
returns that error directly. Sage Nexus does not silently fall back to Copilot
for `codex/gpt-5.5`.

## Smoke Test

1. Start the bridge.
2. Start Docker compose.
3. Open `http://localhost:5174/chat`.
4. Use Sage Auto or Sage Only, or switch a worker on `/models`.
5. Confirm task telemetry reports `codex/gpt-5.5` for the active agent.
6. For tool-capable workers, confirm tool traces still show MCP tool names.

To roll an agent back to Copilot, use the Models page to set that agent to a
Copilot model such as `gpt-4.1`, or clear the override to return to registry
defaults.
