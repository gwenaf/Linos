import { computed, effect, signal } from '@preact/signals'
import { useEffect, useRef } from 'preact/hooks'
import { connect, type Data } from './ws'
import { Answers, Banner, Qr, Results, Scores, Stage, effectsAt, finalEffects, outroVolume, whenLoaded, scoreKey, scoreTable, unitName } from './ui'

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
const answeredBy = signal<string[]>([])
const pick = signal<Data>(null)
const wagerRequest = signal<Data>(null)
const roundEnd = signal<Data>(null)
const tiebreak = signal<string[] | null>(null)
const banner = signal('')
// Index of a track the server already started before this page joined: no media-started to send, seek instead.
let startedBefore = -1

fetch('/api/join')
  .then((r) => r.json())
  .then((d) => (joinUrl.value = d.urls[0] ?? ''))

// Local track clock, mirroring the server's: it stops during pauses and while a player answers.
// It drives the countdown, the reveal steps and the hints; the server clock stays the reference for scoring.
let clockBase = 0
let clockSince: number | null = null
const clockNow = () => clockBase + (clockSince === null ? 0 : (performance.now() - clockSince) / 1000)
// With pauseOnBuzz off, the track keeps playing while a player answers.
const holderStops = () => !!holder.value && track.value?.pauseOnBuzz !== false
const clockRunning = computed(() => live.value && !paused.value && !holderStops() && !ended.value)
effect(() => {
  if (clockRunning.value && clockSince === null) clockSince = performance.now()
  if (!clockRunning.value && clockSince !== null) {
    clockBase = clockNow()
    clockSince = null
  }
})
setInterval(() => (elapsed.value = clockNow()), 100)

conn.on('lobby-update', (d) => (lobby.value = d))
conn.on('state', (d) => {
  lobby.value = d.lobby
  title.value = d.configured?.title ?? ''
  round.value = d.round ?? null
  scores.value = scoreTable(d.scores)
  paused.value = !!d.paused
  holder.value = d.holder ?? null
  ended.value = d.trackEnd ?? null
  live.value = d.trackState === 'live' || d.trackState === 'ended'
  startedBefore = live.value ? d.track.index : -1
  // Set after the signals: the clock effect may already have run with the previous base.
  clockBase = d.elapsed ?? 0
  clockSince = clockRunning.value ? performance.now() : null
  elapsed.value = clockBase
  track.value = d.track ?? null
  pick.value = d.pick ?? null
  wagerRequest.value = d.wager ?? null
  tiebreak.value = d.tiebreak?.map(unitName) ?? null
})
conn.on('theme-pick-request', (d) => {
  pick.value = d
  roundEnd.value = null
  track.value = null
})
conn.on('theme-picked', (d) => {
  pick.value = null
  banner.value = `Thème ${d.theme}, choisi par ${unitName(d)}`
})
conn.on('wager-request', (d) => {
  wagerRequest.value = d
  roundEnd.value = null
  track.value = null
})
conn.on('wagered', (d) => {
  if (!wagerRequest.value) return
  wagerRequest.value = {
    limits: wagerRequest.value.limits.map((l: Data) => (unitName(l) === unitName(d) ? { ...l, placed: true } : l)),
  }
})
conn.on('joker-used', (d) => (banner.value = `${unitName(d)} joue son joker : points doublés !`))
conn.on('round-end', (d) => (roundEnd.value = d))
conn.on('eliminated', (d) => {
  if (d.units.length) banner.value = `Éliminé : ${d.units.map(unitName).join(', ')}`
})
conn.on('tiebreak', (d) => (tiebreak.value = d.units.map(unitName)))
conn.on('head-start-over', () => (banner.value = 'À tous de jouer !'))
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
  answeredBy.value = []
  pick.value = null
  wagerRequest.value = null
  roundEnd.value = null
  track.value = d
})
conn.on('answered', (d) => {
  const who = d.team ?? d.name
  if (!answeredBy.value.includes(who)) answeredBy.value = [...answeredBy.value, who]
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
        <Banner />
        <p>Cliquez pour activer l'écran de jeu et le son</p>
      </main>
    )
  }
  return (
    <main class="host">
      {paused.value && <div class="banner">Pause</div>}
      {banner.value && <p class="holder">{banner.value}</p>}
      {tiebreak.value && <p class="holder">Mort subite : {tiebreak.value.join(' contre ')}</p>}
      {roundEnd.value && <Results data={roundEnd.value} />}
      {pick.value ? (
        <section>
          <h1>{unitName(pick.value.picker)} choisit un thème</h1>
          <ul class="players">
            {pick.value.themes.map((th: Data) => (
              <li key={th.id}>{th.name}</li>
            ))}
          </ul>
        </section>
      ) : wagerRequest.value ? (
        <section>
          <h1>Les mises</h1>
          <ul class="players">
            {wagerRequest.value.limits.map((l: Data) => (
              <li key={unitName(l)} class={l.placed ? 'ready' : ''}>
                {unitName(l)}
              </li>
            ))}
          </ul>
        </section>
      ) : track.value ? (
        <TrackView />
      ) : (
        !roundEnd.value && <LobbyView />
      )}
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

const mediaUrl = (name: string) => '/media/' + name.split('/').map(encodeURIComponent).join('/')

function TrackView() {
  const t = track.value
  const video = useRef<HTMLVideoElement>(null)
  const reported = useRef(startedBefore)
  const effects = ended.value ? finalEffects(t.reveal, t.media) : effectsAt(t.reveal, elapsed.value)

  const reportStarted = () => {
    if (reported.current === t.index) return
    reported.current = t.index
    conn.send('media-started')
  }

  useEffect(() => {
    const v = video.current
    if (!v) return
    whenLoaded(v, () => {
      // Joining mid-track: resume the media where the server clock is.
      v.currentTime = t.start + (startedBefore === t.index ? clockNow() * t.playbackRate : 0)
      v.playbackRate = t.playbackRate
      v.play().catch(() => {})
    })
  }, [t.index])

  // The media plays unless the game is paused or someone holds the hand, or its outro is over.
  const outroOver = useRef(false)
  useEffect(
    () =>
      effect(() => {
        const v = video.current
        if (!v) return
        if (paused.value || holderStops() || outroOver.current) v.pause()
        else v.play().catch(() => {})
      }),
    [t.index],
  )

  // After the end, the media keeps playing, unveiled, for its outro, then fades out and stops.
  useEffect(() => {
    const v = video.current
    if (!ended.value) outroOver.current = false
    if (!ended.value || !v) return
    const began = performance.now()
    const fade = () => {
      v.volume = outroVolume(t.outro ?? 0, (performance.now() - began) / 1000)
      if (v.volume > 0) return
      outroOver.current = true
      v.pause()
      clearInterval(id)
    }
    const id = setInterval(fade, 100)
    fade()
    return () => clearInterval(id)
  }, [!!ended.value, t.index])

  return (
    <>
      <header class="track">
        {round.value && <span>{round.value.name}</span>}
        <span>
          Piste {t.index + 1} / {t.total}
        </span>
        {t.owner && <span>Thème de {unitName(t.owner)}</span>}
        {live.value && !ended.value && <span class="countdown">{Math.max(0, Math.ceil(t.duration - elapsed.value))}</span>}
      </header>
      <Stage key={t.index} src={t.media} effects={effects} imageUrl={mediaUrl} mediaRef={video} onStarted={reportStarted} />
      {holder.value && (
        <p class="holder">
          {holder.value.name}
          {holder.value.team && ` (${holder.value.team})`} {t.via === 'device' ? 'répond sur son téléphone' : 'a la main !'}
        </p>
      )}
      {!ended.value && (
        <>
          <p class="guesses">À deviner : {t.guesses.map((g: Data) => g.label).join(', ')}</p>
          {answeredBy.value.length > 0 && <p>Ont répondu : {answeredBy.value.join(', ')}</p>}
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
