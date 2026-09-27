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
