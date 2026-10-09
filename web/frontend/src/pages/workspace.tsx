import { CalendarDays, ChevronRight, Mountain, Plus } from 'lucide-react'
import { Link } from 'react-router-dom'
import type { BookingsData, DashboardData, FormData, LakeData, LakesData, ListData, QuickBookingData } from '@/lib/types'
import { ActionForm, FormError, FormFooter, FormSections } from '@/components/forms'
import { ConnectionCard, Details, EmptyState, JobsTable, LinkButton, PageHeading, Resource, Badge } from '@/components/shared'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'

export function Dashboard({ data }: { data: DashboardData }) {
  return <><PageHeading title="Home" eyebrow="Your next day outside" description="Your lakes, upcoming visits, and recent activity."><LinkButton to="/bookings"><Plus size={16} />Book a pass</LinkButton></PageHeading>
    <div className="grid gap-6 lg:grid-cols-2"><Card><div className="panel-heading"><div><h2>Upcoming visits</h2><p>{data.BookingCount} upcoming {data.BookingCount === 1 ? 'visit' : 'visits'}</p></div><CalendarDays size={20} className="text-muted-foreground" /></div>
      {data.Bookings?.length ? <div className="divide-y divide-border">{data.Bookings.map(booking => <Link key={booking.URL} to={booking.URL} className="visit-row"><div><strong>{booking.Name}</strong><p>{booking.Date}</p></div><ChevronRight size={18} /></Link>)}</div> : <EmptyState title="No upcoming visits"><p>Choose a lake, then pick your date and passes.</p><Link to="/bookings" className="text-link">Plan a visit →</Link></EmptyState>}
    </Card><Card><div className="panel-heading"><div><h2>Your lakes</h2><p>Connection status for your accounts</p></div><Link to="/lakes" className="text-link">Manage lakes</Link></div>
      {data.Connections?.map(connection => <Link key={connection.Lake.ID} to={connection.ActionURL} className="connection-row"><div className="lake-symbol"><Mountain size={22} /></div><div><strong>{connection.Lake.Name}</strong><p>{connection.Description}</p><span className="text-link">{connection.ActionLabel} →</span></div><Badge tone={connection.StatusClass}>{connection.Status}</Badge></Link>)}
    </Card></div><div className="section-heading"><div><h2>Recent jobs</h2><p>Follow booking attempts and sign-in checks.</p></div><Link to="/jobs" className="text-link">View all jobs →</Link></div><JobsTable jobs={data.Jobs} /></>
}

export function Lakes({ data }: { data: LakesData }) {
  return <><PageHeading title="Lakes" eyebrow="Find your connection" description="Connect your booking accounts and set up preferences for each lake." />
    <div className="grid gap-6 lg:grid-cols-2">{data.Connections?.map(connection => <ConnectionCard key={connection.Lake.ID} connection={connection} />)}</div></>
}

export function Lake({ data }: { data: LakeData }) {
  return <><PageHeading title={data.Lake.Name} eyebrow="Lakes / Connection & preferences" description="Manage your connection and booking preferences for this lake.">{data.Connection.Configured && <LinkButton to={`/bookings/new?lake_id=${data.Lake.ID}`}>Book a pass</LinkButton>}</PageHeading>
    <Card id="connection"><div className="panel-heading"><div><h2>Connection</h2><p>{data.Lake.Name} uses {data.ProviderName} for pass reservations.</p></div><Badge tone={data.Connection.StatusClass}>{data.Connection.Status}</Badge></div>
      <FormError message={data.ConnectionError} />{data.BookingConnectionNotice && <p className="notice">{data.BookingConnectionNotice}</p>}
      {!data.Connection.DefaultSourceName && <EmptyState title="Set up login codes first"><p>Choose an OTP source, then return here to connect your booking account.</p><LinkButton to="/sources">Set up OTP source</LinkButton></EmptyState>}
      {data.Connection.DefaultSourceName && !data.Profiles?.length && <EmptyState title="Connect your account"><p>Add the mobile number you use with {data.ProviderName}, then sign in to verify the connection.</p><LinkButton to={`/profiles/new?lake_id=${data.Lake.ID}`}>Add {data.ProviderName} account</LinkButton></EmptyState>}
      {!!data.Profiles?.length && <div className="grid gap-4 xl:grid-cols-2">{data.Profiles.map((profile, index) => <Resource key={index} resource={profile} />)}</div>}
      {data.Connection.DefaultSourceName && <div className="mt-6 flex flex-wrap justify-between gap-4 border-t border-border pt-5 text-sm text-muted-foreground"><p>Login codes use <strong>{data.Connection.DefaultSourceName}</strong>. <Link to="/sources" className="text-link">Manage OTP sources</Link></p>{!!data.Profiles?.length && <Link to={`/profiles/new?lake_id=${data.Lake.ID}`} className="text-link">Add another account</Link>}</div>}
    </Card><div className="section-heading"><div><h2>Booking preferences</h2><p>Your vehicle, release schedule, and pass choices for new requests.</p></div><Badge tone={data.Saved ? 'active' : ''}>{data.Saved ? 'Personal defaults' : 'Default preferences'}</Badge></div>
    <Card id="defaults"><ActionForm action={`/lakes/${data.Lake.ID}`}><FormError message={data.FormError} /><p className="field-help">New requests use these values. Existing requests and queued jobs keep their saved settings.</p><FormSections sections={data.Sections} error={!!data.FormError} /><FormFooter submit="Save lake defaults" cancel="/lakes" /></ActionForm></Card>
    {data.Saved && <Card className="mt-5"><ActionForm action={`/lakes/${data.Lake.ID}/reset`}><div className="flex flex-wrap items-center justify-between gap-4"><div><h3>Reset saved preferences</h3><p className="field-help">Restore the lake’s default preferences. Your booking account and existing jobs stay unchanged.</p></div><Button variant="outline" type="submit">Reset saved preferences</Button></div></ActionForm></Card>}
  </>
}

export function Bookings({ data }: { data: BookingsData }) {
  return <><PageHeading title="Bookings" eyebrow="Make time for the lake" description="Choose a lake for your next visit." /><div className="grid gap-6 lg:grid-cols-2">{data.Lakes.map(({ Lake: lake, Ready: ready, Problem: problem }) => <Card key={lake.ID}>
    <div className="lake-symbol"><Mountain size={26} /></div><h2 className="mt-5">{lake.Name}</h2><p className="mt-2 text-sm text-muted-foreground">{ready ? 'Choose your visit date and which passes to try.' : problem}</p>
    <Details fields={[{ Label: 'Timezone', Value: lake.Timezone }]} /><div className="mt-5"><LinkButton to={ready ? `/bookings/new?lake_id=${lake.ID}` : `/lakes/${lake.ID}`}>{ready ? 'Book a pass' : 'Finish lake setup'}</LinkButton></div>
  </Card>)}</div></>
}

export function QuickBooking({ data }: { data: QuickBookingData }) {
  return <><PageHeading title={`Book ${data.Lake.Name}`} eyebrow="New visit" description="Choose when to visit and which passes to try." />
    <Card className="max-w-3xl"><ActionForm action="/bookings/new" fields={[{ Name: 'lake_id', Value: data.Lake.ID }]}><FormError message={data.FormError} />
      <div className="form-context"><p>{data.Problem || <>Booking with <strong>{data.AccountName}</strong></>}</p><Link className="text-link" to={`/lakes/${data.Lake.ID}`}>{data.Problem ? 'Finish lake setup' : 'Manage lake connection'}</Link></div>
      <FormSections sections={[data.Visit, data.Passes]} />
      <div className="notice"><p>If passes haven’t been released, your job waits for the lake’s release schedule. Otherwise, it starts as soon as a worker is available.</p><p className="mt-2">{data.Confirmation} <Link to="/settings" className="text-link">Booking defaults</Link></p></div>
      <FormFooter submit="Book" cancel="/bookings" disabled={!!data.Problem} />
    </ActionForm></Card></>
}

export function ResourceForm({ data }: { data: FormData }) {
  return <><PageHeading title={data.Heading} eyebrow={data.Eyebrow} description={data.Description} /><Card className="max-w-3xl"><ActionForm action={data.ActionURL} fields={data.HiddenFields}>
    <FormError message={data.FormError} /><FormSections sections={data.Sections} sourceSelection={data.SourceSelection} error={!!data.FormError} />
    <FormFooter submit={data.SubmitLabel} cancel={data.CancelURL} disabled={data.SubmitDisabled} help={data.SubmitHelp} />
  </ActionForm></Card></>
}

export function ResourceList({ data }: { data: ListData }) {
  return <><PageHeading title={data.Heading} eyebrow={data.Eyebrow} description={data.Description}>{data.CreateURL && <LinkButton to={data.CreateURL}><Plus size={16} />{data.CreateLabel}</LinkButton>}</PageHeading>
    {data.Notice && <p className="notice mb-5">{data.Notice}</p>}{data.Cards?.length ? <div className="grid gap-5 xl:grid-cols-2">{data.Cards.map((resource, index) => <Resource key={resource.URL || index} resource={resource} />)}</div> : <Card><EmptyState title="No sources yet"><p>{data.EmptyMessage}</p>{data.CreateURL && <LinkButton to={data.CreateURL}>{data.CreateLabel}</LinkButton>}</EmptyState></Card>}
  </>
}
