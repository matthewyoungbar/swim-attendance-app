import { getToken } from './auth.js'

const BASE_URL = import.meta.env.DEV ? '' : '/api'

export function importRoster(file) {
  const form = new FormData()
  form.append('file', file)
  const token = getToken()
  return fetch(`${BASE_URL}/admin/import-roster`, {
    method: 'POST',
    headers: token ? { Authorization: `Bearer ${token}` } : {},
    body: form,
  }).then(r => r.json()).then(json => {
    if (!json.success) throw new Error(json.error || 'Upload failed')
    return json.data
  })
}

async function request(method, path, body, extraHeaders = {}) {
  const token = getToken()
  const headers = {
    'Content-Type': 'application/json',
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
    ...extraHeaders,
  }
  const opts = { method, headers }
  if (body) opts.body = JSON.stringify(body)

  const res = await fetch(`${BASE_URL}${path}`, opts)
  const json = await res.json()
  if (!json.success) {
    const err = new Error(json.error || 'Request failed')
    err.status = res.status
    throw err
  }
  return json.data
}

export const api = {
  getPractices: (email) =>
    request('GET', `/practices${email ? `?email=${encodeURIComponent(email)}` : ''}`),

  syncPractices: () => request('POST', '/practices/sync'),

  getPracticeSignups: (practiceId) =>
    request('GET', `/practices/${practiceId}/signups`),

  createSignup: (practiceId, notes = '') =>
    request('POST', '/signups', { practiceId, notes }),

  deleteSignup: (practiceId) =>
    request('DELETE', `/signups/${practiceId}`),

  getMySignups: () =>
    request('GET', '/my-signups'),

  listUsers: () =>
    request('GET', '/users'),

  updateUserRoles: (email, isAdmin, isCoach, isActive) =>
    request('PUT', `/users/${encodeURIComponent(email)}/roles`, { isAdmin, isCoach, isActive }),

  listSwimmers: () => request('GET', '/swimmers'),
  getAllPractices: () => request('GET', '/practices/all'),
  getAttendance: (practiceId) => request('GET', `/attendance/${practiceId}`),
  saveAttendance: (practiceId, data) => request('PUT', `/attendance/${practiceId}`, data),

  listPasskeys: () => request('GET', '/passkeys'),
  addPasskeyBegin: () => request('POST', '/passkeys/add/begin'),
  addPasskeyComplete: (data) => request('POST', '/passkeys/add/complete', data),
  deletePasskey: (credId) => request('DELETE', `/passkeys/${credId}`),

  auth: {
    check:            (email)  => request('GET', `/auth/check?email=${encodeURIComponent(email)}`),
    me:               ()       => request('GET', '/auth/me'),
    registerBegin:    (data)   => request('POST', '/auth/register/begin', data),
    registerComplete: (data)   => request('POST', '/auth/register/complete', data),
    loginBegin:       ()       => request('POST', '/auth/login/begin'),
    loginComplete:    (data)   => request('POST', '/auth/login/complete', data),
  },
}
