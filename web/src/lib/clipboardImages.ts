/** Return image files from a browser paste event, leaving ordinary text paste alone. */
export function clipboardImages(event: ClipboardEvent): File[] {
  const data = event.clipboardData
  if (!data) return []
  const fromItems = Array.from(data.items)
    .filter((item) => item.kind === 'file' && item.type.startsWith('image/'))
    .map((item) => item.getAsFile())
    .filter((file): file is File => file !== null)
  const files = fromItems.length ? fromItems : Array.from(data.files)
  return files.filter((file) => file.type.startsWith('image/'))
}

/** Path of `absolutePath` relative to the directory `base`, prefixed with ./ when inside it. */
export function relativePath(base: string, absolutePath: string): string {
  const from = base.split('/').filter(Boolean)
  const to = absolutePath.split('/').filter(Boolean)
  let common = 0
  while (common < from.length && common < to.length && from[common] === to[common]) common++
  const relative = [...Array(from.length - common).fill('..'), ...to.slice(common)].join('/')
  return relative.startsWith('..') ? relative : `./${relative}`
}

/** Insert `text` over the selection [start, end), padding with spaces so it stays a separate word. */
export function insertWord(value: string, start: number, end: number, text: string): { value: string; caret: number } {
  const before = value.slice(0, start)
  const after = value.slice(end)
  const lead = before && !/\s$/.test(before) ? ' ' : ''
  const trail = after && !/^\s/.test(after) ? ' ' : ''
  const inserted = `${lead}${text}${trail}`
  return { value: before + inserted + after, caret: before.length + lead.length + text.length }
}
