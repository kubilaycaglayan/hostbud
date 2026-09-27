import { createServer as createUnixServer } from 'node:net'
import { chmod, mkdir, unlink } from 'node:fs/promises'
import { readFileSync } from 'node:fs'
import { Buffer } from 'node:buffer'

const dir = '/run/tailscale'
await mkdir(dir, { recursive: true })
try { await unlink(`${dir}/tailscaled.sock`) } catch { /* no old socket on a fresh volume */ }
const socketServer = createUnixServer((socket) => {
  let data = ''
  socket.on('data', (chunk) => {
    data += chunk
    if (!data.includes('\r\n\r\n')) return
    const path = data.split(' ')[1]
    const mapping = requireMap()
    const status = mapping === 'unknown' ? '404 Not Found' : mapping === 'error' ? '500 Internal Server Error' : '200 OK'
    const body = mapping === 'unknown' ? 'no match' : mapping === 'error' ? 'fake error' : JSON.stringify({ UserProfile: { LoginName: mapping === 'stranger' ? 'stranger@example.com' : 'allowed@example.com' } })
    const response = `HTTP/1.1 ${status}\r\nContent-Type: application/json\r\nContent-Length: ${Buffer.byteLength(body)}\r\nConnection: close\r\n\r\n${body}`
    if (!path.startsWith('/localapi/v0/whois?')) socket.end('404 Not Found\r\nContent-Length: 0\r\n\r\n')
    else socket.end(response)
  })
})
function requireMap() {
  try { return readFileSync(`${dir}/map`, 'utf8').trim() || 'allowed' } catch { return 'allowed' }
}
socketServer.listen(`${dir}/tailscaled.sock`, async () => chmod(`${dir}/tailscaled.sock`, 0o666))
