import { useEffect, useRef, useState } from 'react'
import PlatformIcon from '../components/PlatformIcon'

interface Contact {
  user_id: string
  display_name: string
  telegram_id: string
  platform: string
}

interface Conversation {
  room_id: string
  name: string
  member_count: number
  platform: string
}

interface Message {
  event_id: string
  sender: string
  sender_name: string
  body: string
  msg_type: string
  timestamp: number
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
  const messagesEnd = useRef<HTMLDivElement>(null)

  useEffect(() => {
    fetch('/api/conversations')
      .then(r => r.json())
      .then(data => setConversations(data || []))
      .finally(() => setLoading(false))
  }, [])

  const loadMessages = async (roomID: string) => {
    setSelected(roomID)
    setMsgLoading(true)
    setMessages([])
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
      <div className="w-80 bg-gray-800 border-r border-gray-700 flex flex-col">
        <div className="p-4 border-b border-gray-700 flex items-center justify-between">
          <h3 className="text-sm font-medium text-gray-400">
            Conversations {!loading && `(${conversations.length})`}
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
        <div className="flex-1 overflow-y-auto">
          {loading && (
            <p className="text-gray-500 text-sm p-4 text-center">Loading...</p>
          )}
          {!loading && conversations.length === 0 && (
            <p className="text-gray-500 text-sm p-4 text-center">
              No conversations yet. Send a message on Telegram to create a portal.
            </p>
          )}
          {conversations.map((conv) => (
            <div
              key={conv.room_id}
              onClick={() => loadMessages(conv.room_id)}
              className={`p-4 border-b border-gray-700 cursor-pointer transition-colors ${
                selected === conv.room_id ? 'bg-blue-600' : 'hover:bg-gray-700'
              }`}
            >
              <div className="flex items-center gap-3">
                <div className="w-10 h-10 flex items-center justify-center">
                  <PlatformIcon platform={conv.platform} size={36} />
                </div>
                <div className="min-w-0 flex-1">
                  <p className="text-white font-medium truncate">{conv.name}</p>
                  <p className="text-gray-400 text-xs">
                    {conv.member_count} members · {platformLabel(conv.platform)}
                  </p>
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
            <div className="bg-gray-800 border-b border-gray-700 p-4">
              <div className="flex items-center gap-3">
                <PlatformIcon platform={selectedConvo.platform} size={28} />
                <div>
                  <h3 className="text-white font-medium">{selectedConvo.name}</h3>
                  <p className="text-gray-400 text-sm">{platformLabel(selectedConvo.platform)} · {selectedConvo.member_count} members</p>
                </div>
              </div>
            </div>

            <div className="flex-1 p-4 overflow-y-auto space-y-3">
              {msgLoading && <p className="text-gray-500 text-center">Loading messages...</p>}
              {!msgLoading && messages.length === 0 && (
                <p className="text-gray-500 text-center">No messages yet.</p>
              )}
              {messages.map((msg) => {
                const isMe = msg.sender === '@admin:chat.kayushkin.com'
                return (
                  <div key={msg.event_id} className={`flex ${isMe ? 'justify-end' : 'justify-start'}`}>
                    <div className={`max-w-xs lg:max-w-md px-4 py-2 rounded-lg ${
                      isMe ? 'bg-blue-600 text-white' : 'bg-gray-700 text-white'
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
            <div className="bg-gray-800 border-t border-gray-700 p-4">
              <div className="flex gap-2">
                <input
                  type="text"
                  value={input}
                  onChange={(e) => setInput(e.target.value)}
                  onKeyDown={handleKeyDown}
                  placeholder="Type a message..."
                  disabled={sending}
                  className="flex-1 bg-gray-700 border border-gray-600 rounded-lg px-4 py-2 text-white placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500 disabled:opacity-50"
                />
                <button
                  onClick={sendMessage}
                  disabled={sending || !input.trim()}
                  className="bg-blue-600 hover:bg-blue-700 disabled:opacity-50 px-6 py-2 rounded-lg text-white transition-colors"
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
        <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50" onClick={() => setShowNewChat(false)}>
          <div className="bg-gray-800 rounded-lg w-96 max-h-[70vh] flex flex-col" onClick={e => e.stopPropagation()}>
            <div className="p-4 border-b border-gray-700 flex items-center justify-between">
              <h3 className="text-white font-medium">New Chat</h3>
              <button onClick={() => setShowNewChat(false)} className="text-gray-400 hover:text-white">✕</button>
            </div>
            <div className="p-3 border-b border-gray-700">
              <input
                type="text"
                value={contactSearch}
                onChange={(e) => setContactSearch(e.target.value)}
                placeholder="Search contacts..."
                autoFocus
                className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-white placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500 text-sm"
              />
            </div>
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
                  className="flex items-center gap-3 p-3 hover:bg-gray-700 cursor-pointer transition-colors"
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
