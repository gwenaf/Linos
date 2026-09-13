import { signal } from '@preact/signals'
import { connect, store, type Data } from './ws'
import { Answers, Results, scoreKey, scoreTable, unitName } from './ui'

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
// Phone answers: whether the track clock runs, the guesses this phone no longer answers
// (answered in simultaneous mode, found in buzz mode) and whether the holder already sent an answer.
const live = signal(false)
const done = signal<string[]>([])
const sent = signal(false)
let liveSince = 0
const tick = signal(0)
// Round features: theme pick, wagers, jokers, eliminations, tiebreak and the last round notice.
const pick = signal<Data>(null)
const wagerRequest = signal<Data>(null)
const wagered = signal(false)
const jokers = signal(0)
const doubled = signal(false)
const eliminated = signal<string[]>([])
const tiebreak = signal<string[] | null>(null)
const notice = signal('')
const myUnit = () => lobby.value.players.find((p: Data) => p.name === me.value)?.team || me.value
setInterval(() => tick.value++, 500)

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
  live.value = d.trackState === 'live'
  liveSince = performance.now() - (d.elapsed ?? 0) * 1000
  done.value = (d.track?.mode === 'simultaneous' ? d.answered : d.found) ?? []
  sent.value = !!d.holder && !d.canAnswer
  pick.value = d.pick ?? null
  wagerRequest.value = d.wager ?? null
  wagered.value = !!d.wager?.limits.find((l: Data) => unitName(l) === myUnit())?.placed
  jokers.value = d.jokers ?? jokers.value
  doubled.value = !!d.doubled
  eliminated.value = (d.eliminated ?? []).map(unitName)
  tiebreak.value = d.tiebreak?.map(unitName) ?? null
})
conn.on('theme-pick-request', (d) => {
  pick.value = d
  notice.value = ''
})
conn.on('theme-picked', (d) => {
  pick.value = null
  notice.value = `Thème « ${d.theme} » choisi par ${unitName(d)}`
})
conn.on('wager-request', (d) => {
  wagerRequest.value = d
  wagered.value = false
})
conn.on('wagered', (d) => {
  if (unitName(d) === myUnit()) wagered.value = true
})
conn.on('joker-used', (d) => {
  if (unitName(d) !== myUnit()) return
  jokers.value--
  doubled.value = true
})
conn.on('eliminated', (d) => (eliminated.value = [...eliminated.value, ...d.units.map(unitName)]))
conn.on('tiebreak', (d) => (tiebreak.value = d.units.map(unitName)))
conn.on('round-end', (d) => (notice.value = `Fin de la manche ${d.name}`))
conn.on('lobby-update', (d) => {
  lobby.value = d
  if (d.state === 'lobby' || d.state === 'ready') state.value = d.state
  if (pendingName.value && d.players.some((p: Data) => p.name === pendingName.value)) {
    me.value = pendingName.value
    pendingName.value = ''
  }
})
conn.on('error', () => (pendingName.value = ''))
conn.on('game-start', (d) => {
  state.value = 'in-progress'
  results.value = null
  scores.value = {}
  jokers.value = d.jokers
  eliminated.value = []
  tiebreak.value = null
})
conn.on('track-start', (d) => {
  track.value = d
  ended.value = null
  holder.value = null
  canBuzz.value = false
  feedback.value = ''
  live.value = false
  done.value = []
  sent.value = false
  pick.value = null
  wagerRequest.value = null
})
conn.on('timer-start', () => {
  live.value = true
  liveSince = performance.now()
})
conn.on('buzz-available', () => (canBuzz.value = true))
conn.on('buzz-blocked', () => (canBuzz.value = false))
conn.on('buzz-accepted', (d) => {
  holder.value = d
  canBuzz.value = false
  sent.value = false
  if (d.name === me.value) navigator.vibrate?.(300)
})
conn.on('answer-result', (d) => {
  holder.value = null
  const simultaneous = track.value?.mode === 'simultaneous'
  if (d.correct || simultaneous) done.value = [...done.value, d.guess]
  if (d.name === me.value) feedback.value = d.correct ? `Bravo ! +${d.points}` : 'Raté…'
})
conn.on('track-end', (d) => {
  ended.value = d
  holder.value = null
  canBuzz.value = false
  live.value = false
  doubled.value = false
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
  const simultaneous = t?.mode === 'simultaneous'
  const unitLabel = myUnit()
  const out = eliminated.value.includes(unitLabel) || (tiebreak.value !== null && !tiebreak.value.includes(unitLabel))
  const canAnswer = !out && t && live.value && !paused.value && t.via === 'device' && (simultaneous || (mineHolds && !sent.value))
  const beforeTrack = !!pick.value || !!wagerRequest.value || (t && !live.value && !ended.value)
  const myLimit = wagerRequest.value?.limits.find((l: Data) => unitName(l) === unitLabel)
  return (
    <>
      <header class="track">
        <span>{unitLabel}</span>
        <strong>{score} pts</strong>
      </header>
      {paused.value && <p class="banner">Pause</p>}
      {notice.value && <p>{notice.value}</p>}
      {eliminated.value.includes(unitLabel) && <p class="banner">Éliminé : vous suivez la suite de la partie.</p>}
      {tiebreak.value && <p class="holder">Mort subite : {tiebreak.value.join(' contre ')}</p>}
      {pick.value &&
        (unitName(pick.value.picker) === unitLabel ? (
          <div>
            <p class="holder">Choisissez un thème</p>
            {pick.value.themes.map((th: Data) => (
              <button key={th.id} class="primary" onClick={() => conn.send('pick-theme', { theme: th.id })}>
                {th.name}
              </button>
            ))}
          </div>
        ) : (
          <p>{unitName(pick.value.picker)} choisit le thème…</p>
        ))}
      {myLimit && !wagered.value && (
        <form
          onSubmit={(e) => {
            e.preventDefault()
            conn.send('wager', { amount: Number(new FormData(e.target as HTMLFormElement).get('amount')) })
          }}
        >
          <p class="holder">Misez entre 0 et {myLimit.max} points</p>
          <input name="amount" type="number" min={0} max={myLimit.max} required inputMode="numeric" />
          <button class="primary">Miser</button>
        </form>
      )}
      {wagerRequest.value && wagered.value && <p>Mise enregistrée, en attente des autres.</p>}
      {!out && beforeTrack && jokers.value > 0 && !doubled.value && (
        <button onClick={() => conn.send('use-joker', { type: 'double' })}>Joker : points doublés ({jokers.value})</button>
      )}
      {doubled.value && <p>Joker joué : vos points sont doublés sur cette piste.</p>}
      {t && (
        <p>
          Piste {t.index + 1} / {t.total} — à deviner : {t.guesses.map((g: Data) => g.label).join(', ')}
        </p>
      )}
      {t?.via !== 'device' &&
        t?.guesses
          .filter((g: Data) => g.choices)
        .map((g: Data) => (
          <p key={g.label}>
            {g.label} : {g.choices.join(' · ')}
          </p>
        ))}
      {holder.value && (
        <p class="holder">
          {mineHolds ? (t?.via === 'device' ? 'À vous ! Répondez ci-dessous.' : 'À vous ! Répondez à voix haute.') : `${holder.value.name} a la main`}
        </p>
      )}
      {feedback.value && <p class="feedback">{feedback.value}</p>}
      {canAnswer && <AnswerForm />}
      {simultaneous && live.value && !canAnswer && <p>Réponses envoyées, en attente de la fin de la piste.</p>}
      {!simultaneous && !out && (
        <button class="buzz" disabled={!canBuzz.value} onPointerDown={() => conn.send('buzz')}>
          BUZZ
        </button>
      )}
      {ended.value && <Answers guesses={ended.value.guesses} />}
    </>
  )
}

function AnswerForm() {
  const t = track.value
  void tick.value // re-render so choices appear at choicesAt
  // ponytail: choicesAt counts from timer-start without pauses; good enough for showing buttons.
  const played = (performance.now() - liveSince) / 1000
  const guesses = t.guesses.filter((g: Data) => !done.value.includes(g.label))
  const submit = (guess: string, value: string) => {
    conn.send('answer', { guess, value })
    if (t.mode !== 'simultaneous') sent.value = true
  }
  if (t.mode === 'simultaneous' && guesses.length === 0) return null
  return (
    <div class="answer-form">
      {guesses.map((g: Data) => (
        <form
          key={g.label}
          onSubmit={(e) => {
            e.preventDefault()
            submit(g.label, new FormData(e.target as HTMLFormElement).get('value') as string)
          }}
        >
          <strong>{g.label}</strong>
          {g.type !== 'choice' ? (
            <>
              <input name="value" inputMode={g.type === 'number' ? 'decimal' : 'text'} autoComplete="off" required />
              <button class="primary">Valider</button>
            </>
          ) : played >= (g.choicesAt ?? 0) ? (
            g.choices.map((c: string) => (
              <button type="button" key={c} onClick={() => submit(g.label, c)}>
                {c}
              </button>
            ))
          ) : (
            <small>Choix dans {Math.ceil(g.choicesAt - played)} s</small>
          )}
        </form>
      ))}
    </div>
  )
}
