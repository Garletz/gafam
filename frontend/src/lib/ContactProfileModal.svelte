<script lang="ts">
	import { blurContacts } from '$lib/privacy';
	import { encryptAESGCM } from '$lib/crypto';

	export interface ContactFull {
		id?: number;
		phone_number: string;
		display_name: string;
		email?: string;
		profession?: string;
		skills?: string[];
		languages?: string[];
		notes?: string;
		auto_profile?: string;
		auto_skills?: string[];
		auto_languages?: string[];
		auto_profession?: string;
		last_analyzed_at?: string;
		updated_at?: string;
	}

	interface Props {
		contact: ContactFull;
		vpcUrl: string;
		sessionToken: string;
		onClose: () => void;
		onOpenChat: (phone: string) => void;
		onSaved: (updated: ContactFull) => void;
	}

	let { contact, vpcUrl, sessionToken, onClose, onOpenChat, onSaved }: Props = $props();

	// Editable state copies
	let name = $state(contact.display_name || '');
	let phone = $state(contact.phone_number || '');
	let email = $state(contact.email || '');
	let profession = $state(contact.profession || '');
	let skills = $state<string[]>([...(contact.skills || [])]);
	let languages = $state<string[]>([...(contact.languages || ['fr'])]);
	let notes = $state(contact.notes || '');

	// AI deduced metadata
	let autoProfile = $state(contact.auto_profile || '');
	let autoSkills = $state<string[]>([...(contact.auto_skills || [])]);
	let autoLanguages = $state<string[]>([...(contact.auto_languages || [])]);
	let autoProfession = $state(contact.auto_profession || '');
	let lastAnalyzedAt = $state(contact.last_analyzed_at || '');

	let isSaving = $state(false);
	let isAnalyzing = $state(false);
	let saveMessage = $state('');
	let newSkillInput = $state('');
	let copiedPhone = $state(false);

	const availableLanguages = [
		{ code: 'fr', label: 'Français' },
		{ code: 'en', label: 'English' },
		{ code: 'es', label: 'Español' },
		{ code: 'de', label: 'Deutsch' },
		{ code: 'it', label: 'Italiano' },
		{ code: 'ar', label: 'Arabe' },
		{ code: 'zh', label: 'Chinois' },
		{ code: 'ru', label: 'Russe' },
		{ code: 'pt', label: 'Portugais' }
	];

	const suggestedSkills = [
		'Plomberie', 'Électricité', 'Mécanique', 'Développeur', 'Design',
		'Immobilier', 'Droit/Juridique', 'Comptabilité', 'Médical', 'Travaux/BTP',
		'Cuisine', 'Photo/Vidéo', 'Musique', 'Transport/Logistique', 'Sécurité'
	];

	function toggleSkill(skill: string) {
		const norm = skill.trim();
		if (!norm) return;
		const idx = skills.findIndex(s => s.toLowerCase() === norm.toLowerCase());
		if (idx >= 0) {
			skills = skills.filter((_, i) => i !== idx);
		} else {
			skills = [...skills, norm];
		}
	}

	function addCustomSkill() {
		const val = newSkillInput.trim();
		if (val && !skills.some(s => s.toLowerCase() === val.toLowerCase())) {
			skills = [...skills, val];
			newSkillInput = '';
		}
	}

	function toggleLanguage(code: string) {
		if (languages.includes(code)) {
			if (languages.length > 1) {
				languages = languages.filter(l => l !== code);
			}
		} else {
			languages = [...languages, code];
		}
	}

	function applyAutoProfession() {
		if (autoProfession) {
			profession = autoProfession;
		}
	}

	function applyAllAutoSkills() {
		const merged = new Set([...skills, ...autoSkills]);
		skills = Array.from(merged);
	}

	async function copyNumber() {
		try {
			await navigator.clipboard.writeText(phone);
			copiedPhone = true;
			setTimeout(() => (copiedPhone = false), 1800);
		} catch {}
	}

	async function handleSave() {
		isSaving = true;
		saveMessage = '';
		try {
			// Contacts are sensitive: encrypt over the Cloudflare→VPC hop,
			// same AES-GCM scheme as the rest of the app.
			const plaintext = JSON.stringify({
				phone_number: phone,
				display_name: name,
				email: email,
				profession: profession,
				skills: skills,
				languages: languages,
				notes: notes
			});
			const encrypted = await encryptAESGCM(plaintext, sessionToken);
			const res = await fetch(`/api/proxy/contacts/save?vpcUrl=${encodeURIComponent(vpcUrl)}&token=${encodeURIComponent(sessionToken)}`, {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify(encrypted)
			});
			if (res.ok) {
				saveMessage = 'Enregistré avec succès';
				const updated: ContactFull = {
					...contact,
					phone_number: phone,
					display_name: name,
					email: email,
					profession: profession,
					skills: skills,
					languages: languages,
					notes: notes,
					auto_profile: autoProfile,
					auto_skills: autoSkills,
					auto_languages: autoLanguages,
					auto_profession: autoProfession,
					last_analyzed_at: lastAnalyzedAt
				};
				onSaved(updated);
				setTimeout(() => (saveMessage = ''), 2500);
			} else {
				saveMessage = 'Erreur lors de la sauvegarde';
			}
		} catch (e: any) {
			saveMessage = 'Erreur réseau: ' + e.message;
		} finally {
			isSaving = false;
		}
	}

	async function handleAnalyze() {
		isAnalyzing = true;
		saveMessage = '';
		try {
			const plaintext = JSON.stringify({ phone_number: phone });
			const encrypted = await encryptAESGCM(plaintext, sessionToken);
			const res = await fetch(`/api/proxy/contacts/analyze?vpcUrl=${encodeURIComponent(vpcUrl)}&token=${encodeURIComponent(sessionToken)}`, {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify(encrypted)
			});
			if (res.ok) {
				const data: ContactFull = await res.json();
				if (data) {
					autoProfile = data.auto_profile || '';
					autoSkills = data.auto_skills || [];
					autoLanguages = data.auto_languages || [];
					autoProfession = data.auto_profession || '';
					lastAnalyzedAt = data.last_analyzed_at || new Date().toISOString();
					saveMessage = 'Analyse IA terminée';
					setTimeout(() => (saveMessage = ''), 2500);
				}
			} else {
				saveMessage = 'Échec de l\'analyse IA';
			}
		} catch (e: any) {
			saveMessage = 'Erreur IA: ' + e.message;
		} finally {
			isAnalyzing = false;
		}
	}

	function handleKeydown(e: KeyboardEvent) {
		if (e.key === 'Escape') {
			onClose();
		}
	}
</script>

<svelte:window onkeydown={handleKeydown} />

<!-- Modal Backdrop -->
<div class="c-modal-overlay" onclick={onClose} role="presentation">
	<div class="c-modal" onclick={(e) => e.stopPropagation()} role="dialog" aria-modal="true" aria-labelledby="contact-title">
		<!-- Header -->
		<div class="c-modal__header">
			<div class="c-modal__identity">
				<div class="c-modal__avatar" class:blurred={$blurContacts}>
					{ (name || phone || '?').charAt(0).toUpperCase() }
				</div>
				<div class="c-modal__titles">
					<h2 id="contact-title" class="c-modal__name" class:blurred={$blurContacts}>
						{name || 'Sans Nom'}
					</h2>
					<div class="c-modal__phone-badge">
						<span>{phone}</span>
						<button type="button" class="btn-subtle-copy" onclick={copyNumber}>
							{copiedPhone ? 'Copié' : 'Copier'}
						</button>
					</div>
				</div>
			</div>
			<div class="c-modal__header-actions">
				<button
					type="button"
					class="btn-action-chat"
					onclick={() => { onOpenChat(phone); onClose(); }}
					title="Ouvrir la conversation"
				>
					<svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2">
						<path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/>
					</svg>
					<span>Conversation</span>
				</button>
				<button type="button" class="btn-close" onclick={onClose} aria-label="Fermer">✕</button>
			</div>
		</div>

		<!-- Body Form -->
		<div class="c-modal__body">
			{#if saveMessage}
				<div class="c-alert">{saveMessage}</div>
			{/if}

			<!-- Grid: Left (Manual info) & Right (Autonomous AI) -->
			<div class="c-grid">
				<!-- Column 1: Manual Attributes -->
				<div class="c-col">
					<div class="c-section-title">Informations de Contact</div>
					
					<div class="c-form-group">
						<label for="c-name">Nom complet</label>
						<input id="c-name" type="text" bind:value={name} placeholder="Ex: Pierre Dupont" />
					</div>

					<div class="c-form-group">
						<label for="c-phone">Numéro de téléphone</label>
						<input id="c-phone" type="text" bind:value={phone} readonly class="is-readonly" />
					</div>

					<div class="c-form-group">
						<label for="c-email">Adresse Email</label>
						<input id="c-email" type="email" bind:value={email} placeholder="pierre@example.com" />
					</div>

					<div class="c-form-group">
						<label for="c-profession">Profession / Métier</label>
						<div class="c-input-with-action">
							<input id="c-profession" type="text" bind:value={profession} placeholder="Ex: Plombier indépendant" />
							{#if autoProfession && autoProfession !== profession}
								<button type="button" class="btn-inline-apply" onclick={applyAutoProfession} title="Appliquer la suggestion IA">
									Suggéré: {autoProfession}
								</button>
							{/if}
						</div>
					</div>

					<div class="c-form-group">
						<label for="c-notes">Notes & Contexte</label>
						<textarea id="c-notes" rows="3" bind:value={notes} placeholder="Notes personnelles sur ce contact..."></textarea>
					</div>

					<!-- Languages -->
					<div class="c-form-group">
						<label>Langues parlées</label>
						<div class="c-tags-wrap">
							{#each availableLanguages as l}
								<button
									type="button"
									class="c-lang-tag {languages.includes(l.code) ? 'is-active' : ''}"
									onclick={() => toggleLanguage(l.code)}
								>
									{l.label}
								</button>
							{/each}
						</div>
					</div>
				</div>

				<!-- Column 2: Skills & AI Insights -->
				<div class="c-col">
					<div class="c-section-title">Compétences & Métiers (Skills)</div>

					<!-- Active Skills Pills -->
					<div class="c-skills-box">
						<div class="c-skills-label">Compétences attribuées :</div>
						<div class="c-active-skills">
							{#if skills.length === 0}
								<span class="c-empty-hint">Aucune compétence ajoutée</span>
							{/if}
							{#each skills as sk}
								<span class="c-skill-pill">
									{sk}
									<button type="button" class="c-pill-del" onclick={() => toggleSkill(sk)}>✕</button>
								</span>
							{/each}
						</div>

						<!-- Add Custom Skill -->
						<div class="c-add-skill-row">
							<input
								type="text"
								bind:value={newSkillInput}
								placeholder="Ajouter une compétence..."
								onkeydown={(e) => e.key === 'Enter' && (e.preventDefault(), addCustomSkill())}
							/>
							<button type="button" class="btn-pill-add" onclick={addCustomSkill}>+ Ajouter</button>
						</div>

						<!-- Suggested common skills -->
						<div class="c-suggested-title">Suggestions rapides :</div>
						<div class="c-suggested-grid">
							{#each suggestedSkills as s}
								<button
									type="button"
									class="c-suggest-btn {skills.some(sk => sk.toLowerCase() === s.toLowerCase()) ? 'is-selected' : ''}"
									onclick={() => toggleSkill(s)}
								>
									{skills.some(sk => sk.toLowerCase() === s.toLowerCase()) ? '✓ ' : '+ '}{s}
								</button>
							{/each}
						</div>
					</div>

					<!-- Autonomous AI Deduction Card -->
					<div class="c-ai-card">
						<div class="c-ai-card__header">
							<div class="c-ai-card__title">
								<span class="c-ai-dot"></span>
								<span>Déduction Autonome IA (Vector / SMS)</span>
							</div>
							<button
								type="button"
								class="btn-ai-analyze"
								disabled={isAnalyzing}
								onclick={handleAnalyze}
							>
								{isAnalyzing ? 'Analyse en cours…' : 'Ré-analyser'}
							</button>
						</div>

						<div class="c-ai-card__body">
							{#if autoProfile}
								<p class="c-ai-summary">"{autoProfile}"</p>
							{:else}
								<p class="c-ai-summary is-empty">
									L'IA n'a pas encore analysé les SMS échangés avec ce contact. Clique sur « Ré-analyser » pour déduire ses compétences.
								</p>
							{/if}

							{#if autoSkills && autoSkills.length > 0}
								<div class="c-ai-detected-row">
									<span class="c-ai-detected-label">Compétences détectées :</span>
									<div class="c-tags-wrap">
										{#each autoSkills as ask}
											<button
												type="button"
												class="c-auto-tag {skills.includes(ask) ? 'is-added' : ''}"
												onclick={() => toggleSkill(ask)}
												title="Cliquer pour ajouter aux compétences du contact"
											>
												{skills.includes(ask) ? '✓ ' : '+ '}{ask}
											</button>
										{/each}
									</div>
									<button type="button" class="btn-apply-all" onclick={applyAllAutoSkills}>
										Tout fusionner dans le profil
									</button>
								</div>
							{/if}

							{#if lastAnalyzedAt}
								<div class="c-ai-date">Dernière analyse : {lastAnalyzedAt}</div>
							{/if}
						</div>
					</div>
				</div>
			</div>
		</div>

		<!-- Footer Actions -->
		<div class="c-modal__footer">
			<button type="button" class="btn-secondary" onclick={onClose}>Annuler</button>
			<button type="button" class="btn-primary" disabled={isSaving} onclick={handleSave}>
				{isSaving ? 'Enregistrement…' : 'Enregistrer le profil'}
			</button>
		</div>
	</div>
</div>

<style>
	.c-modal-overlay {
		position: fixed;
		inset: 0;
		background: rgba(0, 0, 0, 0.85);
		backdrop-filter: blur(8px);
		z-index: 9999;
		display: flex;
		align-items: center;
		justify-content: center;
		padding: 16px;
		animation: fadeIn 0.15s ease-out;
	}

	.c-modal {
		background: #000000;
		color: #ffffff;
		border: 1px solid #333333;
		border-radius: 12px;
		width: 100%;
		max-width: 900px;
		max-height: 90vh;
		display: flex;
		flex-direction: column;
		box-shadow: 0 20px 50px rgba(0, 0, 0, 0.9);
		overflow: hidden;
	}

	.c-modal__header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: 20px 24px;
		border-bottom: 1px solid #222222;
		background: #080808;
	}

	.c-modal__identity {
		display: flex;
		align-items: center;
		gap: 16px;
	}

	.c-modal__avatar {
		width: 48px;
		height: 48px;
		border-radius: 50%;
		background: #ffffff;
		color: #000000;
		font-weight: 700;
		font-size: 20px;
		display: flex;
		align-items: center;
		justify-content: center;
		border: 1px solid #ffffff;
	}

	.c-modal__name {
		margin: 0;
		font-size: 18px;
		font-weight: 600;
		letter-spacing: -0.01em;
	}

	.c-modal__phone-badge {
		display: flex;
		align-items: center;
		gap: 8px;
		font-size: 13px;
		color: #888888;
		font-family: monospace;
		margin-top: 4px;
	}

	.btn-subtle-copy {
		background: transparent;
		border: 1px solid #333333;
		color: #aaaaaa;
		border-radius: 4px;
		font-size: 11px;
		padding: 2px 6px;
		cursor: pointer;
	}
	.btn-subtle-copy:hover {
		background: #222222;
		color: #ffffff;
	}

	.c-modal__header-actions {
		display: flex;
		align-items: center;
		gap: 12px;
	}

	.btn-action-chat {
		display: flex;
		align-items: center;
		gap: 6px;
		background: #ffffff;
		color: #000000;
		border: 1px solid #ffffff;
		padding: 8px 14px;
		border-radius: 6px;
		font-size: 13px;
		font-weight: 600;
		cursor: pointer;
		transition: transform 0.1s;
	}
	.btn-action-chat:hover {
		background: #e0e0e0;
		transform: scale(1.02);
	}

	.btn-close {
		background: transparent;
		border: 1px solid #333333;
		color: #888888;
		width: 32px;
		height: 32px;
		border-radius: 6px;
		cursor: pointer;
		font-size: 14px;
		display: flex;
		align-items: center;
		justify-content: center;
	}
	.btn-close:hover {
		background: #222222;
		color: #ffffff;
	}

	.c-modal__body {
		padding: 24px;
		overflow-y: auto;
		flex: 1;
	}

	.c-alert {
		background: #111111;
		border: 1px solid #444444;
		color: #ffffff;
		padding: 10px 14px;
		border-radius: 6px;
		margin-bottom: 16px;
		font-size: 13px;
	}

	.c-grid {
		display: grid;
		grid-template-columns: 1fr 1.2fr;
		gap: 24px;
	}

	@media (max-width: 768px) {
		.c-grid {
			grid-template-columns: 1fr;
		}
	}

	.c-section-title {
		font-size: 12px;
		text-transform: uppercase;
		letter-spacing: 0.08em;
		color: #666666;
		font-weight: 700;
		margin-bottom: 14px;
		padding-bottom: 6px;
		border-bottom: 1px solid #222222;
	}

	.c-form-group {
		margin-bottom: 14px;
	}

	.c-form-group label {
		display: block;
		font-size: 12px;
		font-weight: 500;
		color: #aaaaaa;
		margin-bottom: 6px;
	}

	.c-form-group input,
	.c-form-group textarea {
		width: 100%;
		background: #0d0d0d;
		border: 1px solid #262626;
		border-radius: 6px;
		padding: 9px 12px;
		color: #ffffff;
		font-size: 13px;
		box-sizing: border-box;
		outline: none;
	}
	.c-form-group input:focus,
	.c-form-group textarea:focus {
		border-color: #666666;
		background: #121212;
	}
	.c-form-group input.is-readonly {
		color: #777777;
		background: #080808;
		border-color: #1a1a1a;
	}

	.c-input-with-action {
		display: flex;
		flex-direction: column;
		gap: 6px;
	}

	.btn-inline-apply {
		align-self: flex-start;
		background: #1a1a1a;
		border: 1px dashed #555555;
		color: #cccccc;
		font-size: 11px;
		padding: 3px 8px;
		border-radius: 4px;
		cursor: pointer;
	}
	.btn-inline-apply:hover {
		background: #ffffff;
		color: #000000;
	}

	.c-tags-wrap {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
	}

	.c-lang-tag {
		background: #111111;
		border: 1px solid #262626;
		color: #777777;
		font-size: 11px;
		padding: 4px 10px;
		border-radius: 4px;
		cursor: pointer;
	}
	.c-lang-tag.is-active {
		background: #ffffff;
		border-color: #ffffff;
		color: #000000;
		font-weight: 600;
	}

	.c-skills-box {
		background: #080808;
		border: 1px solid #222222;
		border-radius: 8px;
		padding: 14px;
		margin-bottom: 16px;
	}

	.c-skills-label {
		font-size: 12px;
		color: #888888;
		margin-bottom: 8px;
	}

	.c-active-skills {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
		min-height: 32px;
		margin-bottom: 12px;
	}

	.c-empty-hint {
		font-size: 12px;
		color: #555555;
		font-style: italic;
	}

	.c-skill-pill {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		background: #ffffff;
		color: #000000;
		font-size: 12px;
		font-weight: 600;
		padding: 4px 8px;
		border-radius: 4px;
	}

	.c-pill-del {
		background: transparent;
		border: none;
		color: #000000;
		font-size: 10px;
		cursor: pointer;
		padding: 0;
		line-height: 1;
	}

	.c-add-skill-row {
		display: flex;
		gap: 8px;
		margin-bottom: 14px;
	}
	.c-add-skill-row input {
		flex: 1;
		background: #111111;
		border: 1px solid #333333;
		border-radius: 4px;
		padding: 6px 10px;
		color: #ffffff;
		font-size: 12px;
	}
	.btn-pill-add {
		background: #222222;
		border: 1px solid #444444;
		color: #ffffff;
		font-size: 12px;
		padding: 6px 12px;
		border-radius: 4px;
		cursor: pointer;
	}
	.btn-pill-add:hover {
		background: #ffffff;
		color: #000000;
	}

	.c-suggested-title {
		font-size: 11px;
		color: #666666;
		margin-bottom: 6px;
	}

	.c-suggested-grid {
		display: flex;
		flex-wrap: wrap;
		gap: 4px;
	}

	.c-suggest-btn {
		background: #111111;
		border: 1px solid #222222;
		color: #888888;
		font-size: 11px;
		padding: 3px 8px;
		border-radius: 4px;
		cursor: pointer;
	}
	.c-suggest-btn.is-selected {
		background: #222222;
		border-color: #555555;
		color: #ffffff;
	}

	.c-ai-card {
		background: #080808;
		border: 1px solid #262626;
		border-radius: 8px;
		padding: 14px;
	}

	.c-ai-card__header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		margin-bottom: 10px;
	}

	.c-ai-card__title {
		display: flex;
		align-items: center;
		gap: 6px;
		font-size: 12px;
		font-weight: 600;
		color: #ffffff;
	}

	.c-ai-dot {
		width: 6px;
		height: 6px;
		border-radius: 50%;
		background: #ffffff;
		box-shadow: 0 0 6px rgba(255, 255, 255, 0.8);
	}

	.btn-ai-analyze {
		background: transparent;
		border: 1px solid #444444;
		color: #ffffff;
		font-size: 11px;
		padding: 4px 10px;
		border-radius: 4px;
		cursor: pointer;
	}
	.btn-ai-analyze:hover:not(:disabled) {
		background: #ffffff;
		color: #000000;
	}

	.c-ai-summary {
		font-size: 12px;
		color: #cccccc;
		font-style: italic;
		line-height: 1.5;
		margin: 0 0 10px 0;
	}
	.c-ai-summary.is-empty {
		color: #666666;
		font-style: normal;
	}

	.c-ai-detected-row {
		border-top: 1px solid #1a1a1a;
		padding-top: 10px;
		display: flex;
		flex-direction: column;
		gap: 8px;
	}

	.c-ai-detected-label {
		font-size: 11px;
		color: #888888;
	}

	.c-auto-tag {
		background: #141414;
		border: 1px dashed #555555;
		color: #cccccc;
		font-size: 11px;
		padding: 3px 8px;
		border-radius: 4px;
		cursor: pointer;
	}
	.c-auto-tag.is-added {
		border-style: solid;
		background: #2a2a2a;
		color: #ffffff;
	}

	.btn-apply-all {
		align-self: flex-start;
		background: transparent;
		border: 1px solid #333333;
		color: #aaaaaa;
		font-size: 11px;
		padding: 4px 8px;
		border-radius: 4px;
		cursor: pointer;
		margin-top: 4px;
	}
	.btn-apply-all:hover {
		background: #ffffff;
		color: #000000;
	}

	.c-ai-date {
		font-size: 10px;
		color: #555555;
		margin-top: 8px;
		font-family: monospace;
	}

	.c-modal__footer {
		display: flex;
		align-items: center;
		justify-content: flex-end;
		gap: 10px;
		padding: 16px 24px;
		border-top: 1px solid #222222;
		background: #080808;
	}

	.btn-secondary {
		background: transparent;
		border: 1px solid #333333;
		color: #aaaaaa;
		padding: 8px 16px;
		border-radius: 6px;
		font-size: 13px;
		cursor: pointer;
	}
	.btn-secondary:hover {
		background: #1a1a1a;
		color: #ffffff;
	}

	.btn-primary {
		background: #ffffff;
		border: 1px solid #ffffff;
		color: #000000;
		padding: 8px 20px;
		border-radius: 6px;
		font-size: 13px;
		font-weight: 600;
		cursor: pointer;
	}
	.btn-primary:hover:not(:disabled) {
		background: #e0e0e0;
	}
	.btn-primary:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	@keyframes fadeIn {
		from { opacity: 0; }
		to { opacity: 1; }
	}
</style>
