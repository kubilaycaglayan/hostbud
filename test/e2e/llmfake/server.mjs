/* global URL, setTimeout */
/* E2E-only deterministic OpenAI-compatible classifier. Never logs pane text. */
import { createServer } from 'node:http'
import { Buffer } from 'node:buffer'

let requests = []
let script = { label: 'waiting_input', reason: 'The pane appears to be waiting for input.', status: 200, delayMs: 0 }
function read(req) { return new Promise((resolve, reject) => { const chunks=[]; let size=0; req.on('data',c=>{size+=c.length;if(size>1<<20)reject(new Error('too large'));else chunks.push(c)}); req.on('end',()=>resolve(Buffer.concat(chunks))); req.on('error',reject) }) }
function json(res,status,body) { res.writeHead(status,{'Content-Type':'application/json'}).end(JSON.stringify(body)) }
createServer(async(req,res)=>{
  const url=new URL(req.url,'http://llmfake')
  try {
    if(req.method==='GET'&&url.pathname==='/health') return json(res,200,{ok:true})
    if(req.method==='POST'&&url.pathname==='/v1/chat/completions') {
      const body=JSON.parse((await read(req)).toString()||'{}')
      const pane=body.messages?.find(m=>m.role==='user')?.content ?? ''
      const system=body.messages?.find(m=>m.role==='system')?.content ?? ''
      requests.push({at:Date.now(),pane,system})
      if(script.delayMs) await new Promise(r=>setTimeout(r,script.delayMs))
      if(script.status!==200) return json(res,script.status,{error:'scripted provider error'})
      return json(res,200,{choices:[{message:{content:JSON.stringify({label:script.label,reason:script.reason})}}]})
    }
    if(req.method==='GET'&&url.pathname==='/ctl/requests') return json(res,200,requests)
    if(req.method==='POST'&&url.pathname==='/ctl/script') { script={...script,...JSON.parse((await read(req)).toString()||'{}')};return json(res,200,{ok:true}) }
    if(req.method==='POST'&&url.pathname==='/ctl/reset') { requests=[];script={label:'waiting_input',reason:'The pane appears to be waiting for input.',status:200,delayMs:0};return json(res,200,{ok:true}) }
    return json(res,404,{error:'not found'})
  } catch { return json(res,400,{error:'invalid request'}) }
}).listen(8080)
