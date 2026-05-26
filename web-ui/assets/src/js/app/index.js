import { render } from 'preact';
import av from '../lib/av';

av(async function() {
    const dropzone = this.querySelector('.dropzone');
    if (dropzone) {
        const initDrop = (await import('../lib/drop')).initDrop;
        initDrop(dropzone);
    }
    const progress = this.querySelector('.progress-alert');
    if (progress != null) {
        const initProgressLog = (await import('../lib/progressLog')).initProgressLog;
        initProgressLog(progress);
    }

    const mountEl = this.querySelector('#direct-search-mount');
    if (mountEl && window._searchQuery) {
        const DirectSearchApp = (await import('../lib/discover/components/DirectSearchApp')).DirectSearchApp;
        
        const onStreamClick = async (resource, title, event) => {
            const clickedRow = event ? event.currentTarget : null;
            // Remove any existing inline progress logs first
            const existingAlert = mountEl.querySelector('.progress-alert-direct');
            if (existingAlert) existingAlert.remove();

            // Create progress wrapper element
            const alertEl = document.createElement('form');
            alertEl.className = 'progress-alert progress-alert-block progress-alert-direct mt-2 mb-4 closeable relative z-10 w-full p-5 rounded-2xl border border-w-line/40 bg-w-card/30 backdrop-blur-md animate-[fadeIn_0.2s_ease-out]';
            alertEl.setAttribute('data-async-target', 'main');
            
            const logTarget = document.createElement('div');
            logTarget.className = 'log-target';
            alertEl.appendChild(logTarget);
            
            // Insert right below the clicked row if event is provided
            if (clickedRow && clickedRow.parentNode) {
                clickedRow.parentNode.insertBefore(alertEl, clickedRow.nextSibling);
            } else {
                // Fallback to top of mountEl if no event
                mountEl.insertBefore(alertEl, mountEl.firstChild);
            }
            
            try {
                const formData = new FormData();
                formData.append('resource', resource);
                formData.append('_csrf', window._CSRF);
                
                const response = await fetch('/', {
                    method: 'POST',
                    body: formData,
                    headers: {
                        'Accept': 'application/json',
                        'X-CSRF-TOKEN': window._CSRF,
                    },
                });
                if (!response.ok) throw new Error('POST failed');
                const data = await response.json();
                const logUrl = data.job_log_url;
                if (!logUrl) throw new Error('No job log URL');
                
                alertEl.setAttribute('data-async-progress-log', logUrl);
                const initProgressLog = (await import('../lib/progressLog')).initProgressLog;
                initProgressLog(alertEl);
            } catch (e) {
                alertEl.innerHTML = `<pre class="error">Failed to prepare streaming stream: ${e.message}</pre>`;
            }
        };

        render(<DirectSearchApp query={window._searchQuery} onStreamClick={onStreamClick} />, mountEl);
    }
}, function() {
    const mountEl = this.querySelector('#direct-search-mount');
    if (mountEl) render(null, mountEl);
});

export {}