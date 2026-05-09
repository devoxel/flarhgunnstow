# WebSocket API

On connection the server waits for a `StatusCheck` message. If the session is valid, the server
returns a `StatusCheckResponse` with the full Music UI state — playlists, now-playing track, and the
current queue. If the session is invalid (e.g. expired or wrong ID), the server returns an
`Unverified` status.

## Planned: Push-Based Updates

Currently the client polls with `StatusCheck` every 600ms. The plan is to move to push-based updates
where the server sends `StatusCheckResponse`-equivalent messages whenever state changes, and the client
only sends `StatusCheck` as a periodic heartbeat/health-check (~every 30s).

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
