import { useEffect, useState } from 'react'

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

function Inbox() {
  const [conversations, setConversations] = useState<Conversation[]>([])
  const [selected, setSelected] = useState<string | null>(null)
  const [messages, setMessages] = useState<Message[]>([])
  const [loading, setLoading] = useState(true)
  const [msgLoading, setMsgLoading] = useState(false)
  const [, setError] = useState('')

  useEffect(() => {
    fetch('/api/conversations')
      .then(r => r.json())
      .then(data => setConversations(data || []))
      .catch(e => setError(e.message))
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
    } catch (e: any) {
      setError(e.message)
    } finally {
      setMsgLoading(false)
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
              No conversations yet. Send a message on Telegram to create one.
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
                  <p className="text-gray-400 text-sm">{selectedConvo.member_count} members</p>
                </div>
              </div>
            </div>

            <div className="flex-1 p-4 overflow-y-auto space-y-3">
              {msgLoading && <p className="text-gray-500 text-center">Loading messages...</p>}
              {!msgLoading && messages.length === 0 && (
                <p className="text-gray-500 text-center">No messages in this conversation.</p>
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
            </div>
          </>
        ) : (
          <div className="flex-1 flex items-center justify-center">
            <div className="text-center">
              <div className="text-6xl mb-4">💬</div>
              <h3 className="text-xl text-white mb-2">Select a conversation</h3>
              <p className="text-gray-400">Choose a conversation to view messages.</p>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

export default Inbox
