/**
 * MapLibre GL + local Protomaps PMTiles (VPC) — no CDN.
 * Fonts/sprites: /geo/basemaps-assets/*
 * Tiles: /api/proxy/geo?action=pmtiles (HTTP Range → VPC)
 */
import {
	Map,
	Marker,
	NavigationControl,
	addProtocol,
	type StyleSpecification
} from 'maplibre-gl';
import { Protocol } from 'pmtiles';
import { layers, namedFlavor } from '@protomaps/basemaps';
import 'maplibre-gl/dist/maplibre-gl.css';

let protocolReady = false;

function ensureProtocol() {
	if (protocolReady) return;
	const protocol = new Protocol();
	addProtocol('pmtiles', protocol.tile);
	protocolReady = true;
}

export function pmtilesProxyHttpUrl(vpcUrl: string, token: string): string {
	const params = new URLSearchParams({
		action: 'pmtiles',
		vpcUrl,
		token,
		v: '2'
	});
	return `${window.location.origin}/api/proxy/geo?${params}`;
}

export function pmtilesProxyUrl(vpcUrl: string, token: string): string {
	return `pmtiles://${pmtilesProxyHttpUrl(vpcUrl, token)}`;
}

export type ComposeMapProgress = {
	phase: 'probe' | 'assets' | 'style' | 'tiles' | 'ready' | 'error';
	percent: number;
	detail: string;
};

export type ComposeMapHandle = {
	map: Map;
	marker: Marker;
	setView: (lat: number, lon: number, zoom?: number) => void;
	destroy: () => void;
	resize: () => void;
};

async function probePmtiles(httpUrl: string, onProgress?: (p: ComposeMapProgress) => void) {
	onProgress?.({ phase: 'probe', percent: 5, detail: '1/4 · Connexion basemap VPC (Range)…' });
	const res = await fetch(httpUrl, {
		headers: { Range: 'bytes=0-16383' },
		cache: 'no-store'
	});
	if (!res.ok && res.status !== 206) {
		const text = await res.text().catch(() => '');
		throw new Error(
			res.status === 404
				? 'Basemap absent — Settings → Sync basemap'
				: `Basemap HTTP ${res.status}${text ? `: ${text.slice(0, 120)}` : ''}`
		);
	}
	const buf = await res.arrayBuffer();
	const magic = new TextDecoder().decode(new Uint8Array(buf).slice(0, 7));
	if (magic !== 'PMTiles') {
		throw new Error('Réponse invalide (pas un fichier PMTiles)');
	}
	const cr = res.headers.get('Content-Range') || '';
	onProgress?.({
		phase: 'probe',
		percent: 18,
		detail: cr ? `1/4 · En-tête OK (${cr})` : '1/4 · En-tête PMTiles OK'
	});
}

async function preloadAssets(origin: string, onProgress?: (p: ComposeMapProgress) => void) {
	onProgress?.({ phase: 'assets', percent: 25, detail: '2/4 · Sprites locaux…' });
	const spriteBase = `${origin}/geo/basemaps-assets/sprites/v4/light`;
	const [jsonRes, pngRes] = await Promise.all([
		fetch(`${spriteBase}.json`, { cache: 'force-cache' }),
		fetch(`${spriteBase}.png`, { cache: 'force-cache' })
	]);
	if (!jsonRes.ok || !pngRes.ok) {
		throw new Error(`Sprites manquants (${jsonRes.status}/${pngRes.status})`);
	}
	onProgress?.({ phase: 'assets', percent: 32, detail: '2/4 · Sprites OK' });
}

export async function createComposeMap(opts: {
	container: HTMLElement;
	vpcUrl: string;
	token: string;
	lat: number;
	lon: number;
	onPick: (lat: number, lon: number) => void;
	onProgress?: (p: ComposeMapProgress) => void;
}): Promise<ComposeMapHandle> {
	ensureProtocol();
	const httpUrl = pmtilesProxyHttpUrl(opts.vpcUrl, opts.token);
	const origin = typeof window !== 'undefined' ? window.location.origin : '';

	await probePmtiles(httpUrl, opts.onProgress);
	await preloadAssets(origin, opts.onProgress);

	opts.onProgress?.({
		phase: 'style',
		percent: 40,
		detail: '3/4 · Init MapLibre + source PMTiles…'
	});

	// lang omitted first paint is faster (fewer glyph fetches); labels still work via default stacks when present
	const style: StyleSpecification = {
		version: 8,
		glyphs: `${origin}/geo/basemaps-assets/fonts/{fontstack}/{range}.pbf`,
		sprite: `${origin}/geo/basemaps-assets/sprites/v4/light`,
		sources: {
			protomaps: {
				type: 'vector',
				url: `pmtiles://${httpUrl}`,
				attribution: '© OpenStreetMap · © Protomaps'
			}
		},
		layers: layers('protomaps', namedFlavor('light'), { lang: 'fr' })
	};

	const map = new Map({
		container: opts.container,
		style,
		center: [opts.lon, opts.lat],
		zoom: 6,
		maxZoom: 10,
		minZoom: 1,
		attributionControl: { compact: true },
		fadeDuration: 0
	});
	map.addControl(new NavigationControl({ showCompass: false }), 'top-left');
	map.getCanvas().style.background = '#e8e8e8';

	let mapProgressHint = 40;

	const el = document.createElement('div');
	el.className = 'sca-ml-pin';
	el.innerHTML = '<span class="sca-ml-pin__dot"></span>';
	const marker = new Marker({ element: el, draggable: true })
		.setLngLat([opts.lon, opts.lat])
		.addTo(map);

	marker.on('dragend', () => {
		const ll = marker.getLngLat();
		opts.onPick(ll.lat, ll.lng);
	});
	map.on('click', (e) => {
		marker.setLngLat(e.lngLat);
		opts.onPick(e.lngLat.lat, e.lngLat.lng);
	});

	map.on('error', (e) => {
		const msg = e?.error?.message || 'Erreur carte';
		opts.onProgress?.({
			phase: 'error',
			percent: Math.max(mapProgressHint, 1),
			detail: `Erreur · ${msg}`
		});
	});

	let pending = 0;
	map.on('dataloading', () => {
		pending += 1;
		mapProgressHint = Math.min(88, 48 + pending);
		opts.onProgress?.({
			phase: 'tiles',
			percent: mapProgressHint,
			detail: `4/4 · Chargement données (${pending} req.)…`
		});
	});
	map.on('data', () => {
		opts.onProgress?.({
			phase: 'tiles',
			percent: Math.min(94, mapProgressHint + 2),
			detail: '4/4 · Tuiles / glyphs reçus…'
		});
	});
	map.on('sourcedata', (e) => {
		if (e.sourceId !== 'protomaps') return;
		opts.onProgress?.({
			phase: 'tiles',
			percent: Math.min(90, mapProgressHint + 5),
			detail: e.isSourceLoaded
				? '4/4 · Source PMTiles chargée'
				: '4/4 · Lecture archive PMTiles…'
		});
	});

	// Don't block the UI forever on idle (Range tiles can stream for a long time).
	await new Promise<void>((resolve) => {
		let settled = false;
		const finish = (detail: string) => {
			if (settled) return;
			settled = true;
			clearTimeout(hardCap);
			clearTimeout(softCap);
			opts.onProgress?.({ phase: 'ready', percent: 100, detail });
			resolve();
		};

		const hardCap = setTimeout(() => {
			finish('Carte affichée (chargement lent — tuiles encore en cours)');
		}, 20_000);

		const softCap = setTimeout(() => {
			if (map.loaded()) finish('Carte prête');
			else {
				opts.onProgress?.({
					phase: 'tiles',
					percent: 70,
					detail: '4/4 · Toujours en cours (sprites/tuiles)…'
				});
				map.resize();
			}
		}, 4_000);

		map.once('load', () => {
			opts.onProgress?.({ phase: 'tiles', percent: 60, detail: '3/4 · Style MapLibre OK — tuiles…' });
			requestAnimationFrame(() => {
				map.resize();
				setTimeout(() => map.resize(), 120);
				setTimeout(() => map.resize(), 400);
			});
			// Unblock overlay soon after style; tiles keep filling in
			setTimeout(() => finish('Carte prête — tuiles en cours…'), 600);
		});

		map.once('idle', () => finish('Carte prête'));
	});

	return {
		map,
		marker,
		setView(lat, lon, zoom) {
			marker.setLngLat([lon, lat]);
			map.easeTo({
				center: [lon, lat],
				zoom: zoom ?? Math.max(map.getZoom(), 7),
				duration: 400
			});
		},
		destroy() {
			marker.remove();
			map.remove();
		},
		resize() {
			map.resize();
		}
	};
}
