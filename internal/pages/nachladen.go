package pages

import "html"

// A part of a page that waits on a slow backend comes after the page
// (owner 2026-11-03: „Warum werden die Aufgaben nicht async geladen?“): the
// page holds its place, <div data-nachladen=PATH>, with what it shows
// meanwhile — a plain link that works without script — and a <template>
// with what it says when the part does not come. nachladenScript fetches
// PATH and puts the answer in the place (an empty answer: the part is
// gone); no answer within nachladenMS, or a refusal, and the
// template's text takes the place, so nothing spins for ever. It lives
// beside homesScript, not in it, and finds places again after the page
// was swapped in place (a form's answer, a page fetched again).
const nachladenMS = "5e3" // a part not here after five seconds is not coming

const nachladenScript = `<script>(function(){if(window.homesNachladen)return;window.homesNachladen=1;
function one(el){if(el.hasAttribute('data-laedt'))return;el.setAttribute('data-laedt','');var done=false;
var ctl=window.AbortController?new AbortController():null,uhr=setTimeout(function(){if(ctl)ctl.abort();put(null)},` + nachladenMS + `);
function put(h){if(done)return;if(!el.parentNode)return;done=true;clearTimeout(uhr);if(h===null){var t=el.querySelector('template');h=t?t.innerHTML:''}el.outerHTML=h}
fetch(el.getAttribute('data-nachladen'),{credentials:'same-origin',headers:{'X-Homes-Skript':'1'},signal:ctl?ctl.signal:undefined})
.then(function(r){if(!r.ok)throw 0;return r.text()}).then(put).catch(function(){put(null)})}
function all(){if(!window.fetch)return;document.querySelectorAll('[data-nachladen]').forEach(one)}
if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',all);else all();
if(window.MutationObserver)new MutationObserver(all).observe(document.documentElement,{childList:true,subtree:true})})()</script>`

// nachladen is a part's place: meanwhile is shown until path answers,
// failed when it does not.
func nachladen(path, meanwhile, failed string) string {
	return `<div data-nachladen="` + html.EscapeString(path) + `" aria-busy=true>` + meanwhile + `<template>` + failed + `</template></div>`
}
