import { useEffect, useRef, useState } from 'react'

// The insights-agent is a separate service from the Go dashboard API. In dev,
// vite proxies /ask to it (see vite.config.js); in prod set VITE_AGENT_BASE_URL
// to the agent's URL at build time. VITE_AGENT_API_KEY is sent as X-API-Key
// only when the agent was started with AGENT_API_KEY.
const AGENT_URL = import.meta.env.VITE_AGENT_BASE_URL ?? ''
const AGENT_KEY = import.meta.env.VITE_AGENT_API_KEY

function newThreadId() {
  // randomUUID is available in every browser that runs this dashboard; the
  // Math.random fallback keeps a non-secure context (plain http) from throwing.
  return globalThis.crypto?.randomUUID?.() ?? `t-${Date.now()}-${Math.random().toString(16).slice(2)}`
}

export default function ChatWidget() {
  const [open, setOpen] = useState(false)
  const [threadId, setThreadId] = useState(newThreadId)
  const [messages, setMessages] = useState([])
  const [input, setInput] = useState('')
  const [loading, setLoading] = useState(false)
  const scrollRef = useRef(null)

  useEffect(() => {
    // Keep the latest message in view as the transcript grows.
    scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight })
  }, [messages, loading])

  async function send(e) {
    e.preventDefault()
    const query = input.trim()
    if (!query || loading) return
    setInput('')
    setMessages((m) => [...m, { role: 'user', text: query }])
    setLoading(true)
    try {
      const res = await fetch(`${AGENT_URL}/ask`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          ...(AGENT_KEY ? { 'X-API-Key': AGENT_KEY } : {}),
        },
        body: JSON.stringify({ query, thread_id: threadId }),
      })
      if (!res.ok) {
        let message = `HTTP ${res.status}`
        try {
          const body = await res.json()
          if (body?.detail) message = body.detail
        } catch { /* body wasn't JSON; keep the HTTP status message */ }
        throw new Error(message)
      }
      const data = await res.json()
      setMessages((m) => [
        ...m,
        { role: 'assistant', text: data.answer, fallback: data.fallback_used },
      ])
    } catch (err) {
      setMessages((m) => [...m, { role: 'error', text: err.message }])
    } finally {
      setLoading(false)
    }
  }

  function reset() {
    setMessages([])
    setThreadId(newThreadId())
  }

  if (!open) {
    return (
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="fixed bottom-6 right-6 z-40 inline-flex h-14 w-14 items-center justify-center rounded-full bg-gradient-to-br from-brand-500 to-accent-600 text-white shadow-lg transition-transform hover:-translate-y-0.5"
        aria-label="Open FinOps assistant"
      >
        <svg className="h-6 w-6" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" />
        </svg>
      </button>
    )
  }

  return (
    <div className="fixed bottom-6 right-6 z-40 flex h-[32rem] w-[min(24rem,calc(100vw-3rem))] flex-col overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-2xl dark:border-slate-700 dark:bg-slate-900">
      <div className="flex items-center justify-between border-b border-slate-200 px-4 py-3 dark:border-slate-700">
        <div>
          <p className="text-sm font-semibold text-slate-900 dark:text-slate-50">FinOps assistant</p>
          <p className="text-xs text-slate-500 dark:text-slate-400">Ask about your spend & savings</p>
        </div>
        <div className="flex items-center gap-1">
          <button
            type="button"
            onClick={reset}
            className="rounded-lg px-2 py-1 text-xs font-medium text-slate-500 hover:bg-slate-100 dark:text-slate-400 dark:hover:bg-slate-800"
            aria-label="New conversation"
          >
            New
          </button>
          <button
            type="button"
            onClick={() => setOpen(false)}
            className="inline-flex h-7 w-7 items-center justify-center rounded-lg text-slate-500 hover:bg-slate-100 dark:text-slate-400 dark:hover:bg-slate-800"
            aria-label="Close assistant"
          >
            <svg className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <path d="M18 6 6 18M6 6l12 12" />
            </svg>
          </button>
        </div>
      </div>

      <div ref={scrollRef} className="flex-1 space-y-3 overflow-y-auto px-4 py-4">
        {messages.length === 0 && (
          <p className="mt-8 text-center text-sm text-slate-400 dark:text-slate-500">
            e.g. "How much did I spend on AWS last month?"
          </p>
        )}
        {messages.map((m, i) => (
          <Bubble key={i} message={m} />
        ))}
        {loading && (
          <div className="text-xs text-slate-400 dark:text-slate-500">thinking…</div>
        )}
      </div>

      <form onSubmit={send} className="border-t border-slate-200 p-3 dark:border-slate-700">
        <div className="flex items-end gap-2">
          <textarea
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey) send(e)
            }}
            rows={1}
            placeholder="Ask a question…"
            className="flex-1 resize-none rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 text-sm text-slate-900 outline-none focus:border-brand-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100"
          />
          <button
            type="submit"
            disabled={loading || !input.trim()}
            className="inline-flex h-9 items-center rounded-xl bg-brand-500 px-3 text-sm font-medium text-white shadow-sm transition-colors hover:bg-brand-600 disabled:opacity-40"
          >
            Send
          </button>
        </div>
      </form>
    </div>
  )
}

function Bubble({ message }) {
  const { role, text, fallback } = message
  if (role === 'user') {
    return (
      <div className="ml-auto max-w-[85%] rounded-2xl rounded-br-sm bg-brand-500 px-3 py-2 text-sm text-white">
        {text}
      </div>
    )
  }
  if (role === 'error') {
    return (
      <div className="max-w-[85%] rounded-2xl bg-rose-50 px-3 py-2 text-sm text-rose-700 dark:bg-rose-950 dark:text-rose-300">
        {text}
      </div>
    )
  }
  return (
    <div className="max-w-[90%] space-y-1">
      <div className="whitespace-pre-wrap rounded-2xl rounded-bl-sm bg-slate-100 px-3 py-2 text-sm text-slate-800 dark:bg-slate-800 dark:text-slate-100">
        {text}
      </div>
      {fallback && (
        <p className="px-1 text-[11px] text-amber-600 dark:text-amber-400">
          Verified fallback — the model's draft didn't pass grounding checks.
        </p>
      )}
    </div>
  )
}
