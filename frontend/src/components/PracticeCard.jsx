import React, { useState } from 'react'

const DAY_NAMES = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']
const MONTH_NAMES = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun',
                     'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

function formatTime(iso) {
  const d = new Date(iso)
  const h = d.getHours()
  const m = d.getMinutes().toString().padStart(2, '0')
  const ampm = h >= 12 ? 'PM' : 'AM'
  return `${h % 12 || 12}:${m} ${ampm}`
}

function formatDuration(start, end) {
  const mins = Math.round((new Date(end) - new Date(start)) / 60000)
  if (mins < 60) return `${mins}m`
  const h = Math.floor(mins / 60)
  const m = mins % 60
  return m ? `${h}h ${m}m` : `${h}h`
}

export default function PracticeCard({ practice, onSignup, onCancel, disabled }) {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState(null)

  const start = new Date(practice.startTime)
  const spotsLeft = practice.capacity - practice.signupCount
  const isFull = spotsLeft <= 0
  const isPast = start < new Date()

  async function handleAction() {
    setLoading(true)
    setError(null)
    try {
      if (practice.isSignedUp) {
        await onCancel(practice.id)
      } else {
        await onSignup(practice.id)
      }
    } catch (e) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }

  const urgency = spotsLeft <= 3 && !isFull && !isPast

  return (
    <article className={`practice-card ${practice.isSignedUp ? 'signed-up' : ''} ${isPast ? 'past' : ''}`}>
      <div className="practice-date-badge">
        <span className="day-name">{DAY_NAMES[start.getDay()]}</span>
        <span className="day-num">{start.getDate()}</span>
        <span className="month">{MONTH_NAMES[start.getMonth()]}</span>
      </div>

      <div className="practice-info">
        <h3 className="practice-title">{practice.title}</h3>
        {practice.theme && (
          <span className="practice-theme">{practice.theme}</span>
        )}
        <div className="practice-meta">
          <span className="meta-item">
            <ClockIcon />
            {formatTime(practice.startTime)} · {formatDuration(practice.startTime, practice.endTime)}
          </span>
          {practice.location && (
            <span className="meta-item">
              <PinIcon />
              {practice.location}
            </span>
          )}
        </div>
      </div>

      <div className="practice-actions">
        <div className={`capacity-bar ${isFull ? 'full' : ''} ${urgency ? 'urgent' : ''}`}>
          <div
            className="capacity-fill"
            style={{ width: `${Math.min(100, (practice.signupCount / practice.capacity) * 100)}%` }}
          />
        </div>
        <span className={`spots-label ${urgency ? 'urgent' : ''} ${isFull ? 'full' : ''}`}>
          {isFull ? 'Full' : `${spotsLeft} spot${spotsLeft !== 1 ? 's' : ''} left`}
        </span>

        {!isPast && (
          <button
            className={`signup-btn ${practice.isSignedUp ? 'cancel' : ''} ${isFull && !practice.isSignedUp ? 'disabled' : ''}`}
            onClick={handleAction}
            disabled={loading || disabled || (isFull && !practice.isSignedUp)}
          >
            {loading ? <Spinner /> : practice.isSignedUp ? 'Cancel' : 'Sign Up'}
          </button>
        )}
        {isPast && <span className="past-label">Completed</span>}
        {error && <p className="card-error">{error}</p>}
      </div>
    </article>
  )
}

function ClockIcon() {
  return (
    <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/>
    </svg>
  )
}

function PinIcon() {
  return (
    <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <path d="M21 10c0 7-9 13-9 13s-9-6-9-13a9 9 0 0 1 18 0z"/><circle cx="12" cy="10" r="3"/>
    </svg>
  )
}

function Spinner() {
  return <span className="spinner" aria-label="Loading" />
}
