import { createContext, useContext } from 'react'

// Context object + hook live in a plain module (not the .jsx) so the component
// file exports only <RefreshProvider>, satisfying react-refresh/only-export-
// components (fast refresh breaks when a file mixes component and non-component
// exports).
export const RefreshContext = createContext({
  refreshKey: 0,
  refresh: () => {},
  lastRefreshedAt: null,
})

export function useRefresh() {
  return useContext(RefreshContext)
}
