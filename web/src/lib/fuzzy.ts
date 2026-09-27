export interface FuzzyCandidate {
  label: string
  secondary?: string
}

/** Scores one case-insensitive subsequence, favoring word starts and runs. */
function scoreField(query: string, field: string): number | null {
  const text = field.toLocaleLowerCase()
  const needle = query.toLocaleLowerCase()
  if (!needle) return 0
  if (needle.length > text.length) return null

  let previous = new Map<number, number>()
  for (let at = 0; at < text.length; at++) {
    if (text[at] !== needle[0]) continue
    const boundary = at === 0 || /[^\p{L}\p{N}]/u.test(text[at - 1]) || (/\p{Ll}/u.test(text[at - 1]) && /\p{Lu}/u.test(text[at]))
    previous.set(at, 10 + (boundary ? 9 : 0) - at * 0.15)
  }

  for (let index = 1; index < needle.length; index++) {
    const next = new Map<number, number>()
    for (let at = 0; at < text.length; at++) {
      if (text[at] !== needle[index]) continue
      const boundary = at === 0 || /[^\p{L}\p{N}]/u.test(text[at - 1]) || (/\p{Ll}/u.test(text[at - 1]) && /\p{Lu}/u.test(text[at]))
      let best = Number.NEGATIVE_INFINITY
      for (const [prior, value] of previous) {
        if (prior >= at) continue
        best = Math.max(best, value + 10 + (boundary ? 9 : 0) + (prior === at - 1 ? 16 : 0) - (at - prior - 1) * 0.15)
      }
      if (Number.isFinite(best)) next.set(at, best)
    }
    previous = next
    if (previous.size === 0) return null
  }
  return previous.size ? Math.max(...previous.values()) : null
}

/** Returns matches in descending score, preserving the input order for ties. */
export function fuzzyFilter<T extends FuzzyCandidate>(query: string, candidates: readonly T[], limit = 50): T[] {
  const needle = query.trim()
  if (!needle) return candidates.slice(0, limit)
  return candidates
    .map((candidate, index) => {
      const primary = scoreField(needle, candidate.label)
      const secondary = candidate.secondary ? scoreField(needle, candidate.secondary) : null
      const score = primary === null ? secondary : secondary === null ? primary : Math.max(primary, secondary)
      return { candidate, index, score }
    })
    .filter((entry): entry is { candidate: T; index: number; score: number } => entry.score !== null)
    .sort((a, b) => b.score - a.score || a.index - b.index)
    .slice(0, limit)
    .map(({ candidate }) => candidate)
}
