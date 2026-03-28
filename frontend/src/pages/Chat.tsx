import { useEffect, useRef, useState } from 'react'

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

  const platformIcon = (p: string) => p === 'whatsapp' ? '📱' : p === 'telegram' ? '✈️' : '🔗'

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
        <div className="p-4 border-b border-gray-700">
          <h3 className="text-sm font-medium text-gray-400">
            Conversations {!loading && `(${conversations.length})`}
          </h3>
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
                <div className="w-10 h-10 bg-gray-600 rounded-full flex items-center justify-center text-lg">
                  {platformIcon(conv.platform)}
                </div>
                <div className="min-w-0 flex-1">
                  <p className="text-white font-medium truncate">{conv.name}</p>
                  <p className="text-gray-400 text-xs">
                    {conv.member_count} members
                    {conv.platform && ` · ${conv.platform}`}
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
                <span className="text-xl">{platformIcon(selectedConvo.platform)}</span>
                <div>
                  <h3 className="text-white font-medium">{selectedConvo.name}</h3>
                  <p className="text-gray-400 text-sm">{selectedConvo.platform} · {selectedConvo.member_count} members</p>
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
    </div>
  )
}

export default Chat
