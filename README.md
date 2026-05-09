# flarhgunnstow

- Self-hosted Discord music bot with a browser-based Web UI.
- Ships with playlists for D&D.

## Usage

This bot is only available for you to use self hosted.

## Features

- **Multi-user Web UI** —everyone can use the session URL to add tracks & playlists.
- **Playlist management** — organize tracks into named, categorized playlists.
- **Discord text commands** — `;play`, `;skip`, `;queue`, `;create` for when you don't want to open
  the browser.
- **Local file support** — drop audio files into `videocache/` and they appear automatically as
  playlists.
- **YouTube + Spotify** — resolve tracks from YouTube URLs/search and Spotify playlists (Spotify
  import partially implemented).

## Layout

This project is in two main sections:

- `backend` - contains the discord bot
- `frontend` - contains the frontend UI code

The frontend code is built first (see `run.sh`) and that is hosted by the bot.

## Hosting

- Set `$DISCORD_TOKEN` and use `run.sh` to start the bot.
- You need to have `ffmpeg`, `youtube-dl`, the go toolchain, and the nodejs toolchain.

I'll eventually make a binary release but for now no dice.

### Websocket config

Like any websocket site, you need to make sure upgrading works. Here's a reverse proxy nginx
example:

```nginx
server {
        server_name $YOUR_SERVER_HERE;

        location /ws {
                proxy_pass http://127.0.0.1:9116;
                proxy_http_version 1.1;
                proxy_set_header Upgrade $http_upgrade;
                proxy_set_header Connection "Upgrade";
                proxy_set_header Host $host;
        }

        location / {
                proxy_pass http://127.0.0.1:9116;
        }
}
``
```
