import { useCallback, useState } from 'react'
import { RefreshContext } from './refresh'

export function RefreshProvider({ children }) {
  const [refreshKey, setRefreshKey] = useState(0)
  const [lastRefreshedAt, setLastRefreshedAt] = useState(() => new Date())

  const refresh = useCallback(() => {
    setRefreshKey((k) => k + 1)
    setLastRefreshedAt(new Date())
  }, [])

  return (
    <RefreshContext.Provider value={{ refreshKey, refresh, lastRefreshedAt }}>
      {children}
    </RefreshContext.Provider>
  )
}
