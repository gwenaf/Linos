import type { Data } from './ws'

// Three-way merge of manifests, for two editors on one pack: base is the version both started from,
// mine and theirs what each made of it. What only one side changed is kept; when both changed the
// same value, mine wins. Lists of objects with an id (questions, themes) merge item by item; lists of
// ids (a round's questions, its themes) keep what each side added or removed; other lists (rounds,
// guesses) merge position by position, with what each side appended.
export function merge(base: unknown, mine: unknown, theirs: unknown): unknown {
  if (same(mine, base)) return theirs
  if (same(theirs, base) || same(mine, theirs)) return mine
  if (isObject(base) && isObject(mine) && isObject(theirs)) {
    const out: Data = {}
    for (const k of new Set([...Object.keys(theirs), ...Object.keys(mine)])) {
      const v = merge(base[k], mine[k], theirs[k])
      if (v !== undefined) out[k] = v
    }
    return out
  }
  if (Array.isArray(mine) && Array.isArray(theirs)) {
    const b = Array.isArray(base) ? base : []
    if ([b, mine, theirs].every((l) => l.every((x) => isObject(x) && typeof x.id === 'string'))) {
      const byId = (l: Data[]) => new Map(l.map((x) => [x.id, x]))
      const [bm, mm, tm] = [byId(b), byId(mine), byId(theirs)]
      const ids = [...new Set([...tm.keys(), ...mm.keys()])].filter((id) => !(bm.has(id) && (!mm.has(id) || !tm.has(id))))
      return ids.map((id) => merge(bm.get(id), mm.get(id), tm.get(id)))
    }
    if ([b, mine, theirs].every((l) => l.every((x) => typeof x === 'string'))) {
      const removed = new Set(b.filter((x) => !mine.includes(x) || !theirs.includes(x)))
      return [...new Set([...theirs, ...mine])].filter((x) => !removed.has(x))
    }
    if (mine.length >= b.length && theirs.length >= b.length) {
      return [...b.map((x, i) => merge(x, mine[i], theirs[i])), ...theirs.slice(b.length), ...mine.slice(b.length)]
    }
  }
  return mine
}

const isObject = (v: unknown): v is Data => !!v && typeof v === 'object' && !Array.isArray(v)
const same = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b)
