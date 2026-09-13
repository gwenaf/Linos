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

/** Keeps the latest score of each team or player; key is the display label. */
export function scoreKey(d: Data): string {
  return d.team ?? d.name
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
      <h2>{data.reason === 'aborted' ? 'Partie annulée' : 'Résultats'}</h2>
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
