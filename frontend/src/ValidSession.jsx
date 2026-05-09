import { useState } from 'react'
import PlayerBar from './Player'

function Playlist({ playlists, handlePlaylist }) {
  return (
    <div>
      {playlists.map((pl) => (
        <div className="Playlist" key={pl.url ?? pl.title}>
          <p className="Playlist-Title">
            <a onClick={() => handlePlaylist(pl.title)} className="Playlist-Link">
              {pl.title}
            </a>
          </p>
        </div>
      ))}
    </div>
  )
}

function PlaylistCategory({ name, playlists, handlePlaylist }) {
  const [show, setShow] = useState(true)
  return (
    <div className="PlaylistCategory">
      <h4 className="PlaylistCategory-Title">{name}</h4>
      <div className="PlaylistCategory-Folder" onClick={() => setShow((s) => !s)}>
        {show ? '↓' : '↑'}
      </div>
      {show && <Playlist handlePlaylist={handlePlaylist} playlists={playlists} />}
    </div>
  )
}

export default function ValidSession({ playlists, playing, current_playlist, handlePlaylist, handleSkip }) {
  const categories = playlists.reduce((acc, p) => {
    ;(acc[p.category] ??= []).push(p)
    return acc
  }, {})

  return (
    <div className="ValidSession-body">
      {Object.entries(categories).map(([name, items]) => (
        <PlaylistCategory
          key={name}
          name={name}
          playlists={items}
          handlePlaylist={handlePlaylist}
        />
      ))}
      <PlayerBar
        playing={playing}
        current_playlist={current_playlist}
        handleSkip={handleSkip}
      />
    </div>
  )
}
