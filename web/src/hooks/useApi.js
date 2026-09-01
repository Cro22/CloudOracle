import { useContext, useEffect, useState } from 'react'
import { RefreshContext } from '../context/refresh'

const BASE_URL = import.meta.env.VITE_API_BASE_URL ?? ''

export function useApi(endpoint, { skip = false } = {}) {
  const { refreshKey } = useContext(RefreshContext)
  const [data, setData] = useState(null)
  const [loading, setLoading] = useState(!skip)
  const [error, setError] = useState(null)

  useEffect(() => {
    if (skip) return

    const controller = new AbortController()
    let active = true

    // Reset to the loading state when the endpoint/refreshKey changes, before
    // the new fetch resolves. This synchronous setState is intentional for a
    // fetch-on-change hook; the rule targets accidental cascading renders.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setLoading(true)
    setError(null)

    fetch(`${BASE_URL}${endpoint}`, { signal: controller.signal })
      .then(async (res) => {
        if (!res.ok) {
          let message = `HTTP ${res.status}`
          try {
            const body = await res.json()
            if (body?.error) message = body.error
          } catch { /* body wasn't JSON; keep the HTTP status message */ }
          throw new Error(message)
        }
        return res.json()
      })
      .then((json) => {
        if (!active) return
        setData(json)
      })
      .catch((err) => {
        if (!active || err.name === 'AbortError') return
        setError(err)
      })
      .finally(() => {
        if (active) setLoading(false)
      })

    return () => {
      active = false
      controller.abort()
    }
  }, [endpoint, skip, refreshKey])

  return { data, loading, error }
}
