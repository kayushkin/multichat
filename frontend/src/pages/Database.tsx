import { useEffect, useState } from 'react'

interface PlatformIdentity {
  user_id: string
  platform: string
  avatar_url?: string
  phone?: string
}

interface UnifiedContact {
  display_name: string
  identities: PlatformIdentity[]
  platforms: string[]
}

const platformBadge: Record<string, { label: string; color: string; icon: string }> = {
  telegram: { label: 'Telegram', color: 'bg-sky-500/20 text-sky-300 border-sky-500/30', icon: '✈️' },
  whatsapp: { label: 'WhatsApp', color: 'bg-emerald-500/20 text-emerald-300 border-emerald-500/30', icon: '💬' },
  facebook: { label: 'Facebook', color: 'bg-blue-600/20 text-blue-300 border-blue-600/30', icon: '📘' },
  instagram: { label: 'Instagram', color: 'bg-pink-500/20 text-pink-300 border-pink-500/30', icon: '📷' },
  signal: { label: 'Signal', color: 'bg-blue-400/20 text-blue-200 border-blue-400/30', icon: '🔒' },
  unknown: { label: 'Unknown', color: 'bg-gray-500/20 text-gray-300 border-gray-500/30', icon: '❓' },
}

function Database() {
  const [contacts, setContacts] = useState<UnifiedContact[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [search, setSearch] = useState('')
  const [expanded, setExpanded] = useState<string | null>(null)
  const [filter, setFilter] = useState<string>('all')

  useEffect(() => {
    fetch('/api/contacts/unified')
      .then(r => { if (!r.ok) throw new Error(`${r.status}`); return r.json() })
      .then(setContacts)
      .catch(e => setError(e.message))
      .finally(() => setLoading(false))
  }, [])

  const filtered = contacts.filter(c => {
    const matchesSearch = c.display_name?.toLowerCase().includes(search.toLowerCase())
    const matchesFilter = filter === 'all' || filter === 'multi'
      ? (filter === 'all' || c.platforms.length > 1)
      : c.platforms.includes(filter)
    return matchesSearch && matchesFilter
  })

  const multiPlatformCount = contacts.filter(c => c.platforms.length > 1).length

  if (loading) return (
    <div className="flex items-center justify-center py-20">
      <div className="animate-spin w-8 h-8 border-2 border-blue-500 border-t-transparent rounded-full" />
    </div>
  )

  if (error) return (
    <div className="bg-red-900/30 border border-red-700 rounded-lg p-4">
      <p className="text-red-300">Error: {error}</p>
    </div>
  )

  return (
    <div className="space-y-5">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-white">Contacts</h1>
          <p className="text-gray-400 text-sm mt-1">
            {contacts.length} contacts · {multiPlatformCount} cross-platform
          </p>
        </div>
      </div>

      {/* Filters */}
      <div className="flex gap-2 flex-wrap">
        {[
          { key: 'all', label: `All (${contacts.length})` },
          { key: 'multi', label: `🔗 Cross-Platform (${multiPlatformCount})` },
          { key: 'telegram', label: '✈️ Telegram' },
          { key: 'whatsapp', label: '💬 WhatsApp' },
          { key: 'facebook', label: '📘 Facebook' },
          { key: 'instagram', label: '📷 Instagram' },
        ].map(f => (
          <button
            key={f.key}
            onClick={() => setFilter(f.key)}
            className={`px-3 py-1.5 rounded-lg text-sm transition-colors ${
              filter === f.key
                ? 'bg-blue-600 text-white'
                : 'bg-gray-800 text-gray-300 hover:bg-gray-700 border border-gray-700'
            }`}
          >
            {f.label}
          </button>
        ))}
      </div>

      {/* Search */}
      <input
        type="text"
        placeholder="Search contacts..."
        value={search}
        onChange={e => setSearch(e.target.value)}
        className="w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-2.5 text-white placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500/50"
      />

      {/* Contact List */}
      <div className="space-y-2">
        {filtered.length === 0 ? (
          <div className="text-center py-12 text-gray-500">No contacts match your filters</div>
        ) : (
          filtered.map((contact, i) => (
            <div key={i} className="bg-gray-800 rounded-xl border border-gray-700 overflow-hidden">
              <button
                onClick={() => setExpanded(expanded === contact.display_name ? null : contact.display_name)}
                className="w-full text-left px-5 py-3.5 hover:bg-gray-700/30 transition-colors flex items-center gap-3"
              >
                {/* Avatar */}
                <div className="w-10 h-10 rounded-full bg-gradient-to-br from-blue-500 to-purple-600 flex items-center justify-center text-white font-bold text-sm flex-shrink-0">
                  {contact.display_name?.charAt(0)?.toUpperCase() || '?'}
                </div>

                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="text-white font-medium truncate">{contact.display_name}</span>
                    {contact.platforms.length > 1 && (
                      <span className="text-[10px] px-1.5 py-0.5 rounded bg-amber-500/20 text-amber-300 border border-amber-500/30 font-medium">
                        {contact.platforms.length} platforms
                      </span>
                    )}
                  </div>
                  <div className="flex gap-1.5 mt-1">
                    {contact.platforms.map(p => {
                      const badge = platformBadge[p] || platformBadge.unknown
                      return (
                        <span key={p} className={`text-[10px] px-2 py-0.5 rounded-full border ${badge.color}`}>
                          {badge.icon} {badge.label}
                        </span>
                      )
                    })}
                  </div>
                </div>

                <span className="text-gray-500 text-sm">{expanded === contact.display_name ? '▲' : '▼'}</span>
              </button>

              {/* Expanded Details */}
              {expanded === contact.display_name && (
                <div className="border-t border-gray-700 bg-gray-900/30 px-5 py-3 space-y-2">
                  <p className="text-xs text-gray-500 uppercase tracking-wider font-medium">Platform Identities</p>
                  {contact.identities.map((id, j) => {
                    const badge = platformBadge[id.platform] || platformBadge.unknown
                    return (
                      <div key={j} className="flex items-center gap-3 py-1.5">
                        <span className={`text-xs px-2 py-0.5 rounded-full border ${badge.color}`}>
                          {badge.icon} {badge.label}
                        </span>
                        <span className="text-gray-400 text-xs font-mono truncate">{id.user_id}</span>
                      </div>
                    )
                  })}
                </div>
              )}
            </div>
          ))
        )}
      </div>
    </div>
  )
}

export default Database
