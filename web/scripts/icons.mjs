import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { deflateSync } from 'node:zlib'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const destination = process.argv[2] ? resolve(process.argv[2]) : resolve(root, 'public')
const source = await readFile(process.argv[3] ? resolve(process.argv[3]) : resolve(root, 'icons/hostbud.svg'), 'utf8')
const out = resolve(destination, 'icons')
await mkdir(out, { recursive: true })
await mkdir(destination, { recursive: true })
await writeFile(resolve(destination, 'favicon.svg'), source)

const crcTable = Uint32Array.from({ length: 256 }, (_, n) => {
  let c = n
  for (let k = 0; k < 8; k++) c = (c & 1) ? 0xedb88320 ^ (c >>> 1) : c >>> 1
  return c >>> 0
})
function crc32(bytes) {
  let c = 0xffffffff
  for (const byte of bytes) c = crcTable[(c ^ byte) & 255] ^ (c >>> 8)
  return (c ^ 0xffffffff) >>> 0
}
function chunk(type, data) {
  const name = Buffer.from(type)
  const length = Buffer.alloc(4); length.writeUInt32BE(data.length)
  const crc = Buffer.alloc(4); crc.writeUInt32BE(crc32(Buffer.concat([name, data])))
  return Buffer.concat([length, name, data, crc])
}
function png(size) {
  const rows = Buffer.alloc((size * 4 + 1) * size)
  const color = (hex) => hex.match(/[0-9a-f]{2}/gi).map((part) => parseInt(part, 16))
  const bg = color('0f1115ff')
  const panel = color('5fb3f9ff')
  const ink = color('0f1115ff')
  const put = (x, y, rgba) => rows.set(rgba, y * (size * 4 + 1) + 1 + x * 4)
  const scale = size / 512
  for (let y = 0; y < size; y++) {
    rows[y * (size * 4 + 1)] = 0
    for (let x = 0; x < size; x++) {
      const px = (x + 0.5) / scale, py = (y + 0.5) / scale
      let c = bg
      if (px >= 80 && px < 432 && py >= 80 && py < 432) c = panel
      // Prompt chevron and underscore, centered in the blue terminal tile.
      const stroke = 14
      const diagonal = (px >= 145 && px <= 235 && py >= 174 && py <= 338 &&
        (Math.abs(py - (px - 145) * 1.04 - 174) < stroke || Math.abs(py + (px - 145) * 1.04 - 338) < stroke))
      const underscore = px >= 258 && px <= 365 && py >= 316 && py <= 344
      if (diagonal || underscore) c = ink
      put(x, y, c)
    }
  }
  const header = Buffer.alloc(13)
  header.writeUInt32BE(size, 0); header.writeUInt32BE(size, 4)
  header[8] = 8; header[9] = 6
  return Buffer.concat([
    Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]),
    chunk('IHDR', header), chunk('IDAT', deflateSync(rows)), chunk('IEND', Buffer.alloc(0)),
  ])
}
for (const [name, size] of [['icon-192.png', 192], ['icon-512.png', 512], ['icon-maskable-512.png', 512], ['apple-touch-icon-180.png', 180]]) {
  await writeFile(resolve(out, name), png(size))
}
