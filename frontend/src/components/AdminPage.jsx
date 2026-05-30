import React, { useEffect, useState, useCallback, useRef } from 'react'
import './AdminPage.css'
import { api, importRoster } from '../lib/api'

export default function AdminPage({ currentUserEmail }) {
  const [users, setUsers] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [saving, setSaving] = useState({}) // email → bool
  const [query, setQuery] = useState('')
  const [importing, setImporting] = useState(false)
  const [importResult, setImportResult] = useState(null)
  const fileInputRef = useRef(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const data = await api.listUsers()
      data.sort((a, b) => (a.lastName + a.firstName).localeCompare(b.lastName + b.firstName))
      setUsers(data)
    } catch (e) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load])

  async function handleImport(e) {
    const file = e.target.files[0]
    if (!file) return
    e.target.value = ''
    setImporting(true)
    setImportResult(null)
    try {
      const result = await importRoster(file)
      setImportResult({ ok: true, ...result })
      if (result.created > 0) await load()
    } catch (err) {
      setImportResult({ ok: false, error: err.message })
    } finally {
      setImporting(false)
    }
  }

  async function handleToggle(user, field) {
    const updated = { isAdmin: user.isAdmin, isCoach: user.isCoach, isActive: user.isActive, [field]: !user[field] }
    setSaving(s => ({ ...s, [user.email]: true }))
    try {
      await api.updateUserRoles(user.email, updated.isAdmin, updated.isCoach, updated.isActive)
      setUsers(us => us.map(u => u.email === user.email ? { ...u, ...updated } : u))
    } catch (e) {
      alert(`Failed to update ${user.email}: ${e.message}`)
    } finally {
      setSaving(s => ({ ...s, [user.email]: false }))
    }
  }

  if (loading) {
    return (
      <div className="loading-state">
        <div className="loading-wave">
          {[0,1,2,3,4].map(i => <span key={i} style={{ animationDelay: `${i * 0.1}s` }} />)}
        </div>
        <p>Loading users…</p>
      </div>
    )
  }

  if (error) {
    return (
      <div className="error-state">
        <p>⚠ {error}</p>
        <button onClick={load}>Retry</button>
      </div>
    )
  }

  const q = query.trim().toLowerCase()
  const filtered = q
    ? users.filter(u =>
        `${u.firstName} ${u.lastName} ${u.preferredName ?? ''} ${u.email}`.toLowerCase().includes(q)
      )
    : users

  return (
    <div className="admin-page">
      <div className="admin-header">
        <h2 className="admin-title">User Management</h2>
        <span className="admin-count">
          {q && filtered.length !== users.length ? `${filtered.length} of ` : ''}{users.length} users
        </span>
        <input
          className="admin-search"
          type="search"
          placeholder="Search by name or email…"
          value={query}
          onChange={e => setQuery(e.target.value)}
          autoComplete="off"
        />
        <input ref={fileInputRef} type="file" accept=".xlsx" style={{ display: 'none' }} onChange={handleImport} />
        <button className="admin-import-btn" onClick={() => fileInputRef.current.click()} disabled={importing}>
          {importing ? 'Importing…' : 'Import roster'}
        </button>
      </div>

      {importResult && (
        <ImportResultModal result={importResult} onClose={() => setImportResult(null)} />
      )}

      <div className="admin-table-wrap">
        <table className="admin-table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Email</th>
              <th className="role-col">Active</th>
              <th className="role-col">Admin</th>
              <th className="role-col">Coach</th>
            </tr>
          </thead>
          <tbody>
            {filtered.length === 0 && (
              <tr>
                <td colSpan={5} className="admin-no-results">No users match "{query}"</td>
              </tr>
            )}
            {filtered.map(u => {
              const isSelf = u.email === currentUserEmail
              const busy = saving[u.email]
              const displayName = u.preferredName
                ? `${u.preferredName} ${u.lastName}`
                : `${u.firstName} ${u.lastName}`
              return (
                <tr key={u.email} className={isSelf ? 'self-row' : ''}>
                  <td>
                    <span className="user-display-name">{displayName}</span>
                    {isSelf && <span className="self-badge">you</span>}
                  </td>
                  <td className="user-email-cell">{u.email}</td>
                  <td className="role-col">
                    <RoleToggle
                      checked={u.isActive}
                      disabled={busy || isSelf}
                      title={isSelf ? "Cannot change your own active status" : undefined}
                      onChange={() => handleToggle(u, 'isActive')}
                      color="var(--success)"
                    />
                  </td>
                  <td className="role-col">
                    <RoleToggle
                      checked={u.isAdmin}
                      disabled={busy || isSelf}
                      title={isSelf ? "Cannot change your own admin status" : undefined}
                      onChange={() => handleToggle(u, 'isAdmin')}
                      color="var(--pool)"
                    />
                  </td>
                  <td className="role-col">
                    <RoleToggle
                      checked={u.isCoach}
                      disabled={busy}
                      onChange={() => handleToggle(u, 'isCoach')}
                      color="var(--success)"
                    />
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    </div>
  )
}

function ImportResultModal({ result, onClose }) {
  if (!result.ok) {
    return (
      <div className="modal-overlay" onClick={onClose}>
        <div className="modal-card" onClick={e => e.stopPropagation()}>
          <div className="modal-header">
            <h2>Import failed</h2>
            <button className="modal-close" onClick={onClose}>✕</button>
          </div>
          <p className="modal-error">{result.error}</p>
        </div>
      </div>
    )
  }

  const { created = [], deactivated = [], skipped = 0, errors = [] } = result

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal-card" onClick={e => e.stopPropagation()}>
        <div className="modal-header">
          <h2>Import complete</h2>
          <button className="modal-close" onClick={onClose}>✕</button>
        </div>

        <div className="modal-body">
          <div className="modal-section">
            <div className="modal-section-label">
              <span className={`modal-count ${created.length > 0 ? 'positive' : ''}`}>{created.length}</span>
              new members added
            </div>
            {created.length > 0 && (
              <ul className="modal-list">
                {created.map(u => (
                  <li key={u.email}><span className="modal-name">{u.name}</span><span className="modal-email">{u.email}</span></li>
                ))}
              </ul>
            )}
          </div>

          <div className="modal-section">
            <div className="modal-section-label">
              <span className={`modal-count ${deactivated.length > 0 ? 'negative' : ''}`}>{deactivated.length}</span>
              members deactivated (not in roster)
            </div>
            {deactivated.length > 0 && (
              <ul className="modal-list">
                {deactivated.map(u => (
                  <li key={u.email}><span className="modal-name">{u.name}</span><span className="modal-email">{u.email}</span></li>
                ))}
              </ul>
            )}
          </div>

          <div className="modal-section">
            <div className="modal-section-label">
              <span className="modal-count">{skipped}</span>
              already in system (no change)
            </div>
          </div>

          {errors.length > 0 && (
            <div className="modal-section modal-errors">
              <div className="modal-section-label"><span className="modal-count negative">{errors.length}</span> errors</div>
              <ul className="modal-list">
                {errors.map((e, i) => <li key={i}>{e}</li>)}
              </ul>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

function RoleToggle({ checked, disabled, onChange, color, title }) {
  return (
    <button
      className={`role-toggle ${checked ? 'on' : 'off'}`}
      onClick={onChange}
      disabled={disabled}
      title={title}
      style={checked ? { background: color, borderColor: color } : undefined}
      aria-pressed={checked}
    >
      <span className="role-toggle-knob" />
    </button>
  )
}
