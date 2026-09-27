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
