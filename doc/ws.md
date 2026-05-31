# WebSocket API

On connection the server waits for a `StatusCheck` message. If the session is valid, the server
returns a `StatusCheckResponse` with the full Music UI state — playlists, now-playing track, and the
current queue. If the session is invalid (e.g. expired or wrong ID), the server returns an
`Unverified` status.

## Implemented: Push-Based Updates

The server now pushes `StatusCheckResponse` messages to all connected clients whenever state changes:
playlist selected, track advances, skip, queue modified, or playlists added/removed.

- The server sends an initial snapshot on WebSocket connect.
- The server pushes a `StatusCheckResponse` on every state mutation.
- Each push carries a monotonically-increasing `gen` (generation) counter and a `hash` field (compact
  SHA-256 prefix) so the client can detect dropped messages and self-correct.
- The client sends `StatusCheck` as a heartbeat every 30s for connection health and as a
  self-correction fallback. The client ignores any pushed message whose `gen` is not newer than the
  last seen generation.

## Planned: Multi-User Identity

When multi-user identity is implemented, the initial handshake will include a user display name or token
so the server can attribute queue additions and enforce permissions (skip voting, balanced queue, etc.).

## StatusCheck (sent by client) Ask the server for a status

### Request

```yaml
{ "message": "StatusCheck", }
```

### Responses

```yaml
{ "message": "StatusCheckResponse",
  "status": "Verified",
  "playlists": [],
  "nowPlaying": { "playlist": "https://.../", "song": "Living La Vida Loca", },
} // or 
{ "message": "StatusCheckResponse", "status": "Unverified"}
```

## Music Selection

### Request

```
{ "message": "MusicSelect", "type": "Playlist", "playlist": "https://.../", }

{ "message": "MusicSelect", "type": "SkipSong", }

{ "message": "MusicSelect", "type": "SetSong", "song": "", }
```
