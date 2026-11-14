import type { Data } from './ws'

// Round types offered when adding a round: each sets the round's selection and rules, which stay
// adjustable under "Réglages avancés".
export type RoundType = { name: string; text: string; round: Data }

export const roundTypes: RoundType[] = [
  {
    name: 'Classique',
    text: "Les extraits passent dans l'ordre. On buzze pour répondre : plus on est rapide, plus on marque.",
    round: { selection: 'sequence' },
  },
  {
    name: 'Tous en même temps',
    text: "Chacun tape sa réponse sur son téléphone pendant l'extrait. Les premiers à trouver marquent le plus.",
    round: { selection: 'sequence', rules: { answer: { mode: 'simultaneous', via: 'device' }, scoring: { type: 'rank' } } },
  },
  {
    name: 'Expert',
    text: 'Points fixes, une erreur coûte des points et personne ne reprend la main.',
    round: { selection: 'sequence', rules: { answer: { rebound: 'none' }, scoring: { type: 'fixed', max: 100, wrongPenalty: 50 } } },
  },
  {
    name: 'Au hasard',
    text: 'Des questions tirées au hasard dans les thèmes choisis.',
    round: { selection: 'random', count: 10 },
  },
  {
    name: 'Thème au choix',
    text: "Chacun son tour, on choisit un thème : une question de ce thème est jouée, avec 3 s d'avance pour qui l'a choisi.",
    round: { selection: 'theme-pick', count: 6, rules: { owner: { headStart: 3 } } },
  },
  {
    name: 'Thème entier',
    text: "Qui choisit le thème en joue toutes les questions avec 5 s d'avance. Un autre qui trouve marque double et lui prend ses points.",
    round: { selection: 'theme-pick', pickScope: 'theme', count: 4, rules: { owner: { headStart: 5, othersMultiplier: 2, stolenLosesPoints: true } } },
  },
]

const isObject = (v: unknown): v is Record<string, any> => !!v && typeof v === 'object' && !Array.isArray(v)
const empty = (v: unknown): boolean => v === undefined || (isObject(v) && Object.values(v).every(empty))

// same compares two manifest values, an empty object being the same as none.
function same(a: unknown, b: unknown): boolean {
  if (empty(a) && empty(b)) return true
  if (!isObject(a) || !isObject(b)) return JSON.stringify(a) === JSON.stringify(b)
  return [...new Set([...Object.keys(a), ...Object.keys(b)])].every((k) => same(a[k], b[k]))
}

// typeOf is the round type a round still matches, its count aside.
export const typeOf = (r: Data): RoundType | undefined =>
  roundTypes.find(
    (t) => t.round.selection === r.selection && (t.round.pickScope ?? 'track') === (r.pickScope ?? 'track') && same(t.round.rules, r.rules),
  )

// applyType gives a round the selection and rules of a type, keeping its name, questions and themes.
export function applyType(r: Data, t: RoundType) {
  r.selection = t.round.selection
  if (t.round.pickScope) r.pickScope = t.round.pickScope
  else delete r.pickScope
  if (t.round.count && !r.count) r.count = t.round.count
  if (t.round.rules) r.rules = structuredClone(t.round.rules)
  else delete r.rules
}

// Game defaults, as defaultRules in internal/game/track.go.
const defaults: Data = {
  duration: 30,
  answer: { mode: 'buzz', via: 'oral', answerTime: 10, pauseOnBuzz: true, attempts: 1, wrongLockout: 0, rebound: 'others', fuzziness: 0.2, partial: 'each', incompletePenalty: 0 },
  scoring: { type: 'speed', max: 100, min: 20, ranks: [100, 60, 30], wrongPenalty: 0, reboundBonus: 0 },
  owner: { headStart: 0, exclusive: false, othersBonus: 0, othersMultiplier: 1, stolenPenalty: 0, stolenLosesPoints: false },
  jokers: { double: 0 },
}

// merge lays the fields set in b over a, as the game merges round rules over pack rules.
function merge(a: Data, b: Data | undefined): Data {
  const out: Data = { ...a }
  for (const [k, v] of Object.entries(b ?? {})) out[k] = isObject(v) && isObject(a[k]) ? merge(a[k], v) : (v ?? a[k])
  return out
}

// baseRules is what a round inherits from a pack, or with no pack the game defaults.
export const baseRules = (m?: Data): Data => (m ? merge(defaults, m.rules) : defaults)

const plural =(n: number, word: string) => `${n} ${word}${n > 1 ? 's' : ''}`

// describe tells in plain words how a round plays, or with no round how the pack rules play.
export function describe(m: Data, r?: Data): string[] {
  const rules = merge(merge(defaults, m.rules), r?.rules)
  const { answer: a, scoring: s, owner: o } = rules
  const teams = m.game?.players?.teams
  const who = teams === 'required' ? 'équipe' : teams === 'optional' ? 'joueur ou équipe' : 'joueur'
  const themes = (r?.themes ?? []).map((id: string) => (m.themes ?? []).find((t: Data) => t.id === id)?.name || id).join(', ') || 'aucun thème coché'
  const out: string[] = []
  if (r?.selection === 'sequence') out.push(`${plural(r.tracks?.length ?? 0, 'question')}, dans l'ordre.`)
  if (r?.selection === 'random') out.push(`${plural(r.count ?? 0, 'question')} tirées au hasard. Thèmes : ${themes}.`)
  if (r?.selection === 'theme-pick') {
    const order: Record<string, string> = {
      last: "le dernier au score d'abord",
      first: "le premier au score d'abord",
      random: 'dans un ordre tiré au hasard',
    }
    out.push(`${plural(r.count ?? 0, 'choix')} de thème, chaque ${who} à son tour, ${order[r.picker] ?? "par ordre alphabétique"}. Thèmes : ${themes}.`)
    if (teams && teams !== 'none') out.push("Dans une équipe, c'est le capitaine (le premier à l'avoir rejointe) qui choisit, sur son téléphone.")
    out.push(r.pickScope === 'theme' ? 'Chaque choix enchaîne toutes les questions du thème, dans l’ordre.' : 'Chaque choix joue une question du thème.')
  }
  out.push(`Chaque extrait dure ${rules.duration} s.`)
  const via = a.via === 'device' ? 'sur son téléphone' : "à l'oral"
  if (a.mode === 'simultaneous') {
    out.push(`Tout le monde répond en même temps, ${via}. Les points s'affichent à la fin de l'extrait.`)
  } else {
    out.push(`On buzze${a.pauseOnBuzz ? " (l'extrait se met en pause)" : ''}, puis on a ${a.answerTime} s pour répondre ${via}.`)
    if (a.attempts > 1) out.push(`Chaque ${who} a ${plural(a.attempts, 'essai')} par élément.`)
    out.push(
      a.rebound === 'none'
        ? "Après une erreur, l'extrait s'arrête."
        : a.rebound === 'all'
          ? 'Après une erreur, tout le monde peut rebuzzer.'
          : "Après une erreur, les autres prennent la main d'abord.",
    )
  }
  if (a.wrongLockout > 0) out.push(`Une erreur bloque pendant ${a.wrongLockout} s.`)
  if (a.via === 'device') out.push(a.fuzziness > 0 ? 'Les petites fautes de frappe sont acceptées.' : 'La réponse tapée doit être exacte.')
  else if (a.partial === 'all') out.push("Une réponse incomplète validée par le maître du jeu compte comme fausse.")
  else if (a.incompletePenalty > 0) out.push(`Une réponse incomplète coûte ${a.incompletePenalty} points.`)
  if (s.type === 'speed') out.push(`Un élément trouvé (artiste, titre…) rapporte ${s.max} points tout de suite, puis de moins en moins jusqu'à ${s.min}.`)
  if (s.type === 'fixed') out.push(`Un élément trouvé (artiste, titre…) rapporte ${s.max} points.`)
  if (s.type === 'rank') out.push(`Les premiers à trouver marquent ${s.ranks.join(', ')} points.`)
  if (s.type === 'wager') out.push(`Avant chaque extrait, chaque ${who} mise de ses points : gagnés si bonne réponse, perdus sinon.`)
  if (s.wrongPenalty > 0) out.push(`Une erreur coûte ${s.wrongPenalty} points.`)
  if (s.reboundBonus > 0) out.push(`+${s.reboundBonus} points pour qui trouve après l'erreur d'un autre.`)
  if (rules.jokers.double > 0) out.push(`Jokers « double » permis : ${rules.jokers.double} par ${who} et par partie.`)
  if (!r || r.selection === 'theme-pick') {
    if (o.exclusive) out.push('Seul qui a choisi le thème peut répondre.')
    else {
      if (o.headStart > 0) out.push(`Qui a choisi le thème a ${o.headStart} s d'avance.`)
      const gain = [o.othersMultiplier > 1 && `marque ×${o.othersMultiplier}`, o.othersBonus > 0 && `gagne ${o.othersBonus} points de plus`].filter(Boolean)
      const loss = [o.stolenPenalty > 0 && `${o.stolenPenalty} points`, o.stolenLosesPoints && "les points marqués par l'autre"].filter(Boolean)
      if (gain.length) out.push(`Un autre qui trouve sur ce thème ${gain.join(' et ')}.`)
      if (loss.length) out.push(`Qui a choisi le thème perd ${loss.join(' et ')} quand un autre trouve.`)
    }
  }
  if (r?.eliminate > 0) out.push(`En fin de manche, ${r.eliminate === 1 ? 'le dernier est éliminé' : `les ${r.eliminate} derniers sont éliminés`}.`)
  return out
}
