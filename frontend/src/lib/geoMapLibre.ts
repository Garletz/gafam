/**
 * MapLibre + OpenStreetMap Standard raster (max villes / routes / labels).
 * Places search / SQL stay on the VPC via existing compose + geo proxies.
 */
import { Map, Marker, NavigationControl, type StyleSpecification } from 'maplibre-gl';
import 'maplibre-gl/dist/maplibre-gl.css';

/**
 * OSM Standard — densest free general basemap (cities, roads, POIs, labels baked in).
 * Subdomains a/b/c for parallel tile fetch.
 */
const OSM_STYLE: StyleSpecification = {
	version: 8,
	name: 'OpenStreetMap Standard',
	sources: {
		osm: {
			type: 'raster',
			tiles: [
				'https://a.tile.openstreetmap.org/{z}/{x}/{y}.png',
				'https://b.tile.openstreetmap.org/{z}/{x}/{y}.png',
				'https://c.tile.openstreetmap.org/{z}/{x}/{y}.png'
			],
			tileSize: 256,
			attribution: '© <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a>',
			maxzoom: 19
		}
	},
	layers: [
		{
			id: 'osm',
			type: 'raster',
			source: 'osm',
			paint: {
				// keep tiles crisp (no fade wash)
				'raster-fade-duration': 0,
				'raster-opacity': 1,
				'raster-contrast': 0.05,
				'raster-saturation': 0.1
			}
		}
	]
};

export type ComposeMapProgress = {
	phase: 'style' | 'tiles' | 'ready' | 'error';
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

/** OSM Standard paints street names from ~z15; z16–17 is readable for rues. */
const ZOOM_STREETS = 16;

function initialZoom(lat: number, lon: number): number {
	const nearDefault = Math.abs(lat - 46.5) < 0.05 && Math.abs(lon - 2.35) < 0.05;
	return nearDefault ? 6 : ZOOM_STREETS;
}

export async function createComposeMap(opts: {
	container: HTMLElement;
	lat: number;
	lon: number;
	onPick: (lat: number, lon: number) => void;
	onProgress?: (p: ComposeMapProgress) => void;
}): Promise<ComposeMapHandle> {
	opts.onProgress?.({ phase: 'style', percent: 20, detail: 'Chargement OpenStreetMap…' });

	const lat = opts.lat || 46.5;
	const lon = opts.lon || 2.35;

	const map = new Map({
		container: opts.container,
		style: OSM_STYLE,
		center: [lon, lat],
		zoom: initialZoom(lat, lon),
		maxZoom: 19,
		minZoom: 2,
		attributionControl: { compact: true },
		fadeDuration: 0,
		renderWorldCopies: false
	});
	map.addControl(new NavigationControl({ showCompass: false, visualizePitch: false }), 'top-left');

	const pin = document.createElement('div');
	pin.className = 'sca-ml-pin';
	pin.innerHTML = '<span class="sca-ml-pin__dot"></span>';
	const marker = new Marker({ element: pin, draggable: true }).setLngLat([lon, lat]).addTo(map);

	marker.on('dragend', () => {
		const ll = marker.getLngLat();
		opts.onPick(ll.lat, ll.lng);
	});
	map.on('click', (e) => {
		marker.setLngLat(e.lngLat);
		opts.onPick(e.lngLat.lat, e.lngLat.lng);
	});

	map.on('error', (e) => {
		const msg = e?.error?.message || 'erreur MapLibre';
		console.error('[geoMapLibre]', msg, e?.error);
		opts.onProgress?.({ phase: 'error', percent: 50, detail: `Erreur · ${msg}` });
	});

	await new Promise<void>((resolve) => {
		let done = false;
		const finish = (detail: string) => {
			if (done) return;
			done = true;
			clearTimeout(cap);
			opts.onProgress?.({ phase: 'ready', percent: 100, detail });
			resolve();
		};
		const cap = setTimeout(() => finish('Carte affichée'), 8_000);

		map.once('load', () => {
			opts.onProgress?.({ phase: 'tiles', percent: 70, detail: 'Tuiles OSM (villes + routes)…' });
			map.resize();
			setTimeout(() => map.resize(), 120);
			setTimeout(() => finish('Carte prête'), 400);
		});
		map.once('idle', () => finish('Carte prête'));
	});

	return {
		map,
		marker,
		setView(nextLat, nextLon, zoom) {
			marker.setLngLat([nextLon, nextLat]);
			map.easeTo({
				center: [nextLon, nextLat],
				zoom: zoom ?? ZOOM_STREETS,
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
