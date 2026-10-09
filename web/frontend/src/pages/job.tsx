import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import { Activity, Check, RefreshCw, X } from 'lucide-react'
import type { JobData, JobEvent } from '@/lib/types'
import { apiPath, requestPage } from '@/lib/api'
import { Badge, Details, LinkButton, PageHeading } from '@/components/shared'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'

export interface PairingCandidate { id: string; code: string; masked_sender: string; service: string }
interface LiveState {
  message: string; label: string; class_name: string; status: string;
  started: string; finished: string; confirmation_started: string;
  can_cancel: boolean; awaiting_approval: boolean; terminal: boolean;
}

export function Job({ data }: { data: JobData }) {
  const navigate = useNavigate()
  const queries = useQueryClient()
  const [state, setState] = useState<LiveState>({
    message: data.Job.Message, label: data.Job.StatusLabel, class_name: data.Job.StatusClass,
    status: '', started: data.Job.StartedLabel, finished: data.Job.FinishedLabel,
    confirmation_started: data.Job.ConfirmationLabel, can_cancel: data.Job.CanCancel,
    awaiting_approval: data.Job.AwaitingApproval, terminal: !data.Job.CanCancel,
  })
  const [events, setEvents] = useState<JobEvent[]>(data.Events ?? [])
  const [connection, setConnection] = useState('Connecting to live updates…')
  const [connected, setConnected] = useState(false)
  const [otp, setOTP] = useState('')
  const [pairing, setPairing] = useState<PairingCandidate[]>([])
  const [busy, setBusy] = useState(false)
  const [errors, setErrors] = useState<{ id: number; message: string }[]>([])
  const errorID = useRef(0)
  function showError(message: string) { setErrors(previous => [...previous, { id: ++errorID.current, message }]) }
  function clearSensitive() { setOTP(''); setPairing([]) }
  function expired(path = '/login') { clearSensitive(); queries.clear(); void navigate(path, { replace: true }) }

  useEffect(() => {
    let terminal = !data.Job.CanCancel
    let complete = false
    let lastID = data.LastEventID
    const source = new EventSource(apiPath(`/jobs/${data.Job.ID}/events?after=${lastID}`))
    const clear = () => { setOTP(''); setPairing([]) }
    const listen = <T,>(name: string, callback: (value: T) => void) => {
      source.addEventListener(name, event => {
        try { callback(JSON.parse((event as MessageEvent).data) as T) }
        catch { clear(); setConnected(false); setConnection('Live updates could not be read. Refresh to check the latest progress.'); source.close() }
      })
    }
    source.addEventListener('open', () => { if (!complete) { setConnected(true); setConnection(terminal ? 'Completed · loading final events.' : 'Connected · live updates.') } })
    source.addEventListener('error', () => { clear(); setConnected(false); if (!complete) setConnection('Reconnecting · progress may be out of date. The job may still be running.') })
    source.addEventListener('complete', () => { terminal = true; complete = true; clear(); setConnected(false); setConnection('Completed · live updates ended.'); source.close() })
    source.addEventListener('auth_expired', () => { clear(); source.close(); queries.clear(); void navigate('/login', { replace: true }) })
    listen<LiveState>('state', value => { terminal = value.terminal; setState(value); setConnected(true); setConnection(terminal ? 'Completed · receiving final events.' : 'Connected · live updates.'); if (terminal) clear() })
    listen<{ active: boolean; code: string }>('otp', value => { if (!terminal && document.visibilityState === 'visible') setOTP(value.active ? value.code : '') })
    listen<{ active: boolean; candidates: PairingCandidate[] }>('pairing', value => { if (!terminal && document.visibilityState === 'visible') setPairing(value.active ? value.candidates ?? [] : []) })
    listen<{ id: number; time: string; type: string; message: string }>('job_event', value => {
      if (value.id <= lastID) return
      lastID = value.id
      setEvents(previous => [...previous, { Time: value.time, Type: value.type, Message: value.message }].slice(-500))
    })
    const hidden = () => { if (document.visibilityState !== 'visible') clear() }
    document.addEventListener('visibilitychange', hidden)
    window.addEventListener('pagehide', clear)
    return () => { source.close(); clear(); document.removeEventListener('visibilitychange', hidden); window.removeEventListener('pagehide', clear) }
  }, [data.Job.ID, data.Job.CanCancel, data.LastEventID, navigate, queries])

  async function decide(decision: string, messageID?: string) {
    if (busy || state.terminal || !connected) return
    setBusy(true); clearSensitive()
    const values = new URLSearchParams({ csrf_token: data.CSRFToken, decision })
    if (messageID) values.set('message_id', messageID)
    try {
      const result = await requestPage(`/jobs/${data.Job.ID}/decision`, { method: 'POST', body: values })
      if (result.status !== 204 || result.redirected || result.path !== `/jobs/${data.Job.ID}/decision`) {
        if (result.page?.Page === 'login' || result.page?.Page === 'setup' || result.path.startsWith('/account')) expired(result.path)
        else showError(result.page?.Page === 'error' ? result.page.Data.Message : 'The action could not be confirmed. Check the job status before retrying.')
      }
    } catch (error) { showError(error instanceof Error ? error.message : 'Connection lost. Check the job status before retrying.') }
    finally { setBusy(false) }
  }

  const attention = state.terminal ? '' : pairing.length ? 'Pairing needs your attention. Choose the matching message to continue.' : state.awaiting_approval ? 'Approval needed. Review the booking details before continuing.' : ''
  return <><PageHeading title={data.Job.Command} eyebrow={`Job ${data.Job.ShortID}`} description={`${data.Job.ProfileName} · ${data.Job.CreatedLabel}`}><div className="flex flex-wrap items-center gap-3"><Badge tone={state.class_name}>{state.label}</Badge><LinkButton to="/jobs" secondary>Back to Jobs</LinkButton></div></PageHeading>
    <div className="grid gap-6 xl:grid-cols-[1.4fr_1fr]"><Card><h2>Progress</h2><p role="status" aria-live="polite" className="live-connection">{connected ? <Activity size={15} /> : state.terminal ? <Check size={15} /> : <RefreshCw size={15} />}{connection}</p><p role="status" className="text-sm font-medium text-primary mb-3">{attention}</p><p className="job-message">{state.message}</p>
      {errors.map(error => <div key={error.id} role="alert" className="notice error mt-4 flex justify-between gap-4"><span>{error.message}</span><Button variant="ghost" aria-label="Dismiss notification" onClick={() => setErrors(previous => previous.filter(item => item.id !== error.id))}><X size={16} /></Button></div>)}
      {otp && <div className="otp-panel"><span>Yodel code</span><strong>{otp}</strong><small>Clears after submission or expiry.</small></div>}
      {!!pairing.length && <section className="approval-panel" aria-label="Pair Yodel message"><h3>Choose the matching message</h3><p>Confirm which fresh Messages candidate belongs to this Yodel challenge. Codes and sender details stay in memory.</p><div className="flex flex-wrap gap-3 mt-4">{pairing.map(candidate => <Button key={candidate.id} variant="outline" disabled={busy || !connected} onClick={() => void decide('pair', candidate.id)}>{candidate.code} · {candidate.masked_sender} · {candidate.service}</Button>)}</div></section>}
      {state.awaiting_approval && !state.terminal && <section className="approval-panel" aria-label="Review booking"><h3>Review booking</h3><p>Check the pass described in Progress and these requested details before approving.</p><Details fields={data.Job.BookingReview} /><div className="flex gap-3 mt-5"><Button disabled={busy || !connected} onClick={() => void decide('approve')}>Approve booking</Button><Button variant="destructive" disabled={busy || !connected} onClick={() => void decide('cancel')}>Cancel</Button></div></section>}
      {state.can_cancel && !state.awaiting_approval && !state.terminal && <Button variant="destructive" className="mt-5" disabled={busy || !connected} onClick={() => void decide('cancel-job')}>Cancel job</Button>}
    </Card><Card><h2>Details</h2><Details fields={[...(data.Job.BookingReview ?? []), { Label: 'Confirmation', Value: data.Job.Mode }, ...(data.Job.TimingLabel ? [{ Label: 'Timing', Value: data.Job.TimingLabel }, { Label: 'Expires', Value: data.Job.ExpiresLabel }] : []), { Label: 'Status', Value: state.label }, { Label: 'Earliest start', Value: data.Job.DueLabel }, { Label: 'Started', Value: state.started }, { Label: 'Finished', Value: state.finished }, { Label: 'Final confirmation', Value: state.confirmation_started }]} /></Card></div>
    <div className="section-heading"><div><p className="eyebrow">Activity log</p><h2>Events</h2></div></div><Card><ol className="event-list">{events.map((event, index) => <li key={index}><time>{event.Time}</time><div><strong>{event.Type}</strong><p>{event.Message}</p></div></li>)}</ol>{!events.length && <p className="field-help">Waiting for the first event.</p>}</Card>
  </>
}
