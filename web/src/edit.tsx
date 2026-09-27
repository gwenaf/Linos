import { signal } from '@preact/signals'
import { useEffect, useRef, useState } from 'preact/hooks'
import { Banner, IMAGE, Stage, droppedFiles, effectsAt, finalEffects, outroVolume } from './ui'
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
const exported = signal(false)
const step = signal('pack')

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
  exported.value = false
  step.value = 'pack'
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
  exported.value = true
  const size = (d.size / 1e6).toFixed(1)
  status.value = d.cut
    ? `Exporté : packs/${d.file} (${size} Mo), médias découpés aux extraits joués.`
    : `Exporté : packs/${d.file} (${size} Mo). ffmpeg introuvable : médias copiés entiers ; installez-le pour réduire la taille.`
}

// reveal opens the file manager on the pack folder, or on its exported archive.
async function reveal(archive = false) {
  const res = await fetch(packUrl('/reveal' + (archive ? '?archive=1' : '')), { method: 'POST' })
  if (!res.ok) status.value = `Impossible d'ouvrir le dossier : ${await res.text()}`
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
    <div class="preview">
      <h4>
        Aperçu : {now.toFixed(1)} / {duration} s{now >= duration && ' (fin, réponse dévoilée)'}
      </h4>
      <Stage key={`${t.media}-${start}-${run}`} src={mediaUrl(t.media)} effects={effects} imageUrl={mediaUrl} mediaRef={video} />
      {hints
        .filter((h) => (h.at ?? 0) <= now && h.text)
        .map((h, i) => (
          <p class="hint" key={i}>
            {h.text}
          </p>
        ))}
      <button onClick={() => setRun(run + 1)}>Rejouer</button>
      <h4>Effets</h4>
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
                <Num o={step} path={['at']} label="" placeholder="0" />
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
      <h4>Indices</h4>
      {hints.map((h, i) => (
        <div key={i}>
          <Num o={h} path={['at']} label="À (s)" placeholder="0" />
          <Text o={h} path={['text']} label="Texte" />
          <button onClick={() => update(() => hints.splice(i, 1))}>×</button>
        </div>
      ))}
      <button onClick={() => update(() => (t.hints = [...hints, { at: Math.round(now * 10) / 10, text: '' }]))}>+ indice à {now.toFixed(1)} s</button>
    </div>
  )
}


// The editor follows the game: pack, game settings, shared themes, then each round in play order, then export.
// A pack without rounds plays all its tracks in order: its single step is "Pistes".
function steps(): [string, string][] {
  const rounds: Data[] = manifest.value.rounds ?? []
  return [
    ['pack', 'Pack'],
    ['game', 'Partie'],
    ['themes', 'Thèmes'],
    ...(rounds.length > 0 ? rounds.map((r, i): [string, string] => [`round:${i}`, r.name || `Manche ${i + 1}`]) : [['tracks', 'Pistes'] as [string, string]]),
    ['export', 'Export'],
  ]
}

function addRound() {
  update((m) => {
    m.rounds ??= []
    // The first round takes the tracks already played in order.
    if (m.rounds.length === 0 && (m.tracks ?? []).length > 0) {
      m.rounds.push({ name: 'Manche 1', selection: 'sequence', tracks: m.tracks.map((t: Data) => t.id) })
    }
    m.rounds.push({ name: `Manche ${m.rounds.length + 1}`, selection: 'sequence' })
  })
  step.value = `round:${manifest.value.rounds.length - 1}`
}

function Stepper() {
  const all = steps()
  return (
    <nav class="stepper">
      {all.map(([id, label], i) => (
        <button key={id} class={id === step.value ? 'selected' : ''} onClick={() => (step.value = id)}>
          {i + 1} {label}
        </button>
      ))}
      <button title="Ajouter une manche" onClick={addRound}>
        + manche
      </button>
    </nav>
  )
}

function StepNav() {
  const all = steps()
  const i = all.findIndex(([id]) => id === step.value)
  const next = all[i + 1]
  return (
    <nav class="stepnav">
      <button disabled={i <= 0} onClick={() => (step.value = all[i - 1][0])}>
        ‹ Précédent
      </button>
      {next && <button onClick={() => (step.value = next[0])}>{next[0].startsWith('round:') ? `${next[1]} ›` : 'Suivant ›'}</button>}
    </nav>
  )
}

function PackStep() {
  const m = manifest.value
  const images = media.value.filter((x) => IMAGE.test(x))
  return (
    <>
      <h2>Pack</h2>
      <Text o={m} path={['title']} label="Titre" />
      <Text o={m} path={['author']} label="Auteur" />
      <Text o={m} path={['language']} label="Langue" />
      <Pick o={m} path={['cover']} label="Couverture" options={images.map((x) => [x, x])} />
    </>
  )
}

function GameStep() {
  const m = manifest.value
  const g = ['game']
  return (
    <>
      <h2>Partie</h2>
      <Ids o={m} path={[...g, 'control', 'allowed']} label="Contrôles possibles" options={[['master', 'maître du jeu'], ['auto', 'autonome']]} />
      <Pick o={m} path={[...g, 'control', 'default']} label="Contrôle par défaut" options={[['master', 'maître du jeu'], ['auto', 'autonome']]} />
      <br />
      <Pick o={m} path={[...g, 'players', 'teams']} label="Équipes" options={[['none', 'non'], ['optional', 'facultatives'], ['required', 'obligatoires']]} />
      <Num o={m} path={[...g, 'players', 'minTeams']} label="min" int />
      <Num o={m} path={[...g, 'players', 'maxTeams']} label="max" int />
      <br />
      <Pick o={m} path={[...g, 'end', 'type']} label="Fin" options={[['rounds', 'après les manches'], ['score', 'score cible'], ['elimination', 'élimination']]} />
      {m.game?.end?.type === 'score' && <Num o={m} path={[...g, 'end', 'target']} label="Score cible" int />}
      <br />
      <Pick o={m} path={[...g, 'tiebreak', 'type']} label="Égalité" options={[['sudden-death', 'mort subite']]} />
      {m.game?.tiebreak?.type && <Ids o={m} path={[...g, 'tiebreak', 'themes']} label="Thèmes de la mort subite" options={themeOptions()} />}
      <h3>Règles par défaut</h3>
      <p>
        <small>Chaque manche peut les remplacer. Vide : valeur par défaut du jeu.</small>
      </p>
      <Rules o={m} />
    </>
  )
}

function ThemesStep() {
  const themes: Data[] = manifest.value.themes ?? []
  return (
    <>
      <h2>Thèmes</h2>
      <p>
        <small>
          Facultatif. Thèmes communs à toute la partie : les manches tirées au hasard ou au choix des joueurs y puisent leurs pistes, la mort
          subite aussi.
        </small>
      </p>
      {themes.map((t, i) => (
        <div key={t.id}>
          <input value={t.name} placeholder="Nom" onChange={(e) => update(() => (t.name = value(e)))} /> <small class="inline">{t.id}</small>{' '}
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
    </>
  )
}

function newTrackId(tracks: Data[]) {
  const ids = new Set(tracks.map((t) => t.id))
  let n = tracks.length + 1
  while (ids.has(`t${n}`)) n++
  return `t${n}`
}

// upload sends media to the pack; each audio or video file becomes a track, which place files where it belongs.
async function upload(files: File[], place: (m: Data, ids: string[]) => void) {
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
  update((m) => {
    m.tracks ??= []
    const ids: string[] = []
    for (const u of d.uploads) {
      if (IMAGE.test(u.media)) continue
      const id = newTrackId(m.tracks)
      m.tracks.push({ id, media: u.media, guesses: u.guesses })
      ids.push(id)
    }
    place(m, ids)
  })
  media.value = [...media.value, ...d.uploads.map((u: Data) => u.media)]
  status.value = `${d.uploads.length} fichier(s) ajouté(s).`
}

function Dropzone({ onFiles, label }: { onFiles: (files: File[]) => void; label: string }) {
  const onDrop = (e: DragEvent) => {
    e.preventDefault()
    dragging.value = false
    // Entries must be read during the drop event; the item list is emptied once it returns.
    const entries = [...(e.dataTransfer?.items ?? [])].map((i) => i.webkitGetAsEntry()).filter((x): x is FileSystemEntry => !!x)
    droppedFiles(entries).then(onFiles)
  }
  return (
    <label
      class={dragging.value ? 'dropzone over' : 'dropzone'}
      onDragOver={(e) => {
        e.preventDefault()
        dragging.value = true
      }}
      onDragLeave={() => (dragging.value = false)}
      onDrop={onDrop}
    >
      + {label}
      <input type="file" multiple hidden onChange={(e) => onFiles([...((e.target as HTMLInputElement).files ?? [])])} />
    </label>
  )
}

const trackById = (id: string): Data => (manifest.value.tracks ?? []).find((t: Data) => t.id === id)
const clock = (s: number) => `${Math.floor(s / 60)}:${String(Math.floor(s % 60)).padStart(2, '0')}`

// deleteTrack removes a track no longer used by any round.
function deleteTrack(m: Data, id: string) {
  if ((m.rounds ?? []).some((r: Data) => r.tracks?.includes(id))) return
  m.tracks = m.tracks.filter((t: Data) => t.id !== id)
  if (preview.value?.id === id) preview.value = null
}

// TrackList shows tracks one per line; the selected one opens its editor and preview below it.
function TrackList({ ids, move, remove }: { ids: string[]; move?: (i: number, d: number) => void; remove: (id: string) => void }) {
  const defaultDuration = manifest.value.rules?.duration ?? DEFAULT_DURATION
  return (
    <ol class="tracks">
      {ids.map((id, i) => {
        const t = trackById(id)
        if (!t) return <li key={id}>{id} (introuvable)</li>
        const start = t.start ?? 0
        const open = preview.value === t
        return (
          <li key={id} class={open ? 'open' : ''}>
            <div class="row">
              <button class="link" onClick={() => (preview.value = open ? null : t)}>
                {open ? '▾' : '▸'} {t.guesses?.map((g: Data) => g.answers?.[0] ?? g.answer).filter(Boolean).join(' · ') || t.id}
              </button>
              <small class="inline">
                {t.media} {clock(start)} → {clock(start + (t.duration ?? defaultDuration))}
                {t.outro ? ` +${t.outro} s` : ''}
              </small>
              {move && (
                <>
                  <button disabled={i === 0} onClick={() => move(i, -1)}>
                    ↑
                  </button>
                  <button disabled={i === ids.length - 1} onClick={() => move(i, 1)}>
                    ↓
                  </button>
                </>
              )}
              <button onClick={() => remove(id)}>×</button>
            </div>
            {open && <TrackEditor t={t} />}
          </li>
        )
      })}
    </ol>
  )
}

function TrackEditor({ t }: { t: Data }) {
  const defaultDuration = manifest.value.rules?.duration ?? DEFAULT_DURATION
  return (
    <div class="track-editor">
      <label>
        Id{' '}
        <input
          value={t.id}
          onChange={(e) =>
            update(() => {
              renameRef('tracks', t.id, value(e))
              t.id = value(e)
            })
          }
        />
      </label>
      <label>
        Média{' '}
        <select value={t.media} onChange={(e) => update(() => (t.media = value(e)))}>
          {!media.value.includes(t.media) && <option value={t.media}>{t.media} (introuvable)</option>}
          {media.value.map((m) => (
            <option key={m} value={m}>
              {m}
            </option>
          ))}
        </select>
      </label>
      <br />
      <Num o={t} path={['start']} label="Début (s)" placeholder="0" />
      <Num o={t} path={['duration']} label="Durée (s)" placeholder={String(defaultDuration)} />
      <Num o={t} path={['outro']} label="Suite après la fin (s)" placeholder="0" />
      <Num o={t} path={['playbackRate']} label="Vitesse" placeholder="1" />
      {(manifest.value.themes ?? []).length > 0 && <Ids o={t} path={['themes']} label="Thèmes" options={themeOptions()} />}
      <h4>À deviner</h4>
      <Guesses track={t} />
      <Preview />
    </div>
  )
}

function RoundStep({ i }: { i: number }) {
  const m = manifest.value
  const r: Data = m.rounds[i]
  const [uploadTheme, setUploadTheme] = useState('')
  const move = (j: number, d: number) =>
    update(() => {
      const [id] = r.tracks.splice(j, 1)
      r.tracks.splice(j + d, 0, id)
    })
  const moveRound = (d: number) => {
    update((m) => {
      m.rounds.splice(i, 1)
      m.rounds.splice(i + d, 0, r)
    })
    step.value = `round:${i + d}`
  }
  const others = (m.tracks ?? []).filter((t: Data) => !r.tracks?.includes(t.id))
  const pool = (m.tracks ?? []).filter((t: Data) => t.themes?.some((th: string) => r.themes?.includes(th))).map((t: Data) => t.id)
  const target = uploadTheme || r.themes?.[0] || ''
  return (
    <>
      <h2>
        Manche {i + 1}{' '}
        <button disabled={i === 0} onClick={() => moveRound(-1)}>
          ↑
        </button>
        <button disabled={i === m.rounds.length - 1} onClick={() => moveRound(1)}>
          ↓
        </button>
        <button
          onClick={() => {
            if (!confirm(`Supprimer la manche ${i + 1} ? Ses pistes restent dans le pack.`)) return
            update((m) => m.rounds.splice(i, 1))
            step.value = m.rounds.length > 0 ? `round:${Math.max(0, i - 1)}` : 'tracks'
          }}
        >
          Supprimer
        </button>
      </h2>
      <Text o={r} path={['name']} label="Nom" />
      <label>
        Pistes{' '}
        <select value={r.selection} onChange={(e) => update(() => (r.selection = value(e)))}>
          <option value="sequence">liste fixe, dans l'ordre</option>
          <option value="random">tirées au hasard dans des thèmes</option>
          <option value="theme-pick">thème choisi par les joueurs</option>
        </select>
      </label>
      {r.selection === 'sequence' ? (
        <>
          <TrackList
            ids={r.tracks ?? []}
            move={move}
            remove={(id) =>
              update((m) => {
                r.tracks = r.tracks.filter((x: string) => x !== id)
                deleteTrack(m, id)
              })
            }
          />
          <Dropzone label="déposer des médias ou cliquer pour en choisir" onFiles={(f) => upload(f, (_, ids) => (r.tracks = [...(r.tracks ?? []), ...ids]))} />
          {others.length > 0 && (
            <label>
              Reprendre une piste{' '}
              <select value="" onChange={(e) => update(() => (r.tracks = [...(r.tracks ?? []), value(e)]))}>
                <option value="">—</option>
                {others.map((t: Data) => (
                  <option key={t.id} value={t.id}>
                    {t.id} ({t.media})
                  </option>
                ))}
              </select>
            </label>
          )}
        </>
      ) : (
        <>
          <Ids o={r} path={['themes']} label="Thèmes" options={themeOptions()} />
          {themeOptions().length === 0 && (
            <p>
              <small>Créez d'abord des thèmes à l'étape Thèmes.</small>
            </p>
          )}
          <Num o={r} path={['count']} label="Nombre de pistes jouées" int />
          {r.selection === 'theme-pick' && <Pick o={r} path={['picker']} label="Qui choisit" options={[['rotation', 'chacun son tour']]} />}
          <h3>Pistes des thèmes ({pool.length})</h3>
          <TrackList ids={pool} remove={(id) => update((m) => deleteTrack(m, id))} />
          {target && (
            <>
              <label>
                Thème des nouvelles pistes{' '}
                <select value={target} onChange={(e) => setUploadTheme(value(e))}>
                  {(r.themes ?? []).map((th: string) => (
                    <option key={th} value={th}>
                      {themeOptions().find(([id]) => id === th)?.[1] ?? th}
                    </option>
                  ))}
                </select>
              </label>
              <Dropzone
                label="déposer des médias ou cliquer pour en choisir"
                onFiles={(f) => upload(f, (m, ids) => ids.forEach((id) => (trackById(id).themes = [target])))}
              />
            </>
          )}
        </>
      )}
      <Num o={r} path={['eliminate']} label="Éliminés en fin de manche" int />
      <details>
        <summary>Règles propres à cette manche (vide : règles par défaut)</summary>
        <Rules o={r} />
      </details>
    </>
  )
}

// TracksStep edits a pack without rounds: every track, played in order.
function TracksStep() {
  const m = manifest.value
  const ids = (m.tracks ?? []).map((t: Data) => t.id)
  return (
    <>
      <h2>Pistes</h2>
      <p>
        <small>Sans manche, toutes les pistes sont jouées dans l'ordre. « + manche » découpe la partie.</small>
      </p>
      <TrackList
        ids={ids}
        move={(i, d) =>
          update((m) => {
            const [t] = m.tracks.splice(i, 1)
            m.tracks.splice(i + d, 0, t)
          })
        }
        remove={(id) => update((m) => deleteTrack(m, id))}
      />
      <Dropzone label="déposer des médias ou cliquer pour en choisir" onFiles={(f) => upload(f, () => {})} />
    </>
  )
}

function ExportStep() {
  const m = manifest.value
  const rounds: Data[] = m.rounds ?? []
  const used = new Set(rounds.flatMap((r) => (r.selection === 'sequence' ? (r.tracks ?? []) : [])))
  const unused = rounds.some((r) => r.selection !== 'sequence') ? [] : (m.tracks ?? []).filter((t: Data) => !used.has(t.id))
  return (
    <>
      <h2>Export</h2>
      <p>
        {rounds.length || 'Aucune'} manche(s), {(m.tracks ?? []).length} piste(s), {media.value.length} média(s).
      </p>
      {rounds.length > 0 && unused.length > 0 && (
        <p class="warning">
          Pistes dans aucune manche, donc jamais jouées : {unused.map((t: Data) => t.id).join(', ')}.
        </p>
      )}
      {problems.value.length > 0 ? (
        <div class="warning">
          <p>À corriger avant l'export (enregistrez pour mettre à jour) :</p>
          <ul>
            {problems.value.map((p) => (
              <li key={p}>{p}</li>
            ))}
          </ul>
        </div>
      ) : (
        <p>Le pack est jouable.</p>
      )}
      <p>
        <small>ffmpeg, s'il est installé, découpe chaque média aux extraits joués pour alléger le fichier.</small>
      </p>
      <button class="primary" disabled={dirty.value || problems.value.length > 0} onClick={exportPack}>
        Exporter en .linospack
      </button>
      {exported.value && <button onClick={() => reveal(true)}>Afficher le .linospack</button>}
    </>
  )
}

function Landing() {
  return (
    <>
      <Banner />
      <p>
        <small>Éditeur de packs</small>
      </p>
      <h2>Ouvrir un pack</h2>
      {packs.value.length === 0 && (
        <p>
          <small>Aucun pack dossier dans packs/.</small>
        </p>
      )}
      <ul class="packs">
        {packs.value.map((p) => (
          <li key={p}>
            <button class="link" onClick={() => open(p)}>
              ▸ {p}
            </button>
          </li>
        ))}
      </ul>
      <h2>Nouveau pack</h2>
      <form
        onSubmit={(e) => {
          e.preventDefault()
          create()
        }}
      >
        <input value={newName.value} placeholder="Nom du dossier" onInput={(e) => (newName.value = value(e))} />{' '}
        <button disabled={!newName.value.trim()}>Créer</button>
      </form>
    </>
  )
}

function close() {
  if (dirty.value && !confirm('Modifications non enregistrées : les abandonner ?')) return
  manifest.value = null
  current.value = ''
  dirty.value = false
  status.value = ''
  listPacks()
}

export default function Edit() {
  const m = manifest.value
  const s = step.value
  return (
    <main class="edit">
      {!m ? (
        <Landing />
      ) : (
        <>
          <header class="topbar">
            <button class="link" onClick={close}>
              ‹ Packs
            </button>
            <strong>{m.title || current.value}</strong>
            <span class="spacer" />
            {status.value && <small class="inline">{status.value}</small>}
            <button onClick={() => reveal()}>Ouvrir le dossier</button>
            <button class="primary" disabled={!dirty.value} onClick={save}>
              {dirty.value ? 'Enregistrer' : 'Enregistré'}
            </button>
          </header>
          <Stepper />
          <section class="step">
            {s === 'pack' && <PackStep />}
            {s === 'game' && <GameStep />}
            {s === 'themes' && <ThemesStep />}
            {s === 'tracks' && <TracksStep />}
            {s.startsWith('round:') && m.rounds?.[Number(s.slice(6))] && <RoundStep key={s} i={Number(s.slice(6))} />}
            {s === 'export' && <ExportStep />}
          </section>
          <StepNav />
        </>
      )}
      {!m && status.value && <p>{status.value}</p>}
    </main>
  )
}
