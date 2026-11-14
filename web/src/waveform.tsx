import { useEffect, useRef, useState } from 'preact/hooks'

// Peaks are computed by the browser, never by Go: the file is decoded at 8 kHz mono-ish (one channel
// kept) to stay light in memory, then reduced to COLUMNS maxima. Cached per file for the page's life.
const COLUMNS = 1000
type Peaks = { duration: number; peaks: Float32Array }
const cache = new Map<string, Promise<Peaks>>()

function peaksOf(src: string): Promise<Peaks> {
  let p = cache.get(src)
  if (!p) {
    p = fetch(src)
      .then((r) => r.arrayBuffer())
      .then((buf) => new OfflineAudioContext(1, 1, 8000).decodeAudioData(buf))
      .then((audio) => {
        const data = audio.getChannelData(0)
        const peaks = new Float32Array(COLUMNS)
        const per = Math.max(1, Math.floor(data.length / COLUMNS))
        for (let c = 0; c < COLUMNS; c++) {
          let peak = 0
          for (let i = c * per; i < Math.min(data.length, (c + 1) * per); i++) peak = Math.max(peak, Math.abs(data[i]))
          peaks[c] = peak
        }
        return { duration: audio.duration, peaks }
      })
    p.catch(() => cache.delete(src)) // a failed decode may succeed later, e.g. once the file is complete
    cache.set(src, p)
  }
  return p
}

const tenth = (x: number) => Math.round(x * 10) / 10
const clock = (s: number) => `${Math.floor(s / 60)}:${(s % 60).toFixed(1).padStart(4, '0')}`
// A handle is grabbed within this many CSS pixels.
const GRAB = 10

// Waveform shows a whole media file and the part played, [start, end]: its handles are dragged, a click
// moves the play head, space plays or pauses. "Écouter la sélection" plays just the selection.
export function Waveform(props: { src: string; start: number; end: number; onChange: (start: number, end: number) => void }) {
  const { src, start, end, onChange } = props
  const canvas = useRef<HTMLCanvasElement>(null)
  const audio = useRef<HTMLAudioElement>(null)
  const [data, setData] = useState<Peaks | null>(null)
  const [failed, setFailed] = useState(false)
  const [head, setHead] = useState(start)
  const [playing, setPlaying] = useState(false)
  // Selection playback stops at the end handle; free playback runs on.
  const stopAt = useRef<number | null>(null)
  const drag = useRef<'start' | 'end' | null>(null)
  // The decoded length when known, else the player's, so the handles work even without peaks.
  const [fileDuration, setFileDuration] = useState(0)
  const duration = data?.duration ?? fileDuration

  useEffect(() => {
    setData(null)
    setFailed(false)
    peaksOf(src).then(setData, () => setFailed(true))
  }, [src])

  // Play head follows the audio while it plays.
  useEffect(() => {
    if (!playing) return
    let frame = 0
    const tick = () => {
      const a = audio.current
      if (!a) return
      if (stopAt.current !== null && a.currentTime >= stopAt.current) {
        a.pause()
        a.currentTime = stopAt.current
      }
      setHead(a.currentTime)
      if (a.paused) setPlaying(false)
      else frame = requestAnimationFrame(tick)
    }
    tick()
    return () => cancelAnimationFrame(frame)
  }, [playing])

  // Stop when the file changes or the editor closes.
  useEffect(() => () => audio.current?.pause(), [src])

  useEffect(() => {
    const c = canvas.current
    if (!c) return
    const dpr = devicePixelRatio || 1
    const w = c.clientWidth * dpr
    const h = c.clientHeight * dpr
    c.width = w
    c.height = h
    const g = c.getContext('2d')!
    const css = getComputedStyle(c)
    const text = css.getPropertyValue('--text') || '#e8e8e8'
    const muted = css.getPropertyValue('--muted') || '#8c8c8c'
    g.clearRect(0, 0, w, h)
    if (duration <= 0) return
    const x = (t: number) => (t / duration) * w
    g.globalAlpha = 0.15
    g.fillStyle = text
    g.fillRect(x(start), 0, x(end) - x(start), h)
    g.globalAlpha = 1
    if (data) {
      for (let px = 0; px < w; px++) {
        const t = (px / w) * duration
        const peak = data.peaks[Math.min(COLUMNS - 1, Math.floor((px / w) * COLUMNS))]
        g.fillStyle = t >= start && t <= end ? text : muted
        const bar = Math.max(1, peak * h * 0.9)
        g.fillRect(px, (h - bar) / 2, 1, bar)
      }
    }
    g.fillStyle = text
    for (const t of [start, end]) g.fillRect(x(t) - dpr, 0, 2 * dpr, h)
    g.fillStyle = css.getPropertyValue('--warn') || '#d9b36c'
    g.fillRect(x(head) - dpr / 2, 0, dpr, h)
  }, [data, duration, start, end, head])

  const timeAt = (e: PointerEvent) => {
    const r = canvas.current!.getBoundingClientRect()
    return Math.max(0, Math.min(duration, ((e.clientX - r.left) / r.width) * duration))
  }
  const near = (e: PointerEvent, t: number) => {
    const r = canvas.current!.getBoundingClientRect()
    return Math.abs(e.clientX - r.left - (t / duration) * r.width) <= GRAB
  }
  const seek = (t: number) => {
    if (audio.current) audio.current.currentTime = t
    setHead(t)
  }
  const play = (from: number, until: number | null) => {
    const a = audio.current
    if (!a) return
    stopAt.current = until
    a.currentTime = from
    a.play().then(() => setPlaying(true), () => {})
  }
  const toggle = () => {
    const a = audio.current
    if (!a) return
    if (!a.paused) a.pause()
    else play(head, null)
  }
  const set = (s: number, e: number) => onChange(tenth(Math.max(0, Math.min(s, e))), tenth(Math.min(duration || e, Math.max(s, e))))

  return (
    <div
      class="waveform"
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.key !== ' ' || (e.target as HTMLElement).tagName === 'INPUT') return
        e.preventDefault()
        toggle()
      }}
    >
      <audio ref={audio} src={src} preload="metadata" onLoadedMetadata={(e) => setFileDuration((e.target as HTMLAudioElement).duration || 0)} />
      <canvas
        ref={canvas}
        onPointerDown={(e) => {
          if (duration <= 0) return
          ;(e.target as HTMLElement).setPointerCapture(e.pointerId)
          drag.current = near(e, end) ? 'end' : near(e, start) ? 'start' : null
          if (!drag.current) seek(timeAt(e))
        }}
        onPointerMove={(e) => {
          if (!drag.current) {
            canvas.current!.style.cursor = duration > 0 && (near(e, start) || near(e, end)) ? 'ew-resize' : 'pointer'
            return
          }
          const t = timeAt(e)
          if (drag.current === 'start') set(Math.min(t, end - 0.1), end)
          else set(start, Math.max(t, start + 0.1))
        }}
        onPointerUp={() => (drag.current = null)}
      />
      {failed && <small>Forme d'onde indisponible pour ce fichier ; l'écoute et les boutons restent utilisables.</small>}
      {!data && !failed && <small>Calcul de la forme d'onde…</small>}
      <div class="waveform-controls">
        <button onClick={toggle}>{playing ? '❚❚ Pause' : '▶ Lecture'}</button>
        <button onClick={() => (playing && stopAt.current !== null ? audio.current?.pause() : play(start, end))}>
          {playing && stopAt.current !== null ? '■ Arrêter' : '▶ Écouter la sélection'}
        </button>
        <span class="inline">{clock(head)}</span>
        <button onClick={() => set(head, end > head ? end : head + (end - start))}>Début ici</button>
        <button onClick={() => head > start && set(start, head)}>Fin ici</button>
        <label>
          de{' '}
          <input type="number" min={0} step={0.1} value={start} onChange={(e) => set(Number((e.target as HTMLInputElement).value), end)} />
        </label>
        <label>
          à <input type="number" min={0} step={0.1} value={end} onChange={(e) => set(start, Number((e.target as HTMLInputElement).value))} />
        </label>
        <small class="inline">({tenth(end - start)} s)</small>
      </div>
    </div>
  )
}
