import { useEffect, useRef, useState } from 'react'
import PlatformIcon from './PlatformIcon'
import LoadsPanel from './LoadsPanel'

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

interface ConversationViewProps {
  conversationId: string
  conversation: Conversation
  tags: Tag[]
  allContactTags: Record<string, Tag[]>
  onRefreshTags: () => Promise<void>
  canClose?: boolean
  onClose?: () => void
}

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

function ConversationView({ conversationId, conversation, tags, allContactTags, onRefreshTags, canClose, onClose }: ConversationViewProps) {
  const [messages, setMessages] = useState<Message[]>([])
  const [paginationToken, setPaginationToken] = useState<string | null>(null)
  const [hasMore, setHasMore] = useState(false)
  const [loadingMore, setLoadingMore] = useState(false)
  const [msgLoading, setMsgLoading] = useState(false)
  const scrollContainerRef = useRef<HTMLDivElement>(null)
  const [input, setInput] = useState('')
  const [sending, setSending] = useState(false)
  const [showMembers, setShowMembers] = useState(false)
  const [members, setMembers] = useState<{ user_id: string; display_name: string; platform?: string }[]>([])
  const [membersLoading, setMembersLoading] = useState(false)
  const [summary, setSummary] = useState<{ summary: string; topics: string[]; sentiment: string } | null>(null)
  const [summarizing, setSummarizing] = useState(false)
  const [showSummary, setShowSummary] = useState(false)
  const [selectedRoomTags, setSelectedRoomTags] = useState<Tag[]>([])
  const [showTagManager, setShowTagManager] = useState(false)
  const [showInlineTagInput, setShowInlineTagInput] = useState(false)
  const [newInlineTag, setNewInlineTag] = useState('')
  const [autoTagging, setAutoTagging] = useState(false)
  const [autoTagSuggestions, setAutoTagSuggestions] = useState<{ existing_tags: string[]; new_tags: string[] } | null>(null)
  const [memberTags, setMemberTags] = useState<Record<string, Tag[]>>({})
  const messagesEnd = useRef<HTMLDivElement>(null)
  const membersRef = useRef<HTMLDivElement>(null)
  const isLoadingOlderRef = useRef(false)

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

  const loadRoomTags = async () => {
    try {
      const membersRes = await fetch(`/api/conversations/${encodeURIComponent(conversationId)}/members`)
      if (!membersRes.ok) return
      const roomMembers = await membersRes.json()
      setSelectedRoomTags(getRoomTagsFromMembers(roomMembers))
    } catch { /* skip */ }
  }

  const loadMemberTags = async () => {
    const membersRes = await fetch(`/api/conversations/${encodeURIComponent(conversationId)}/members`)
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

  const loadMessages = async () => {
    setMsgLoading(true)
    setMessages([])
    setShowMembers(false)
    setShowSummary(false)
    setSummary(null)
    setSelectedRoomTags([])
    setShowTagManager(false)
    setAutoTagSuggestions(null)
    setMemberTags({})
    try {
      const [msgRes] = await Promise.all([
        fetch(`/api/conversations/${encodeURIComponent(conversationId)}/messages?limit=50`),
        loadRoomTags().catch(() => {}),
        loadMemberTags().catch(() => {}),
      ])
      if (msgRes.ok) {
        const data = await msgRes.json()
        setMessages(data.messages || data || [])
        setPaginationToken(data.end || null)
        setHasMore(data.has_more || false)
      }
    } catch { /* skip */ }
    setMsgLoading(false)
  }

  useEffect(() => { loadMessages() }, [conversationId])

  const loadOlderMessages = async () => {
    if (!paginationToken || loadingMore) return
    setLoadingMore(true)
    const container = scrollContainerRef.current
    const prevHeight = container?.scrollHeight || 0
    try {
      const res = await fetch(`/api/conversations/${encodeURIComponent(conversationId)}/messages?limit=50&from=${encodeURIComponent(paginationToken)}`)
      if (!res.ok) return
      const data = await res.json()
      const older = data.messages || data || []
      if (older.length > 0) {
        isLoadingOlderRef.current = true
        setMessages(prev => [...older, ...prev])
        setPaginationToken(data.end || null)
        setHasMore(data.has_more || false)
        requestAnimationFrame(() => {
          if (container) container.scrollTop = container.scrollHeight - prevHeight
        })
      } else {
        setHasMore(false)
      }
    } finally {
      setLoadingMore(false)
    }
  }

  useEffect(() => {
    if (isLoadingOlderRef.current) { isLoadingOlderRef.current = false; return }
    messagesEnd.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages])

  // Poll for new messages
  useEffect(() => {
    const interval = setInterval(async () => {
      try {
        const res = await fetch(`/api/conversations/${encodeURIComponent(conversationId)}/messages?limit=50`)
        if (res.ok) {
          const data = await res.json()
          const msgs = data.messages || data || []
          if (msgs.length !== messages.length) {
            setMessages(msgs)
            setPaginationToken(data.end || null)
            setHasMore(data.has_more || false)
          }
        }
      } catch {}
    }, 3000)
    return () => clearInterval(interval)
  }, [conversationId, messages.length])

  const sendMessage = async () => {
    if (!input.trim() || sending) return
    setSending(true)
    try {
      const res = await fetch(`/api/conversations/${encodeURIComponent(conversationId)}/send`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ body: input.trim() }),
      })
      if (res.ok) {
        setInput('')
        const msgRes = await fetch(`/api/conversations/${encodeURIComponent(conversationId)}/messages?limit=100`)
        if (msgRes.ok) { const d = await msgRes.json(); setMessages(d.messages || d || []); setPaginationToken(d.end || null); setHasMore(d.has_more || false) }
      }
    } finally { setSending(false) }
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); sendMessage() }
  }

  const toggleMembers = async () => {
    if (showMembers) { setShowMembers(false); return }
    setShowMembers(true)
    setMembersLoading(true)
    try {
      const res = await fetch(`/api/conversations/${encodeURIComponent(conversationId)}/members`)
      if (res.ok) setMembers(await res.json() || [])
    } finally { setMembersLoading(false) }
  }

  const summarize = async () => {
    setSummarizing(true); setShowSummary(true); setSummary(null)
    try {
      const res = await fetch('/api/ai/summarize', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ room_id: conversationId }),
      })
      const data = await res.json()
      if (data.error) throw new Error(data.error)
      setSummary(data)
    } catch (e: any) {
      setSummary({ summary: `Error: ${e.message}`, topics: [], sentiment: 'neutral' })
    } finally { setSummarizing(false) }
  }

  const autoTag = async () => {
    setAutoTagging(true); setAutoTagSuggestions(null)
    try {
      const res = await fetch('/api/ai/auto-tag', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ room_id: conversationId }),
      })
      const data = await res.json()
      if (data.error) throw new Error(data.error)
      setAutoTagSuggestions(data)
    } catch { /* skip */ } finally { setAutoTagging(false) }
  }

  const applyAutoTag = async (tagName: string, isNew: boolean) => {
    let tagId: number | null = null
    if (isNew) {
      const res = await fetch('/api/tags', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: tagName, color: '#f5d40c' }) })
      if (res.ok) { const t = await res.json(); tagId = t.id }
    } else {
      const existing = tags.find(t => t.name.toLowerCase() === tagName.toLowerCase())
      if (existing) tagId = existing.id
    }
    if (!tagId) return
    const membersRes = await fetch(`/api/conversations/${encodeURIComponent(conversationId)}/members`)
    if (!membersRes.ok) return
    const roomMembers = await membersRes.json()
    for (const m of (roomMembers || [])) {
      if (m.is_me) continue
      await fetch('/api/contacts/tags', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ contact_user_id: m.user_id, tag_id: tagId }) })
    }
    setAutoTagSuggestions(prev => prev ? { existing_tags: prev.existing_tags.filter(t => t !== tagName), new_tags: prev.new_tags.filter(t => t !== tagName) } : null)
    await onRefreshTags()
    loadRoomTags()
    loadMemberTags()
  }

  const assignTagToContact = async (contactId: string, tagId: number) => {
    await fetch('/api/contacts/tags', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ contact_user_id: contactId, tag_id: tagId }) })
    await onRefreshTags()
    loadMemberTags()
    loadRoomTags()
  }

  const removeTagFromContact = async (contactId: string, tagId: number) => {
    await fetch(`/api/contacts/tags?contact=${encodeURIComponent(contactId)}&tag=${tagId}`, { method: 'DELETE' })
    await onRefreshTags()
    loadMemberTags()
    loadRoomTags()
  }

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (membersRef.current && !membersRef.current.contains(e.target as Node)) setShowMembers(false)
    }
    if (showMembers) document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [showMembers])

  return (
    <div className="flex-1 flex flex-col min-w-0">
      {/* Header */}
      <div className="bg-gray-950 border-b border-gray-700 p-4 relative" ref={membersRef}>
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3 min-w-0">
            <PlatformIcon platform={conversation.platform} size={28} />
            <div className="min-w-0">
              <h3 className="text-white font-medium truncate">{conversation.name}</h3>
              <button onClick={toggleMembers} className="text-gray-400 text-sm hover:text-blue-400 transition-colors">
                {platformLabel(conversation.platform)} · {conversation.member_count} members
                <span className="ml-1 text-xs">{showMembers ? '▲' : '▼'}</span>
              </button>
            </div>
          </div>
          <div className="flex items-center gap-2 flex-shrink-0">
            <button onClick={summarize} disabled={summarizing}
              className="flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg text-gray-900 text-xs font-medium transition-all disabled:opacity-50"
              style={{ backgroundColor: '#f5d40c' }}>
              {summarizing ? <span className="animate-spin inline-block w-3 h-3 border-2 border-gray-900 border-t-transparent rounded-full" /> : <span>✨</span>}
              {summarizing ? 'Analyzing...' : 'AI Summary'}
            </button>
            <button onClick={autoTag} disabled={autoTagging}
              className="flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg text-xs font-medium transition-all disabled:opacity-50 bg-gray-800 text-gray-300 hover:bg-gray-700 border border-gray-700">
              {autoTagging ? <span className="animate-spin inline-block w-3 h-3 border-2 border-gray-300 border-t-transparent rounded-full" /> : <span>🏷</span>}
              {autoTagging ? 'Tagging...' : 'Auto-tag'}
            </button>
            {canClose && (
              <button onClick={onClose} className="text-gray-500 hover:text-white text-sm ml-1" title="Close pane">✕</button>
            )}
          </div>
        </div>
        {showMembers && (
          <div className="absolute top-full left-0 right-0 z-50 bg-gray-950 border border-gray-700 rounded-b-lg shadow-xl max-h-72 overflow-y-auto">
            {membersLoading && <p className="text-gray-500 text-sm p-3 text-center">Loading members...</p>}
            {!membersLoading && members.length === 0 && <p className="text-gray-500 text-sm p-3 text-center">No members found.</p>}
            {members.map(m => (
              <div key={m.user_id} className="flex items-center gap-3 px-4 py-2 hover:bg-gray-800/50">
                <div className="w-7 h-7 bg-gray-600 rounded-full flex items-center justify-center text-xs text-white">{m.display_name.charAt(0)}</div>
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
              <button key={t} onClick={() => applyAutoTag(t, false)}
                className="text-[11px] px-2.5 py-1 rounded-full font-medium transition-colors hover:opacity-80"
                style={{ backgroundColor: '#f5d40c22', color: '#f5d40c', border: '1px solid #f5d40c44' }}>✓ {t}</button>
            ))}
            {(autoTagSuggestions.new_tags || []).map(t => (
              <button key={t} onClick={() => applyAutoTag(t, true)}
                className="text-[11px] px-2.5 py-1 rounded-full font-medium transition-colors border border-dashed border-green-500/40 text-green-400 hover:bg-green-500/10">+ {t}</button>
            ))}
            {(autoTagSuggestions.existing_tags || []).length === 0 && (autoTagSuggestions.new_tags || []).length === 0 && (
              <span className="text-xs text-gray-500">No tags suggested</span>
            )}
          </div>
        </div>
      )}

      {/* Tags bar */}
      <div className="border-b border-gray-800 px-4 py-2 flex items-center gap-2 flex-wrap bg-black/20">
        <span className="text-[10px] text-gray-500 uppercase tracking-wider">Tags:</span>
        {selectedRoomTags.map(t => (
          <span key={t.id} className="text-[11px] px-2 py-0.5 rounded-full border font-medium" style={{ backgroundColor: `${t.color}22`, color: t.color, borderColor: `${t.color}44` }}>{t.name}</span>
        ))}
        {showInlineTagInput ? (
          <input value={newInlineTag} onChange={e => setNewInlineTag(e.target.value)}
            onKeyDown={async e => {
              if (e.key === 'Enter' && newInlineTag.trim()) {
                await fetch('/api/tags', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: newInlineTag.trim(), color: '#f5d40c' }) })
                setNewInlineTag(''); setShowInlineTagInput(false)
              }
              if (e.key === 'Escape') { setNewInlineTag(''); setShowInlineTagInput(false) }
            }}
            placeholder="Tag name..." className="bg-gray-900 border border-gray-700 rounded px-2 py-0.5 text-[11px] text-white w-24 focus:outline-none" autoFocus />
        ) : (
          <button onClick={() => setShowInlineTagInput(true)} className="text-[11px] w-5 h-5 rounded-full border border-dashed border-gray-700 text-gray-500 hover:text-white hover:border-gray-500 transition-colors flex items-center justify-center">+</button>
        )}
        <button onClick={() => setShowTagManager(!showTagManager)} className="text-[11px] px-2 py-0.5 rounded-full border border-dashed border-gray-700 text-gray-500 hover:text-white hover:border-gray-500 transition-colors">
          {showTagManager ? '✕ Close' : '✏️ Manage'}
        </button>
      </div>

      {/* Tag manager */}
      {showTagManager && (
        <div className="border-b border-gray-800 bg-black/30 px-4 py-3 space-y-2 max-h-48 overflow-y-auto">
          <p className="text-[10px] text-gray-500 uppercase tracking-wider font-medium">Tag contacts in this conversation</p>
          {Object.entries(memberTags).map(([userId, uTags]) => {
            const member = members.find(m => m.user_id === userId)
            const name = member?.display_name || userId.split(':')[0].replace('@', '').replace(/^(telegram_|whatsapp_|meta_|signal_)/, '')
            return (
              <div key={userId} className="flex items-center gap-2 flex-wrap py-1">
                <span className="text-xs text-gray-300 font-medium min-w-[120px] truncate">{name}</span>
                {(uTags || []).map(t => (
                  <span key={t.id} className="text-[10px] px-1.5 py-0.5 rounded-full border font-medium flex items-center gap-1" style={{ backgroundColor: `${t.color}22`, color: t.color, borderColor: `${t.color}44` }}>
                    {t.name} <span onClick={() => removeTagFromContact(userId, t.id)} className="cursor-pointer opacity-60 hover:opacity-100">×</span>
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

      {/* Loads panel */}
      <LoadsPanel conversationId={conversationId} />

      {/* AI Summary */}
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

      {/* Messages */}
      <div ref={scrollContainerRef} className="flex-1 p-4 overflow-y-auto space-y-3" onScroll={(e) => {
        const el = e.currentTarget
        if (el.scrollTop < 50 && hasMore && !loadingMore) loadOlderMessages()
      }}>
        {loadingMore && (
          <div className="flex items-center justify-center gap-2 py-3">
            <span className="animate-spin inline-block w-5 h-5 border-2 border-t-transparent rounded-full" style={{ borderColor: '#f5d40c', borderTopColor: 'transparent' }} />
            <span className="text-sm text-gray-400">Loading older messages...</span>
          </div>
        )}
        {hasMore && !loadingMore && (
          <button onClick={loadOlderMessages} className="w-full text-center py-3 text-sm text-gray-500 hover:text-gray-300 transition-colors">↑ Load older messages</button>
        )}
        {msgLoading && <p className="text-gray-500 text-center">Loading messages...</p>}
        {!msgLoading && messages.length === 0 && <p className="text-gray-500 text-center">No messages yet.</p>}
        {messages.map((msg) => (
          <div key={msg.event_id} className={`flex ${msg.is_me ? 'justify-end' : 'justify-start'}`}>
            <div className={`max-w-xs lg:max-w-md px-4 py-2 rounded-lg ${msg.is_me ? 'bg-blue-600 text-white' : 'bg-gray-800 text-white'}`}>
              {!msg.is_me && <p className="text-xs text-blue-300 mb-1">{msg.sender_name || msg.sender}</p>}
              <p className="whitespace-pre-wrap">{msg.body}</p>
              <p className="text-xs text-gray-400 mt-1">{formatTime(msg.timestamp)}</p>
            </div>
          </div>
        ))}
        <div ref={messagesEnd} />
      </div>

      {/* Message input */}
      <div className="bg-gray-950 border-t border-gray-700 p-4">
        <div className="flex gap-2">
          <input type="text" value={input} onChange={(e) => setInput(e.target.value)} onKeyDown={handleKeyDown}
            placeholder="Type a message..." disabled={sending}
            className="flex-1 bg-gray-800 border border-gray-600 rounded-lg px-4 py-2 text-white placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500 disabled:opacity-50" />
          <button onClick={sendMessage} disabled={sending || !input.trim()}
            className="disabled:opacity-50 px-6 py-2 rounded-lg text-gray-900 font-medium transition-colors"
            style={{ backgroundColor: '#f5d40c' }}>Send</button>
        </div>
      </div>
    </div>
  )
}

export default ConversationView
