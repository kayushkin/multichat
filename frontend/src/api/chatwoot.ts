import type { AuthResponse, Conversation, ConversationsResponse, Message, MessagesResponse } from '../types/chatwoot'

const BASE = '/api/v1'

function getToken(): string {
  return localStorage.getItem('chatwoot_token') || ''
}

function getAccountId(): number {
  return Number(localStorage.getItem('chatwoot_account_id') || '1')
}

async function api<T>(path: string, opts: RequestInit = {}): Promise<T> {
  const res = await fetch(`${BASE}/accounts/${getAccountId()}${path}`, {
    ...opts,
    headers: {
      'Content-Type': 'application/json',
      api_access_token: getToken(),
      ...opts.headers,
    },
  })
  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`)
  }
  return res.json()
}

export async function login(email: string, password: string): Promise<AuthResponse> {
  const res = await fetch('/auth/sign_in', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password }),
  })
  if (!res.ok) throw new Error('Invalid credentials')
  return res.json()
}

export async function getConversations(page = 1, status = 'open'): Promise<ConversationsResponse> {
  return api(`/conversations?page=${page}&status=${status}`)
}

export async function getConversation(id: number): Promise<Conversation> {
  return api(`/conversations/${id}`)
}

export async function getMessages(conversationId: number): Promise<Message[]> {
  const data = await api<MessagesResponse>(`/conversations/${conversationId}/messages`)
  return data.payload
}

export async function sendMessage(conversationId: number, content: string): Promise<Message> {
  return api(`/conversations/${conversationId}/messages`, {
    method: 'POST',
    body: JSON.stringify({ content, message_type: 'outgoing' }),
  })
}

export async function getContacts(page = 1, q = ''): Promise<{ payload: Array<{ id: number; name: string; email: string | null; phone_number: string | null; avatar_url: string | null }> }> {
  const search = q ? `&q=${encodeURIComponent(q)}` : ''
  return api(`/contacts?page=${page}${search}`)
}

export async function getContactConversations(contactId: number): Promise<{ payload: Conversation[] }> {
  return api(`/contacts/${contactId}/conversations`)
}
