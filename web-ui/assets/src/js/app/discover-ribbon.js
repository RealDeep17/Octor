import av from '../lib/av';
import { rebindAsync } from '../lib/async';
import { CINEMETA_BASE } from '../lib/discover/client';
import { langPath } from '../lib/i18n';

function esc(s) {
    return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

function renderCard(item) {
    const poster = item.poster || '';
    const name = item.name || 'Unknown';
    const year = item.releaseInfo || item.year || '';
    const type = item.type || 'movie';
    const href = langPath('/discover') + '?id=' + encodeURIComponent(item.id) + '&type=' + encodeURIComponent(type);

    const a = document.createElement('a');
    a.href = href;
    a.setAttribute('data-umami-event', 'discover-ribbon-click');
    a.className = 'shrink-0 w-[140px] group cursor-pointer text-left';
    a.innerHTML =
        '<div class="aspect-[2/3] rounded-xl overflow-hidden border border-o-line group-hover:border-o-primary/30 group-hover:shadow-[0_0_20px_rgba(0,206,201,0.1)] transition-all duration-300">' +
            (poster
                ? '<img src="' + esc(poster) + '" alt="' + esc(name) + '" class="w-full h-full object-cover group-hover:scale-105 transition-transform duration-300" loading="lazy" />'
                : '<div class="w-full h-full bg-gradient-to-br from-o-primary/20 via-o-primary/10 to-o-primary/15 flex items-center justify-center"><div class="text-center font-bold text-sm p-2 text-o-accent/60 line-clamp-3">' + esc(name) + '</div></div>') +
        '</div>' +
        '<p class="mt-2 text-sm font-medium text-o-text truncate group-hover:text-o-primary transition-colors">' + esc(name) + '</p>' +
        (year ? '<p class="text-xs text-o-muted">' + esc(year) + '</p>' : '');

    a.addEventListener('click', (e) => {
        e.preventDefault();
        e.stopPropagation();
        e.stopImmediatePropagation();

        if (window._userId) {
            const event = new CustomEvent('open-discover-modal', {
                detail: {
                    id: item.id,
                    type: type,
                    name: name,
                    poster: poster,
                }
            });
            window.dispatchEvent(event);
        } else {
            window.location.href = langPath('/login') + '?from=discover&return-url=' + encodeURIComponent(langPath('/'));
        }
    });

    return a;
}

av(async function () {
    const moviesContainer = this.querySelector('#discover-ribbon-cards-movies');
    const seriesContainer = this.querySelector('#discover-ribbon-cards-series');
    if (!moviesContainer && !seriesContainer) return;

    try {
        const [moviesRes, seriesRes] = await Promise.all([
            fetch(`${CINEMETA_BASE}/catalog/movie/top.json`),
            fetch(`${CINEMETA_BASE}/catalog/series/top.json`)
        ]);

        if (moviesContainer && moviesRes.ok) {
            const moviesData = await moviesRes.json();
            const movies = moviesData.metas || [];
            const slicedMovies = movies.slice(0, 14);
            if (slicedMovies.length > 0) {
                moviesContainer.innerHTML = '';
                for (const item of slicedMovies) {
                    item.type = 'movie';
                    moviesContainer.appendChild(renderCard(item));
                }
                rebindAsync(moviesContainer);
            }
        }

        if (seriesContainer && seriesRes.ok) {
            const seriesData = await seriesRes.json();
            const series = seriesData.metas || [];
            const slicedSeries = series.slice(0, 14);
            if (slicedSeries.length > 0) {
                seriesContainer.innerHTML = '';
                for (const item of slicedSeries) {
                    item.type = 'series';
                    seriesContainer.appendChild(renderCard(item));
                }
                rebindAsync(seriesContainer);
            }
        }
    } catch (e) {
        // On error keep skeleton — not critical
    }
});

export {};
