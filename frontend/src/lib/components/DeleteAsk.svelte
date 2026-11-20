<!--
  "Throw this copy away?"

  The plainest window in the app, and it is plain because there is nothing
  behind it. A copy is a folder of files on disk; nothing holds it open, the
  server does not have to be stopped for it to go, and nothing in the repack
  can bring it back. There is no third button here for the same reason as in
  RestoreAsk: "не сейчас" is the same decision as "нет".

  It names the folder, its date and its size, because "удалить копию" without
  saying which one is how a person deletes the wrong one - the list is sorted
  newest first, and the newest is the one they will not miss until it is gone.
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
	import { bytes, stamp } from '$lib/format.js'
	import { t } from '$lib/i18n.svelte.js'

	const point = $derived(hub.points.find((p) => p.Name === hub.deletePoint))
</script>

<Dialog open={hub.deletePoint !== null} onOpenChange={(o) => !o && (hub.deletePoint = null)}>
	<DialogContent data-role="delete-ask">
		<DialogHeader>
			<DialogTitle>{t('delete.title')}</DialogTitle>
			<DialogDescription class="text-muted-foreground">
				{t('delete.desc', {
					name: String(point?.Name ?? ''),
					date: point ? stamp(point.Created) || t('unknownDate') : t('unknownDate'),
					size: point ? bytes(point.SizeBytes) : ''
				})}.
			</DialogDescription>
		</DialogHeader>

		<p class="text-xs leading-relaxed text-warn">
			{t('delete.warn1')} <code class="text-foreground">backups\</code> {t('delete.warn2')}
		</p>

		<DialogFooter>
			<Button variant="outline" data-role="delete-no" onclick={() => (hub.deletePoint = null)}>
				{t('delete.keep')}
			</Button>
			<Button
				variant="destructive"
				data-role="delete-yes"
				onclick={() => hub.deletePoint && hub.deleteBackup(hub.deletePoint, Number(point?.SizeBytes) || 0)}
			>
				{t('delete.yes')}
			</Button>
		</DialogFooter>
	</DialogContent>
</Dialog>
