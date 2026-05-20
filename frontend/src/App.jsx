import React, { useState, useCallback } from 'react'
import PracticeCard from './components/PracticeCard'
import AuthFlow from './components/AuthFlow'
import { usePractices } from './hooks/usePractices'
import { api } from './lib/api'
import { getStoredUser, clearAuth } from './lib/auth'

export default function App() {
  const [user, setUser] = useState(getStoredUser)
  const [syncing, setSyncing] = useState(false)
  const [syncMsg, setSyncMsg] = useState(null)
  const [tab, setTab] = useState('upcoming') // 'upcoming' | 'mine'

  const { practices, loading, error, reload } = usePractices()

  function handleLogout() {
    setUser(null)
    clearAuth()
  }

  const handleSignup = useCallback(async (practiceId) => {
    await api.createSignup(practiceId)
    await reload()
  }, [reload])

  const handleCancel = useCallback(async (practiceId) => {
    await api.deleteSignup(practiceId)
    await reload()
  }, [reload])

  async function handleSync() {
    setSyncing(true)
    setSyncMsg(null)
    try {
      const result = await api.syncPractices()
      setSyncMsg(`Synced ${result.synced} practices from calendar.`)
      await reload()
    } catch (e) {
      setSyncMsg(`Sync failed: ${e.message}`)
    } finally {
      setSyncing(false)
    }
  }

  const myPractices = practices.filter(p => p.isSignedUp)
  const displayedPractices = tab === 'mine' ? myPractices : practices

  // ─── Not logged in ────────────────────────────────────────────────────────

  if (!user) {
    return (
      <div className="app">
        <header className="app-header">
          <div className="header-logo">
            <WaveIcon />
            <span>SwimSignup</span>
          </div>
        </header>
        <main className="login-page">
          <AuthFlow onAuth={setUser} />
        </main>
      </div>
    )
  }

  // ─── Logged in ────────────────────────────────────────────────────────────

  return (
    <div className="app">
      <header className="app-header">
        <div className="header-logo">
          <WaveIcon />
          <span>SwimSignup</span>
        </div>
        <div className="header-right">
          <button className="sync-btn" onClick={handleSync} disabled={syncing}>
            {syncing ? '⟳ Syncing…' : '⟳ Sync Calendar'}
          </button>
          <div className="user-pill" onClick={handleLogout} title="Click to sign out">
            <span className="user-avatar">{(user.preferredName || user.firstName || '?')[0].toUpperCase()}</span>
            <span className="user-name">{user.preferredName || user.firstName} {user.lastName}</span>
          </div>
        </div>
      </header>

      {syncMsg && (
        <div className={`sync-banner ${syncMsg.startsWith('Sync failed') ? 'error' : 'success'}`}>
          {syncMsg}
          <button onClick={() => setSyncMsg(null)}>✕</button>
        </div>
      )}

      <main className="main-content">
        <div className="tabs">
          <button
            className={`tab ${tab === 'upcoming' ? 'active' : ''}`}
            onClick={() => setTab('upcoming')}
          >
            All Practices
            <span className="tab-count">{practices.length}</span>
          </button>
          <button
            className={`tab ${tab === 'mine' ? 'active' : ''}`}
            onClick={() => setTab('mine')}
          >
            My Signups
            <span className="tab-count">{myPractices.length}</span>
          </button>
        </div>

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
            />
          ))}
        </div>
      </main>
    </div>
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
