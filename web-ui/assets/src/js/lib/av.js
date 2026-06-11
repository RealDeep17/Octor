export default function (init, destroy = null) {
    const script = document.currentScript;
    const target = script ? script.parentElement : null;
    window.av = window.av || [];
    window.av.push([target, init, destroy, script]);
}