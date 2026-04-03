import { useEffect, useState } from 'react'
import PlatformIcon from '../components/PlatformIcon'
import ConversationView from '../components/ConversationView'

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

function Chat() {
  const [conversations, setConversations] = useState<Conversation[]>([])
  const [selectedPanes, setSelectedPanes] = useState<string[]>([]) // up to 2 conversation IDs
  const [loading, setLoading] = useState(true)
  const [showNewChat, setShowNewChat] = useState(false)
  const [contacts, setContacts] = useState<Contact[]>([])
  const [contactSearch, setContactSearch] = useState('')
  const [search, setSearch] = useState('')
  const [platformFilter, setPlatformFilter] = useState<string | null>(null)
  const [tags, setTags] = useState<Tag[]>([])
  const [tagFilters, setTagFilters] = useState<number[]>([])
  const [roomTags, setRoomTags] = useState<Record<string, Tag[]>>({})
  const [allContactTags, setAllContactTags] = useState<Record<string, Tag[]>>({})
  const [showAddContact, setShowAddContact] = useState(false)
  const [newContact, setNewContact] = useState({ display_name: '', platform: 'telegram', identifier: '' })
  const [addingContact, setAddingContact] = useState(false)

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
      const rt: Record<string, Tag[]> = {}
      for (const conv of (convos || [])) {
        const tagMap = new Map<number, Tag>()
        for (const mid of (conv.member_ids || [])) {
          for (const tag of (bulk[mid] || [])) tagMap.set(tag.id, tag)
        }
        if (tagMap.size > 0) rt[conv.room_id] = Array.from(tagMap.values())
      }
      setRoomTags(rt)
    }).finally(() => setLoading(false))
  }, [])

  const selectConversation = (roomId: string, split: boolean) => {
    if (split && selectedPanes.length >= 1) {
      // Add as second pane (or replace second)
      if (selectedPanes[0] === roomId) return // already in first pane
      setSelectedPanes([selectedPanes[0], roomId])
    } else {
      setSelectedPanes([roomId])
    }
  }

  const closePane = (index: number) => {
    setSelectedPanes(prev => prev.filter((_, i) => i !== index))
  }

  const filteredConversations = conversations.filter(conv => {
    if (platformFilter && conv.platform !== platformFilter) return false
    if (tagFilters.length > 0 && !tagFilters.every(tf => (roomTags[conv.room_id] || []).some(t => t.id === tf))) return false
    if (search) {
      const q = search.toLowerCase()
      return conv.name.toLowerCase().includes(q) || (conv.last_message && conv.last_message.toLowerCase().includes(q))
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
    if (d.toDateString() === now.toDateString()) return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
    return d.toLocaleDateString([], { month: 'short', day: 'numeric' }) + ' ' + d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  }

  const filteredContacts = contacts.filter(c => c.display_name.toLowerCase().includes(contactSearch.toLowerCase()))

  const startChat = async (contact: Contact) => {
    setShowNewChat(false)
    try {
      const res = await fetch('/api/conversations/new', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ user_id: contact.user_id }),
      })
      if (!res.ok) throw new Error(`${res.status}`)
      const data = await res.json()
      const convRes = await fetch('/api/conversations')
      if (convRes.ok) setConversations(await convRes.json() || [])
      if (data.room_id) selectConversation(data.room_id, false)
    } catch {}
  }

  const addContact = async () => {
    if (!newContact.display_name || !newContact.identifier) return
    setAddingContact(true)
    try {
      const res = await fetch('/api/contacts/add', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(newContact),
      })
      if (res.ok) {
        const contactsRes = await fetch('/api/contacts')
        if (contactsRes.ok) setContacts(await contactsRes.json() || [])
        setNewContact({ display_name: '', platform: 'telegram', identifier: '' })
        setShowAddContact(false)
      }
    } finally { setAddingContact(false) }
  }

  return (
    <div className="h-full flex">
      {/* Left Panel — Conversation list */}
      <div className="w-80 bg-gray-950 border-r border-gray-700 flex flex-col flex-shrink-0">
        <div className="p-3 border-b border-gray-700 space-y-2">
          <div className="flex items-center justify-between">
            <h3 className="text-sm font-medium text-gray-400">
              Chats {!loading && `(${filteredConversations.length})`}
            </h3>
            <button onClick={() => { setShowNewChat(true); if (contacts.length === 0) fetch('/api/contacts').then(r => r.json()).then(data => setContacts(data || [])) }}
              className="text-blue-400 hover:text-blue-300 text-sm font-medium">+ New</button>
          </div>
          <input type="text" value={search} onChange={(e) => setSearch(e.target.value)} placeholder="Search chats..."
            className="w-full bg-gray-800 border border-gray-600 rounded-lg px-3 py-1.5 text-sm text-white placeholder-gray-400 focus:outline-none focus:ring-1 focus:ring-blue-500" />
          <div className="flex gap-1.5 flex-wrap">
            <button onClick={() => setPlatformFilter(null)}
              className={`px-2 py-0.5 rounded-full text-xs transition-colors ${!platformFilter ? 'text-gray-900' : 'bg-gray-800 text-gray-400 hover:text-white'}`}
              style={!platformFilter ? { backgroundColor: '#f5d40c' } : undefined}>All</button>
            {activePlatforms.map(p => (
              <button key={p} onClick={() => setPlatformFilter(platformFilter === p ? null : p)}
                className={`px-2 py-0.5 rounded-full text-xs flex items-center gap-1 transition-colors ${platformFilter === p ? 'text-gray-900' : 'bg-gray-800 text-gray-400 hover:text-white'}`}
                style={platformFilter === p ? { backgroundColor: '#f5d40c' } : undefined}>
                <PlatformIcon platform={p} size={12} />{platformLabel(p)}
              </button>
            ))}
          </div>
          {tags.length > 0 && (
            <div className="flex gap-1.5 flex-wrap">
              {tags.map(t => (
                <button key={t.id} onClick={() => setTagFilters(prev => prev.includes(t.id) ? prev.filter(id => id !== t.id) : [...prev, t.id])}
                  className={`px-2 py-0.5 rounded-full text-xs transition-colors ${tagFilters.includes(t.id) ? 'text-gray-900 font-medium' : 'text-gray-500 hover:text-white border border-gray-700'}`}
                  style={tagFilters.includes(t.id) ? { backgroundColor: t.color } : undefined}>
                  🏷 {t.name}
                </button>
              ))}
            </div>
          )}
        </div>
        <div className="flex-1 overflow-y-auto">
          {loading && <p className="text-gray-500 text-sm p-4 text-center">Loading...</p>}
          {!loading && filteredConversations.length === 0 && (
            <p className="text-gray-500 text-sm p-4 text-center">{conversations.length === 0 ? 'No conversations yet.' : 'No matching conversations.'}</p>
          )}
          {filteredConversations.map((conv) => {
            const isSelected = selectedPanes.includes(conv.room_id)
            return (
              <div key={conv.room_id}
                onClick={() => selectConversation(conv.room_id, false)}
                onAuxClick={(e) => { if (e.button === 1) { e.preventDefault(); selectConversation(conv.room_id, true) } }}
                className={`px-3 py-2.5 border-b border-gray-700/50 cursor-pointer transition-colors ${isSelected ? 'bg-gray-800 border-l-4' : 'hover:bg-gray-800'}`}
                style={isSelected ? { borderLeftColor: '#f5d40c' } : undefined}
                title="Click to open · Middle-click to split"
              >
                <div className="flex items-center gap-3">
                  <div className="w-9 h-9 flex-shrink-0 flex items-center justify-center">
                    <PlatformIcon platform={conv.platform} size={32} />
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-1.5 min-w-0">
                        <p className="text-white font-medium text-sm truncate">{conv.name}</p>
                        {conv.member_count > 2 && (
                          <span className="text-[10px] px-1.5 py-0 rounded bg-gray-700 text-gray-400 flex-shrink-0">👥 {conv.member_count}</span>
                        )}
                      </div>
                      <div className="flex items-center gap-1 flex-shrink-0 ml-2">
                        {selectedPanes.length < 2 && selectedPanes[0] !== conv.room_id && selectedPanes.length > 0 && (
                          <button onClick={(e) => { e.stopPropagation(); selectConversation(conv.room_id, true) }}
                            className="text-gray-600 hover:text-blue-400 text-xs px-1" title="Open in split view">⫽</button>
                        )}
                        {conv.last_activity > 0 && (
                          <span className="text-gray-500 text-xs">{formatTime(conv.last_activity)}</span>
                        )}
                      </div>
                    </div>
                    <p className="text-gray-400 text-xs truncate">
                      {conv.last_message || `${conv.member_count} members · ${platformLabel(conv.platform)}`}
                    </p>
                    {(roomTags[conv.room_id] || []).length > 0 && (
                      <div className="flex gap-1 mt-0.5">
                        {(roomTags[conv.room_id] || []).map(t => (
                          <span key={t.id} className="text-[9px] px-1.5 py-0 rounded" style={{ backgroundColor: `${t.color}22`, color: t.color }}>{t.name}</span>
                        ))}
                      </div>
                    )}
                  </div>
                </div>
              </div>
            )
          })}
        </div>
      </div>

      {/* Right Panel — Conversation view(s) */}
      <div className="flex-1 flex min-w-0">
        {selectedPanes.length > 0 ? (
          selectedPanes.map((paneId, index) => {
            const conv = conversations.find(c => c.room_id === paneId)
            if (!conv) return null
            return (
              <div key={paneId} className={`flex-1 flex flex-col min-w-0 ${index > 0 ? 'border-l border-gray-700' : ''}`}>
                <ConversationView
                  conversationId={paneId}
                  conversation={conv}
                  tags={tags}
                  allContactTags={allContactTags}
                  onRefreshTags={fetchBulkTags}
                  canClose={selectedPanes.length > 1}
                  onClose={() => closePane(index)}
                />
              </div>
            )
          })
        ) : (
          <div className="flex-1 flex items-center justify-center">
            <div className="text-center">
              <div className="text-6xl mb-4">💬</div>
              <h3 className="text-xl text-white mb-2">Select a conversation</h3>
              <p className="text-gray-400">Choose a conversation to start chatting.</p>
              <p className="text-gray-600 text-sm mt-2">Middle-click or use ⫽ to open split view</p>
            </div>
          </div>
        )}
      </div>

      {/* New Chat Modal */}
      {showNewChat && (
        <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50" onClick={() => { setShowNewChat(false); setShowAddContact(false); setNewContact({ display_name: '', platform: 'telegram', identifier: '' }) }}>
          <div className="bg-gray-950 rounded-lg w-96 max-h-[70vh] flex flex-col" onClick={e => e.stopPropagation()}>
            <div className="p-4 border-b border-gray-700 flex items-center justify-between">
              <h3 className="text-white font-medium">New Chat</h3>
              <button onClick={() => { setShowNewChat(false); setShowAddContact(false) }} className="text-gray-400 hover:text-white">✕</button>
            </div>
            <div className="p-3 border-b border-gray-700">
              <input type="text" value={contactSearch} onChange={(e) => setContactSearch(e.target.value)} placeholder="Search contacts..." autoFocus
                className="w-full bg-gray-800 border border-gray-600 rounded-lg px-3 py-2 text-white placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500 text-sm" />
            </div>
            <div className="p-3 border-b border-gray-700">
              <button onClick={() => setShowAddContact(!showAddContact)}
                className="w-full flex items-center justify-center gap-2 py-2 px-3 rounded-lg border border-gray-600 text-gray-300 hover:text-white hover:border-gray-500 transition-colors text-sm">
                <span>+</span><span>Add New Contact</span>
              </button>
            </div>
            {showAddContact && (
              <div className="p-4 border-b border-gray-700 bg-gray-750">
                <h4 className="text-white font-medium mb-3 text-sm">Add New Contact</h4>
                <div className="space-y-3">
                  <input type="text" value={newContact.display_name} onChange={(e) => setNewContact({ ...newContact, display_name: e.target.value })}
                    placeholder="Contact name" className="w-full bg-gray-800 border border-gray-600 rounded-lg px-3 py-2 text-white placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500 text-sm" />
                  <select value={newContact.platform} onChange={(e) => setNewContact({ ...newContact, platform: e.target.value })}
                    className="w-full bg-gray-800 border border-gray-600 rounded-lg px-3 py-2 text-white focus:outline-none focus:ring-2 focus:ring-blue-500 text-sm">
                    <option value="telegram">Telegram</option><option value="whatsapp">WhatsApp</option><option value="signal">Signal</option>
                    <option value="messenger">Messenger</option><option value="instagram">Instagram</option>
                  </select>
                  <input type="text" value={newContact.identifier} onChange={(e) => setNewContact({ ...newContact, identifier: e.target.value })}
                    placeholder="Phone number or username" className="w-full bg-gray-800 border border-gray-600 rounded-lg px-3 py-2 text-white placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500 text-sm" />
                  <div className="flex gap-2">
                    <button onClick={() => setShowAddContact(false)} className="flex-1 py-2 px-3 rounded-lg border border-gray-600 text-gray-300 hover:text-white hover:border-gray-500 transition-colors text-sm">Cancel</button>
                    <button onClick={addContact} disabled={addingContact || !newContact.display_name || !newContact.identifier}
                      className="flex-1 py-2 px-3 rounded-lg text-gray-900 font-medium transition-colors text-sm disabled:opacity-50"
                      style={{ backgroundColor: addingContact || !newContact.display_name || !newContact.identifier ? '#6b7280' : '#f5d40c' }}>
                      {addingContact ? 'Adding...' : 'Add'}
                    </button>
                  </div>
                </div>
              </div>
            )}
            <div className="flex-1 overflow-y-auto">
              {filteredContacts.length === 0 && (
                <p className="text-gray-500 text-sm p-4 text-center">{contacts.length === 0 ? 'Loading contacts...' : 'No matching contacts.'}</p>
              )}
              {filteredContacts.map((contact) => (
                <div key={contact.user_id} onClick={() => startChat(contact)}
                  className="flex items-center gap-3 p-3 hover:bg-gray-800 cursor-pointer transition-colors">
                  <div className="w-9 h-9 bg-blue-600 rounded-full flex items-center justify-center text-sm text-white">{contact.display_name.charAt(0)}</div>
                  <div className="min-w-0 flex-1">
                    <p className="text-white text-sm truncate">{contact.display_name}</p>
                    <p className="text-gray-500 text-xs flex items-center gap-1"><PlatformIcon platform={contact.platform} size={14} />{platformLabel(contact.platform)}</p>
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
