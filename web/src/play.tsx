import { signal } from '@preact/signals'
import { connect, store, type Data } from './ws'
import { Answers, Results, scoreKey, scoreTable } from './ui'

const TOKEN = 'linos-player'
const conn = connect('player', () => ({ token: store(TOKEN) ?? '' }))

const me = signal('')
const pendingName = signal('')
const state = signal('lobby')
const lobby = signal<Data>({ players: [], teams: [] })
const track = signal<Data>(null)
const ended = signal<Data>(null)
const holder = signal<Data>(null)
const canBuzz = signal(false)
const paused = signal(false)
const feedback = signal('')
const scores = signal<Record<string, number>>({})
const results = signal<Data>(null)
const kicked = signal(false)

conn.on('welcome', (d) => {
  store(TOKEN, d.token)
  me.value = d.name
  state.value = d.state
})
conn.on('state', (d) => {
  state.value = d.state
  lobby.value = d.lobby
  track.value = d.track ?? null
  ended.value = d.trackEnd ?? null
  holder.value = d.holder ?? null
  canBuzz.value = !!d.canBuzz
  paused.value = !!d.paused
  scores.value = scoreTable(d.scores)
})
conn.on('lobby-update', (d) => {
  lobby.value = d
  if (d.state === 'lobby' || d.state === 'ready') state.value = d.state
  if (pendingName.value && d.players.some((p: Data) => p.name === pendingName.value)) {
    me.value = pendingName.value
    pendingName.value = ''
  }
})
conn.on('error', () => (pendingName.value = ''))
conn.on('game-start', () => {
  state.value = 'in-progress'
  results.value = null
  scores.value = {}
})
conn.on('track-start', (d) => {
  track.value = d
  ended.value = null
  holder.value = null
  canBuzz.value = false
  feedback.value = ''
})
conn.on('buzz-available', () => (canBuzz.value = true))
conn.on('buzz-blocked', () => (canBuzz.value = false))
conn.on('buzz-accepted', (d) => {
  holder.value = d
  canBuzz.value = false
  if (d.name === me.value) navigator.vibrate?.(300)
})
conn.on('answer-result', (d) => {
  holder.value = null
  if (d.name === me.value) feedback.value = d.correct ? `Bravo ! +${d.points}` : 'Raté…'
})
conn.on('track-end', (d) => {
  ended.value = d
  holder.value = null
  canBuzz.value = false
})
conn.on('game-paused', () => {
  paused.value = true
  canBuzz.value = false
})
conn.on('game-resumed', () => (paused.value = false))
conn.on('score-update', (d) => (scores.value = { ...scores.value, [scoreKey(d)]: d.score }))
conn.on('game-end', (d) => {
  results.value = d
  track.value = null
  state.value = 'lobby'
})
conn.on('kicked', (d) => {
  if (d.name !== me.value) return
  kicked.value = true
  store(TOKEN, null)
  conn.stop()
})

export default function Play() {
  if (kicked.value) {
    return (
      <main class="play">
        <p>Vous avez été exclu de la partie.</p>
      </main>
    )
  }
  return (
    <main class="play">
      {!conn.connected.value && <p class="banner">Reconnexion…</p>}
      {conn.error.value && (
        <p class="error" onClick={() => (conn.error.value = '')}>
          {conn.error.value}
        </p>
      )}
      {!me.value ? <Identify /> : state.value === 'lobby' || state.value === 'ready' ? <Lobby /> : <Game />}
    </main>
  )
}

function Identify() {
  const submit = (e: Event) => {
    e.preventDefault()
    const name = (new FormData(e.target as HTMLFormElement).get('name') as string).trim()
    pendingName.value = name
    conn.send('identify', { name })
  }
  return (
    <form onSubmit={submit}>
      <h1>Linos</h1>
      <label>
        Votre pseudo
        <input name="name" maxLength={32} required autoFocus />
      </label>
      <button class="primary">Rejoindre</button>
    </form>
  )
}

function Lobby() {
  const mine = lobby.value.players.find((p: Data) => p.name === me.value)
  const joinTeam = (e: Event) => {
    e.preventDefault()
    conn.send('join-team', { team: new FormData(e.target as HTMLFormElement).get('team') })
  }
  return (
    <>
      {results.value && <Results data={results.value} />}
      <h1>{me.value}</h1>
      <form onSubmit={joinTeam} class="team">
        <input name="team" list="teams" placeholder="Équipe (facultatif)" maxLength={32} value={mine?.team ?? ''} />
        <datalist id="teams">
          {lobby.value.teams.map((t: string) => (
            <option key={t} value={t} />
          ))}
        </datalist>
        <button>Valider l'équipe</button>
      </form>
      <button class={mine?.ready ? 'ready primary' : 'primary'} onClick={() => conn.send('ready', { ready: !mine?.ready })}>
        {mine?.ready ? 'Prêt ! (annuler)' : 'Je suis prêt'}
      </button>
      <p>
        {lobby.value.players.filter((p: Data) => p.ready).length} / {lobby.value.players.length} joueurs prêts
      </p>
    </>
  )
}

function Game() {
  const t = track.value
  const mine = lobby.value.players.find((p: Data) => p.name === me.value)
  const score = scores.value[mine?.team || me.value] ?? 0
  const mineHolds = holder.value?.name === me.value
  return (
    <>
      <header class="track">
        <span>{mine?.team || me.value}</span>
        <strong>{score} pts</strong>
      </header>
      {paused.value && <p class="banner">Pause</p>}
      {t && (
        <p>
          Piste {t.index + 1} / {t.total} — à deviner : {t.guesses.map((g: Data) => g.label).join(', ')}
        </p>
      )}
      {t?.guesses
        .filter((g: Data) => g.choices)
        .map((g: Data) => (
          <p key={g.label}>
            {g.label} : {g.choices.join(' · ')}
          </p>
        ))}
      {holder.value && <p class="holder">{mineHolds ? 'À vous ! Répondez à voix haute.' : `${holder.value.name} a la main`}</p>}
      {feedback.value && <p class="feedback">{feedback.value}</p>}
      <button class="buzz" disabled={!canBuzz.value} onPointerDown={() => conn.send('buzz')}>
        BUZZ
      </button>
      {ended.value && <Answers guesses={ended.value.guesses} />}
    </>
  )
}
