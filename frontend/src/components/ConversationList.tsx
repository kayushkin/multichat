import { useQuery } from '@tanstack/react-query'
import { getConversations } from '../api/chatwoot'
import type { Conversation } from '../types/chatwoot'

type Props = {
  selected: number | null
  onSelect: (id: number) => void
  platformFilter: string
}

function platformIcon(channel: string) {
  if (channel?.includes('telegram') || channel?.includes('Telegram')) return { icon: '✈️', color: 'var(--telegram)' }
  if (channel?.includes('whatsapp') || channel?.includes('WhatsApp')) return { icon: '💬', color: 'var(--whatsapp)' }
  return { icon: '🌐', color: 'var(--text-muted)' }
}

function timeAgo(ts: number) {
  const diff = Math.floor(Date.now() / 1000) - ts
  if (diff < 60) return 'now'
  if (diff < 3600) return `${Math.floor(diff / 60)}m`
  if (diff < 86400) return `${Math.floor(diff / 3600)}h`
  return `${Math.floor(diff / 86400)}d`
}

export default function ConversationList({ selected, onSelect, platformFilter }: Props) {
  const { data, isLoading } = useQuery({
    queryKey: ['conversations'],
    queryFn: () => getConversations(1, 'open'),
    refetchInterval: 5000,
  })

  const conversations = (data?.data?.payload || []).filter((c: Conversation) => {
    if (platformFilter === 'all') return true
    const ch = c.meta?.channel?.toLowerCase() || ''
    return ch.includes(platformFilter)
  })

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-full">
        <div className="text-sm" style={{ color: 'var(--text-muted)' }}>Loading...</div>
      </div>
    )
  }

  if (!conversations.length) {
    return (
      <div className="flex flex-col items-center justify-center h-full px-4 text-center">
        <div className="text-3xl mb-3">💬</div>
        <div className="text-sm font-medium" style={{ color: 'var(--text-secondary)' }}>No conversations yet</div>
        <div className="text-xs mt-1" style={{ color: 'var(--text-muted)' }}>
          Messages will appear here once channels are connected
        </div>
      </div>
    )
  }

  return (
    <div className="overflow-y-auto h-full">
      {conversations.map((c: Conversation) => {
        const { icon, color } = platformIcon(c.meta?.channel)
        const name = c.meta?.sender?.name || `Conversation #${c.id}`
        const lastMsg = c.last_non_activity_message?.content || 'No messages'
        const isSelected = selected === c.id

        return (
          <div
            key={c.id}
            onClick={() => onSelect(c.id)}
            className="flex items-start gap-3 px-4 py-3 cursor-pointer transition-colors"
            style={{
              background: isSelected ? 'var(--bg-tertiary)' : 'transparent',
              borderLeft: isSelected ? '3px solid var(--accent)' : '3px solid transparent',
            }}
          >
            <div
              className="w-10 h-10 rounded-full flex items-center justify-center text-sm font-semibold shrink-0"
              style={{ background: 'var(--bg-tertiary)', color }}
            >
              {icon}
            </div>
            <div className="flex-1 min-w-0">
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium truncate" style={{ color: 'var(--text-primary)' }}>{name}</span>
                <span className="text-xs shrink-0 ml-2" style={{ color: 'var(--text-muted)' }}>
                  {timeAgo(c.timestamp)}
                </span>
              </div>
              <p className="text-xs mt-0.5 truncate" style={{ color: 'var(--text-secondary)' }}>{lastMsg}</p>
            </div>
            {c.unread_count > 0 && (
              <div
                className="w-5 h-5 rounded-full flex items-center justify-center text-xs font-bold shrink-0"
                style={{ background: 'var(--accent)', color: 'white' }}
              >
                {c.unread_count}
              </div>
            )}
          </div>
        )
      })}
    </div>
  )
}
