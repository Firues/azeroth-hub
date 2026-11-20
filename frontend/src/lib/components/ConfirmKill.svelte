<!--
  "A process would not die - may I kill it."

  A separate dialog from the restore one on purpose. That one asks about
  throwing the whole database away on purpose; this one is about a stuck
  process and an unsaved character. Under one heading the wording would have to
  be hedged into meaning neither.

  Both answers close it. A dialog left on screen after being answered looks
  like the app is still waiting, and the next stop opens a second one behind it.
-->
<script lang="ts">
	import {
		Dialog,
		DialogContent,
		DialogDescription,
		DialogFooter,
		DialogHeader,
		DialogTitle
	} from '$lib/components/ui/dialog'
	import { Button } from '$lib/components/ui/button'
	import { hub } from '$lib/state.svelte.js'
	import { SERVICE_NAME } from '$lib/words.js'
	import { t } from '$lib/i18n.svelte.js'
</script>

<Dialog open={hub.confirmKill !== null} onOpenChange={(o) => !o && hub.answerConfirm(false)}>
	<DialogContent data-role="confirm-kill" class="border-gold-dim/40">
		<DialogHeader>
			<DialogTitle>{t('kill.title')}</DialogTitle>
			<DialogDescription class="text-muted-foreground">
				{t('kill.desc', {
					svc: hub.confirmKill ? (SERVICE_NAME[hub.confirmKill.service] || hub.confirmKill.service) : '',
					pid: hub.confirmKill?.pid ?? 0
				})}
			</DialogDescription>
		</DialogHeader>

		<!--
			The old sentence here said "close its window and let it save the
			players". For a world this dialog is reached only after both graceful
			routes failed, so that advice was never more than a guess - and after a
			panel restart it names a window that does not exist. What is true in
			every case that gets here: the save pass is not going to run, and the
			lost ground is bounded by PlayerSaveInterval.
		-->
		<p class="text-xs leading-relaxed text-warn">
			{t('kill.warn')}
		</p>

		<DialogFooter>
			<Button variant="outline" data-role="confirm-no" onclick={() => hub.answerConfirm(false)}>
				{t('kill.keep')}
			</Button>
			<Button variant="destructive" data-role="confirm-yes" onclick={() => hub.answerConfirm(true)}>
				{t('kill.yes')}
			</Button>
		</DialogFooter>
	</DialogContent>
</Dialog>
