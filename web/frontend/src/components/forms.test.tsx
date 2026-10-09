import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { ActionForm, ActionsContext, FormSections, InputField } from './forms'
import { SettingsNav, Network, Account, User } from '@/pages/settings'
import { base, field, section } from '@/test/fixtures'

function wrapper(children: React.ReactNode, submit = vi.fn(async (_action: string, _values: URLSearchParams) => {})) {
  return render(<MemoryRouter><ActionsContext.Provider value={{ submit, csrf: 'synthetic-csrf', busy: false }}>{children}</ActionsContext.Provider></MemoryRouter>)
}

describe('React forms', () => {
  it('keeps provider drafts while excluding inactive required fields from validation and submission', async () => {
    const submit = vi.fn(async (_action: string, _values: URLSearchParams) => {})
    const sections = [section('Source', [field({ Name: 'provider', Label: 'Provider', Type: 'select', Options: [{ Value: 'bluebubbles', Label: 'BlueBubbles', Selected: false }, { Value: 'twilio', Label: 'Twilio', Selected: true }] })]), section('BlueBubbles', [field({ Name: 'bb_base_url', Label: 'Server URL', Value: 'unfinished URL', Required: true })], { Provider: 'bluebubbles' }), section('Twilio', [field({ Name: 'twilio_auth_token', Label: 'Auth token', Type: 'password', Value: 'synthetic-unsaved-token', Required: true })], { Provider: 'twilio' })]
    wrapper(<ActionForm action="/sources/new"><FormSections sections={sections} sourceSelection /><button type="submit">Save</button></ActionForm>, submit)
    expect(screen.getByLabelText('Server URL')).toBeDisabled()
    fireEvent.change(screen.getByLabelText('Provider'), { target: { value: 'bluebubbles' } })
    expect(screen.getByLabelText('Server URL')).toBeEnabled()
    expect(screen.getByLabelText('Auth token')).toBeDisabled()
    fireEvent.change(screen.getByLabelText('Provider'), { target: { value: 'twilio' } })
    expect(screen.getByLabelText('Auth token')).toHaveValue('synthetic-unsaved-token')
    fireEvent.click(screen.getByText('Save'))
    await waitFor(() => expect(submit).toHaveBeenCalledOnce())
    expect(Object.fromEntries(submit.mock.calls[0][1])).toEqual({ provider: 'twilio', twilio_auth_token: 'synthetic-unsaved-token', csrf_token: 'synthetic-csrf' })
  })

  it('preserves values, labels, descriptions, numeric bounds, and selected options', () => {
    render(<><InputField field={field({ Name: 'timeout', Label: 'Action timeout', Type: 'number', Value: '15000', Min: '1000', Max: '120000', Step: '1000', Help: 'Milliseconds' })} /><InputField field={field({ Name: 'channel', Label: 'Browser channel', Type: 'select', Options: [{ Value: '', Label: 'Chromium', Selected: false }, { Value: 'chrome', Label: 'Chrome', Selected: true }] })} /><InputField field={field({ Name: 'headless', Label: 'No visible browser', Type: 'checkbox', Checked: true, Help: 'Run in the background' })} /></>)
    expect(screen.getByLabelText('Action timeout')).toHaveValue(15000)
    expect(screen.getByLabelText('Action timeout')).toHaveAccessibleDescription('Milliseconds')
    expect(screen.getByLabelText('Action timeout')).toHaveAttribute('step', '1000')
    expect(screen.getByLabelText('Action timeout')).toHaveAttribute('min', '1000')
    expect(screen.getByLabelText('Action timeout')).toHaveAttribute('max', '120000')
    expect(screen.getByLabelText('Browser channel')).toHaveValue('chrome')
    expect(screen.getByRole('checkbox')).toBeChecked()
  })

  it('uses the Advanced flag rather than a section title and renders hostile placeholders as text', () => {
    const { container } = wrapper(<FormSections sections={[section('Provider URLs', [field({ Name: 'url', Label: 'URL', Placeholder: '"><img src=x onerror=alert(1)>' })], { Advanced: true })]} />)
    expect(screen.getByText('Advanced settings')).toBeVisible()
    expect(screen.getByLabelText('URL')).not.toBeVisible()
    expect(container.querySelector('img')).toBeNull()
    fireEvent.click(screen.getByText('Advanced settings'))
  })

  it('shows admin navigation only for administrators and respects managed network configuration', () => {
    const first = wrapper(<SettingsNav data={base} />)
    expect(screen.queryByRole('link', { name: 'Network' })).toBeNull()
    first.unmount()
    wrapper(<Network data={{ ...base, IsAdmin: true, HostCheckEnabled: true, AllowedHosts: 'lake-pass.example', CurrentHost: 'lake-pass.example', ManagedReason: 'Managed by deployment', FormError: '' }} />)
    expect(screen.getByRole('link', { name: 'Network' })).toBeVisible()
    expect(screen.getByRole('switch')).toBeChecked()
    expect(screen.getByRole('switch')).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Save network settings' })).toBeDisabled()
  })

  it('does not offer username changes before replacing a temporary password', () => {
    wrapper(<Account data={{ ...base, Error: '', FormUsername: 'member', PasswordRequired: true }} />)
    expect(screen.queryByRole('button', { name: 'Change username' })).toBeNull()
    expect(screen.getByRole('button', { name: 'Change password' })).toBeVisible()
  })

  it('requires a disabled member and a typed username before deletion', () => {
    const data = { ...base, Heading: 'Manage member', Description: '', Error: '', FormUsername: 'member', UserID: 2, Creating: false, Enabled: true, DeleteAllowed: false }
    const view = wrapper(<User data={data} />)
    expect(screen.queryByRole('button', { name: 'Delete member' })).toBeNull()
    view.unmount()
    wrapper(<User data={{ ...data, Enabled: false, DeleteAllowed: true }} />)
    fireEvent.click(screen.getByRole('button', { name: 'Delete member' }))
    expect(screen.getByLabelText('Type member to confirm')).toHaveValue('')
    expect(screen.getByLabelText('Type member to confirm')).toBeRequired()
  })
})
