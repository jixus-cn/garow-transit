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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { CompactDateTimeRangePicker } from '@/features/usage-logs/components/compact-date-time-range-picker'

import { QAChats } from './components/qa-chats'
import { QAReader } from './components/qa-reader'
import type { QAFilter } from './qa-api'

export function QAIndex() {
  const { t } = useTranslation()
  const [view, setView] = useState<'chats' | 'unassigned' | 'search'>('chats')
  const [draft, setDraft] = useState(''),
    [query, setQuery] = useState('')
  const [filter, setFilter] = useState<QAFilter>({})
  return (
    <SectionPageLayout fixedContent>
      <SectionPageLayout.Title>
        {t('Conversation Audit')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='flex h-full min-h-0 flex-col gap-3'>
          <div className='flex flex-wrap items-center gap-2'>
            <Button
              size='sm'
              variant={view === 'chats' ? 'default' : 'outline'}
              aria-pressed={view === 'chats'}
              onClick={() => setView('chats')}
            >
              {t('Conversations')}
            </Button>
            <Button
              size='sm'
              variant={view === 'unassigned' ? 'default' : 'outline'}
              aria-pressed={view === 'unassigned'}
              onClick={() => setView('unassigned')}
            >
              {t('Unassigned exchanges')}
            </Button>
            <form
              className='flex gap-2'
              onSubmit={(e) => {
                e.preventDefault()
                if (draft.trim()) {
                  setQuery(draft.trim())
                  setView('search')
                }
              }}
            >
              <Input
                className='h-8 w-52'
                aria-label={t('Search prompts and replies')}
                placeholder={t('Search prompts and replies')}
                value={draft}
                maxLength={200}
                onChange={(e) => setDraft(e.target.value)}
              />
              <Button
                size='sm'
                variant='outline'
                type='submit'
                disabled={!draft.trim()}
              >
                {t('Search')}
              </Button>
            </form>
            <Input
              className='h-8 w-24'
              aria-label={t('User ID')}
              placeholder={t('User ID')}
              type='number'
              min={1}
              value={filter.user_id ?? ''}
              onChange={(e) =>
                setFilter({
                  ...filter,
                  user_id: e.target.value ? Number(e.target.value) : undefined,
                })
              }
            />
            <CompactDateTimeRangePicker
              start={
                filter.start_timestamp
                  ? new Date(filter.start_timestamp * 1000)
                  : undefined
              }
              end={
                filter.end_timestamp
                  ? new Date(filter.end_timestamp * 1000)
                  : undefined
              }
              onChange={(range) =>
                setFilter({
                  ...filter,
                  start_timestamp: range.start
                    ? Math.floor(+range.start / 1000)
                    : undefined,
                  end_timestamp: range.end
                    ? Math.floor(+range.end / 1000)
                    : undefined,
                })
              }
            />
          </div>
          {view === 'chats' ? (
            <QAChats
              key={JSON.stringify(filter)}
              filter={filter}
              onUnassigned={() => setView('unassigned')}
            />
          ) : (
            <QAReader
              query={view === 'search' ? query : undefined}
              filter={filter}
            />
          )}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
