import type { InjectionKey } from 'vue'
import type { SplitDir } from '@/lib/layout'

/** Opens the create dialog; the new session goes into a new pane beside
 * `paneId` (provided by App). */
export const NEW_SESSION_FOR_SPLIT: InjectionKey<(paneId: string, dir: SplitDir) => void> = Symbol('newSessionForSplit')
