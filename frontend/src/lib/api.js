// lib/api.js — typed API client for the swim signup backend

const BASE_URL = import.meta.env.VITE_API_URL ?? ''

async function request(method, path, body, headers = {}) {
  const opts = {
    method,
    headers: { 'Content-Type': 'application/json', ...headers },
  }
  if (body) opts.body = JSON.stringify(body)

  const res = await fetch(`${BASE_URL}${path}`, opts)
  const json = await res.json()
  if (!json.success) throw new Error(json.error || 'Request failed')
  return json.data
}

export const api = {
  /** Fetch upcoming practices, optionally with signup status for an email */
  getPractices: (email) =>
    request('GET', `/practices${email ? `?email=${encodeURIComponent(email)}` : ''}`),

  /** Admin: sync practices from Google Calendar */
  syncPractices: () => request('POST', '/practices/sync'),

  /** Get all signups for a practice (admin) */
  getPracticeSignups: (practiceId) =>
    request('GET', `/practices/${practiceId}/signups`),

  /** Sign up for a practice */
  createSignup: (practiceId, swimmerEmail, swimmerName, notes = '') =>
    request('POST', '/signups', { practiceId, swimmerEmail, swimmerName, notes }),

  /** Cancel signup */
  deleteSignup: (practiceId, swimmerEmail) =>
    request('DELETE', `/signups/${practiceId}`, null, {
      'X-Swimmer-Email': swimmerEmail,
    }),

  /** Get all practices a swimmer is signed up for */
  getMySignups: (email) =>
    request('GET', `/my-signups?email=${encodeURIComponent(email)}`),
}
