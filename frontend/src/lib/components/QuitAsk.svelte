<!--
  "Close the panel with servers still running?"

  Asked from the one button that can close the window, and asked whenever
  anything at all is up - one of the three, not only the world. The world is
  the one that outlives the panel, but MySQL is the one that is expensive to
  leave behind, and a person who closed the panel over its head deserves the
  question either way. What is running is named here, because "some of it" is
  not a thing to agree to without knowing which part.

  The question is about the servers, not the panel: the window closes either
  way, and the two answers decide what happens to the realm.

  "Остановить и выйти" is first and comes second only by convention, because
  the pipe that carries "server shutdown 1" is held only while this panel
  lives. That is the one moment when the stop is the measured one, with the
  save pass and the countdown. Declining is not "never mind, close anyway":
  it cancels the close, because leaving the world running is a thing to do on
  purpose, and the way to do it on purpose is "Выход" in the tray, which says
  what it means.

  When the world was started outside this panel there is no pipe to write to,
  so its stop can only be forced. Saying so here costs one sentence and
  spares the surprise later.
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
	import { t } from '$lib/i18n.svelte.js'

	const worldUp = $derived(hub.realmRunning)
	const foreign = $derived(hub.stateOf('world') === 'external')
	const allUp = $derived(hub.total > 0 && hub.upCount === hub.total)
	const upList = $derived(hub.upNames.join(', '))
</script>

<Dialog open={hub.askQuit} onOpenChange={(o) => (hub.askQuit = o)}>
	<DialogContent data-role="quit-ask" class="border-gold-dim/40">
		<DialogHeader>
			<DialogTitle>{t('quit.title')}</DialogTitle>
			<DialogDescription class="text-muted-foreground">
				{#if !allUp}
					{t('quit.nowRunning')}<b class="text-foreground">{upList}</b>.
				{/if}
				{t('quit.order')}
				{#if worldUp}
					{t('quit.save')}
				{/if}
			</DialogDescription>
		</DialogHeader>

		{#if foreign}
			<p data-role="quit-ask-forced" class="text-xs leading-relaxed text-warn">
				{t('quit.forced')}
			</p>
		{/if}

		<p class="text-xs leading-relaxed text-muted-foreground">
			{t('quit.decline')}
		</p>

		<DialogFooter>
			<Button variant="outline" data-role="quit-stay" onclick={() => (hub.askQuit = false)}>
				{t('btn.close2')}
			</Button>
			<Button variant="destructive" data-role="quit-stop" onclick={() => hub.stopAndQuit()}>
				{t('quit.stopGo')}
			</Button>
		</DialogFooter>
	</DialogContent>
</Dialog>
