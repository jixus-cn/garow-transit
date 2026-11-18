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
import { useInfiniteQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { useAuthStore } from '@/stores/auth-store'

import { qaTurns, type QAFilter } from '../qa-api'
import { QAExchange } from './qa-exchange'

export function QAReader(props: {
  chat?: string
  query?: string
  filter?: QAFilter
}) {
  const { t } = useTranslation()
  const user = useAuthStore((s) => s.auth.user?.id)
  const chat = props.chat ?? '',
    query = props.query ?? '',
    filter = props.filter ?? {}
  const q = useInfiniteQuery({
    queryKey: ['qa-turns', user, chat, query, filter],
    queryFn: ({ pageParam }) => qaTurns(chat, query, pageParam, filter),
    initialPageParam: 1,
    getNextPageParam: (p) =>
      p.page * p.page_size < p.total ? p.page + 1 : undefined,
    retry: false,
    gcTime: 0,
  })
  if (q.isPending) return <LoadingState />
  if (q.isError) {
    return (
      <ErrorState
        title={t('Failed to load conversation')}
        onRetry={() => void q.refetch()}
      />
    )
  }
  return (
    <div className='min-h-0 space-y-4 overflow-auto pb-4'>
      {chat && (
        <Button
          variant='outline'
          size='sm'
          nativeButton={false}
          render={<Link to='/conversation-sessions' />}
        >
          {t('Conversation Audit')}
        </Button>
      )}
      {!q.data.pages[0]?.total && <EmptyState title={t('No records')} />}
      {q.data.pages
        .flatMap((p) => p.items)
        .map((turn) => (
          <QAExchange key={turn.id} turn={turn} />
        ))}
      {q.hasNextPage && (
        <Button
          variant='outline'
          disabled={q.isFetchingNextPage}
          onClick={() => void q.fetchNextPage()}
        >
          {t('Load more')}
        </Button>
      )}
    </div>
  )
}
