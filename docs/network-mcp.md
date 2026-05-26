# Network MCP Runtime

ATM currently exposes its built-in MCP tools by asking the selected agent CLI to
spawn short-lived `atm mcp ...` stdio subprocesses. That keeps the integration
simple, but it spreads active task state across process boundaries and forces
state transfer through temporary files.

This document defines the target design for an in-process network MCP runtime.
The goal is to keep active MCP state owned by the main `atm run` process while
still preserving the existing stdio subcommands as a compatibility fallback.

## Goals

- Run built-in ATM MCP tools from the main process during `run` and `exec`.
- Use the official Go MCP SDK for protocol handling and streamable HTTP
  transport.
- Bind only to loopback and protect every active MCP endpoint with an
  unguessable token.
- Keep durable outputs on disk so task reports, structured outputs, and DB
  state remain recoverable after interruption.
- Preserve existing `atm mcp check/output/db/defs` stdio commands for external
  users and older tool clients.
- Allow Codex and Claude Code to connect through network MCP configuration when
  supported.

## Non-Goals

- No public remote MCP service.
- No shared daemon outside the lifetime of the current `atm run` or `atm exec`.
- No change to the user-facing todo DSL.
- No replacement of user-declared external `/mcp` servers. User MCP configs are
  passed through as declared.

## Transport Model

The main process owns a single `NetworkMCPManager`:

```txt
atm run/exec process
  -> NetworkMCPManager
     -> listens on 127.0.0.1:0
     -> routes /mcp/{token} to an SDK mcp.Server
```

Each internal MCP exposure registers a short-lived session:

```txt
RegisterCheck(result sink)    -> http://127.0.0.1:{port}/mcp/{token}
RegisterOutput(schema, sink)  -> http://127.0.0.1:{port}/mcp/{token}
RegisterDB(dbs, readonly)     -> http://127.0.0.1:{port}/mcp/{token}
RegisterDefs(handler, refs)   -> http://127.0.0.1:{port}/mcp/{token}
```

The token is generated with `crypto/rand` and is never derived from the task
text, output path, or report id. The manager removes each registration when the
agent invocation finishes or when the run context is cancelled.

## Built-In Tools

The built-in tools keep their existing names:

- `atm_report_check`
- `atm_report_output`
- `atm_db_list`
- `atm_db_get`
- `atm_db_scan`
- `atm_db_append`
- `atm_db_set`
- `atm_db_delete`
- `atm_def_*`

The MCP SDK owns `initialize`, `tools/list`, `tools/call`, session ids, and
streamable HTTP behavior. ATM code owns only tool definitions, argument
decoding, authorization checks, and durable writes.

## Agent Configuration

Codex supports streamable HTTP MCP servers via `--url` in `codex mcp add`, and
the CLI config accepts an MCP server URL. ATM should emit Codex config using the
same server names it already uses for stdio:

```toml
mcp_servers.atm_output.url = "http://127.0.0.1:{port}/mcp/{token}"
```

Claude Code supports HTTP MCP servers in `.mcp.json` style config:

```json
{
  "mcpServers": {
    "atm_output": {
      "type": "http",
      "url": "http://127.0.0.1:{port}/mcp/{token}"
    }
  }
}
```

If a tool adapter cannot use network MCP, ATM falls back to the existing stdio
server spec for that adapter.

## State Ownership

Active state lives in memory:

- Check result sink for the current condition evaluation.
- Output schema and result sink for the current structured output.
- DB runtime permissions for the current task block.
- Definition handler bound to the current engine and runtime variables.

Durable state still lives on disk:

- Check and output result files remain the handshake for existing runner code.
- DB files remain JSON documents under the configured `.atm` or output
  directory.
- Task reports and generated result blocks remain unchanged.

This split keeps crash recovery and audit behavior stable while avoiding
separate MCP subprocesses for active protocol state.

## Lifecycle

1. `engine.Run` starts or reuses a manager scoped to the current process.
2. Before launching an agent, the runner registers only the internal MCP tools
   needed for that invocation.
3. The runner emits network MCP config for Codex or Claude Code.
4. The agent connects to the loopback URL and calls tools through streamable
   HTTP.
5. When the agent command returns, the runner unregisters the session tokens.
6. When the engine exits, the manager shuts down the HTTP server.

## Compatibility

The old stdio commands remain:

```sh
atm mcp check -result-file ...
atm mcp output -result-file ... -schema-file ...
atm mcp db -config-file ...
atm mcp defs -config-file ...
```

They continue to serve users that explicitly call those commands and provide a
fallback for older agent clients.

## Safety

- Listen only on `127.0.0.1`.
- Use a random token in every endpoint path.
- Reject requests for unknown or expired tokens.
- Unregister endpoints as soon as the owning agent invocation completes.
- Keep DB access checks in the tool handler, not in the client config.
- Never expose network MCP URLs in todo documents or durable report blocks.

## Implementation Plan

1. Add the official Go MCP SDK and raise the Go version to a supported version.
2. Add a `NetworkMCPManager` with streamable HTTP routing by token.
3. Factor existing check/output/db tool handlers so they can be used by both
   stdio and network transports.
4. Add a definition MCP registration path from the engine, because definition
   calls need access to the active `Engine`.
5. Extend internal MCP server specs to support either stdio command config or
   HTTP URL config.
6. Emit HTTP MCP config for Codex and Claude Code, with stdio fallback retained.
7. Keep existing stdio tests and add network manager tests for tool listing,
   tool calls, token rejection, and unregister behavior.
