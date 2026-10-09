import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Link, NavLink, useLocation, useNavigate } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { CalendarDays, Home, ListChecks, LogOut, Menu, Mountain, Radio, Settings as SettingsIcon, X } from 'lucide-react'
import { requestPage } from '@/lib/api'
import type { Base, Build, Page } from '@/lib/types'
import { ActionForm, ActionsContext } from '@/components/forms'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { JobsTable, LinkButton, PageHeading } from '@/components/shared'
import { Dashboard, Lakes, Lake, Bookings, QuickBooking, ResourceForm, ResourceList } from '@/pages/workspace'
import { Account, Network, Settings, User, Users } from '@/pages/settings'
import { Auth } from '@/pages/auth'
import { Job } from '@/pages/job'

export default function App() {
  const location = useLocation()
  const navigate = useNavigate()
  const queries = useQueryClient()
  const path = location.pathname + location.search
  const [submitted, setSubmitted] = useState<{ path: string; page: Page; revision: number } | null>(null)
  const [error, setError] = useState('')
  const [dismissedFlash, setDismissedFlash] = useState('')
  const lastPath = useRef(path)
  lastPath.current = path
  const query = useQuery({ queryKey: ['page', path], queryFn: ({ signal }) => requestPage(path, { signal }), staleTime: 0, gcTime: 0, retry: false, refetchOnWindowFocus: false })
  const result = query.data
  const page = (submitted?.path === path ? submitted.page : null) ?? result?.page
  useEffect(() => {
    setError('')
    if (result && result.path.split('#')[0] !== path) void navigate(result.path, { replace: true })
  }, [result, path, navigate])
  useEffect(() => { if (page) document.title = `${page.Data.Title} · Lake Pass Bot` }, [page])
  useEffect(() => {
    if (!page || !location.hash) return
    try { document.getElementById(decodeURIComponent(location.hash.slice(1)))?.scrollIntoView({ block: 'start' }) }
    catch { /* A malformed URL fragment has no matching section. */ }
  }, [page, location.hash])
  useEffect(() => {
    if (submitted?.path === path) document.getElementById('form-error')?.focus()
  }, [submitted, path])
  const mutation = useMutation({
    mutationFn: ({ action, values }: { action: string; values: URLSearchParams }) => requestPage(action, { method: 'POST', body: values }),
    retry: false,
  })
  async function submit(action: string, values: URLSearchParams) {
    if (mutation.isPending) return
    setError('')
    const startedPath = path
    try {
      const response = await mutation.mutateAsync({ action, values })
      if (lastPath.current !== startedPath) return
      if (!response.page) { await query.refetch(); return }
      if (response.status >= 400) {
        setSubmitted(previous => ({ path, page: response.page!, revision: (previous?.revision ?? 0) + 1 }))
      } else {
        setSubmitted(null)
        queries.removeQueries({ queryKey: ['page'] })
        if (response.path === path) await query.refetch()
        else void navigate(response.path)
      }
    } catch (error) { if (lastPath.current === startedPath) setError(error instanceof Error ? error.message : 'The action could not be confirmed. Check Jobs before retrying.') }
  }
  if (query.isPending || !page || (result && result.path.split('#')[0] !== path && submitted?.path !== path)) {
    if (query.error) return <main className="standalone-error"><Card><h1>Connection unavailable</h1><p>{query.error.message}</p><Button className="mt-5" onClick={() => void query.refetch()}>Try again</Button></Card></main>
    return <main className="loading-screen" role="status"><Mountain size={30} /><p>Loading Lake Pass Bot…</p></main>
  }
  const flash = page.Data.Flash
  const flashKey = path + (flash?.Message ?? '')
  return <ActionsContext.Provider value={{ submit, busy: mutation.isPending, csrf: page.Data.CSRFToken }}>
    <Shell data={page.Data} build={page.Build}>
      {error && <div role="alert" className="notice error mb-6 flex justify-between gap-4"><span>{error}</span><Button variant="ghost" onClick={() => setError('')} aria-label="Dismiss notification"><X size={16} /></Button></div>}
      {flash && dismissedFlash !== flashKey && <div role={flash.Kind === 'error' ? 'alert' : 'status'} className={`notice ${flash.Kind} mb-6 flex justify-between gap-4`}><div>{flash.Message}{flash.ActionURL && <> <Link className="text-link" to={flash.ActionURL}>{flash.ActionLabel}</Link></>}</div><Button variant="ghost" aria-label="Dismiss notification" onClick={() => setDismissedFlash(flashKey)}><X size={16} /></Button></div>}
      <div key={`${path}:${submitted?.path === path ? submitted.revision : 0}`}><PageContent page={page} /></div>
    </Shell>
  </ActionsContext.Provider>
}

function PageContent({ page }: { page: Page }) {
  switch (page.Page) {
    case 'login': return <Auth data={page.Data} />
    case 'setup': return <Auth data={page.Data} setup />
    case 'dashboard': return <Dashboard data={page.Data} />
    case 'lakes': return <Lakes data={page.Data} />
    case 'lake': return <Lake data={page.Data} />
    case 'bookings': return <Bookings data={page.Data} />
    case 'quick_booking': return <QuickBooking data={page.Data} />
    case 'form': return <ResourceForm data={page.Data} />
    case 'list': return <ResourceList data={page.Data} />
    case 'jobs': return <><PageHeading title="Jobs" description="Follow booking attempts, sign-in checks, and pairing." /><JobsTable jobs={page.Data.Jobs} /></>
    case 'job': return <Job data={page.Data} />
    case 'account': return <Account data={page.Data} />
    case 'settings': return <Settings data={page.Data} />
    case 'network_settings': return <Network data={page.Data} />
    case 'users': return <Users data={page.Data} />
    case 'user': return <User data={page.Data} />
    case 'error': return <Card className="max-w-2xl mx-auto my-10"><PageHeading title={page.Data.Title} description={page.Data.Message} /><LinkButton to={page.Data.ReturnURL}>{page.Data.ReturnLabel}</LinkButton></Card>
  }
}

const navigation = [
  { path: '/', label: 'Home', icon: Home }, { path: '/lakes', label: 'Lakes', icon: Mountain },
  { path: '/bookings', label: 'Bookings', icon: CalendarDays }, { path: '/jobs', label: 'Jobs', icon: ListChecks },
  { path: '/sources', label: 'OTP sources', icon: Radio }, { path: '/settings', label: 'Settings', icon: SettingsIcon },
]

function Shell({ data, build, children }: { data: Base; build: Build; children: ReactNode }) {
  const [menu, setMenu] = useState(false)
  const location = useLocation()
  useEffect(() => setMenu(false), [location.pathname])
  useEffect(() => {
    if (!menu) return
    const close = (event: KeyboardEvent) => { if (event.key === 'Escape') setMenu(false) }
    window.addEventListener('keydown', close)
    return () => window.removeEventListener('keydown', close)
  }, [menu])
  return <div className={data.Authenticated ? 'app-layout' : 'public-layout'}>
    {data.Authenticated && <><aside id="mobile-navigation" className={`sidebar ${menu ? 'is-open' : ''}`}><Link to="/" className="brand"><Mountain size={27} /><span>Lake Pass Bot<small>Make room for outside.</small></span></Link>
      <nav aria-label="Main navigation">{navigation.map(({ path, label, icon: Icon }) => <NavLink key={path} to={path} end={path === '/'} className={({ isActive }) => isActive || (path === '/lakes' && location.pathname.startsWith('/profiles')) || (path === '/settings' && (location.pathname.startsWith('/account') || location.pathname.startsWith('/admin'))) ? 'active' : ''}><Icon size={18} /><span>{label}</span></NavLink>)}</nav>
      <div className="sidebar-account"><Link to="/account" aria-label={`Account settings for ${data.Username}`}><span className="avatar">{data.Username.slice(0, 1).toUpperCase()}</span><span>{data.Username}<small>{data.IsAdmin ? 'Administrator' : 'Member'}</small></span></Link><ActionForm action="/logout"><Button variant="ghost" type="submit" aria-label="Sign out"><LogOut size={17} /></Button></ActionForm></div>
    </aside><header className="mobile-header"><Link to="/" className="brand"><Mountain size={22} /><span>Lake Pass Bot</span></Link><Button variant="ghost" aria-label="Toggle navigation" aria-expanded={menu} aria-controls="mobile-navigation" onClick={() => setMenu(!menu)}>{menu ? <X size={22} /> : <Menu size={22} />}</Button></header></>}
    <div className="main-column"><main id="main-content" className="main-content">{children}</main><footer id="build-info" data-version={build.Version} data-revision={build.Revision}><span>Lake Pass Bot</span><div>{build.ReleaseURL ? <a href={build.ReleaseURL} target="_blank" rel="noreferrer">{build.Label}</a> : <span>{build.Label}</span>}{build.CommitURL && <a href={build.CommitURL} target="_blank" rel="noreferrer" title={`Build ${build.Revision}`}>Build {build.ShortRevision}</a>}</div></footer></div>
  </div>
}
