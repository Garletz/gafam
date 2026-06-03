<script lang="ts">
  import { goto } from '$app/navigation';

  let phoneInput = $state('');

  function addDigit(d: string) {
    if (phoneInput.length < 15) phoneInput += d;
  }

  function removeDigit() {
    phoneInput = phoneInput.slice(0, -1);
  }

  function handleSearch() {
    const clean = phoneInput.replace(/\s/g, '');
    if (clean.length >= 6) {
      if (window.location.hostname === 'localhost' || window.location.hostname.includes('127.0.0.1')) {
        window.location.href = `/${clean}`;
      } else {
        window.location.href = `https://${clean}.gafam.cloud`;
      }
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') handleSearch();
  }
</script>

<svelte:head>
  <title>GAFAM</title>
</svelte:head>

<main class="home">
  <div class="home__content">
    <div class="home__logo">GAFAM</div>

    <div class="search-box">
      <input
        type="tel"
        class="search-input"
        bind:value={phoneInput}
        onkeydown={handleKeydown}
        autofocus
      />
    </div>

    <div class="keyboard">
      <div class="keyboard-row">
        {#each ['1','2','3','4','5'] as d}
          <button class="key" onclick={() => addDigit(d)}>{d}</button>
        {/each}
      </div>
      <div class="keyboard-row">
        {#each ['6','7','8','9','0'] as d}
          <button class="key" onclick={() => addDigit(d)}>{d}</button>
        {/each}
      </div>
      <div class="keyboard-row">
        <button class="key key-action" onclick={() => addDigit('+')}>+</button>
        <button class="key key-action" onclick={removeDigit}>EFFACER</button>
        <button class="key key-action" onclick={handleSearch}>VALIDER</button>
      </div>
    </div>
  </div>
</main>

<style>
  .home {
    min-height: 100vh;
    display: flex;
    flex-direction: column;
    align-items: center;
    background: #ffffff;
  }

  .home__content {
    display: flex;
    flex-direction: column;
    align-items: center;
    width: 100%;
    margin-top: 20vh; /* Google style spacing from top */
    padding: 0 20px;
  }

  .home__logo {
    font-size: 80px;
    font-weight: 700;
    color: #202124;
    letter-spacing: -3px;
    user-select: none;
    margin-bottom: 30px;
  }

  .search-box {
    width: 100%;
    max-width: 584px; /* Exact Google search bar width */
    margin-bottom: 32px;
  }

  .search-input {
    width: 100%;
    height: 48px;
    padding: 0 20px;
    border-radius: 24px;
    border: 1px solid #dfe1e5;
    background: #ffffff;
    color: #202124;
    font-size: 18px;
    font-weight: 500;
    letter-spacing: 2px;
    text-align: center;
    transition: box-shadow 0.2s, border-color 0.2s;
  }

  .search-input:hover {
    box-shadow: 0 1px 6px rgba(32, 33, 36, 0.28);
    border-color: rgba(223, 225, 229, 0);
  }

  .search-input:focus {
    outline: none;
    box-shadow: 0 1px 6px rgba(32, 33, 36, 0.28);
    border-color: rgba(223, 225, 229, 0);
  }

  .keyboard {
    display: flex;
    flex-direction: column;
    gap: 12px;
    align-items: center;
  }

  .keyboard-row {
    display: flex;
    gap: 12px;
    justify-content: center;
  }

  .key {
    background: #f8f9fa;
    border: 1px solid #f8f9fa;
    border-radius: 4px;
    color: #3c4043;
    font-size: 18px;
    font-weight: 500;
    height: 40px;
    min-width: 50px;
    padding: 0 16px;
    transition: all 0.2s;
  }

  .key:hover {
    box-shadow: 0 1px 1px rgba(0,0,0,0.1);
    background: #f1f3f4;
    border: 1px solid #dadce0;
    color: #202124;
  }

  .key:active {
    background: #e8eaed;
  }

  .key-action {
    font-size: 14px;
    letter-spacing: 0.5px;
  }

  @media (max-width: 480px) {
    .home__logo { font-size: 60px; }
    .search-input { font-size: 16px; }
    .key { min-width: 44px; padding: 0 12px; height: 44px; }
  }
</style>
