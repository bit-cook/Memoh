import { afterEach, expect, it, vi } from 'vitest'
import { loadRemoteIcon } from './remote-icon'

afterEach(() => vi.unstubAllGlobals())

it('returns only image bytes without using authenticated session headers', async () => {
  const request = vi.fn().mockResolvedValue(new Response('<svg/>', {
    headers: { 'Content-Type': 'image/svg+xml; charset=utf-8' },
  }))
  vi.stubGlobal('fetch', request)
  expect(await loadRemoteIcon('https://example.com/icon.svg')).toBe('data:image/svg+xml;base64,PHN2Zy8+')
  expect(request.mock.calls[0]![1]).toEqual({ credentials: 'omit', signal: expect.any(AbortSignal) })
})

it.each(['file:///etc/passwd', 'data:image/png;base64,AA==', 'https://user:pass@example.com/icon', null])(
  'rejects unsupported or credential-bearing icon URL %s before a request', async (url) => {
    const request = vi.fn()
    vi.stubGlobal('fetch', request)
    await expect(loadRemoteIcon(url)).rejects.toThrow()
    expect(request).not.toHaveBeenCalled()
  },
)

it.each([
  new Response('blocked', { status: 403 }),
  new Response('<html/>', { headers: { 'Content-Type': 'text/html' } }),
  new Response('', { headers: { 'Content-Type': 'image/png' } }),
  new Response('x', { headers: { 'Content-Type': 'image/png', 'Content-Length': '524289' } }),
  new Response(new Uint8Array(524289), { headers: { 'Content-Type': 'image/png' } }),
])('rejects invalid or oversized responses even without Content-Length', async (response) => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response))
  await expect(loadRemoteIcon('https://example.com/icon')).rejects.toThrow()
})

it('bounds concurrent downloads and releases slots after failure', async () => {
  const pending: Array<(value: Response) => void> = []
  const request = vi.fn(() => new Promise<Response>(resolve => pending.push(resolve)))
  vi.stubGlobal('fetch', request)
  const results = Promise.allSettled(Array.from({ length: 8 }, (_, i) => loadRemoteIcon(`https://example.com/${i}`)))
  expect(request).toHaveBeenCalledTimes(6)
  for (let i = 0; i < 8; i++) {
    await vi.waitFor(() => expect(pending[i]).toBeDefined())
    pending[i]!(new Response('bad', { status: 500 }))
  }
  expect((await results).every(result => result.status === 'rejected')).toBe(true)
})
