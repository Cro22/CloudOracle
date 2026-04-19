import { useEffect, useState } from 'react'
import SummaryCards from './SummaryCards'
import CostByServiceChart from './CostByServiceChart'
import CostTrendChart from './CostTrendChart'
import FindingsTable from './FindingsTable'
import { useRefresh } from '../context/RefreshContext'

function Logo() {
  return (
    <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-gradient-to-br from-brand-500 to-accent-600 shadow-sm">
      <svg className="h-5 w-5 text-white" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
        <path d="M17.5 19H9a7 7 0 1 1 6.71-9H17.5a4.5 4.5 0 0 1 0 9z" />
      </svg>
    </div>
  )
}

function ThemeToggle({ dark, onToggle }) {
  return (
    <button
      type="button"
      onClick={onToggle}
      className="inline-flex h-10 w-10 items-center justify-center rounded-xl border border-slate-200 bg-white text-slate-600 shadow-sm transition-all hover:-translate-y-0.5 hover:shadow-md dark:border-slate-700 dark:bg-slate-900 dark:text-slate-300"
      aria-label="Toggle theme"
    >
      {dark ? (
        <svg className="h-5 w-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <circle cx="12" cy="12" r="5" />
          <path d="M12 1v2M12 21v2M4.22 4.22l1.42 1.42M18.36 18.36l1.42 1.42M1 12h2M21 12h2M4.22 19.78l1.42-1.42M18.36 5.64l1.42-1.42" />
        </svg>
      ) : (
        <svg className="h-5 w-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z" />
        </svg>
      )}
    </button>
  )
}

function RefreshButton() {
  const { refresh } = useRefresh()
  const [spinning, setSpinning] = useState(false)

  const onClick = () => {
    refresh()
    setSpinning(true)
    setTimeout(() => setSpinning(false), 600)
  }

  return (
    <button
      type="button"
      onClick={onClick}
      className="inline-flex h-10 items-center gap-2 rounded-xl border border-slate-200 bg-white px-3 text-sm font-medium text-slate-700 shadow-sm transition-all hover:-translate-y-0.5 hover:shadow-md dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200"
      aria-label="Refresh data"
    >
      <svg
        className={`h-4 w-4 ${spinning ? 'animate-spin' : ''}`}
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
      >
        <path d="M23 4v6h-6" />
        <path d="M1 20v-6h6" />
        <path d="M20.49 9A9 9 0 0 0 5.64 5.64L1 10" />
        <path d="M3.51 15a9 9 0 0 0 14.85 3.36L23 14" />
      </svg>
      <span className="hidden sm:inline">Refresh</span>
    </button>
  )
}

function StatusPill() {
  const { lastRefreshedAt } = useRefresh()
  const [, force] = useState(0)

  useEffect(() => {
    const id = setInterval(() => force((n) => n + 1), 30_000)
    return () => clearInterval(id)
  }, [])

  const label = lastRefreshedAt
    ? `Updated ${lastRefreshedAt.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit' })}`
    : 'Not updated'

  return (
    <div className="hidden items-center gap-2 rounded-full border border-slate-200 bg-white px-3 py-1.5 text-xs font-medium text-slate-600 shadow-sm sm:inline-flex dark:border-slate-700 dark:bg-slate-900 dark:text-slate-300">
      <span className="h-2 w-2 rounded-full bg-emerald-500" />
      {label}
    </div>
  )
}

export default function DashboardLayout() {
  const [dark, setDark] = useState(false)

  return (
    <div className={dark ? 'dark' : ''}>
      <div className="min-h-screen bg-slate-50 text-slate-900 transition-colors dark:bg-slate-950 dark:text-slate-100">
        <header className="border-b border-slate-200 bg-white/80 backdrop-blur dark:border-slate-800 dark:bg-slate-950/80">
          <div className="mx-auto flex max-w-7xl items-center justify-between gap-4 px-6 py-5">
            <div className="flex items-center gap-3">
              <Logo />
              <div>
                <h1 className="text-lg font-semibold leading-tight tracking-tight text-slate-900 dark:text-slate-50">
                  CloudOracle
                </h1>
                <p className="text-xs text-slate-500 dark:text-slate-400">
                  FinOps Dashboard
                </p>
              </div>
            </div>
            <div className="flex items-center gap-3">
              <StatusPill />
              <RefreshButton />
              <ThemeToggle dark={dark} onToggle={() => setDark((v) => !v)} />
            </div>
          </div>
        </header>

        <main className="mx-auto max-w-7xl px-6 py-8">
          <div className="mb-6 flex flex-wrap items-end justify-between gap-2">
            <div>
              <h2 className="text-2xl font-semibold tracking-tight text-slate-900 dark:text-slate-50">
                Cost overview
              </h2>
              <p className="mt-1 text-sm text-slate-500 dark:text-slate-400">
                Snapshot of spend, waste, and trends across your cloud accounts.
              </p>
            </div>
          </div>

          <div className="space-y-6">
            <SummaryCards />

            <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
              <CostByServiceChart />
              <CostTrendChart />
            </div>

            <FindingsTable />
          </div>

          <footer className="mt-12 border-t border-slate-200 pt-6 text-center text-xs text-slate-400 dark:border-slate-800 dark:text-slate-500">
            CloudOracle · built with Go · React · Recharts
          </footer>
        </main>
      </div>
    </div>
  )
}
