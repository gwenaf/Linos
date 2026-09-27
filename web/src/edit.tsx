import { signal } from '@preact/signals'
import { useEffect, useRef, useState } from 'preact/hooks'
import { IMAGE, Stage, droppedFiles, effectsAt, finalEffects, outroVolume } from './ui'
import type { Data } from './ws'

// Pack editor: folder packs of packs/, host machine only. The manifest is edited as loaded,
// so fields this page does not show yet are kept when saving.
const packs = signal<string[]>([])
const current = signal('')
const manifest = signal<Data>(null)
const media = signal<string[]>([])
const problems = signal<string[]>([])
const status = signal('')
const dirty = signal(false)
const preview = signal<Data>(null)
const newName = signal('')
const dragging = signal(false)

const MEDIA = /\.(mp3|m4a|aac|ogg|opus|flac|wav|mp4|m4v|webm|jpe?g|png|webp|gif)$/i
const DEFAULT_DURATION = 30

const packUrl = (suffix = '') => `/api/edit/${encodeURIComponent(current.value)}${suffix}`
const mediaUrl = (name: string) => packUrl(`/media/${name.split('/').map(encodeURIComponent).join('/')}`)
const value = (e: Event) => (e.target as HTMLInputElement).value
const optionalNumber = (v: string) => (v === '' ? undefined : Number(v))

addEventListener('beforeunload', (e) => {
  if (dirty.value) e.preventDefault()
})

async function listPacks() {
  packs.value = await (await fetch('/api/edit')).json()
}
listPacks()

async function open(name: string) {
  if (dirty.value && !confirm('Modifications non enregistrées : les abandonner ?')) return
  const res = await fetch(`/api/edit/${encodeURIComponent(name)}`)
  if (!res.ok) {
    status.value = await res.text()
    return
  }
  const d = await res.json()
  current.value = name
  manifest.value = d.manifest
  media.value = d.media
  problems.value = d.problems
  dirty.value = false
  preview.value = null
  status.value = ''
}

async function create() {
  const name = newName.value.trim()
  const res = await fetch('/api/edit', { method: 'POST', body: JSON.stringify({ name }) })
  if (!res.ok) {
    status.value = `Création impossible : ${await res.text()}`
    return
  }
  newName.value = ''
  await listPacks()
  await open(name)
}

async function save() {
  const res = await fetch(packUrl(), { method: 'PUT', body: JSON.stringify(prune(manifest.value)) })
  if (!res.ok) {
    status.value = `Échec de l'enregistrement : ${await res.text()}`
    return
  }
  problems.value = (await res.json()).problems
  dirty.value = false
  status.value = 'Enregistré.'
}

// exportPack writes packs/<name>.linospack, media cut by ffmpeg when it is installed.
async function exportPack() {
  status.value = 'Export en cours : le découpage des médias peut prendre quelques minutes…'
  const res = await fetch(packUrl('/export'), { method: 'POST' })
  if (!res.ok) {
    status.value = `Échec de l'export : ${await res.text()}`
    return
  }
  const d = await res.json()
  const size = (d.size / 1e6).toFixed(1)
  status.value = d.cut
    ? `Exporté : packs/${d.file} (${size} Mo), médias découpés aux extraits joués.`
    : `Exporté : packs/${d.file} (${size} Mo). ffmpeg introuvable : médias copiés entiers ; installez-le pour réduire la taille.`
}

// update edits the manifest in place, then publishes a new reference so the page renders again.
function update(fn: (m: Data) => void) {
  fn(manifest.value)
  manifest.value = { ...manifest.value }
  dirty.value = true
}

const isObject = (v: unknown) => !!v && typeof v === 'object' && !Array.isArray(v)

// prune drops the empty objects left by cleared fields ("rules": {"answer": {}}).
function prune(v: Data): Data {
  if (Array.isArray(v)) return v.map(prune)
  if (!isObject(v)) return v
  const out: Data = {}
  for (const [k, x] of Object.entries(v)) {
    const p = prune(x)
    if (p !== undefined && !(isObject(p) && Object.keys(p).length === 0)) out[k] = p
  }
  return out
}

// put sets a nested field of o, creating the objects on the way; an empty value removes the field,
// so a round falls back on the pack rules and the pack on the game defaults.
function put(o: Data, path: string[], v: unknown) {
  update(() => {
    const parent = path.slice(0, -1).reduce((x, k) => (x[k] ??= {}), o)
    if (v === undefined || v === '' || (Array.isArray(v) && v.length === 0)) delete parent[path[path.length - 1]]
    else parent[path[path.length - 1]] = v
  })
}
const get = (o: Data, path: string[]) => path.reduce((x, k) => x?.[k], o)

// renameRef follows a renamed (or deleted, when to is undefined) track or theme id in every list referencing it.
function renameRef(kind: 'tracks' | 'themes', from: string, to?: string) {
  const m = manifest.value
  const lists: (string[] | undefined)[] =
    kind === 'tracks'
      ? (m.rounds ?? []).map((r: Data) => r.tracks)
      : [...(m.tracks ?? []).map((t: Data) => t.themes), ...(m.rounds ?? []).map((r: Data) => r.themes), m.game?.tiebreak?.themes]
  for (const list of lists) {
    const i = list?.indexOf(from) ?? -1
    if (i < 0) continue
    if (to) list![i] = to
    else list!.splice(i, 1)
  }
}

type Field = { o: Data; path: string[]; label: string }

function Num({ o, path, label, int, placeholder }: Field & { int?: boolean; placeholder?: string }) {
  return (
    <label>
      {label}{' '}
      <input
        type="number"
        step={int ? 1 : 'any'}
        value={get(o, path) ?? ''}
        placeholder={placeholder}
        onChange={(e) => {
          const n = optionalNumber(value(e))
          put(o, path, int && n !== undefined ? Math.round(n) : n)
        }}
      />
    </label>
  )
}

function Text({ o, path, label }: Field) {
  return (
    <label>
      {label} <input value={get(o, path) ?? ''} onChange={(e) => put(o, path, value(e).trim())} />
    </label>
  )
}

// Pick chooses among fixed values; the empty choice keeps the inherited or default value.
function Pick({ o, path, label, options, bool }: Field & { options: [string, string][]; bool?: boolean }) {
  const v = get(o, path)
  return (
    <label>
      {label}{' '}
      <select
        value={v === undefined ? '' : String(v)}
        onChange={(e) => {
          const s = value(e)
          put(o, path, bool && s !== '' ? s === 'true' : s)
        }}
      >
        <option value="">—</option>
        {options.map(([val, text]) => (
          <option key={val} value={val}>
            {text}
          </option>
        ))}
      </select>
    </label>
  )
}

// Ids toggles values of a list; checked values are appended, so the click order is the list order.
function Ids({ o, path, label, options }: Field & { options: [string, string][] }) {
  const list: string[] = get(o, path) ?? []
  return (
    <fieldset>
      <legend>{label}</legend>
      {options.length === 0 && <small>aucun</small>}
      {options.map(([val, text]) => (
        <label key={val}>
          <input
            type="checkbox"
            checked={list.includes(val)}
            onChange={(e) => put(o, path, (e.target as HTMLInputElement).checked ? [...list, val] : list.filter((x) => x !== val))}
          />
          {text}
        </label>
      ))}
    </fieldset>
  )
}

const yesNo: [string, string][] = [
  ['true', 'oui'],
  ['false', 'non'],
]
const themeOptions = (): [string, string][] => (manifest.value.themes ?? []).map((t: Data) => [t.id, t.name || t.id])

// Rules edits a rules object: the pack defaults, or a round's overrides (empty fields inherit).
function Rules({ o }: { o: Data }) {
  const r = (...path: string[]) => ['rules', ...path]
  const ranks: number[] | undefined = get(o, r('scoring', 'ranks'))
  return (
    <div class="rules">
      <Num o={o} path={r('duration')} label="Durée d'une piste (s)" placeholder={String(DEFAULT_DURATION)} />
      <fieldset>
        <legend>Réponses</legend>
        <Pick o={o} path={r('answer', 'mode')} label="Mode" options={[['buzz', 'buzz'], ['simultaneous', 'simultanées']]} />
        <Pick o={o} path={r('answer', 'via')} label="Sur" options={[['oral', "l'oral"], ['device', 'le téléphone']]} />
        <Num o={o} path={r('answer', 'answerTime')} label="Temps de réponse (s)" />
        <Pick o={o} path={r('answer', 'pauseOnBuzz')} label="Pause au buzz" options={yesNo} bool />
        <Num o={o} path={r('answer', 'attempts')} label="Essais" int />
        <Num o={o} path={r('answer', 'wrongLockout')} label="Blocage après erreur (s)" />
        <Pick o={o} path={r('answer', 'rebound')} label="Rebond" options={[['none', 'aucun'], ['others', 'les autres'], ['all', 'tout le monde']]} />
        <Num o={o} path={r('answer', 'fuzziness')} label="Tolérance aux fautes (0 à 1)" />
      </fieldset>
      <fieldset>
        <legend>Points</legend>
        <Pick
          o={o}
          path={r('scoring', 'type')}
          label="Barème"
          options={[['speed', 'rapidité'], ['fixed', 'fixe'], ['rank', 'classement'], ['wager', 'mise']]}
        />
        <Num o={o} path={r('scoring', 'max')} label="Max" int />
        <Num o={o} path={r('scoring', 'min')} label="Min" int />
        <label>
          Classement{' '}
          <input
            value={ranks?.join(', ') ?? ''}
            placeholder="100, 60, 30"
            onChange={(e) => put(o, r('scoring', 'ranks'), value(e).split(',').map((x) => x.trim()).filter(Boolean).map(Number))}
          />
        </label>
        <Num o={o} path={r('scoring', 'wrongPenalty')} label="Pénalité d'erreur" int />
        <Num o={o} path={r('scoring', 'reboundBonus')} label="Bonus de rebond" int />
        <Num o={o} path={r('jokers', 'double')} label="Jokers double" int />
      </fieldset>
      <fieldset>
        <legend>Thème choisi</legend>
        <Num o={o} path={r('owner', 'headStart')} label="Avance du choisisseur (s)" />
        <Pick o={o} path={r('owner', 'exclusive')} label="Exclusif" options={yesNo} bool />
        <Num o={o} path={r('owner', 'othersBonus')} label="Bonus des autres" int />
      </fieldset>
    </div>
  )
}

function General() {
  const m = manifest.value
  const images = media.value.filter((x) => IMAGE.test(x))
  return (
    <details open>
      <summary>Pack</summary>
      <Text o={m} path={['title']} label="Titre" />
      <Text o={m} path={['author']} label="Auteur" />
      <Text o={m} path={['language']} label="Langue" />
      <Pick o={m} path={['cover']} label="Couverture" options={images.map((x) => [x, x])} />
    </details>
  )
}

function Game() {
  const m = manifest.value
  const g = ['game']
  return (
    <details>
      <summary>Partie</summary>
      <Ids o={m} path={[...g, 'control', 'allowed']} label="Contrôles possibles" options={[['master', 'maître du jeu'], ['auto', 'autonome']]} />
      <Pick o={m} path={[...g, 'control', 'default']} label="Contrôle par défaut" options={[['master', 'maître du jeu'], ['auto', 'autonome']]} />
      <Pick o={m} path={[...g, 'players', 'teams']} label="Équipes" options={[['none', 'non'], ['optional', 'facultatives'], ['required', 'obligatoires']]} />
      <Num o={m} path={[...g, 'players', 'minTeams']} label="Équipes min" int />
      <Num o={m} path={[...g, 'players', 'maxTeams']} label="Équipes max" int />
      <Pick o={m} path={[...g, 'end', 'type']} label="Fin" options={[['rounds', 'après les manches'], ['score', 'score cible'], ['elimination', 'élimination']]} />
      <Num o={m} path={[...g, 'end', 'target']} label="Score cible" int />
      <Pick o={m} path={[...g, 'tiebreak', 'type']} label="Égalité" options={[['sudden-death', 'mort subite']]} />
      <Ids o={m} path={[...g, 'tiebreak', 'themes']} label="Thèmes de la mort subite" options={themeOptions()} />
    </details>
  )
}

function Themes() {
  const themes: Data[] = manifest.value.themes ?? []
  return (
    <details>
      <summary>Thèmes ({themes.length})</summary>
      {themes.map((t, i) => (
        <div key={t.id}>
          <code>{t.id}</code> <input value={t.name} placeholder="Nom" onChange={(e) => update(() => (t.name = value(e)))} />
          <button
            onClick={() =>
              update((m) => {
                m.themes.splice(i, 1)
                renameRef('themes', t.id)
              })
            }
          >
            Supprimer
          </button>
        </div>
      ))}
      <form
        onSubmit={(e) => {
          e.preventDefault()
          const input = (e.target as HTMLFormElement).elements.namedItem('name') as HTMLInputElement
          const name = input.value.trim()
          const id = name.toLowerCase().normalize('NFD').replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '') || 'theme'
          if (!name || themes.some((t) => t.id === id)) return
          update((m) => (m.themes = [...themes, { id, name }]))
          input.value = ''
        }}
      >
        <input name="name" placeholder="Nouveau thème" /> <button>Ajouter</button>
      </form>
    </details>
  )
}

function Rounds() {
  const rounds: Data[] = manifest.value.rounds ?? []
  const trackOptions = (): [string, string][] => (manifest.value.tracks ?? []).map((t: Data) => [t.id, t.id])
  const move = (i: number, d: number) =>
    update((m) => {
      const [r] = m.rounds.splice(i, 1)
      m.rounds.splice(i + d, 0, r)
    })
  return (
    <details>
      <summary>Manches ({rounds.length})</summary>
      <p>
        <small>Sans manche, toutes les pistes sont jouées dans l'ordre.</small>
      </p>
      {rounds.map((r, i) => (
        <fieldset key={i}>
          <legend>
            Manche {i + 1} <button disabled={i === 0} onClick={() => move(i, -1)}>↑</button>
            <button disabled={i === rounds.length - 1} onClick={() => move(i, 1)}>↓</button>
            <button onClick={() => update((m) => m.rounds.splice(i, 1))}>Supprimer</button>
          </legend>
          <Text o={r} path={['name']} label="Nom" />
          <label>
            Pistes{' '}
            <select value={r.selection} onChange={(e) => update(() => (r.selection = value(e)))}>
              <option value="sequence">liste fixe</option>
              <option value="random">tirées au hasard dans des thèmes</option>
              <option value="theme-pick">thème choisi par les joueurs</option>
            </select>
          </label>
          {r.selection === 'sequence' ? (
            <Ids o={r} path={['tracks']} label="Pistes, dans l'ordre des clics" options={trackOptions()} />
          ) : (
            <>
              <Ids o={r} path={['themes']} label="Thèmes" options={themeOptions()} />
              <Num o={r} path={['count']} label="Nombre de pistes" int />
            </>
          )}
          {r.selection === 'theme-pick' && <Pick o={r} path={['picker']} label="Qui choisit" options={[['rotation', 'chacun son tour']]} />}
          <Num o={r} path={['eliminate']} label="Éliminés en fin de manche" int />
          <details>
            <summary>Règles de la manche (vide : celles du pack)</summary>
            <Rules o={r} />
          </details>
        </fieldset>
      ))}
      <button onClick={() => update((m) => (m.rounds = [...rounds, { name: `Manche ${rounds.length + 1}`, selection: 'sequence' }]))}>
        + manche
      </button>
    </details>
  )
}

function newTrackId(tracks: Data[]) {
  const ids = new Set(tracks.map((t) => t.id))
  let n = tracks.length + 1
  while (ids.has(`t${n}`)) n++
  return `t${n}`
}

async function upload(files: File[]) {
  files = files.filter((f) => MEDIA.test(f.name))
  if (files.length === 0) {
    status.value = 'Aucun média (audio, vidéo ou image).'
    return
  }
  status.value = `Envoi de ${files.length} fichier(s)…`
  const form = new FormData()
  files.forEach((f) => form.append('files', f, f.name))
  const res = await fetch(packUrl('/media'), { method: 'POST', body: form })
  if (!res.ok) {
    status.value = `Échec de l'envoi : ${await res.text()}`
    return
  }
  const d = await res.json()
  // Each audio or video file becomes a track; images stay in the media list.
  update((m) => {
    m.tracks ??= []
    for (const u of d.uploads) {
      if (!IMAGE.test(u.media)) m.tracks.push({ id: newTrackId(m.tracks), media: u.media, guesses: u.guesses })
    }
  })
  media.value = [...media.value, ...d.uploads.map((u: Data) => u.media)]
  status.value = `${d.uploads.length} fichier(s) ajouté(s). Pensez à enregistrer.`
}

function Dropzone() {
  const onDrop = (e: DragEvent) => {
    e.preventDefault()
    dragging.value = false
    // Entries must be read during the drop event; the item list is emptied once it returns.
    const entries = [...(e.dataTransfer?.items ?? [])].map((i) => i.webkitGetAsEntry()).filter((x): x is FileSystemEntry => !!x)
    droppedFiles(entries).then(upload)
  }
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
        Déposez ici des médias ou un dossier : chaque audio ou vidéo devient une piste, artiste et titre lus dans les tags ou le nom
        « Artiste - Titre ».
      </p>
      <label>
        Fichiers{' '}
        <input type="file" multiple onChange={(e) => upload([...((e.target as HTMLInputElement).files ?? [])])} />
      </label>
    </div>
  )
}

function Guesses({ track }: { track: Data }) {
  const set = (fn: () => void) => update(fn)
  return (
    <>
      {(track.guesses ?? []).map((g: Data, j: number) => (
        <div key={j}>
          <input value={g.label} placeholder="Élément" onInput={(e) => set(() => (g.label = value(e)))} />
          {g.type === 'text' ? (
            <input
              value={(g.answers ?? []).join(' ; ')}
              placeholder="Réponses acceptées, séparées par ;"
              onChange={(e) => set(() => (g.answers = value(e).split(';').map((a) => a.trim()).filter(Boolean)))}
            />
          ) : (
            <small> {g.type}</small>
          )}
          <button onClick={() => set(() => track.guesses.splice(j, 1))}>×</button>
        </div>
      ))}
      <button onClick={() => set(() => (track.guesses = [...(track.guesses ?? []), { label: '', type: 'text', answers: [] }]))}>
        + élément
      </button>
    </>
  )
}

function Tracks() {
  const tracks: Data[] = manifest.value.tracks ?? []
  const duration = manifest.value.rules?.duration ?? DEFAULT_DURATION
  return (
    <section>
      <h2>Pistes ({tracks.length})</h2>
      <table>
        <thead>
          <tr>
            <th>Id</th>
            <th>Média</th>
            <th>Début (s)</th>
            <th>Durée (s)</th>
            <th>Suite après la fin (s)</th>
            <th>Thèmes</th>
            <th>À deviner</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {tracks.map((t, i) => (
            <tr key={i}>
              <td>
                <input
                  value={t.id}
                  onChange={(e) =>
                    update(() => {
                      renameRef('tracks', t.id, value(e))
                      t.id = value(e)
                    })
                  }
                />
              </td>
              <td>
                <select value={t.media} onChange={(e) => update(() => (t.media = value(e)))}>
                  {!media.value.includes(t.media) && <option value={t.media}>{t.media} (introuvable)</option>}
                  {media.value.map((m) => (
                    <option key={m} value={m}>
                      {m}
                    </option>
                  ))}
                </select>
              </td>
              <td>
                <input type="number" min={0} step="0.1" value={t.start ?? ''} placeholder="0" onChange={(e) => update(() => (t.start = optionalNumber(value(e))))} />
              </td>
              <td>
                <input type="number" min={1} step="0.1" value={t.duration ?? ''} placeholder={String(duration)} onChange={(e) => update(() => (t.duration = optionalNumber(value(e))))} />
              </td>
              <td>
                <input type="number" min={0} step="0.1" value={t.outro ?? ''} placeholder="0" onChange={(e) => update(() => (t.outro = optionalNumber(value(e))))} />
              </td>
              <td>
                <Ids o={t} path={['themes']} label="" options={themeOptions()} />
              </td>
              <td>
                <Guesses track={t} />
              </td>
              <td>
                <button onClick={() => (preview.value = t)}>Écouter</button>
                <button
                  onClick={() =>
                    update((m) => {
                      m.tracks.splice(i, 1)
                      renameRef('tracks', t.id)
                    })
                  }
                >
                  Supprimer
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  )
}

// Preview plays the extract as the game will: from start, for its duration, at its playback rate,
// with its effects; steps edited here apply live.
function Preview() {
  const t = preview.value
  const video = useRef<HTMLVideoElement>(null)
  const [now, setNow] = useState(0)
  const [run, setRun] = useState(0)
  const start = t.start ?? 0
  const rate = t.playbackRate ?? 1
  const duration = t.duration ?? manifest.value.rules?.duration ?? DEFAULT_DURATION
  const outro = t.outro ?? 0

  useEffect(() => {
    const began = performance.now()
    const v = video.current
    if (v) {
      v.onloadedmetadata = () => {
        v.currentTime = start
        v.playbackRate = rate
        v.play().catch(() => {})
      }
    }
    // Pictures have no media clock: the preview counts wall time.
    const id = setInterval(() => {
      const elapsed = v ? (v.currentTime - start) / rate : (performance.now() - began) / 1000
      if (v) v.volume = elapsed < duration ? 1 : outroVolume(outro, elapsed - duration)
      if (elapsed >= duration + outro) v?.pause()
      setNow(Math.min(elapsed, duration + outro))
    }, 100)
    return () => clearInterval(id)
  }, [t.media, start, rate, duration, outro, run])

  const steps: Data[] = t.reveal ?? []
  const hints: Data[] = t.hints ?? []
  const effects = now >= duration ? finalEffects(steps, t.media) : effectsAt(steps, now)
  const images = media.value.filter((x) => IMAGE.test(x))
  return (
    <section>
      <h2>
        Aperçu de {t.id} : {now.toFixed(1)} / {duration} s{now >= duration && ' (fin, réponse dévoilée)'}
      </h2>
      <Stage key={`${t.media}-${start}-${run}`} src={mediaUrl(t.media)} effects={effects} imageUrl={mediaUrl} mediaRef={video} />
      {hints
        .filter((h) => (h.at ?? 0) <= now && h.text)
        .map((h, i) => (
          <p class="hint" key={i}>
            {h.text}
          </p>
        ))}
      <button onClick={() => setRun(run + 1)}>Rejouer</button> <button onClick={() => (preview.value = null)}>Fermer</button>
      <h3>Effets</h3>
      <p>
        <small>
          Chaque étape s'applique à son instant ; flou, pixelisation et gris varient progressivement jusqu'à l'étape suivante qui les fixe. Vide :
          inchangé.
        </small>
      </p>
      <table>
        <thead>
          <tr>
            <th>À (s)</th>
            <th>Son</th>
            <th>Image</th>
            <th>Flou (px)</th>
            <th>Pixels (px)</th>
            <th>Gris (0 à 1)</th>
            <th>Photo fixe</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {steps.map((step, i) => (
            <tr key={i}>
              <td>
                <Num o={step} path={['at']} label="" />
              </td>
              <td>
                <Pick o={step} path={['audio']} label="" options={yesNo} bool />
              </td>
              <td>
                <Pick o={step} path={['video']} label="" options={yesNo} bool />
              </td>
              <td>
                <Num o={step} path={['blur']} label="" />
              </td>
              <td>
                <Num o={step} path={['pixelate']} label="" />
              </td>
              <td>
                <Num o={step} path={['grayscale']} label="" />
              </td>
              <td>
                <select
                  value={step.image === undefined ? '-' : step.image}
                  onChange={(e) =>
                    update(() => {
                      if (value(e) === '-') delete step.image
                      else step.image = value(e)
                    })
                  }
                >
                  <option value="-">—</option>
                  <option value="">aucune</option>
                  {images.map((x) => (
                    <option key={x} value={x}>
                      {x}
                    </option>
                  ))}
                </select>
              </td>
              <td>
                <button onClick={() => update(() => steps.splice(i, 1))}>×</button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      <button onClick={() => update(() => (t.reveal = [...steps, { at: Math.round(now * 10) / 10 }]))}>+ étape à {now.toFixed(1)} s</button>
      <h3>Indices</h3>
      {hints.map((h, i) => (
        <div key={i}>
          <Num o={h} path={['at']} label="À (s)" />
          <Text o={h} path={['text']} label="Texte" />
          <button onClick={() => update(() => hints.splice(i, 1))}>×</button>
        </div>
      ))}
      <button onClick={() => update(() => (t.hints = [...hints, { at: Math.round(now * 10) / 10, text: '' }]))}>+ indice à {now.toFixed(1)} s</button>
    </section>
  )
}

export default function Edit() {
  const m = manifest.value
  return (
    <main class="control">
      <header>
        <h1>Éditeur de packs</h1>
        <a href="/control">Pilotage</a>
      </header>
      <section>
        <h2>Packs</h2>
        {packs.value.length === 0 && <p>Aucun pack dossier dans packs/.</p>}
        {packs.value.map((p) => (
          <button key={p} class={p === current.value ? 'selected' : ''} onClick={() => open(p)}>
            {p}
          </button>
        ))}
        <form
          onSubmit={(e) => {
            e.preventDefault()
            create()
          }}
        >
          <input value={newName.value} placeholder="Nom du nouveau pack" onInput={(e) => (newName.value = value(e))} />
          <button disabled={!newName.value.trim()}>Créer</button>
        </form>
      </section>
      {status.value && <p>{status.value}</p>}
      {m && (
        <>
          <section class="savebar">
            <strong>{m.title || current.value}</strong>{' '}
            <button class="primary" disabled={!dirty.value} onClick={save}>
              Enregistrer
            </button>{' '}
            <button disabled={dirty.value || problems.value.length > 0} onClick={exportPack}>
              Exporter en .linospack
            </button>
          </section>
          {problems.value.length > 0 && (
            <section class="warning">
              <p>Pas encore jouable :</p>
              <ul>
                {problems.value.map((p) => (
                  <li key={p}>{p}</li>
                ))}
              </ul>
            </section>
          )}
          <General />
          <Game />
          <details>
            <summary>Règles</summary>
            <Rules o={m} />
          </details>
          <Themes />
          <Dropzone />
          <Tracks />
          <Preview />
          <Rounds />
        </>
      )}
    </main>
  )
}
