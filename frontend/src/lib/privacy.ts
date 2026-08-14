import { writable } from 'svelte/store';
import { browser } from '$app/environment';

// Client-only display preference: blur contact names in the chat list so a
// shoulder-surfer can't read who you're talking to. Persisted in localStorage,
// not synced to the VPC — it's a per-device privacy setting.
const KEY = 'gafam_blur_contacts';

const initial =
	browser && typeof localStorage !== 'undefined' && localStorage.getItem(KEY) === '1';

export const blurContacts = writable(initial);

export function setBlurContacts(v: boolean) {
	blurContacts.set(v);
	if (browser) {
		try {
			localStorage.setItem(KEY, v ? '1' : '0');
		} catch {}
	}
}
