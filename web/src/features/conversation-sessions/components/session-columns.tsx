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
import type { ColumnDef } from '@tanstack/react-table'
import { Link } from '@tanstack/react-router'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { TruncatedCell } from '@/components/data-table'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import dayjs from '@/lib/dayjs'
import { formatNumber } from '@/lib/format'

import type { ConversationSession } from '../api'

const SOURCE_LABEL: Record<string, string> = {
  client: 'Client identifier',
  responses: 'Responses link',
  prefix: 'Prefix chain',
  residual: 'Unattributed fragments',
}

export function useSessionColumns(): ColumnDef<ConversationSession>[] {
  const { t } = useTranslation()
  return useMemo(() => {
    const columns: ColumnDef<ConversationSession>[] = [
      {
        accessorKey: 'updated_at',
        header: t('Updated'),
        size: 180,
        cell: ({ row }) => (
          <span className='font-mono tabular-nums'>
            {dayjs.unix(row.original.updated_at).format('YYYY-MM-DD HH:mm:ss')}
          </span>
        ),
        meta: { label: t('Updated'), mobileTitle: true },
      },
      {
        accessorKey: 'user_id',
        header: t('User'),
        size: 80,
        cell: ({ row }) => (
          <span className='font-mono'>#{row.original.user_id}</span>
        ),
        meta: { label: t('User') },
      },
      {
        accessorKey: 'token_name',
        header: t('API Keys'),
        size: 120,
        cell: ({ row }) => (
          <TruncatedCell className='max-w-32'>
            {row.original.token_name || '—'}
          </TruncatedCell>
        ),
        meta: { label: t('API Keys') },
      },
      {
        accessorKey: 'last_model',
        header: t('Models'),
        size: 160,
        cell: ({ row }) => (
          <TruncatedCell className='max-w-40'>
            {row.original.last_model || row.original.model || '—'}
          </TruncatedCell>
        ),
        meta: { label: t('Models') },
      },
      {
        accessorKey: 'source',
        header: t('Linkage'),
        size: 140,
        cell: ({ row }) => (
          <Badge
            variant={row.original.source === 'residual' ? 'warning' : 'secondary'}
          >
            {t(SOURCE_LABEL[row.original.source] ?? row.original.source)}
          </Badge>
        ),
        meta: { label: t('Linkage') },
      },
      {
        id: 'counts',
        header: t('Messages / Requests'),
        size: 140,
        accessorFn: (entry) => `${entry.message_count}/${entry.request_count}`,
        cell: ({ row }) => (
          <span className='font-mono tabular-nums'>
            {formatNumber(row.original.message_count)} /{' '}
            {formatNumber(row.original.request_count)}
          </span>
        ),
        meta: { label: t('Messages / Requests') },
      },
      {
        id: 'actions',
        header: t('Actions'),
        size: 90,
        cell: ({ row }) => (
          <Button
            variant='outline'
            size='sm'
            render={
              <Link
                to='/conversation-sessions/$sessionId'
                params={{ sessionId: String(row.original.id) }}
              />
            }
          >
            {t('Open')}
          </Button>
        ),
        meta: { label: t('Actions') },
      },
    ]
    return columns
  }, [t])
}