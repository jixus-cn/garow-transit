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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DataTablePage, useDataTable } from '@/components/data-table'
import { SectionPageLayout } from '@/components/layout'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { useStatus } from '@/hooks/use-status'
import { requireServerSuccess } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

import { getConversationSessions, type SessionFilters } from './api'
import { useSessionColumns } from './components/session-columns'
import { SessionFilterBar } from './components/session-filter-bar'
import { QAIndex } from './qa-index'

const EMPTY_SESSIONS: never[] = []

export function ConversationSessions() {
  const { status } = useStatus()
  return status?.conversation_qa === false ? <LegacySessions /> : <QAIndex />
}

function LegacySessions() {
  const { t } = useTranslation()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const [filters, setFilters] = useState<SessionFilters>({
    p: 1,
    page_size: 20,
  })
  const invalidRange =
    filters.start_timestamp !== undefined &&
    filters.end_timestamp !== undefined &&
    filters.start_timestamp > filters.end_timestamp
  const query = useQuery({
    queryKey: ['conversation-sessions', userId, filters],
    queryFn: async () =>
      requireServerSuccess(await getConversationSessions(filters)),
    enabled: !invalidRange,
    retry: false,
  })
  const columns = useSessionColumns()
  const { table } = useDataTable({
    columns,
    data: query.isError
      ? EMPTY_SESSIONS
      : (query.data?.items ?? EMPTY_SESSIONS),
    getRowId: (entry) => String(entry.id),
    totalCount: query.isError ? 0 : (query.data?.total ?? 0),
    pagination: { pageIndex: filters.p - 1, pageSize: filters.page_size },
    onPaginationChange: (updater) => {
      if (query.isFetching || query.isError || invalidRange) return
      setFilters((previous) => {
        const current = {
          pageIndex: previous.p - 1,
          pageSize: previous.page_size,
        }
        const next = typeof updater === 'function' ? updater(current) : updater
        return {
          ...previous,
          p: next.pageSize === previous.page_size ? next.pageIndex + 1 : 1,
          page_size: next.pageSize,
        }
      })
    },
    enableRowSelection: false,
    enableSorting: false,
    manualFiltering: true,
    manualPagination: true,
  })
  const update = (patch: Partial<SessionFilters>) =>
    setFilters((previous) => ({ ...previous, ...patch, p: 1 }))

  return (
    <SectionPageLayout fixedContent>
      <SectionPageLayout.Title>
        {t('Conversation Audit')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='flex h-full min-h-0 flex-col'>
          <DataTablePage
            table={table}
            columns={columns}
            isLoading={query.isPending && !invalidRange}
            isFetching={query.isFetching}
            emptyTitle={
              query.isError
                ? t('Failed to load conversations')
                : t('No records')
            }
            paginationInFooter
            className='h-auto min-h-0 flex-1'
            applyHeaderSize
            getColumnClassName={() => 'py-2'}
            toolbar={
              <div className='shrink-0 space-y-2'>
                <SessionFilterBar
                  table={table}
                  filters={filters}
                  onChange={update}
                  isFetching={query.isFetching}
                  onSearch={() => {
                    if (!invalidRange) void query.refetch()
                  }}
                  onReset={() =>
                    setFilters({ p: 1, page_size: filters.page_size })
                  }
                />
                {invalidRange && (
                  <Alert variant='destructive'>
                    <AlertDescription>
                      {t('End time must be after start time')}
                    </AlertDescription>
                  </Alert>
                )}
                {query.isError && (
                  <Alert variant='destructive'>
                    <AlertDescription className='flex items-center justify-between gap-2'>
                      <span>{t('Failed to load conversations')}</span>
                      <Button
                        size='sm'
                        variant='outline'
                        onClick={() => void query.refetch()}
                      >
                        {t('Retry')}
                      </Button>
                    </AlertDescription>
                  </Alert>
                )}
              </div>
            }
          />
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
