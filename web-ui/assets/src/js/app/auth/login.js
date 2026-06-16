import { init as initI18n, t, tf } from '../../lib/auth/i18n';

window.submitLoginForm = function(target, e) {
    (async (data) => {
        await initI18n();
        const initProgressLog = (await import('../../lib/progressLog')).initProgressLog;
        const pl = initProgressLog(document.querySelector('.progress-alert'));
        pl.clear();
        const e = pl.inProgress('login', tf('auth.progress.sendingMagicLink', data.email));
        const supertokens = (await import('../../lib/supertokens'));
        try {
            const inviteCodeInput = document.getElementById('invite-code-input');
            const inviteCode = inviteCodeInput ? inviteCodeInput.value.trim() : '';

            const res = await fetch('/auth/invite-code', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                    'X-CSRF-Token': window._CSRF,
                },
                body: JSON.stringify({ email: data.email, inviteCode: inviteCode }),
            });
            if (!res.ok) {
                const errData = await res.json().catch(() => ({}));
                throw new Error(errData.error || 'Verification failed');
            }

            await supertokens.sendMagicLink(data, window._CSRF);
            e.done(tf('auth.progress.magicLinkSent', data.email));
        } catch (err) {
            console.error(err);
            if (err.statusText) {
                e.error(err.statusText.toLowerCase());
            } else if (err.message) {
                e.error(err.message.toLowerCase());
            } else {
                e.error(t('auth.progress.unknownError'));
            }
        }
        e.close();
    })({
        email: target.querySelector('input[name=email]').value,
    });
    e.preventDefault();
    return false;
}

window.signInWith = function(e, provider) {
    (async () => {
        await initI18n();
        const initProgressLog = (await import('../../lib/progressLog')).initProgressLog;
        const pl = initProgressLog(document.querySelector('.progress-alert'));
        pl.clear();
        const progressEntry = pl.inProgress('login', tf('auth.progress.redirectingTo', provider));
        const supertokens = (await import('../../lib/supertokens'));
        try {
            const inviteCodeInput = document.getElementById('invite-code-input');
            const inviteCode = inviteCodeInput ? inviteCodeInput.value.trim() : '';

            const res = await fetch('/auth/invite-code', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                    'X-CSRF-Token': window._CSRF,
                },
                body: JSON.stringify({ inviteCode: inviteCode }),
            });
            if (!res.ok) {
                const errData = await res.json().catch(() => ({}));
                throw new Error(errData.error || 'Verification failed');
            }

            await supertokens.signInWith(window._CSRF, provider);
        } catch (err) {
            console.error(err);
            if (err.statusText) {
                progressEntry.error(err.statusText.toLowerCase());
            } else if (err.message) {
                progressEntry.error(err.message.toLowerCase());
            } else {
                progressEntry.error(tf('auth.progress.redirectFailed', provider));
            }
            progressEntry.close();
        }
    })();
    e.preventDefault();
    return false;
}
