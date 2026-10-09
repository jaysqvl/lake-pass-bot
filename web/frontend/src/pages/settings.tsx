import { useState } from 'react'
import { Link, NavLink } from 'react-router-dom'
import type { AccountData, Base, NetworkData, SettingsData, UserData, UsersData } from '@/lib/types'
import { ActionForm, FormError, FormFooter, FormSections } from '@/components/forms'
import { Badge, LinkButton, PageHeading } from '@/components/shared'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'

export function SettingsNav({ data }: { data: Base }) {
  return <nav className="settings-nav" aria-label="Settings"><NavLink to="/settings" end>Booking defaults</NavLink><NavLink to="/account">Account</NavLink>{data.IsAdmin && <><NavLink to="/settings/network">Network</NavLink><NavLink to="/admin/users">Users</NavLink></>}</nav>
}
export function Settings({ data }: { data: SettingsData }) {
  return <><PageHeading title="Settings" description="Defaults for booking confirmation, preparation, and browser behavior." /><SettingsNav data={data} /><Card className="max-w-4xl"><ActionForm action="/settings"><FormError message={data.FormError} /><FormSections sections={data.Sections} /><FormFooter submit="Save settings" /></ActionForm></Card></>
}

export function Account({ data }: { data: AccountData }) {
  return <><PageHeading title="Account" description={`Manage the account for ${data.Username}.`} /><SettingsNav data={data} /><FormError message={data.Error} />
    {data.PasswordRequired && <p className="notice warn mb-5">Change your temporary password before continuing.</p>}
    <div className="grid gap-6 xl:grid-cols-2">{!data.PasswordRequired && <Card><h2>Change username</h2><p className="field-help mb-5">Use your current password to confirm this change.</p><ActionForm action="/account/username"><div className="field"><label htmlFor="account-username">Username</label><input id="account-username" name="username" defaultValue={data.FormUsername} required autoComplete="username" /></div><Password id="username-password" name="current_password" label="Current password" current /><FormFooter submit="Change username" /></ActionForm></Card>}
      <Card><h2>Change password</h2><p className="field-help mb-5">Changing your password signs out all existing sessions.</p><ActionForm action="/account/password"><Password id="current-password" name="current_password" label="Current password" current /><Password id="new-password" name="new_password" label="New password" /><Password id="confirm-password" name="password_confirm" label="Confirm new password" /><FormFooter submit="Change password" /></ActionForm></Card>
    </div></>
}

export function Network({ data }: { data: NetworkData }) {
  return <><PageHeading title="Network" eyebrow="Settings" description="Manage the addresses used to reach this Lake Pass Bot server." /><SettingsNav data={data} /><Card className="max-w-3xl"><FormError message={data.FormError} />{data.ManagedReason && <p role="note" className="notice mb-5">{data.ManagedReason}</p>}
    <ActionForm action="/settings/network" disabled={!!data.ManagedReason}><h2>Hostname checks</h2><p className="field-help">Choose whether to restrict the hostnames this server accepts.</p>
      <label className="check-field" htmlFor="host-check-enabled"><input id="host-check-enabled" name="host_check_enabled" type="checkbox" role="switch" value="on" defaultChecked={data.HostCheckEnabled} aria-describedby="host-check-help" /><span>Enable hostname checks<small id="host-check-help">Off by default. When enabled, only allowed hostnames can reach the app.</small></span></label>
      <div className="field"><label htmlFor="allowed-hosts">Allowed hostnames</label><textarea id="allowed-hosts" name="allowed_hosts" rows={5} defaultValue={data.AllowedHosts} spellCheck={false} autoCapitalize="none" aria-describedby="allowed-hosts-help" /><small id="allowed-hosts-help">Enter one hostname per line, including the port if you use one. Leave out http://, https://, and paths. Localhost remains available.</small></div>
      <p className="notice">Current address: <code>{data.CurrentHost}</code>. Keep this address in the list when enabling checks so you can still access the app.</p><FormFooter submit="Save network settings" help="Changes apply to all users immediately after saving." />
    </ActionForm></Card></>
}

export function Users({ data }: { data: UsersData }) {
  return <><PageHeading title="Users" description="Manage member access to Lake Pass Bot."><LinkButton to="/admin/users/new">New user</LinkButton></PageHeading><SettingsNav data={data} />
    <Card className="p-0 overflow-hidden"><div className="table-scroll"><table><thead><tr><th>Username</th><th>Role</th><th>Status</th><th>Password</th><th>Created</th></tr></thead><tbody>{data.Users?.map(user => <tr key={user.ID}><td>{user.Editable ? <Link className="text-link" to={`/admin/users/${user.ID}`}>{user.Username}</Link> : user.Username}</td><td>{user.Role}</td><td><Badge tone={user.StatusClass}>{user.Status}</Badge></td><td>{user.PasswordState}</td><td>{user.CreatedLabel}</td></tr>)}</tbody></table></div></Card></>
}

export function User({ data }: { data: UserData }) {
  const [deleting, setDeleting] = useState(false)
  return <><PageHeading title={data.Heading} description={data.Description} /><SettingsNav data={data} /><FormError message={data.Error} /><div className="grid gap-6 xl:grid-cols-2"><Card><ActionForm action={data.Creating ? '/admin/users/new' : `/admin/users/${data.UserID}`}>
    <div className="field"><label htmlFor="member-username">Username</label><input id="member-username" name="username" defaultValue={data.FormUsername} autoComplete="off" required /></div>
    {data.Creating ? <><Password id="member-password" name="password" label="Temporary password" /><Password id="member-password-confirm" name="password_confirm" label="Confirm temporary password" /></> : <label className="check-field"><input name="enabled" type="checkbox" value="1" defaultChecked={data.Enabled} /><span>Account enabled</span></label>}
    <FormFooter submit={data.Creating ? 'Create user' : 'Save user'} cancel="/admin/users" /></ActionForm></Card>
    {!data.Creating && <Card><h2>Reset password</h2><p className="field-help mb-5">Existing sessions are revoked. The member must change this temporary password when signing in.</p><ActionForm action={`/admin/users/${data.UserID}/password`}><Password id="reset-password" name="password" label="Temporary password" /><Password id="reset-password-confirm" name="password_confirm" label="Confirm temporary password" /><FormFooter submit="Reset password" /></ActionForm></Card>}
  </div>{!data.Creating && data.DeleteAllowed && <Card className="mt-6 border-destructive/30"><h2>Delete member</h2><p className="field-help mt-2">Delete this member’s account, jobs, reservations, and managed local files. This cannot be undone.</p>
    {deleting ? <ActionForm action={`/admin/users/${data.UserID}/delete`}><div className="field mt-4"><label htmlFor="member-delete-confirm">Type {data.FormUsername} to confirm</label><input id="member-delete-confirm" name="confirm_username" required autoComplete="off" spellCheck={false} /></div><div className="flex gap-3"><Button type="submit" variant="destructive">Delete member and data</Button><Button type="button" variant="outline" onClick={() => setDeleting(false)}>Keep member</Button></div></ActionForm> : <Button type="button" variant="destructive" className="mt-4" onClick={() => setDeleting(true)}>Delete member</Button>}
  </Card>}</>
}

export function Password({ id, name, label, current = false }: { id: string; name: string; label: string; current?: boolean }) {
  return <div className="field"><label htmlFor={id}>{label}</label><input id={id} name={name} type="password" required autoComplete={current ? 'current-password' : 'new-password'} minLength={current ? undefined : 12} /></div>
}
