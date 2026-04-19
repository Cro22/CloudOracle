import { useApi } from '../hooks/useApi'
import { useRefresh } from '../context/RefreshContext'
import { formatCurrency } from '../lib/theme'
import { ErrorState, SkeletonBlock } from './UIStates'

function Card({ label, value, accent, sublabel, icon }) {
  return (
    <div className="group relative overflow-hidden rounded-2xl border border-slate-200 bg-white p-6 shadow-sm transition-all duration-200 hover:-translate-y-0.5 hover:shadow-lg dark:border-slate-800 dark:bg-slate-900">
      <div className={`absolute inset-x-0 top-0 h-1 ${accent}`} />
      <div className="flex items-start justify-between gap-4">
        <div className="flex-1">
          <p className="text-xs font-medium uppercase tracking-wider text-slate-500 dark:text-slate-400">
            {label}
          </p>
          <p className="mt-3 text-3xl font-semibold text-slate-900 tabular-nums tracking-tight dark:text-slate-50">
            {value}
          </p>
          {sublabel && (
            <p className="mt-2 text-sm text-slate-500 dark:text-slate-400">{sublabel}</p>
          )}
        </div>
        <div className="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-xl bg-slate-100 text-slate-600 transition-colors group-hover:bg-slate-900 group-hover:text-white dark:bg-slate-800 dark:text-slate-300">
          {icon}
        </div>
      </div>
    </div>
  )
}

function CardSkeleton({ accent }) {
  return (
    <div className="relative overflow-hidden rounded-2xl border border-slate-200 bg-white p-6 shadow-sm dark:border-slate-800 dark:bg-slate-900">
      <div className={`absolute inset-x-0 top-0 h-1 ${accent}`} />
      <SkeletonBlock className="h-3 w-24" />
      <SkeletonBlock className="mt-4 h-8 w-32" />
      <SkeletonBlock className="mt-3 h-3 w-20" />
    </div>
  )
}

export default function SummaryCards() {
  const { data, loading, error } = useApi('/api/summary')
  const { refresh } = useRefresh()

  if (error) {
    return <ErrorState title="Failed to load summary" error={error} onRetry={refresh} compact />
  }

  if (loading || !data) {
    return (
      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        <CardSkeleton accent="bg-gradient-to-r from-brand-500 to-brand-600" />
        <CardSkeleton accent="bg-gradient-to-r from-emerald-500 to-emerald-600" />
        <CardSkeleton accent="bg-gradient-to-r from-amber-500 to-orange-500" />
        <CardSkeleton accent="bg-gradient-to-r from-accent-500 to-accent-600" />
      </div>
    )
  }

  const highCount = data.by_severity?.High ?? 0
  const providerCount = Object.keys(data.by_provider ?? {}).length
  const savingsPct = data.total_monthly_cost > 0
    ? ((data.total_potential_savings / data.total_monthly_cost) * 100).toFixed(1)
    : '0.0'

  return (
    <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
      <Card
        label="Total Resources"
        value={(data.total_resources ?? 0).toLocaleString()}
        sublabel={providerCount > 0 ? `across ${providerCount} clouds` : 'no providers'}
        accent="bg-gradient-to-r from-brand-500 to-brand-600"
        icon={
          <svg className="h-5 w-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
            <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z" />
            <path d="m7.5 4.21 4.5 2.6 4.5-2.6" />
            <path d="m7.5 19.79 4.5-2.6V4.21" />
          </svg>
        }
      />
      <Card
        label="Monthly Cost"
        value={formatCurrency(data.total_monthly_cost)}
        sublabel="current run-rate"
        accent="bg-gradient-to-r from-emerald-500 to-emerald-600"
        icon={
          <svg className="h-5 w-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
            <line x1="12" y1="2" x2="12" y2="22" />
            <path d="M17 5H9.5a3.5 3.5 0 0 0 0 7h5a3.5 3.5 0 0 1 0 7H6" />
          </svg>
        }
      />
      <Card
        label="Potential Savings"
        value={formatCurrency(data.total_potential_savings)}
        sublabel={`${savingsPct}% of spend`}
        accent="bg-gradient-to-r from-amber-500 to-orange-500"
        icon={
          <svg className="h-5 w-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
            <polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2" />
          </svg>
        }
      />
      <Card
        label="Findings"
        value={
          <span className="inline-flex items-center gap-3">
            {data.findings_count ?? 0}
            {highCount > 0 && (
              <span className="inline-flex items-center rounded-full bg-red-100 px-2.5 py-0.5 text-xs font-semibold text-red-700 dark:bg-red-500/20 dark:text-red-400">
                {highCount} High
              </span>
            )}
          </span>
        }
        sublabel="detected by rules engine"
        accent="bg-gradient-to-r from-accent-500 to-accent-600"
        icon={
          <svg className="h-5 w-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
            <path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" />
            <line x1="12" y1="9" x2="12" y2="13" />
            <line x1="12" y1="17" x2="12.01" y2="17" />
          </svg>
        }
      />
    </div>
  )
}
