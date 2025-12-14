# Fly TUI Streaming v1.0 (Go) — Dev-first local control plane

Runs claude code (and potentially other agents eg codex) remotely on the server in isolated fly machines, streaming PTY to the user so that it feels to them that they are working with a local agent.

## 1) Scope and milestones

### Milestone 0 (build this first)

**Goal:** You can run `tui` locally, spin up an isolated Fly Machine per session, and attach to it with “local-terminal feel”.

* ✅ Executor runtime **deployed on Fly** (a Fly app that hosts per-session Machines).
* ✅ API server runs **locally** (localhost) and calls **Fly Machines API** over the public endpoint.
* ✅ CLI runs **locally** and connects **directly** to the executor over WebSocket (not via API).
* ✅ No user auth / accounts / OAuth / etc.
* ✅ Still use a **per-session capability token** (random secret) to prevent arbitrary internet connections.

### Milestone 1 (later)

Deploy the API server to Fly:

* Switch Machines API base URL to Fly’s **internal** endpoint when running on Fly.
* Still no user auth required.

### Milestone 2 (later)

Add real auth + multi-tenancy:

* User identity, quotas, billing, orgs, etc.
* Replace capability token with signed JWTs (or keep JWTs but add user claims).

---

## 2) Recommended stack (all Go)

### Why this stack

You want:

* small binaries,
* fast iteration,
* “infra-friendly” reliability,
* and libraries that are widely adopted.

### CLI (`tui`)

* **Go** + `spf13/cobra` for commands/subcommands (de-facto in Go CLIs). ([GitHub][1])
* `golang.org/x/term` for raw terminal mode (`MakeRaw` / `Restore`). ([Go Packages][2])
* WebSocket client: `github.com/coder/websocket` (current maintained home of the nhooyr websocket lib; minimal/idiomatic). ([GitHub][3])

### Local API server (`tui-api`)

* **Go** + `net/http` + `go-chi/chi` router (lightweight, idiomatic). ([GitHub][4])
  Keep it boring: JSON in/out, a handful of endpoints.
* Calls Fly Machines API using `net/http` client.

### Executor runtime (`tui-exec-runtime`) (runs inside each Fly Machine)

* **Go** + `net/http`
* PTY: `github.com/creack/pty` (standard Go PTY lib). ([GitHub][6])
* WebSocket server: `github.com/coder/websocket` ([GitHub][3])

---

## 3) Architecture (Milestone 0)

### Components

1. **Local API server** (`tui-api`, runs on `http://127.0.0.1:4815`)

   * Creates per-session Fly Machines via the Machines API.
   * Returns `machine_id`, `connect_url`, `connect_token`.
   * Stores session records locally (file-based).

2. **Local CLI** (`tui`)

   * Calls local API server.
   * Connects directly to executor over WebSocket.
   * Streams terminal bytes bidirectionally.

3. **Fly executor app** (`tui-exec` Fly app)

   * Hosts many executor Machines (one per session).
   * Each Machine runs `tui-exec-runtime`, which spawns the agent CLI in a PTY.

---

## 4) Fly integration details you must follow

### 4.1 Machines API base URL differs local vs on-Fly

Fly documents two base URLs for the Machines API:

* **Internal**: `http://_api.internal:4280` (only from within Fly’s private network)
* **Public**: `https://api.machines.dev` (from outside Fly)

**Milestone 0 uses the public base URL** (local dev machine is outside Fly):
`FLY_MACHINES_API_BASE=https://api.machines.dev`

**Milestone 1 (API deployed on Fly) uses internal**:
`FLY_MACHINES_API_BASE=http://_api.internal:4280`

### 4.2 Directly routing the CLI to the correct Machine

Your CLI must force the request to land on the right executor Machine via:

`fly-force-instance-id: <machine_id>`

This is the simplest approach for a **CLI** client (browsers can’t set WS headers, but that’s not your MVP). Fly’s docs explicitly define `fly-force-instance-id` and its behavior (no fallback if unavailable).

### 4.3 Listing sessions without a DB (metadata filters)

Fly Machines list endpoint supports `metadata.{key}=value` filtering. ([Fly][7])

In Milestone 0 you can keep local state, but **you should still tag machines with metadata** so Milestone 1 is easy.

Example:

* `metadata.project=tui`
* `metadata.session=<uuid>`
* `metadata.owner=<local-username>` (placeholder until auth exists)

### 4.4 One-time Fly networking requirement (shared IPv4)

If you create apps with **public services in Machine config** via Machines API, Fly says you need to allocate a shared IPv4: `fly ips allocate-v4 --shared`. ([Fly][8])

---

## 5) Session model and lifecycle

### Session identifier

* `session_id` (client-facing) = generated UUID (Milestone 0)
* `machine_id` (Fly identifier) returned by Machines API

Store a local mapping: `session_id -> machine_id -> connect_token -> created_at`.

### Lifecycle

1. CLI calls local API: `POST /v1/sessions`
2. Local API creates Fly Machine:

   * uses Machines API: create Machine, define services, env, metadata
   * waits for ready using Machines API wait endpoint (recommended in docs) ([Fly][7])
3. Local API returns connect info.
4. CLI dials: `wss://tui-exec.fly.dev/connect` with:

   * header `fly-force-instance-id: <machine_id>`
   * header `authorization: bearer <connect_token>`
5. Executor runtime validates token, spawns agent in PTY, starts streaming.
6. On exit:

   * runtime sends exit status
   * CLI restores terminal state
7. Stop/delete:

   * CLI calls local API → local API stops/deletes the Fly machine

---

## 6) Capability token (NOT “auth”, but required even in Milestone 0)

Even without user auth, you **must** prevent randoms on the internet from attaching.

### How it works

* Local API generates a random `connect_token` per session (32+ bytes, base64url).
* API injects it into the Machine env as `CONNECT_TOKEN`.
* CLI sends it as `Authorization: Bearer <connect_token>`.
* Executor runtime checks `CONNECT_TOKEN == incoming token`.

This is intentionally simple and local-dev friendly.

**Later** you can swap this to JWTs without changing the wire shape.

---

## 7) Interfaces

## 7.1 Local API (`tui-api`) endpoints (Milestone 0)

Base: `http://127.0.0.1:4815`

### `POST /v1/sessions`

Creates a new Fly executor Machine.

Request:

```json
{
  "agent": "claude_code|codex|amp|custom",
  "cmd": ["claude", "code"],
  "region": "iad|sjc|ams|auto",
  "cpus": 1,
  "memory_mb": 1024,
  "ttl_sec": 7200
}
```

Response:

```json
{
  "session_id": "uuid",
  "machine_id": "01J…",
  "connect_url": "wss://tui-exec.fly.dev/connect",
  "connect_token": "base64url",
  "headers": {
    "fly-force-instance-id": "01J…"
  }
}
```

### `GET /v1/sessions`

Lists local-known sessions (from local file).

### `GET /v1/sessions/{session_id}`

Shows session mapping and (optional) live machine state (by querying Machines API).

### `POST /v1/sessions/{session_id}/stop`

Stops (and optionally deletes) the machine.

---

## 7.2 Executor runtime (`tui-exec-runtime`) API

### `GET /healthz`

Always `200 OK` once process booted.

### `GET /connect` (WebSocket)

* Validates `Authorization: Bearer <token>`
* Spawns PTY if first attach
* Streams bytes bidirectionally

---

## 8) Data plane protocol (WebSocket)

Keep it dead simple for v1:

### WS binary frames

* **Client → Server**: raw stdin bytes
* **Server → Client**: raw PTY output bytes

### WS text frames (JSON control)

Client → Server:

* `{"type":"resize","cols":120,"rows":40}`
* `{"type":"signal","name":"SIGINT"}`
* `{"type":"ping"}`

Server → Client:

* `{"type":"ready"}`
* `{"type":"exit","code":0,"signal":null}`
* `{"type":"pong"}`
* `{"type":"error","message":"…"}`

### Keepalive

Send ping every ~25s when idle (either direction) to avoid idle connection drops.

---

## 9) Local dev experience (minimal)

### One-time: deploy the executor app on Fly

1. Create Fly app `tui-exec`
2. Allocate shared IPv4 (needed for public services created via Machines API): ([Fly][8])

   * `fly ips allocate-v4 --shared -a tui-exec`
3. Deploy an initial placeholder Machine or just ensure app exists (your API will create Machines dynamically).

### Run locally (Milestone 0)

In one terminal:

* `FLY_API_TOKEN=…`
* `FLY_MACHINES_API_BASE=https://api.machines.dev`
* `TUI_EXEC_APP=tui-exec`
* `tui-api` (binds to `127.0.0.1:4815`)

In another terminal:

* `tui run --agent claude_code -- claude code`

---

## 10) What changes when you deploy the API to Fly (Milestone 1)

* Change Machines API base URL to internal: `http://_api.internal:4280`
* Stop using local session file as the “source of truth”

  * Instead: list Machines using `metadata.{key}` filters. ([Fly][7])
* Everything else stays the same:

  * CLI still connects directly to executor with `fly-force-instance-id`.

---

## 11) Deliverables for Milestone 0 (hand to an agent)

### Repo structure

* `cmd/tui` — CLI
* `cmd/tui-api` — local API server
* `cmd/tui-exec-runtime` — executor runtime binary (containerized)

### Required features

* API: create/wait/stop/delete machine via Machines API (public endpoint) ([Fly][7])
* Executor: PTY spawn + WS relay (`creack/pty`) ([GitHub][6])
* CLI: raw mode + WS relay + force routing header ([Go Packages][2])
* Capability token check (simple env match)

---

If you want, I can also give you:

* a **concrete Machines API “create machine” JSON payload template** (services + env + metadata),
* and a **very small Go skeleton** (3 main packages) that your agent can expand.

[1]: https://github.com/spf13/cobra?utm_source=chatgpt.com "spf13/cobra: A Commander for modern Go CLI interactions"
[2]: https://pkg.go.dev/golang.org/x/term?utm_source=chatgpt.com "term package - golang.org/x/term - ..."
[3]: https://github.com/coder/websocket?utm_source=chatgpt.com "Minimal and idiomatic WebSocket library for Go"
[4]: https://github.com/go-chi/chi?utm_source=chatgpt.com "go-chi/chi: lightweight, idiomatic and composable router for ..."
[5]: https://github.com/danielgtaylor/huma?utm_source=chatgpt.com "danielgtaylor/huma: Huma REST/HTTP API Framework for ..."
[6]: https://github.com/creack/pty?utm_source=chatgpt.com "creack/pty: PTY interface for Go"
[7]: https://fly.io/docs/machines/api/machines-resource/ "Machines · Fly Docs"
[8]: https://fly.io/docs/networking/services/?utm_source=chatgpt.com "Public Network Services · Fly Docs"
