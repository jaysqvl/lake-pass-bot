import { createContext, useContext, useState, type FormEvent, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import type { Field, HiddenField, Section } from '@/lib/types'
import { Button } from './ui/button'

export interface AppActions { submit: (action: string, values: URLSearchParams) => Promise<void>; busy: boolean; csrf: string }
export const ActionsContext = createContext<AppActions | null>(null)
export function useActions() {
  const context = useContext(ActionsContext)
  if (!context) throw new Error('Application actions are unavailable')
  return context
}

export function ActionForm({ action, children, fields, className, disabled = false }: { action: string; children: ReactNode; fields?: HiddenField[] | null; className?: string; disabled?: boolean }) {
  const { submit, busy, csrf } = useActions()
  function send(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (busy || disabled) return
    const values = new URLSearchParams()
    for (const [key, value] of new FormData(event.currentTarget)) if (typeof value === 'string') values.append(key, value)
    values.set('csrf_token', csrf)
    void submit(action, values)
  }
  return <form method="post" onSubmit={send} className={className} aria-busy={busy}>
    {fields?.map(field => <input key={field.Name} type="hidden" name={field.Name} value={field.Value} />)}
    <fieldset disabled={busy || disabled} className="min-w-0 space-y-5">{children}</fieldset>
  </form>
}

export function InputField({ field }: { field: Field }) {
  const id = `field-${field.Name}`
  const helpID = field.Help ? `help-${field.Name}` : undefined
  const common = { id, name: field.Name, required: field.Required, 'aria-describedby': helpID }
  if (field.Type === 'checkbox') return <label htmlFor={id} className={`check-field ${field.Wide ? 'md:col-span-2' : ''}`}>
    <input {...common} type="checkbox" value="1" defaultChecked={field.Checked} />
    <span>{field.Label}{field.Help && <small id={helpID}>{field.Help}</small>}</span>
  </label>
  return <div className={`field ${field.Wide ? 'md:col-span-2' : ''}`}>
    <label htmlFor={id}>{field.Label}</label>
    {field.Type === 'select'
      ? <select {...common} defaultValue={field.Options?.find(option => option.Selected)?.Value ?? ''}>
        {field.Options?.map(option => <option key={option.Value} value={option.Value}>{option.Label}</option>)}
      </select>
      : <input {...common} type={field.Type} defaultValue={field.Value} placeholder={field.Placeholder || undefined} step={field.Step || undefined} min={field.Min || undefined} max={field.Max || undefined} autoComplete={field.Type === 'password' ? 'new-password' : undefined} />}
    {field.Help && <small id={helpID}>{field.Help}</small>}
  </div>
}

export function FormSections({ sections, sourceSelection = false, error = false }: { sections: Section[]; sourceSelection?: boolean; error?: boolean }) {
  const selected = sections.flatMap(section => section.Fields ?? []).find(field => field.Name === 'provider')?.Options?.find(option => option.Selected)?.Value ?? 'bluebubbles'
  const [provider, setProvider] = useState(selected)
  const section = (item: Section) => <fieldset key={item.Title} className="form-section" hidden={!!item.Provider && item.Provider !== provider} disabled={!!item.Provider && item.Provider !== provider}>
    <legend>{item.Title}</legend>
    {(item.Help || item.HelpURL) && <p className="field-help">{item.Help} {item.HelpURL && <Link to={item.HelpURL}>{item.HelpLabel}</Link>}</p>}
    <div className="grid gap-5 md:grid-cols-2">{item.Fields?.map(field => <InputField key={field.Name} field={field} />)}</div>
  </fieldset>
  return <div onChange={event => {
    const control = event.target
    if (sourceSelection && control instanceof HTMLSelectElement && control.name === 'provider') setProvider(control.value)
  }} className="space-y-7">
    {sections.filter(item => !item.Advanced).map(section)}
    {sections.some(item => item.Advanced) && <details open={error || undefined} className="advanced-settings"><summary>Advanced settings</summary><div className="space-y-7 pt-5">{sections.filter(item => item.Advanced).map(section)}</div></details>}
  </div>
}

export function FormError({ message }: { message?: string }) {
  return message ? <div id="form-error" role="alert" tabIndex={-1} className="notice error">{message}</div> : null
}

export function FormFooter({ submit, cancel, disabled, help }: { submit: string; cancel?: string; disabled?: boolean; help?: string }) {
  return <div className="form-footer">{help && <p className="field-help">{help}</p>}<div className="flex flex-wrap gap-3">
    <Button type="submit" disabled={disabled}>{submit}</Button>
    {cancel && <Button variant="ghost" asChild><Link to={cancel}>Cancel</Link></Button>}
  </div></div>
}
