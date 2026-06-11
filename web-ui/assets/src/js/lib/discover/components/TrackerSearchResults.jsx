import { useCallback, useState } from 'preact/hooks';

function formatBytes(bytes, decimals = 2) {
	if (!bytes) return '0 Bytes';
	const k = 1024;
	const dm = decimals < 0 ? 0 : decimals;
	const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB'];
	const i = Math.floor(Math.log(bytes) / Math.log(k));
	return parseFloat((bytes / Math.pow(k, i)).toFixed(dm)) + ' ' + sizes[i];
}

export function TrackerSearchResults({ results, onStreamClick, loading }) {
	const [downloadingHash, setDownloadingHash] = useState(null);

	const handleDownload = useCallback(async (item) => {
		if (downloadingHash) return;
		setDownloadingHash(item.infoHash);
		try {
			await onStreamClick(item.magnetUrl || item.downloadUrl || item.infoHash, null, item.title, null);
		} catch (e) {
			// error handled upstream
		} finally {
			setDownloadingHash(null);
		}
	}, [onStreamClick, downloadingHash]);

	if (loading) {
		return (
			<div class="text-center py-16">
				<span class="loading loading-spinner loading-lg text-w-cyan"></span>
				<p class="text-w-sub mt-4">Searching indexers via Prowlarr...</p>
			</div>
		);
	}

	if (!results || results.length === 0) {
		return (
			<div class="text-center py-16">
				<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="w-16 h-16 text-w-muted/40 mx-auto mb-4">
					<path stroke-linecap="round" stroke-linejoin="round" d="m9.75 9.75 4.5 4.5m0-4.5-4.5 4.5M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z" />
				</svg>
				<p class="text-lg font-semibold text-w-sub mb-2">No torrent releases found</p>
				<p class="text-sm text-w-muted">Try checking indexers or search with different keywords.</p>
			</div>
		);
	}

	return (
		<div class="overflow-x-auto bg-base-300/40 rounded-xl border border-w-line/40 backdrop-blur-md mt-6">
			<table class="table w-full text-w-text">
				<thead>
					<tr class="border-b border-w-line/40 text-w-muted text-xs uppercase tracking-wider">
						<th class="py-3.5 pl-4 text-left">Release Title</th>
						<th class="py-3.5 px-3 text-left">Indexer</th>
						<th class="py-3.5 px-3 text-right">Size</th>
						<th class="py-3.5 px-3 text-center">Peers</th>
						<th class="py-3.5 pr-4 text-center">Action</th>
					</tr>
				</thead>
				<tbody class="divide-y divide-w-line/20">
					{results.map((item) => (
						<tr key={item.guid || item.infoHash} class="hover:bg-w-surface/30 transition-colors">
							<td class="py-4 pl-4 font-medium text-sm max-w-xs md:max-w-md lg:max-w-xl truncate" title={item.title}>
								{item.title}
							</td>
							<td class="py-4 px-3 text-sm text-w-sub">
								<span class="badge badge-sm bg-base-200 border-w-line text-w-sub font-mono">{item.indexer}</span>
							</td>
							<td class="py-4 px-3 text-sm text-right text-w-sub whitespace-nowrap">
								{formatBytes(item.size)}
							</td>
							<td class="py-4 px-3 text-sm text-center whitespace-nowrap">
								<span class="text-emerald-500 font-semibold">{item.seeders}</span>
								<span class="text-w-muted mx-1">/</span>
								<span class="text-w-muted">{item.leechers}</span>
							</td>
							<td class="py-4 pr-4 text-center whitespace-nowrap">
								<button
									class="btn btn-xs btn-outline hover:btn-solid border-w-line text-w-cyan hover:bg-w-cyan hover:text-base-300 font-medium tracking-wide uppercase px-3 py-1.5 h-auto"
									onClick={() => handleDownload(item)}
									disabled={downloadingHash != null}
								>
									{downloadingHash === item.infoHash ? (
										<span class="loading loading-spinner loading-xs"></span>
									) : (
										'Stream'
									)}
								</button>
							</td>
						</tr>
					))}
				</tbody>
			</table>
		</div>
	);
}
