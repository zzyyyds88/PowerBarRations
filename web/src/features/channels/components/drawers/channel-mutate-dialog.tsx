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
import { ChannelMutateContext } from './channel-mutate-context'
import type { ChannelMutateDialogProps } from './channel-mutate-dialog-model'
import { ChannelMutateDialogView } from './channel-mutate-dialog-view'
import { useChannelMutateActions } from './use-channel-mutate-actions'
import { useChannelMutateState } from './use-channel-mutate-state'

export type { ChannelMutateDialogProps } from './channel-mutate-dialog-model'

export function ChannelMutateDialog(props: ChannelMutateDialogProps) {
  const state = useChannelMutateState(props)
  const actions = useChannelMutateActions(state, props)
  const value = { ...state, ...actions }

  return (
    <ChannelMutateContext.Provider value={value}>
      <ChannelMutateDialogView
        open={props.open}
        onOpenChange={props.onOpenChange}
      />
    </ChannelMutateContext.Provider>
  )
}
