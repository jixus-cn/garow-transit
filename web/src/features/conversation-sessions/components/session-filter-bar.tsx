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
import type { Table } from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { CompactDateTimeRangePicker } from '@/features/usage-logs/components/compact-date-time-range-picker'
import {
  LogsFilterField,
  LogsFilterInput,
  LogsFilterToolbar,
} from '@/features/usage-logs/components/logs-filter-toolbar'
import dayjs from '@/lib/dayjs'

import type { ConversationSession, SessionFilters } from '../api'
import { SessionSearchDialog } from './session-search-dialog'

export function SessionFilterBar(props: {
  table: Table<ConversationSession>
  filters: SessionFilters
  onChange: (patch: Partial<SessionFilters>) => void
  onSearch: () => void
  onReset: () => void
  isFetching: boolean
}) {
  const { t } = useTranslation()
  const filters = props.filters
  const hasActiveFilters =
    !!filters.include_fragments ||
    filters.user_id !== undefined ||
    filters.start_timestamp !== undefined ||
    filters.end_timestamp !== undefined
  return (
    <LogsFilterToolbar
      table={props.table}
      hasActiveFilters={hasActiveFilters}
      searchLoading={props.isFetching}
      onSearch={props.onSearch}
      onReset={props.onReset}
      actionEnd={
        <>
          <Button
            variant='outline'
            size='sm'
            aria-pressed={!!filters.include_fragments}
            onClick={() =>
              props.onChange({
                include_fragments: !filters.include_fragments,
                p: 1,
              })
            }
          >
            {t('Show unattributed fragments')}
          </Button>
          <SessionSearchDialog userId={filters.user_id} />
        </>
      }
      primaryFilters={
        <>
          <LogsFilterField>
            <LogsFilterInput
              type='number'
              min={1}
              value={filters.user_id ?? ''}
              placeholder={t('User ID')}
              aria-label={t('User ID')}
              onChange={(event) => {
                const raw = event.target.value
                props.onChange({
                  user_id: raw === '' ? undefined : Number(raw),
                })
              }}
            />
          </LogsFilterField>
          <LogsFilterField wide>
            <CompactDateTimeRangePicker
              start={
                filters.start_timestamp
                  ? dayjs.unix(filters.start_timestamp).toDate()
                  : undefined
              }
              end={
                filters.end_timestamp
                  ? dayjs.unix(filters.end_timestamp).toDate()
                  : undefined
              }
              onChange={(range) =>
                props.onChange({
                  start_timestamp: range.start
                    ? Math.floor(range.start.getTime() / 1000)
                    : undefined,
                  end_timestamp: range.end
                    ? Math.floor(range.end.getTime() / 1000)
                    : undefined,
                })
              }
            />
          </LogsFilterField>
        </>
      }
    />
  )
}
