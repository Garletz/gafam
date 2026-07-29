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

export function pmtilesProxyUrl(vpcUrl: string, token: string): string {
	const params = new URLSearchParams({
		action: 'pmtiles',
		vpcUrl,
		token
	});
	const abs = `${window.location.origin}/api/proxy/geo?${params}`;
	return `pmtiles://${abs}`;
}

export type ComposeMapHandle = {
	map: Map;
	marker: Marker;
	setView: (lat: number, lon: number, zoom?: number) => void;
	destroy: () => void;
	resize: () => void;
};

export async function createComposeMap(opts: {
	container: HTMLElement;
	vpcUrl: string;
	token: string;
	lat: number;
	lon: number;
	onPick: (lat: number, lon: number) => void;
}): Promise<ComposeMapHandle> {
	ensureProtocol();

	const style: StyleSpecification = {
		version: 8,
		glyphs: '/geo/basemaps-assets/fonts/{fontstack}/{range}.pbf',
		sprite: '/geo/basemaps-assets/sprites/v4/light',
		sources: {
			protomaps: {
				type: 'vector',
				url: pmtilesProxyUrl(opts.vpcUrl, opts.token),
				attribution: '© OpenStreetMap · © Protomaps'
			}
		},
		layers: layers('protomaps', namedFlavor('light'), { lang: 'fr' })
	};

	const map = new Map({
		container: opts.container,
		style,
		center: [opts.lon, opts.lat],
		zoom: 5,
		maxZoom: 10,
		attributionControl: { compact: true }
	});
	map.addControl(new NavigationControl({ showCompass: false }), 'top-left');

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

	await new Promise<void>((resolve) => {
		if (map.loaded()) resolve();
		else map.once('load', () => resolve());
	});

	return {
		map,
		marker,
		setView(lat, lon, zoom) {
			marker.setLngLat([lon, lat]);
			map.easeTo({
				center: [lon, lat],
				zoom: zoom ?? Math.max(map.getZoom(), 6),
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
