import { useQuery } from '@tanstack/react-query'
import { getMessages, sendMessage } from '../api/chatwoot'
import { useState, useRef, useEffect, type FormEvent } from 'react'
import type { Message } from '../types/chatwoot'

type Props = { conversationId: number }

function formatTime(ts: number) {
  return new Date(ts * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

export default function MessageThread({ conversationId }: Props) {
  const [input, setInput] = useState('')
  const [sending, setSending] = useState(false)
  const bottomRef = useRef<HTMLDivElement>(null)

  const { data: messages = [], refetch } = useQuery({
    queryKey: ['messages', conversationId],
    queryFn: () => getMessages(conversationId),
    refetchInterval: 3000,
  })

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages])

  const handleSend = async (e: FormEvent) => {
    e.preventDefault()
    if (!input.trim() || sending) return
    setSending(true)
    try {
      await sendMessage(conversationId, input.trim())
      setInput('')
      refetch()
    } finally {
      setSending(false)
    }
  }

  const sorted = [...messages].sort((a, b) => a.created_at - b.created_at)

  return (
    <div className="flex flex-col h-full">
      {/* Messages */}
      <div className="flex-1 overflow-y-auto px-6 py-4 space-y-3">
        {sorted.map((msg: Message) => {
          const isOutgoing = msg.message_type === 1
          const isActivity = msg.message_type === 2

          if (isActivity) {
            return (
              <div key={msg.id} className="flex justify-center">
                <span className="text-xs px-3 py-1 rounded-full" style={{ background: 'var(--bg-tertiary)', color: 'var(--text-muted)' }}>
                  {msg.content}
                </span>
              </div>
            )
          }

          return (
            <div key={msg.id} className={`flex ${isOutgoing ? 'justify-end' : 'justify-start'}`}>
              <div className="max-w-[70%]">
                {!isOutgoing && msg.sender && (
                  <div className="text-xs mb-1 ml-1" style={{ color: 'var(--text-muted)' }}>
                    {msg.sender.name}
                  </div>
                )}
                <div
                  className="px-4 py-2.5 rounded-2xl text-sm leading-relaxed"
                  style={{
                    background: isOutgoing ? 'var(--accent)' : 'var(--bg-tertiary)',
                    color: isOutgoing ? 'white' : 'var(--text-primary)',
                    borderBottomRightRadius: isOutgoing ? '6px' : undefined,
                    borderBottomLeftRadius: !isOutgoing ? '6px' : undefined,
                  }}
                >
                  {msg.content}
                </div>
                <div className={`text-xs mt-1 ${isOutgoing ? 'text-right mr-1' : 'ml-1'}`} style={{ color: 'var(--text-muted)' }}>
                  {formatTime(msg.created_at)}
                </div>
              </div>
            </div>
          )
        })}
        <div ref={bottomRef} />
      </div>

      {/* Input */}
      <form onSubmit={handleSend} className="px-4 py-3 flex gap-3" style={{ borderTop: '1px solid var(--border)' }}>
        <input
          type="text"
          value={input}
          onChange={e => setInput(e.target.value)}
          placeholder="Type a message..."
          className="flex-1 px-4 py-2.5 rounded-xl text-sm outline-none"
          style={{ background: 'var(--bg-tertiary)', color: 'var(--text-primary)', border: '1px solid var(--border)' }}
        />
        <button
          type="submit"
          disabled={sending || !input.trim()}
          className="px-6 py-2.5 rounded-xl text-sm font-medium transition-colors cursor-pointer disabled:opacity-40"
          style={{ background: 'var(--accent)', color: 'white' }}
        >
          Send
        </button>
      </form>
    </div>
  )
}
