import React, { useState } from 'react'
import { api } from '../lib/api'
import { performRegistration, performLogin, storeAuth } from '../lib/auth'

const STEPS = { EMAIL: 'email', REGISTER: 'register', LOADING: 'loading' }

export default function AuthFlow({ onAuth }) {
  const [step, setStep]   = useState(STEPS.EMAIL)
  const [email, setEmail] = useState('')
  const [form, setForm]   = useState({ firstName: '', lastName: '', preferredName: '', phone: '' })
  const [error, setError] = useState(null)

  function field(key) {
    return { value: form[key], onChange: e => setForm(f => ({ ...f, [key]: e.target.value })) }
  }

  async function handleEmailSubmit(e) {
    e.preventDefault()
    const trimmed = email.trim().toLowerCase()
    if (!trimmed.includes('@')) { setError('Enter a valid email.'); return }
    setEmail(trimmed)
    setError(null)
    setStep(STEPS.LOADING)
    try {
      const { exists } = await api.auth.check(trimmed)
      if (exists) {
        await doLogin(trimmed)
      } else {
        setStep(STEPS.REGISTER)
      }
    } catch (err) {
      setError(err.message)
      setStep(STEPS.EMAIL)
    }
  }

  async function doLogin(loginEmail) {
    setStep(STEPS.LOADING)
    try {
      const { sessionId, options } = await api.auth.loginBegin({ email: loginEmail })
      const body = await performLogin(options, sessionId)
      const { token, user } = await api.auth.loginComplete(body)
      storeAuth(token, user)
      onAuth(user)
    } catch (err) {
      setError(err.message)
      setStep(STEPS.EMAIL)
    }
  }

  async function handleRegisterSubmit(e) {
    e.preventDefault()
    if (!form.firstName.trim() || !form.lastName.trim()) {
      setError('First and last name are required.')
      return
    }
    setError(null)
    setStep(STEPS.LOADING)
    try {
      const { sessionId, options } = await api.auth.registerBegin({ email, ...form })
      const body = await performRegistration(options, sessionId)
      const { token, user } = await api.auth.registerComplete(body)
      storeAuth(token, user)
      onAuth(user)
    } catch (err) {
      setError(err.message)
      setStep(STEPS.REGISTER)
    }
  }

  if (step === STEPS.LOADING) {
    return (
      <div className="login-card">
        <p style={{ textAlign: 'center', color: '#64748b' }}>
          Follow the passkey prompt…
        </p>
      </div>
    )
  }

  if (step === STEPS.REGISTER) {
    return (
      <div className="login-card">
        <h1>Create your account</h1>
        <p className="login-subtitle">{email}</p>
        <form onSubmit={handleRegisterSubmit} className="login-form">
          <div className="field">
            <label>First name</label>
            <input type="text" placeholder="Alex" autoFocus {...field('firstName')} />
          </div>
          <div className="field">
            <label>Last name</label>
            <input type="text" placeholder="Smith" {...field('lastName')} />
          </div>
          <div className="field">
            <label>Preferred name <span style={{ color: '#94a3b8' }}>(optional)</span></label>
            <input type="text" placeholder="Alex" {...field('preferredName')} />
          </div>
          <div className="field">
            <label>Phone <span style={{ color: '#94a3b8' }}>(optional)</span></label>
            <input type="tel" placeholder="+1 555 000 0000" {...field('phone')} />
          </div>
          {error && <p className="login-error">{error}</p>}
          <button type="submit" className="login-btn">Create passkey →</button>
          <button type="button" className="login-btn" style={{ background: 'none', color: '#64748b', marginTop: 4 }}
            onClick={() => { setStep(STEPS.EMAIL); setError(null) }}>
            ← Back
          </button>
        </form>
      </div>
    )
  }

  return (
    <div className="login-card">
      <h1>Welcome back</h1>
      <p className="login-subtitle">Sign in with your passkey</p>
      <form onSubmit={handleEmailSubmit} className="login-form">
        <div className="field">
          <label htmlFor="email">Email address</label>
          <input
            id="email"
            type="email"
            placeholder="alex@example.com"
            value={email}
            onChange={e => setEmail(e.target.value)}
            autoFocus
          />
        </div>
        {error && <p className="login-error">{error}</p>}
        <button type="submit" className="login-btn">Continue →</button>
      </form>
    </div>
  )
}