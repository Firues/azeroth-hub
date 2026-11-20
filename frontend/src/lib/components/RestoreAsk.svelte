<!--
  "Throw the database away on purpose?"

  The restore begins on the rollback click, so closing this window leaves
  nothing running - which is also why there is no third button: "не сейчас" is
  the same decision as "нет".

  It does not cancel the restore later, and that is not an oversight. By the
  time the restore is running, mysql\data is already being unpacked over; the
  insurance copy the panel takes first is the only way back, and it does not
  restore itself.
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
	import { stamp } from '$lib/format.js'
	import { t } from '$lib/i18n.svelte.js'

	const point = $derived(hub.points.find((p) => p.Name === hub.restorePoint))
</script>

<Dialog open={hub.restorePoint !== null} onOpenChange={(o) => !o && (hub.restorePoint = null)}>
	<DialogContent data-role="restore-ask" class="border-gold-dim/40">
		<DialogHeader>
			<DialogTitle>{t('restore.title')}</DialogTitle>
			<DialogDescription class="text-muted-foreground">
				{t('restore.desc', {
					name: String(point?.Name ?? ''),
					date: point ? stamp(point.Created) || t('unknownDate') : t('unknownDate')
				})}.
			</DialogDescription>
		</DialogHeader>

		<p class="text-xs leading-relaxed text-warn">
			{t('restore.warn')}
		</p>

		<DialogFooter>
			<Button variant="outline" data-role="restore-no" onclick={() => (hub.restorePoint = null)}>
				{t('restore.no')}
			</Button>
			<Button
				variant="destructive"
				data-role="restore-yes"
				onclick={() => hub.restorePoint && hub.restoreSnapshot(hub.restorePoint)}
			>
				{t('restore.yes')}
			</Button>
		</DialogFooter>
	</DialogContent>
</Dialog>
