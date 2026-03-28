const stats = [
  { label: 'Active Conversations', value: '—', icon: '💬', color: 'var(--accent)' },
  { label: 'Messages Today', value: '—', icon: '📨', color: 'var(--telegram)' },
  { label: 'Avg Response Time', value: '—', icon: '⚡', color: 'var(--warning)' },
  { label: 'Platforms Connected', value: '0', icon: '🔗', color: 'var(--success)' },
]

export default function Dashboard() {
  return (
    <div className="p-8 max-w-5xl mx-auto">
      <h2 className="text-xl font-bold mb-1" style={{ color: 'var(--text-primary)' }}>Dashboard</h2>
      <p className="text-sm mb-8" style={{ color: 'var(--text-secondary)' }}>Overview of your unified communications</p>

      <div className="grid grid-cols-2 lg:grid-cols-4 gap-4 mb-8">
        {stats.map(s => (
          <div key={s.label} className="p-5 rounded-xl" style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)' }}>
            <div className="text-2xl mb-2">{s.icon}</div>
            <div className="text-2xl font-bold mb-1" style={{ color: s.color }}>{s.value}</div>
            <div className="text-xs" style={{ color: 'var(--text-muted)' }}>{s.label}</div>
          </div>
        ))}
      </div>

      {/* Platform status */}
      <div className="rounded-xl p-6" style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)' }}>
        <h3 className="text-sm font-semibold mb-4" style={{ color: 'var(--text-primary)' }}>Platform Status</h3>
        <div className="space-y-3">
          {[
            { name: 'Telegram', icon: '✈️', color: 'var(--telegram)', status: 'Configure in Chatwoot' },
            { name: 'WhatsApp', icon: '💬', color: 'var(--whatsapp)', status: 'Configure in Chatwoot' },
          ].map(p => (
            <div key={p.name} className="flex items-center justify-between py-2">
              <div className="flex items-center gap-3">
                <span style={{ color: p.color }}>{p.icon}</span>
                <span className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>{p.name}</span>
              </div>
              <span className="text-xs px-3 py-1 rounded-full" style={{ background: 'var(--bg-tertiary)', color: 'var(--text-muted)' }}>
                {p.status}
              </span>
            </div>
          ))}
        </div>
      </div>

      {/* AI Analysis section */}
      <div className="mt-6 rounded-xl p-6" style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)' }}>
        <h3 className="text-sm font-semibold mb-2" style={{ color: 'var(--text-primary)' }}>🤖 AI Analysis</h3>
        <p className="text-xs" style={{ color: 'var(--text-secondary)' }}>
          AI-powered conversation analysis will be available once messages start flowing. 
          Features include: sentiment tracking, topic extraction, conversation summaries, and escalation flagging.
        </p>
      </div>
    </div>
  )
}
