import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { Resvg } from '@resvg/resvg-js'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const destination = process.argv[2] ? resolve(process.argv[2]) : resolve(root, 'public')
const source = await readFile(process.argv[3] ? resolve(process.argv[3]) : resolve(root, 'icons/hostbud.svg'), 'utf8')
const out = resolve(destination, 'icons')
await mkdir(out, { recursive: true })
await writeFile(resolve(destination, 'favicon.svg'), source)

// Render the actual vector master, including gradients and antialiased edges.
// The opaque square and inset mark let iOS/Android apply their own icon masks.
for (const [name, size] of [['icon-192.png', 192], ['icon-512.png', 512], ['icon-maskable-512.png', 512], ['apple-touch-icon-180.png', 180]]) {
  const renderer = new Resvg(source, { fitTo: { mode: 'width', value: size } })
  await writeFile(resolve(out, name), renderer.render().asPng())
}
