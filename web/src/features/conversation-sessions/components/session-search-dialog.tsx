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
import { Search } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { EmptyState } from '@/components/empty-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import dayjs from '@/lib/dayjs'

import { searchConversationMessages } from '../api'

export function SessionSearchDialog(props: { userId?: number }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [keyword, setKeyword] = useState('')
  const [submitted, setSubmitted] = useState('')
  const query = useQuery({
    queryKey: ['conversation-message-search', submitted, props.userId],
    queryFn: () =>
      searchConversationMessages({ q: submitted, user_id: props.userId }),
    enabled: open && submitted.length > 0,
    retry: false,
  })
  return (
    <Dialog
      open={open}
      onOpenChange={setOpen}
      title={t('Search conversation content')}
      description={t(
        'Search retained user and assistant text. Root access is logged.'
      )}
      trigger={
        <Button variant='outline' size='sm'>
          <Search className='size-4' aria-hidden='true' />
          {t('Search content')}
        </Button>
      }
    >
      <form
        className='flex gap-2'
        onSubmit={(event) => {
          event.preventDefault()
          setSubmitted(keyword.trim())
        }}
      >
        <Input
          value={keyword}
          onChange={(event) => setKeyword(event.target.value)}
          placeholder={t('Keyword')}
          aria-label={t('Keyword')}
          maxLength={200}
        />
        <Button type='submit' size='sm' disabled={keyword.trim().length === 0}>
          {t('Search')}
        </Button>
      </form>
      {query.isPending && submitted && <LoadingState inline />}
      {query.data && query.data.items.length === 0 && (
        <EmptyState title={t('No records')} />
      )}
      {query.data && query.data.items.length > 0 && (
        <ul className='flex flex-col gap-2'>
          {query.data.items.map((message) => (
            <li key={message.id} className='rounded-md border p-2.5 text-sm'>
              <div className='text-muted-foreground mb-1 flex items-center justify-between gap-2 text-xs'>
                <span>
                  {message.origin === 'snapshot' &&
                    t('Client-supplied history')}
                  {message.origin !== 'snapshot' &&
                    (message.role === 'user'
                      ? t('Client-submitted text')
                      : t('Assistant'))}{' '}
                  ·{' '}
                  {dayjs.unix(message.created_at).format('YYYY-MM-DD HH:mm:ss')}
                </span>
                <Button
                  variant='link'
                  size='sm'
                  className='h-auto p-0'
                  render={
                    <Link
                      to='/conversation-sessions/$sessionId'
                      params={{ sessionId: String(message.session_id) }}
                      onClick={() => setOpen(false)}
                    />
                  }
                >
                  {t('Open session')}
                </Button>
              </div>
              <p className='line-clamp-3 break-words whitespace-pre-wrap'>
                {message.content}
              </p>
            </li>
          ))}
        </ul>
      )}
    </Dialog>
  )
}
