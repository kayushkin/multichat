import { useEffect, useState } from 'react'

interface Conversation {
  room_id: string
  name: string
  platform: string
  last_message: string
  last_activity: number
  member_count: number
}

interface Message {
  event_id: string
  sender: string
  sender_name: string
  body: string
  msg_type: string
  timestamp: number
  is_me: boolean
}

interface AISummary {
  summary: string
  topics: string[]
  sentiment: string
}

function Inbox() {
  const [conversations, setConversations] = useState<Conversation[]>([])
  const [selectedConv, setSelectedConv] = useState<Conversation | null>(null)
  const [messages, setMessages] = useState<Message[]>([])
  const [loading, setLoading] = useState(true)
  const [messagesLoading, setMessagesLoading] = useState(false)
  const [summary, setSummary] = useState<AISummary | null>(null)
  const [summaryLoading, setSummaryLoading] = useState(false)
  const [searchTerm, setSearchTerm] = useState('')

  const fetchConversations = async () => {
    setLoading(true)
    try {
      const res = await fetch('/api/conversations')
      if (!res.ok) throw new Error('Failed to fetch conversations')
      const data = await res.json()
      setConversations(data || [])
    } catch (e) {
      console.error('Error fetching conversations:', e)
    } finally {
      setLoading(false)
    }
  }

  const fetchMessages = async (roomId: string) => {
    setMessagesLoading(true)
    try {
      const res = await fetch(`/api/conversations/${encodeURIComponent(roomId)}/messages`)
      if (!res.ok) throw new Error('Failed to fetch messages')
      const data = await res.json()
      setMessages(data || [])
    } catch (e) {
      console.error('Error fetching messages:', e)
      setMessages([])
    } finally {
      setMessagesLoading(false)
    }
  }

  const summarizeConversation = async () => {
    if (!selectedConv) return
    
    setSummaryLoading(true)
    try {
      const res = await fetch('/api/ai/summarize', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ room_id: selectedConv.room_id })
      })
      
      if (!res.ok) {
        const error = await res.text()
        throw new Error(error)
      }
      
      const data = await res.json()
      setSummary(data)
    } catch (e: any) {
      console.error('Error summarizing:', e)
      alert(`Failed to summarize: ${e.message}`)
    } finally {
      setSummaryLoading(false)
    }
  }

  useEffect(() => { fetchConversations() }, [])

  const handleConversationSelect = (conv: Conversation) => {
    setSelectedConv(conv)
    setSummary(null)
    fetchMessages(conv.room_id)
  }

  const filteredConversations = conversations.filter(conv =>
    conv.name.toLowerCase().includes(searchTerm.toLowerCase()) ||
    conv.platform.toLowerCase().includes(searchTerm.toLowerCase())
  )

  const formatTime = (timestamp: number) => {
    const date = new Date(timestamp)
    const now = new Date()
    const diffHours = (now.getTime() - date.getTime()) / (1000 * 60 * 60)
    
    if (diffHours < 24) {
      return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
    } else {
      return date.toLocaleDateString()
    }
  }

  return (
    <div className="h-full flex">
      {/* Left Panel — Conversation list */}
      <div className="w-80 bg-gray-800 border-r border-gray-700 flex flex-col">
        <div className="p-4 border-b border-gray-700">
          <input
            type="text"
            placeholder="Search conversations..."
            value={searchTerm}
            onChange={(e) => setSearchTerm(e.target.value)}
            className="w-full bg-gray-700 border border-gray-600 rounded-md px-3 py-2 text-white placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500"
          />
        </div>
        
        <div className="flex-1 overflow-y-auto">
          {loading ? (
            <div className="flex items-center justify-center py-8">
              <p className="text-gray-500">Loading conversations...</p>
            </div>
          ) : filteredConversations.length === 0 ? (
            <div className="flex items-center justify-center py-8">
              <p className="text-gray-500 text-sm px-4 text-center">
                {searchTerm ? 'No matching conversations' : 'No conversations yet. Connect a platform to see your messages.'}
              </p>
            </div>
          ) : (
            <div className="space-y-1 p-2">
              {filteredConversations.map((conv) => (
                <div
                  key={conv.room_id}
                  onClick={() => handleConversationSelect(conv)}
                  className={`p-3 rounded-lg cursor-pointer transition-colors ${
                    selectedConv?.room_id === conv.room_id
                      ? 'bg-blue-600/30 border border-blue-500'
                      : 'hover:bg-gray-700'
                  }`}
                >
                  <div className="flex items-center justify-between mb-1">
                    <h3 className="text-white font-medium truncate">{conv.name}</h3>
                    <div className={`w-2 h-2 rounded-full ${
                      conv.platform === 'telegram' ? 'bg-blue-500' :
                      conv.platform === 'whatsapp' ? 'bg-green-500' :
                      conv.platform === 'facebook' ? 'bg-blue-600' :
                      'bg-purple-500'
                    }`} />
                  </div>
                  <p className="text-gray-400 text-xs truncate mb-1">
                    {conv.last_message || 'No recent message'}
                  </p>
                  <div className="flex justify-between items-center">
                    <span className="text-gray-500 text-xs capitalize">{conv.platform}</span>
                    <span className="text-gray-500 text-xs">{formatTime(conv.last_activity)}</span>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>

      {/* Right Panel — Message thread */}
      <div className="flex-1 flex flex-col">
        {!selectedConv ? (
          <div className="flex-1 flex items-center justify-center">
            <div className="text-center">
              <div className="text-6xl mb-4">💬</div>
              <h3 className="text-xl text-white mb-2">No conversation selected</h3>
              <p className="text-gray-400">Select a conversation to view messages.</p>
            </div>
          </div>
        ) : (
          <>
            {/* Header */}
            <div className="bg-gray-800 border-b border-gray-700 p-4 flex items-center justify-between">
              <div>
                <h2 className="text-white font-semibold">{selectedConv.name}</h2>
                <p className="text-gray-400 text-sm capitalize">{selectedConv.platform} • {selectedConv.member_count} members</p>
              </div>
              <button
                onClick={summarizeConversation}
                disabled={summaryLoading}
                className="bg-purple-600 hover:bg-purple-700 disabled:opacity-50 px-4 py-2 rounded-lg text-white transition-colors flex items-center gap-2"
              >
                <span>✨</span>
                {summaryLoading ? 'Summarizing...' : 'Summarize'}
              </button>
            </div>

            {/* AI Summary */}
            {summary && (
              <div className="bg-purple-900/30 border-b border-purple-500/30 p-4">
                <h3 className="text-purple-300 font-semibold mb-2">AI Summary</h3>
                <p className="text-gray-300 mb-3">{summary.summary}</p>
                <div className="flex gap-4">
                  <div>
                    <span className="text-purple-400 text-sm font-medium">Topics: </span>
                    <span className="text-gray-300 text-sm">{summary.topics.join(', ')}</span>
                  </div>
                  <div>
                    <span className="text-purple-400 text-sm font-medium">Sentiment: </span>
                    <span className={`text-sm capitalize ${
                      summary.sentiment === 'positive' ? 'text-green-400' :
                      summary.sentiment === 'negative' ? 'text-red-400' :
                      'text-gray-400'
                    }`}>{summary.sentiment}</span>
                  </div>
                </div>
              </div>
            )}

            {/* Messages */}
            <div className="flex-1 overflow-y-auto p-4">
              {messagesLoading ? (
                <div className="flex items-center justify-center py-8">
                  <p className="text-gray-500">Loading messages...</p>
                </div>
              ) : messages.length === 0 ? (
                <div className="flex items-center justify-center py-8">
                  <p className="text-gray-500">No messages in this conversation.</p>
                </div>
              ) : (
                <div className="space-y-3">
                  {messages.map((msg) => (
                    <div key={msg.event_id} className={`flex ${msg.is_me ? 'justify-end' : 'justify-start'}`}>
                      <div className={`max-w-lg rounded-lg p-3 ${
                        msg.is_me 
                          ? 'bg-blue-600 text-white' 
                          : 'bg-gray-700 text-gray-100'
                      }`}>
                        {!msg.is_me && (
                          <p className="text-gray-300 text-xs mb-1">{msg.sender_name}</p>
                        )}
                        <p>{msg.body}</p>
                        <p className={`text-xs mt-1 ${msg.is_me ? 'text-blue-200' : 'text-gray-400'}`}>
                          {formatTime(msg.timestamp)}
                        </p>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </>
        )}
      </div>
    </div>
  )
}

export default Inbox
