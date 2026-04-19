import { useMemo, useState } from 'react'
import { useApi } from '../hooks/useApi'
import { useRefresh } from '../context/RefreshContext'
import { formatCurrency } from '../lib/theme'
import { EmptyState, ErrorState, SkeletonBlock } from './UIStates'

const severityRank = { High: 3, Medium: 2, Low: 1 }

const severityStyles = {
  High: 'bg-red-100 text-red-700 ring-1 ring-red-200 dark:bg-red-500/20 dark:text-red-400 dark:ring-red-500/30',
  Medium: 'bg-amber-100 text-amber-700 ring-1 ring-amber-200 dark:bg-amber-500/20 dark:text-amber-400 dark:ring-amber-500/30',
  Low: 'bg-emerald-100 text-emerald-700 ring-1 ring-emerald-200 dark:bg-emerald-500/20 dark:text-emerald-400 dark:ring-emerald-500/30',
}

function SortHeader({ label, column, sort, setSort, className = '' }) {
  const active = sort.column === column
  const direction = active ? sort.direction : null

  return (
    <th
      scope="col"
      className={`cursor-pointer select-none px-4 py-3 text-left text-xs font-semibold uppercase tracking-wider text-slate-500 transition-colors hover:text-slate-900 dark:text-slate-400 dark:hover:text-slate-100 ${className}`}
      onClick={() =>
        setSort({
          column,
          direction: active && direction === 'desc' ? 'asc' : 'desc',
        })
      }
    >
      <span className="inline-flex items-center gap-1.5">
        {label}
        <svg
          className={`h-3 w-3 transition-opacity ${active ? 'opacity-100' : 'opacity-30'}`}
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2.5"
        >
          {direction === 'asc' ? (
            <path d="M18 15l-6-6-6 6" />
          ) : (
            <path d="M6 9l6 6 6-6" />
          )}
        </svg>
      </span>
    </th>
  )
}

function Shell({ children, savings }) {
  return (
    <div className="overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-sm dark:border-slate-800 dark:bg-slate-900">
      <div className="flex items-start justify-between gap-6 border-b border-slate-200 px-6 py-5 dark:border-slate-800">
        <div>
          <h3 className="text-base font-semibold text-slate-900 dark:text-slate-50">Findings</h3>
          <p className="mt-1 text-sm text-slate-500 dark:text-slate-400">
            Waste detected by the rules engine, ranked by monthly savings
          </p>
        </div>
        <div className="text-right">
          <p className="text-xs font-medium uppercase tracking-wide text-slate-500 dark:text-slate-400">
            Potential savings
          </p>
          <p className="mt-0.5 text-xl font-semibold text-amber-600 tabular-nums dark:text-amber-400">
            {savings}
          </p>
        </div>
      </div>
      {children}
    </div>
  )
}

function SkeletonRows() {
  return (
    <div className="divide-y divide-slate-100 px-6 py-4 dark:divide-slate-800">
      {Array.from({ length: 6 }).map((_, i) => (
        <div key={i} className="flex items-center gap-4 py-3">
          <SkeletonBlock className="h-5 w-16 rounded-full" />
          <SkeletonBlock className="h-4 w-12" />
          <SkeletonBlock className="h-4 flex-1" />
          <SkeletonBlock className="h-4 w-20" />
        </div>
      ))}
    </div>
  )
}

export default function FindingsTable() {
  const { data, loading, error } = useApi('/api/findings')
  const { refresh } = useRefresh()
  const [sort, setSort] = useState({ column: 'savings', direction: 'desc' })

  const findings = data?.findings ?? []

  const sorted = useMemo(() => {
    const rows = [...findings]
    rows.sort((a, b) => {
      let cmp = 0
      switch (sort.column) {
        case 'severity':
          cmp = (severityRank[a.Severity] ?? 0) - (severityRank[b.Severity] ?? 0)
          break
        case 'savings':
          cmp = (a.MonthlySavings ?? 0) - (b.MonthlySavings ?? 0)
          break
        case 'cost':
          cmp = (a.MonthlyCost ?? 0) - (b.MonthlyCost ?? 0)
          break
        case 'service':
          cmp = (a.Service ?? '').localeCompare(b.Service ?? '')
          break
        default:
          cmp = 0
      }
      return sort.direction === 'asc' ? cmp : -cmp
    })
    return rows
  }, [findings, sort])

  if (error) {
    return (
      <Shell savings="—">
        <div className="px-6 py-6">
          <ErrorState title="Failed to load findings" error={error} onRetry={refresh} compact />
        </div>
      </Shell>
    )
  }

  if (loading || !data) {
    return (
      <Shell savings="—">
        <SkeletonRows />
      </Shell>
    )
  }

  if (findings.length === 0) {
    return (
      <Shell savings={formatCurrency(0)}>
        <div className="px-6 py-6">
          <EmptyState
            title="No findings"
            description="No waste detected. Run 'oracle seed' with richer data, or keep watching."
          />
        </div>
      </Shell>
    )
  }

  return (
    <Shell savings={formatCurrency(data.total_potential_savings)}>
      <div className="overflow-x-auto">
        <table className="min-w-full divide-y divide-slate-200 dark:divide-slate-800">
          <thead className="bg-slate-50 dark:bg-slate-900/60">
            <tr>
              <SortHeader label="Severity" column="severity" sort={sort} setSort={setSort} />
              <SortHeader label="Service" column="service" sort={sort} setSort={setSort} />
              <th scope="col" className="hidden px-4 py-3 text-left text-xs font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-400 md:table-cell">
                Resource
              </th>
              <th scope="col" className="hidden px-4 py-3 text-left text-xs font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-400 lg:table-cell">
                Rule
              </th>
              <SortHeader
                label="Cost / mo"
                column="cost"
                sort={sort}
                setSort={setSort}
                className="hidden text-right sm:table-cell"
              />
              <SortHeader
                label="Savings / mo"
                column="savings"
                sort={sort}
                setSort={setSort}
                className="text-right"
              />
              <th scope="col" className="hidden px-4 py-3 text-left text-xs font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-400 xl:table-cell">
                Recommendation
              </th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
            {sorted.map((f, i) => (
              <tr
                key={`${f.ResourceID}-${f.Rule}-${i}`}
                className="transition-colors hover:bg-slate-50 dark:hover:bg-slate-800/40"
              >
                <td className="whitespace-nowrap px-4 py-4">
                  <span
                    className={`inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-semibold ${
                      severityStyles[f.Severity] ?? 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300'
                    }`}
                  >
                    {f.Severity ?? '—'}
                  </span>
                </td>
                <td className="whitespace-nowrap px-4 py-4 text-sm font-medium text-slate-900 dark:text-slate-100">
                  {f.Service}
                </td>
                <td className="hidden px-4 py-4 md:table-cell">
                  <div className="font-mono text-xs text-slate-900 dark:text-slate-100">
                    {f.ResourceID}
                  </div>
                  <div className="text-xs text-slate-500 dark:text-slate-400">
                    {f.ResourceType}
                    {f.Region ? ` · ${f.Region}` : ''}
                  </div>
                </td>
                <td className="hidden whitespace-nowrap px-4 py-4 text-xs text-slate-500 dark:text-slate-400 lg:table-cell">
                  <code className="rounded bg-slate-100 px-1.5 py-0.5 dark:bg-slate-800">
                    {f.Rule}
                  </code>
                </td>
                <td className="hidden whitespace-nowrap px-4 py-4 text-right text-sm tabular-nums text-slate-700 sm:table-cell dark:text-slate-300">
                  {formatCurrency(f.MonthlyCost)}
                </td>
                <td className="whitespace-nowrap px-4 py-4 text-right text-sm font-semibold tabular-nums text-amber-600 dark:text-amber-400">
                  {formatCurrency(f.MonthlySavings)}
                </td>
                <td className="hidden max-w-sm px-4 py-4 text-xs text-slate-600 xl:table-cell dark:text-slate-400">
                  {f.Recommendation}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Shell>
  )
}
