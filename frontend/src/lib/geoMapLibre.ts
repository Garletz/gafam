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
	phase: 'probe' | 'style' | 'tiles' | 'ready' | 'error';
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

/** Warm the PMTiles header via Range so MapLibre doesn't start cold. */
async function probePmtiles(httpUrl: string, onProgress?: (p: ComposeMapProgress) => void) {
	onProgress?.({ phase: 'probe', percent: 8, detail: 'Connexion basemap VPC…' });
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
	onProgress?.({ phase: 'probe', percent: 22, detail: 'En-tête PMTiles OK' });
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
	await probePmtiles(httpUrl, opts.onProgress);

	opts.onProgress?.({ phase: 'style', percent: 35, detail: 'Chargement du style MapLibre…' });

	const origin = typeof window !== 'undefined' ? window.location.origin : '';
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

	// Visible canvas while vector tiles stream in
	map.getCanvas().style.background = '#d4e3ef';

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
		opts.onProgress?.({ phase: 'error', percent: 0, detail: msg });
	});

	let tileHits = 0;
	map.on('sourcedata', (e) => {
		if (e.sourceId !== 'protomaps' || !e.isSourceLoaded) return;
		tileHits += 1;
		const pct = Math.min(92, 45 + tileHits * 8);
		opts.onProgress?.({
			phase: 'tiles',
			percent: pct,
			detail: 'Téléchargement des tuiles vectorielles…'
		});
	});

	await new Promise<void>((resolve, reject) => {
		let settled = false;
		const done = () => {
			if (settled) return;
			settled = true;
			clearTimeout(t);
			clearTimeout(fallback);
			opts.onProgress?.({ phase: 'ready', percent: 100, detail: 'Carte prête' });
			resolve();
		};
		const t = setTimeout(() => {
			if (!settled) {
				settled = true;
				reject(new Error('Timeout chargement carte (45s)'));
			}
		}, 45_000);
		// If idle is slow (many Range round-trips), still show the map after style load
		const fallback = setTimeout(() => {
			if (map.loaded()) done();
		}, 8_000);
		map.once('load', () => {
			opts.onProgress?.({ phase: 'tiles', percent: 55, detail: 'Style prêt — tuiles…' });
			requestAnimationFrame(() => {
				map.resize();
				setTimeout(() => map.resize(), 100);
				setTimeout(() => map.resize(), 400);
			});
		});
		map.once('idle', done);
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
