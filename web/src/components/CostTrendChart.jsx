import {
  AreaChart,
  Area,
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  Legend,
} from 'recharts'
import { useApi } from '../hooks/useApi'
import { useRefresh } from '../context/RefreshContext'
import { colorForService, formatCurrency } from '../lib/theme'
import { EmptyState, ErrorState, SkeletonBlock } from './UIStates'

function formatDateLabel(dateStr) {
  if (!dateStr) return ''
  const d = new Date(dateStr)
  if (Number.isNaN(d.getTime())) return dateStr
  return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })
}

function StackedTooltip({ active, payload, label }) {
  if (!active || !payload?.length) return null
  const rows = payload.filter((p) => p.value > 0)
  const total = rows.reduce((acc, p) => acc + (p.value ?? 0), 0)
  return (
    <div className="rounded-lg border border-slate-200 bg-white/95 px-4 py-3 text-sm shadow-lg backdrop-blur dark:border-slate-700 dark:bg-slate-900/95">
      <p className="mb-2 font-semibold text-slate-700 dark:text-slate-200">{formatDateLabel(label)}</p>
      {rows.map((p) => (
        <div key={p.dataKey} className="flex items-center justify-between gap-6">
          <span className="flex items-center gap-2 text-xs text-slate-600 dark:text-slate-400">
            <span className="h-2.5 w-2.5 rounded-full" style={{ backgroundColor: p.color }} />
            {p.dataKey}
          </span>
          <span className="font-medium text-slate-900 tabular-nums dark:text-slate-100">
            {formatCurrency(p.value)}
          </span>
        </div>
      ))}
      <div className="mt-2 flex items-center justify-between gap-6 border-t border-slate-200 pt-2 dark:border-slate-700">
        <span className="text-xs font-semibold uppercase tracking-wide text-slate-500 dark:text-slate-400">
          Total
        </span>
        <span className="font-semibold text-slate-900 tabular-nums dark:text-slate-100">
          {formatCurrency(total)}
        </span>
      </div>
    </div>
  )
}

function TotalTooltip({ active, payload, label }) {
  if (!active || !payload?.length) return null
  return (
    <div className="rounded-lg border border-slate-200 bg-white/95 px-4 py-3 text-sm shadow-lg backdrop-blur dark:border-slate-700 dark:bg-slate-900/95">
      <p className="mb-1 font-semibold text-slate-700 dark:text-slate-200">{formatDateLabel(label)}</p>
      <p className="font-medium text-slate-900 tabular-nums dark:text-slate-100">
        {formatCurrency(payload[0].value)}
      </p>
    </div>
  )
}

function ChartShell({ title, subtitle, badge, children }) {
  return (
    <div className="flex h-full flex-col rounded-2xl border border-slate-200 bg-white p-6 shadow-sm dark:border-slate-800 dark:bg-slate-900">
      <div className="mb-6 flex items-start justify-between gap-4">
        <div>
          <h3 className="text-base font-semibold text-slate-900 dark:text-slate-50">{title}</h3>
          <p className="mt-1 text-sm text-slate-500 dark:text-slate-400">{subtitle}</p>
        </div>
        {badge}
      </div>
      <div className="flex-1" style={{ minHeight: 280 }}>
        {children}
      </div>
    </div>
  )
}

export default function CostTrendChart() {
  const { data, loading, error } = useApi('/api/trends')
  const { refresh } = useRefresh()

  const title = 'Cost Trend'
  const subtitle = 'Monthly total stacked by service'

  if (error) {
    return (
      <ChartShell title={title} subtitle={subtitle}>
        <ErrorState title="Failed to load trends" error={error} onRetry={refresh} />
      </ChartShell>
    )
  }

  if (loading || !data) {
    return (
      <ChartShell title={title} subtitle={subtitle}>
        <div className="flex h-full items-end gap-2 pb-6">
          {[0.3, 0.4, 0.55, 0.5, 0.65, 0.7, 0.85, 0.78].map((h, i) => (
            <SkeletonBlock key={i} className="flex-1" style={{ height: `${h * 100}%` }} />
          ))}
        </div>
      </ChartShell>
    )
  }

  if (!Array.isArray(data) || data.length === 0) {
    return (
      <ChartShell title={title} subtitle={subtitle}>
        <EmptyState
          title="No cost snapshots yet"
          description="Each 'oracle seed' creates a snapshot. Run it on a few days to build a trend."
        />
      </ChartShell>
    )
  }

  const services = Array.from(
    data.reduce((set, row) => {
      Object.keys(row.breakdown_by_service ?? {}).forEach((s) => set.add(s))
      return set
    }, new Set()),
  )

  const latest = data[data.length - 1]
  const first = data[0]
  const delta = (latest?.total_cost ?? 0) - (first?.total_cost ?? 0)
  const deltaPct = first?.total_cost > 0 ? (delta / first.total_cost) * 100 : 0
  const trendUp = delta >= 0

  const badge = data.length >= 2 && (
    <span
      className={`inline-flex items-center gap-1 rounded-full px-2.5 py-1 text-xs font-semibold ${
        trendUp
          ? 'bg-red-100 text-red-700 dark:bg-red-500/20 dark:text-red-400'
          : 'bg-emerald-100 text-emerald-700 dark:bg-emerald-500/20 dark:text-emerald-400'
      }`}
    >
      <svg className="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3">
        {trendUp ? (
          <path d="M7 17L17 7M17 7H9M17 7V15" />
        ) : (
          <path d="M17 7L7 17M7 17H15M7 17V9" />
        )}
      </svg>
      {trendUp ? '+' : ''}
      {deltaPct.toFixed(1)}%
    </span>
  )

  if (services.length === 0) {
    const totalData = data.map((row) => ({
      date: row.date,
      total_cost: row.total_cost ?? 0,
    }))
    return (
      <ChartShell title={title} subtitle="Monthly total cost" badge={badge}>
        <ResponsiveContainer width="100%" height="100%">
          <LineChart data={totalData} margin={{ top: 8, right: 12, left: 0, bottom: 0 }}>
            <defs>
              <linearGradient id="trend-total" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor="#3b82f6" stopOpacity={0.8} />
                <stop offset="100%" stopColor="#3b82f6" stopOpacity={0.1} />
              </linearGradient>
            </defs>
            <CartesianGrid strokeDasharray="3 3" stroke="currentColor" className="text-slate-200 dark:text-slate-800" vertical={false} />
            <XAxis
              dataKey="date"
              tickFormatter={formatDateLabel}
              stroke="currentColor"
              className="text-slate-400"
              tick={{ fontSize: 12 }}
              axisLine={false}
              tickLine={false}
            />
            <YAxis
              tickFormatter={(v) => `$${(v / 1000).toFixed(0)}k`}
              stroke="currentColor"
              className="text-slate-400"
              tick={{ fontSize: 12 }}
              axisLine={false}
              tickLine={false}
            />
            <Tooltip content={<TotalTooltip />} />
            <Line type="monotone" dataKey="total_cost" stroke="#3b82f6" strokeWidth={2.5} dot={{ r: 4 }} activeDot={{ r: 6 }} />
          </LineChart>
        </ResponsiveContainer>
      </ChartShell>
    )
  }

  const stackedData = data.map((row) => ({
    date: row.date,
    ...(row.breakdown_by_service ?? {}),
  }))

  return (
    <ChartShell title={title} subtitle={subtitle} badge={badge}>
      <ResponsiveContainer width="100%" height="100%">
        <AreaChart data={stackedData} margin={{ top: 8, right: 12, left: 0, bottom: 0 }}>
          <defs>
            {services.map((s) => (
              <linearGradient key={s} id={`grad-${s}`} x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor={colorForService(s)} stopOpacity={0.55} />
                <stop offset="100%" stopColor={colorForService(s)} stopOpacity={0.05} />
              </linearGradient>
            ))}
          </defs>
          <CartesianGrid strokeDasharray="3 3" stroke="currentColor" className="text-slate-200 dark:text-slate-800" vertical={false} />
          <XAxis
            dataKey="date"
            tickFormatter={formatDateLabel}
            stroke="currentColor"
            className="text-slate-400"
            tick={{ fontSize: 12 }}
            axisLine={false}
            tickLine={false}
          />
          <YAxis
            tickFormatter={(v) => (v >= 1000 ? `$${(v / 1000).toFixed(0)}k` : `$${v}`)}
            stroke="currentColor"
            className="text-slate-400"
            tick={{ fontSize: 12 }}
            axisLine={false}
            tickLine={false}
          />
          <Tooltip content={<StackedTooltip />} />
          <Legend
            verticalAlign="bottom"
            height={32}
            iconType="circle"
            iconSize={8}
            wrapperStyle={{ fontSize: 12, paddingTop: 8 }}
          />
          {services.map((s) => (
            <Area
              key={s}
              type="monotone"
              dataKey={s}
              stackId="1"
              stroke={colorForService(s)}
              strokeWidth={2}
              fill={`url(#grad-${s})`}
            />
          ))}
        </AreaChart>
      </ResponsiveContainer>
    </ChartShell>
  )
}
