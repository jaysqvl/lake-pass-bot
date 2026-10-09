import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { Job } from './job'
import { build, jobData } from '@/test/fixtures'

class Stream extends EventTarget {
  static instances: Stream[] = []
  closed = false
  constructor(public url: string) { super(); Stream.instances.push(this) }
  close() { this.closed = true }
  emit(name: string, data: unknown = {}) { act(() => this.dispatchEvent(new MessageEvent(name, { data: JSON.stringify(data) }))) }
}
const running = { status: 'running', label: 'Running', class_name: 'active', message: 'Checking passes', started: 'Started now', finished: '—', confirmation_started: '—', can_cancel: true, awaiting_approval: false, terminal: false }
function open(terminal = false) {
  render(<QueryClientProvider client={new QueryClient()}><MemoryRouter initialEntries={['/jobs/42']}><Routes><Route path="/jobs/42" element={<Job data={jobData(terminal)} />} /><Route path="/login" element={<h1>Sign in again</h1>} /><Route path="/account" element={<h1>Replace your password</h1>} /></Routes></MemoryRouter></QueryClientProvider>)
  return Stream.instances.at(-1)!
}
function sensitive(stream: Stream) {
  stream.emit('otp', { active: true, code: '123456' })
  stream.emit('pairing', { active: true, candidates: [{ id: 'message-1', code: '654321', masked_sender: '***1234', service: 'SMS' }] })
}
function expectCleared() {
  expect(screen.queryByText('123456')).toBeNull()
  expect(screen.queryByRole('button', { name: /654321/ })).toBeNull()
}
beforeEach(() => { Stream.instances = []; vi.stubGlobal('EventSource', Stream); vi.stubGlobal('fetch', vi.fn(async () => new Response(null, { status: 204 }))); Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' }) })
afterEach(() => { vi.unstubAllGlobals() })

describe('live jobs', () => {
  it('reconnects without replaying decisions, retains stale progress, deduplicates history and drains final events', () => {
    const stream = open()
    expect(stream.url).toBe('/api/jobs/42/events?after=7')
    stream.emit('open'); stream.emit('state', running); sensitive(stream)
    stream.emit('error')
    expectCleared()
    expect(screen.getByText(/Reconnecting/)).toBeVisible()
    expect(screen.getByText('Checking passes')).toBeVisible()
    expect(stream.closed).toBe(false)
    expect(fetch).not.toHaveBeenCalled()
    stream.emit('open'); stream.emit('state', { ...running, terminal: true, can_cancel: false, label: 'Succeeded', finished: 'Finished now', confirmation_started: 'Confirmed now', message: 'Booking confirmed' })
    expect(screen.getByText('Finished now')).toBeVisible()
    expect(screen.getByText('Confirmed now')).toBeVisible()
    expect(screen.queryByRole('button', { name: 'Cancel job' })).toBeNull()
    sensitive(stream); expectCleared()
    for (const id of [7, 8, 8, 9]) stream.emit('job_event', { id, time: '12:00:01', type: 'progress', message: `Event ${id}` })
    expect(screen.getAllByText('Event 8')).toHaveLength(1)
    expect(screen.getAllByText('Event 9')).toHaveLength(1)
    expect(stream.closed).toBe(false)
    stream.emit('complete'); expect(stream.closed).toBe(true)
    stream.emit('error'); expect(screen.getByText('Completed · live updates ended.')).toBeVisible()
  })

  it.each(['error', 'complete', 'auth_expired', 'pagehide', 'hidden', 'terminal'])('%s clears displayed OTPs and pairing candidates', signal => {
    const stream = open(); sensitive(stream)
    expect(screen.getByText('123456')).toBeVisible()
    if (signal === 'pagehide') act(() => window.dispatchEvent(new Event('pagehide')))
    else if (signal === 'hidden') { Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' }); act(() => document.dispatchEvent(new Event('visibilitychange'))) }
    else if (signal === 'terminal') stream.emit('state', { ...running, terminal: true, can_cancel: false })
    else stream.emit(signal)
    expectCleared()
    if (signal === 'auth_expired') expect(screen.getByText('Sign in again')).toBeVisible()
  })

  it('rejects transient secrets and actions on initially terminal jobs', () => {
    const stream = open(true); sensitive(stream); expectCleared()
    expect(screen.queryByRole('button', { name: 'Cancel job' })).toBeNull()
    expect(fetch).not.toHaveBeenCalled()
  })

  it('shows booking review, keeps cancellation choices distinct and submits only the selected pairing identity', async () => {
    const stream = open(); stream.emit('open'); stream.emit('state', { ...running, awaiting_approval: true })
    expect(screen.getByRole('region', { name: 'Review booking' })).toHaveTextContent('2030-07-14')
    expect(screen.getByRole('region', { name: 'Review booking' })).toHaveTextContent('TEST CAR')
    expect(screen.queryByRole('button', { name: 'Cancel job' })).toBeNull()
    sensitive(stream)
    const attention = screen.getByText(/Pairing needs your attention/)
    expect(attention).not.toHaveTextContent('654321')
    fireEvent.click(screen.getByRole('button', { name: /654321/ }))
    await waitFor(() => expect(fetch).toHaveBeenCalledOnce())
    const [url, request] = vi.mocked(fetch).mock.calls[0]
    expect(url).toBe('/api/jobs/42/decision')
    expect(Object.fromEntries(request!.body as URLSearchParams)).toEqual({ csrf_token: 'synthetic-csrf', decision: 'pair', message_id: 'message-1' })
    expect(request?.credentials).toBe('same-origin')
    expectCleared()
  })

  it('does not retry an uncertain approval and reports a safe error', async () => {
    vi.mocked(fetch).mockRejectedValue(new Error('private network failure'))
    const stream = open(); stream.emit('open'); stream.emit('state', { ...running, awaiting_approval: true })
    fireEvent.click(screen.getByRole('button', { name: 'Approve booking' }))
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('Check Jobs before retrying'))
    expect(fetch).toHaveBeenCalledOnce()
    expect(screen.getByRole('alert')).not.toHaveTextContent('private network failure')
  })

  it('does not treat an authentication redirect or HTML page as a successful decision', async () => {
    const response = new Response(JSON.stringify({ Page: 'login', Data: { Title: 'Sign in' }, Build: build }), { headers: { 'Content-Type': 'application/json' } })
    Object.defineProperty(response, 'url', { value: `${window.location.origin}/api/login` })
    vi.mocked(fetch).mockResolvedValue(response)
    const stream = open(); stream.emit('open'); sensitive(stream); stream.emit('state', { ...running, awaiting_approval: true })
    fireEvent.click(screen.getByRole('button', { name: 'Approve booking' }))
    await waitFor(() => expect(screen.getByText('Sign in again')).toBeVisible())
    expect(fetch).toHaveBeenCalledOnce(); expectCleared()
  })

  it.each(['redirected-204', 'unexpected-200', 'html'])('rejects a decision result of %s without retrying', async kind => {
    const response = kind === 'redirected-204' ? new Response(null, { status: 204 })
      : kind === 'html' ? new Response('<html>private response</html>', { headers: { 'Content-Type': 'text/html' } })
        : new Response(JSON.stringify({ Page: 'job', Data: jobData(), Build: build }), { headers: { 'Content-Type': 'application/json' } })
    Object.defineProperty(response, 'url', { value: `${window.location.origin}/api/jobs/42/decision` })
    if (kind === 'redirected-204') Object.defineProperty(response, 'redirected', { value: true })
    vi.mocked(fetch).mockResolvedValue(response)
    const stream = open(); stream.emit('open'); sensitive(stream); stream.emit('state', { ...running, awaiting_approval: true })
    fireEvent.click(screen.getByRole('button', { name: 'Approve booking' }))
    await waitFor(() => expect(screen.getByRole('alert')).toBeVisible())
    expect(screen.getByRole('alert')).not.toHaveTextContent('private response')
    expect(fetch).toHaveBeenCalledOnce(); expectCleared()
  })

  it('navigates to required password replacement and clears transient codes', async () => {
    const response = new Response(JSON.stringify({ Page: 'account', Data: { Title: 'Account' }, Build: build }), { headers: { 'Content-Type': 'application/json' } })
    Object.defineProperty(response, 'url', { value: `${window.location.origin}/api/account?password=required` })
    vi.mocked(fetch).mockResolvedValue(response)
    const stream = open(); stream.emit('open'); sensitive(stream); stream.emit('state', { ...running, awaiting_approval: true })
    fireEvent.click(screen.getByRole('button', { name: 'Approve booking' }))
    await waitFor(() => expect(screen.getByText('Replace your password')).toBeVisible())
    expect(fetch).toHaveBeenCalledOnce(); expectCleared()
  })

  it('keeps separate failed actions visible until each notification is dismissed', async () => {
    vi.mocked(fetch).mockRejectedValue(new Error('private network failure'))
    const stream = open(); stream.emit('open'); stream.emit('state', { ...running, awaiting_approval: true })
    fireEvent.click(screen.getByRole('button', { name: 'Approve booking' }))
    await waitFor(() => expect(screen.getAllByRole('alert')).toHaveLength(1))
    fireEvent.click(screen.getByRole('button', { name: 'Approve booking' }))
    await waitFor(() => expect(screen.getAllByRole('alert')).toHaveLength(2))
    fireEvent.click(screen.getAllByRole('button', { name: 'Dismiss notification' })[0])
    expect(screen.getAllByRole('alert')).toHaveLength(1)
    expect(fetch).toHaveBeenCalledTimes(2)
  })
})
