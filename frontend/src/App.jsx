import React, { useState, useCallback, useEffect } from 'react'
import { Routes, Route, useNavigate, Navigate, Link } from 'react-router'
import PracticeCard from './components/PracticeCard'
import AuthFlow from './components/AuthFlow'
import AdminPage from './components/AdminPage'
import AccountPage from './components/AccountPage'
import { usePractices } from './hooks/usePractices'
import { api } from './lib/api'
import { getStoredUser, storeAuth, getToken, clearAuth } from './lib/auth'

export default function App() {
  const [user, setUser] = useState(getStoredUser)
  const navigate = useNavigate()

  // Refresh user profile from the server on every load so role changes
  // (e.g. isAdmin set in DynamoDB) are picked up without requiring logout.
  useEffect(() => {
    if (!getToken()) return
    api.auth.me().then(freshUser => {
      setUser(freshUser)
      storeAuth(getToken(), freshUser)
    }).catch(err => {
      if (err.status === 401) {
        clearAuth()
        setUser(null)
      }
    })
  }, [])

  const handleAuth = useCallback((newUser) => {
    setUser(newUser)
  }, [])

  function handleLogout() {
    setUser(null)
    clearAuth()
    navigate('/')
  }

  if (!user) {
    return (
      <div className="app">
        <header className="app-header">
          <div className="header-logo" onClick={() => navigate('/')} style={{ cursor: 'pointer' }}>
            <WaveIcon />
            <span>SwimSignup</span>
          </div>
        </header>
        <main className="login-page">
          <AuthFlow onAuth={handleAuth} />
        </main>
      </div>
    )
  }

  return (
    <div className="app">
      <header className="app-header">
        <Link to="/" className="header-logo">
          <WaveIcon />
          <span>SwimSignup</span>
        </Link>
        <div className="header-right">
          <SyncButton />
          <div className="user-pill" onClick={() => navigate('/account')} title="Manage your account">
            <span className="user-avatar">{(user.preferredName || user.firstName || '?')[0].toUpperCase()}</span>
            <span className="user-name">{user.preferredName || user.firstName} {user.lastName}</span>
          </div>
        </div>
      </header>

      <main className="main-content">
        <Routes>
          <Route path="/account" element={<AccountPage user={user} onLogout={handleLogout} />} />
          <Route path="*" element={<PracticesView user={user} />} />
        </Routes>
      </main>
    </div>
  )
}

function SyncButton() {
  const [syncing, setSyncing] = useState(false)
  const [syncMsg, setSyncMsg] = useState(null)

  async function handleSync() {
    setSyncing(true)
    setSyncMsg(null)
    try {
      const result = await api.syncPractices()
      setSyncMsg(`Synced ${result.synced} practices from calendar.`)
    } catch (e) {
      setSyncMsg(`Sync failed: ${e.message}`)
    } finally {
      setSyncing(false)
    }
  }

  return (
    <>
      <button className="sync-btn" onClick={handleSync} disabled={syncing}>
        {syncing ? '⟳ Syncing…' : '⟳ Sync Calendar'}
      </button>
      {syncMsg && (
        <div className={`sync-banner ${syncMsg.startsWith('Sync failed') ? 'error' : 'success'}`}
          style={{ position: 'fixed', top: 60, left: 0, right: 0, zIndex: 99 }}>
          {syncMsg}
          <button onClick={() => setSyncMsg(null)}>✕</button>
        </div>
      )}
    </>
  )
}

function PracticesView({ user }) {
  const [tab, setTab] = useState('upcoming')
  const { practices, loading, error, reload } = usePractices()

  const handleSignup = useCallback(async (practiceId) => {
    await api.createSignup(practiceId)
    await reload()
  }, [reload])

  const handleCancel = useCallback(async (practiceId) => {
    await api.deleteSignup(practiceId)
    await reload()
  }, [reload])

  const myPractices = practices.filter(p => p.isSignedUp)
  const displayedPractices = tab === 'mine' ? myPractices : practices

  return (
    <>
      <div className="tabs">
        <button className={`tab ${tab === 'upcoming' ? 'active' : ''}`} onClick={() => setTab('upcoming')}>
          All Practices
          <span className="tab-count">{practices.length}</span>
        </button>
        <button className={`tab ${tab === 'mine' ? 'active' : ''}`} onClick={() => setTab('mine')}>
          My Signups
          <span className="tab-count">{myPractices.length}</span>
        </button>
        {user.isAdmin && (
          <button className={`tab ${tab === 'admin' ? 'active' : ''}`} onClick={() => setTab('admin')}>
            Admin
          </button>
        )}
      </div>

      {tab === 'admin' ? (
        <AdminPage currentUserEmail={user.email} />
      ) : (
        <>
          {!user.isActive && (
            <div className="sync-banner error" style={{ justifyContent: 'center' }}>
              Your account is not currently active. Contact an admin to sign up for practices.
            </div>
          )}

          {loading && (
            <div className="loading-state">
              <div className="loading-wave">
                {[0,1,2,3,4].map(i => <span key={i} style={{ animationDelay: `${i * 0.1}s` }} />)}
              </div>
              <p>Loading practices…</p>
            </div>
          )}

          {error && !loading && (
            <div className="error-state">
              <p>⚠ {error}</p>
              <button onClick={reload}>Retry</button>
            </div>
          )}

          {!loading && !error && displayedPractices.length === 0 && (
            <div className="empty-state">
              {tab === 'mine'
                ? <p>You haven't signed up for any practices yet.</p>
                : <p>No upcoming practices. Try syncing the calendar.</p>
              }
            </div>
          )}

          <div className="practice-list">
            {displayedPractices.map(p => (
              <PracticeCard
                key={p.id}
                practice={p}
                onSignup={handleSignup}
                onCancel={handleCancel}
                disabled={!user.isActive}
              />
            ))}
          </div>
        </>
      )}
    </>
  )
}

function WaveIcon({ size = 24 }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
      <path d="M2 12c1.5-2 3-2 4.5 0s3 2 4.5 0 3-2 4.5 0 3 2 4.5 0"/>
      <path d="M2 17c1.5-2 3-2 4.5 0s3 2 4.5 0 3-2 4.5 0 3 2 4.5 0"/>
      <path d="M2 7c1.5-2 3-2 4.5 0s3 2 4.5 0 3-2 4.5 0 3 2 4.5 0"/>
    </svg>
  )
}
