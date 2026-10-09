import type { Page } from './types'

export class ConnectionError extends Error {}

export function apiPath(path: string) {
  if (!path.startsWith('/') || path.startsWith('//')) throw new Error('Invalid application path')
  return `/api${path}`
}

export async function requestPage(path: string, options: RequestInit = {}) {
  return follow(path, options, false, 0)
}

async function follow(path: string, options: RequestInit, redirected: boolean, depth: number): Promise<{ page: Page | null; path: string; status: number; redirected: boolean }> {
  if (depth > 8) throw new ConnectionError('The server returned too many redirects. Refresh before continuing.')
  let response: Response
  try {
    response = await fetch(apiPath(path), {
      ...options, credentials: 'same-origin', cache: 'no-store',
      headers: { Accept: 'application/json', ...options.headers, 'X-Lake-Pass-Navigation': 'manual' },
    })
  } catch (error) {
    if (error instanceof DOMException && error.name === 'AbortError') throw error
    throw new ConnectionError('Connection lost. Check Jobs before retrying a booking or approval.')
  }
  const final = new URL(response.url || apiPath(path), window.location.origin)
  if (final.origin !== window.location.origin || !final.pathname.startsWith('/api/')) {
    throw new ConnectionError('The server returned an unexpected response. Refresh and check Jobs before retrying.')
  }
  const requested = new URL(apiPath(path), window.location.origin)
  const hash = final.hash || (final.pathname === requested.pathname && final.search === requested.search ? requested.hash : '')
  const resolvedPath = final.pathname.slice(4) + final.search + hash
  redirected ||= response.redirected
  if (response.status === 204) return { page: null, path: resolvedPath, status: response.status, redirected }
  if (!response.headers.get('Content-Type')?.startsWith('application/json')) {
    throw new ConnectionError('The server could not confirm the result. Refresh and check Jobs before retrying.')
  }
  let body: unknown
  try { body = await response.json() }
  catch { throw new ConnectionError('The server returned an unreadable response. Refresh and check Jobs before retrying.') }
  if (response.ok && body && typeof body === 'object' && 'Redirect' in body) {
    const target = body.Redirect
    if (typeof target !== 'string' || !target.startsWith('/') || target.startsWith('//') || target.includes('\\')) {
      throw new ConnectionError('The server returned an unexpected redirect. Refresh before continuing.')
    }
    return follow(target, { signal: options.signal }, true, depth + 1)
  }
  const page = body as Page
  if (!page || typeof page.Page !== 'string' || !page.Data || !page.Build) {
    throw new ConnectionError('The server returned an incomplete response. Refresh before continuing.')
  }
  return { page, path: resolvedPath, status: response.status, redirected }
}
