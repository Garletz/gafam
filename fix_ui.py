with open("frontend/src/routes/[phone]/+page.svelte", "r") as f:
    lines = f.readlines()

# Extract script (lines 1 to 390 are the <script lang="ts"> ... </script>)
script_part = "".join(lines[:390])

html_part = """
<svelte:head>
  <title>{phone} — GAFAM Relay</title>
</svelte:head>

<main class="relay-page">
  <header class="relay-header">
    <a href="/" class="relay-header__logo">
      <span class="logo-g">G</span><span class="logo-rest">AFAM</span>
    </a>
    <div class="relay-header__phone">{phone}</div>
    {#if state === 'connected'}
      <button class="relay-header__logout" onclick={logout}>Logout</button>
    {/if}
  </header>

  <div class="relay-content">
    {#if state === 'setup'}
      <!-- SETUP CHALLENGE -->
      <div class="login-card">
        <h2 class="login-card__title">Authorization Required</h2>
        <p class="login-card__desc">Press <strong>"Authorize Web Login"</strong> on your phone and enter the challenge time below.</p>

        <form class="login-card__field" onsubmit={startChallengeFlow}>
          <label>Challenge Time</label>
          <input type="text" placeholder="e.g. 18:36" bind:value={inputTime} required />
          <button type="submit" class="login-card__btn" style="width:100%; margin-top:16px;">Next</button>
        </form>

        {#if statusMsg}
          <p class="login-card__status">{statusMsg}</p>
        {/if}
      </div>

    {:else if state === 'waiting'}
      <!-- WAITING FOR TARGET TIME -->
      <div class="login-card">
        <h2 class="login-card__title">Safe Retrieved</h2>
        <p class="login-card__desc">Waiting for {challengeTimeStr.substring(0,2)}:{challengeTimeStr.substring(2,4)} to open the safe.</p>

        <div class="countdown-display">
          {timeRemaining}s
        </div>

        <div class="waiting-dots">
          <span></span><span></span><span></span>
        </div>
      </div>

    {:else if state === 'challenge'}
      <!-- ACTIVE CHALLENGE (CLICKS) -->
      <div class="login-card challenge-card">
        <h2 class="login-card__title">Challenge Active</h2>
        <p class="login-card__desc">Click the button below the exact number of times shown on your phone.</p>

        <div class="challenge-timer">Time left: {challengeRemaining}s</div>
        
        <button class="challenge-btn" onclick={registerClick}>
          <div class="btn-pulse"></div>
          IMPULSE
        </button>
        
        <div class="challenge-counter">Registered impulses: {challengeClicks}</div>
      </div>

    {:else}
      <!-- CONNECTED: NEW MESSENGER UI -->
      <div class="messenger-ui">
        <!-- SIDEBAR -->
        <aside class="sidebar">
          <div class="sidebar__header">
            <h2>Chats</h2>
            <button class="btn-icon" onclick={loadSms}>↻</button>
          </div>
          <div class="sidebar__list">
            {#each Object.keys(conversations()) as sender}
              <button class="chat-item {selectedSender === sender ? 'active' : ''}" onclick={() => selectedSender = sender}>
                <div class="chat-item__avatar">{ (contacts[sender] || sender).charAt(0).toUpperCase() }</div>
                <div class="chat-item__info">
                  <div class="chat-item__name">{contacts[sender] || sender}</div>
                  <div class="chat-item__preview">{conversations()[sender][conversations()[sender].length - 1].body.substring(0, 30)}...</div>
                </div>
              </button>
            {/each}
          </div>
        </aside>

        <!-- MAIN CHAT -->
        <main class="chat-main">
          {#if selectedSender}
            <div class="chat-main__header">
              <h3>{contacts[selectedSender] || selectedSender}</h3>
            </div>
            <div class="chat-main__messages">
              {#each conversations()[selectedSender] as sms}
                <div class="msg">
                  <div class="msg__bubble">{sms.body}</div>
                  <div class="msg__time">{formatTime(sms.timestamp)}</div>
                </div>
              {/each}
            </div>
            <div class="chat-main__input">
              <form class="outbox-form" onsubmit={sendSms}>
                <input type="text" placeholder="Send a message..." bind:value={outboxBody} required />
                <button type="submit" class="btn-send" onclick={() => outboxRecipient = selectedSender!}>Send</button>
              </form>
              {#if outboxStatus}
                <div class="outbox-status">{outboxStatus}</div>
              {/if}
            </div>
          {:else}
            <div class="chat-main__empty">
              <p>Select a chat to start messaging</p>
            </div>
          {/if}
        </main>
      </div>
    {/if}
  </div>
</main>
"""

css_part = """
<style>
  :global(body) {
    background: #000;
    color: white;
    margin: 0;
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
  }
  .relay-page {
    min-height: 100vh;
    display: flex;
    flex-direction: column;
  }
  .relay-header {
    display: flex;
    align-items: center;
    padding: 16px 32px;
    background: #111;
    border-bottom: 1px solid #333;
  }
  .relay-header__logo {
    font-size: 20px;
    font-weight: bold;
    text-decoration: none;
    color: white;
    margin-right: 16px;
  }
  .logo-g { color: white; }
  .logo-rest { color: #888; }
  .relay-header__phone {
    flex: 1;
    color: #aaa;
    font-size: 14px;
    letter-spacing: 1px;
  }
  .relay-header__logout {
    padding: 6px 16px;
    background: transparent;
    border: 1px solid #555;
    color: #ccc;
    border-radius: 6px;
    cursor: pointer;
  }
  .relay-header__logout:hover {
    border-color: white;
    color: white;
  }
  .relay-content {
    flex: 1;
    display: flex;
    justify-content: center;
    align-items: center;
    padding: 20px;
  }
  .login-card {
    background: #111;
    padding: 40px;
    border-radius: 12px;
    border: 1px solid #333;
    text-align: center;
    width: 100%;
    max-width: 400px;
  }
  .login-card__title { margin: 0 0 10px; font-size: 20px; }
  .login-card__desc { color: #888; font-size: 14px; margin-bottom: 24px; }
  .login-card__field label { display: block; text-align: left; margin-bottom: 8px; color: #aaa; font-size: 12px; text-transform: uppercase; }
  .login-card__field input { width: 100%; box-sizing: border-box; padding: 12px; background: #222; border: 1px solid #444; color: white; border-radius: 6px; margin-bottom: 16px; }
  .login-card__btn { width: 100%; padding: 12px; background: white; color: black; font-weight: bold; border: none; border-radius: 6px; cursor: pointer; }
  .login-card__status { color: #ff4444; margin-top: 16px; font-size: 13px; }
  
  .challenge-btn {
    width: 150px; height: 150px; border-radius: 50%; background: #333; color: white; font-size: 18px; border: none; cursor: pointer; margin: 20px auto; display: block; box-shadow: 0 0 20px rgba(255,255,255,0.1);
  }
  .challenge-btn:active { background: #555; transform: scale(0.95); }
  
  .messenger-ui {
    display: flex;
    width: 100%;
    max-width: 1200px;
    height: 80vh;
    border: 1px solid #333;
    border-radius: 12px;
    overflow: hidden;
    background: #0a0a0a;
  }
  .sidebar {
    width: 300px;
    border-right: 1px solid #333;
    display: flex;
    flex-direction: column;
    background: #111;
  }
  .sidebar__header {
    padding: 20px;
    border-bottom: 1px solid #333;
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  .sidebar__header h2 { margin: 0; font-size: 18px; }
  .btn-icon { background: none; border: none; color: #888; cursor: pointer; font-size: 18px; }
  .btn-icon:hover { color: white; }
  .sidebar__list { flex: 1; overflow-y: auto; }
  .chat-item {
    display: flex;
    padding: 15px 20px;
    border: none;
    background: transparent;
    width: 100%;
    text-align: left;
    cursor: pointer;
    border-bottom: 1px solid #222;
    gap: 12px;
    align-items: center;
    color: white;
  }
  .chat-item:hover, .chat-item.active { background: #222; }
  .chat-item__avatar {
    width: 40px; height: 40px; border-radius: 50%; background: #444; display: flex; align-items: center; justify-content: center; font-weight: bold;
  }
  .chat-item__info { flex: 1; overflow: hidden; }
  .chat-item__name { font-weight: 600; font-size: 15px; margin-bottom: 4px; }
  .chat-item__preview { font-size: 13px; color: #888; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  
  .chat-main {
    flex: 1;
    display: flex;
    flex-direction: column;
  }
  .chat-main__header {
    padding: 20px;
    border-bottom: 1px solid #333;
    background: #111;
  }
  .chat-main__header h3 { margin: 0; font-size: 18px; }
  .chat-main__messages {
    flex: 1;
    overflow-y: auto;
    padding: 20px;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }
  .msg { align-self: flex-start; max-width: 70%; }
  .msg__bubble { background: #222; color: white; padding: 12px 16px; border-radius: 16px; border-bottom-left-radius: 4px; font-size: 15px; line-height: 1.4; }
  .msg__time { font-size: 11px; color: #666; margin-top: 4px; margin-left: 4px; }
  
  .chat-main__input {
    padding: 20px;
    border-top: 1px solid #333;
    background: #111;
  }
  .chat-main__empty {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    color: #666;
  }
  .outbox-form { display: flex; gap: 12px; }
  .outbox-form input { flex: 1; padding: 14px 20px; border-radius: 24px; border: 1px solid #444; background: #000; color: white; font-size: 15px; outline: none; }
  .outbox-form input:focus { border-color: #666; }
  .btn-send { padding: 0 24px; border-radius: 24px; background: white; color: black; font-weight: 600; font-size: 15px; border: none; cursor: pointer; }
  .btn-send:hover { background: #ccc; }
  .outbox-status { font-size: 13px; color: #888; margin-top: 8px; text-align: center; }
</style>
"""

with open("frontend/src/routes/[phone]/+page.svelte", "w") as f:
    f.write(script_part + html_part + css_part)

