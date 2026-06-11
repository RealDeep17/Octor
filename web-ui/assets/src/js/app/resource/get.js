import av from '../../lib/av';
av( async function() {
    if (window._ads !== undefined && window._sessionExpired !== true) {
        const renderAd = (await import('../../lib/ads')).default;
        for (const ad of window._ads) {
            renderAd(this, ad);
        }
    }

    // Aspect ratio toggler for detail page (MUST run before any action check/early return)
    const posterContainer = document.getElementById('detail-poster-container');
    const layoutToggle = document.getElementById('detail-layout-toggle');
    if (posterContainer && layoutToggle) {
        const videoType = posterContainer.getAttribute('data-video-type');
        const videoId = posterContainer.getAttribute('data-video-id');

        function applyLayout(layout) {
            const isHorizontal = layout === 'horizontal';
            
            // Toggle classes on container
            if (isHorizontal) {
                posterContainer.classList.remove('aspect-[2/3]', 'w-[100px]', 'sm:w-[140px]');
                posterContainer.classList.add('aspect-[3/2]', 'w-[200px]', 'sm:w-[315px]');
            } else {
                posterContainer.classList.remove('aspect-[3/2]', 'w-[200px]', 'sm:w-[315px]');
                posterContainer.classList.add('aspect-[2/3]', 'w-[100px]', 'sm:w-[140px]');
            }
            
            // Hide/show image elements
            const verticalImg = document.getElementById('detail-poster-vertical');
            const horizontalImg = document.getElementById('detail-poster-horizontal');
            if (verticalImg) {
                verticalImg.classList.toggle('hidden', isHorizontal);
            }
            if (horizontalImg) {
                horizontalImg.classList.toggle('hidden', !isHorizontal);
            }
            
            // Toggle toggle icons
            const vertIcon = layoutToggle.querySelector('.vertical-icon');
            const horizIcon = layoutToggle.querySelector('.horizontal-icon');
            if (vertIcon) {
                vertIcon.classList.toggle('hidden', isHorizontal);
            }
            if (horizIcon) {
                horizIcon.classList.toggle('hidden', !isHorizontal);
            }
        }

        // Apply local storage preference on page load if it exists
        if (videoType && videoId) {
            const hasAuth = posterContainer.getAttribute('data-has-auth') === 'true';
            const dbPref = posterContainer.getAttribute('data-layout-pref');
            if (hasAuth && dbPref) {
                // If user is authenticated, keep localStorage in sync with database preference
                localStorage.setItem('octor-layout-' + videoType + '-' + videoId, dbPref);
            } else {
                // If unauthenticated, apply localStorage preference
                const localPref = localStorage.getItem('octor-layout-' + videoType + '-' + videoId);
                if (localPref) {
                    applyLayout(localPref);
                }
            }
        }

        layoutToggle.addEventListener('click', function(e) {
            e.preventDefault();
            e.stopPropagation();
            
            if (!videoType || !videoId) return;
            
            const isHorizontal = posterContainer.classList.contains('aspect-[3/2]');
            const newLayout = isHorizontal ? 'vertical' : 'horizontal';
            
            applyLayout(newLayout);
            
            // Save to localStorage
            localStorage.setItem('octor-layout-' + videoType + '-' + videoId, newLayout);
            
            // Save to database
            fetch('/library/' + videoType + '/' + videoId + '/layout', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                    'X-CSRF-TOKEN': window._CSRF || ''
                },
                body: JSON.stringify({ layout: newLayout })
            }).catch(err => console.error('Failed to save detail poster layout preference:', err));
        });
    }

    const query = window.location.hash.replace('#', '');
    const urlParams = new URLSearchParams(query);
    const action = urlParams.get('action');
    const modal = urlParams.get('modal');
    const purge = urlParams.get('purge');
    const debug = urlParams.get('debug');
    if (!action) return;
    let form = document.querySelector('form.' + action);
    // "stream" is a shorthand — try stream-video first, then stream-audio
    if (!form && action === 'stream') {
        form = document.querySelector('form.stream-video') || document.querySelector('form.stream-audio');
    }
    if (!form) return;
    if (purge) {
        const purgeInput = document.createElement('input');
        purgeInput.setAttribute('type', 'hidden');
        purgeInput.setAttribute('name', 'purge');
        purgeInput.setAttribute('value', 'true');
        form.appendChild(purgeInput);
    }
    if (debug) {
        const debugInput = document.createElement('input');
        debugInput.setAttribute('type', 'hidden');
        debugInput.setAttribute('name', 'debug');
        debugInput.setAttribute('value', debug);
        form.appendChild(debugInput);
    }
    form.requestSubmit();
    if (modal) {
        window.addEventListener('player_ready', function () {
            if (!modal) return;
            const checkbox = document.getElementById(modal + '-checkbox');
            checkbox.checked = true;
        });
    }
});
