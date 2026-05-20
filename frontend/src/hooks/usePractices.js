import { useState, useEffect, useCallback } from 'react'
import { api } from '../lib/api'

export function usePractices() {
  const [practices, setPractices] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const data = await api.getPractices()
      const sorted = (data || []).sort((a, b) => new Date(a.startTime) - new Date(b.startTime))
      setPractices(sorted)
    } catch (e) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load])

  return { practices, loading, error, reload: load }
}

export function useMySignups(email) {
  const [signups, setSignups] = useState([])
  const [loading, setLoading] = useState(false)

  const load = useCallback(async () => {
    if (!email) return
    setLoading(true)
    try {
      const data = await api.getMySignups(email)
      setSignups(data || [])
    } catch {
      setSignups([])
    } finally {
      setLoading(false)
    }
  }, [email])

  useEffect(() => { load() }, [load])

  return { signups, loading, reload: load }
}
