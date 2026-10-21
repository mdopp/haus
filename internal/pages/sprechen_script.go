package pages

import (
	"encoding/json"
	"strings"
)

// sprechenScript is the Sprechen field's script (haus#63), right after its
// form; without it the form is a plain post and still works. On /sprechen
// (data-hier) it sends in the background and shows at once "… denkt nach"
// with a running indicator and the seconds, then the reply in pieces as it
// comes, and at the end the exchange as a reload would show it — or, never
// silence, the line that nobody answers. On Start it hands the sentence to
// /sprechen (sessionStorage), where the answer has its page. The
// microphone records in the browser (Web Audio, no browser speech service),
// makes a 16 kHz mono WAV and sends it to Haus (HoerenPath), which asks the
// box's speech-to-text; the words then go like typed ones. At most 30 s.
// Its words are not lines the page shows (Counted leaves scripts out).
var sprechenScript = strings.Replace(sprechenScriptSrc, "STUMM", jsString(saysDown("")), 1)

const sprechenScriptSrc = `<script>(function(){
var f=document.currentScript&&document.currentScript.previousElementSibling,me=document.currentScript;
if(!f||!window.fetch||!window.TextDecoder||!window.JSON)return;
var i=f.querySelector('input[name=nachricht]'),send=f.querySelector('.senden'),mic=f.querySelector('.mikro'),note=f.querySelector('.hinweis'),here=f.hasAttribute('data-hier'),KEY='haus.sprechen';
function say(t){note.textContent=t||''}
function busy(on){i.disabled=on;send.disabled=on;if(mic)mic.disabled=on}
function keep(v){try{if(v==null)sessionStorage.removeItem(KEY);else sessionStorage.setItem(KEY,v);return true}catch(e){return false}}
function kept(){try{return sessionStorage.getItem(KEY)}catch(e){return null}}
function el(tag,cls,text){var e=document.createElement(tag);if(cls)e.className=cls;if(text!=null)e.textContent=text;return e}
function ask(text){
 var olds=document.querySelectorAll('.gespraech,.vorher');for(var k=0;k<olds.length;k++)olds[k].parentNode.removeChild(olds[k]);
 var sec=el('section','gespraech');sec.setAttribute('aria-label','Gespräch');sec.setAttribute('aria-live','polite');
 var q=el('p','frage');q.appendChild(el('span','wer','Du'));q.appendChild(document.createTextNode(text));sec.appendChild(q);
 var wait=el('p','denkt'),secs=el('span','sek','');wait.appendChild(el('span','laeuft'));wait.appendChild(document.createTextNode(f.getAttribute('data-denkt')+' '));wait.appendChild(secs);sec.appendChild(wait);
 me.parentNode.insertBefore(sec,me.nextSibling);
 var t0=Date.now(),tick=setInterval(function(){var s=Math.round((Date.now()-t0)/1000);secs.textContent=s>=3?s+' s':''},1000);
 var done=false,ans=null,para=null;busy(true);
 function event(raw){raw.split('\n').forEach(function(l){
  if(l.indexOf('data:')!==0)return;var j;try{j=JSON.parse(l.slice(5))}catch(e){return}
  if(j.stueck){if(!ans){ans=el('div','antwort');ans.appendChild(el('span','wer',f.getAttribute('data-wer')));para=el('p');ans.appendChild(para);sec.insertBefore(ans,wait)}para.textContent+=j.stueck}
  if(j.fertig){done=true;var box=document.createElement('div');box.innerHTML=j.fertig;var n=box.firstElementChild;if(n){n.setAttribute('aria-live','polite');sec.parentNode.replaceChild(n,sec);sec=n}}
 })}
 fetch('/sprechen',{method:'POST',credentials:'same-origin',headers:{'Accept':'text/event-stream','Content-Type':'application/x-www-form-urlencoded;charset=UTF-8'},body:'nachricht='+encodeURIComponent(text)})
 .then(function(r){
  if(!r.ok||!r.body||!r.body.getReader)throw new Error('kein Strom');
  var rd=r.body.getReader(),dec=new TextDecoder(),buf='';
  function pump(){return rd.read().then(function(x){
   if(x.done)return;buf+=dec.decode(x.value,{stream:true});
   for(var k=buf.indexOf('\n\n');k>=0;k=buf.indexOf('\n\n')){event(buf.slice(0,k));buf=buf.slice(k+2)}
   return pump()})}
  return pump()})
 .catch(function(){})
 .then(function(){
  clearInterval(tick);
  if(!done){if(wait.parentNode)wait.parentNode.removeChild(wait);sec.appendChild(el('p','keine',f.getAttribute('data-wer')+STUMM))}
  busy(false);if(done)i.value='';i.focus()})
}
f.addEventListener('submit',function(e){
 var text=i.value.trim();if(!text)return;
 if(here){e.preventDefault();ask(text);return}
 if(keep(text)){e.preventDefault();location.assign('/sprechen')}
});
if(here){var p=kept();if(p){keep(null);i.value=p;ask(p)}}
var AC=window.AudioContext||window.webkitAudioContext;
if(!mic||!AC||!navigator.mediaDevices||!navigator.mediaDevices.getUserMedia)return;
mic.hidden=false;
var rec=null,fail='Die Spracherkennung antwortet gerade nicht. Tippe deine Frage lieber ein.';
function wav(parts,len,rate){
 var R=16000,ratio=rate/R,n=Math.floor(len/ratio),all=new Float32Array(len),o=0;
 parts.forEach(function(p){all.set(p,o);o+=p.length});
 var v=new DataView(new ArrayBuffer(44+n*2));
 function s(off,str){for(var k=0;k<str.length;k++)v.setUint8(off+k,str.charCodeAt(k))}
 s(0,'RIFF');v.setUint32(4,36+n*2,true);s(8,'WAVE');s(12,'fmt ');v.setUint32(16,16,true);v.setUint16(20,1,true);v.setUint16(22,1,true);
 v.setUint32(24,R,true);v.setUint32(28,R*2,true);v.setUint16(32,2,true);v.setUint16(34,16,true);s(36,'data');v.setUint32(40,n*2,true);
 for(var k=0;k<n;k++){var a=Math.floor(k*ratio),b=Math.min(len,Math.floor((k+1)*ratio)),sum=0;for(var m=a;m<b;m++)sum+=all[m];
  var x=b>a?sum/(b-a):(all[a]||0);x=Math.max(-1,Math.min(1,x));v.setInt16(44+k*2,x<0?x*32768:x*32767,true)}
 return new Blob([v.buffer],{type:'audio/wav'})}
mic.addEventListener('click',function(){
 if(rec){rec();return}
 navigator.mediaDevices.getUserMedia({audio:{channelCount:1,echoCancellation:true,noiseSuppression:true}}).then(function(st){
  var ac=new AC(),src=ac.createMediaStreamSource(st),pr=ac.createScriptProcessor(4096,1,1),parts=[],len=0,timer;
  pr.onaudioprocess=function(e){var d=e.inputBuffer.getChannelData(0);parts.push(new Float32Array(d));len+=d.length};
  src.connect(pr);pr.connect(ac.destination);
  mic.setAttribute('aria-pressed','true');mic.classList.add('an');say('Aufnahme läuft … zum Beenden noch einmal auf das Mikrofon tippen.');
  rec=function(){
   clearTimeout(timer);rec=null;mic.setAttribute('aria-pressed','false');mic.classList.remove('an');
   st.getTracks().forEach(function(t){t.stop()});pr.disconnect();src.disconnect();var rate=ac.sampleRate;if(ac.close)ac.close();
   if(len<rate*0.3){say('Das war zu kurz. Tippe auf das Mikrofon, sprich, und tippe dann noch einmal.');return}
   say('Wird in Text verwandelt …');busy(true);
   fetch('` + HoerenPath + `',{method:'POST',credentials:'same-origin',headers:{'Content-Type':'audio/wav'},body:wav(parts,len,rate)})
   .then(function(r){return r.json()})
   .then(function(j){busy(false);if(j&&j.ok&&j.text){say('');i.value=j.text;if(f.requestSubmit)f.requestSubmit();else f.submit()}else say((j&&j.says)||fail)})
   .catch(function(){busy(false);say(fail)})};
  timer=setTimeout(function(){if(rec)rec()},30000);
 }).catch(function(){say('Das Mikrofon ist nicht freigegeben. Erlaube es im Browser für diese Seite, oder tippe deine Frage ein.')});
});
})();</script>`

// jsString is s as a script's string literal.
func jsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
