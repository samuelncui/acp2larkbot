# Lark Module Design

This document describes the current Lark-side implementation.

## Gateway Responsibilities

The `lark` package currently provides:

- websocket event intake
- outbound text message creation
- outbound text message update
- outbound card creation and patch
- CardKit streaming card operations
- self-message tracking

## Inbound Events

The gateway currently subscribes to `P2MessageReceiveV1` websocket events.

The event adapter extracts:

- `event_id`
- `message_id`
- `chat_id`
- `chat_type`
- `message_type`
- `sender_id`
- `sender_type`
- `thread_id`
- mention IDs
- text payload

The current router only handles text messages.

## Self-Message Filter

The self-message filter is an in-memory guard against loops.

It ignores events when:

- `sender_type == bot`
- `sender_app_id == lark.ignore.self_app_id`
- the event is a message update and updates are configured to be ignored
- the event is a card event and card events are configured to be ignored
- the `message_id` matches a recently remembered self-generated message

The remembered message set has:

- TTL based expiration
- a maximum size cap
- FIFO-style pruning via an internal list

## Dedupe Boundary

Lark dedupe is **not** handled inside the filter. It is handled at the app layer using the state store.

The current order is:

1. self-message filter
2. dedupe store check
3. router/authz

## Outbound Rendering Modes

The application can instantiate one of two renderers:

- `StreamingRenderer`
- `CardStreamingRenderer`

### Text streaming renderer

Behavior:

1. Send an initial text message with `思考中...`
2. Accumulate ACP deltas in a local buffer
3. Periodically update the same Lark message
4. On finish, write the final text
5. On error, append a failure message and flush

If `UpdateText` fails and fallback is enabled as `append_messages`, the renderer sends a new text message instead.

### Card streaming renderer

Behavior:

1. Create a CardKit card with `streaming_mode: true`
2. Send the card into the target chat
3. Stream updates to the markdown element via CardKit sequence numbers
4. Finalize the card by setting `streaming_mode: false`

This is the only renderer mode that currently uses CardKit streaming APIs.

## Important Current Notes

The config surface is broader than the current implementation.

What is actually used today:

- `update_interval`
- `min_update_chars`
- `max_update_chars`
- `max_updates_per_message`
- `max_final_chars`
- `truncate_notice`
- `fallback`

What is currently **not** implemented in renderer logic:

- rate limit enforcement
- retry/backoff handling
- `Retry-After` handling for 429
- stream duration timeout enforcement
- fallback message count tracking

## Reply Targeting

The renderer API includes `ReplyToMessageID`, but the current gateway implementation does not use it to build threaded or reply-style outbound messages.

Messages are sent directly to the chat ID.
