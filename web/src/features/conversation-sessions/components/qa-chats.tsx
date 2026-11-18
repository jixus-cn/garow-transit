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
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import type { ColumnDef, PaginationState } from '@tanstack/react-table'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  DataTablePage,
  useDataTable,
  TruncatedCell,
} from '@/components/data-table'
import { ErrorState } from '@/components/error-state'
import dayjs from '@/lib/dayjs'
import { useAuthStore } from '@/stores/auth-store'

import { qaChats, type QAChat, type QAFilter } from '../qa-api'

const empty: QAChat[] = []
export function QAChats(props: { filter: QAFilter }) {
  const { t } = useTranslation()
  const user = useAuthStore((s) => s.auth.user?.id)
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: 20,
  })
  const q = useQuery({
    queryKey: ['qa-chats', user, pagination, props.filter],
    queryFn: () =>
      qaChats(pagination.pageIndex + 1, pagination.pageSize, props.filter),
    retry: false,
    gcTime: 0,
  })
  const columns = useMemo<ColumnDef<QAChat>[]>(
    () => [
      {
        accessorKey: 'preview',
        header: t('Topic preview'),
        cell: ({ row }) => (
          <TruncatedCell>
            <Link
              className='text-primary underline'
              to='/conversation-sessions/$sessionId'
              params={{ sessionId: row.original.chat_key }}
            >
              {row.original.preview}
            </Link>
          </TruncatedCell>
        ),
      },
      {
        accessorKey: 'username',
        header: t('User'),
        cell: ({ row }) => row.original.username || `#${row.original.user_id}`,
      },
      { accessorKey: 'turn_count', header: t('Exchanges') },
      {
        accessorKey: 'updated_at',
        header: t('Updated'),
        cell: ({ row }) =>
          dayjs.unix(row.original.updated_at).format('YYYY-MM-DD HH:mm'),
      },
    ],
    [t]
  )
  const { table } = useDataTable({
    columns,
    data: q.data?.items ?? empty,
    getRowId: (r) => r.chat_key,
    totalCount: q.data?.total ?? 0,
    pagination,
    onPaginationChange: setPagination,
    manualPagination: true,
    enableRowSelection: false,
    enableSorting: false,
  })
  if (q.isError) {
    return (
      <ErrorState
        title={t('Failed to load conversations')}
        onRetry={() => void q.refetch()}
      />
    )
  }
  return (
    <DataTablePage
      table={table}
      columns={columns}
      isLoading={q.isPending}
      isFetching={q.isFetching}
      emptyTitle={t('No records')}
      paginationInFooter
      className='h-auto min-h-0 flex-1'
    />
  )
}
