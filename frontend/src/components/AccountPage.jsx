import React, { useEffect, useState, useCallback } from 'react'
import { useNavigate } from 'react-router'
import { api } from '../lib/api'
import { performRegistration } from '../lib/auth'

function transportLabel(transports) {
  if (!transports || transports.length === 0) return 'Passkey'
  const has = t => transports.includes(t)
  if (has('internal') && has('hybrid')) return 'Synced passkey'
  if (has('internal')) return 'This device'
  if (has('hybrid')) return 'Cross-device'
  if (has('usb')) return 'USB security key'
  if (has('nfc')) return 'NFC security key'
  return 'Passkey'
}

function formatDate(iso) {
  return new Date(iso).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })
}

export default function AccountPage({ user, onLogout }) {
  const navigate = useNavigate()
  const [passkeys, setPasskeys] = useState([])
  const [loading, setLoading] = useState(true)
  const [adding, setAdding] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(null)
  const [error, setError] = useState(null)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      setPasskeys(await api.listPasskeys())
    } catch (e) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load])

  async function handleAddPasskey() {
    setAdding(true)
    setError(null)
    try {
      const { sessionId, options } = await api.addPasskeyBegin()
      const body = await performRegistration(options, sessionId)
      await api.addPasskeyComplete(body)
      await load()
    } catch (e) {
      setError(e.message)
    } finally {
      setAdding(false)
    }
  }

  async function handleDelete(credId) {
    try {
      await api.deletePasskey(credId)
      setPasskeys(ps => ps.filter(p => p.id !== credId))
      setConfirmDelete(null)
    } catch (e) {
      setError(e.message)
      setConfirmDelete(null)
    }
  }

  const displayName = user.preferredName
    ? `${user.preferredName} ${user.lastName}`
    : `${user.firstName} ${user.lastName}`

  return (
    <div className="account-page">
      <button className="account-back" onClick={() => navigate('/')}>← Practices</button>
      <div className="account-section">
        <h2 className="account-section-title">Your info</h2>
        <div className="account-info-grid">
          <span className="account-info-label">Name</span>
          <span className="account-info-value">{displayName}</span>
          <span className="account-info-label">Email</span>
          <span className="account-info-value">{user.email}</span>
          {user.phone && <>
            <span className="account-info-label">Phone</span>
            <span className="account-info-value">{user.phone}</span>
          </>}
        </div>
      </div>

      <div className="account-section">
        <h2 className="account-section-title">Passkeys</h2>
        {loading ? (
          <p className="account-muted">Loading…</p>
        ) : (
          <div className="passkey-list">
            {passkeys.map(pk => (
              <div key={pk.id} className="passkey-row">
                <KeyIcon />
                <div className="passkey-info">
                  <span className="passkey-label">{transportLabel(pk.transport)}</span>
                  <span className="passkey-date">Added {formatDate(pk.createdAt)}</span>
                </div>
                {confirmDelete === pk.id ? (
                  <div className="passkey-confirm">
                    <span>Remove?</span>
                    <button className="passkey-confirm-yes" onClick={() => handleDelete(pk.id)}>Yes</button>
                    <button className="passkey-confirm-no" onClick={() => setConfirmDelete(null)}>Cancel</button>
                  </div>
                ) : (
                  <button
                    className="passkey-delete"
                    onClick={() => setConfirmDelete(pk.id)}
                    disabled={passkeys.length <= 1}
                    title={passkeys.length <= 1 ? 'Cannot remove your only passkey' : 'Remove passkey'}
                  >
                    Remove
                  </button>
                )}
              </div>
            ))}
          </div>
        )}
        {error && <p className="account-error">{error}</p>}
        <button className="account-add-btn" onClick={handleAddPasskey} disabled={adding}>
          {adding ? 'Follow the passkey prompt…' : '+ Add passkey'}
        </button>
      </div>

      <button className="account-signout" onClick={onLogout}>Sign out</button>
    </div>
  )
}

function KeyIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <circle cx="7.5" cy="15.5" r="5.5"/>
      <path d="M21 2l-9.6 9.6"/>
      <path d="M15.5 7.5l3 3L22 7l-3-3"/>
    </svg>
  )
}