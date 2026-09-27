import { signal } from '@preact/signals'
import { droppedFiles } from './ui'
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
const IMAGE = /\.(jpe?g|png|webp|gif)$/i
const VIDEO = /\.(mp4|m4v|webm)$/i
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
  const res = await fetch(packUrl(), { method: 'PUT', body: JSON.stringify(manifest.value) })
  if (!res.ok) {
    status.value = `Échec de l'enregistrement : ${await res.text()}`
    return
  }
  problems.value = (await res.json()).problems
  dirty.value = false
  status.value = 'Enregistré.'
}

// update edits the manifest in place, then publishes a new reference so the page renders again.
function update(fn: (m: Data) => void) {
  fn(manifest.value)
  manifest.value = { ...manifest.value }
  dirty.value = true
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
            <th>À deviner</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {tracks.map((t, i) => (
            <tr key={i}>
              <td>
                <input value={t.id} onChange={(e) => update(() => (t.id = value(e)))} />
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
                <Guesses track={t} />
              </td>
              <td>
                <button onClick={() => (preview.value = t)}>Écouter</button>
                <button onClick={() => update((m) => m.tracks.splice(i, 1))}>Supprimer</button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  )
}

// Preview plays the extract as the game will: from start, for its duration, at its playback rate.
function Preview() {
  const t = preview.value
  if (!t) return null
  const start = t.start ?? 0
  const end = start + (t.duration ?? manifest.value.rules?.duration ?? DEFAULT_DURATION)
  const props = {
    key: `${t.media}-${start}-${end}`,
    src: mediaUrl(t.media),
    controls: true,
    autoPlay: true,
    onLoadedMetadata: (e: Event) => {
      const el = e.target as HTMLMediaElement
      el.currentTime = start
      el.playbackRate = t.playbackRate ?? 1
    },
    onTimeUpdate: (e: Event) => {
      const el = e.target as HTMLMediaElement
      if (el.currentTime >= end) el.pause()
    },
  }
  return (
    <section>
      <h2>
        Aperçu de {t.id} : {start} s à {end} s
      </h2>
      {VIDEO.test(t.media) ? <video {...props} /> : <audio {...props} />}
      <button onClick={() => (preview.value = null)}>Fermer</button>
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
          <section>
            <label>
              Titre <input value={m.title} onInput={(e) => update(() => (m.title = value(e)))} />
            </label>{' '}
            <button class="primary" disabled={!dirty.value} onClick={save}>
              Enregistrer
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
          <Dropzone />
          <Tracks />
          <Preview />
        </>
      )}
    </main>
  )
}
