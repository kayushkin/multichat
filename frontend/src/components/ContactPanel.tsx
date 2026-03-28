import type { Contact } from '../types/chatwoot'

type Props = {
  contact: Contact | null
}

export default function ContactPanel({ contact }: Props) {
  if (!contact) {
    return (
      <div className="flex flex-col items-center justify-center h-full px-4 text-center">
        <div className="text-3xl mb-3">👤</div>
        <div className="text-sm" style={{ color: 'var(--text-muted)' }}>Select a conversation to view contact details</div>
      </div>
    )
  }

  return (
    <div className="p-5 space-y-6">
      {/* Contact header */}
      <div className="flex flex-col items-center text-center">
        <div
          className="w-16 h-16 rounded-full flex items-center justify-center text-xl font-bold mb-3"
          style={{ background: 'var(--bg-tertiary)', color: 'var(--accent)' }}
        >
          {contact.avatar_url ? (
            <img src={contact.avatar_url} className="w-full h-full rounded-full object-cover" alt="" />
          ) : (
            contact.name?.charAt(0)?.toUpperCase() || '?'
          )}
        </div>
        <h3 className="text-sm font-semibold" style={{ color: 'var(--text-primary)' }}>{contact.name}</h3>
        {contact.email && <p className="text-xs mt-1" style={{ color: 'var(--text-secondary)' }}>{contact.email}</p>}
        {contact.phone_number && <p className="text-xs" style={{ color: 'var(--text-secondary)' }}>{contact.phone_number}</p>}
      </div>

      {/* AI Insights section */}
      <div>
        <h4 className="text-xs font-semibold uppercase tracking-wider mb-3" style={{ color: 'var(--text-muted)' }}>
          AI Insights
        </h4>
        <div className="space-y-2">
          <div className="p-3 rounded-lg" style={{ background: 'var(--bg-tertiary)', border: '1px solid var(--border)' }}>
            <div className="flex items-center gap-2 mb-1">
              <span className="text-xs">📊</span>
              <span className="text-xs font-medium" style={{ color: 'var(--text-secondary)' }}>Sentiment</span>
            </div>
            <div className="flex items-center gap-2">
              <div className="flex-1 h-1.5 rounded-full overflow-hidden" style={{ background: 'var(--bg-primary)' }}>
                <div className="h-full rounded-full" style={{ width: '72%', background: 'var(--success)' }} />
              </div>
              <span className="text-xs font-medium" style={{ color: 'var(--success)' }}>Positive</span>
            </div>
          </div>
          <div className="p-3 rounded-lg" style={{ background: 'var(--bg-tertiary)', border: '1px solid var(--border)' }}>
            <div className="flex items-center gap-2 mb-2">
              <span className="text-xs">🏷️</span>
              <span className="text-xs font-medium" style={{ color: 'var(--text-secondary)' }}>Topics</span>
            </div>
            <div className="flex flex-wrap gap-1">
              {['Shipping', 'Tracking', 'ETA'].map(tag => (
                <span
                  key={tag}
                  className="text-xs px-2 py-0.5 rounded-full"
                  style={{ background: 'var(--bg-primary)', color: 'var(--accent)' }}
                >
                  {tag}
                </span>
              ))}
            </div>
          </div>
          <div className="p-3 rounded-lg" style={{ background: 'var(--bg-tertiary)', border: '1px solid var(--border)' }}>
            <div className="flex items-center gap-2 mb-2">
              <span className="text-xs">📝</span>
              <span className="text-xs font-medium" style={{ color: 'var(--text-secondary)' }}>Summary</span>
            </div>
            <p className="text-xs leading-relaxed" style={{ color: 'var(--text-primary)' }}>
              Customer inquiring about shipment status. Positive tone, requesting tracking update for order #4521.
            </p>
          </div>
        </div>
      </div>
    </div>
  )
}
