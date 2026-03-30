import { useEffect, useRef, useState } from 'react'
import PlatformIcon from '../components/PlatformIcon'

interface Contact {
  user_id: string
  display_name: string
  telegram_id: string
  platform: string
}

interface Tag {
  id: number
  name: string
  color: string
}

interface Conversation {
  room_id: string
  name: string
  member_count: number
  member_ids?: string[]
  platform: string
  last_activity: number
  last_message?: string
}

interface Message {
  event_id: string
  sender: string
  sender_name: string
  body: string
  msg_type: string
  timestamp: number
  is_me?: boolean
}

function Chat() {
  const [conversations, setConversations] = useState<Conversation[]>([])
  const [selected, setSelected] = useState<string | null>(null)
  const [messages, setMessages] = useState<Message[]>([])
  const [loading, setLoading] = useState(true)
  const [msgLoading, setMsgLoading] = useState(false)
  const [input, setInput] = useState('')
  const [sending, setSending] = useState(false)
  const [showNewChat, setShowNewChat] = useState(false)
  const [contacts, setContacts] = useState<Contact[]>([])
  const [contactSearch, setContactSearch] = useState('')
  const [search, setSearch] = useState('')
  const [platformFilter, setPlatformFilter] = useState<string | null>(null)
  const [showMembers, setShowMembers] = useState(false)
  const [members, setMembers] = useState<{ user_id: string; display_name: string; platform?: string }[]>([])
  const [membersLoading, setMembersLoading] = useState(false)
  const [summary, setSummary] = useState<{ summary: string; topics: string[]; sentiment: string } | null>(null)
  const [summarizing, setSummarizing] = useState(false)
  const [showSummary, setShowSummary] = useState(false)
  const [tags, setTags] = useState<Tag[]>([])
  const [tagFilters, setTagFilters] = useState<number[]>([])
  const [roomTags, setRoomTags] = useState<Record<string, Tag[]>>({})
  const [selectedRoomTags, setSelectedRoomTags] = useState<Tag[]>([])
  const [newInlineTag, setNewInlineTag] = useState('')
  const [showInlineTagInput, setShowInlineTagInput] = useState(false)
  const [autoTagging, setAutoTagging] = useState(false)
  const [autoTagSuggestions, setAutoTagSuggestions] = useState<{ existing_tags: string[]; new_tags: string[] } | null>(null)
  const [showAddContact, setShowAddContact] = useState(false)
  const [newContact, setNewContact] = useState({
    display_name: '',
    platform: 'telegram',
    identifier: ''
  })
  const [addingContact, setAddingContact] = useState(false)
  const messagesEnd = useRef<HTMLDivElement>(null)
  const membersRef = useRef<HTMLDivElement>(null)

  // All contact→tag mappings (fetched once)
  const [allContactTags, setAllContactTags] = useState<Record<string, Tag[]>>({})

  const fetchBulkTags = async () => {
    try {
      const res = await fetch('/api/contacts/tags/bulk')
      if (res.ok) setAllContactTags(await res.json() || {})
    } catch { /* skip */ }
  }

  useEffect(() => {
    Promise.all([
      fetch('/api/conversations').then(r => r.json()),
      fetch('/api/tags').then(r => r.json()),
      fetch('/api/contacts/tags/bulk').then(r => r.json()),
    ]).then(([convos, t, bulk]) => {
      setConversations(convos || [])
      setTags(t || [])
      setAllContactTags(bulk || {})
      // Build roomTags from member_ids + bulk contact tags
      const rt: Record<string, Tag[]> = {}
      for (const conv of (convos || [])) {
        const tagMap = new Map<number, Tag>()
        for (const mid of (conv.member_ids || [])) {
          for (const tag of (bulk[mid] || [])) {
            tagMap.set(tag.id, tag)
          }
        }
        if (tagMap.size > 0) rt[conv.room_id] = Array.from(tagMap.values())
      }
      setRoomTags(rt)
    }).finally(() => setLoading(false))
  }, [])

  // Derive room tags from members + bulk contact tags (no extra API calls)
  const getRoomTagsFromMembers = (roomMembers: any[]): Tag[] => {
    const tagMap = new Map<number, Tag>()
    for (const m of roomMembers) {
      if (m.is_me) continue
      for (const t of (allContactTags[m.user_id] || [])) {
        tagMap.set(t.id, t)
      }
    }
    return Array.from(tagMap.values())
  }

  const loadRoomTags = async (roomId: string) => {
    try {
      const membersRes = await fetch(`/api/conversations/${encodeURIComponent(roomId)}/members`)
      if (!membersRes.ok) return
      const roomMembers = await membersRes.json()
      const tags = getRoomTagsFromMembers(roomMembers)
      setSelectedRoomTags(tags)
      setRoomTags(prev => ({ ...prev, [roomId]: tags }))
    } catch { /* skip */ }
  }

  const autoTag = async () => {
    if (!selected) return
    setAutoTagging(true)
    setAutoTagSuggestions(null)
    try {
      const res = await fetch('/api/ai/auto-tag', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ room_id: selected }),
      })
      const data = await res.json()
      if (data.error) throw new Error(data.error)
      setAutoTagSuggestions(data)
    } catch (e: any) {
      console.error('Auto-tag failed:', e)
    } finally {
      setAutoTagging(false)
    }
  }

  const applyAutoTag = async (tagName: string, isNew: boolean) => {
    if (!selected) return
    let tagId: number | null = null
    if (isNew) {
      // Create the tag first
      const res = await fetch('/api/tags', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: tagName, color: '#f5d40c' }),
      })
      if (res.ok) {
        const t = await res.json()
        tagId = t.id
        setTags(prev => [...prev, t])
      }
    } else {
      const existing = tags.find(t => t.name.toLowerCase() === tagName.toLowerCase())
      if (existing) tagId = existing.id
    }
    if (!tagId) return
    // Assign to non-self members
    const membersRes = await fetch(`/api/conversations/${encodeURIComponent(selected)}/members`)
    if (!membersRes.ok) return
    const roomMembers = await membersRes.json()
    for (const m of (roomMembers || [])) {
      if (m.is_me) continue
      await fetch('/api/contacts/tags', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ contact_user_id: m.user_id, tag_id: tagId }),
      })
    }
    // Remove from suggestions
    setAutoTagSuggestions(prev => prev ? {
      existing_tags: prev.existing_tags.filter(t => t !== tagName),
      new_tags: prev.new_tags.filter(t => t !== tagName),
    } : null)
    await fetchBulkTags()
    loadRoomTags(selected)
    loadMemberTags(selected)
  }

  const [memberTags, setMemberTags] = useState<Record<string, Tag[]>>({})
  const [showTagManager, setShowTagManager] = useState(false)

  const loadMemberTags = async (roomId: string) => {
    const membersRes = await fetch(`/api/conversations/${encodeURIComponent(roomId)}/members`)
    if (!membersRes.ok) return
    const roomMembers = await membersRes.json()
    const mt: Record<string, Tag[]> = {}
    setMembers(roomMembers || [])
    for (const m of (roomMembers || [])) {
      if (m.is_me) continue
      mt[m.user_id] = allContactTags[m.user_id] || []
    }
    setMemberTags(mt)
  }

  const assignTagToContact = async (contactId: string, tagId: number) => {
    await fetch('/api/contacts/tags', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ contact_user_id: contactId, tag_id: tagId }),
    })
    await fetchBulkTags()
    if (selected) { loadMemberTags(selected); loadRoomTags(selected) }
  }

  const removeTagFromContact = async (contactId: string, tagId: number) => {
    await fetch(`/api/contacts/tags?contact=${encodeURIComponent(contactId)}&tag=${tagId}`, { method: 'DELETE' })
    await fetchBulkTags()
    if (selected) { loadMemberTags(selected); loadRoomTags(selected) }
  }

  const loadMessages = async (roomID: string) => {
    setSelected(roomID)
    setMsgLoading(true)
    setMessages([])
    setShowMembers(false)
    setShowSummary(false)
    setSummary(null)
    setSelectedRoomTags([])
    setShowTagManager(false)
    setAutoTagSuggestions(null)
    setMemberTags({})
    loadRoomTags(roomID)
    loadMemberTags(roomID)
    try {
      const res = await fetch(`/api/conversations/${encodeURIComponent(roomID)}/messages?limit=100`)
      if (!res.ok) throw new Error(`${res.status}`)
      const data = await res.json()
      setMessages(data || [])
    } finally {
      setMsgLoading(false)
    }
  }

  useEffect(() => {
    messagesEnd.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages])

  // Poll for new messages every 3s
  useEffect(() => {
    if (!selected) return
    const interval = setInterval(async () => {
      try {
        const res = await fetch(`/api/conversations/${encodeURIComponent(selected)}/messages?limit=100`)
        if (res.ok) {
          const data = await res.json()
          if (data && data.length !== messages.length) {
            setMessages(data)
          }
        }
      } catch {}
    }, 3000)
    return () => clearInterval(interval)
  }, [selected, messages.length])

  const sendMessage = async () => {
    if (!input.trim() || !selected || sending) return
    setSending(true)
    try {
      const res = await fetch(`/api/conversations/${encodeURIComponent(selected)}/send`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ body: input.trim() }),
      })
      if (res.ok) {
        setInput('')
        // Reload messages
        const msgRes = await fetch(`/api/conversations/${encodeURIComponent(selected)}/messages?limit=100`)
        if (msgRes.ok) setMessages(await msgRes.json() || [])
      }
    } finally {
      setSending(false)
    }
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      sendMessage()
    }
  }

  const startChat = async (contact: Contact) => {
    setShowNewChat(false)
    try {
      const res = await fetch('/api/conversations/new', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ user_id: contact.user_id }),
      })
      if (!res.ok) throw new Error(`${res.status}`)
      const data = await res.json()
      // Refresh conversation list and select the new room
      const convRes = await fetch('/api/conversations')
      if (convRes.ok) setConversations(await convRes.json() || [])
      if (data.room_id) loadMessages(data.room_id)
    } catch {}
  }

  const filteredContacts = contacts.filter(c =>
    c.display_name.toLowerCase().includes(contactSearch.toLowerCase())
  )

  const addContact = async () => {
    if (!newContact.display_name || !newContact.identifier) return
    
    setAddingContact(true)
    try {
      const res = await fetch('/api/contacts/add', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(newContact),
      })
      if (res.ok) {
        // Refresh contacts list
        const contactsRes = await fetch('/api/contacts')
        if (contactsRes.ok) setContacts(await contactsRes.json() || [])
        
        // Reset form
        setNewContact({ display_name: '', platform: 'telegram', identifier: '' })
        setShowAddContact(false)
      } else {
        const error = await res.json()
        alert(`Failed to add contact: ${error.error}`)
      }
    } catch (e) {
      alert('Failed to add contact')
    } finally {
      setAddingContact(false)
    }
  }

  // Close members dropdown on click outside
  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (membersRef.current && !membersRef.current.contains(e.target as Node)) {
        setShowMembers(false)
      }
    }
    if (showMembers) document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [showMembers])

  const summarize = async () => {
    if (!selected) return
    setSummarizing(true)
    setShowSummary(true)
    setSummary(null)
    try {
      const res = await fetch('/api/ai/summarize', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ room_id: selected }),
      })
      const data = await res.json()
      if (data.error) throw new Error(data.error)
      setSummary(data)
    } catch (e: any) {
      setSummary({ summary: `Error: ${e.message}`, topics: [], sentiment: 'neutral' })
    } finally {
      setSummarizing(false)
    }
  }

  const toggleMembers = async () => {
    if (showMembers) { setShowMembers(false); return }
    if (!selected) return
    setShowMembers(true)
    setMembersLoading(true)
    try {
      const res = await fetch(`/api/conversations/${encodeURIComponent(selected)}/members`)
      if (res.ok) setMembers(await res.json() || [])
    } finally {
      setMembersLoading(false)
    }
  }

  const filteredConversations = conversations.filter(conv => {
    if (platformFilter && conv.platform !== platformFilter) return false
    if (tagFilters.length > 0 && !tagFilters.every(tf => (roomTags[conv.room_id] || []).some(t => t.id === tf))) return false
    if (search) {
      const q = search.toLowerCase()
      return conv.name.toLowerCase().includes(q) ||
        (conv.last_message && conv.last_message.toLowerCase().includes(q))
    }
    return true
  })

  const activePlatforms = [...new Set(conversations.map(c => c.platform).filter(Boolean))]

  const platformLabel = (p: string) => {
    switch (p) {
      case 'telegram': return 'Telegram'
      case 'whatsapp': return 'WhatsApp'
      case 'signal': return 'Signal'
      case 'facebook': case 'messenger': return 'Messenger'
      case 'instagram': return 'Instagram'
      default: return p || 'Unknown'
    }
  }

  const formatTime = (ts: number) => {
    const d = new Date(ts)
    const now = new Date()
    if (d.toDateString() === now.toDateString()) {
      return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
    }
    return d.toLocaleDateString([], { month: 'short', day: 'numeric' }) + ' ' +
      d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  }

  const selectedConvo = conversations.find(c => c.room_id === selected)

  return (
    <div className="h-full flex">
      {/* Left Panel */}
      <div className="w-80 bg-gray-950 border-r border-gray-700 flex flex-col">
        <div className="p-3 border-b border-gray-700 space-y-2">
          <div className="flex items-center justify-between">
            <h3 className="text-sm font-medium text-gray-400">
              Chats {!loading && `(${filteredConversations.length})`}
            </h3>
            <button
              onClick={() => {
                setShowNewChat(true)
                if (contacts.length === 0) {
                  fetch('/api/contacts').then(r => r.json()).then(data => setContacts(data || []))
                }
              }}
              className="text-blue-400 hover:text-blue-300 text-sm font-medium"
            >
              + New
            </button>
          </div>
          <input
            type="text"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search chats..."
            className="w-full bg-gray-800 border border-gray-600 rounded-lg px-3 py-1.5 text-sm text-white placeholder-gray-400 focus:outline-none focus:ring-1 focus:ring-blue-500"
          />
          <div className="flex gap-1.5 flex-wrap">
            <button
              onClick={() => setPlatformFilter(null)}
              className={`px-2 py-0.5 rounded-full text-xs transition-colors ${
                !platformFilter ? 'text-gray-900' : 'bg-gray-800 text-gray-400 hover:text-white'
              }`}
              style={!platformFilter ? { backgroundColor: '#f5d40c' } : undefined}
            >
              All
            </button>
            {activePlatforms.map(p => (
              <button
                key={p}
                onClick={() => setPlatformFilter(platformFilter === p ? null : p)}
                className={`px-2 py-0.5 rounded-full text-xs flex items-center gap-1 transition-colors ${
                  platformFilter === p ? 'text-gray-900' : 'bg-gray-800 text-gray-400 hover:text-white'
                }`}
                style={platformFilter === p ? { backgroundColor: '#f5d40c' } : undefined}
              >
                <PlatformIcon platform={p} size={12} />
                {platformLabel(p)}
              </button>
            ))}
          </div>
          {tags.length > 0 && (
            <div className="flex gap-1.5 flex-wrap">
              {tags.map(t => (
                <button
                  key={t.id}
                  onClick={() => setTagFilters(prev => prev.includes(t.id) ? prev.filter(id => id !== t.id) : [...prev, t.id])}
                  className={`px-2 py-0.5 rounded-full text-xs transition-colors ${
                    tagFilters.includes(t.id) ? 'text-gray-900 font-medium' : 'text-gray-500 hover:text-white border border-gray-700'
                  }`}
                  style={tagFilters.includes(t.id) ? { backgroundColor: t.color } : undefined}
                >
                  🏷 {t.name}
                </button>
              ))}
            </div>
          )}
        </div>
        <div className="flex-1 overflow-y-auto">
          {loading && (
            <p className="text-gray-500 text-sm p-4 text-center">Loading...</p>
          )}
          {!loading && filteredConversations.length === 0 && (
            <p className="text-gray-500 text-sm p-4 text-center">
              {conversations.length === 0
                ? 'No conversations yet.'
                : 'No matching conversations.'}
            </p>
          )}
          {filteredConversations.map((conv) => (
            <div
              key={conv.room_id}
              onClick={() => loadMessages(conv.room_id)}
              className={`px-3 py-2.5 border-b border-gray-700/50 cursor-pointer transition-colors ${
                selected === conv.room_id ? 'bg-gray-800 border-l-4' : 'hover:bg-gray-800'
              }`}
              style={selected === conv.room_id ? { borderLeftColor: '#f5d40c' } : undefined}
            >
              <div className="flex items-center gap-3">
                <div className="w-9 h-9 flex-shrink-0 flex items-center justify-center">
                  <PlatformIcon platform={conv.platform} size={32} />
                </div>
                <div className="min-w-0 flex-1">
                  <div className="flex items-center justify-between">
                    <p className="text-white font-medium text-sm truncate">{conv.name}</p>
                    {conv.last_activity > 0 && (
                      <span className="text-gray-500 text-xs flex-shrink-0 ml-2">
                        {formatTime(conv.last_activity)}
                      </span>
                    )}
                  </div>
                  <p className="text-gray-400 text-xs truncate">
                    {conv.last_message || `${conv.member_count} members · ${platformLabel(conv.platform)}`}
                  </p>
                  {(roomTags[conv.room_id] || []).length > 0 && (
                    <div className="flex gap-1 mt-0.5">
                      {(roomTags[conv.room_id] || []).map(t => (
                        <span key={t.id} className="text-[9px] px-1.5 py-0 rounded" style={{ backgroundColor: `${t.color}22`, color: t.color }}>
                          {t.name}
                        </span>
                      ))}
                    </div>
                  )}
                </div>
              </div>
            </div>
          ))}
        </div>
      </div>

      {/* Right Panel */}
      <div className="flex-1 flex flex-col">
        {selected && selectedConvo ? (
          <>
            <div className="bg-gray-950 border-b border-gray-700 p-4 relative" ref={membersRef}>
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-3">
                  <PlatformIcon platform={selectedConvo.platform} size={28} />
                  <div>
                    <h3 className="text-white font-medium">{selectedConvo.name}</h3>
                    <button
                      onClick={toggleMembers}
                      className="text-gray-400 text-sm hover:text-blue-400 transition-colors"
                    >
                      {platformLabel(selectedConvo.platform)} · {selectedConvo.member_count} members
                      <span className="ml-1 text-xs">{showMembers ? '▲' : '▼'}</span>
                    </button>
                  </div>
                </div>
                <button
                  onClick={summarize}
                  disabled={summarizing}
                  className="flex items-center gap-2 px-3 py-1.5 rounded-lg text-gray-900 text-sm font-medium transition-all disabled:opacity-50"
                  style={{ backgroundColor: '#f5d40c' }}
                >
                  {summarizing ? (
                    <span className="animate-spin inline-block w-4 h-4 border-2 border-gray-900 border-t-transparent rounded-full" />
                  ) : (
                    <span>✨</span>
                  )}
                  {summarizing ? 'Analyzing...' : 'AI Summary'}
                </button>
                <button
                  onClick={autoTag}
                  disabled={autoTagging}
                  className="flex items-center gap-2 px-3 py-1.5 rounded-lg text-sm font-medium transition-all disabled:opacity-50 bg-gray-800 text-gray-300 hover:bg-gray-700 border border-gray-700"
                >
                  {autoTagging ? (
                    <span className="animate-spin inline-block w-4 h-4 border-2 border-gray-300 border-t-transparent rounded-full" />
                  ) : (
                    <span>🏷</span>
                  )}
                  {autoTagging ? 'Tagging...' : 'Auto-tag'}
                </button>
              </div>
              {showMembers && (
                <div className="absolute top-full left-0 right-0 z-50 bg-gray-950 border border-gray-700 rounded-b-lg shadow-xl max-h-72 overflow-y-auto">
                  {membersLoading && (
                    <p className="text-gray-500 text-sm p-3 text-center">Loading members...</p>
                  )}
                  {!membersLoading && members.length === 0 && (
                    <p className="text-gray-500 text-sm p-3 text-center">No members found.</p>
                  )}
                  {members.map(m => (
                    <div key={m.user_id} className="flex items-center gap-3 px-4 py-2 hover:bg-gray-800/50">
                      <div className="w-7 h-7 bg-gray-600 rounded-full flex items-center justify-center text-xs text-white">
                        {m.display_name.charAt(0)}
                      </div>
                      <span className="text-white text-sm flex-1 truncate">{m.display_name}</span>
                      {m.platform && <PlatformIcon platform={m.platform} size={14} />}
                    </div>
                  ))}
                </div>
              )}
            </div>

            {/* Auto-tag suggestions */}
            {autoTagSuggestions && (
              <div className="border-b border-gray-800 bg-gradient-to-r from-yellow-900/10 to-gray-900/10 px-4 py-2.5">
                <div className="flex items-start justify-between mb-2">
                  <span className="text-xs text-gray-400 font-medium">🏷 AI suggested tags:</span>
                  <button onClick={() => setAutoTagSuggestions(null)} className="text-gray-500 hover:text-gray-300 text-xs">✕</button>
                </div>
                <div className="flex gap-2 flex-wrap">
                  {(autoTagSuggestions.existing_tags || []).map(t => (
                    <button
                      key={t}
                      onClick={() => applyAutoTag(t, false)}
                      className="text-[11px] px-2.5 py-1 rounded-full font-medium transition-colors hover:opacity-80"
                      style={{ backgroundColor: '#f5d40c22', color: '#f5d40c', border: '1px solid #f5d40c44' }}
                    >
                      ✓ {t}
                    </button>
                  ))}
                  {(autoTagSuggestions.new_tags || []).map(t => (
                    <button
                      key={t}
                      onClick={() => applyAutoTag(t, true)}
                      className="text-[11px] px-2.5 py-1 rounded-full font-medium transition-colors border border-dashed border-green-500/40 text-green-400 hover:bg-green-500/10"
                    >
                      + {t}
                    </button>
                  ))}
                  {(autoTagSuggestions.existing_tags || []).length === 0 && (autoTagSuggestions.new_tags || []).length === 0 && (
                    <span className="text-xs text-gray-500">No tags suggested for this conversation</span>
                  )}
                </div>
              </div>
            )}

            {/* Tags bar */}
            {selected && (
              <div className="border-b border-gray-800 px-4 py-2 flex items-center gap-2 flex-wrap bg-black/20">
                <span className="text-[10px] text-gray-500 uppercase tracking-wider">Tags:</span>
                {selectedRoomTags.map(t => (
                  <span key={t.id} className="text-[11px] px-2 py-0.5 rounded-full border font-medium" style={{ backgroundColor: `${t.color}22`, color: t.color, borderColor: `${t.color}44` }}>
                    {t.name}
                  </span>
                ))}
                {showInlineTagInput ? (
                  <span className="flex items-center gap-1">
                    <input
                      value={newInlineTag}
                      onChange={e => setNewInlineTag(e.target.value)}
                      onKeyDown={async e => {
                        if (e.key === 'Enter' && newInlineTag.trim()) {
                          const res = await fetch('/api/tags', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: newInlineTag.trim(), color: '#f5d40c' }) })
                          if (res.ok) { const t = await res.json(); setTags(prev => [...prev, t]) }
                          setNewInlineTag(''); setShowInlineTagInput(false)
                        }
                        if (e.key === 'Escape') { setNewInlineTag(''); setShowInlineTagInput(false) }
                      }}
                      placeholder="Tag name..."
                      className="bg-gray-900 border border-gray-700 rounded px-2 py-0.5 text-[11px] text-white w-24 focus:outline-none"
                      autoFocus
                    />
                  </span>
                ) : (
                  <button
                    onClick={() => setShowInlineTagInput(true)}
                    className="text-[11px] w-5 h-5 rounded-full border border-dashed border-gray-700 text-gray-500 hover:text-white hover:border-gray-500 transition-colors flex items-center justify-center"
                  >+</button>
                )}
                <button
                  onClick={() => setShowTagManager(!showTagManager)}
                  className="text-[11px] px-2 py-0.5 rounded-full border border-dashed border-gray-700 text-gray-500 hover:text-white hover:border-gray-500 transition-colors"
                >
                  {showTagManager ? '✕ Close' : '✏️ Manage'}
                </button>
              </div>
            )}
            {/* Tag manager panel */}
            {showTagManager && selected && (
              <div className="border-b border-gray-800 bg-black/30 px-4 py-3 space-y-2 max-h-48 overflow-y-auto">
                <p className="text-[10px] text-gray-500 uppercase tracking-wider font-medium">Tag contacts in this conversation</p>
                {Object.keys(memberTags).length === 0 && (
                  <p className="text-xs text-gray-500">Loading contacts...</p>
                )}
                {Object.entries(memberTags).map(([userId, uTags]) => {
                  const member = members.find(m => m.user_id === userId)
                  const name = member?.display_name || userId.split(':')[0].replace('@', '').replace(/^(telegram_|whatsapp_|meta_|signal_)/, '')
                  return (
                    <div key={userId} className="flex items-center gap-2 flex-wrap py-1">
                      <span className="text-xs text-gray-300 font-medium min-w-[120px] truncate">{name}</span>
                      {(uTags || []).map(t => (
                        <span key={t.id} className="text-[10px] px-1.5 py-0.5 rounded-full border font-medium flex items-center gap-1" style={{ backgroundColor: `${t.color}22`, color: t.color, borderColor: `${t.color}44` }}>
                          {t.name}
                          <span onClick={() => removeTagFromContact(userId, t.id)} className="cursor-pointer opacity-60 hover:opacity-100">×</span>
                        </span>
                      ))}
                      {tags.filter(t => !(uTags || []).some(ut => ut.id === t.id)).map(t => (
                        <button key={t.id} onClick={() => assignTagToContact(userId, t.id)} className="text-[10px] px-1.5 py-0.5 rounded-full border border-dashed border-gray-700 text-gray-500 hover:text-white hover:border-gray-500 transition-colors">+ {t.name}</button>
                      ))}
                    </div>
                  )
                })}
              </div>
            )}

            {showSummary && (
              <div className="border-b border-gray-700 bg-gradient-to-r from-purple-900/20 to-blue-900/20 p-4">
                {summarizing ? (
                  <div className="flex items-center gap-3 text-purple-300">
                    <span className="animate-spin inline-block w-4 h-4 border-2 border-purple-400 border-t-transparent rounded-full" />
                    <span className="text-sm">Analyzing conversation with AI...</span>
                  </div>
                ) : summary ? (
                  <div className="space-y-2">
                    <div className="flex items-start justify-between">
                      <span className="text-white font-semibold text-sm">✨ AI Analysis</span>
                      <button onClick={() => setShowSummary(false)} className="text-gray-500 hover:text-gray-300 text-xs">✕</button>
                    </div>
                    <p className="text-gray-200 text-sm leading-relaxed">{summary.summary}</p>
                    <div className="flex flex-wrap gap-2">
                      {summary.sentiment && (
                        <span className={`text-xs px-2 py-0.5 rounded-full border ${
                          summary.sentiment === 'positive' ? 'bg-emerald-500/20 text-emerald-300 border-emerald-500/30' :
                          summary.sentiment === 'negative' ? 'bg-red-500/20 text-red-300 border-red-500/30' :
                          summary.sentiment === 'urgent' ? 'bg-amber-500/20 text-amber-300 border-amber-500/30' :
                          'bg-gray-500/20 text-gray-300 border-gray-500/30'
                        }`}>
                          {summary.sentiment === 'positive' ? '😊' : summary.sentiment === 'negative' ? '😟' : summary.sentiment === 'urgent' ? '🚨' : '😐'} {summary.sentiment}
                        </span>
                      )}
                      {summary.topics?.map((t, i) => (
                        <span key={i} className="text-xs px-2 py-0.5 rounded-full bg-gray-800/50 text-gray-300 border border-gray-600">{t}</span>
                      ))}
                    </div>
                  </div>
                ) : null}
              </div>
            )}

            <div className="flex-1 p-4 overflow-y-auto space-y-3">
              {msgLoading && <p className="text-gray-500 text-center">Loading messages...</p>}
              {!msgLoading && messages.length === 0 && (
                <p className="text-gray-500 text-center">No messages yet.</p>
              )}
              {messages.map((msg) => {
                const isMe = !!msg.is_me
                return (
                  <div key={msg.event_id} className={`flex ${isMe ? 'justify-end' : 'justify-start'}`}>
                    <div className={`max-w-xs lg:max-w-md px-4 py-2 rounded-lg ${
                      isMe ? 'bg-blue-600 text-white' : 'bg-gray-800 text-white'
                    }`}>
                      {!isMe && (
                        <p className="text-xs text-blue-300 mb-1">{msg.sender_name || msg.sender}</p>
                      )}
                      <p className="whitespace-pre-wrap">{msg.body}</p>
                      <p className="text-xs text-gray-400 mt-1">{formatTime(msg.timestamp)}</p>
                    </div>
                  </div>
                )
              })}
              <div ref={messagesEnd} />
            </div>

            {/* Message input */}
            <div className="bg-gray-950 border-t border-gray-700 p-4">
              <div className="flex gap-2">
                <input
                  type="text"
                  value={input}
                  onChange={(e) => setInput(e.target.value)}
                  onKeyDown={handleKeyDown}
                  placeholder="Type a message..."
                  disabled={sending}
                  className="flex-1 bg-gray-800 border border-gray-600 rounded-lg px-4 py-2 text-white placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500 disabled:opacity-50"
                />
                <button
                  onClick={sendMessage}
                  disabled={sending || !input.trim()}
                  className="disabled:opacity-50 px-6 py-2 rounded-lg text-gray-900 font-medium transition-colors"
                  style={{ backgroundColor: '#f5d40c' }}
                >
                  Send
                </button>
              </div>
            </div>
          </>
        ) : (
          <div className="flex-1 flex items-center justify-center">
            <div className="text-center">
              <div className="text-6xl mb-4">💬</div>
              <h3 className="text-xl text-white mb-2">Select a conversation</h3>
              <p className="text-gray-400">Choose a conversation to start chatting.</p>
            </div>
          </div>
        )}
      </div>

      {/* New Chat Modal */}
      {showNewChat && (
        <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50" onClick={() => {
          setShowNewChat(false)
          setShowAddContact(false)
          setNewContact({ display_name: '', platform: 'telegram', identifier: '' })
        }}>
          <div className="bg-gray-950 rounded-lg w-96 max-h-[70vh] flex flex-col" onClick={e => e.stopPropagation()}>
            <div className="p-4 border-b border-gray-700 flex items-center justify-between">
              <h3 className="text-white font-medium">New Chat</h3>
              <button 
                onClick={() => {
                  setShowNewChat(false)
                  setShowAddContact(false)
                  setNewContact({ display_name: '', platform: 'telegram', identifier: '' })
                }} 
                className="text-gray-400 hover:text-white"
              >
                ✕
              </button>
            </div>
            <div className="p-3 border-b border-gray-700">
              <input
                type="text"
                value={contactSearch}
                onChange={(e) => setContactSearch(e.target.value)}
                placeholder="Search contacts..."
                autoFocus
                className="w-full bg-gray-800 border border-gray-600 rounded-lg px-3 py-2 text-white placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500 text-sm"
              />
            </div>
            {/* Add New Contact Button */}
            <div className="p-3 border-b border-gray-700">
              <button
                onClick={() => setShowAddContact(!showAddContact)}
                className="w-full flex items-center justify-center gap-2 py-2 px-3 rounded-lg border border-gray-600 text-gray-300 hover:text-white hover:border-gray-500 transition-colors text-sm"
              >
                <span>+</span>
                <span>Add New Contact</span>
              </button>
            </div>

            {/* Add Contact Form */}
            {showAddContact && (
              <div className="p-4 border-b border-gray-700 bg-gray-750">
                <h4 className="text-white font-medium mb-3 text-sm">Add New Contact</h4>
                <div className="space-y-3">
                  <div>
                    <input
                      type="text"
                      value={newContact.display_name}
                      onChange={(e) => setNewContact({ ...newContact, display_name: e.target.value })}
                      placeholder="Contact name"
                      className="w-full bg-gray-800 border border-gray-600 rounded-lg px-3 py-2 text-white placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500 text-sm"
                    />
                  </div>
                  <div>
                    <select
                      value={newContact.platform}
                      onChange={(e) => setNewContact({ ...newContact, platform: e.target.value })}
                      className="w-full bg-gray-800 border border-gray-600 rounded-lg px-3 py-2 text-white focus:outline-none focus:ring-2 focus:ring-blue-500 text-sm"
                    >
                      <option value="telegram">Telegram</option>
                      <option value="whatsapp">WhatsApp</option>
                      <option value="signal">Signal</option>
                      <option value="messenger">Messenger</option>
                      <option value="instagram">Instagram</option>
                    </select>
                  </div>
                  <div>
                    <input
                      type="text"
                      value={newContact.identifier}
                      onChange={(e) => setNewContact({ ...newContact, identifier: e.target.value })}
                      placeholder={newContact.platform === 'telegram' ? 'Phone number or username' : 'Phone number or username'}
                      className="w-full bg-gray-800 border border-gray-600 rounded-lg px-3 py-2 text-white placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500 text-sm"
                    />
                  </div>
                  <div className="flex gap-2">
                    <button
                      onClick={() => setShowAddContact(false)}
                      className="flex-1 py-2 px-3 rounded-lg border border-gray-600 text-gray-300 hover:text-white hover:border-gray-500 transition-colors text-sm"
                    >
                      Cancel
                    </button>
                    <button
                      onClick={addContact}
                      disabled={addingContact || !newContact.display_name || !newContact.identifier}
                      className="flex-1 py-2 px-3 rounded-lg text-gray-900 font-medium transition-colors text-sm disabled:opacity-50 disabled:cursor-not-allowed"
                      style={{ backgroundColor: addingContact || !newContact.display_name || !newContact.identifier ? '#6b7280' : '#f5d40c' }}
                    >
                      {addingContact ? 'Adding...' : 'Add'}
                    </button>
                  </div>
                </div>
              </div>
            )}

            <div className="flex-1 overflow-y-auto">
              {filteredContacts.length === 0 && (
                <p className="text-gray-500 text-sm p-4 text-center">
                  {contacts.length === 0 ? 'Loading contacts...' : 'No matching contacts.'}
                </p>
              )}
              {filteredContacts.map((contact) => (
                <div
                  key={contact.user_id}
                  onClick={() => startChat(contact)}
                  className="flex items-center gap-3 p-3 hover:bg-gray-800 cursor-pointer transition-colors"
                >
                  <div className="w-9 h-9 bg-blue-600 rounded-full flex items-center justify-center text-sm text-white">
                    {contact.display_name.charAt(0)}
                  </div>
                  <div className="min-w-0 flex-1">
                    <p className="text-white text-sm truncate">{contact.display_name}</p>
                    <p className="text-gray-500 text-xs flex items-center gap-1">
                      <PlatformIcon platform={contact.platform} size={14} />
                      {platformLabel(contact.platform)}
                    </p>
                  </div>
                </div>
              ))}
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

export default Chat
