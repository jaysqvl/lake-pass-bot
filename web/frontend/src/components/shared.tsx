import { ArrowUpRight, Check, Mountain, Plus } from 'lucide-react'
import { Link } from 'react-router-dom'
import type { ReactNode } from 'react'
import type { Connection, JobRow, LabelValue, ResourceCard } from '@/lib/types'
import { Button } from './ui/button'
import { Card } from './ui/card'
import { ActionForm } from './forms'

export function PageHeading({ title, eyebrow, description, children }: { title: string; eyebrow?: string; description?: string; children?: ReactNode }) {
  return <header className="page-heading"><div>{eyebrow && <p className="eyebrow">{eyebrow}</p>}<h1>{title}</h1>{description && <p>{description}</p>}</div>{children}</header>
}
export function Badge({ children, tone = '' }: { children: ReactNode; tone?: string }) { return <span className={`badge ${tone}`}>{children}</span> }
export function Details({ fields }: { fields?: LabelValue[] | null }) { return fields?.length ? <dl className="details-list">{fields.map((field, index) => <div key={`${field.Label}-${index}`}><dt>{field.Label}</dt><dd>{field.Value || '—'}</dd></div>)}</dl> : null }
export function EmptyState({ title, children }: { title: string; children: ReactNode }) { return <div className="empty-state"><div className="empty-icon"><Mountain size={24} /></div><h3>{title}</h3><div>{children}</div></div> }
export function LinkButton({ to, children, secondary = false }: { to: string; children: ReactNode; secondary?: boolean }) { return <Button variant={secondary ? 'outline' : 'default'} asChild><Link to={to}>{children}</Link></Button> }
export function ConnectionCard({ connection }: { connection: Connection }) {
  return <Card><div className="flex items-start justify-between gap-3"><div className="lake-symbol"><Mountain size={24} /></div><Badge tone={connection.StatusClass}>{connection.Status}</Badge></div>
    <h2 className="mt-5">{connection.Lake.Name}</h2><p className="mt-2 text-sm text-muted-foreground">{connection.Description}</p>
    <Details fields={connection.Fields} />
    <div className="mt-5 flex flex-wrap gap-3"><LinkButton to={connection.ActionURL} secondary>{connection.ActionLabel}<ArrowUpRight size={15} /></LinkButton>{connection.Configured && <LinkButton to={`/bookings/new?lake_id=${connection.Lake.ID}`}>Book a pass</LinkButton>}</div>
  </Card>
}
export function Resource({ resource }: { resource: ResourceCard }) {
  return <Card className="resource-card"><div className="flex flex-wrap items-start justify-between gap-3"><div><p className="eyebrow">{resource.Subtitle}</p><h3>{resource.Title}</h3></div><div className="flex gap-2">{resource.Default && <Badge tone="ok"><Check size={12} />Default</Badge>}{resource.Status && <Badge tone={resource.StatusClass}>{resource.Status}</Badge>}</div></div>
    {resource.Description && <p className="mt-3 text-sm text-muted-foreground">{resource.Description}</p>}<Details fields={resource.Fields} />
    <div className="mt-5 flex flex-wrap items-center gap-3">{resource.Actions?.map(action => <Button key={action.URL} variant={action.Class === 'primary' ? 'default' : 'outline'} asChild><Link to={action.URL}>{action.Label}</Link></Button>)}
      {resource.PostActions?.map(action => <ActionForm key={action.URL} action={action.URL} fields={action.Fields} className="resource-action">
        {action.SelectName && <div className="field"><label htmlFor={`${action.SelectName}-${action.URL}`}>{action.SelectLabel}</label><select id={`${action.SelectName}-${action.URL}`} name={action.SelectName} required defaultValue={action.SelectOptions?.find(option => option.Selected)?.Value ?? ''}>{action.SelectOptions?.map(option => <option key={option.Value} value={option.Value}>{option.Label}</option>)}</select></div>}
        <Button type="submit" variant={action.Class === 'primary' ? 'default' : 'outline'}>{action.Label}</Button>
      </ActionForm>)}
    </div>
  </Card>
}
export function JobsTable({ jobs }: { jobs: JobRow[] | null }) {
  if (!jobs?.length) return <Card><EmptyState title="No jobs yet"><p>Booking attempts and sign-in checks will appear here.</p><LinkButton to="/bookings"><Plus size={16} />Book a pass</LinkButton></EmptyState></Card>
  return <Card className="overflow-hidden p-0"><div className="table-scroll" tabIndex={0} role="region" aria-label="Job history"><table><thead><tr><th>Job</th><th>Status</th><th>Account</th><th>Earliest start</th><th>Confirmation</th></tr></thead><tbody>{jobs.map(job => <tr key={job.ID}>
    <td><Link className="table-link" to={`/jobs/${job.ID}`}>{job.Command}<ArrowUpRight size={14} /></Link><small>{job.ShortID}{job.RequestName && ` · ${job.RequestName}`}</small></td>
    <td><Badge tone={job.StatusClass}>{job.StatusLabel}</Badge></td><td>{job.ProfileName}</td><td>{job.DueLabel}</td><td>{job.ModeLabel}</td>
  </tr>)}</tbody></table></div></Card>
}
