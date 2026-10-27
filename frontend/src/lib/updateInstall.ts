import { get } from 'svelte/store'
import { api } from './api'
import { chatRunRepo, autoUpdateCheck, commitDraft } from './stores'
import { shownTasks } from './tasks'
import { confirmDialog, whenNoDialog } from './ui'
import { createUpdateController, restartBlockers, startUpdate } from './update'

/** The app's one update checker (Settings' Check now and App's start and
 *  focus checks share its "last check" and "already shown" memory). */
export const updateChecker = createUpdateController({
  check: api.checkForUpdate,
  now: Date.now,
  auto: () => get(autoUpdateCheck),
})

/** The card's "Update and restart", wired to the real app. */
export const installUpdate = () =>
  startUpdate({
    prepare: api.prepareUpdate,
    apply: api.applyUpdate,
    confirm: (reasons) => confirmDialog({ title: 'Restart CommitTree?', message: reasons.join('\n'), confirmLabel: 'Restart anyway' }),
    quiet: whenNoDialog,
    staged: api.stagedUpdateVersion,
    refresh: () => updateChecker.run('manual'),
    blockers: () => restartBlockers(get(shownTasks), get(chatRunRepo), get(commitDraft)),
  })
