<script>
  import '../app.css';
  import '../i18n';
  import { isLoading } from 'svelte-i18n';
</script>

{#if $isLoading}
  <div style="display: flex; justify-content: center; align-items: center; height: 100vh;">
    <p>Loading...</p>
  </div>
{:else}
  <slot />
{/if}
