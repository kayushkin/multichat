import { useEffect, useState } from 'react'
import PlatformIcon from '../components/PlatformIcon'

interface PlatformIdentity {
  user_id: string
  platform: string
  avatar_url?: string
}

interface UnifiedContact {
  display_name: string
  identities: PlatformIdentity[]
  platforms: string[]
}

interface Tag {
  id: number
  name: string
  color: string
}

const ITS_YELLOW = '#f5d40c'

function Database() {
  const [contacts, setContacts] = useState<UnifiedContact[]>([])
  const [loading, setLoading] = useState(true)
  const [search, setSearch] = useState('')
  const [filter, setFilter] = useState('all')
  const [expanded, setExpanded] = useState<string | null>(null)
  const [tags, setTags] = useState<Tag[]>([])
  const [contactTags, setContactTags] = useState<Record<string, Tag[]>>({})
  const [newTagName, setNewTagName] = useState('')
  const [showNewTag, setShowNewTag] = useState(false)
  const [tagFilter, setTagFilter] = useState<number | null>(null)

  useEffect(() => {
    Promise.all([
      fetch('/api/contacts/unified').then(r => r.json()),
      fetch('/api/tags').then(r => r.json()),
    ]).then(([c, t]) => {
      setContacts(c || [])
      setTags(t || [])
    }).finally(() => setLoading(false))
  }, [])

  const loadContactTags = async (userId: string) => {
    const res = await fetch(`/api/contacts/tags?contact=${encodeURIComponent(userId)}`)
    const data = await res.json()
    setContactTags(prev => ({ ...prev, [userId]: data || [] }))
  }

  const toggleExpand = (name: string, userId: string) => {
    if (expanded === name) {
      setExpanded(null)
    } else {
      setExpanded(name)
      loadContactTags(userId)
    }
  }

  const createTag = async () => {
    if (!newTagName.trim()) return
    const res = await fetch('/api/tags', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: newTagName.trim(), color: ITS_YELLOW }),
    })
    if (res.ok) {
      const tag = await res.json()
      setTags(prev => [...prev, tag])
      setNewTagName('')
      setShowNewTag(false)
    }
  }

  const deleteTag = async (tagId: number) => {
    await fetch(`/api/tags?id=${tagId}`, { method: 'DELETE' })
    setTags(prev => prev.filter(t => t.id !== tagId))
    setContactTags(prev => {
      const next = { ...prev }
      for (const k in next) { next[k] = next[k].filter(t => t.id !== tagId) }
      return next
    })
  }

  const assignTag = async (contactUserId: string, tagId: number) => {
    await fetch('/api/contacts/tags', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ contact_user_id: contactUserId, tag_id: tagId }),
    })
    loadContactTags(contactUserId)
  }

  const removeTag = async (contactUserId: string, tagId: number) => {
    await fetch(`/api/contacts/tags?contact=${encodeURIComponent(contactUserId)}&tag=${tagId}`, { method: 'DELETE' })
    loadContactTags(contactUserId)
  }

  const multiPlatformCount = contacts.filter(c => c.platforms.length > 1).length

  const filtered = contacts.filter(c => {
    if (search && !c.display_name?.toLowerCase().includes(search.toLowerCase())) return false
    if (filter === 'multi' && c.platforms.length < 2) return false
    if (filter !== 'all' && filter !== 'multi' && !c.platforms.includes(filter)) return false
    return true
  })

  if (loading) return (
    <div className="flex items-center justify-center py-20">
      <div className="animate-spin w-8 h-8 border-2 border-t-transparent rounded-full" style={{ borderColor: ITS_YELLOW, borderTopColor: 'transparent' }} />
    </div>
  )

  return (
    <div className="space-y-5">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-white">Contacts</h1>
          <p className="text-gray-400 text-sm mt-1">{contacts.length} contacts · {multiPlatformCount} cross-platform</p>
        </div>
        <button
          onClick={() => setShowNewTag(!showNewTag)}
          className="px-3 py-1.5 rounded-lg text-sm font-medium text-gray-900"
          style={{ backgroundColor: ITS_YELLOW }}
        >
          + New Tag
        </button>
      </div>

      {/* New Tag Form */}
      {showNewTag && (
        <div className="bg-gray-950 rounded-lg border border-gray-800 p-4 flex gap-3 items-end">
          <div className="flex-1">
            <label className="text-xs text-gray-400 mb-1 block">Tag Name</label>
            <input
              value={newTagName}
              onChange={e => setNewTagName(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && createTag()}
              placeholder="e.g. LTL, Refrigerated, Power Only"
              className="w-full bg-gray-900 border border-gray-700 rounded-lg px-3 py-2 text-white text-sm focus:outline-none focus:ring-1"
              style={{ '--tw-ring-color': ITS_YELLOW } as any}
              autoFocus
            />
          </div>
          <button onClick={createTag} className="px-4 py-2 rounded-lg text-sm font-medium text-gray-900" style={{ backgroundColor: ITS_YELLOW }}>Create</button>
          <button onClick={() => setShowNewTag(false)} className="px-4 py-2 rounded-lg text-sm text-gray-400 hover:text-white bg-gray-800">Cancel</button>
        </div>
      )}

      {/* Tags bar */}
      {tags.length > 0 && (
        <div className="flex gap-2 flex-wrap">
          <span className="text-xs text-gray-500 self-center mr-1">Tags:</span>
          {tags.map(tag => (
            <button
              key={tag.id}
              onClick={() => setTagFilter(tagFilter === tag.id ? null : tag.id)}
              className={`text-xs px-2.5 py-1 rounded-full border font-medium transition-colors ${
                tagFilter === tag.id ? 'text-gray-900' : 'text-gray-300 border-gray-600 hover:border-gray-400'
              }`}
              style={tagFilter === tag.id ? { backgroundColor: tag.color, borderColor: tag.color } : undefined}
            >
              {tag.name}
              <span
                onClick={e => { e.stopPropagation(); deleteTag(tag.id) }}
                className="ml-1.5 opacity-50 hover:opacity-100 cursor-pointer"
              >×</span>
            </button>
          ))}
        </div>
      )}

      {/* Filters */}
      <div className="flex gap-2 flex-wrap">
        {[
          { key: 'all', label: `All (${contacts.length})` },
          { key: 'multi', label: `🔗 Cross-Platform (${multiPlatformCount})` },
          { key: 'telegram', label: 'Telegram' },
          { key: 'whatsapp', label: 'WhatsApp' },
          { key: 'facebook', label: 'Facebook' },
          { key: 'instagram', label: 'Instagram' },
        ].map(f => (
          <button
            key={f.key}
            onClick={() => setFilter(f.key)}
            className={`px-3 py-1.5 rounded-lg text-sm transition-colors ${
              filter === f.key ? 'text-gray-900 font-medium' : 'bg-gray-900 text-gray-300 hover:bg-gray-800 border border-gray-700'
            }`}
            style={filter === f.key ? { backgroundColor: ITS_YELLOW } : undefined}
          >
            {f.key !== 'all' && f.key !== 'multi' && <PlatformIcon platform={f.key} size={14} className="inline mr-1.5 -mt-0.5" />}
            {f.label}
          </button>
        ))}
      </div>

      <input
        type="text"
        placeholder="Search contacts..."
        value={search}
        onChange={e => setSearch(e.target.value)}
        className="w-full bg-gray-950 border border-gray-800 rounded-lg px-4 py-2.5 text-white placeholder-gray-500 focus:outline-none focus:ring-1"
      />

      {/* Contact List */}
      <div className="space-y-2">
        {filtered.length === 0 ? (
          <div className="text-center py-12 text-gray-500">No contacts match your filters</div>
        ) : filtered.map((contact, i) => {
          const primaryId = contact.identities?.[0]?.user_id || contact.display_name
          const cTags = contactTags[primaryId] || []
          return (
            <div key={i} className="bg-gray-950 rounded-xl border border-gray-800 overflow-hidden">
              <button
                onClick={() => toggleExpand(contact.display_name, primaryId)}
                className="w-full text-left px-5 py-3.5 hover:bg-gray-900/50 transition-colors flex items-center gap-3"
              >
                <div className="w-10 h-10 rounded-full flex items-center justify-center text-sm font-bold flex-shrink-0" style={{ backgroundColor: ITS_YELLOW, color: '#1a1a00' }}>
                  {contact.display_name?.charAt(0)?.toUpperCase() || '?'}
                </div>
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2 flex-wrap">
                    <span className="text-white font-medium truncate">{contact.display_name}</span>
                    {contact.platforms.length > 1 && (
                      <span className="text-[10px] px-1.5 py-0.5 rounded font-medium" style={{ backgroundColor: `${ITS_YELLOW}33`, color: ITS_YELLOW, border: `1px solid ${ITS_YELLOW}44` }}>
                        {contact.platforms.length} platforms
                      </span>
                    )}
                    {cTags.map(t => (
                      <span key={t.id} className="text-[10px] px-1.5 py-0.5 rounded border font-medium" style={{ backgroundColor: `${t.color}22`, color: t.color, borderColor: `${t.color}44` }}>
                        {t.name}
                      </span>
                    ))}
                  </div>
                  <div className="flex gap-1.5 mt-1">
                    {contact.platforms.map(p => (
                      <span key={p} className="flex items-center gap-1 text-[10px] text-gray-400">
                        <PlatformIcon platform={p} size={12} />
                        <span className="capitalize">{p}</span>
                      </span>
                    ))}
                  </div>
                </div>
                <span className="text-gray-500 text-sm">{expanded === contact.display_name ? '▲' : '▼'}</span>
              </button>

              {expanded === contact.display_name && (
                <div className="border-t border-gray-800 bg-black/30 px-5 py-3 space-y-3">
                  <p className="text-xs text-gray-500 uppercase tracking-wider font-medium">Platform Identities</p>
                  {contact.identities?.map((id, j) => (
                    <div key={j} className="flex items-center gap-3 py-1">
                      <PlatformIcon platform={id.platform} size={16} />
                      <span className="capitalize text-sm text-gray-300">{id.platform}</span>
                      <span className="text-gray-500 text-xs font-mono truncate">{id.user_id}</span>
                    </div>
                  ))}

                  {/* Tags management */}
                  <div className="pt-2 border-t border-gray-800">
                    <p className="text-xs text-gray-500 uppercase tracking-wider font-medium mb-2">Tags</p>
                    <div className="flex gap-2 flex-wrap">
                      {cTags.map(t => (
                        <span key={t.id} className="text-xs px-2 py-1 rounded-full border font-medium flex items-center gap-1" style={{ backgroundColor: `${t.color}22`, color: t.color, borderColor: `${t.color}44` }}>
                          {t.name}
                          <span onClick={() => removeTag(primaryId, t.id)} className="cursor-pointer opacity-60 hover:opacity-100">×</span>
                        </span>
                      ))}
                      {tags.filter(t => !cTags.some(ct => ct.id === t.id)).map(t => (
                        <button
                          key={t.id}
                          onClick={() => assignTag(primaryId, t.id)}
                          className="text-xs px-2 py-1 rounded-full border border-dashed border-gray-600 text-gray-500 hover:text-white hover:border-gray-400 transition-colors"
                        >
                          + {t.name}
                        </button>
                      ))}
                    </div>
                  </div>
                </div>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}

export default Database
