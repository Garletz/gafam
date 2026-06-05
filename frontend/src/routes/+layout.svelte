<script lang="ts">
  import '../app.css';
  import { onMount } from 'svelte';
  import { page } from '$app/stores';

  let { children } = $props();

  let connectedAccounts = $state<string[]>([]);
  let isMenuOpen = $state(false);

  function loadAccounts() {
    const accounts: string[] = [];
    for (let i = 0; i < localStorage.length; i++) {
      const key = localStorage.key(i);
      if (key && key.startsWith('gafam_auth_')) {
        const phone = key.replace('gafam_auth_', '');
        accounts.push(phone);
      }
    }
    connectedAccounts = accounts;
  }

  onMount(() => {
    loadAccounts();
    window.addEventListener('storage', loadAccounts);
    window.addEventListener('gafam-auth-changed', loadAccounts);
    return () => {
      window.removeEventListener('storage', loadAccounts);
      window.removeEventListener('gafam-auth-changed', loadAccounts);
    };
  });

  async function logoutAccount(phone: string) {
    try {
      const authData = JSON.parse(localStorage.getItem(`gafam_auth_${phone}`) || '{}');
      if (authData.vpcUrl && authData.sessionToken) {
        const proxyParams = new URLSearchParams({ vpcUrl: authData.vpcUrl, token: authData.sessionToken, certFingerprint: authData.certFingerprint || '' });
        fetch(`/api/proxy?${proxyParams.toString()}`, { method: 'DELETE' }).catch(() => {});
      }
    } catch(e) {}

    localStorage.removeItem(`gafam_auth_${phone}`);
    loadAccounts();
    window.dispatchEvent(new Event('gafam-auth-changed'));
    
    if ($page.url.pathname === `/${phone}`) {
      window.location.href = '/';
    }
  }
</script>

<div class="app-container">
  <header class="global-header">
    <a href="/" class="global-header__logo">
      GAFAM
    </a>
    
    <div class="global-header__spacer"></div>

    <div class="account-menu-container">
      <button class="account-avatar" onclick={() => isMenuOpen = !isMenuOpen}>
        {connectedAccounts.length}
      </button>
      
      {#if isMenuOpen}
        <div class="account-dropdown">
          <div class="account-dropdown__title">Connected Accounts</div>
          {#if connectedAccounts.length === 0}
            <div class="account-dropdown__empty">0 accounts</div>
          {:else}
            <div class="account-list">
              {#each connectedAccounts as phone}
                <div class="account-item">
                  <a href={`/${phone}`} class="account-item__phone" onclick={() => isMenuOpen = false}>{phone}</a>
                  <button class="account-item__logout" onclick={() => logoutAccount(phone)}>Sign out</button>
                </div>
              {/each}
            </div>
          {/if}
        </div>
      {/if}
    </div>
  </header>

  <div class="app-content">
    {@render children()}
  </div>
</div>

<style>
  .app-container {
    min-height: 100vh;
    display: flex;
    flex-direction: column;
  }
  .global-header {
    display: flex;
    align-items: center;
    padding: 12px 24px;
    background: #ffffff;
    border-bottom: 1px solid #dfe1e5;
    position: relative;
    z-index: 50;
  }
  .global-header__logo {
    font-size: 22px;
    font-weight: 600;
    text-decoration: none;
    color: #202124;
    letter-spacing: -0.5px;
  }
  .global-header__spacer {
    flex: 1;
  }
  
  .account-menu-container {
    position: relative;
  }
  
  .account-avatar {
    width: 36px;
    height: 36px;
    border-radius: 50%;
    background: #ffffff;
    color: #202124;
    font-size: 16px;
    font-weight: 600;
    border: 1px solid #dfe1e5;
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    transition: all 0.2s;
  }
  .account-avatar:hover {
    background: #f8f9fa;
    border-color: #bdc1c6;
  }
  
  .account-dropdown {
    position: absolute;
    top: 48px;
    right: 0;
    background: #ffffff;
    border-radius: 8px;
    box-shadow: 0 4px 12px rgba(0,0,0,0.15);
    border: 1px solid #dfe1e5;
    width: 260px;
    padding: 16px 0;
    z-index: 100;
  }
  .account-dropdown__title {
    font-size: 13px;
    color: #5f6368;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    font-weight: 600;
    padding: 0 16px 12px;
    border-bottom: 1px solid #f1f3f4;
    margin-bottom: 8px;
  }
  .account-dropdown__empty {
    padding: 16px;
    text-align: center;
    color: #5f6368;
    font-size: 14px;
  }
  .account-list {
    display: flex;
    flex-direction: column;
  }
  .account-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 10px 16px;
    transition: background 0.2s;
  }
  .account-item:hover {
    background: #f8f9fa;
  }
  .account-item__phone {
    font-size: 15px;
    color: #202124;
    font-weight: 500;
    text-decoration: none;
  }
  .account-item__phone:hover {
    text-decoration: underline;
  }
  .account-item__logout {
    padding: 6px 12px;
    background: #ffffff;
    border: 1px solid #dfe1e5;
    color: #3c4043;
    border-radius: 4px;
    cursor: pointer;
    font-size: 13px;
    font-weight: 500;
  }
  .account-item__logout:hover {
    background: #f1f3f4;
  }

  .app-content {
    flex: 1;
    display: flex;
    flex-direction: column;
  }
</style>
