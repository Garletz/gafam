# Deployment Rules

- **Cloudflare Frontend**: To deploy the web interface to Cloudflare, you MUST compile the SvelteKit project first. Execute `npm run build && npx wrangler deploy` from within the `frontend/` directory. Do not pass a project name explicitly.

