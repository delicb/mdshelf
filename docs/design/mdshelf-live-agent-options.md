> **Design exploration, August 2026.** This document surveys options for
> pushing review comments to a live agent. None of these options is
> implemented. The shipped review flow polls with `mdshelf review show`.

# Live Agent Comments for MDShelf

## Goal

A reviewer leaves a comment on a document. The linked agent receives the comment without a manual prompt.

The agent can then:

1. Read the comment thread.
2. Update the document or answer the question.
3. Add a reply with `mdshelf review address`.
4. Continue the thread when the reviewer replies or reopens the comment.

## Current MDShelf Support

MDShelf already has most of the event flow.

- `daemon.go` exposes `GET /api/watch?since=<revision>`.
- `changes.go` waits for changes and returns them at once.
- `review_http.go` publishes a `review` event after each review change.
- `mdshelf review show --json` returns the current comment threads.
- `mdshelf review address` adds an agent reply.
- The browser receives review changes through the same live feed.

The watch endpoint uses long polling. Each request waits for a change or returns after 20 seconds.

This design gives near-zero delay when a comment changes. A separate polling timer is not necessary.

The current event only reports that a review changed. It does not identify the comment or the actor.

A consumer must read the latest review state and compare it with its prior state.

## Main Gap

MDShelf can report a new comment. It cannot start a turn in an agent session.

A complete design needs:

1. A document-to-session link.
2. A process that watches review events.
3. A method that starts or queues an agent turn.
4. Event filtering that ignores agent replies.
5. A work queue that prevents concurrent file changes.

## Agent Skills

The existing MDShelf skill tells an agent how to use the review commands.

A skill cannot receive events or start a model turn. It only gives instructions after an agent starts a turn.

A skill can tell the agent to start a watcher. The agent host must provide the wake method.

The Agent Skills format is portable across several agents. Host tools and event behavior are not portable.

## MCP

### Useful MCP Features

An MDShelf MCP server could expose:

- A document comments resource.
- A `review_show` tool.
- A `review_address` tool.
- Resource change notifications.
- A blocking `wait_for_comment` tool.

MCP supports server-to-client notification streams. A client can subscribe to resource changes.

### MCP Limit

MCP does not define a common coding-agent turn control method.

The MCP host decides how it handles a resource update. It can refresh a view, add context, or ignore the update.

Standard MCP does not require the host to insert a user message and start a new agent turn.

MCP sampling is also a poor fit. Sampling creates a nested model call through the MCP client.

That call does not reliably resume the main coding-agent session. Client support and tool support are optional.

### Blocking Tool Option

A `wait_for_comment` tool could keep one MCP tool call open.

The flow would be:

1. The agent calls `wait_for_comment`.
2. The MCP server waits for a review event.
3. The tool returns the new comment.
4. The agent handles the comment.
5. The agent calls `wait_for_comment` again.

This option can work, but it is fragile. Tool timeouts differ between hosts, and the agent process must stay open.

### Claude Code Channels

Claude Code Channels provide a direct push model through an MCP extension.

A channel declares the `claude/channel` capability. It sends `notifications/claude/channel` events into an active Claude Code session.

This feature closely matches the MDShelf goal. It has these limits:

- It works only with Claude Code.
- It is a research preview.
- The Claude session must stay open.
- Custom channels need a development flag during the preview.

MCP push is therefore useful for Claude. It is not a common solution for all agents.

## ACP

The Agent Client Protocol controls coding-agent sessions.

ACP defines:

- Session creation and resume.
- `session/prompt` requests.
- Streamed session updates.
- Cancellation.
- Tool permission requests.
- MCP server setup for a session.

An MDShelf bridge can act as an ACP client:

```text
MDShelf review event
        |
        v
MDShelf agent bridge
        |
        v
ACP session/prompt
        |
        v
Coding agent
        |
        v
mdshelf review address
```

ACP is the closest common protocol for this use case.

Support is broad but not equal:

| Agent | ACP support |
| --- | --- |
| Gemini CLI | Native |
| Cursor CLI | Native |
| Claude | Adapter |
| Codex CLI | Adapter |
| Pi | Adapter |
| OpenCode and other agents | Native or listed integration |

The bridge must usually start and own the agent process. ACP cannot inject a prompt into any unrelated terminal session.

## A2A

The Agent2Agent protocol supports long tasks, event streams, task polling, and webhook notifications.

A2A is useful when MDShelf calls a remote agent service. It is too large for the first local integration.

Most local coding agents do not expose an A2A server. An extra gateway would still be necessary.

## Implementation Options

### Option 1: Pi Extension

A Pi extension can provide the smallest live integration.

The extension can:

1. Register a document with MDShelf.
2. Watch `/api/watch` in the background.
3. Read changed comments.
4. Call `pi.sendMessage()` with `triggerTurn: true`.
5. Queue the message as a follow-up when the agent is busy.
6. Stop the watcher during `session_shutdown`.

The current MDShelf skill can handle the rest of the flow.

This option keeps the original agent context and terminal. It only works with Pi.

### Option 2: Skill and JSONL Watch Command

Add a command such as:

```sh
mdshelf review watch --jsonl /absolute/path/document.md
```

It could emit:

```json
{
  "type": "comment.created",
  "documentId": "24-character-id",
  "reviewRevision": 4,
  "commentId": "comment_24-character-id"
}
```

The skill can start this command with a host background-process tool.

This Pi setup can wake the agent after a matching process event. Other hosts might not support that behavior.

The JSONL command is still useful as a common input for other bridges.

### Option 3: ACP Supervisor

Create a separate `mdshelf-agent` process.

The supervisor can:

1. Watch MDShelf review events.
2. Start an ACP agent process.
3. Keep the ACP session ID.
4. Send a prompt for each reviewer event.
5. Handle tool permission requests.
6. Track agent status.
7. Write replies through the MDShelf CLI or API.

This is the best portable design. Native ACP support can be used where available.

Adapters can support agents without native ACP support.

### Option 4: Agent Process Per Comment

A watcher can start or resume a headless agent when a comment arrives.

Examples include:

- Claude print mode with session resume.
- Codex non-interactive mode with session resume.
- Gemini headless mode.
- Pi RPC mode.

This approach works after the original terminal closes.

It also has clear costs:

- Each run has startup cost.
- Session context can be smaller.
- Each agent uses different command options.
- Permission handling is harder.
- Concurrent runs can change the same file.

Use one queue for each document or repository. Batch comments that arrive close together.

### Option 5: Agent-Specific Drivers

A common bridge can use the best control method for each agent.

| Agent | Preferred control method |
| --- | --- |
| Pi | Extension API, SDK, or RPC mode |
| Claude Code | MCP Channel or Agent SDK |
| Codex | Codex app-server |
| Gemini CLI | ACP |
| Cursor CLI | ACP |

This design gives better agent support. It also needs more code than one ACP-only bridge.

## Recommended Plan

### Stage 1: Prove the Flow with Pi

1. Add `mdshelf review watch --jsonl`.
2. Emit only reviewer actions.
3. Support comment creation, reviewer replies, and reopen actions.
4. Add a small Pi extension or use the Pi process wake feature.
5. Queue one agent follow-up after comments arrive.
6. Use the current skill to read, change, and address comments.
7. Keep comment resolution under reviewer control.

This stage uses the current MDShelf event feed and needs few changes.

### Stage 2: Add a Portable Supervisor

Build a separate bridge with:

- ACP as the main agent driver.
- Pi RPC and Codex app-server fallback drivers.
- Claude Channels as an optional direct driver.
- A one-shot process driver for headless agents.
- A document-to-session map.
- One sequential work queue for each repository.
- Event deduplication with comment and reply IDs.

Keep agent launch code outside the MDShelf daemon. The daemon must not manage model credentials or agent permissions.

## Event Design

The watch command should report reviewer events, not all review writes.

Useful event types are:

- `comment.created`
- `comment.reviewer_replied`
- `comment.reopened`

The bridge should ignore:

- Agent replies.
- Reviewer resolve actions.
- Its own status updates.

A reconnect should return a full review snapshot. The bridge can deduplicate work with comment IDs and reviewer reply IDs.

## Safety Rules

Automatic agent wake increases the effect of each comment.

The bridge must:

- Treat each comment as untrusted input.
- Watch only documents that the user attaches.
- Never approve tool permissions automatically.
- Run one write-capable job for each repository.
- Ignore agent-authored review events.
- Address a comment only after successful work.
- Recover after daemon or agent restart.
- Prevent repeated work after reconnect.
- Batch comments that arrive during an active turn.

## Key Product Choice

The design depends on the required lifetime.

Use a Pi extension or Claude Channel when the original agent session stays open.

Use an ACP supervisor or a resumable headless worker when comments must work after the terminal closes.

## Sources

- [Agent Skills specification](https://agentskills.io/specification)
- [MCP resources](https://modelcontextprotocol.io/specification/2025-11-25/server/resources)
- [MCP subscriptions](https://modelcontextprotocol.io/specification/2026-07-28/basic/patterns/subscriptions)
- [MCP sampling](https://modelcontextprotocol.io/specification/2026-07-28/client/sampling)
- [Claude Code Channels](https://code.claude.com/docs/en/channels-reference)
- [ACP introduction](https://agentclientprotocol.com/get-started/introduction)
- [ACP agents](https://agentclientprotocol.com/get-started/agents)
- [ACP session setup](https://agentclientprotocol.com/protocol/v2/session-setup)
- [A2A streaming and push](https://a2a-protocol.org/latest/topics/streaming-and-async/)
