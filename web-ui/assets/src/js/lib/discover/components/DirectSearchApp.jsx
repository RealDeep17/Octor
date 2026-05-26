import { useState, useEffect, useMemo, useCallback } from 'preact/hooks';
import { parseStreamName, extractInfoHash, extractFileIdx } from '../stream';
import { extractLanguages, LANG_MAP } from '../lang';
import { loadPrefs, savePrefs } from '../prefs';
import { chipClass } from './discoverUtils';
import { t, tf, langPath } from '../i18n';

function formatBytes(bytes, decimals = 2) {
	if (!bytes) return '0 Bytes';
	const k = 1024;
	const dm = decimals < 0 ? 0 : decimals;
	const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB'];
	const i = Math.floor(Math.log(bytes) / Math.log(k));
	return parseFloat((bytes / Math.pow(k, i)).toFixed(dm)) + ' ' + sizes[i];
}

function formatAge(publishDate) {
	if (!publishDate) return null;
	try {
		const pub = new Date(publishDate);
		const now = new Date();
		const diffMs = now.getTime() - pub.getTime();
		if (diffMs < 0) return null;

		const diffMins = Math.floor(diffMs / (1000 * 60));
		if (diffMins < 60) {
			return `${diffMins}m ago`;
		}

		const diffHours = Math.floor(diffMs / (1000 * 60 * 60));
		if (diffHours < 24) {
			return `${diffHours}h ago`;
		}

		const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24));
		if (diffDays < 30) {
			return `${diffDays}d ago`;
		}

		const diffMonths = Math.floor(diffMs / (1000 * 60 * 60 * 24 * 30.4));
		if (diffMonths < 12) {
			return `${diffMonths}mo ago`;
		}

		const diffYears = Math.floor(diffMs / (1000 * 60 * 60 * 24 * 365));
		return `${diffYears}y ago`;
	} catch (e) {
		return null;
	}
}

const PLAY_ICON = (
	<svg class="w-4 h-4 fill-current" viewBox="0 0 24 24">
		<polygon points="5 3 19 12 5 21 5 3"></polygon>
	</svg>
);

function isSeasonPack(title) {
	const cleanTitle = String(title || '').toLowerCase();
	// 1. Explicit season words/ranges: "season 1", "season 1-3", "seasons 1-5", "complete season", "complete series"
	if (/\b(?:seasons?|temporadas?)\s*\d{1,2}(?:\s*-\s*\d{1,2})?\b/.test(cleanTitle)) return true;
	if (/\bcomplete\s+season\b/.test(cleanTitle)) return true;
	if (/\bcomplete\s+series\b/.test(cleanTitle)) return true;
	if (/\bseason\s*complete\b/.test(cleanTitle)) return true;
	
	// 2. Season range: "s01-s03", "s01-03", "s1-3"
	if (/\bs\d{1,2}\s*-\s*s?\d{1,2}\b/.test(cleanTitle)) return true;

	// 3. Episode ranges: "s01e01-12", "s1e01-e12", "s01e01-s01e12", "s5e1-8"
	if (/\bs\d{1,2}e\d{1,2}\s*-\s*(?:e?\d{1,2})\b/.test(cleanTitle)) return true;
	if (/\bs\d{1,2}e\d{1,2}\s*-\s*\d{1,2}\b/.test(cleanTitle)) return true;
	if (/\bs\d{1,2}e\d{1,2}\s*(?:of|\/)\s*\d{1,2}\b/.test(cleanTitle)) return true;
	if (/\bs\d{1,2}\s*e\d{1,2}\s*-\s*e?\d{1,2}\b/.test(cleanTitle)) return true;
	if (/\bs\d{1,2}\s*e\d{1,2}\s*-\s*\d{1,2}\b/.test(cleanTitle)) return true;
	if (/\b(?:episodes?|eps?)\s*\d{1,2}\s*-\s*\d{1,2}\b/.test(cleanTitle)) return true;

	// 4. "s01" or "s1" alone (without single episode "eXX" marker)
	// E.g. "The Boys S05 2160p" but not "The Boys S05E01 2160p"
	if (/\bs\d{1,2}\b/.test(cleanTitle)) {
		if (!/\bs\d{1,2}\s*e\d{1,2}\b/.test(cleanTitle) && !/\bs\d{1,2}e\d{1,2}\b/.test(cleanTitle)) {
			return true;
		}
	}
	return false;
}

const EXTRA_LABEL_PATTERNS = [
	{ label: '8K', re: /\b(?:4320p|8k)\b/i },
	{ label: '4K', re: /\b(?:2160p|4k|uhd)\b/i },
	{ label: '1080p', re: /\b1080p\b/i },
	{ label: '720p', re: /\b720p\b/i },
	{ label: '480p', re: /\b(?:480p|sd|576p)\b/i },
	{ label: 'Season', re: /\b(?:seasons?|temporadas?)\s*\d{1,2}(?:\s*-\s*\d{1,2})?\b|\bcomplete\s+season\b|\bcomplete\s+series\b|\bseason\s*complete\b|\bs\d{1,2}\s*-\s*s?\d{1,2}\b|\bs\d{1,2}e\d{1,2}\s*-\s*(?:e?\d{1,2})\b|\bs\d{1,2}e\d{1,2}\s*(?:of|\/)\s*\d{1,2}\b|\bs\d{1,2}e\d{1,2}\s*-\s*\d{1,2}\b|\bs\d{1,2}\s*e\d{1,2}\s*-\s*e?\d{1,2}\b|\bs\d{1,2}\s*e\d{1,2}\s*-\s*\d{1,2}\b/i },
	{ label: 'Pack', re: /\b(?:pack|collection|siterip|playlist|discography|anthology|trilogy|quadrilogy|tetralogy|duology)\b|\b(?:[4-9]|\d{2,})\s*(?:videos?|files?)\b/i },
	{ label: 'DV', re: /\b(?:dolby[ .-]?vision|dovi|dv)\b/i },
	{ label: 'HDR10+', re: /\b(?:hdr10\+|hdr10plus)\b/i },
	{ label: 'HDR10', re: /\bhdr10\b/i },
	{ label: 'HDR', re: /\bhdr\b/i },
	{ label: 'REMUX', re: /\bremux\b/i },
	{ label: 'BluRay', re: /\bblu[ ._-]?ray\b|\bbluray\b/i },
	{ label: 'BRRip', re: /\bbrrip\b/i },
	{ label: 'BDRip', re: /\bbdrip\b/i },
	{ label: 'WEB-DL', re: /\bweb[ ._-]?dl\b/i },
	{ label: 'WEBRip', re: /\bwebrip\b/i },
	{ label: 'HDTV', re: /\bhdtv\b/i },
	{ label: 'DVDRip', re: /\bdvdrip\b/i },
	{ label: 'CAM', re: /\b(?:cam|camrip)\b/i },
	{ label: 'TS', re: /\b(?:ts|telesync|tc|telecine)\b/i },
	{ label: 'Multi-Audio', re: /\b(?:multi[ ._-]?(?:audio|lang|language)|multiaudio|multi-audio|multi)\b/i },
	{ label: 'Dual-Audio', re: /\b(?:dual[ ._-]?(?:audio|lang|language)|dualaudio|dual-audio|dual)\b/i },
	{ label: 'Dubbed', re: /\b(?:dubbed|dub)\b/i },
	{ label: 'Subbed', re: /\b(?:subbed|sub)\b/i },
	{ label: 'Multi-Sub', re: /\b(?:multi[ ._-]?(?:sub|subs|subtitle|subtitles)|multisub|multi-sub)\b/i },
	{ label: 'HEVC', re: /\b(?:hevc|h[ ._-]?265|x265)\b/i },
	{ label: 'AVC', re: /\b(?:avc|h[ ._-]?264|x264)\b/i },
	{ label: 'AV1', re: /\bav1\b/i },
	{ label: 'VP9', re: /\bvp9\b/i },
	{ label: '10bit', re: /\b10[ ._-]?bit\b/i },
	{ label: '8bit', re: /\b8[ ._-]?bit\b/i },
	{ label: 'Atmos', re: /\batmos\b/i },
	{ label: 'TrueHD', re: /\btruehd\b/i },
	{ label: 'DTS-HD', re: /\bdts[ ._-]?hd\b/i },
	{ label: 'DTS-X', re: /\bdts[ ._-]?x\b/i },
	{ label: 'DTS', re: /\bdts\b/i },
	{ label: 'DD+', re: /\b(?:ddp|dd\+|eac3|e-ac-3)\b/i },
	{ label: 'AC3', re: /\bac3\b/i },
	{ label: 'AAC', re: /\baac\b/i },
	{ label: 'FLAC', re: /\bflac\b/i },
	{ label: 'OPUS', re: /\bopus\b/i },
	{ label: 'MP3', re: /\bmp3\b/i },
	{ label: 'Stereo', re: /\b(?:stereo|2\.0|2ch)\b/i },
	{ label: '5.1', re: /\b(?:5\.1|6ch)\b/i },
	{ label: '7.1', re: /\b(?:7\.1|8ch)\b/i },
	{ label: '.mkv', re: /\.mkv\b/i },
	{ label: '.mp4', re: /\.mp4\b/i },
	{ label: '.avi', re: /\.avi\b/i },
];

const LABEL_ORDER = [
	'8K', '4K', '1080p', '720p', '480p', 'Season', 'Pack',
	'DV', 'HDR10+', 'HDR10', 'HDR',
	'REMUX', 'BluRay', 'BRRip', 'BDRip', 'WEB-DL', 'WEBRip', 'HDTV', 'DVDRip', 'CAM', 'TS',
	'Multi-Audio', 'Dual-Audio', 'Dubbed', 'Subbed', 'Multi-Sub',
	'HEVC', 'AV1', 'AVC', 'VP9', '10bit', '8bit',
	'Atmos', 'TrueHD', 'DTS-HD', 'DTS-X', 'DTS', 'DD+', 'AC3', 'AAC', 'FLAC', 'OPUS', 'MP3', 'Stereo', '5.1', '7.1',
	'.mkv', '.mp4', '.avi',
];

function canonicalLabel(label) {
	const raw = String(label || '').trim();
	const compact = raw.toLowerCase().replace(/[\s._-]+/g, '');
	if (compact === '4320p' || compact === '8k') return '8K';
	if (compact === '2160p' || compact === '4k' || compact === 'uhd') return '4K';
	if (compact === '1080p') return '1080p';
	if (compact === '720p') return '720p';
	if (compact === '480p' || compact === 'sd' || compact === '576p') return '480p';
	if (compact === 'season') return 'Season';
	if (compact === 'pack') return 'Pack';
	if (compact === 'dolbyvision' || compact === 'dovi' || compact === 'dv') return 'DV';
	if (compact === 'hdr10+' || compact === 'hdr10plus') return 'HDR10+';
	if (compact === 'hdr10') return 'HDR10';
	if (compact === 'hdr') return 'HDR';
	if (compact === 'webdl') return 'WEB-DL';
	if (compact === 'webrip') return 'WEBRip';
	if (compact === 'bluray' || compact === 'blurayrip') return 'BluRay';
	if (compact === 'brrip') return 'BRRip';
	if (compact === 'bdrip') return 'BDRip';
	if (compact === 'hdtv') return 'HDTV';
	if (compact === 'dvdrip') return 'DVDRip';
	if (compact === 'cam' || compact === 'camrip') return 'CAM';
	if (compact === 'ts' || compact === 'telesync' || compact === 'tc' || compact === 'telecine') return 'TS';
	if (compact === 'multiaudio' || compact === 'multi' || compact === 'multi-audio') return 'Multi-Audio';
	if (compact === 'dualaudio' || compact === 'dual' || compact === 'dual-audio') return 'Dual-Audio';
	if (compact === 'dubbed' || compact === 'dub') return 'Dubbed';
	if (compact === 'subbed' || compact === 'sub') return 'Subbed';
	if (compact === 'multisub') return 'Multi-Sub';
	if (compact === 'h265' || compact === 'x265' || compact === 'hevc') return 'HEVC';
	if (compact === 'av1') return 'AV1';
	if (compact === 'h264' || compact === 'x264' || compact === 'avc') return 'AVC';
	if (compact === 'vp9') return 'VP9';
	if (compact === '10bit') return '10bit';
	if (compact === '8bit') return '8bit';
	if (compact === 'atmos') return 'Atmos';
	if (compact === 'truehd') return 'TrueHD';
	if (compact === 'dtshd') return 'DTS-HD';
	if (compact === 'dtsx') return 'DTS-X';
	if (compact === 'dts') return 'DTS';
	if (compact === 'ddp' || compact === 'dd+' || compact === 'eac3' || compact === 'eac') return 'DD+';
	if (compact === 'ac3') return 'AC3';
	if (compact === 'aac') return 'AAC';
	if (compact === 'flac') return 'FLAC';
	if (compact === 'opus') return 'OPUS';
	if (compact === 'mp3') return 'MP3';
	if (compact === 'stereo' || compact === '2.0' || compact === '2ch') return 'Stereo';
	if (compact === '5.1' || compact === '6ch') return '5.1';
	if (compact === '7.1' || compact === '8ch') return '7.1';
	if (compact === 'mkv') return '.mkv';
	if (compact === 'mp4') return '.mp4';
	if (compact === 'avi') return '.avi';
	return raw;
}

function sortLabels(labels) {
	return [...labels].sort((a, b) => {
		const ai = LABEL_ORDER.indexOf(a);
		const bi = LABEL_ORDER.indexOf(b);
		if (ai !== -1 && bi !== -1) return ai - bi;
		if (ai !== -1) return -1;
		if (bi !== -1) return 1;
		return a.localeCompare(b);
	});
}

function enrichStreamInfo(stream) {
	const info = parseStreamName(stream.name);
	const text = `${stream.name || ''}\n${stream.title || ''}`;
	const labels = [];
	const seen = new Set();
	for (const raw of info.labels) {
		const label = canonicalLabel(raw);
		const key = label.toLowerCase();
		if (!seen.has(key)) {
			seen.add(key);
			labels.push(label);
		}
	}
	for (const { label, re } of EXTRA_LABEL_PATTERNS) {
		const key = label.toLowerCase();
		if (!seen.has(key) && re.test(text)) {
			seen.add(key);
			labels.push(label);
		}
	}

	const cleanTitle = text.toLowerCase();
	const isSeason = isSeasonPack(text);
	let isPack = false;

	// Pack matches keywords or files >= 5
	if (/\b(?:pack|collection|siterip|playlist|discography|anthology|trilogy|quadrilogy|tetralogy|duology)\b/i.test(cleanTitle)) {
		isPack = true;
	}
	if (/\b(?:[4-9]|\d{2,})\s*(?:videos?|files?)\b/i.test(cleanTitle)) {
		isPack = true;
	}
	if (stream.files >= 5) {
		isPack = true;
	}
	if (isSeason) {
		isPack = true; // pack is backup of season
	}

	if (isSeason && !seen.has('season')) {
		seen.add('season');
		labels.push('Season');
	}
	if (isPack && !seen.has('pack')) {
		seen.add('pack');
		labels.push('Pack');
	}

	info.labels = sortLabels(labels);
	info.size = stream.size || 0;
	info.seeds = stream.seeds || 0;
	return info;
}

function getLabelGroup(label) {
	const lower = String(label || '').toLowerCase();
	for (const [group, labels] of Object.entries(FILTER_GROUPS)) {
		if (labels.some(l => l.toLowerCase() === lower)) {
			return group;
		}
	}
	return null;
}

function getLabelBadgeClass(label) {
	return 'bg-[#eab308] text-black';
}

function getStreamScore(info) {
	let score = 0;

	// 1. Resolution
	const labels = info.labels.map(l => l.toLowerCase());
	if (labels.includes('8k')) score += 20000;
	else if (labels.includes('4k')) score += 10000;
	else if (labels.includes('1080p')) score += 5000;
	else if (labels.includes('720p')) score += 2000;
	else if (labels.includes('480p')) score += 500;

	// 2. Video Range / HDR
	if (labels.includes('dv')) score += 3000;
	if (labels.includes('hdr10+')) score += 2000;
	else if (labels.includes('hdr10')) score += 1500;
	else if (labels.includes('hdr')) score += 1000;

	// 3. Source / Quality
	if (labels.includes('remux')) score += 6000;
	else if (labels.includes('bluray')) score += 4000;
	else if (labels.includes('web-dl')) score += 3500;
	else if (labels.includes('webrip')) score += 3000;
	else if (labels.includes('hdtv')) score += 1500;
	else if (labels.includes('dvdrip')) score += 1000;
	else if (labels.includes('cam')) score -= 10000; // Demote CAM
	else if (labels.includes('ts')) score -= 8000;   // Demote TS

	// 4. Audio Quality
	if (labels.includes('atmos')) score += 1500;
	else if (labels.includes('truehd')) score += 1200;
	else if (labels.includes('dts-hd')) score += 1000;
	else if (labels.includes('dd+')) score += 800;
	else if (labels.includes('ac3')) score += 500;
	else if (labels.includes('5.1')) score += 400;

	// 5. Seeds weighting (logarithmic-like scaling to avoid seeds dominating everything)
	const seeds = info.seeds || 0;
	if (seeds > 0) {
		score += Math.min(seeds, 100) * 15; // Up to 1500 points
		if (seeds > 100) {
			score += Math.min(seeds - 100, 900) * 2; // Up to 1800 more points for high seeds
		}
	} else {
		score -= 5000; // Heavily penalize 0 seed torrents
	}

	// 6. Size weighting (larger files in same category usually mean higher bitrate/quality)
	// We convert size to GB
	const sizeGB = (info.size || 0) / (1024 * 1024 * 1024);
	if (sizeGB > 0) {
		score += Math.min(sizeGB, 60) * 50; // Up to 3000 points
	}

	return score;
}

const FILTER_GROUPS = {
	resolution: ['8K', '4K', '1080p', '720p', '480p'],
	seasonPack: ['Season', 'Pack'],
	videoRange: ['DV', 'HDR10+', 'HDR10', 'HDR'],
	sourceRelease: ['REMUX', 'BluRay', 'BRRip', 'BDRip', 'WEB-DL', 'WEBRip', 'HDTV', 'DVDRip', 'CAM', 'TS'],
	audioSubtitle: ['Multi-Audio', 'Dual-Audio', 'Dubbed', 'Subbed', 'Multi-Sub'],
	videoCodecs: ['HEVC', 'AV1', 'AVC', '10bit'],
	audioCodecs: ['Atmos', 'DD+', 'AC3', 'AAC', 'Stereo', '5.1'],
	containers: ['.mkv', '.mp4', '.avi'],
};

function StreamRow({ stream, info, onStreamClick }) {
	const sizeStr = formatBytes(info.size);
	const seeds = info.seeds || 0;
	const indexers = stream.indexers || [stream.indexer || info.source || 'Torrent'];
	const indexerStr = indexers.join(', ');
	const ageStr = formatAge(stream.publishDate);
	const [preparing, setPreparing] = useState(false);

	const handleClick = useCallback(async (e) => {
		if (preparing) return;
		setPreparing(true);
		try {
			await onStreamClick(stream.magnetUrl || stream.infoHash || stream.downloadUrl, stream.name, e);
		} catch (err) {
			// handled upstream
		} finally {
			setPreparing(false);
		}
	}, [onStreamClick, stream, preparing]);

	const langs = extractLanguages(stream.name);

	return (
		<div
			onClick={handleClick}
			class="flex items-center gap-3 p-3.5 rounded-xl border border-w-line bg-w-card/30 hover:border-w-cyan/40 hover:bg-w-surface/50 transition-all cursor-pointer relative"
		>
			<div class="flex-shrink-0 w-9 h-9 rounded-full bg-w-cyan/10 flex items-center justify-center text-w-cyan">
				{preparing ? (
					<span class="loading loading-spinner loading-xs text-w-cyan"></span>
				) : (
					PLAY_ICON
				)}
			</div>
			<div class="min-w-0 flex-1">
				{/* Line 1: Release Title */}
				<div class="text-sm font-semibold text-w-text mb-1.5 leading-snug whitespace-normal break-words" title={stream.name}>
					{stream.name}
				</div>
				{/* Line 2: Tags + Size + Indexer + Peers */}
				<div class="flex items-center gap-2 flex-wrap text-xs text-w-sub">
					{info.labels.map(label => (
						<span key={label} class={`text-[10px] px-1.5 py-0.5 rounded font-semibold uppercase tracking-wider ${getLabelBadgeClass(label)}`}>
							{label}
						</span>
					))}
					{info.labels.length > 0 && <span class="text-w-sub/40 text-[10px] select-none">|</span>}
					<span class="text-[10px] px-1.5 py-0.5 rounded font-semibold uppercase tracking-wider bg-[#374151] text-[#f3f4f6] flex items-center gap-1 font-mono">
						👤 {seeds}
					</span>
					<span class="text-w-sub/40 text-[10px] select-none">|</span>
					<span class="text-[10px] px-1.5 py-0.5 rounded font-semibold uppercase tracking-wider bg-[#374151] text-[#f3f4f6] flex items-center gap-1 font-mono">
						💾 {sizeStr}
					</span>
					<span class="text-w-sub/40 text-[10px] select-none">|</span>
					<span class="text-[10px] px-1.5 py-0.5 rounded font-semibold uppercase tracking-wider bg-[#374151] text-[#f3f4f6] flex items-center gap-1 font-mono">
						⚙️ {indexerStr}
					</span>
					{ageStr && (
						<>
							<span class="text-w-sub/40 text-[10px] select-none">|</span>
							<span class="text-[10px] px-1.5 py-0.5 rounded font-semibold uppercase tracking-wider bg-[#374151] text-[#f3f4f6] flex items-center gap-1 font-mono">
								📅 {ageStr}
							</span>
						</>
					)}
					{langs.length > 0 && (
						<>
							<span class="text-w-sub/40 text-[10px] select-none">|</span>
							<span class="text-[10px] px-1.5 py-0.5 rounded font-semibold uppercase tracking-wider bg-[#374151] text-[#f3f4f6] flex items-center gap-1">
								{langs.map((lang, idx) => (
									<span key={lang.name} class="inline-flex items-center">
										{idx > 0 && <span class="text-w-muted/30 mx-0.5">/</span>}
										<span title={lang.name}>{lang.flag}</span>
									</span>
								))}
							</span>
						</>
					)}
				</div>
			</div>
		</div>
	);
}

const LOADING_PHRASES = [
	"Bribing the seeders for more bandwidth...",
	"Maxing out your ISP's patience...",
	"Negotiating with public indexers...",
	"Avoiding the copyright trolls...",
	"Feeding the DHT seeders...",
	"Reticulating torrent splines...",
	"Summoning peers from the deep web...",
	"Scanning the swarm for high-bitrate gems...",
	"Spinning up the magnetizer coils...",
	"Fishing for high-quality remuxes...",
	"Tuning DHT antennas for maximum signal...",
	"Convincing leechers to upload for once...",
	"Evading detection by the broadband police...",
	"Downloading more RAM to handle the swarm...",
	"Consulting the ancient oracle of public trackers...",
	"Decrypting secret seeder handshakes...",
	"Greasing the wheels of the P2P network...",
	"Checking under the DHT cushions for extra seeders...",
	"Asking the tracker nicely for some peers...",
	"Bypassing the local ISP throttle gates..."
];

export function DirectSearchApp({ query, onStreamClick }) {
	const [searchQuery, setSearchQuery] = useState(query);
	const [results, setResults] = useState([]);
	const [loading, setLoading] = useState(!!query);
	const [loadingText, setLoadingText] = useState(LOADING_PHRASES[Math.floor(Math.random() * LOADING_PHRASES.length)]);

	useEffect(() => {
		if (!loading) return;
		const interval = setInterval(() => {
			setLoadingText(prev => {
				const candidates = LOADING_PHRASES.filter(p => p !== prev);
				return candidates[Math.floor(Math.random() * candidates.length)];
			});
		}, 5000);
		return () => clearInterval(interval);
	}, [loading]);
	const [error, setError] = useState(null);
	const [showFilters, setShowFilters] = useState(false);
	const [sortBy, setSortBy] = useState('default'); // 'default' | 'seeds' | 'size' | 'date'
	const [currentPage, setCurrentPage] = useState(1);

	const selectSortBy = useCallback((mode) => {
		setSortBy(mode);
		if (document.activeElement) {
			document.activeElement.blur();
		}
	}, []);

	// Active filter states
	const [activeLabels, setActiveLabels] = useState({});
	const [activeSources, setActiveSources] = useState({});
	const [activeLang, setActiveLang] = useState(null);

	const performSearch = useCallback(async (q) => {
		if (!q.trim()) return;
		setLoadingText(LOADING_PHRASES[Math.floor(Math.random() * LOADING_PHRASES.length)]);
		setLoading(true);
		setError(null);
		try {
			const res = await fetch(langPath(`/discover/search`) + `?q=${encodeURIComponent(q)}`);
			if (!res.ok) throw new Error('Failed to fetch search results');
			const data = await res.json();
			if (!Array.isArray(data)) {
				throw new Error('Search API returned an invalid response format');
			}
			
			// Map results into standard stream schema and deduplicate client-side with multi-indexer aggregation
			const deduped = [];
			const seen = new Set();
			for (const item of data) {
				const hash = (item.infoHash || '').toLowerCase();
				const key = hash ? hash : item.title.toLowerCase().replace(/[^a-z0-9]/g, '') + '_' + item.size;
				if (seen.has(key)) {
					const existing = deduped.find(d => d.key === key);
					if (existing && item.indexer && !existing.indexers.includes(item.indexer)) {
						existing.indexers.push(item.indexer);
						// Keep the one with more seeds/peers for play quality
						if (item.seeders > existing.seeds) {
							existing.seeds = item.seeders;
							existing.leechers = item.leechers;
						}
					}
					continue;
				}
				seen.add(key);
				deduped.push({
					key,
					name: item.title,
					title: `👤 ${item.seeders} 💾 ${item.size} ⚙️ ${item.indexer}`,
					size: item.size,
					seeds: item.seeders,
					leechers: item.leechers,
					indexer: item.indexer,
					indexers: [item.indexer || 'Torrent'],
					infoHash: item.infoHash,
					magnetUrl: item.magnetUrl,
					downloadUrl: item.downloadUrl,
					publishDate: item.publishDate,
					files: item.files,
				});
			}
			setResults(deduped);
		} catch (err) {
			setError(err.message);
			setResults([]);
		} finally {
			setLoading(false);
		}
	}, []);

	useEffect(() => {
		performSearch(query);
	}, [query, performSearch]);

	const handleSearchSubmit = useCallback((e) => {
		e.preventDefault();
		const newQ = e.target.elements.q.value;
		setSearchQuery(newQ);
		// Update URL without full reload
		const url = new URL(window.location);
		url.searchParams.set('q', newQ);
		window.history.pushState({}, '', url);
		performSearch(newQ);
	}, [performSearch]);

	const enriched = useMemo(() => results.map(s => enrichStreamInfo(s)), [results]);

	const { allLabels, allLanguages } = useMemo(() => {
		const labels = new Set();
		const langs = new Set();
		for (const s of results) {
			const text = `${s.name || ''}\n${s.title || ''}`;
			extractLanguages(text).forEach(l => langs.add(l.name));
		}
		for (const info of enriched) {
			info.labels.forEach(l => labels.add(l));
		}
		return {
			allLabels: sortLabels([...labels]),
			allLanguages: [...langs].sort(),
		};
	}, [results, enriched]);

	const toggleLabel = useCallback((lbl) => {
		setActiveLabels(prev => {
			const next = { ...prev };
			if (next[lbl]) delete next[lbl];
			else next[lbl] = true;
			return next;
		});
	}, []);

	const toggleLang = useCallback((langName) => {
		setActiveLang(prev => (prev === langName ? null : langName));
	}, []);

	useEffect(() => {
		setCurrentPage(1);
	}, [searchQuery, activeLabels, activeLang, sortBy]);

	const filtered = useMemo(() => {
		const activeLblKeys = Object.keys(activeLabels).filter(k => activeLabels[k]);
		const activeGroups = {};
		if (activeLang) {
			activeGroups['languages'] = [activeLang];
		}
		for (const lbl of activeLblKeys) {
			const group = getLabelGroup(lbl);
			if (group) {
				if (!activeGroups[group]) {
					activeGroups[group] = [];
				}
				activeGroups[group].push(lbl.toLowerCase());
			}
		}

		return results.map((s, i) => {
			let show = true;
			for (const [groupName, activeFilters] of Object.entries(activeGroups)) {
				if (groupName === 'languages') {
					const text = `${s.name || ''}\n${s.title || ''}`;
					const streamLangs = extractLanguages(text).map(l => l.name);
					if (!streamLangs.includes(activeLang)) {
						show = false;
						break;
					}
				} else {
					const streamLabelsLower = enriched[i].labels.map(l => l.toLowerCase());
					const matchesAny = activeFilters.some(filterLower => streamLabelsLower.includes(filterLower));
					if (!matchesAny) {
						show = false;
						break;
					}
				}
			}
			return { stream: s, enriched: enriched[i], visible: show };
		});
	}, [results, enriched, activeLabels, activeLang]);

	const sortedFiltered = useMemo(() => {
		const items = filtered.filter(item => item.visible);
		if (sortBy === 'seeds') {
			items.sort((a, b) => {
				const diff = (b.enriched.seeds || 0) - (a.enriched.seeds || 0);
				if (diff !== 0) return diff;
				return (b.enriched.size || 0) - (a.enriched.size || 0);
			});
		} else if (sortBy === 'size') {
			items.sort((a, b) => {
				const diff = (b.enriched.size || 0) - (a.enriched.size || 0);
				if (diff !== 0) return diff;
				return (b.enriched.seeds || 0) - (a.enriched.seeds || 0);
			});
		} else if (sortBy === 'date') {
			items.sort((a, b) => {
				const da = a.stream.publishDate ? new Date(a.stream.publishDate).getTime() : 0;
				const db = b.stream.publishDate ? new Date(b.stream.publishDate).getTime() : 0;
				const timeA = isNaN(da) ? 0 : da;
				const timeB = isNaN(db) ? 0 : db;
				if (timeB !== timeA) return timeB - timeA;
				return (b.enriched.seeds || 0) - (a.enriched.seeds || 0);
			});
		} else {
			// 'default' - Use Supreme Sorter score!
			items.sort((a, b) => {
				return getStreamScore(b.enriched) - getStreamScore(a.enriched);
			});
		}
		return items;
	}, [filtered, sortBy]);

	const getSortLabel = useCallback((mode) => {
		if (mode === 'seeds') return 'Seeds';
		if (mode === 'size') return 'Size';
		if (mode === 'date') return 'Date/Recent';
		return 'Default';
	}, []);

	const resultsPerPage = 50;
	const totalPages = Math.ceil(sortedFiltered.length / resultsPerPage);

	const paginatedFiltered = useMemo(() => {
		const start = (currentPage - 1) * resultsPerPage;
		return sortedFiltered.slice(start, start + resultsPerPage);
	}, [sortedFiltered, currentPage]);

	return (
		<div class="w-full max-w-4xl mx-auto px-4 py-8 text-left">
			{/* Top Search Bar */}
			<form onSubmit={handleSearchSubmit} class="join w-full mb-8 flex">
				<div class="join-item flex items-center search-container rounded-l-[14px] flex-1 h-12 bg-w-card/40 border border-w-line/40 transition-all px-4">
					<svg class="shrink-0 text-o-muted mr-3" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="8"/><path d="M21 21l-4.35-4.35"/></svg>
					<input
						type="text"
						name="q"
						value={searchQuery}
						onInput={(e) => setSearchQuery(e.target.value)}
						placeholder="Search direct indexers..."
						class="input bg-transparent border-0 focus:outline-none flex-1 text-base placeholder:text-o-muted font-sans text-w-text h-full"
						required
					/>
				</div>
				<button type="submit" class="btn join-item btn-pink px-7 h-12 rounded-r-[14px] uppercase tracking-wide font-medium">Search</button>
			</form>

			{/* Sorting & Filter Header */}
			<div class="flex flex-wrap items-center justify-between gap-3 mb-6">
				<div class="flex items-center gap-2">
					<div class="dropdown dropdown-bottom">
						<button
							tabindex="0"
							class="btn btn-sm bg-w-card/40 border border-w-line text-w-sub hover:border-w-cyan/40 hover:text-w-text font-medium tracking-wide uppercase px-4 py-2 h-auto flex items-center gap-1.5"
						>
							<svg class="w-3.5 h-3.5 shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
								<line x1="12" y1="5" x2="12" y2="19"></line>
								<polyline points="19 12 12 19 5 12"></polyline>
							</svg>
							Sort: {getSortLabel(sortBy)}
							<svg class="w-3 h-3 transition-transform duration-200" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M19 9l-7 7-7-7"/></svg>
						</button>
						<ul tabindex="0" class="dropdown-content menu bg-base-200 border border-w-line rounded-box z-toast w-40 p-1.5 shadow-xl mt-1">
							<li>
								<button onClick={() => selectSortBy('default')} class={`text-xs px-3 py-2 rounded-md ${sortBy === 'default' ? 'bg-w-cyan/15 text-w-cyan font-semibold' : 'text-w-sub hover:text-w-text'}`}>
									Default
								</button>
							</li>
							<li>
								<button onClick={() => selectSortBy('seeds')} class={`text-xs px-3 py-2 rounded-md ${sortBy === 'seeds' ? 'bg-w-cyan/15 text-w-cyan font-semibold' : 'text-w-sub hover:text-w-text'}`}>
									Seeds
								</button>
							</li>
							<li>
								<button onClick={() => selectSortBy('size')} class={`text-xs px-3 py-2 rounded-md ${sortBy === 'size' ? 'bg-w-cyan/15 text-w-cyan font-semibold' : 'text-w-sub hover:text-w-text'}`}>
									Size
								</button>
							</li>
							<li>
								<button onClick={() => selectSortBy('date')} class={`text-xs px-3 py-2 rounded-md ${sortBy === 'date' ? 'bg-w-cyan/15 text-w-cyan font-semibold' : 'text-w-sub hover:text-w-text'}`}>
									Date/Recent
								</button>
							</li>
						</ul>
					</div>

					{/* Quick Resolution & High-Value Pills */}
					{['4K', '1080p', '720p', '480p', 'Season', 'Pack'].map(res => {
						if (!allLabels.some(l => l.toLowerCase() === res.toLowerCase())) return null;
						const active = !!activeLabels[res];
						return (
							<button
								key={res}
								onClick={() => toggleLabel(res)}
								class={chipClass(active)}
							>
								{res}
							</button>
						);
					})}
				</div>

				<button
					onClick={() => setShowFilters(prev => !prev)}
					class={`btn btn-sm flex items-center gap-1.5 uppercase font-medium tracking-wide px-4 py-2 h-auto ${chipClass(showFilters)}`}
				>
					<svg class="w-3.5 h-3.5 shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
						<line x1="4" y1="21" x2="4" y2="14"></line>
						<line x1="4" y1="10" x2="4" y2="3"></line>
						<line x1="12" y1="21" x2="12" y2="12"></line>
						<line x1="12" y1="8" x2="12" y2="3"></line>
						<line x1="20" y1="21" x2="20" y2="16"></line>
						<line x1="20" y1="12" x2="20" y2="3"></line>
						<line x1="1" y1="14" x2="7" y2="14"></line>
						<line x1="9" y1="8" x2="15" y2="8"></line>
						<line x1="17" y1="16" x2="23" y2="16"></line>
					</svg>
					Filters
					<svg class={`w-3 h-3 transition-transform duration-200 ${showFilters ? 'rotate-180' : ''}`} viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M19 9l-7 7-7-7"/></svg>
				</button>
			</div>

			{/* Advanced Supreme Filters Drawer */}
			{showFilters && (
				<div class="bg-w-card/30 rounded-2xl border border-w-line/40 p-5 mb-8 backdrop-blur-md flex flex-col gap-4 animate-[fadeIn_0.2s_ease-out]">
					{allLanguages.length > 0 && (
						<div>
							<h4 class="text-xs font-semibold text-w-muted uppercase tracking-widest mb-2">Languages</h4>
							<div class="flex flex-wrap gap-1.5">
								{allLanguages.map(lang => {
									const langObj = LANG_MAP[lang.toLowerCase()];
									const flag = langObj ? langObj.flag : '';
									return (
										<button
											key={lang}
											onClick={() => toggleLang(lang)}
											class={chipClass(activeLang === lang)}
										>
											{flag ? flag + ' ' : ''}{lang}
										</button>
									);
								})}
							</div>
						</div>
					)}

					{Object.entries(FILTER_GROUPS).map(([groupKey, labels]) => {
						// Don't render resolution group since it is already on the quick pills
						if (groupKey === 'resolution') return null;
						const available = labels.filter(l => allLabels.some(al => al.toLowerCase() === l.toLowerCase()));
						if (available.length === 0) return null;

						const titles = {
							videoRange: 'Video Range',
							sourceRelease: 'Source / Release',
							audioSubtitle: 'Audio / Subtitle',
							videoCodecs: 'Video Codecs',
							audioCodecs: 'Audio Codecs',
							containers: 'Containers',
						};

						return (
							<div key={groupKey}>
								<h4 class="text-xs font-semibold text-w-muted uppercase tracking-widest mb-2">{titles[groupKey] || groupKey}</h4>
								<div class="flex flex-wrap gap-1.5">
									{available.map(lbl => {
										const active = !!activeLabels[lbl];
										return (
											<button
												key={lbl}
												onClick={() => toggleLabel(lbl)}
												class={chipClass(active)}
											>
												{lbl}
											</button>
										);
									})}
								</div>
							</div>
						);
					})}
				</div>
			)}

			{/* Results Surface */}
			{loading && (
				<div class="text-center py-20">
					<span class="loading loading-spinner loading-lg text-w-cyan"></span>
					<p class="text-w-sub mt-4 font-medium">{loadingText}</p>
				</div>
			)}

			{!loading && error && (
				<div class="alert alert-error max-w-xl mx-auto rounded-xl border border-red-500/20 bg-red-500/5 text-red-400 p-4">
					<svg xmlns="http://www.w3.org/2000/svg" class="stroke-current shrink-0 h-6 w-6" fill="none" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10 14l2-2m0 0l2-2m-2 2l-2-2m2 2l2 2m7-2a9 9 0 11-18 0 9 9 0 0118 0z" /></svg>
					<span>{error}</span>
				</div>
			)}

			{!loading && !error && sortedFiltered.length === 0 && (
				<div class="text-center py-16 bg-w-card/10 rounded-2xl border border-w-line/20 backdrop-blur-md">
					<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="w-16 h-16 text-w-muted/30 mx-auto mb-4">
						<path stroke-linecap="round" stroke-linejoin="round" d="m9.75 9.75 4.5 4.5m0-4.5-4.5 4.5M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z" />
					</svg>
					<p class="text-lg font-semibold text-w-sub mb-1">No indexer releases match your query</p>
					<p class="text-sm text-w-muted">Try removing active filters or adjusting the search keywords.</p>
				</div>
			)}

			{!loading && !error && paginatedFiltered.length > 0 && (
				<>
					<div class="flex flex-col gap-3">
						{paginatedFiltered.map((item, idx) => (
							<StreamRow
								key={item.stream.guid || item.stream.infoHash || idx}
								stream={item.stream}
								info={item.enriched}
								onStreamClick={onStreamClick}
							/>
						))}
					</div>

					{totalPages > 1 && (
						<div class="flex justify-center items-center gap-2 mt-8">
							<div class="join border border-w-line bg-w-card/20 rounded-xl overflow-hidden">
								<button
									onClick={() => setCurrentPage(prev => Math.max(prev - 1, 1))}
									disabled={currentPage === 1}
									class="join-item btn btn-sm bg-transparent border-0 text-w-sub hover:bg-w-card/60 disabled:bg-transparent disabled:text-w-muted/30 px-3 h-9"
								>
									<svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
										<polyline points="15 18 9 12 15 6"></polyline>
									</svg>
								</button>

								{Array.from({ length: totalPages }, (_, i) => i + 1).map(page => {
									const isNear = Math.abs(page - currentPage) <= 1;
									const isFirstOrLast = page === 1 || page === totalPages;
									
									if (!isNear && !isFirstOrLast) {
										if (page === 2 || page === totalPages - 1) {
											return <span key={page} class="join-item flex items-center justify-center w-8 h-9 text-xs text-w-muted select-none">...</span>;
										}
										return null;
									}

									const active = page === currentPage;
									return (
										<button
											key={page}
											onClick={() => setCurrentPage(page)}
											class={`join-item btn btn-sm border-0 w-9 h-9 text-xs font-semibold ${
												active
													? 'bg-w-cyan text-black hover:bg-w-cyanL font-bold'
													: 'bg-transparent text-w-sub hover:bg-w-card/60'
											}`}
										>
											{page}
										</button>
									);
								})}

								<button
									onClick={() => setCurrentPage(prev => Math.min(prev + 1, totalPages))}
									disabled={currentPage === totalPages}
									class="join-item btn btn-sm bg-transparent border-0 text-w-sub hover:bg-w-card/60 disabled:bg-transparent disabled:text-w-muted/30 px-3 h-9"
								>
									<svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
										<polyline points="9 18 15 12 9 6"></polyline>
									</svg>
								</button>
							</div>
						</div>
					)}
				</>
			)}
		</div>
	);
}
