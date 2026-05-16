import av from '../lib/av';
av(async function() {
    const self = this;
    const progress = self.querySelector('.progress-alert');
    const el = document.createElement('div');
    const initProgressLog = (await import('../lib/progressLog')).initProgressLog;
    initProgressLog(progress, function(ev) {
        if (ev.level !== 'rendertemplate') return;
        el.classList.add('mb-5');
        self.appendChild(el);
        ev.render(el);
        el.classList.add('hidden');

        const showPlayer = () => {
            progress.classList.add('hidden');
            el.classList.remove('hidden');
        };
        const showPlayerError = (event) => {
            const message = event?.detail?.message || 'Player failed to initialize. Please try again.';
            this.sdk.error('player initialization', message);
            el.classList.remove('hidden');
        };

        window.addEventListener('player_ready', showPlayer, {once: true});
        window.addEventListener('player_error', showPlayerError, {once: true});
    });
});

export {}
