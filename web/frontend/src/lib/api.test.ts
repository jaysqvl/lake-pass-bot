import { afterEach, describe, expect, it, vi } from 'vitest'
import { apiPath, requestPage } from './api'
import { base, build } from '@/test/fixtures'

afterEach(() => vi.unstubAllGlobals())

function response(body: string, url: string, status = 200, kind = 'application/json') {
  const result = new Response(body, { status, headers: { 'Content-Type': kind } })
  Object.defineProperty(result, 'url', { value: url })
  return result
}

describe('same-origin API client', () => {
  it('follows a server redirect into a UI route while keeping credentials in cookies', async () => {
    const fetch = vi.fn(async () => response(JSON.stringify({ Page: 'account', Data: base, Build: build }), `${window.location.origin}/api/account?password=required`))
    vi.stubGlobal('fetch', fetch)
    const result = await requestPage('/jobs/42')
    expect(result.path).toBe('/account?password=required')
    expect(fetch).toHaveBeenCalledWith('/api/jobs/42', expect.objectContaining({ credentials: 'same-origin', cache: 'no-store', headers: { Accept: 'application/json', 'X-Lake-Pass-Navigation': 'manual' } }))
    expect(() => apiPath('//another.example')).toThrow()
  })

  it('preserves navigation anchors and follows a submitted form with a GET', async () => {
    const fetch = vi.fn()
      .mockResolvedValueOnce(response(JSON.stringify({ Redirect: '/lakes/buntzen?ok=created#connection' }), `${window.location.origin}/api/profiles/new`))
      .mockResolvedValueOnce(response(JSON.stringify({ Page: 'lake', Data: base, Build: build }), `${window.location.origin}/api/lakes/buntzen?ok=created`))
    vi.stubGlobal('fetch', fetch)
    const result = await requestPage('/profiles/new', { method: 'POST', body: new URLSearchParams({ csrf_token: 'synthetic' }) })
    expect(result).toMatchObject({ path: '/lakes/buntzen?ok=created#connection', redirected: true })
    expect(fetch).toHaveBeenCalledTimes(2)
    expect(fetch.mock.calls[1][0]).toBe('/api/lakes/buntzen?ok=created#connection')
    expect(fetch.mock.calls[1][1]).not.toHaveProperty('method')
    expect(fetch.mock.calls[1][1]).not.toHaveProperty('body')
  })

  it.each(['//another.example', '/\\another.example', 'https://another.example'])('rejects an invalid navigation target %s before following it', async target => {
    const fetch = vi.fn(async () => response(JSON.stringify({ Redirect: target }), `${window.location.origin}/api/login`))
    vi.stubGlobal('fetch', fetch)
    await expect(requestPage('/login', { method: 'POST' })).rejects.toThrow('unexpected redirect')
    expect(fetch).toHaveBeenCalledOnce()
  })

  it.each([
    ['text/html', '<html>private-upstream-diagnostic</html>'],
    ['application/json', '{private-upstream-diagnostic'],
    ['application/json', '{}'],
  ])('does not expose an unexpected %s response or repeat a mutation', async (kind, body) => {
    const fetch = vi.fn(async () => response(body, `${window.location.origin}/api/jobs/42/decision`, 502, kind))
    vi.stubGlobal('fetch', fetch)
    const result = requestPage('/jobs/42/decision', { method: 'POST', body: new URLSearchParams({ decision: 'approve' }) })
    await expect(result).rejects.not.toThrow('private-upstream-diagnostic')
    expect(fetch).toHaveBeenCalledOnce()
  })

  it('rejects an external redirect without accepting it as a mutation success', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => response('{}', 'https://another.example/api/jobs/42')))
    await expect(requestPage('/jobs/42/decision', { method: 'POST' })).rejects.toThrow('unexpected response')
  })
})
