import { signal } from '@preact/signals'
import { useEffect, useRef, useState } from 'preact/hooks'
import { Banner, IMAGE, Stage, droppedFiles, effectsAt, finalEffects, outroVolume, whenLoaded } from './ui'
import { Waveform } from './waveform'
import { merge } from './merge'
import { type RoundType, applyType, baseRules, describe, roundTypes, typeOf } from './rounds'
import type { Data } from './ws'

// Pack editor: folder packs of packs/, host rights only. The manifest is edited as loaded, so fields
// this page does not show yet are kept when saving. One screen: the questions on the left, the open
// question (or a round, the settings, the export) on the right; every change is saved shortly after.
const packs = signal<string[]>([])
const archives = signal<string[]>([])
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
// playlist holds the tracks of a round being previewed, in play order.
const playlist = signal<string[]>([])
// step is what the right panel shows: "q:<track id>", "theme:<id>", "round:<index>", "new-round", "wizard",
// "settings" or "export".
const step = signal('settings')

const AUDIO = /\.(mp3|m4a|aac|ogg|opus|flac|wav)$/i
const MEDIA = /\.(mp3|m4a|aac|ogg|opus|flac|wav|mp4|m4v|webm|jpe?g|png|webp|gif)$/i
const DEFAULT_DURATION = 30

const packUrl = (suffix = '') => `/api/edit/${encodeURIComponent(current.value)}${suffix}`
const mediaUrl = (name: string) => packUrl(`/media/${name.split('/').map(encodeURIComponent).join('/')}`)
const value = (e: Event) => (e.target as HTMLInputElement).value
const optionalNumber = (v: string) => (v === '' ? undefined : Number(v))

addEventListener('beforeunload', (e) => {
  if (dirty.value) e.preventDefault()
})

// request is fetch, saying so when Linos cannot be reached (closed, or stopped) instead of failing silently.
async function request(url: string, init?: RequestInit) {
  try {
    return await fetch(url, init)
  } catch (err) {
    status.value = "Linos ne répond pas : vérifiez qu'il est toujours lancé, puis réessayez."
    throw err
  }
}

async function listPacks() {
  const d = await (await request('/api/edit')).json()
  packs.value = d.folders
  archives.value = d.archives
}

// unpack copies an exported archive into a new folder pack, then opens it.
async function unpack(name: string) {
  const res = await request('/api/edit/unpack', { method: 'POST', body: JSON.stringify({ name }) })
  if (!res.ok) {
    status.value = `Impossible d'ouvrir l'archive : ${await res.text()}`
    return
  }
  const d = await res.json()
  await listPacks()
  await open(d.name)
}
listPacks()

async function open(name: string) {
  await save()
  const res = await request(`/api/edit/${encodeURIComponent(name)}`)
  if (!res.ok) {
    status.value = await res.text()
    return
  }
  const d = await res.json()
  current.value = name
  base = structuredClone(d.manifest)
  version = res.headers.get('ETag') ?? ''
  manifest.value = d.manifest
  media.value = d.media
  problems.value = d.problems
  dirty.value = false
  preview.value = null
  status.value = ''
  exported.value = false
  playlist.value = []
  // A new pack opens on the guided setup.
  const first = d.manifest.tracks?.[0]
  if (first) select(first.id)
  else if (d.manifest.rounds?.length) step.value = 'round:0'
  else {
    wizardStep.value = 0
    step.value = 'wizard'
  }
}

const importStart = signal(30)
const importDuration = signal(DEFAULT_DURATION)

// dropFiles starts from what the user has: audio files become a new pack opened on its tracks,
// .linospack archives received from someone join the library once checked by the server.
async function dropFiles(files: File[]) {
  const audio = files.filter((f) => AUDIO.test(f.name))
  const packFiles = files.filter((f) => f.name.endsWith('.linospack'))
  if (audio.length === 0 && packFiles.length === 0) {
    status.value = 'Ni fichier audio (mp3, m4a, aac, ogg, opus, flac, wav) ni pack .linospack.'
    return
  }
  const messages: string[] = []
  if (packFiles.length > 0) {
    status.value = `Ajout de ${packFiles.length} pack(s)…`
    const form = new FormData()
    packFiles.forEach((f) => form.append('files', f, f.name))
    const res = await request('/api/edit/archives', { method: 'POST', body: form })
    if (!res.ok) messages.push(`Échec de l'ajout : ${await res.text()}`)
    else {
      const d = await res.json()
      for (const name of d.saved) if (!d.invalid[name]) messages.push(`Pack ajouté : ${name}.`)
      for (const [name, err] of Object.entries(d.invalid)) messages.push(`${name} refusé, ce n'est pas un pack valide : ${err}`)
    }
    await listPacks()
  }
  if (audio.length > 0) {
    status.value = `Import de ${audio.length} fichier(s)…`
    const form = new FormData()
    audio.forEach((f) => form.append('files', f, f.name))
    const res = await request(`/api/import?start=${importStart.value}&duration=${importDuration.value}`, { method: 'POST', body: form })
    if (!res.ok) messages.push(`Échec de l'import : ${await res.text()}`)
    else {
      const d = await res.json()
      await listPacks()
      await open(d.pack)
      messages.push(`${d.tracks} piste(s) importée(s) : vérifiez les réponses lues dans les tags.`)
    }
  }
  status.value = messages.join(' ')
}

async function create() {
  const name = newName.value.trim()
  const res = await request('/api/edit', { method: 'POST', body: JSON.stringify({ name }) })
  if (!res.ok) {
    status.value = `Création impossible : ${await res.text()}`
    return
  }
  newName.value = ''
  await listPacks()
  await open(name)
}

// Saves run one after the other, so an older manifest never lands after a newer one.
let saving = Promise.resolve()
let saveTimer: ReturnType<typeof setTimeout> | undefined
let edits = 0
// Another editor may work on the same pack: base is the last manifest known to be on disk and version
// its tag. A save from an older version is refused; the other editor's changes are then merged in.
let base: Data = null
let version = ''

// save writes the manifest if it changed; it resolves once every pending save is done.
function save() {
  clearTimeout(saveTimer)
  saving = saving.then(saveNow).catch(() => {}) // request already says when Linos is unreachable
  return saving
}

async function saveNow(): Promise<void> {
  if (!dirty.value || !current.value) return
  const sent = edits
  const body = JSON.stringify(prune(manifest.value))
  status.value = 'Enregistrement…'
  const res = await request(packUrl(), { method: 'PUT', headers: { 'If-Match': version }, body })
  if (res.status === 412) {
    await pull()
    return saveNow()
  }
  if (!res.ok) {
    status.value = `Échec de l'enregistrement : ${await res.text()}`
    return
  }
  const d = await res.json()
  base = JSON.parse(body)
  version = res.headers.get('ETag') ?? ''
  if (sent !== edits) return // changed meanwhile: the next save reports
  problems.value = d.problems
  dirty.value = false
  status.value = 'Enregistré'
}

// pull brings in what another editor saved, merged with the changes not saved here yet.
async function pull() {
  const name = current.value
  const res = await request(packUrl(), { headers: { 'If-None-Match': version } })
  if (res.status !== 200 || name !== current.value) return
  const d = await res.json()
  const merged = merge(base, prune(manifest.value), d.manifest) as Data
  base = d.manifest
  version = res.headers.get('ETag') ?? ''
  media.value = d.media
  problems.value = d.problems
  manifest.value = merged
  if (preview.value) preview.value = trackById(preview.value.id) ?? null
  status.value = 'Modifié par un autre éditeur : changements repris.'
  if (JSON.stringify(merged) !== JSON.stringify(d.manifest)) {
    dirty.value = true
    edits++
  }
}

// Every two seconds, an open pack takes in the changes of other editors (unchanged: a 304, nothing to do).
let pulling = false
setInterval(() => {
  if (!current.value || pulling || document.hidden) return
  pulling = true
  saving = saving
    .then(pull)
    .catch(() => {})
    .finally(() => (pulling = false))
}, 2000)

// exportPack writes packs/<name>.linospack, media cut by ffmpeg when it is installed.
async function exportPack() {
  await save()
  status.value = 'Export en cours : le découpage des médias peut prendre quelques minutes…'
  const res = await request(packUrl('/export'), { method: 'POST' })
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
  const res = await request(packUrl('/reveal' + (archive ? '?archive=1' : '')), { method: 'POST' })
  if (!res.ok) status.value = `Impossible d'ouvrir le dossier : ${await res.text()}`
}

// update edits the manifest in place, then publishes a new reference so the page renders again.
// The change is saved automatically a moment later: no button, nothing to forget.
function update(fn: (m: Data) => void) {
  fn(manifest.value)
  manifest.value = { ...manifest.value }
  dirty.value = true
  edits++
  clearTimeout(saveTimer)
  saveTimer = setTimeout(save, 800)
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

// A field left empty takes the value at the same path of base, shown greyed: the pack rules for a
// round, the game defaults for the pack.
function Num({ o, path, label, int, placeholder, base }: Field & { int?: boolean; placeholder?: string; base?: Data }) {
  const inherited = base && get(base, path)
  return (
    <label>
      {label}{' '}
      <input
        type="number"
        step={int ? 1 : 'any'}
        value={get(o, path) ?? ''}
        placeholder={placeholder ?? (inherited === undefined ? undefined : String(inherited))}
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
function Pick({ o, path, label, options, bool, base }: Field & { options: [string, string][]; bool?: boolean; base?: Data }) {
  const v = get(o, path)
  const inherited = base && get(base, path)
  const inheritedText = inherited === undefined ? '' : (options.find(([x]) => x === String(inherited))?.[1] ?? String(inherited))
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
        <option value="">{inheritedText ? `${inheritedText} (par défaut)` : '—'}</option>
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
  const base = { rules: baseRules(o === manifest.value ? undefined : manifest.value) }
  return (
    <div class="rules">
      <Num o={o} path={r('duration')} label="Durée d'une piste (s)" base={base} />
      <fieldset>
        <legend>Réponses</legend>
        <Pick o={o} path={r('answer', 'mode')} label="Mode" options={[['buzz', 'buzz'], ['simultaneous', 'simultanées']]} base={base} />
        <Pick o={o} path={r('answer', 'via')} label="Sur" options={[['oral', "l'oral"], ['device', 'le téléphone']]} base={base} />
        <Num o={o} path={r('answer', 'answerTime')} label="Temps de réponse (s)" base={base} />
        <Pick o={o} path={r('answer', 'pauseOnBuzz')} label="Pause au buzz" options={yesNo} bool base={base} />
        <Num o={o} path={r('answer', 'attempts')} label="Essais" int base={base} />
        <Num o={o} path={r('answer', 'wrongLockout')} label="Blocage après erreur (s)" base={base} />
        <Pick o={o} path={r('answer', 'rebound')} label="Rebond" options={[['none', 'aucun'], ['others', 'les autres'], ['all', 'tout le monde']]} base={base} />
        <Num o={o} path={r('answer', 'fuzziness')} label="Tolérance aux fautes (0 à 1)" base={base} />
        <Pick
          o={o}
          base={base}
          path={r('answer', 'partial')}
          label="Réponse incomplète (validée par le maître du jeu)"
          options={[
            ['each', 'chaque élément trouvé rapporte'],
            ['all', 'tout ou rien : comptée fausse'],
          ]}
        />
        <Num o={o} path={r('answer', 'incompletePenalty')} label="Pénalité si incomplète" int base={base} />
      </fieldset>
      <fieldset>
        <legend>Points</legend>
        <Pick
          o={o}
          base={base}
          path={r('scoring', 'type')}
          label="Barème"
          options={[['speed', 'rapidité'], ['fixed', 'fixe'], ['rank', 'classement'], ['wager', 'mise']]}
        />
        <Num o={o} path={r('scoring', 'max')} label="Max" int base={base} />
        <Num o={o} path={r('scoring', 'min')} label="Min" int base={base} />
        <label>
          Classement{' '}
          <input
            value={ranks?.join(', ') ?? ''}
            placeholder={get(base, r('scoring', 'ranks'))?.join(', ')}
            onChange={(e) => put(o, r('scoring', 'ranks'), value(e).split(',').map((x) => x.trim()).filter(Boolean).map(Number))}
          />
        </label>
        <Num o={o} path={r('scoring', 'wrongPenalty')} label="Pénalité d'erreur" int base={base} />
        <Num o={o} path={r('scoring', 'reboundBonus')} label="Bonus de rebond" int base={base} />
        <Num o={o} path={r('jokers', 'double')} label="Jokers double" int base={base} />
      </fieldset>
      <fieldset>
        <legend>Thème choisi</legend>
        <Num o={o} path={r('owner', 'headStart')} label="Avance du choisisseur (s)" base={base} />
        <Pick o={o} path={r('owner', 'exclusive')} label="Exclusif" options={yesNo} bool base={base} />
        <Num o={o} path={r('owner', 'othersBonus')} label="Bonus des autres" int base={base} />
        <Num o={o} path={r('owner', 'othersMultiplier')} label="Points des autres multipliés par" int base={base} />
        <Num o={o} path={r('owner', 'stolenPenalty')} label="Le choisisseur perd (points fixes)" int base={base} />
        <Pick o={o} path={r('owner', 'stolenLosesPoints')} label="Le choisisseur perd les points volés" options={yesNo} bool base={base} />
      </fieldset>
    </div>
  )
}

const splitList = (v: string) => v.split(';').map((a) => a.trim()).filter(Boolean)

// retype switches a guess between free text, multiple choice and number, keeping what still fits.
function retype(g: Data, type: string) {
  const known: string[] = g.type === 'choice' ? g.choices ?? [] : g.type === 'text' ? g.answers ?? [] : []
  for (const k of ['answers', 'answer', 'choices', 'choicesAt', 'tolerance']) delete g[k]
  g.type = type
  if (type === 'text') g.answers = known
  if (type === 'choice') {
    g.choices = known
    if (known.length > 0) g.answer = known[0]
  }
}

// Guesses edits what players must find: free text, a multiple choice or a number.
function Guesses({ track }: { track: Data }) {
  return (
    <>
      {(track.guesses ?? []).map((g: Data, j: number) => (
        <div key={j}>
          <input value={g.label} placeholder="Élément (ex. Artiste)" onInput={(e) => update(() => (g.label = value(e)))} />{' '}
          <select value={g.type} onChange={(e) => update(() => retype(g, value(e)))}>
            <option value="text">texte libre</option>
            <option value="choice">QCM</option>
            <option value="number">nombre</option>
          </select>{' '}
          <button onClick={() => update(() => track.guesses.splice(j, 1))}>×</button>
          <br />
          {g.type === 'text' && (
            <input
              value={(g.answers ?? []).join(' ; ')}
              placeholder="Réponses acceptées, séparées par ;"
              onChange={(e) => update(() => (g.answers = splitList(value(e))))}
            />
          )}
          {g.type === 'choice' && (
            <>
              <input
                value={(g.choices ?? []).join(' ; ')}
                placeholder="Choix proposés, séparés par ;"
                onChange={(e) =>
                  update(() => {
                    g.choices = splitList(value(e))
                    if (!g.choices.includes(g.answer)) g.answer = g.choices[0]
                  })
                }
              />
              <label>
                Bonne réponse{' '}
                <select value={g.answer ?? ''} onChange={(e) => update(() => (g.answer = value(e)))}>
                  {(g.choices ?? []).map((c: string) => (
                    <option key={c} value={c}>
                      {c}
                    </option>
                  ))}
                </select>
              </label>
              <Num o={g} path={['choicesAt']} label="Choix affichés à (s)" placeholder="0" />
            </>
          )}
          {g.type === 'number' && (
            <>
              <Num o={g} path={['answer']} label="Réponse" />
              <Num o={g} path={['tolerance']} label="Tolérance ±" placeholder="0" />
            </>
          )}
        </div>
      ))}
      <button onClick={() => update(() => (track.guesses = [...(track.guesses ?? []), { label: '', type: 'text', answers: [] }]))}>
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
  // Playing only on demand: the preview must not cover the file being located above.
  const [playing, setPlaying] = useState(playlist.value.length > 0)
  const start = t.start ?? 0
  const rate = t.playbackRate ?? 1
  const duration = t.duration ?? manifest.value.rules?.duration ?? DEFAULT_DURATION
  const outro = t.outro ?? 0

  useEffect(() => {
    if (!playing) return
    const began = performance.now()
    let done = false
    const v = video.current
    if (v) {
      whenLoaded(v, () => {
        v.currentTime = start
        v.playbackRate = rate
        v.play().catch(() => {})
      })
    }
    // Pictures have no media clock: the preview counts wall time.
    const id = setInterval(() => {
      const elapsed = v ? (v.currentTime - start) / rate : (performance.now() - began) / 1000
      if (v) v.volume = elapsed < duration ? 1 : outroVolume(outro, elapsed - duration)
      if (elapsed >= duration + outro && !done) {
        done = true
        v?.pause()
        setPlaying(false)
        playNext(t)
      }
      setNow(Math.max(0, Math.min(elapsed, duration + outro)))
    }, 100)
    return () => {
      clearInterval(id)
      v?.pause()
    }
  }, [t.media, start, rate, duration, outro, run, playing])

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
      {playing ? (
        <button onClick={() => setPlaying(false)}>Arrêter</button>
      ) : (
        <button
          onClick={() => {
            setNow(0)
            setRun(run + 1)
            setPlaying(true)
          }}
        >
          Écouter l'extrait
        </button>
      )}
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

// addRound adds an empty round of a type; the first one takes the questions already there, those of a
// theme excepted: they are drawn by theme rounds.
function addRound(type: RoundType, open = true) {
  update((m) => {
    m.rounds ??= []
    const loose = (m.tracks ?? []).filter((t: Data) => !t.themes?.length)
    if (m.rounds.length === 0 && loose.length > 0) {
      m.rounds.push({ name: 'Manche 1', selection: 'sequence', tracks: loose.map((t: Data) => t.id) })
    }
    const r: Data = { name: `Manche ${m.rounds.length + 1}` }
    applyType(r, type)
    m.rounds.push(r)
  })
  if (open) step.value = `round:${manifest.value.rounds.length - 1}`
}

// Card is one choice among a few, with what it does.
function Card({ on, title, text, pick }: { on: boolean; title: string; text: string; pick: () => void }) {
  return (
    <button class={on ? 'card selected' : 'card'} onClick={pick}>
      <strong>{title}</strong>
      <small>{text}</small>
    </button>
  )
}

// TypeCards offers the round types; the one a round still matches is selected.
function TypeCards({ current, onPick }: { current?: RoundType; onPick: (t: RoundType) => void }) {
  return (
    <div class="cards">
      {roundTypes.map((t) => (
        <Card key={t.name} on={t === current} title={t.name} text={t.text} pick={() => onPick(t)} />
      ))}
    </div>
  )
}

// Summary tells in plain words how a round plays, or the pack rules without a round.
function Summary({ r }: { r?: Data }) {
  return (
    <div class="summary">
      <h3>{r ? 'Ce qui se passera' : 'En jeu, par défaut'}</h3>
      <ul>
        {describe(manifest.value, r).map((x, i) => (
          <li key={i}>{x}</li>
        ))}
      </ul>
    </div>
  )
}

function NewRoundPanel() {
  return (
    <>
      <h2>Nouvelle manche</h2>
      <p>
        <small>Choisissez un type de manche : tout reste modifiable ensuite.</small>
      </p>
      <TypeCards onPick={(t) => addRound(t)} />
    </>
  )
}

const wizardStep = signal(0)

// finishWizard leaves the guided setup on the first question to fill.
function finishWizard() {
  const m = manifest.value
  const i = (m.rounds ?? []).findIndex((r: Data) => r.selection === 'sequence')
  const theme = (m.rounds ?? []).find((r: Data) => r.themes?.length)?.themes[0]
  if (i >= 0) addQuestion(i)
  else if (theme) addQuestion(undefined, theme)
  else addQuestion()
}

// Players chooses who plays (each for themselves or in teams) and who referees, as cards.
function Players() {
  const m = manifest.value
  const teams = m.game?.players?.teams ?? 'none'
  const allowed: string[] = m.game?.control?.allowed ?? []
  const control = allowed.length === 1 ? allowed[0] : ''
  const setControl = (c: string) =>
    update((m) => {
      m.game ??= {}
      if (c) m.game.control = { allowed: [c], default: c }
      else delete m.game.control
      // Without a gamemaster nobody hears oral answers: they are typed on the phones.
      m.rules ??= {}
      m.rules.answer ??= {}
      if (c === 'auto') m.rules.answer.via = 'device'
      else delete m.rules.answer.via
    })
  const setTeams = (t: string) => put(m, ['game', 'players', 'teams'], t === 'none' ? undefined : t)
  return (
    <>
      <h3>Qui joue ?</h3>
      <div class="cards">
        <Card on={teams === 'none'} title="Chacun pour soi" text="Chaque joueur marque ses points." pick={() => setTeams('none')} />
        <Card on={teams === 'required'} title="En équipes" text="Les joueurs rejoignent une équipe, qui marque les points." pick={() => setTeams('required')} />
        <Card on={teams === 'optional'} title="Au choix" text="Équipes ou non, décidé au lancement de la partie." pick={() => setTeams('optional')} />
      </div>
      <h3>Qui arbitre ?</h3>
      <div class="cards">
        <Card on={control === 'master'} title="Un maître du jeu" text="Il pilote la partie, écoute les réponses à l'oral et les valide." pick={() => setControl('master')} />
        <Card on={control === 'auto'} title="Personne" text="Les réponses sont tapées sur les téléphones et vérifiées automatiquement." pick={() => setControl('auto')} />
        <Card on={control === ''} title="Au choix" text="Décidé au lancement de la partie." pick={() => setControl('')} />
      </div>
    </>
  )
}

// Wizard sets a new pack up in two steps: who plays and who referees, then its rounds.
function Wizard() {
  const m = manifest.value
  const rounds: Data[] = m.rounds ?? []
  return (
    <div class="wizard">
      <h2>
        Nouveau pack · {wizardStep.value === 0 ? 'Joueurs' : 'Manches'} <small class="inline">({wizardStep.value + 1}/2)</small>
      </h2>
      {wizardStep.value === 0 ? (
        <>
          <Players />
          <p>
            <button class="primary" onClick={() => (wizardStep.value = 1)}>
              Suivant
            </button>
          </p>
        </>
      ) : (
        <>
          <p>
            <small>Ajoutez les manches dans l'ordre où elles seront jouées ; les questions viendront ensuite.</small>
          </p>
          {rounds.length > 0 && (
            <ol class="wizard-rounds">
              {rounds.map((r, i) => (
                <li key={i}>
                  <strong>{r.name}</strong> · {typeOf(r)?.name ?? 'personnalisée'}{' '}
                  {r.selection !== 'sequence' && (
                    <input
                      value={(r.themes ?? []).map(themeName).join(' ; ')}
                      placeholder="Thèmes, séparés par ; (ex. Rock ; Années 80)"
                      onChange={(e) => update((m) => (r.themes = splitList(value(e)).map((name) => themeByName(m, name))))}
                    />
                  )}{' '}
                  <button onClick={() => update((m) => m.rounds.splice(i, 1))}>×</button>
                </li>
              ))}
            </ol>
          )}
          <h3>{rounds.length ? 'Ajouter une manche' : 'Première manche'}</h3>
          <TypeCards onPick={(t) => addRound(t, false)} />
          <p>
            <button onClick={() => (wizardStep.value = 0)}>Précédent</button>{' '}
            <button class="primary" disabled={rounds.length === 0} onClick={finishWizard}>
              Terminer
            </button>
          </p>
        </>
      )}
      <p>
        <button class="link" onClick={finishWizard}>
          Passer : tout régler soi-même
        </button>
      </p>
    </div>
  )
}

// select opens a question; a round preview going on is stopped unless it is the one moving on.
function select(id: string, keepPlaylist = false) {
  if (!keepPlaylist) playlist.value = []
  step.value = `q:${id}`
  preview.value = trackById(id) ?? null
}

// go shows the place of a problem, or any step.
function go(to: string) {
  if (to.startsWith('q:')) select(to.slice(2))
  else step.value = to
}

const emptyGuesses = () => [
  { label: 'Artiste', type: 'text', answers: [] },
  { label: 'Titre', type: 'text', answers: [] },
]

// addQuestion adds an empty question and opens it: in a theme when given, else in a round when given
// (its list, or its first theme).
function addQuestion(round?: number, theme?: string) {
  let id = ''
  update((m) => {
    m.tracks ??= []
    id = newTrackId(m.tracks)
    const t: Data = { id, media: '', guesses: emptyGuesses() }
    const r = round === undefined ? undefined : m.rounds?.[round]
    if (theme) t.themes = [theme]
    else if (r?.selection === 'sequence') r.tracks = [...(r.tracks ?? []), id]
    else if (r?.themes?.length) t.themes = [r.themes[0]]
    m.tracks.push(t)
  })
  select(id)
}

function removeQuestion(id: string) {
  if (!confirm('Supprimer cette question ?')) return
  update((m) => {
    renameRef('tracks', id)
    m.tracks = m.tracks.filter((t: Data) => t.id !== id)
  })
  const first = manifest.value.tracks?.[0]
  if (first) select(first.id)
  else step.value = 'settings'
}

// questionList is the list a question is ordered in: its sequence round's, else the pack's.
function questionList(m: Data, id: string): { list: Data[]; at: number } {
  const r = (m.rounds ?? []).find((r: Data) => r.selection === 'sequence' && r.tracks?.includes(id))
  return r ? { list: r.tracks, at: r.tracks.indexOf(id) } : { list: m.tracks, at: m.tracks.findIndex((t: Data) => t.id === id) }
}

// moveQuestion moves a question to another position of its list.
function moveQuestion(id: string, to: number) {
  update((m) => {
    const { list, at } = questionList(m, id)
    if (at < 0 || to < 0 || to >= list.length || to === at) return
    const [x] = list.splice(at, 1)
    list.splice(to, 0, x)
  })
}

// questionName names a question by its answers, else its file.
function questionName(t: Data): string {
  const answers = (t.guesses ?? []).map((g: Data) => g.answers?.[0] ?? g.answer).filter((a: unknown) => a !== undefined && a !== '')
  return answers.join(' · ') || t.media?.split('/').pop() || 'nouvelle question'
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
      <Players />
      {m.game?.players?.teams && m.game.players.teams !== 'none' && (
        <p>
          <Num o={m} path={[...g, 'players', 'minTeams']} label="Équipes : au moins" int />
          <Num o={m} path={[...g, 'players', 'maxTeams']} label="au plus" int />
        </p>
      )}
      <h3>Fin de partie</h3>
      <Pick
        o={m}
        path={[...g, 'end', 'type']}
        label="La partie finit"
        options={[['rounds', 'après les manches'], ['score', 'à un score cible'], ['elimination', "quand il ne reste qu'un joueur"]]}
        base={{ game: { end: { type: 'rounds' } } }}
      />
      {m.game?.end?.type === 'score' && <Num o={m} path={[...g, 'end', 'target']} label="Score cible" int />}
      <Pick o={m} path={[...g, 'tiebreak', 'type']} label="En cas d'égalité" options={[['sudden-death', 'mort subite']]} />
      {m.game?.tiebreak?.type && <Ids o={m} path={[...g, 'tiebreak', 'themes']} label="Thèmes de la mort subite" options={themeOptions()} />}
      <h3>Règles</h3>
      <Summary />
      <details>
        <summary>Réglages avancés (chaque manche peut les remplacer)</summary>
        <Rules o={m} />
      </details>
    </>
  )
}

// addTheme creates a theme, drawn by a round when given, and opens it.
function addTheme(name: string, round?: number) {
  if (!name.trim()) return
  let id = ''
  update((m) => {
    id = themeByName(m, name)
    const r = round === undefined ? undefined : m.rounds?.[round]
    if (r && !r.themes?.includes(id)) r.themes = [...(r.themes ?? []), id]
  })
  step.value = `theme:${id}`
}

// themeByName is the id of the theme of that name, created when missing.
function themeByName(m: Data, name: string): string {
  name = name.trim()
  m.themes ??= []
  const known = m.themes.find((t: Data) => t.name === name)
  if (known) return known.id
  const base = name.toLowerCase().normalize('NFD').replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '') || 'theme'
  let id = base
  for (let i = 2; m.themes.some((t: Data) => t.id === id); i++) id = `${base}-${i}`
  m.themes.push({ id, name })
  return id
}

const themeQuestions = (id: string): string[] => (manifest.value.tracks ?? []).filter((t: Data) => t.themes?.includes(id)).map((t: Data) => t.id)
const themeName = (id: string): string => (manifest.value.themes ?? []).find((t: Data) => t.id === id)?.name || id

// ThemePanel names a theme, takes its questions in bulk and tells which rounds draw from it.
function ThemePanel({ id }: { id: string }) {
  const m = manifest.value
  const i = (m.themes ?? []).findIndex((t: Data) => t.id === id)
  const th: Data = m.themes[i]
  const rounds: [Data, number][] = (m.rounds ?? [])
    .map((r: Data, j: number): [Data, number] => [r, j])
    .filter(([r]: [Data, number]) => r.selection !== 'sequence' && r.themes?.includes(id))
  return (
    <>
      <h2>
        Thème · {th.name || id}{' '}
        <button
          onClick={() => {
            if (!confirm(`Supprimer le thème « ${th.name || id} » ? Ses questions restent dans le pack.`)) return
            update((m) => {
              m.themes.splice(i, 1)
              renameRef('themes', id)
            })
            step.value = 'settings'
          }}
        >
          Supprimer
        </button>
      </h2>
      <Problems only={`theme:${id}`} />
      <Text o={th} path={['name']} label="Nom" />
      <p>
        {themeQuestions(id).length} question(s). Chacune a son extrait et sa révélation : ajoutez-les avec « + question » sous le thème, à gauche,
        ou déposez ici plusieurs fichiers (une question par fichier).
      </p>
      <Dropzone label="Déposer plusieurs fichiers dans ce thème" onFiles={(f) => upload(f, (m, ids) => ids.forEach((q) => (trackById(q).themes = [id])))} />
      {rounds.length > 0 ? (
        <p>
          <small>
            Joué dans :{' '}
            {rounds.map(([r, j], k) => (
              <span key={j}>
                {k > 0 && ', '}
                <button class="link" onClick={() => (step.value = `round:${j}`)}>
                  {r.name || `Manche ${j + 1}`}
                </button>
              </span>
            ))}
          </small>
        </p>
      ) : (
        <p class="warning">
          Aucune manche ne tire ce thème : cochez-le dans une manche « Au hasard » ou « Thème au choix », sinon ses questions ne sont jouées
          que dans un pack sans manche.
        </p>
      )}
    </>
  )
}

// NewTheme adds a theme by its name, to a round when given.
function NewTheme({ round }: { round?: number }) {
  return (
    <form
      class="new-theme"
      onSubmit={(e) => {
        e.preventDefault()
        const input = (e.target as HTMLFormElement).elements.namedItem('name') as HTMLInputElement
        addTheme(input.value, round)
        input.value = ''
      }}
    >
      <input name="name" placeholder="Nouveau thème" /> <button>+ thème</button>
    </form>
  )
}

// playNext moves a round preview to the next track, or ends it after the last one.
function playNext(t: Data) {
  const i = playlist.value.indexOf(t.id)
  if (i < 0) return
  const next = playlist.value[i + 1]
  if (next) select(next, true)
  else playlist.value = []
}

// previewRound plays a round as the game would: its list in order, or a random draw from its themes.
function previewRound(r: Data, pool: string[]) {
  const ids = r.selection === 'sequence' ? [...(r.tracks ?? [])] : [...pool].sort(() => Math.random() - 0.5).slice(0, r.count || pool.length)
  if (ids.length === 0) return
  select(ids[0])
  playlist.value = ids
}

function RoundPreview({ r, pool }: { r: Data; pool: string[] }) {
  if (playlist.value.length === 0) {
    return <button onClick={() => previewRound(r, pool)}>▶ Aperçu de la manche</button>
  }
  const i = playlist.value.indexOf(preview.value?.id) + 1
  return (
    <button onClick={() => (playlist.value = [])}>
      ■ Arrêter l'aperçu ({i} / {playlist.value.length})
    </button>
  )
}

// newTrackId is random, so two editors adding a question at once never pick the same id.
function newTrackId(tracks: Data[]) {
  const ids = new Set(tracks.map((t) => t.id))
  let id = ''
  while (!id || ids.has(id)) id = `t${Math.random().toString(36).slice(2, 8)}`
  return id
}

// sendMedia uploads files to the pack and returns, for each, its name and the guesses its tags suggest.
async function sendMedia(files: File[]): Promise<Data[] | null> {
  files = files.filter((f) => MEDIA.test(f.name))
  if (files.length === 0) {
    status.value = 'Aucun média (audio, vidéo ou image).'
    return null
  }
  status.value = `Envoi de ${files.length} fichier(s)…`
  const form = new FormData()
  files.forEach((f) => form.append('files', f, f.name))
  const res = await request(packUrl('/media'), { method: 'POST', body: form })
  if (!res.ok) {
    status.value = `Échec de l'envoi : ${await res.text()}`
    return null
  }
  const uploads: Data[] = (await res.json()).uploads
  media.value = [...new Set([...media.value, ...uploads.map((u) => u.media)])]
  status.value = `${uploads.length} fichier(s) ajouté(s).`
  return uploads
}

// usedBy finds the question, other than except, already playing a file: the server reports an
// identical file under its existing name, so a song dropped twice is caught.
const usedBy = (name: string, except?: Data): Data | undefined => (manifest.value.tracks ?? []).find((t: Data) => t !== except && t.media === name)

// mayReuse asks before a question takes a song another question already plays (two extracts of
// one song are allowed, a slip is not).
function mayReuse(t: Data, name: string): boolean {
  const other = usedBy(name, t)
  return !other || confirm(`Ce morceau est déjà la question « ${questionName(other)} ». L'utiliser aussi ici ?`)
}

// upload adds one question per audio or video file; place puts the new ids where they belong.
async function upload(files: File[], place: (m: Data, ids: string[]) => void) {
  const uploads = await sendMedia(files)
  if (!uploads) return
  // A song some question already plays is not added again.
  const known = uploads.filter((u) => usedBy(u.media))
  let first = ''
  update((m) => {
    m.tracks ??= []
    const ids: string[] = []
    for (const u of uploads) {
      if (IMAGE.test(u.media) || known.includes(u)) continue
      const id = newTrackId(m.tracks)
      m.tracks.push({ id, media: u.media, guesses: u.guesses })
      ids.push(id)
    }
    place(m, ids)
    first = ids[0] ?? ''
  })
  if (first) select(first)
  if (known.length > 0) {
    status.value = `Déjà dans le pack, non ajouté(s) : ${known.map((u) => `« ${questionName(usedBy(u.media))} »`).join(', ')}.`
  }
}

// uploadInto puts a dropped file in a question, as its media or its outro file. A question without
// answers yet takes the ones the tags suggest.
async function uploadInto(t: Data, field: 'media' | 'outroMedia', files: File[]) {
  const uploads = await sendMedia(files.slice(0, 1))
  const u = uploads?.[0]
  if (!u || (field === 'media' && !mayReuse(t, u.media))) return
  update(() => {
    t[field] = u.media
    const blank = (t.guesses ?? []).every((g: Data) => !(g.answers ?? []).length)
    if (field === 'media' && blank && u.guesses.length > 0) t.guesses = u.guesses
  })
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

// RoundPanel sets a round up; its questions are listed and added on the left.
function RoundPanel({ i }: { i: number }) {
  const m = manifest.value
  const r: Data = m.rounds[i]
  const moveRound = (d: number) => {
    update((m) => {
      m.rounds.splice(i, 1)
      m.rounds.splice(i + d, 0, r)
    })
    step.value = `round:${i + d}`
  }
  const others = (m.tracks ?? []).filter((t: Data) => !r.tracks?.includes(t.id))
  const pool = roundPool(r)
  return (
    <>
      <h2>
        {r.name || `Manche ${i + 1}`}{' '}
        <button disabled={i === 0} onClick={() => moveRound(-1)} title="Monter">
          ↑
        </button>
        <button disabled={i === m.rounds.length - 1} onClick={() => moveRound(1)} title="Descendre">
          ↓
        </button>
        <button
          onClick={() => {
            if (!confirm(`Supprimer la manche ${i + 1} ? Ses questions restent dans le pack.`)) return
            update((m) => m.rounds.splice(i, 1))
            step.value = 'settings'
          }}
        >
          Supprimer
        </button>
      </h2>
      <Problems only={`round:${i}`} />
      <Text o={r} path={['name']} label="Nom" />
      <h3>Type de manche</h3>
      <TypeCards
        current={typeOf(r)}
        onPick={(t) => (typeOf(r) || !r.rules || confirm('Remplacer les réglages avancés de cette manche ?')) && update(() => applyType(r, t))}
      />
      {!typeOf(r) && (
        <p>
          <small>Manche personnalisée : ses réglages avancés diffèrent de tous les types.</small>
        </p>
      )}
      <Summary r={r} />
      <RoundPreview r={r} pool={pool} />
      {r.selection !== 'sequence' && (
        <p>
          <small>Les questions se créent dans les thèmes, à gauche.</small>
        </p>
      )}
      {r.selection === 'sequence' ? (
        others.length > 0 && (
          <label>
            Reprendre une question{' '}
            <select value="" onChange={(e) => update(() => (r.tracks = [...(r.tracks ?? []), value(e)]))}>
              <option value="">—</option>
              {others.map((t: Data) => (
                <option key={t.id} value={t.id}>
                  {questionName(t)}
                </option>
              ))}
            </select>
          </label>
        )
      ) : (
        <>
          <Ids o={r} path={['themes']} label="Thèmes" options={themeOptions()} />
          <NewTheme round={i} />
          <Num o={r} path={['count']} label={r.selection === 'theme-pick' ? 'Nombre de choix de thème' : 'Nombre de questions tirées'} int />
        </>
      )}
      <Num o={r} path={['eliminate']} label="Éliminés en fin de manche" int />
      <details>
        <summary>Réglages avancés (vide : règles par défaut du pack)</summary>
        <label>
          Questions{' '}
          <select value={r.selection} onChange={(e) => update(() => (r.selection = value(e)))}>
            <option value="sequence">liste fixe, dans l'ordre</option>
            <option value="random">tirées au hasard dans des thèmes</option>
            <option value="theme-pick">thème choisi par les joueurs</option>
          </select>
        </label>
        {r.selection === 'theme-pick' && (
          <>
            <Pick o={r} path={['picker']} label="Qui choisit" options={[['rotation', 'chacun son tour']]} />
            <Pick
              o={r}
              path={['pickScope']}
              label="Chaque choix joue"
              options={[
                ['track', 'une question du thème'],
                ['theme', "tout le thème, dans l'ordre"],
              ]}
            />
          </>
        )}
        <Rules o={r} />
      </details>
    </>
  )
}

// roundPool lists the questions a random or theme-pick round draws from: those of its themes.
function roundPool(r: Data): string[] {
  return (manifest.value.tracks ?? []).filter((t: Data) => t.themes?.some((th: string) => r.themes?.includes(th))).map((t: Data) => t.id)
}

function ExportPanel() {
  const m = manifest.value
  const rounds: Data[] = m.rounds ?? []
  const used = new Set(rounds.flatMap((r) => (r.selection === 'sequence' ? (r.tracks ?? []) : [])))
  const unused = rounds.some((r) => r.selection !== 'sequence') ? [] : (m.tracks ?? []).filter((t: Data) => !used.has(t.id))
  return (
    <>
      <h2>Export</h2>
      <p>
        {rounds.length || 'Aucune'} manche(s), {(m.tracks ?? []).length} question(s), {media.value.length} fichier(s).
      </p>
      {rounds.length > 0 && unused.length > 0 && (
        <p class="warning">Questions dans aucune manche, donc jamais jouées : {unused.map(questionName).join(', ')}.</p>
      )}
      {problems.value.length > 0 ? <Problems /> : <p>Le pack est jouable.</p>}
      <p>
        <small>ffmpeg, s'il est installé, découpe chaque média aux extraits joués pour alléger le fichier.</small>
      </p>
      <button class="primary" disabled={problems.value.length > 0} onClick={exportPack}>
        Exporter en .linospack
      </button>
      {exported.value && (
        <>
          <button onClick={() => reveal(true)}>Afficher le .linospack</button>{' '}
          <a href={`/api/edit/archives/${encodeURIComponent(current.value)}.linospack`} download>
            Télécharger
          </a>
        </>
      )}
      <h3>Tester</h3>
      <p>
        <small>Charge ce pack dans le pilotage ; il faut au moins un joueur connecté pour lancer la partie.</small>
      </p>
      <button disabled={problems.value.length > 0} onClick={play}>
        Jouer ce pack
      </button>
      <h3>Dossier</h3>
      <button onClick={() => reveal()}>Ouvrir le dossier du pack</button>
    </>
  )
}

// play saves, then opens the control page with this pack loaded.
async function play() {
  await save()
  window.open(`/control?pack=${encodeURIComponent(current.value)}`, 'linos-control')
}

function SettingsPanel() {
  return (
    <>
      <PackStep />
      <GameStep />
    </>
  )
}

// Validator messages (see internal/pack/validate.go), in the page's language.
const messages: [RegExp, string][] = [
  [/title is required/, 'titre manquant'],
  [/at least one track is required/, 'aucune question'],
  [/media "" is not a valid relative path/, 'aucun fichier choisi'],
  [/is not a supported media file/, 'fichier non pris en charge'],
  [/an outroMedia needs an outro duration/, 'révélation : durée manquante'],
  [/\.id is required/, 'identifiant manquant'],
  [/\.id "(.*)" is duplicated/, 'identifiant « $1 » en double'],
  [/image "(.*)" not found/, 'photo « $1 » introuvable'],
  [/cover "(.*)" not found/, 'couverture « $1 » introuvable'],
  [/"(.*)" not found/, 'média « $1 » introuvable'],
  [/at least one guess is required/, 'rien à deviner'],
  [/label is required/, 'élément à deviner sans nom'],
  [/label "(.*)" is duplicated/, 'élément « $1 » en double'],
  [/a text guess needs answers/, 'aucune réponse acceptée'],
  [/answer must be one of choices/, 'bonne réponse du QCM absente des choix'],
  [/answer must be a number/, 'réponse numérique manquante'],
  [/a sequence round needs tracks/, 'aucune question dans la manche'],
  [/round needs themes/, 'aucun thème choisi'],
  [/count must be positive/, 'nombre de questions tirées ou de choix de thème manquant'],
  [/unknown id "(.*)"/, 'référence inconnue « $1 »'],
  [/cannot be negative/, 'valeur négative'],
  [/grayscale must be between 0 and 1/, 'gris hors de 0 à 1'],
  [/fuzziness must be between 0 and 1/, 'tolérance aux fautes hors de 0 à 1'],
  [/attempts must be at least 1/, 'essais : au moins 1'],
  [/control\.default/, 'contrôle par défaut absent des contrôles possibles'],
  [/minTeams exceeds maxTeams/, 'équipes : min supérieur à max'],
  [/end\.target must be positive/, 'score cible manquant'],
  [/min exceeds max/, 'points : min supérieur à max'],
  [/othersMultiplier must be at least 1/, 'multiplicateur : au moins 1'],
  [/partial|pickScope/, 'valeur inconnue'],
]

// problemPlace finds where a validator problem is fixed ("q:<id>", "round:<i>" or "settings") and what it concerns.
function problemPlace(p: string): { step: string; where: string; text: string } {
  const m = manifest.value
  const found = messages.find(([re]) => re.test(p))
  const text = found ? found[1].replace('$1', p.match(found[0])?.[1] ?? '') : p
  const round = /^rounds\[(\d+)\]/.exec(p)
  if (round) return { step: `round:${round[1]}`, where: m.rounds?.[Number(round[1])]?.name || `Manche ${Number(round[1]) + 1}`, text }
  const track = /^tracks\[(\d+)\]/.exec(p)
  if (track) {
    const t = m.tracks?.[Number(track[1])]
    return { step: t ? `q:${t.id}` : 'settings', where: t ? questionName(t) : `Question ${Number(track[1]) + 1}`, text }
  }
  const theme = /^themes\[(\d+)\]/.exec(p)
  const th = theme && m.themes?.[Number(theme[1])]
  if (th) return { step: `theme:${th.id}`, where: th.name || th.id, text }
  return { step: 'settings', where: /^(game|rules)/.test(p) ? 'Partie' : 'Pack', text }
}

// Problems lists what keeps the pack from being played; only those of one place when given.
function Problems({ only }: { only?: string }) {
  const list = problems.value.map(problemPlace).filter((x) => !only || x.step === only)
  if (list.length === 0) return null
  return (
    <div class="warning">
      <p>{only ? 'À corriger ici :' : "À corriger avant l'export :"}</p>
      <ul>
        {list.map((x, i) => (
          <li key={i}>
            {only ? (
              x.text
            ) : (
              <>
                <button class="link" onClick={() => go(x.step)}>
                  {x.where}
                </button>{' '}
                : {x.text}
              </>
            )}
          </li>
        ))}
      </ul>
    </div>
  )
}

const hasProblem = (to: string) => problems.value.some((p) => problemPlace(p).step === to)

// moveTo moves a dragged question between the places listing questions: "round:<i>" (a sequence round),
// "theme:<id>" or "loose" (in no round or theme). It lands before a question of the place, else at its end;
// themes and loose questions follow the pack order.
function moveTo(id: string, from: string, to: string, before?: string) {
  update((m) => {
    const t = trackById(id)
    if (!t) return
    if (from !== to) {
      if (from.startsWith('round:')) {
        const r = m.rounds[Number(from.slice(6))]
        r.tracks = (r.tracks ?? []).filter((x: string) => x !== id)
      }
      if (from.startsWith('theme:')) t.themes = (t.themes ?? []).filter((x: string) => x !== from.slice(6))
      if (to.startsWith('theme:')) t.themes = [...(t.themes ?? []), to.slice(6)]
      if (to === 'loose') t.themes = []
    }
    if (to.startsWith('round:')) {
      const r = m.rounds[Number(to.slice(6))]
      const list: string[] = (r.tracks ?? []).filter((x: string) => x !== id)
      const at = before ? list.indexOf(before) : -1
      list.splice(at < 0 ? list.length : at, 0, id)
      r.tracks = list
    } else if (before) {
      m.tracks.splice(m.tracks.indexOf(t), 1)
      m.tracks.splice(m.tracks.findIndex((x: Data) => x.id === before), 0, t)
    }
  })
}

const DRAG = 'text/x-linos-question'

// dropOn makes an element take dragged questions into a place, before a question when given.
const dropOn = (place: string, before?: string) => ({
  onDragOver: (e: DragEvent) => e.dataTransfer?.types.includes(DRAG) && e.preventDefault(),
  onDrop: (e: DragEvent) => {
    const [from, id] = (e.dataTransfer?.getData(DRAG) ?? '').split('\n')
    if (!id) return
    e.preventDefault()
    e.stopPropagation()
    if (id !== before) moveTo(id, from, place, before)
  },
})

// QuestionList lists the questions of a place in play order; a question is dragged to reorder it, or
// to another round or theme.
function QuestionList({ ids, place }: { ids: string[]; place: string }) {
  return (
    <ol class="questions">
      {ids.map((id, i) => {
        const t = trackById(id)
        if (!t) return null
        return (
          <li
            key={id}
            class={step.value === `q:${id}` ? 'selected' : ''}
            draggable
            onDragStart={(e) => e.dataTransfer?.setData(DRAG, `${place}\n${id}`)}
            {...dropOn(place, id)}
          >
            <button class="link" onClick={() => select(id)}>
              {i + 1}. {questionName(t)}
            </button>
            {hasProblem(`q:${id}`) && <span class="warn-mark" title="À corriger"> !</span>}
          </li>
        )
      })}
    </ol>
  )
}

const selectionNames: Record<string, string> = { sequence: 'liste fixe', random: 'au hasard', 'theme-pick': 'thème au choix' }

// placeDropped puts the questions dropped in bulk where the user is: the open theme, the theme or round
// of the open question, the open round, else the last round.
function placeDropped(m: Data, ids: string[]) {
  const rounds: Data[] = m.rounds ?? []
  const s = step.value
  const q = s.startsWith('q:') ? trackById(s.slice(2)) : null
  const theme = s.startsWith('theme:') ? s.slice(6) : q?.themes?.[0]
  if (theme) {
    ids.forEach((id) => (trackById(id).themes = [theme]))
    return
  }
  let i = s.startsWith('round:') ? Number(s.slice(6)) : rounds.findIndex((r) => r.tracks?.includes(q?.id))
  if (i < 0) i = rounds.length - 1
  const r = rounds[i]
  if (r?.selection === 'sequence') r.tracks = [...(r.tracks ?? []), ...ids]
  else if (r?.themes?.length) ids.forEach((id) => (trackById(id).themes = [r.themes[0]]))
}

// ThemeBlock lists a theme and its questions in the outline.
function ThemeBlock({ id }: { id: string }) {
  return (
    <div class="theme" {...dropOn(`theme:${id}`)}>
      <h4>
        <button class={step.value === `theme:${id}` ? 'link selected' : 'link'} onClick={() => (step.value = `theme:${id}`)}>
          {themeName(id)}
        </button>
        {hasProblem(`theme:${id}`) && <span class="warn-mark" title="À corriger"> !</span>}
      </h4>
      <QuestionList ids={themeQuestions(id)} place={`theme:${id}`} />
      <button onClick={() => addQuestion(undefined, id)}>+ question</button>
    </div>
  )
}

// Outline is the left column: the rounds in play order, each theme round with its themes, then the
// themes no round draws.
function Outline() {
  const m = manifest.value
  const rounds: Data[] = m.rounds ?? []
  const tracks: Data[] = m.tracks ?? []
  const themes: Data[] = m.themes ?? []
  const placed = new Set<string>()
  tracks.forEach((t) => t.themes?.length && placed.add(t.id))
  const themeRound = rounds.some((r) => r.selection !== 'sequence')
  const drawn = new Set(rounds.flatMap((r) => (r.selection !== 'sequence' && r.themes) || []))
  const unused = themes.filter((th) => !drawn.has(th.id))
  return (
    <nav class="outline">
      {rounds.length === 0 && (
        <section {...dropOn('loose')}>
          <h3>Questions</h3>
          <QuestionList ids={tracks.filter((t) => !t.themes?.length).map((t) => t.id)} place="loose" />
          <button onClick={() => addQuestion()}>+ question</button>
        </section>
      )}
      {rounds.map((r, i) => {
        const sequence = r.selection === 'sequence'
        if (sequence) (r.tracks ?? []).forEach((id: string) => placed.add(id))
        return (
          <section key={i} {...(sequence ? dropOn(`round:${i}`) : {})}>
            <h3>
              <button class={step.value === `round:${i}` ? 'link selected' : 'link'} onClick={() => (step.value = `round:${i}`)}>
                {r.name || `Manche ${i + 1}`}
              </button>
              {hasProblem(`round:${i}`) && <span class="warn-mark" title="À corriger"> !</span>}{' '}
              <small class="inline">{selectionNames[r.selection] ?? r.selection}</small>
            </h3>
            {sequence ? (
              <>
                <QuestionList ids={r.tracks ?? []} place={`round:${i}`} />
                <button onClick={() => addQuestion(i)}>+ question</button>
              </>
            ) : (
              <>
                {(r.themes ?? []).map((id: string) => (
                  <ThemeBlock key={id} id={id} />
                ))}
                <NewTheme round={i} />
              </>
            )}
          </section>
        )
      })}
      <button class={step.value === 'new-round' ? 'selected' : ''} onClick={() => (step.value = 'new-round')}>
        + manche
      </button>
      {(!themeRound || unused.length > 0) && (
        <section>
          <h3>{themeRound ? 'Thèmes hors manche' : 'Thèmes'}</h3>
          {themeRound && (
            <p>
              <small>Aucune manche ne les tire : cochez-les dans une manche à thèmes.</small>
            </p>
          )}
          {unused.map((th) => (
            <ThemeBlock key={th.id} id={th.id} />
          ))}
          {!themeRound && <NewTheme />}
        </section>
      )}
      {rounds.length > 0 && tracks.some((t) => !placed.has(t.id)) && (
        <section {...dropOn('loose')}>
          <h3>Hors manche</h3>
          <p>
            <small>Jamais jouées : ajoutez-les à une manche ou à un thème, ou supprimez-les.</small>
          </p>
          <QuestionList ids={tracks.filter((t) => !placed.has(t.id)).map((t) => t.id)} place="loose" />
        </section>
      )}
      <Dropzone label="Déposer plusieurs fichiers : une question par fichier" onFiles={(f) => upload(f, placeDropped)} />
    </nav>
  )
}

// MediaChoice picks the file of a question, or of its reveal: from the pack's, or dropped.
function MediaChoice({ file, onPick, onFiles }: { file?: string; onPick: (name: string) => void; onFiles: (files: File[]) => void }) {
  const files = media.value.filter((x) => x !== file)
  return (
    <div class="media-choice">
      {file && <strong>{file}</strong>}{' '}
      {files.length > 0 && (
        <select value="" onChange={(e) => value(e) && onPick(value(e))}>
          <option value="">{file ? 'changer pour un fichier du pack…' : 'choisir un fichier du pack…'}</option>
          {files.map((x) => (
            <option key={x} value={x}>
              {x}
            </option>
          ))}
        </select>
      )}
      <Dropzone label={file ? 'ou déposer un autre fichier' : 'Déposer le fichier ici, ou cliquer pour le choisir'} onFiles={onFiles} />
    </div>
  )
}

// QuestionPanel edits one question in the order it is thought: what is heard, what is revealed,
// what must be found; the rest is folded away.
function QuestionPanel({ t }: { t: Data }) {
  const m = manifest.value
  const defaultDuration = m.rules?.duration ?? DEFAULT_DURATION
  const start = t.start ?? 0
  const rate = t.playbackRate ?? 1
  const duration = t.duration ?? defaultDuration
  const playable = t.media && !IMAGE.test(t.media)
  // Reveal: another file once one is chosen or being chosen, else the rest of the media, else nothing.
  const reveal = t.outroMedia !== undefined ? 'file' : (t.outro ?? 0) > 0 ? 'rest' : 'none'
  const { list, at } = questionList(m, t.id)
  return (
    <div class="question">
      <h2>
        Question {at + 1} · {questionName(t)}{' '}
        <button disabled={at <= 0} onClick={() => moveQuestion(t.id, at - 1)} title="Monter">
          ↑
        </button>
        <button disabled={at >= list.length - 1} onClick={() => moveQuestion(t.id, at + 1)} title="Descendre">
          ↓
        </button>
        <button onClick={() => removeQuestion(t.id)}>Supprimer</button>
      </h2>
      <Problems only={`q:${t.id}`} />

      <h3>1. Ce que les joueurs entendent</h3>
      <MediaChoice file={t.media || undefined} onPick={(name) => mayReuse(t, name) && put(t, ['media'], name)} onFiles={(f) => uploadInto(t, 'media', f)} />
      {playable && (
        <Waveform
          src={mediaUrl(t.media)}
          start={start}
          end={start + duration * rate}
          onChange={(s, e) =>
            update(() => {
              t.start = s
              t.duration = Math.round(((e - s) / rate) * 10) / 10
            })
          }
        />
      )}
      {t.media && IMAGE.test(t.media) && <Num o={t} path={['duration']} label="Durée (s)" placeholder={String(defaultDuration)} />}

      <h3>2. La révélation</h3>
      <p>
        <small>Ce qu'on entend une fois la réponse dévoilée, en fondu de sortie.</small>
      </p>
      <div class="choices">
        <label>
          <input
            type="radio"
            checked={reveal === 'rest'}
            onChange={() =>
              update(() => {
                delete t.outroMedia
                delete t.outroStart
                t.outro ||= 10
              })
            }
          />{' '}
          la suite du morceau
        </label>
        <label>
          <input
            type="radio"
            checked={reveal === 'file'}
            onChange={() =>
              update(() => {
                t.outroMedia = ''
                t.outro ||= 15
              })
            }
          />{' '}
          un autre fichier (le morceau entier après sa basse seule…)
        </label>
        <label>
          <input
            type="radio"
            checked={reveal === 'none'}
            onChange={() =>
              update(() => {
                delete t.outroMedia
                delete t.outroStart
                delete t.outro
              })
            }
          />{' '}
          rien, la musique s'arrête
        </label>
      </div>
      {reveal === 'rest' && <Num o={t} path={['outro']} label="Durée de la suite (s)" />}
      {reveal === 'file' && (
        <>
          <MediaChoice file={t.outroMedia || undefined} onPick={(name) => put(t, ['outroMedia'], name)} onFiles={(f) => uploadInto(t, 'outroMedia', f)} />
          {t.outroMedia && !IMAGE.test(t.outroMedia) ? (
            <Waveform
              src={mediaUrl(t.outroMedia)}
              start={t.outroStart ?? 0}
              end={(t.outroStart ?? 0) + (t.outro ?? 15)}
              onChange={(s, e) =>
                update(() => {
                  t.outroStart = s
                  t.outro = Math.round((e - s) * 10) / 10
                })
              }
            />
          ) : (
            <p>
              <small>Choisissez le fichier joué à la révélation.</small>
            </p>
          )}
        </>
      )}

      <h3>3. Ce qu'il faut trouver</h3>
      <Guesses track={t} />
      {themeOptions().length > 0 && <Ids o={t} path={['themes']} label="Thèmes de la question" options={themeOptions()} />}

      <details open={playlist.value.length > 0}>
        <summary>Écouter comme les joueurs · effets et indices</summary>
        {preview.value?.id === t.id && t.media && <Preview />}
      </details>
      <details>
        <summary>Plus : vitesse, identifiant</summary>
        <Num o={t} path={['playbackRate']} label="Vitesse" placeholder="1" />
        <label>
          Identifiant{' '}
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
      </details>
    </div>
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
      {archives.value.length > 0 && (
        <>
          <h2>Modifier une archive</h2>
          <p>
            <small>Une archive .linospack se modifie par une copie en dossier, à réexporter ensuite.</small>
          </p>
          <ul class="packs">
            {archives.value.map((a) => (
              <li key={a}>
                <button class="link" onClick={() => unpack(a)}>
                  ▸ {a}
                </button>{' '}
                <a href={`/api/edit/archives/${encodeURIComponent(a)}`} download>
                  télécharger
                </a>
              </li>
            ))}
          </ul>
        </>
      )}
      <h2>Nouveau pack</h2>
      <Dropzone label="Déposer un dossier ou des musiques : un pack est créé, artiste et titre lus dans les tags (sinon le nom « Artiste - Titre »). Un .linospack reçu s'ajoute aux packs." onFiles={dropFiles} />
      <p>
        <label>
          Début des extraits (s) <input type="number" min={0} value={importStart.value} onInput={(e) => (importStart.value = Number(value(e)))} />
        </label>{' '}
        <label>
          Durée (s) <input type="number" min={1} value={importDuration.value} onInput={(e) => (importDuration.value = Number(value(e)))} />
        </label>
      </p>
      <p>
        <small>Ou un pack vide, à remplir piste par piste :</small>
      </p>
      <form
        onSubmit={(e) => {
          e.preventDefault()
          create()
        }}
      >
        <input value={newName.value} placeholder="Nom du pack" onInput={(e) => (newName.value = value(e))} />{' '}
        <button disabled={!newName.value.trim()}>Créer</button>
      </form>
    </>
  )
}

async function close() {
  await save()
  manifest.value = null
  current.value = ''
  status.value = ''
  listPacks()
}

export default function Edit() {
  const m = manifest.value
  if (!m) {
    return (
      <main class="edit">
        <Landing />
        {status.value && <p>{status.value}</p>}
      </main>
    )
  }
  const s = step.value
  const q = s.startsWith('q:') ? trackById(s.slice(2)) : null
  const round = s.startsWith('round:') ? Number(s.slice(6)) : -1
  const theme = s.startsWith('theme:') && (m.themes ?? []).some((t: Data) => t.id === s.slice(6)) ? s.slice(6) : ''
  return (
    <main class="edit editor">
      <header class="topbar">
        <button class="link" onClick={close}>
          ‹ Packs
        </button>
        <input class="title" aria-label="Titre du pack" value={m.title ?? ''} onChange={(e) => put(m, ['title'], value(e).trim())} />
        <span class="spacer" />
        {status.value && <small class="inline">{status.value}</small>}
        {problems.value.length > 0 && (
          <button class="link" onClick={() => (step.value = 'export')}>
            {problems.value.length} à corriger
          </button>
        )}
        <button class={s === 'settings' ? 'selected' : ''} onClick={() => (step.value = 'settings')}>
          Réglages
        </button>
        <button disabled={problems.value.length > 0} onClick={play}>
          ▶ Jouer
        </button>
        <button class={s === 'export' ? 'primary selected' : 'primary'} onClick={() => (step.value = 'export')}>
          Exporter
        </button>
      </header>
      <div class="workspace">
        <Outline />
        <section class="panel">
          {q && <QuestionPanel key={q.id} t={q} />}
          {round >= 0 && m.rounds?.[round] && <RoundPanel key={s} i={round} />}
          {theme && <ThemePanel key={s} id={theme} />}
          {s === 'settings' && <SettingsPanel />}
          {s === 'export' && <ExportPanel />}
          {s === 'new-round' && <NewRoundPanel />}
          {s === 'wizard' && <Wizard />}
          {!q && round < 0 && !theme && !['settings', 'export', 'new-round', 'wizard'].includes(s) && <p>Choisissez une question à gauche, ou ajoutez-en une.</p>}
        </section>
      </div>
    </main>
  )
}
