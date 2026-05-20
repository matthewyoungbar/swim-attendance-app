const TOKEN_KEY = 'swim_token'
const USER_KEY  = 'swim_user'

export const getToken = () => localStorage.getItem(TOKEN_KEY)
export const getStoredUser = () => { try { return JSON.parse(localStorage.getItem(USER_KEY)) } catch { return null } }

export function storeAuth(token, user) {
  localStorage.setItem(TOKEN_KEY, token)
  localStorage.setItem(USER_KEY, JSON.stringify(user))
}

export function clearAuth() {
  localStorage.removeItem(TOKEN_KEY)
  localStorage.removeItem(USER_KEY)
}

// ── WebAuthn helpers ──────────────────────────────────────────────────────────

function b64urlToBuffer(b64url) {
  const b64 = b64url.replace(/-/g, '+').replace(/_/g, '/')
  const pad = (4 - b64.length % 4) % 4
  const binary = atob(b64 + '='.repeat(pad))
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i)
  return bytes.buffer
}

function bufferToB64url(buf) {
  const bytes = new Uint8Array(buf)
  let binary = ''
  for (const b of bytes) binary += String.fromCharCode(b)
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=/g, '')
}

export async function performRegistration(options, sessionId) {
  const pk = options.publicKey
  const credential = await navigator.credentials.create({
    publicKey: {
      ...pk,
      challenge: b64urlToBuffer(pk.challenge),
      user: { ...pk.user, id: b64urlToBuffer(pk.user.id) },
      excludeCredentials: (pk.excludeCredentials || []).map(c => ({ ...c, id: b64urlToBuffer(c.id) })),
    },
  })
  return {
    sessionId,
    credential: {
      id: credential.id,
      rawId: bufferToB64url(credential.rawId),
      type: credential.type,
      response: {
        clientDataJSON:    bufferToB64url(credential.response.clientDataJSON),
        attestationObject: bufferToB64url(credential.response.attestationObject),
        transports: credential.response.getTransports?.() ?? [],
      },
      clientExtensionResults: credential.getClientExtensionResults(),
    },
  }
}

export async function performLogin(options, sessionId) {
  const pk = options.publicKey
  const assertion = await navigator.credentials.get({
    publicKey: {
      ...pk,
      challenge: b64urlToBuffer(pk.challenge),
      allowCredentials: (pk.allowCredentials || []).map(c => ({ ...c, id: b64urlToBuffer(c.id) })),
    },
  })
  return {
    sessionId,
    credential: {
      id: assertion.id,
      rawId: bufferToB64url(assertion.rawId),
      type: assertion.type,
      response: {
        clientDataJSON:    bufferToB64url(assertion.response.clientDataJSON),
        authenticatorData: bufferToB64url(assertion.response.authenticatorData),
        signature:         bufferToB64url(assertion.response.signature),
        userHandle: assertion.response.userHandle ? bufferToB64url(assertion.response.userHandle) : null,
      },
      clientExtensionResults: assertion.getClientExtensionResults(),
    },
  }
}