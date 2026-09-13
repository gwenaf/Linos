import { computed, effect, signal } from '@preact/signals'
import { useEffect, useRef } from 'preact/hooks'
import { connect, type Data } from './ws'
import { Answers, Qr, Results, Scores, scoreKey } from './ui'

const conn = connect('host')

const activated = signal(false)
const joinUrl = signal('')
const title = signal('')
const lobby = signal<Data>({ players: [] })
const round = signal<Data>(null)
const track = signal<Data>(null)
const live = signal(false)
const ended = signal<Data>(null)
const holder = signal<Data>(null)
const paused = signal(false)
const scores = signal<Record<string, number>>({})
const results = signal<Data>(null)
const elapsed = signal(0)

fetch('/api/join')
  .then((r) => r.json())
  .then((d) => (joinUrl.value = d.urls[0] ?? ''))

// Local track clock, mirroring the server's: it stops during pauses and while a player answers.
// It drives the countdown, the reveal steps and the hints; the server clock stays the reference for scoring.
let clockBase = 0
let clockSince: number | null = null
const clockNow = () => clockBase + (clockSince === null ? 0 : (performance.now() - clockSince) / 1000)
const clockRunning = computed(() => live.value && !paused.value && !holder.value && !ended.value)
effect(() => {
  if (clockRunning.value && clockSince === null) clockSince = performance.now()
  if (!clockRunning.value && clockSince !== null) {
    clockBase = clockNow()
    clockSince = null
  }
})
setInterval(() => (elapsed.value = clockNow()), 100)

conn.on('lobby-update', (d) => (lobby.value = d))
conn.on('configured', (d) => (title.value = d.title))
conn.on('game-start', (d) => {
  title.value = d.title
  scores.value = {}
  results.value = null
  round.value = null
})
conn.on('round-start', (d) => (round.value = d))
conn.on('track-start', (d) => {
  clockBase = 0
  clockSince = null
  elapsed.value = 0
  live.value = false
  ended.value = null
  holder.value = null
  track.value = d
})
conn.on('timer-start', () => (live.value = true))
conn.on('buzz-accepted', (d) => (holder.value = d))
conn.on('answer-result', () => (holder.value = null))
conn.on('track-end', (d) => {
  ended.value = d
  holder.value = null
})
conn.on('game-paused', () => (paused.value = true))
conn.on('game-resumed', () => (paused.value = false))
conn.on('score-update', (d) => (scores.value = { ...scores.value, [scoreKey(d)]: d.score }))
conn.on('game-end', (d) => {
  results.value = d
  track.value = null
  live.value = false
})

export default function Host() {
  if (!activated.value) {
    // Browsers only allow sound after a user gesture on the page.
    return (
      <main class="host activate" onClick={() => (activated.value = true)}>
        <h1>Linos</h1>
        <p>Cliquez pour activer l'écran de jeu et le son</p>
      </main>
    )
  }
  return (
    <main class="host">
      {paused.value && <div class="banner">Pause</div>}
      {track.value ? <TrackView /> : <LobbyView />}
      <Scores scores={scores.value} />
    </main>
  )
}

function LobbyView() {
  return (
    <>
      <h1>{title.value || 'Linos'}</h1>
      {results.value && <Results data={results.value} />}
      {joinUrl.value && <Qr url={joinUrl.value} caption="Scannez pour jouer" />}
      <ul class="players">
        {lobby.value.players.map((p: Data) => (
          <li key={p.name} class={p.ready ? 'ready' : ''}>
            {p.name}
            {p.team && <small> {p.team}</small>}
          </li>
        ))}
      </ul>
    </>
  )
}

type Reveal = { audio: boolean; video: boolean; blur: number }

function revealAt(steps: Data[] | null, t: number): Reveal {
  const r: Reveal = { audio: true, video: true, blur: 0 }
  for (const s of [...(steps ?? [])].sort((a, b) => a.at - b.at)) {
    if (s.at > t) break
    if (s.audio != null) r.audio = s.audio
    if (s.video != null) r.video = s.video
    if (s.blur != null) r.blur = s.blur
  }
  return r
}

function TrackView() {
  const t = track.value
  const video = useRef<HTMLVideoElement>(null)
  const reported = useRef(-1)
  const isImage = /\.(jpe?g|png|webp|gif|avif)$/i.test(t.media)
  const reveal = ended.value ? { audio: true, video: true, blur: 0 } : revealAt(t.reveal, elapsed.value)

  const reportStarted = () => {
    if (reported.current === t.index) return
    reported.current = t.index
    conn.send('media-started')
  }

  useEffect(() => {
    const v = video.current
    if (!v) return
    v.onloadedmetadata = () => {
      v.currentTime = t.start
      v.playbackRate = t.playbackRate
      v.play().catch(() => {})
    }
  }, [t.index])

  // The media plays unless the game is paused or someone holds the hand; after the end it keeps playing, unveiled.
  useEffect(
    () =>
      effect(() => {
        const v = video.current
        if (!v) return
        if (paused.value || holder.value) v.pause()
        else v.play().catch(() => {})
      }),
    [t.index],
  )

  const style = { filter: `blur(${reveal.blur}px)`, visibility: reveal.video ? 'visible' : 'hidden' }
  return (
    <>
      <header class="track">
        {round.value && <span>{round.value.name}</span>}
        <span>
          Piste {t.index + 1} / {t.total}
        </span>
        {live.value && !ended.value && <span class="countdown">{Math.max(0, Math.ceil(t.duration - elapsed.value))}</span>}
      </header>
      <div class="stage">
        {isImage ? (
          <img key={t.index} src={t.media} style={style} onLoad={reportStarted} />
        ) : (
          <video key={t.index} ref={video} src={t.media} muted={!reveal.audio} style={style} onPlaying={reportStarted} />
        )}
      </div>
      {holder.value && (
        <p class="holder">
          {holder.value.name}
          {holder.value.team && ` (${holder.value.team})`} a la main !
        </p>
      )}
      {!ended.value && (
        <>
          <p class="guesses">À deviner : {t.guesses.map((g: Data) => g.label).join(', ')}</p>
          {(t.hints ?? [])
            .filter((h: Data) => h.at <= elapsed.value)
            .map((h: Data) => (
              <p class="hint" key={h.at}>
                {h.text}
              </p>
            ))}
        </>
      )}
      {ended.value && <Answers guesses={ended.value.guesses} />}
    </>
  )
}
