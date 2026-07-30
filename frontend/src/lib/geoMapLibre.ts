/**
 * MapLibre + OpenFreeMap public CDN style (no PMTiles, no VPC tiles).
 * Places search / SQL stay on the VPC via existing compose + geo proxies.
 */
import { Map, Marker, NavigationControl } from 'maplibre-gl';
import 'maplibre-gl/dist/maplibre-gl.css';

const OPENFREEMAP_STYLE = 'https://tiles.openfreemap.org/styles/liberty';

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

export async function createComposeMap(opts: {
	container: HTMLElement;
	lat: number;
	lon: number;
	onPick: (lat: number, lon: number) => void;
	onProgress?: (p: ComposeMapProgress) => void;
}): Promise<ComposeMapHandle> {
	opts.onProgress?.({ phase: 'style', percent: 20, detail: 'Chargement style OpenFreeMap…' });

	const map = new Map({
		container: opts.container,
		style: OPENFREEMAP_STYLE,
		center: [opts.lon || 2.35, opts.lat || 46.5],
		zoom: 6,
		attributionControl: { compact: true },
		fadeDuration: 0
	});
	map.addControl(new NavigationControl({ showCompass: false }), 'top-left');

	const pin = document.createElement('div');
	pin.className = 'sca-ml-pin';
	pin.innerHTML = '<span class="sca-ml-pin__dot"></span>';
	const marker = new Marker({ element: pin, draggable: true })
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
			opts.onProgress?.({ phase: 'tiles', percent: 70, detail: 'Style OK — tuiles…' });
			map.resize();
			setTimeout(() => map.resize(), 120);
			setTimeout(() => finish('Carte prête'), 400);
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
				zoom: zoom ?? Math.max(map.getZoom(), 12),
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
