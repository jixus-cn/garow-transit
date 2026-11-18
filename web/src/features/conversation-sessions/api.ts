/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { t } from 'i18next'

import type { ApiResponse } from '@/features/profile/types'
import { api } from '@/lib/api'
import { createServerError } from '@/lib/server-error-message'

export interface ConversationSession {
  id: number
  session_key: string
  client_key: string
  user_id: number
  token_id: number
  token_name: string
  source:
    | 'client'
    | 'responses'
    | 'prefix'
    | 'residual'
    | 'snapshot'
    | 'fragment'
  parent_session_id: number
  link_reason: string
  model: string
  last_model: string
  protocol: string
  created_at: number
  updated_at: number
  message_count: number
  user_message_count: number
  request_count: number
  last_outcome: string
  username?: string
  preview?: string
}

export interface ConversationMessage {
  id: number
  session_id: number
  role: 'user' | 'assistant'
  kind: 'text' | 'context' | 'unknown' | 'final' | 'commentary' | 'partial'
  origin?: 'request' | 'response' | 'snapshot' | 'legacy'
  item_index?: number
  part_index?: number
  content: string
  truncated: boolean
  interjection: boolean
  content_hash: string
  request_id: string
  created_at: number
}

export interface ConversationLedger {
  request_id: string
  user_id: number
  created_at: number
  model: string
  response_model: string
  token_id: number
  token_name: string
  protocol: string
  kind: string
  response_id: string
  previous_response_id: string
  outcome: string
  truncated: boolean
  session_key: string
  session_state: string
  new_user_messages: number
  assistant_text_chars: number
  tool_calls: number
}

export interface SessionFilters {
  include_fragments?: boolean
  p: number
  page_size: number
  user_id?: number
  start_timestamp?: number
  end_timestamp?: number
}

export interface SessionDetail {
  session: ConversationSession
  messages: ConversationMessage[]
  ledger: ConversationLedger[]
  has_more: boolean
}

export async function getConversationSessions(
  params: SessionFilters
): Promise<{ items: ConversationSession[]; total: number }> {
  const response = await api.get<
    ApiResponse<{ items: ConversationSession[]; total: number }>
  >('/api/conversations/sessions', { params })
  if (!response.data.success || !response.data.data) {
    throw createServerError(response.data, t('Failed to load conversations'))
  }
  return response.data.data
}

export async function getConversationSessionDetail(
  sessionId: number,
  beforeId?: number,
  pageSize = 200
): Promise<SessionDetail> {
  const response = await api.get<ApiResponse<SessionDetail>>(
    `/api/conversations/sessions/${sessionId}`,
    { params: { before_id: beforeId, page_size: pageSize } }
  )
  if (!response.data.success || !response.data.data) {
    throw createServerError(response.data, t('Failed to load conversation'))
  }
  return response.data.data
}

export async function searchConversationMessages(params: {
  q: string
  user_id?: number
  start_timestamp?: number
  end_timestamp?: number
  page_size?: number
}): Promise<{ items: ConversationMessage[] }> {
  const response = await api.get<ApiResponse<{ items: ConversationMessage[] }>>(
    '/api/conversations/messages/search',
    { params }
  )
  if (!response.data.success || !response.data.data) {
    throw createServerError(response.data, t('Failed to search conversations'))
  }
  return response.data.data
}
