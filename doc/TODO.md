## Wanted Text API

```
playlist add name [url] | add a playlist
  or pl
playlist [list]         | list playlists
play [playlist]         | play playlist
play [url]              | queue song to ephemeral playlist ("Now Playing")
play [playlist_url]     | queue all songs in playlist to ephemeral playlist
playnext [url]          | add song to ephermal playlist after currently playing song
 or play_next
 or pn
save_current [name]     | saves all songs in ephemeral playlist to a new playlist
remove N                | remove Nth song in ephemeral playlist
remove N..M             | remove a group of songs from playlist
remove_all [user]       | clears all of a certain person's additions to the playlist (maybe shouldn't add this one)
ui                      | create a UI session where you can control this stuff on your browser
  or party
  or ...
queue balanced          | set queue mode to balanced, round robin style playing.
queue normal            | set queue mode to expected
pause                   | if playing, pause
play                    | if paused, play
clear                   | clear now playing
bounce                  | bot exits the channel
queue                   | list all songs
  or q
```
