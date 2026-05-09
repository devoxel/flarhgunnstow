import { useState, useEffect, useRef, useCallback } from 'react'
import './App.css'
import InvalidSession from './InvalidSession'
import ValidSession from './ValidSession'

export default function App() {
  const [appState, setAppState] = useState({
    validated: false,
    playlists: [],
    playing: '',
    current_playlist: [],
  })
  const socketRef = useRef(null)

  const send = useCallback((msg) => {
    if (socketRef.current?.readyState === WebSocket.OPEN) {
      socketRef.current.send(JSON.stringify(msg))
    }
  }, [])

  const handlePlaylist = useCallback((title) => {
    send({ message: 'MusicSelect', type: 'Playlist', title })
  }, [send])

  const handleSkip = useCallback(() => {
    send({ message: 'MusicSkip' })
  }, [send])

  useEffect(() => {
    const session = new URLSearchParams(window.location.search).get('s')
    let isMounted = true
    let intervalId

    function connect() {
      if (!isMounted) return

      const proto = window.location.protocol === 'https:' ? 'wss' : 'ws'
      const ws = new WebSocket(`${proto}://${window.location.host}/ws?s=${session}`)
      socketRef.current = ws

      ws.onopen = () => {
        intervalId = setInterval(() => {
          if (ws.readyState === WebSocket.OPEN) {
            ws.send(JSON.stringify({ message: 'StatusCheck' }))
          }
        }, 600)
      }

      ws.onmessage = (ev) => {
        const msg = JSON.parse(ev.data)
        if (msg.message === 'StatusCheckResponse') {
          setAppState({
            validated: true,
            playlists: msg.playlists ?? [],
            playing: msg.playing ?? '',
            current_playlist: msg.current_playlist ?? [],
          })
        }
      }

      ws.onclose = () => {
        clearInterval(intervalId)
        if (isMounted) {
          setTimeout(connect, 3000)
        }
      }

      ws.onerror = () => {
        ws.close()
      }
    }

    connect()

    return () => {
      isMounted = false
      clearInterval(intervalId)
      if (socketRef.current) {
        socketRef.current.onclose = null
        socketRef.current.close()
      }
    }
  }, [])

  return (
    <div className="App">
      {appState.validated ? (
        <ValidSession
          handlePlaylist={handlePlaylist}
          handleSkip={handleSkip}
          playlists={appState.playlists}
          playing={appState.playing}
          current_playlist={appState.current_playlist}
        />
      ) : (
        <InvalidSession />
      )}
    </div>
  )
}
