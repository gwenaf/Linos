import { signal } from '@preact/signals'
import { connect, isLocalPage, store, type Data } from './ws'
import { Answers, Qr, Results, Scores, scoreKey } from './ui'

const MASTER_TOKEN = 'linos-master'
const invite = new URLSearchParams(location.search).get('invite')

// On the host machine the page is control by origin; a gamemaster phone joins with an invite, then its token.
const conn =
  isLocalPage && !invite
    ? connect('control')
    : connect('player', () => {
        const token = store(MASTER_TOKEN)
        return token ? { token } : { invite }
      })

const state = signal('lobby')
const lobby = signal<Data>({ players: [], teams: [] })
const packs = signal<Data[]>([])
const packsDir = signal('')
const configured = signal<Data>(null)
const round = signal<Data>(null)
const track = signal<Data>(null)
const trackEnded = signal(false)
const found = signal<string[]>([])
const holder = signal<Data>(null)
const paused = signal<Data>(null)
const scores = signal<Record<string, number>>({})
const results = signal<Data>(null)
const joinUrls = signal<string[]>([])
const inviteUrl = signal('')

if (isLocalPage) {
  fetch('/api/join')
    .then((r) => r.json())
    .then((d) => (joinUrls.value = d.urls))
}

conn.on('welcome', (d) => {
  if (d.role !== 'control') {
    store(MASTER_TOKEN, null)
    conn.error.value = 'Invitation invalide ou expirée : scannez un nouveau QR code maître du jeu.'
    conn.stop()
    return
  }
  if (d.token) store(MASTER_TOKEN, d.token)
  if (invite) history.replaceState(null, '', '/control')
  state.value = d.state
  conn.send('list-packs')
})
conn.on('lobby-update', (d) => {
  lobby.value = d
  state.value = d.state
})
conn.on('packs', (d) => {
  packs.value = d.packs
  packsDir.value = d.dir
})
conn.on('configured', (d) => (configured.value = d))
conn.on('master-invite', (d) => {
  const base = joinUrls.value[0]?.replace(/\/play$/, '') ?? location.origin
  inviteUrl.value = `${base}/control?invite=${d.code}`
})
conn.on('game-start', () => {
  state.value = 'in-progress'
  scores.value = {}
  results.value = null
  round.value = null
})
conn.on('round-start', (d) => (round.value = d))
conn.on('track-start', (d) => {
  track.value = d
  trackEnded.value = false
  found.value = []
  holder.value = null
})
conn.on('buzz-accepted', (d) => (holder.value = d))
conn.on('answer-result', (d) => {
  holder.value = null
  if (d.correct) found.value = [...found.value, d.guess]
})
conn.on('track-end', () => {
  trackEnded.value = true
  holder.value = null
})
conn.on('score-update', (d) => (scores.value = { ...scores.value, [scoreKey(d)]: d.score }))
conn.on('game-paused', (d) => {
  paused.value = d
  state.value = d.reason === 'control' ? 'paused' : 'technical-pause'
})
conn.on('game-resumed', () => {
  paused.value = null
  state.value = 'in-progress'
})
conn.on('game-end', (d) => {
  results.value = d
  track.value = null
  holder.value = null
  paused.value = null
})

export default function Control() {
  const inLobby = state.value === 'lobby' || state.value === 'ready'
  return (
    <main class="control">
      <header>
        <h1>Linos</h1>
        <span class={conn.connected.value ? 'status ok' : 'status'}>{conn.connected.value ? 'Connecté' : 'Reconnexion…'}</span>
        {isLocalPage && <button onClick={() => window.open('/host', 'linos-host')}>Ouvrir l'écran de jeu</button>}
        {isLocalPage && <button onClick={() => conn.send('master-invite')}>Téléphone maître du jeu</button>}
      </header>
      {conn.error.value && (
        <p class="error" onClick={() => (conn.error.value = '')}>
          {conn.error.value}
        </p>
      )}
      {inviteUrl.value && (
        <section onClick={() => (inviteUrl.value = '')}>
          <Qr url={inviteUrl.value} caption="Scannez avec le téléphone du maître du jeu (valable 2 minutes, usage unique)" />
        </section>
      )}
      {inLobby ? <Lobby /> : <Game />}
      <Scores scores={scores.value} />
    </main>
  )
}

function Lobby() {
  return (
    <>
      {results.value && <Results data={results.value} />}
      {isLocalPage && (
        <section>
          <h2>Rejoindre</h2>
          {joinUrls.value.length > 0 ? (
            <Qr url={joinUrls.value[0]} caption="Les joueurs scannent ce code, sur le même Wi-Fi" />
          ) : (
            <p>Aucune adresse réseau locale trouvée : vérifiez la connexion Wi-Fi.</p>
          )}
          {joinUrls.value.length > 1 && <p>Autres adresses : {joinUrls.value.slice(1).join(', ')}</p>}
        </section>
      )}
      <section>
        <h2>Pack</h2>
        <button onClick={() => conn.send('list-packs')}>Actualiser</button>
        {packs.value.length === 0 && (
          <p>
            Aucun pack trouvé dans <code>{packsDir.value}</code> : placez-y un dossier de pack ou un fichier .linospack, puis actualisez.
          </p>
        )}
        <ul class="packs">
          {packs.value.map((p) => (
            <li key={p.name}>
              <button disabled={!!p.error} class={configured.value?.pack === p.name ? 'selected' : ''} onClick={() => conn.send('configure', { pack: p.name })}>
                {p.title || p.name}
              </button>
              {p.error && <small class="error">{p.error}</small>}
            </li>
          ))}
        </ul>
      </section>
      <section>
        <h2>Joueurs</h2>
        <table>
          <tbody>
            {lobby.value.players.map((p: Data) => (
              <tr key={p.name}>
                <td>{p.name}</td>
                <td>{p.team}</td>
                <td>{p.connected ? (p.ready ? 'Prêt' : 'Pas prêt') : 'Déconnecté'}</td>
                <td>
                  <button onClick={() => conn.send('kick', { name: p.name })}>Exclure</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        <button class="primary" disabled={state.value !== 'ready' || !configured.value} onClick={() => conn.send('start-game')}>
          Lancer la partie
        </button>
        {!configured.value && <small>Choisissez un pack.</small>}
        {configured.value && state.value !== 'ready' && <small>Tous les joueurs connectés doivent être prêts.</small>}
      </section>
    </>
  )
}

function Game() {
  const t = track.value
  const p = paused.value
  return (
    <>
      {p && (
        <section class="warning">
          {p.reason === 'control' ? (
            <p>Partie en pause.</p>
          ) : (
            <>
              <p>Pause technique.</p>
              {p.hostMissing && <p>L'écran de jeu est déconnecté : rouvrez-le.</p>}
              {p.missingPlayers.map((name: string) => (
                <p key={name}>
                  {name} est déconnecté. <button onClick={() => conn.send('kick', { name })}>Exclure</button>
                </p>
              ))}
            </>
          )}
          <button class="primary" onClick={() => conn.send('resume')}>
            {p.reason === 'control' ? 'Reprendre' : 'Reprendre sans les absents'}
          </button>
        </section>
      )}
      {round.value && <h2>Manche : {round.value.name}</h2>}
      {t && (
        <section>
          <h2>
            Piste {t.index + 1} / {t.total}
            {trackEnded.value && ' (terminée)'}
          </h2>
          <Answers guesses={t.guesses.filter((g: Data) => !found.value.includes(g.label))} />
          {found.value.length > 0 && <p>Trouvé : {found.value.join(', ')}</p>}
          {holder.value && (
            <div class="holder">
              <p>
                <strong>{holder.value.name}</strong>
                {holder.value.team && ` (${holder.value.team})`} a la main
              </p>
              {t.guesses
                .filter((g: Data) => !found.value.includes(g.label))
                .map((g: Data) => (
                  <button key={g.label} class="good" onClick={() => conn.send('validate', { correct: true, guess: g.label })}>
                    Bonne réponse : {g.label}
                  </button>
                ))}
              <button class="bad" onClick={() => conn.send('validate', { correct: false })}>
                Mauvaise réponse
              </button>
            </div>
          )}
          <button class="primary" onClick={() => conn.send('skip')}>
            {trackEnded.value ? 'Piste suivante' : 'Passer la piste'}
          </button>
        </section>
      )}
      <section class="actions">
        {state.value === 'in-progress' && <button onClick={() => conn.send('pause')}>Pause</button>}
        <button onClick={() => conn.send('end-game')}>Terminer la partie</button>
        <button onClick={() => confirm('Annuler la partie ? Les scores ne seront pas conservés.') && conn.send('abort')}>Annuler la partie</button>
      </section>
    </>
  )
}
