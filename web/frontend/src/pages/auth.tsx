import { Mountain } from 'lucide-react'
import type { AuthData } from '@/lib/types'
import { ActionForm, FormError, FormFooter } from '@/components/forms'
import { Card } from '@/components/ui/card'
import { Password } from './settings'

export function Auth({ data, setup = false }: { data: AuthData; setup?: boolean }) {
  return <div className="auth-layout"><div className="auth-intro"><div className="lake-symbol"><Mountain size={28} /></div><p className="eyebrow mt-5">Lake Pass Bot</p><h1>A little less planning.<br />A little more lake.</h1><p>Connect your accounts, choose a date, and follow your reservation from one place.</p></div>
    <Card className="auth-card"><h2>{setup ? 'First-run setup' : 'Welcome back'}</h2><p className="field-help mt-2 mb-6">{setup ? 'Create the administrator account for this server.' : 'Sign in to plan your next visit.'}</p><ActionForm action={setup ? '/setup' : '/login'}>
      <FormError message={data.Error} />{data.Message && <p role="status" className="notice ok">{data.Message}</p>}
      {setup && <div className="field"><label htmlFor="setup-token">One-time setup token</label><input id="setup-token" name="setup_token" type="password" autoComplete="off" required /><small>Find the token in your server’s startup output.</small></div>}
      <div className="field"><label htmlFor="username">Username</label><input id="username" name="username" autoComplete="username" required defaultValue={data.Username} /></div>
      <Password id="password" name="password" label="Password" current={!setup} />{setup && <Password id="password-confirm" name="password_confirm" label="Confirm password" />}
      <FormFooter submit={setup ? 'Create administrator' : 'Sign in'} />
    </ActionForm></Card></div>
}
