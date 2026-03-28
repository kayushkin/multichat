export type Platform = 'telegram' | 'whatsapp' | 'web' | 'api'

export type ConversationStatus = 'open' | 'resolved' | 'pending' | 'snoozed'

export type Contact = {
  id: number
  name: string
  email: string | null
  phone_number: string | null
  avatar_url: string | null
  identifier: string | null
  custom_attributes: Record<string, unknown>
}

export type Inbox = {
  id: number
  name: string
  channel_type: string
  avatar_url: string | null
}

export type Message = {
  id: number
  content: string | null
  content_type: string
  message_type: number // 0=incoming, 1=outgoing, 2=activity
  created_at: number
  conversation_id: number
  sender: {
    id: number
    name: string
    avatar_url: string | null
    type: string
  } | null
  attachments: Array<{
    id: number
    file_type: string
    data_url: string
    thumb_url: string | null
  }>
}

export type Conversation = {
  id: number
  inbox_id: number
  status: ConversationStatus
  muted: boolean
  unread_count: number
  timestamp: number
  last_non_activity_message: Message | null
  messages: Message[]
  meta: {
    sender: Contact
    channel: string
  }
  contact_last_seen_at: number
  agent_last_seen_at: number
  custom_attributes: Record<string, unknown>
}

export type ConversationsResponse = {
  data: {
    meta: {
      all_count: number
      mine_count: number
      unassigned_count: number
    }
    payload: Conversation[]
  }
}

export type MessagesResponse = {
  payload: Message[]
  meta: {
    contact_last_seen_at: number
  }
}

export type AuthResponse = {
  data: {
    access_token: string
    account_id: number
    available_name: string
    avatar_url: string
    confirmed: boolean
    email: string
    name: string
    role: string
    uid: string
  }
}
