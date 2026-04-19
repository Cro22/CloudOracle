import { useState } from 'react'
import { useApi } from '../hooks/useApi'
import { useRefresh } from '../context/RefreshContext'
import { formatCurrency } from '../lib/theme'
import { EmptyState, ErrorState, SkeletonBlock } from './UIStates'

const severityStyles = {
  High: 'bg-red-100 text-red-700 ring-1 ring-red-200 dark:bg-red-500/20 dark:text-red-400 dark:ring-red-500/30',
  Medium: 'bg-amber-100 text-amber-700 ring-1 ring-amber-200 dark:bg-amber-500/20 dark:text-amber-400 dark:ring-amber-500/30',
  Low: 'bg-emerald-100 text-emerald-700 ring-1 ring-emerald-200 dark:bg-emerald-500/20 dark:text-emerald-400 dark:ring-emerald-500/30',
}

const PAGE_SIZE_OPTIONS = [10, 20, 50, 100]

function SortHeader({ label, column, sort, onSortChange, className = '' }) {
  const active = sort.column === column
  const direction = active ? sort.direction : null

  return (
    <th
      scope="col"
      className={`cursor-pointer select-none px-4 py-3 text-left text-xs font-semibold uppercase tracking-wider text-slate-500 transition-colors hover:text-slate-900 dark:text-slate-400 dark:hover:text-slate-100 ${className}`}
      onClick={() =>
        onSortChange({
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

function Shell({ children, savings, footer }) {
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
      {footer}
    </div>
  )
}

function SkeletonRows({ rows = 6 }) {
  return (
    <div className="divide-y divide-slate-100 px-6 py-4 dark:divide-slate-800">
      {Array.from({ length: rows }).map((_, i) => (
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

function PaginationBar({ page, pageSize, total, totalPages, onPage, onPageSize, busy }) {
  const startIdx = total === 0 ? 0 : (page - 1) * pageSize + 1
  const endIdx = Math.min(page * pageSize, total)

  return (
    <div className="flex flex-col gap-3 border-t border-slate-200 px-6 py-4 text-sm dark:border-slate-800 sm:flex-row sm:items-center sm:justify-between">
      <div className="flex items-center gap-4 text-slate-600 dark:text-slate-400">
        <span>
          Showing{' '}
          <span className="font-semibold text-slate-900 tabular-nums dark:text-slate-100">
            {startIdx}–{endIdx}
          </span>{' '}
          of{' '}
          <span className="font-semibold text-slate-900 tabular-nums dark:text-slate-100">{total}</span>
        </span>

        <label className="inline-flex items-center gap-2">
          <span className="text-xs uppercase tracking-wide text-slate-500 dark:text-slate-400">Rows</span>
          <select
            value={pageSize}
            onChange={(e) => onPageSize(Number(e.target.value))}
            className="rounded-md border border-slate-200 bg-white px-2 py-1 text-xs font-medium text-slate-700 shadow-sm focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200"
          >
            {PAGE_SIZE_OPTIONS.map((n) => (
              <option key={n} value={n}>
                {n}
              </option>
            ))}
          </select>
        </label>
      </div>

      <div className="flex items-center gap-2">
        {busy && (
          <svg
            className="h-4 w-4 animate-spin text-slate-400"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            aria-label="Loading"
          >
            <path d="M21 12a9 9 0 1 1-6.219-8.56" />
          </svg>
        )}
        <button
          type="button"
          disabled={page <= 1}
          onClick={() => onPage(1)}
          className="rounded-md border border-slate-200 bg-white px-2.5 py-1 text-xs font-medium text-slate-600 shadow-sm transition-colors hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-40 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-300 dark:hover:bg-slate-800"
          aria-label="First page"
        >
          «
        </button>
        <button
          type="button"
          disabled={page <= 1}
          onClick={() => onPage(page - 1)}
          className="rounded-md border border-slate-200 bg-white px-3 py-1 text-xs font-medium text-slate-700 shadow-sm transition-colors hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-40 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200 dark:hover:bg-slate-800"
        >
          Prev
        </button>
        <span className="min-w-[6ch] text-center text-xs font-medium text-slate-700 tabular-nums dark:text-slate-200">
          {page} / {totalPages}
        </span>
        <button
          type="button"
          disabled={page >= totalPages}
          onClick={() => onPage(page + 1)}
          className="rounded-md border border-slate-200 bg-white px-3 py-1 text-xs font-medium text-slate-700 shadow-sm transition-colors hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-40 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200 dark:hover:bg-slate-800"
        >
          Next
        </button>
        <button
          type="button"
          disabled={page >= totalPages}
          onClick={() => onPage(totalPages)}
          className="rounded-md border border-slate-200 bg-white px-2.5 py-1 text-xs font-medium text-slate-600 shadow-sm transition-colors hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-40 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-300 dark:hover:bg-slate-800"
          aria-label="Last page"
        >
          »
        </button>
      </div>
    </div>
  )
}

export default function FindingsTable() {
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  const [sort, setSort] = useState({ column: 'savings', direction: 'desc' })

  const endpoint = `/api/findings?page=${page}&page_size=${pageSize}&sort=${sort.column}&order=${sort.direction}`
  const { data, loading, error } = useApi(endpoint)
  const { refresh } = useRefresh()

  const handleSortChange = (next) => {
    setSort(next)
    setPage(1)
  }

  const handlePageSizeChange = (next) => {
    setPageSize(next)
    setPage(1)
  }

  if (error) {
    return (
      <Shell savings="—">
        <div className="px-6 py-6">
          <ErrorState title="Failed to load findings" error={error} onRetry={refresh} compact />
        </div>
      </Shell>
    )
  }

  if (!data) {
    return (
      <Shell savings="—">
        <SkeletonRows />
      </Shell>
    )
  }

  const findings = data.findings ?? []
  const totalPages = data.total_pages ?? 1
  const total = data.total_count ?? 0

  const footer = (
    <PaginationBar
      page={data.page ?? page}
      pageSize={data.page_size ?? pageSize}
      total={total}
      totalPages={totalPages}
      onPage={setPage}
      onPageSize={handlePageSizeChange}
      busy={loading}
    />
  )

  if (total === 0) {
    return (
      <Shell savings={formatCurrency(0)} footer={footer}>
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
    <Shell savings={formatCurrency(data.total_potential_savings)} footer={footer}>
      <div
        className={`overflow-x-auto transition-opacity ${loading ? 'opacity-60' : 'opacity-100'}`}
        aria-busy={loading}
      >
        <table className="min-w-full divide-y divide-slate-200 dark:divide-slate-800">
          <thead className="bg-slate-50 dark:bg-slate-900/60">
            <tr>
              <SortHeader label="Severity" column="severity" sort={sort} onSortChange={handleSortChange} />
              <SortHeader label="Service" column="service" sort={sort} onSortChange={handleSortChange} />
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
                onSortChange={handleSortChange}
                className="hidden text-right sm:table-cell"
              />
              <SortHeader
                label="Savings / mo"
                column="savings"
                sort={sort}
                onSortChange={handleSortChange}
                className="text-right"
              />
              <th scope="col" className="hidden px-4 py-3 text-left text-xs font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-400 xl:table-cell">
                Recommendation
              </th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
            {findings.map((f, i) => (
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
