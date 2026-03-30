import { useEffect, useState } from 'react'

interface Conversation {
  room_id: string
  name: string
  member_count: number
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
  is_me: boolean
}

interface AISummary {
  summary: string
  topics: string[]
  sentiment: string
}

const platformColors: Record<string, string> = {
  telegram: 'bg-sky-500',
  whatsapp: 'bg-emerald-500',
  facebook: 'bg-blue-600',
  instagram: 'bg-pink-500',
  signal: 'bg-blue-400',
}

const platformBadgeColors: Record<string, string> = {
  telegram: 'bg-sky-500/20 text-sky-300 border-sky-500/30',
  whatsapp: 'bg-emerald-500/20 text-emerald-300 border-emerald-500/30',
  facebook: 'bg-blue-600/20 text-blue-300 border-blue-600/30',
  instagram: 'bg-pink-500/20 text-pink-300 border-pink-500/30',
  signal: 'bg-blue-400/20 text-blue-200 border-blue-400/30',
}

const sentimentColors: Record<string, string> = {
  positive: 'bg-emerald-500/20 text-emerald-300 border-emerald-500/30',
  negative: 'bg-red-500/20 text-red-300 border-red-500/30',
  neutral: 'bg-gray-500/20 text-gray-300 border-gray-500/30',
  urgent: 'bg-amber-500/20 text-amber-300 border-amber-500/30',
}

const sentimentEmoji: Record<string, string> = {
  positive: '😊',
  negative: '😟',
  neutral: '😐',
  urgent: '🚨',
}

function timeAgo(ts: number): string {
  if (!ts) return ''
  const now = Date.now()
  const diff = now - ts
  const mins = Math.floor(diff / 60000)
  if (mins < 1) return 'just now'
  if (mins < 60) return `${mins}m ago`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours}h ago`
  const days = Math.floor(hours / 24)
  if (days < 7) return `${days}d ago`
  return new Date(ts).toLocaleDateString()
}

function Inbox() {
  const [conversations, setConversations] = useState<Conversation[]>([])
  const [selected, setSelected] = useState<Conversation | null>(null)
  const [messages, setMessages] = useState<Message[]>([])
  const [loadingConvos, setLoadingConvos] = useState(true)
  const [loadingMsgs, setLoadingMsgs] = useState(false)
  const [search, setSearch] = useState('')
  const [summary, setSummary] = useState<AISummary | null>(null)
  const [summarizing, setSummarizing] = useState(false)
  const [showSummary, setShowSummary] = useState(false)

  useEffect(() => {
    fetch('/api/conversations')
      .then(r => r.json())
      .then(setConversations)
      .catch(console.error)
      .finally(() => setLoadingConvos(false))
  }, [])

  const selectConversation = async (conv: Conversation) => {
    setSelected(conv)
    setMessages([])
    setSummary(null)
    setShowSummary(false)
    setLoadingMsgs(true)
    try {
      const res = await fetch(`/api/conversations/${encodeURIComponent(conv.room_id)}/messages`)
      const data = await res.json()
      setMessages(data || [])
    } catch (e) {
      console.error(e)
    } finally {
      setLoadingMsgs(false)
    }
  }

  const summarize = async () => {
    if (!selected) return
    setSummarizing(true)
    setShowSummary(true)
    setSummary(null)
    try {
      const res = await fetch('/api/ai/summarize', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ room_id: selected.room_id }),
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

  const filtered = conversations.filter(c =>
    c.name?.toLowerCase().includes(search.toLowerCase()) ||
    c.last_message?.toLowerCase().includes(search.toLowerCase())
  )

  return (
    <div className="h-full flex -m-6">
      {/* Conversation List */}
      <div className="w-80 bg-gray-800/50 border-r border-gray-700 flex flex-col flex-shrink-0">
        <div className="p-3 border-b border-gray-700">
          <input
            type="text"
            placeholder="Search conversations..."
            value={search}
            onChange={e => setSearch(e.target.value)}
            className="w-full bg-gray-700/50 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500/50 focus:border-blue-500"
          />
        </div>
        <div className="flex-1 overflow-y-auto">
          {loadingConvos ? (
            <div className="flex items-center justify-center py-12">
              <div className="animate-spin w-6 h-6 border-2 border-blue-500 border-t-transparent rounded-full" />
            </div>
          ) : filtered.length === 0 ? (
            <p className="text-gray-500 text-sm text-center p-4">No conversations found</p>
          ) : (
            filtered.map(conv => (
              <button
                key={conv.room_id}
                onClick={() => selectConversation(conv)}
                className={`w-full text-left px-4 py-3 border-b border-gray-700/50 hover:bg-gray-700/30 transition-colors ${
                  selected?.room_id === conv.room_id ? 'bg-gray-700/50 border-l-2 border-l-blue-500' : ''
                }`}
              >
                <div className="flex items-center gap-2.5">
                  <div className={`w-2 h-2 rounded-full flex-shrink-0 ${platformColors[conv.platform] || 'bg-gray-500'}`} />
                  <span className="text-white font-medium text-sm truncate">{conv.name}</span>
                  <span className="text-gray-500 text-xs ml-auto flex-shrink-0">{timeAgo(conv.last_activity)}</span>
                </div>
                {conv.last_message && (
                  <p className="text-gray-400 text-xs mt-1 truncate pl-4.5 ml-[18px]">{conv.last_message}</p>
                )}
              </button>
            ))
          )}
        </div>
      </div>

      {/* Message Thread */}
      <div className="flex-1 flex flex-col min-w-0">
        {selected ? (
          <>
            {/* Header */}
            <div className="px-5 py-3 border-b border-gray-700 flex items-center justify-between bg-gray-800/30">
              <div className="flex items-center gap-3">
                <div className={`w-3 h-3 rounded-full ${platformColors[selected.platform] || 'bg-gray-500'}`} />
                <div>
                  <h3 className="text-white font-semibold">{selected.name}</h3>
                  <span className={`text-xs px-2 py-0.5 rounded-full border ${platformBadgeColors[selected.platform] || 'bg-gray-700 text-gray-300'}`}>
                    {selected.platform} · {selected.member_count} members
                  </span>
                </div>
              </div>
              <button
                onClick={summarize}
                disabled={summarizing}
                className="flex items-center gap-2 px-4 py-2 bg-gradient-to-r from-purple-600 to-blue-600 hover:from-purple-500 hover:to-blue-500 disabled:opacity-50 rounded-lg text-white text-sm font-medium transition-all shadow-lg shadow-purple-900/20"
              >
                {summarizing ? (
                  <div className="animate-spin w-4 h-4 border-2 border-white border-t-transparent rounded-full" />
                ) : (
                  <span>✨</span>
                )}
                {summarizing ? 'Analyzing...' : 'AI Summary'}
              </button>
            </div>

            {/* AI Summary Panel */}
            {showSummary && (
              <div className="border-b border-gray-700 bg-gradient-to-r from-purple-900/20 to-blue-900/20 p-4">
                {summarizing ? (
                  <div className="flex items-center gap-3 text-purple-300">
                    <div className="animate-spin w-5 h-5 border-2 border-purple-400 border-t-transparent rounded-full" />
                    <span className="text-sm">Analyzing conversation with AI...</span>
                  </div>
                ) : summary ? (
                  <div className="space-y-3">
                    <div className="flex items-start justify-between">
                      <h4 className="text-white font-semibold text-sm flex items-center gap-2">
                        ✨ AI Analysis
                      </h4>
                      <button onClick={() => setShowSummary(false)} className="text-gray-500 hover:text-gray-300 text-xs">✕ Close</button>
                    </div>
                    <p className="text-gray-200 text-sm leading-relaxed">{summary.summary}</p>
                    <div className="flex flex-wrap gap-2">
                      {summary.sentiment && (
                        <span className={`text-xs px-2.5 py-1 rounded-full border font-medium ${sentimentColors[summary.sentiment] || sentimentColors.neutral}`}>
                          {sentimentEmoji[summary.sentiment] || '😐'} {summary.sentiment}
                        </span>
                      )}
                      {summary.topics?.map((topic, i) => (
                        <span key={i} className="text-xs px-2.5 py-1 rounded-full bg-gray-700/50 text-gray-300 border border-gray-600">
                          {topic}
                        </span>
                      ))}
                    </div>
                  </div>
                ) : null}
              </div>
            )}

            {/* Messages */}
            <div className="flex-1 overflow-y-auto p-5 space-y-3">
              {loadingMsgs ? (
                <div className="flex items-center justify-center py-12">
                  <div className="animate-spin w-6 h-6 border-2 border-blue-500 border-t-transparent rounded-full" />
                </div>
              ) : messages.length === 0 ? (
                <div className="text-center py-12">
                  <p className="text-gray-500">No messages in this conversation</p>
                </div>
              ) : (
                messages.map(msg => (
                  <div key={msg.event_id} className={`flex ${msg.is_me ? 'justify-end' : 'justify-start'}`}>
                    <div className={`max-w-[70%] rounded-2xl px-4 py-2.5 ${
                      msg.is_me
                        ? 'bg-blue-600 text-white rounded-br-md'
                        : 'bg-gray-700/70 text-gray-100 rounded-bl-md'
                    }`}>
                      {!msg.is_me && (
                        <p className="text-xs font-medium text-blue-300 mb-1">{msg.sender_name}</p>
                      )}
                      <p className="text-sm leading-relaxed whitespace-pre-wrap break-words">{msg.body}</p>
                      <p className={`text-[10px] mt-1 ${msg.is_me ? 'text-blue-200/60' : 'text-gray-500'}`}>
                        {new Date(msg.timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
                      </p>
                    </div>
                  </div>
                ))
              )}
            </div>
          </>
        ) : (
          <div className="flex-1 flex items-center justify-center">
            <div className="text-center">
              <div className="text-6xl mb-4">💬</div>
              <h3 className="text-xl text-white mb-2">Select a conversation</h3>
              <p className="text-gray-400">Choose a conversation from the sidebar to view messages</p>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

export default Inbox
