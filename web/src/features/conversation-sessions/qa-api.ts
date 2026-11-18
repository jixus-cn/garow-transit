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

export interface QATurn {
  id: number
  user_id: number
  created_at: number
  chat_key: string
  prompt: string
  final: string
}
export interface QAChat {
  chat_key: string
  username: string
  user_id: number
  updated_at: number
  preview: string
  turn_count: number
}
export interface QAFilter {
  user_id?: number
  start_timestamp?: number
  end_timestamp?: number
}
interface QAPage<T> {
  items: T[]
  total: number
  page: number
  page_size: number
}
function read<T>(body: ApiResponse<T>): T {
  if (!body.success || !body.data) {
    throw createServerError(body, t('Failed to load conversation'))
  }
  return body.data
}
export async function qaChats(page: number, size: number, filter: QAFilter) {
  const r = await api.get<ApiResponse<QAPage<QAChat>>>(
    '/api/conversations/sessions',
    { params: { p: page, page_size: size, ...filter } }
  )
  return read(r.data)
}
export async function qaTurns(
  chat: string,
  query: string,
  p: number,
  filter: QAFilter
) {
  const params = { p, page_size: 20, ...filter }
  if (query) {
    const r = await api.post<ApiResponse<QAPage<QATurn>>>(
      '/api/conversations/search',
      { q: query },
      { params }
    )
    return read(r.data)
  }
  const path = chat
    ? `/api/conversations/sessions/${encodeURIComponent(chat)}`
    : '/api/conversations/unassigned'
  const r = await api.get<ApiResponse<QAPage<QATurn>>>(path, { params })
  return read(r.data)
}
export async function qaByRequest(id: string) {
  const r = await api.get<ApiResponse<{ turn: QATurn | null }>>(
    `/api/conversations/requests/${encodeURIComponent(id)}`
  )
  return read(r.data).turn
}
