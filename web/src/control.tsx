import { signal } from '@preact/signals'
import { connect, isLocalPage, store, type Data } from './ws'
import { Answers, Qr, Results, Scores, scoreKey, scoreTable, unitName } from './ui'

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
const inviteQr = signal<{ url: string; caption: string } | null>(null)
const controlMode = signal('master')
// Answers typed on phones: the holder's pending answer (buzz mode) and everything answered (simultaneous mode).
const holderAnswer = signal<Data>(null)
const submissions = signal<Data[]>([])
const pick = signal<Data>(null)
const wagerRequest = signal<Data>(null)
const eliminated = signal<string[]>([])
const tiebreak = signal<string[] | null>(null)
const events = signal<string[]>([])
const logEvent = (text: string) => (events.value = [text, ...events.value].slice(0, 8))
const importStatus = signal('')
const importStart = signal(30)
const importDuration = signal(30)
const dragging = signal(false)

const network = signal<Data>(null)
const showHelp = signal(false)
const firewallResult = signal('')

if (isLocalPage) {
  fetch('/api/join')
    .then((r) => r.json())
    .then((d) => (joinUrls.value = d.urls))
  fetch('/api/network')
    .then((r) => r.json())
    .then((d) => (network.value = d))
  // Nobody joined after 30 s: the phones probably cannot reach the PC.
  setTimeout(() => {
    if (lobby.value.players.length === 0) showHelp.value = true
  }, 30_000)
}

const AUDIO = /\.(mp3|m4a|aac|ogg|opus|flac|wav)$/i

// Dropped folders arrive as entries: walk them to collect every file inside.
async function droppedFiles(entries: FileSystemEntry[]): Promise<File[]> {
  const files: File[] = []
  const walk = async (entry: FileSystemEntry): Promise<void> => {
    if (entry.isFile) {
      files.push(await new Promise<File>((ok, ko) => (entry as FileSystemFileEntry).file(ok, ko)))
      return
    }
    const reader = (entry as FileSystemDirectoryEntry).createReader()
    for (;;) {
      const batch = await new Promise<FileSystemEntry[]>((ok, ko) => reader.readEntries(ok, ko))
      if (batch.length === 0) return
      for (const child of batch) await walk(child)
    }
  }
  for (const entry of entries) await walk(entry)
  return files
}

async function importMusic(files: File[]) {
  const audio = files.filter((f) => AUDIO.test(f.name))
  if (audio.length === 0) {
    importStatus.value = 'Aucun fichier audio (mp3, m4a, aac, ogg, opus, flac, wav).'
    return
  }
  importStatus.value = `Import de ${audio.length} fichier(s)…`
  const form = new FormData()
  audio.forEach((f) => form.append('files', f, f.name))
  const via = controlMode.value === 'auto' ? 'device' : ''
  const res = await fetch(`/api/import?start=${importStart.value}&duration=${importDuration.value}&via=${via}`, { method: 'POST', body: form })
  if (!res.ok) {
    importStatus.value = `Échec de l'import : ${await res.text()}`
    return
  }
  const d = await res.json()
  importStatus.value = `${d.tracks} piste(s) importée(s) dans le pack ${d.pack}.`
  conn.send('list-packs')
  conn.send('configure', { pack: d.pack, control: controlMode.value })
}

async function allowFirewall() {
  firewallResult.value = 'Acceptez la demande de Windows sur ce PC…'
  const res = await fetch('/api/firewall', { method: 'POST' })
  firewallResult.value = res.ok ? 'Règle ajoutée : réessayez depuis un téléphone.' : `Échec : ${await res.text()}`
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
  conn.send('list-packs')
})
// Sent after every join: a reloaded or reconnected page resumes the game as it is.
conn.on('state', (d) => {
  state.value = d.state
  lobby.value = d.lobby
  configured.value = d.configured ?? null
  controlMode.value = d.configured?.control ?? controlMode.value
  round.value = d.round ?? null
  track.value = d.track ?? null
  trackEnded.value = d.trackState === 'ended'
  found.value = d.found ?? []
  holder.value = d.holder ?? null
  paused.value = d.paused ?? null
  scores.value = scoreTable(d.scores)
  pick.value = d.pick ?? null
  wagerRequest.value = d.wager ?? null
  eliminated.value = (d.eliminated ?? []).map(unitName)
  tiebreak.value = d.tiebreak?.map(unitName) ?? null
})
conn.on('theme-pick-request', (d) => (pick.value = d))
conn.on('theme-picked', (d) => {
  pick.value = null
  logEvent(`${unitName(d)} a choisi le thème ${d.theme}`)
})
conn.on('wager-request', (d) => (wagerRequest.value = d))
conn.on('wagered', (d) => {
  if (!wagerRequest.value) return
  wagerRequest.value = {
    limits: wagerRequest.value.limits.map((l: Data) => (unitName(l) === unitName(d) ? { ...l, placed: true } : l)),
  }
})
conn.on('joker-used', (d) => logEvent(`${unitName(d)} joue son joker double`))
conn.on('head-start-over', () => logEvent("Fin de l'avance du propriétaire du thème"))
conn.on('lockout', (d) => logEvent(`${unitName(d)} bloqué ${d.duration} s`))
conn.on('track-end', (d) => {
  if (d.reason === 'missed') logEvent('Mauvaise réponse sans rebond : piste terminée')
})
conn.on('round-end', (d) => logEvent(`Fin de la manche ${d.name}`))
conn.on('eliminated', (d) => {
  eliminated.value = [...eliminated.value, ...d.units.map(unitName)]
  if (d.units.length) logEvent(`Éliminé : ${d.units.map(unitName).join(', ')}`)
})
conn.on('tiebreak', (d) => {
  const units: string[] = d.units.map(unitName)
  tiebreak.value = units
  logEvent(`Mort subite : ${units.join(' contre ')}`)
})
conn.on('lobby-update', (d) => {
  lobby.value = d
  state.value = d.state
})
conn.on('packs', (d) => {
  packs.value = d.packs
  packsDir.value = d.dir
})
conn.on('configured', (d) => {
  configured.value = d
  controlMode.value = d.control
})
conn.on('answer-submitted', (d) => (holderAnswer.value = d))
const base = () => joinUrls.value[0]?.replace(/\/play$/, '') ?? location.origin
conn.on('master-invite', (d) => {
  inviteQr.value = {
    url: `${base()}/control?invite=${d.code}`,
    caption: 'Scannez avec le téléphone du maître du jeu (valable 2 minutes, usage unique)',
  }
})
conn.on('reconnect-invite', (d) => {
  inviteQr.value = {
    url: `${base()}/play?invite=${d.code}`,
    caption: `Scannez avec le téléphone de ${d.name} pour reprendre sa place (valable 2 minutes, usage unique)`,
  }
})
conn.on('game-start', () => {
  state.value = 'in-progress'
  scores.value = {}
  results.value = null
  round.value = null
  eliminated.value = []
  tiebreak.value = null
  events.value = []
})
conn.on('round-start', (d) => (round.value = d))
conn.on('track-start', (d) => {
  track.value = d
  trackEnded.value = false
  found.value = []
  holder.value = null
  holderAnswer.value = null
  submissions.value = []
  pick.value = null
  wagerRequest.value = null
})
conn.on('buzz-accepted', (d) => {
  holder.value = d
  holderAnswer.value = null
})
conn.on('answer-result', (d) => {
  holder.value = null
  holderAnswer.value = null
  if (d.correct) found.value = [...found.value, d.guess]
  if (d.value !== undefined) submissions.value = [...submissions.value, d]
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
      {inviteQr.value && (
        <section onClick={() => (inviteQr.value = null)}>
          <Qr url={inviteQr.value.url} caption={inviteQr.value.caption} />
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
          {network.value?.categories?.includes('Public') && (
            <p class="warning">
              Ce réseau est classé « Public » : Windows peut bloquer les téléphones. <button onClick={allowFirewall}>Autoriser Linos dans le pare-feu</button>
            </p>
          )}
          {joinUrls.value.length > 0 ? (
            <Qr url={joinUrls.value[0]} caption="Les joueurs scannent ce code, sur le même Wi-Fi" />
          ) : (
            <p>Aucune adresse réseau locale trouvée : vérifiez la connexion Wi-Fi.</p>
          )}
          <ul>
            {joinUrls.value.map((url) => (
              <li key={url}>
                <code>{url}</code>
                {network.value && url.includes(`//${network.value.hotspot}`) && <small>Point d'accès Windows : 8 appareils maximum par défaut</small>}
              </li>
            ))}
          </ul>
          <button onClick={() => (showHelp.value = !showHelp.value)}>Les téléphones n'arrivent pas à se connecter ?</button>
          {firewallResult.value && <p>{firewallResult.value}</p>}
          {showHelp.value && <NetworkHelp />}
        </section>
      )}
      <section>
        <h2>Pack</h2>
        <button onClick={() => conn.send('list-packs')}>Actualiser</button>
        <p>
          <label>
            <input type="radio" name="control" checked={controlMode.value === 'master'} onChange={() => (controlMode.value = 'master')} /> Maître du jeu : je
            valide les réponses
          </label>
          <label>
            <input type="radio" name="control" checked={controlMode.value === 'auto'} onChange={() => (controlMode.value = 'auto')} /> Automatique : réponses
            sur les téléphones, validées par Linos
          </label>
        </p>
        {packs.value.length === 0 && (
          <p>
            Aucun pack trouvé dans <code>{packsDir.value}</code> : placez-y un dossier de pack ou un fichier .linospack, puis actualisez.
          </p>
        )}
        {isLocalPage && <ImportMusic />}
        <ul class="packs">
          {packs.value.map((p) => (
            <li key={p.name}>
              <button disabled={!!p.error} class={configured.value?.pack === p.name ? 'selected' : ''} onClick={() => conn.send('configure', { pack: p.name, control: controlMode.value })}>
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
                  {!p.connected && (
                    <button onClick={() => conn.send('reconnect-invite', { name: p.name })}>Reconnecter</button>
                  )}
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

function ImportMusic() {
  const onDrop = (e: DragEvent) => {
    e.preventDefault()
    dragging.value = false
    // Entries must be read during the drop event; the item list is emptied once it returns.
    const entries = [...(e.dataTransfer?.items ?? [])].map((i) => i.webkitGetAsEntry()).filter((x): x is FileSystemEntry => !!x)
    droppedFiles(entries).then(importMusic)
  }
  const onPick = (e: Event) => importMusic([...((e.target as HTMLInputElement).files ?? [])])
  return (
    <div
      class={dragging.value ? 'dropzone over' : 'dropzone'}
      onDragOver={(e) => {
        e.preventDefault()
        dragging.value = true
      }}
      onDragLeave={() => (dragging.value = false)}
      onDrop={onDrop}
    >
      <p>
        <strong>Import rapide</strong> : déposez ici un dossier ou des fichiers audio. Artiste et titre sont lus dans les tags, sinon dans le nom
        « Artiste - Titre ».
      </p>
      <label>
        Début des extraits (s) <input type="number" min={0} value={importStart.value} onInput={(e) => (importStart.value = Number((e.target as HTMLInputElement).value))} />
      </label>
      <label>
        Durée (s) <input type="number" min={1} value={importDuration.value} onInput={(e) => (importDuration.value = Number((e.target as HTMLInputElement).value))} />
      </label>
      <label>
        Fichiers <input type="file" multiple accept="audio/*" onChange={onPick} />
      </label>
      <label>
        Dossier <input type="file" {...{ webkitdirectory: '' }} onChange={onPick} />
      </label>
      {importStatus.value && <p>{importStatus.value}</p>}
    </div>
  )
}

function NetworkHelp() {
  const health = joinUrls.value[0]?.replace(/\/play$/, '/health')
  return (
    <div class="help">
      <h3>Aucun téléphone ne se connecte ?</h3>
      <ol>
        <li>
          Sur un téléphone, ouvrez <code>{health}</code> dans le navigateur. Rien ne s'affiche : le téléphone n'atteint pas ce PC, continuez.
        </li>
        <li>Vérifiez que le téléphone est sur le même Wi-Fi que ce PC, pas en 4G ni sur un réseau invité, sans VPN.</li>
        <li>
          Sur iPhone, un navigateur autre que Safari doit avoir l'autorisation « Réseau local » (Réglages › Confidentialité et sécurité › Réseau local).
        </li>
        <li>
          Autorisez Linos dans le pare-feu : <button onClick={allowFirewall}>Autoriser</button>
        </li>
        <li>
          Beaucoup de box et de Wi-Fi publics isolent les appareils entre eux. Solutions :
          <ul>
            <li>
              Activez le point d'accès mobile de Windows (Paramètres › Réseau et Internet), connectez les téléphones dessus et relancez Linos. Limité à 8
              appareils ; au-delà, valeur <code>WifiMaxPeers</code> (jusqu'à 128) dans le registre <code>HKLM\SYSTEM\CurrentControlSet\Services\icssvc\Settings</code>, puis redémarrage.
            </li>
            <li>Désactivez l'isolation des clients ou le mode invité dans les réglages Wi-Fi de la box.</li>
            <li>Pour jouer partout à 30 ou plus : un mini-routeur de voyage, sans isolation, auquel le PC et les téléphones se connectent.</li>
          </ul>
        </li>
      </ol>
    </div>
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
                  {name} est déconnecté.{' '}
                  <button onClick={() => conn.send('reconnect-invite', { name })}>Reconnecter</button>
                  <button onClick={() => conn.send('kick', { name })}>Exclure</button>
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
      {tiebreak.value && <p class="holder">Mort subite : {tiebreak.value.join(' contre ')}</p>}
      {eliminated.value.length > 0 && <p>Éliminés : {eliminated.value.join(', ')}</p>}
      {pick.value && (
        <section>
          <p>
            <strong>{unitName(pick.value.picker)}</strong> choisit un thème parmi : {pick.value.themes.map((th: Data) => th.name).join(', ')}
          </p>
          <button onClick={() => conn.send('skip')}>Passer ce choix</button>
        </section>
      )}
      {wagerRequest.value && (
        <section>
          <h3>Mises</h3>
          <ul>
            {wagerRequest.value.limits.map((l: Data) => (
              <li key={unitName(l)}>
                {unitName(l)} (jusqu'à {l.max}) : {l.placed ? 'misé' : 'en attente'}
              </li>
            ))}
          </ul>
          <button onClick={() => conn.send('skip')}>Passer cette piste</button>
        </section>
      )}
      {events.value.length > 0 && (
        <ul class="events">
          {events.value.map((e, i) => (
            <li key={i}>{e}</li>
          ))}
        </ul>
      )}
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
              {holderAnswer.value && (
                <p>
                  Réponse : <strong>{holderAnswer.value.value}</strong> ({holderAnswer.value.guess}, jugée {holderAnswer.value.correct ? 'correcte' : 'incorrecte'}{' '}
                  par Linos)
                </p>
              )}
              {configured.value?.control !== 'auto' && (
                <>
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
                </>
              )}
            </div>
          )}
          {submissions.value.length > 0 && (
            <table>
              <tbody>
                {submissions.value.map((s, i) => (
                  <tr key={i}>
                    <td>{s.team ?? s.name}</td>
                    <td>{s.guess}</td>
                    <td>{s.value}</td>
                    <td>{s.correct ? 'Correct' : 'Faux'}</td>
                    <td>{s.points}</td>
                  </tr>
                ))}
              </tbody>
            </table>
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
