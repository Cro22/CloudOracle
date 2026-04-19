import { createContext, useCallback, useContext, useState } from 'react'

export const RefreshContext = createContext({
  refreshKey: 0,
  refresh: () => {},
  lastRefreshedAt: null,
})

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

export function useRefresh() {
  return useContext(RefreshContext)
}
