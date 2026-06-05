import re

with open("frontend/src/routes/[phone]/+page.svelte", "r") as f:
    content = f.read()

# 1. Update Script
script_match = re.search(r'<script lang="ts">(.*?)</script>', content, re.DOTALL)
if script_match:
    script = script_match.group(1)
    
    # Add new state variables
    if "let contacts = $state" not in script:
        script = script.replace("let smsList = $state<any[]>([]);", 
                                "let smsList = $state<any[]>([]);\n  let contacts = $state<Record<string, string>>({});\n  let selectedSender = $state<string | null>(null);")
    
    # Add loadContacts function and update loadSms
    new_funcs = """
  let conversations = $derived(() => {
    const groups: Record<string, any[]> = {};
    for (const sms of smsList) {
      if (!groups[sms.sender]) groups[sms.sender] = [];
      groups[sms.sender].push(sms);
    }
    // Sort each group by timestamp ascending
    for (const k in groups) {
      groups[k].sort((a,b) => a.timestamp - b.timestamp);
    }
    return groups;
  });

  async function loadContacts() {
    try {
      const proxyParams = new URLSearchParams({ vpcUrl, token: sessionToken, certFingerprint });
      const res = await fetch(`/api/proxy/contacts?${proxyParams.toString()}`);
      if (res.ok) {
        const list = await res.json();
        const map: Record<string, string> = {};
        for (const c of list) {
          map[c.phone_number] = c.display_name;
        }
        contacts = map;
      }
    } catch(e) {}
  }
"""
    if "loadContacts()" not in script:
        script = script.replace("async function loadSms() {", new_funcs + "\n  async function loadSms() {\n    loadContacts();")
    
    content = content[:script_match.start(1)] + script + content[script_match.end(1):]

# 2. Update HTML
html_dashboard = """
      <!-- CONNECTED: SMS DASHBOARD -->
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
"""

content = re.sub(r'<!-- CONNECTED: SMS DASHBOARD -->.*?</div>\s*</div>\s*{/if}\s*</div>\s*</main>', html_dashboard + "\n    {/if}\n  </div>\n</main>", content, flags=re.DOTALL)

# 3. Append CSS
css_additions = """
  /* Messenger UI */
  .messenger-ui {
    display: flex;
    height: 80vh;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--bg-card);
    overflow: hidden;
  }
  .sidebar {
    width: 300px;
    border-right: 1px solid var(--border);
    display: flex;
    flex-direction: column;
    background: #000;
  }
  .sidebar__header {
    padding: 20px;
    border-bottom: 1px solid var(--border);
    display: flex;
    justify-content: space-between;
    align-items: center;
    color: white;
  }
  .sidebar__list {
    flex: 1;
    overflow-y: auto;
  }
  .chat-item {
    display: flex;
    padding: 15px 20px;
    border: none;
    background: transparent;
    width: 100%;
    text-align: left;
    cursor: pointer;
    border-bottom: 1px solid var(--border);
    gap: 12px;
    align-items: center;
    color: white;
  }
  .chat-item:hover, .chat-item.active {
    background: #111;
  }
  .chat-item__avatar {
    width: 40px;
    height: 40px;
    border-radius: 50%;
    background: #333;
    display: flex;
    align-items: center;
    justify-content: center;
    font-weight: bold;
    color: white;
  }
  .chat-item__info {
    flex: 1;
    overflow: hidden;
  }
  .chat-item__name {
    font-weight: 600;
    margin-bottom: 4px;
  }
  .chat-item__preview {
    font-size: 12px;
    color: #888;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .chat-main {
    flex: 1;
    display: flex;
    flex-direction: column;
    background: #050505;
  }
  .chat-main__header {
    padding: 20px;
    border-bottom: 1px solid var(--border);
    background: #000;
    color: white;
  }
  .chat-main__messages {
    flex: 1;
    overflow-y: auto;
    padding: 20px;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }
  .msg {
    align-self: flex-start;
    max-width: 70%;
  }
  .msg__bubble {
    background: #222;
    color: white;
    padding: 12px 16px;
    border-radius: 12px;
    border-bottom-left-radius: 4px;
    font-size: 14px;
    line-height: 1.5;
  }
  .msg__time {
    font-size: 11px;
    color: #666;
    margin-top: 4px;
    margin-left: 4px;
  }
  .chat-main__input {
    padding: 20px;
    border-top: 1px solid var(--border);
    background: #000;
  }
  .chat-main__empty {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    color: #666;
  }
  .outbox-form {
    display: flex;
    gap: 12px;
  }
  .outbox-form input {
    flex: 1;
    padding: 14px;
    border-radius: 20px;
    border: 1px solid #333;
    background: #111;
    color: white;
  }
  .btn-send {
    padding: 0 24px;
    border-radius: 20px;
    background: white;
    color: black;
    font-weight: bold;
    border: none;
    cursor: pointer;
  }
  .btn-icon {
    background: transparent;
    border: none;
    color: white;
    cursor: pointer;
    font-size: 18px;
  }
</style>
"""

content = content.replace("</style>", css_additions)

with open("frontend/src/routes/[phone]/+page.svelte", "w") as f:
    f.write(content)
