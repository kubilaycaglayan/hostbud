/* global console, setTimeout, URL */
// hostbud-e2e-pushfake: records every push POST (headers and the encrypted
// body) and answers per endpoint path as the scenario set it (201 by
// default; 404, 410, 500, a status list, or a delay). Reachable only on the
// hostbud-e2e network. Scenarios decrypt the bodies with their own keys.
//
//   POST /push/<id>           a push message (what hostbud sends)
//   GET  /ctl/posts?path=<p>  the recorded POSTs to <p> (all without path)
//   POST /ctl/answer          {path, statuses: [..], delayMs?} (the last repeats)
//   POST /ctl/reset           forget everything
//   GET  /health
import { createServer } from 'node:http'
import { Buffer } from 'node:buffer'

let posts = []
let answers = new Map()

function read(req) {
  return new Promise((resolve, reject) => {
    const chunks = []
    let size = 0
    req.on('data', (c) => {
      size += c.length
      if (size > 1 << 20) reject(new Error('too large'))
      else chunks.push(c)
    })
    req.on('end', () => resolve(Buffer.concat(chunks)))
    req.on('error', reject)
  })
}

function json(res, status, body) {
  res.writeHead(status, { 'Content-Type': 'application/json' }).end(JSON.stringify(body))
}

createServer(async (req, res) => {
  const url = new URL(req.url, 'http://pushfake')
  try {
    if (req.method === 'GET' && url.pathname === '/health') return json(res, 200, { ok: true })
    if (req.method === 'POST' && url.pathname.startsWith('/push/')) {
      const body = await read(req)
      posts.push({ path: url.pathname, at: Date.now(), headers: req.headers, body: body.toString('base64') })
      const a = answers.get(url.pathname) ?? { statuses: [201], delayMs: 0 }
      const status = a.statuses.length > 1 ? a.statuses.shift() : a.statuses[0]
      if (a.delayMs) await new Promise((r) => setTimeout(r, a.delayMs))
      res.writeHead(status, a.retryAfter ? { 'Retry-After': String(a.retryAfter) } : {}).end()
      return
    }
    if (req.method === 'GET' && url.pathname === '/ctl/posts') {
      const path = url.searchParams.get('path')
      return json(res, 200, path ? posts.filter((p) => p.path === path) : posts)
    }
    if (req.method === 'POST' && url.pathname === '/ctl/answer') {
      const { path, statuses, delayMs = 0, retryAfter } = JSON.parse((await read(req)).toString() || '{}')
      if (typeof path !== 'string' || !path.startsWith('/push/') || !Array.isArray(statuses) || !statuses.length) return json(res, 400, { error: 'path and statuses required' })
      answers.set(path, { statuses: [...statuses], delayMs, retryAfter })
      return json(res, 200, { ok: true })
    }
    if (req.method === 'POST' && url.pathname === '/ctl/reset') {
      posts = []
      answers = new Map()
      return json(res, 200, { ok: true })
    }
    json(res, 404, { error: 'not found' })
  } catch (e) {
    json(res, 400, { error: String(e) })
  }
}).listen(8080, () => console.log('pushfake listening on :8080'))
