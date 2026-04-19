import { useState } from 'react'
import {
  BarChart,
  Bar,
  XAxis,
  YAxis,
  Tooltip,
  ResponsiveContainer,
  Cell,
  CartesianGrid,
} from 'recharts'
import { useApi } from '../hooks/useApi'
import { useRefresh } from '../context/RefreshContext'
import { colorForService, formatCurrency } from '../lib/theme'
import { EmptyState, ErrorState, SkeletonBlock } from './UIStates'

function CustomTooltip({ active, payload }) {
  if (!active || !payload?.length) return null
  const { name, cost, savings, count } = payload[0].payload
  return (
    <div className="rounded-lg border border-slate-200 bg-white/95 px-4 py-3 text-sm shadow-lg backdrop-blur dark:border-slate-700 dark:bg-slate-900/95">
      <p className="font-semibold uppercase tracking-wide text-slate-700 dark:text-slate-200">
        {name}
      </p>
      <p className="mt-1 font-medium text-slate-900 tabular-nums dark:text-slate-100">
        {formatCurrency(cost)} / mo
      </p>
      {typeof count === 'number' && (
        <p className="text-xs text-slate-500 dark:text-slate-400">
          {count} resource{count === 1 ? '' : 's'}
        </p>
      )}
      {savings > 0 && (
        <p className="text-xs font-medium text-amber-600 dark:text-amber-400">
          {formatCurrency(savings)} potential savings
        </p>
      )}
    </div>
  )
}

function ChartShell({ children, groupBy, setGroupBy, count }) {
  return (
    <div className="flex h-full flex-col rounded-2xl border border-slate-200 bg-white p-6 shadow-sm dark:border-slate-800 dark:bg-slate-900">
      <div className="mb-6 flex items-start justify-between gap-4">
        <div>
          <h3 className="text-base font-semibold text-slate-900 dark:text-slate-50">
            Cost by {groupBy === 'service' ? 'Service' : 'Provider'}
          </h3>
          <p className="mt-1 text-sm text-slate-500 dark:text-slate-400">
            {groupBy === 'service'
              ? 'Monthly spend per AWS / GCP / Azure service'
              : 'Monthly spend per cloud provider'}
          </p>
        </div>
        <div className="flex items-center gap-2">
          {typeof count === 'number' && (
            <span className="rounded-full bg-slate-100 px-2.5 py-1 text-xs font-medium text-slate-600 dark:bg-slate-800 dark:text-slate-300">
              {count}
            </span>
          )}
          <div className="inline-flex rounded-md border border-slate-200 bg-white p-0.5 text-xs font-medium dark:border-slate-700 dark:bg-slate-900">
            <button
              type="button"
              onClick={() => setGroupBy('service')}
              className={`rounded px-2.5 py-1 transition-colors ${
                groupBy === 'service'
                  ? 'bg-slate-900 text-white dark:bg-slate-100 dark:text-slate-900'
                  : 'text-slate-600 hover:text-slate-900 dark:text-slate-300 dark:hover:text-slate-50'
              }`}
            >
              Service
            </button>
            <button
              type="button"
              onClick={() => setGroupBy('provider')}
              className={`rounded px-2.5 py-1 transition-colors ${
                groupBy === 'provider'
                  ? 'bg-slate-900 text-white dark:bg-slate-100 dark:text-slate-900'
                  : 'text-slate-600 hover:text-slate-900 dark:text-slate-300 dark:hover:text-slate-50'
              }`}
            >
              Provider
            </button>
          </div>
        </div>
      </div>
      <div className="flex-1" style={{ minHeight: 280 }}>
        {children}
      </div>
    </div>
  )
}

export default function CostByServiceChart() {
  const [groupBy, setGroupBy] = useState('service')
  const { data, loading, error } = useApi('/api/summary')
  const { refresh } = useRefresh()

  if (error) {
    return (
      <ChartShell groupBy={groupBy} setGroupBy={setGroupBy}>
        <ErrorState title="Failed to load cost breakdown" error={error} onRetry={refresh} />
      </ChartShell>
    )
  }

  if (loading || !data) {
    return (
      <ChartShell groupBy={groupBy} setGroupBy={setGroupBy}>
        <div className="flex h-full flex-col justify-end gap-3 pb-6">
          {[0.4, 0.75, 0.55, 0.9, 0.3].map((w, i) => (
            <SkeletonBlock key={i} className="h-6" style={{ width: `${w * 100}%` }} />
          ))}
        </div>
      </ChartShell>
    )
  }

  const source = groupBy === 'service' ? data.by_service : data.by_provider
  const rows = Object.entries(source ?? {})
    .map(([name, v]) => ({
      name,
      cost: v.cost ?? 0,
      savings: v.savings ?? 0,
      count: v.count ?? 0,
    }))
    .filter((r) => r.cost > 0 || r.count > 0)
    .sort((a, b) => b.cost - a.cost)

  if (rows.length === 0) {
    return (
      <ChartShell groupBy={groupBy} setGroupBy={setGroupBy}>
        <EmptyState
          title="No cost data yet"
          description="Run 'oracle seed' to populate resources, then refresh."
        />
      </ChartShell>
    )
  }

  return (
    <ChartShell groupBy={groupBy} setGroupBy={setGroupBy} count={rows.length}>
      <ResponsiveContainer width="100%" height="100%">
        <BarChart
          data={rows}
          layout="vertical"
          margin={{ top: 4, right: 24, left: 8, bottom: 4 }}
          barCategoryGap={10}
        >
          <CartesianGrid
            strokeDasharray="3 3"
            stroke="currentColor"
            className="text-slate-200 dark:text-slate-800"
            horizontal={false}
          />
          <XAxis
            type="number"
            tickFormatter={(v) => (v >= 1000 ? `$${(v / 1000).toFixed(1)}k` : `$${v}`)}
            stroke="currentColor"
            className="text-slate-400"
            tick={{ fontSize: 12 }}
            axisLine={false}
            tickLine={false}
          />
          <YAxis
            type="category"
            dataKey="name"
            stroke="currentColor"
            className="text-slate-500 dark:text-slate-400"
            tick={{ fontSize: 12, fontWeight: 500 }}
            axisLine={false}
            tickLine={false}
            width={80}
          />
          <Tooltip content={<CustomTooltip />} cursor={{ fill: 'rgba(148, 163, 184, 0.08)' }} />
          <Bar dataKey="cost" radius={[0, 6, 6, 0]}>
            {rows.map((entry) => (
              <Cell key={entry.name} fill={colorForService(entry.name)} />
            ))}
          </Bar>
        </BarChart>
      </ResponsiveContainer>
    </ChartShell>
  )
}
