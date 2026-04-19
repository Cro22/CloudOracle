export function SkeletonBlock({ className = '' }) {
  return (
    <div
      className={`animate-pulse rounded-lg bg-slate-200/80 dark:bg-slate-800/70 ${className}`}
    />
  )
}

export function SkeletonCard() {
  return (
    <div className="rounded-2xl border border-slate-200 bg-white p-6 shadow-sm dark:border-slate-800 dark:bg-slate-900">
      <div className="absolute inset-x-0 top-0 h-1 bg-slate-200 dark:bg-slate-800" />
      <SkeletonBlock className="h-3 w-24" />
      <SkeletonBlock className="mt-4 h-8 w-32" />
      <SkeletonBlock className="mt-3 h-3 w-20" />
    </div>
  )
}

export function ErrorState({ title = 'Something went wrong', error, onRetry, compact = false }) {
  const message = error instanceof Error ? error.message : String(error ?? 'Unknown error')

  if (compact) {
    return (
      <div className="flex items-center justify-between gap-4 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-400">
        <span className="truncate">
          <span className="font-medium">{title}.</span> {message}
        </span>
        {onRetry && (
          <button
            type="button"
            onClick={onRetry}
            className="flex-shrink-0 rounded-md border border-red-300 bg-white px-3 py-1 text-xs font-semibold text-red-700 transition-colors hover:bg-red-50 dark:border-red-500/40 dark:bg-red-500/10 dark:text-red-300 dark:hover:bg-red-500/20"
          >
            Retry
          </button>
        )}
      </div>
    )
  }

  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 rounded-2xl border border-red-200 bg-red-50/60 px-6 py-10 text-center dark:border-red-500/30 dark:bg-red-500/5">
      <svg className="h-8 w-8 text-red-500" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
        <circle cx="12" cy="12" r="10" />
        <line x1="12" y1="8" x2="12" y2="12" />
        <line x1="12" y1="16" x2="12.01" y2="16" />
      </svg>
      <div>
        <p className="font-semibold text-red-700 dark:text-red-400">{title}</p>
        <p className="mt-1 text-sm text-red-600/80 dark:text-red-400/80">{message}</p>
      </div>
      {onRetry && (
        <button
          type="button"
          onClick={onRetry}
          className="mt-1 rounded-md border border-red-300 bg-white px-4 py-1.5 text-xs font-semibold text-red-700 transition-colors hover:bg-red-50 dark:border-red-500/40 dark:bg-red-500/10 dark:text-red-300 dark:hover:bg-red-500/20"
        >
          Retry
        </button>
      )}
    </div>
  )
}

export function EmptyState({ title = 'No data yet', description, icon }) {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 rounded-2xl border border-dashed border-slate-300 bg-slate-50/60 px-6 py-10 text-center dark:border-slate-700 dark:bg-slate-900/40">
      <div className="flex h-10 w-10 items-center justify-center rounded-full bg-slate-200 text-slate-500 dark:bg-slate-800 dark:text-slate-400">
        {icon ?? (
          <svg className="h-5 w-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
            <rect x="3" y="3" width="18" height="18" rx="2" />
            <path d="M3 9h18M9 21V9" />
          </svg>
        )}
      </div>
      <div>
        <p className="text-sm font-semibold text-slate-700 dark:text-slate-200">{title}</p>
        {description && (
          <p className="mt-1 text-xs text-slate-500 dark:text-slate-400">{description}</p>
        )}
      </div>
    </div>
  )
}
