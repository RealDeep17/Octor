import {makeDebug} from './debug';
const debug = await makeDebug('octor:embed:message');
export default function init() {
    if (window.av) {
        for (const data  of window.av) {
            initAsyncView(...data);
        }
    }
    window.av = {
        push(data) {
            initAsyncView(...data);
        }
    }
}
function addAsyncView(el, name) {
    if (!el) return;
    const current = el.getAttribute('data-async-view') || '';
    const views = current ? current.split(' ') : [];
    if (!views.includes(name)) {
        views.push(name);
        el.setAttribute('data-async-view', views.join(' '));
    }
}

function initAsyncView(target, init, destroy, script) {
    let scriptEl = script;
    if (!scriptEl && target) {
        const scripts = target.getElementsByTagName('script');
        scriptEl = scripts[scripts.length-1];
    }
    if (!scriptEl) {
        debug(`octor:async view failed, no script element found`);
        return;
    }
    const src = scriptEl.src;
    const url = new URL(src);
    const name = url.pathname.replace(/\.js$/, '');
    addAsyncView(target, name);
    const onLoad = function(e) {
        debug(`octor:async view script loaded name=%o`, name);
        const target = e.detail.target;
        addAsyncView(target, name);
        if (target && !target.reload) {
            target.reload = function() {
                return new Promise(function(resolve, _) {
                    target.reloadResolve = resolve;
                })
            }
        }
        init.call(target);
    }
    const onDestroy = async (e) => {
        debug(`octor:async view script destroyed name=%o`, name);
        const event = new CustomEvent(`async:${name}_destroyed`);
        if (destroy) {
            let target = document;
            if (e && e.detail && e.detail.target) {
                target = e.detail.target;
            }
            await destroy.call(target);
        }
        window.dispatchEvent(event);
    }
    let key = `__async${name}_loaded`;
    if (!window[key]) {
        window.addEventListener(`async:${name}`, onLoad);
        window.addEventListener(`async:${name}_destroy`, onDestroy);
        window[key] = true;
        onLoad({
            detail: {
                target,
            },
        });
    }

}
