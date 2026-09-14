// Public artwork is fetched outside the authenticated Electron session. Some
// icon hosts reject Chromium requests (including plain <img> loads) and omit
// CORS headers on the error response. Never forward session cookies or renderer
// headers to these third-party URLs.
const maxBytes = 512 * 1024
const maxConcurrent = 6
let active = 0
const waiting: Array<() => void> = []

export async function loadRemoteIcon(input: unknown): Promise<string> {
  if (typeof input !== 'string') throw new Error('Invalid icon URL')
  const url = new URL(input)
  if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password) {
    throw new Error('Invalid icon URL')
  }

  if (active >= maxConcurrent) await new Promise<void>(resolve => waiting.push(resolve))
  else active++
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), 15_000)
  try {
    const response = await fetch(url, { credentials: 'omit', signal: controller.signal })
    const type = response.headers.get('content-type')?.split(';')[0].trim().toLowerCase() ?? ''
    if (!response.ok || !type.startsWith('image/')) throw new Error('Icon request failed')
    if (Number(response.headers.get('content-length')) > maxBytes) throw new Error('Icon too large')
    if (!response.body) throw new Error('Empty icon')

    // Check streamed bytes as well: Content-Length can be missing or refer to
    // the compressed body. Do not buffer an unbounded remote response over IPC.
    const chunks: Uint8Array[] = []
    let size = 0
    for await (const chunk of response.body) {
      size += chunk.byteLength
      if (size > maxBytes) throw new Error('Icon too large')
      chunks.push(chunk)
    }
    if (!size) throw new Error('Empty icon')
    return `data:${type};base64,${Buffer.concat(chunks, size).toString('base64')}`
  } finally {
    clearTimeout(timeout)
    controller.abort()
    const next = waiting.shift()
    if (next) next()
    else active--
  }
}
