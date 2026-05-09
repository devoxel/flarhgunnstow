import { useState } from 'react'

function Player({ playing, current_playlist, handleSkip }) {
  const [showQueue, setShowQueue] = useState(false)

  return (
    <div className="Player">
      <div className="Player-NowPlaying">
        <span className="Player-Note">♫&nbsp;</span>
        <span className="Player-Text">Now Playing: </span>
        <span className="Player-Name">{playing.name}&nbsp;</span>
        <span className="Player-Sep"> - </span>
        <span className="Player-Artist">{playing.artist}</span>
      </div>

      <button type="button" className="Player-SkipButton" onClick={handleSkip}>
        &gt;&gt;
      </button>

      <div className="Player-PopUp">
        <button
          type="button"
          className="Player-PopUpButton"
          onClick={() => setShowQueue((s) => !s)}
        >
          Queue
        </button>
        {showQueue && (
          <div className="Player-PopUpContent">
            {current_playlist.map((track, i) => (
              <div className="Player-Track" key={i}>
                <span className="Player-TrackName">{track.name}&nbsp;</span>
                <span className="Player-TrackSep"> - </span>
                <span className="Player-TrackArtist">{track.artist}</span>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

export default function PlayerBar({ playing, current_playlist, handleSkip }) {
  return (
    <div className="PlayerBar">
      {current_playlist.length > 0 ? (
        <Player playing={playing} current_playlist={current_playlist} handleSkip={handleSkip} />
      ) : (
        <div className="Player-Empty" />
      )}
    </div>
  )
}
