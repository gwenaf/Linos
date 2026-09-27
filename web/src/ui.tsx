import type { RefObject } from 'preact'
import { useEffect, useRef } from 'preact/hooks'
import type { Data } from './ws'

export function Qr({ url, caption }: { url: string; caption: string }) {
  return (
    <figure class="qr">
      <img src={`/qr.png?data=${encodeURIComponent(url)}`} alt={caption} />
      <figcaption>
        {caption}
        <br />
        <code>{url}</code>
      </figcaption>
    </figure>
  )
}

/** Display label of a team or solo player payload ({team} or {name}). */
export function unitName(d: Data): string {
  return d.team ?? d.name
}

/** Keeps the latest score of each team or player; key is the display label. */
export function scoreKey(d: Data): string {
  return d.team ?? d.name
}

/** Builds the score table from the results list of a state snapshot. */
export function scoreTable(results: Data[]): Record<string, number> {
  return Object.fromEntries(results.map((r) => [scoreKey(r), r.score]))
}

export function Scores({ scores }: { scores: Record<string, number> }) {
  const rows = Object.entries(scores).sort((a, b) => b[1] - a[1])
  if (rows.length === 0) return null
  return (
    <ol class="scores">
      {rows.map(([name, score]) => (
        <li key={name}>
          <span>{name}</span>
          <strong>{score}</strong>
        </li>
      ))}
    </ol>
  )
}

export function Results({ data }: { data: Data }) {
  return (
    <section class="results">
      <h2>{data.name ? `Fin de la manche ${data.name}` : data.reason === 'aborted' ? 'Partie annulée' : 'Résultats'}</h2>
      <ol>
        {data.results.map((r: Data) => (
          <li key={r.team ?? r.name}>
            <span>{r.team ?? r.name}</span>
            <strong>{r.score}</strong>
          </li>
        ))}
      </ol>
    </section>
  )
}

export function Answers({ guesses }: { guesses: Data[] }) {
  return (
    <ul class="answers">
      {guesses.map((g) => (
        <li key={g.label}>
          <strong>{g.label}</strong> : {g.type === 'text' ? g.answers.join(' / ') : String(g.answer)}
        </li>
      ))}
    </ul>
  )
}

// Dropped folders arrive as entries: walk them to collect every file inside.
export async function droppedFiles(entries: FileSystemEntry[]): Promise<File[]> {
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

export const IMAGE = /\.(jpe?g|png|webp|gif|avif)$/i
export const VIDEO = /\.(mp4|m4v|webm)$/i

export type Effects = { audio: boolean; video: boolean; blur: number; pixelate: number; grayscale: number; image: string }
const numeric = ['blur', 'pixelate', 'grayscale'] as const

// effectsAt applies the reveal steps at t seconds of play: switches take the last step reached,
// numeric effects move linearly from the last step setting them to the next one.
export function effectsAt(steps: Data[] | null | undefined, t: number): Effects {
  const at = (s: Data) => s.at ?? 0
  const sorted = [...(steps ?? [])].sort((a, b) => at(a) - at(b))
  const e: Effects = { audio: true, video: true, blur: 0, pixelate: 0, grayscale: 0, image: '' }
  for (const s of sorted.filter((s) => at(s) <= t)) {
    if (s.audio != null) e.audio = s.audio
    if (s.video != null) e.video = s.video
    if (s.image != null) e.image = s.image
  }
  for (const k of numeric) {
    const set = sorted.filter((s) => s[k] != null)
    const prev = set.filter((s) => at(s) <= t).at(-1)
    const next = set.find((s) => at(s) > t)
    if (prev) e[k] = next ? prev[k] + ((next[k] - prev[k]) * (t - at(prev))) / (at(next) - at(prev)) : prev[k]
  }
  return e
}

// finalEffects unveils the media once the track ends; an audio track keeps its picture.
export function finalEffects(steps: Data[] | null | undefined, media: string): Effects {
  const image = VIDEO.test(media) ? '' : effectsAt(steps, Infinity).image
  return { audio: true, video: true, blur: 0, pixelate: 0, grayscale: 0, image }
}

// Stage shows a track's media with its effects. Pixelation draws the picture shrunk on a canvas,
// scaled back up without smoothing.
export function Stage(props: {
  src: string
  effects: Effects
  imageUrl: (name: string) => string
  mediaRef?: RefObject<HTMLVideoElement>
  onStarted?: () => void
}) {
  const { src, effects: e, imageUrl, onStarted } = props
  const ownRef = useRef<HTMLVideoElement>(null)
  const video = props.mediaRef ?? ownRef
  const picture = useRef<HTMLImageElement>(null)
  const canvas = useRef<HTMLCanvasElement>(null)
  const pixelate = useRef(e.pixelate)
  pixelate.current = e.pixelate
  const isImage = IMAGE.test(src)
  const showsPicture = isImage || !!e.image
  const pixelated = e.pixelate > 1

  useEffect(() => {
    if (!pixelated) return
    let frame = 0
    const draw = () => {
      const c = canvas.current
      const img = picture.current
      const v = video.current
      const [source, w, h] = showsPicture && img ? [img, img.naturalWidth, img.naturalHeight] : v ? [v, v.videoWidth, v.videoHeight] : [null, 0, 0]
      if (c && source && w > 0) {
        c.width = Math.max(1, Math.round(w / pixelate.current))
        c.height = Math.max(1, Math.round(h / pixelate.current))
        c.getContext('2d')!.drawImage(source, 0, 0, c.width, c.height)
      }
      frame = requestAnimationFrame(draw)
    }
    draw()
    return () => cancelAnimationFrame(frame)
  }, [pixelated, showsPicture, src])

  const style = { filter: `blur(${e.blur}px) grayscale(${e.grayscale})`, visibility: e.video ? 'visible' : 'hidden' }
  const hidden = { display: 'none' }
  return (
    <div class="stage">
      {showsPicture && <img ref={picture} src={e.image ? imageUrl(e.image) : src} style={pixelated ? hidden : style} onLoad={isImage ? onStarted : undefined} />}
      {!isImage && <video ref={video} src={src} muted={!e.audio} style={showsPicture || pixelated ? hidden : style} onPlaying={onStarted} />}
      {pixelated && <canvas ref={canvas} style={style} />}
    </div>
  )
}

// whenLoaded runs fn once the media knows its duration; a cached media may know it before the page asks.
export function whenLoaded(v: HTMLMediaElement, fn: () => void) {
  if (v.readyState >= HTMLMediaElement.HAVE_METADATA) fn()
  else v.onloadedmetadata = fn
}

// outroVolume is the media volume `after` seconds past the track end: full, then fading out
// over the last 2 seconds of the outro; 0 means the media stops.
export function outroVolume(outro: number, after: number): number {
  const left = outro - after
  return left <= 0 ? 0 : Math.min(1, left / Math.min(2, outro))
}

const BANNER = String.raw` _     ___ _   _  ___  ____
| |   |_ _| \ | |/ _ \/ ___|
| |    | ||  \| | | | \___ \
| |___ | || |\  | |_| |___) |
|_____|___|_| \_|\___/|____/`

// Banner is the Linos title, drawn in ASCII: the app's only decoration.
export function Banner() {
  return (
    <pre class="ascii" aria-label="Linos">
      {BANNER}
    </pre>
  )
}
