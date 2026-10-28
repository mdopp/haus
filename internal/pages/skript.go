package pages

import "strconv"

// hausScript is the family pages' one script (owner, 2026-10-24: scripts
// are welcome): every form posts in the background instead of reloading
// the page. The pressed button answers at once — a switch flips, a button
// with data-laeuft says what runs — and the answer replaces the page in
// place: the scroll stays, the focus stays, open folds stay open, the
// address follows a redirect. A refusal the server says in a sentence
// (text/plain) is shown at the bottom and the page stays as it was.
//
// A page that changes by itself carries <div hidden data-auffrischen=N>:
// it is fetched again every N seconds, in place, while the page is seen
// and nobody is typing or pressing. With data-live it also listens on
// LivePath and is fetched again as soon as a thing on it changes (Home
// Assistant pushes it), so N is only the fallback.
//
// A form with data-ganz, or one whose answer is not a page (an app link),
// goes the browser's own way. The script sits in the head and listens on
// the document, so a page swapped in needs nothing bound again; the
// page's own inline scripts run again after a swap.
const hausScript = `<script>(function(){if(window.hausSkript)return;window.hausSkript=1;
var busy=0,last=0,timer=null,sec=1e3,es=null;
var saysAway='Haus ist gerade nicht erreichbar. Prüf die Verbindung und versuch es dann noch einmal.';
var saysLong='Haus hat nach einer Minute noch nicht geantwortet. Lade die Seite neu und schau nach, ob es geklappt hat, bevor du es noch einmal versuchst.';
function or(a,b){return a?a:b}
function act(e){return e.form?e.form.getAttribute('action'):''}
function say(t){var h=document.getElementById('haus-hinweis');if(!h){h=document.createElement('div');h.id='haus-hinweis';h.setAttribute('role','alert');document.body.appendChild(h)}
h.innerHTML='';var p=document.createElement('span');p.textContent=t;var x=document.createElement('button');x.type='button';x.className='zu';x.setAttribute('aria-label','Hinweis schließen');x.textContent='×';
x.onclick=function(){h.className=''};h.appendChild(p);h.appendChild(x);h.className='zeigt'}
function keep(){var a=document.activeElement,f=null;if(a&&a!==document.body){f={id:a.id,name:a.name,act:act(a)}}
var open=[];document.querySelectorAll('details[open]>summary').forEach(function(s){open.push(s.textContent)});return {x:scrollX,y:scrollY,f:f,open:open}}
function restore(k,same){document.querySelectorAll('details>summary').forEach(function(s){if(k.open.indexOf(s.textContent)>=0)s.parentNode.open=true});
scrollTo(same?k.x:0,same?k.y:0);var f=k.f;if(!f)return;var el=f.id?document.getElementById(f.id):null;
if(!el&&f.name)document.querySelectorAll('[name="'+f.name+'"]').forEach(function(e){if(!el&&act(e)===f.act&&e.type!=='hidden')el=e});
if(el&&el.focus)el.focus({preventScroll:true})}
function swap(text,url){var d=new DOMParser().parseFromString(text,'text/html');var same=new URL(url,location.href).pathname===location.pathname;var k=keep();
document.title=d.title;document.body.innerHTML=d.body.innerHTML;if(url!==location.href)history.replaceState(null,'',url);
document.body.querySelectorAll('script').forEach(function(o){var s=document.createElement('script');s.textContent=o.textContent;o.parentNode.replaceChild(s,o)});
restore(k,same);plan()}
function heard(e){var m=document.querySelector('[data-live]');if(!m)return;
if(m.getAttribute('data-live')!=='alle'&&document.body.innerHTML.indexOf(e.data)<0)return;clearTimeout(timer);timer=setTimeout(fresh,400)}
function plan(){clearTimeout(timer);var m=document.querySelector('[data-auffrischen]');if(!m)return;
if(m.hasAttribute('data-live')&&!es&&window.EventSource){es=new EventSource('/live');es.addEventListener('geaendert',heard)}timer=setTimeout(fresh,or(+m.getAttribute('data-auffrischen'),15)*sec)}
function typing(){var a=document.activeElement;return a&&['INPUT','TEXTAREA','SELECT'].indexOf(a.tagName)>=0&&a.type!=='hidden'&&a.type!=='submit'}
function fresh(){if(document.hidden){timer=setTimeout(fresh,3*sec);return}if(busy){timer=setTimeout(fresh,3*sec);return}
if(typing()){timer=setTimeout(fresh,3*sec);return}if(Date.now()-last<4*sec){timer=setTimeout(fresh,3*sec);return}
fetch(location.href,{credentials:'same-origin',headers:{'X-Haus-Skript':'1'}}).then(function(r){if(!r.ok)throw 0;return r.text()}).then(function(t){if(!busy)swap(t,location.href)}).catch(function(){timer=setTimeout(fresh,30*sec)})}
function flip(b){b.setAttribute('aria-checked',b.getAttribute('aria-checked')==='true'?'false':'true')}
document.addEventListener('click',function(e){var b=e.target.closest?e.target.closest('button[data-kopieren]'):null;if(!b)return;var i=b.previousElementSibling;if(!i)return;
i.select();function ok(){b.textContent='Kopiert'}if(navigator.clipboard){navigator.clipboard.writeText(i.value).then(ok,function(){document.execCommand('copy');ok()})}else{document.execCommand('copy');ok()}});
document.addEventListener('submit',function(e){var f=e.target;if(e.defaultPrevented)return;
if(or(f.getAttribute('method'),'').toLowerCase()!=='post'){var g=or(e.submitter,f.querySelector('button'));if(g&&g.hasAttribute('data-laeuft'))setTimeout(function(){g.textContent=g.getAttribute('data-laeuft');g.disabled=true},0);return}
if(f.hasAttribute('data-ganz'))return;if(!window.fetch)return;
var q=f.getAttribute('data-frage');if(q&&!confirm(q)){e.preventDefault();return}
e.preventDefault();var b=or(e.submitter,f.querySelector('button')),was=null,sw=b&&b.getAttribute('role')==='switch';last=Date.now();busy++;
if(b){if(sw)flip(b);if(b.hasAttribute('data-laeuft')){was=b.textContent;b.textContent=b.getAttribute('data-laeuft')}b.disabled=true;b.setAttribute('aria-busy','true')}
var body=new FormData(f);if(e.submitter&&e.submitter.name)body.append(e.submitter.name,e.submitter.value);
if(f.enctype!=='multipart/form-data')body=new URLSearchParams(body);
function back(){if(!b)return;b.disabled=false;b.removeAttribute('aria-busy');if(was!==null)b.textContent=was;if(sw)flip(b)}
var ctl=window.AbortController?new AbortController():null,lang=false,uhr=setTimeout(function(){lang=true;if(ctl)ctl.abort()},60*sec);
fetch(or(f.getAttribute('action'),location.pathname+location.search),{method:'POST',body:body,credentials:'same-origin',headers:{'X-Haus-Skript':'1'},signal:ctl?ctl.signal:undefined}).then(function(r){clearTimeout(uhr);var ct=or(r.headers.get('Content-Type'),'');
return r.text().then(function(t){busy--;if(ct.indexOf('text/html')===0){swap(t,r.url);return}back();say(or(t.trim(),'Das hat nicht geklappt.'))})})
.catch(function(){clearTimeout(uhr);busy--;back();say(lang?saysLong:saysAway)})},false);
if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',plan);else plan()})()</script>`

// hausStyle is what the script shows: the sentence at the bottom, a
// pressed button.
const hausStyle = `<style>#haus-hinweis{position:fixed;left:50%;bottom:calc(76px + env(safe-area-inset-bottom));transform:translate(-50%,20px);max-width:min(32rem,calc(100% - 32px));
padding:12px 16px;border-radius:14px;background:var(--danger-soft);border:1px solid var(--danger);color:var(--ink);opacity:0;pointer-events:none;transition:opacity .2s,transform .2s;z-index:20;backdrop-filter:blur(8px)}
#haus-hinweis.zeigt{opacity:1;transform:translate(-50%,0);pointer-events:auto}#haus-hinweis{display:flex;gap:10px;align-items:flex-start}#haus-hinweis span{flex:1}
#haus-hinweis .zu{min-height:32px;min-width:32px;padding:0;background:none;color:var(--ink);font-size:1.2rem;line-height:1}button[aria-busy=true]{opacity:.6;cursor:progress}
@media (prefers-reduced-motion:reduce){#haus-hinweis{transition:none}}</style>`

// refreshEvery marks a page that changes by itself: hausScript fetches it
// again every n seconds, in place.
func refreshEvery(n int) string {
	return `<div hidden data-auffrischen=` + strconv.Itoa(n) + `></div>`
}
