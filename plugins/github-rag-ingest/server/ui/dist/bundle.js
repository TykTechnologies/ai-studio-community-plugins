"use strict";(()=>{var St=Object.defineProperty;var At=Object.getOwnPropertyDescriptor;var n=(r,e,t,s)=>{for(var i=s>1?void 0:s?At(e,t):e,o=r.length-1,a;o>=0;o--)(a=r[o])&&(i=(s?a(e,t,i):a(i))||i);return s&&i&&St(e,t,i),i};var K=globalThis,Z=K.ShadowRoot&&(K.ShadyCSS===void 0||K.ShadyCSS.nativeShadow)&&"adoptedStyleSheets"in Document.prototype&&"replace"in CSSStyleSheet.prototype,H=Symbol(),lt=new WeakMap,z=class{constructor(e,t,s){if(this._$cssResult$=!0,s!==H)throw Error("CSSResult is not constructable. Use `unsafeCSS` or `css` instead.");this.cssText=e,this.t=t}get styleSheet(){let e=this.o,t=this.t;if(Z&&e===void 0){let s=t!==void 0&&t.length===1;s&&(e=lt.get(t)),e===void 0&&((this.o=e=new CSSStyleSheet).replaceSync(this.cssText),s&&lt.set(t,e))}return e}toString(){return this.cssText}},dt=r=>new z(typeof r=="string"?r:r+"",void 0,H),$=(r,...e)=>{let t=r.length===1?r[0]:e.reduce((s,i,o)=>s+(a=>{if(a._$cssResult$===!0)return a.cssText;if(typeof a=="number")return a;throw Error("Value passed to 'css' function must be a 'css' function result: "+a+". Use 'unsafeCSS' to pass non-literal values, but take care to ensure page security.")})(i)+r[o+1],r[0]);return new z(t,r,H)},ct=(r,e)=>{if(Z)r.adoptedStyleSheets=e.map(t=>t instanceof CSSStyleSheet?t:t.styleSheet);else for(let t of e){let s=document.createElement("style"),i=K.litNonce;i!==void 0&&s.setAttribute("nonce",i),s.textContent=t.cssText,r.appendChild(s)}},G=Z?r=>r:r=>r instanceof CSSStyleSheet?(e=>{let t="";for(let s of e.cssRules)t+=s.cssText;return dt(t)})(r):r;var{is:kt,defineProperty:Et,getOwnPropertyDescriptor:Pt,getOwnPropertyNames:Ct,getOwnPropertySymbols:It,getPrototypeOf:jt}=Object,E=globalThis,pt=E.trustedTypes,Tt=pt?pt.emptyScript:"",Dt=E.reactiveElementPolyfillSupport,M=(r,e)=>r,F={toAttribute(r,e){switch(e){case Boolean:r=r?Tt:null;break;case Object:case Array:r=r==null?r:JSON.stringify(r)}return r},fromAttribute(r,e){let t=r;switch(e){case Boolean:t=r!==null;break;case Number:t=r===null?null:Number(r);break;case Object:case Array:try{t=JSON.parse(r)}catch{t=null}}return t}},Y=(r,e)=>!kt(r,e),ht={attribute:!0,type:String,converter:F,reflect:!1,useDefault:!1,hasChanged:Y};Symbol.metadata??(Symbol.metadata=Symbol("metadata")),E.litPropertyMetadata??(E.litPropertyMetadata=new WeakMap);var S=class extends HTMLElement{static addInitializer(e){this._$Ei(),(this.l??(this.l=[])).push(e)}static get observedAttributes(){return this.finalize(),this._$Eh&&[...this._$Eh.keys()]}static createProperty(e,t=ht){if(t.state&&(t.attribute=!1),this._$Ei(),this.prototype.hasOwnProperty(e)&&((t=Object.create(t)).wrapped=!0),this.elementProperties.set(e,t),!t.noAccessor){let s=Symbol(),i=this.getPropertyDescriptor(e,s,t);i!==void 0&&Et(this.prototype,e,i)}}static getPropertyDescriptor(e,t,s){let{get:i,set:o}=Pt(this.prototype,e)??{get(){return this[t]},set(a){this[t]=a}};return{get:i,set(a){let c=i?.call(this);o?.call(this,a),this.requestUpdate(e,c,s)},configurable:!0,enumerable:!0}}static getPropertyOptions(e){return this.elementProperties.get(e)??ht}static _$Ei(){if(this.hasOwnProperty(M("elementProperties")))return;let e=jt(this);e.finalize(),e.l!==void 0&&(this.l=[...e.l]),this.elementProperties=new Map(e.elementProperties)}static finalize(){if(this.hasOwnProperty(M("finalized")))return;if(this.finalized=!0,this._$Ei(),this.hasOwnProperty(M("properties"))){let t=this.properties,s=[...Ct(t),...It(t)];for(let i of s)this.createProperty(i,t[i])}let e=this[Symbol.metadata];if(e!==null){let t=litPropertyMetadata.get(e);if(t!==void 0)for(let[s,i]of t)this.elementProperties.set(s,i)}this._$Eh=new Map;for(let[t,s]of this.elementProperties){let i=this._$Eu(t,s);i!==void 0&&this._$Eh.set(i,t)}this.elementStyles=this.finalizeStyles(this.styles)}static finalizeStyles(e){let t=[];if(Array.isArray(e)){let s=new Set(e.flat(1/0).reverse());for(let i of s)t.unshift(G(i))}else e!==void 0&&t.push(G(e));return t}static _$Eu(e,t){let s=t.attribute;return s===!1?void 0:typeof s=="string"?s:typeof e=="string"?e.toLowerCase():void 0}constructor(){super(),this._$Ep=void 0,this.isUpdatePending=!1,this.hasUpdated=!1,this._$Em=null,this._$Ev()}_$Ev(){this._$ES=new Promise(e=>this.enableUpdating=e),this._$AL=new Map,this._$E_(),this.requestUpdate(),this.constructor.l?.forEach(e=>e(this))}addController(e){(this._$EO??(this._$EO=new Set)).add(e),this.renderRoot!==void 0&&this.isConnected&&e.hostConnected?.()}removeController(e){this._$EO?.delete(e)}_$E_(){let e=new Map,t=this.constructor.elementProperties;for(let s of t.keys())this.hasOwnProperty(s)&&(e.set(s,this[s]),delete this[s]);e.size>0&&(this._$Ep=e)}createRenderRoot(){let e=this.shadowRoot??this.attachShadow(this.constructor.shadowRootOptions);return ct(e,this.constructor.elementStyles),e}connectedCallback(){this.renderRoot??(this.renderRoot=this.createRenderRoot()),this.enableUpdating(!0),this._$EO?.forEach(e=>e.hostConnected?.())}enableUpdating(e){}disconnectedCallback(){this._$EO?.forEach(e=>e.hostDisconnected?.())}attributeChangedCallback(e,t,s){this._$AK(e,s)}_$ET(e,t){let s=this.constructor.elementProperties.get(e),i=this.constructor._$Eu(e,s);if(i!==void 0&&s.reflect===!0){let o=(s.converter?.toAttribute!==void 0?s.converter:F).toAttribute(t,s.type);this._$Em=e,o==null?this.removeAttribute(i):this.setAttribute(i,o),this._$Em=null}}_$AK(e,t){let s=this.constructor,i=s._$Eh.get(e);if(i!==void 0&&this._$Em!==i){let o=s.getPropertyOptions(i),a=typeof o.converter=="function"?{fromAttribute:o.converter}:o.converter?.fromAttribute!==void 0?o.converter:F;this._$Em=i;let c=a.fromAttribute(t,o.type);this[i]=c??this._$Ej?.get(i)??c,this._$Em=null}}requestUpdate(e,t,s){if(e!==void 0){let i=this.constructor,o=this[e];if(s??(s=i.getPropertyOptions(e)),!((s.hasChanged??Y)(o,t)||s.useDefault&&s.reflect&&o===this._$Ej?.get(e)&&!this.hasAttribute(i._$Eu(e,s))))return;this.C(e,t,s)}this.isUpdatePending===!1&&(this._$ES=this._$EP())}C(e,t,{useDefault:s,reflect:i,wrapped:o},a){s&&!(this._$Ej??(this._$Ej=new Map)).has(e)&&(this._$Ej.set(e,a??t??this[e]),o!==!0||a!==void 0)||(this._$AL.has(e)||(this.hasUpdated||s||(t=void 0),this._$AL.set(e,t)),i===!0&&this._$Em!==e&&(this._$Eq??(this._$Eq=new Set)).add(e))}async _$EP(){this.isUpdatePending=!0;try{await this._$ES}catch(t){Promise.reject(t)}let e=this.scheduleUpdate();return e!=null&&await e,!this.isUpdatePending}scheduleUpdate(){return this.performUpdate()}performUpdate(){if(!this.isUpdatePending)return;if(!this.hasUpdated){if(this.renderRoot??(this.renderRoot=this.createRenderRoot()),this._$Ep){for(let[i,o]of this._$Ep)this[i]=o;this._$Ep=void 0}let s=this.constructor.elementProperties;if(s.size>0)for(let[i,o]of s){let{wrapped:a}=o,c=this[i];a!==!0||this._$AL.has(i)||c===void 0||this.C(i,void 0,o,c)}}let e=!1,t=this._$AL;try{e=this.shouldUpdate(t),e?(this.willUpdate(t),this._$EO?.forEach(s=>s.hostUpdate?.()),this.update(t)):this._$EM()}catch(s){throw e=!1,this._$EM(),s}e&&this._$AE(t)}willUpdate(e){}_$AE(e){this._$EO?.forEach(t=>t.hostUpdated?.()),this.hasUpdated||(this.hasUpdated=!0,this.firstUpdated(e)),this.updated(e)}_$EM(){this._$AL=new Map,this.isUpdatePending=!1}get updateComplete(){return this.getUpdateComplete()}getUpdateComplete(){return this._$ES}shouldUpdate(e){return!0}update(e){this._$Eq&&(this._$Eq=this._$Eq.forEach(t=>this._$ET(t,this[t]))),this._$EM()}updated(e){}firstUpdated(e){}};S.elementStyles=[],S.shadowRootOptions={mode:"open"},S[M("elementProperties")]=new Map,S[M("finalized")]=new Map,Dt?.({ReactiveElement:S}),(E.reactiveElementVersions??(E.reactiveElementVersions=[])).push("2.1.1");var B=globalThis,Q=B.trustedTypes,ut=Q?Q.createPolicy("lit-html",{createHTML:r=>r}):void 0,yt="$lit$",P=`lit$${Math.random().toFixed(9).slice(2)}$`,$t="?"+P,Ut=`<${$t}>`,T=document,L=()=>T.createComment(""),q=r=>r===null||typeof r!="object"&&typeof r!="function",at=Array.isArray,Ot=r=>at(r)||typeof r?.[Symbol.iterator]=="function",tt=`[ 	
\f\r]`,N=/<(?:(!--|\/[^a-zA-Z])|(\/?[a-zA-Z][^>\s]*)|(\/?$))/g,gt=/-->/g,ft=/>/g,I=RegExp(`>|${tt}(?:([^\\s"'>=/]+)(${tt}*=${tt}*(?:[^ 	
\f\r"'\`<>=]|("|')|))|$)`,"g"),mt=/'/g,vt=/"/g,_t=/^(?:script|style|textarea|title)$/i,nt=r=>(e,...t)=>({_$litType$:r,strings:e,values:t}),d=nt(1),Wt=nt(2),Kt=nt(3),D=Symbol.for("lit-noChange"),g=Symbol.for("lit-nothing"),bt=new WeakMap,j=T.createTreeWalker(T,129);function xt(r,e){if(!at(r)||!r.hasOwnProperty("raw"))throw Error("invalid template strings array");return ut!==void 0?ut.createHTML(e):e}var zt=(r,e)=>{let t=r.length-1,s=[],i,o=e===2?"<svg>":e===3?"<math>":"",a=N;for(let c=0;c<t;c++){let l=r[c],u,m,p=-1,w=0;for(;w<l.length&&(a.lastIndex=w,m=a.exec(l),m!==null);)w=a.lastIndex,a===N?m[1]==="!--"?a=gt:m[1]!==void 0?a=ft:m[2]!==void 0?(_t.test(m[2])&&(i=RegExp("</"+m[2],"g")),a=I):m[3]!==void 0&&(a=I):a===I?m[0]===">"?(a=i??N,p=-1):m[1]===void 0?p=-2:(p=a.lastIndex-m[2].length,u=m[1],a=m[3]===void 0?I:m[3]==='"'?vt:mt):a===vt||a===mt?a=I:a===gt||a===ft?a=N:(a=I,i=void 0);let k=a===I&&r[c+1].startsWith("/>")?" ":"";o+=a===N?l+Ut:p>=0?(s.push(u),l.slice(0,p)+yt+l.slice(p)+P+k):l+P+(p===-2?c:k)}return[xt(r,o+(r[t]||"<?>")+(e===2?"</svg>":e===3?"</math>":"")),s]},R=class r{constructor({strings:e,_$litType$:t},s){let i;this.parts=[];let o=0,a=0,c=e.length-1,l=this.parts,[u,m]=zt(e,t);if(this.el=r.createElement(u,s),j.currentNode=this.el.content,t===2||t===3){let p=this.el.content.firstChild;p.replaceWith(...p.childNodes)}for(;(i=j.nextNode())!==null&&l.length<c;){if(i.nodeType===1){if(i.hasAttributes())for(let p of i.getAttributeNames())if(p.endsWith(yt)){let w=m[a++],k=i.getAttribute(p).split(P),W=/([.?@])?(.*)/.exec(w);l.push({type:1,index:o,name:W[2],strings:k,ctor:W[1]==="."?st:W[1]==="?"?it:W[1]==="@"?rt:O}),i.removeAttribute(p)}else p.startsWith(P)&&(l.push({type:6,index:o}),i.removeAttribute(p));if(_t.test(i.tagName)){let p=i.textContent.split(P),w=p.length-1;if(w>0){i.textContent=Q?Q.emptyScript:"";for(let k=0;k<w;k++)i.append(p[k],L()),j.nextNode(),l.push({type:2,index:++o});i.append(p[w],L())}}}else if(i.nodeType===8)if(i.data===$t)l.push({type:2,index:o});else{let p=-1;for(;(p=i.data.indexOf(P,p+1))!==-1;)l.push({type:7,index:o}),p+=P.length-1}o++}}static createElement(e,t){let s=T.createElement("template");return s.innerHTML=e,s}};function U(r,e,t=r,s){if(e===D)return e;let i=s!==void 0?t._$Co?.[s]:t._$Cl,o=q(e)?void 0:e._$litDirective$;return i?.constructor!==o&&(i?._$AO?.(!1),o===void 0?i=void 0:(i=new o(r),i._$AT(r,t,s)),s!==void 0?(t._$Co??(t._$Co=[]))[s]=i:t._$Cl=i),i!==void 0&&(e=U(r,i._$AS(r,e.values),i,s)),e}var et=class{constructor(e,t){this._$AV=[],this._$AN=void 0,this._$AD=e,this._$AM=t}get parentNode(){return this._$AM.parentNode}get _$AU(){return this._$AM._$AU}u(e){let{el:{content:t},parts:s}=this._$AD,i=(e?.creationScope??T).importNode(t,!0);j.currentNode=i;let o=j.nextNode(),a=0,c=0,l=s[0];for(;l!==void 0;){if(a===l.index){let u;l.type===2?u=new V(o,o.nextSibling,this,e):l.type===1?u=new l.ctor(o,l.name,l.strings,this,e):l.type===6&&(u=new ot(o,this,e)),this._$AV.push(u),l=s[++c]}a!==l?.index&&(o=j.nextNode(),a++)}return j.currentNode=T,i}p(e){let t=0;for(let s of this._$AV)s!==void 0&&(s.strings!==void 0?(s._$AI(e,s,t),t+=s.strings.length-2):s._$AI(e[t])),t++}},V=class r{get _$AU(){return this._$AM?._$AU??this._$Cv}constructor(e,t,s,i){this.type=2,this._$AH=g,this._$AN=void 0,this._$AA=e,this._$AB=t,this._$AM=s,this.options=i,this._$Cv=i?.isConnected??!0}get parentNode(){let e=this._$AA.parentNode,t=this._$AM;return t!==void 0&&e?.nodeType===11&&(e=t.parentNode),e}get startNode(){return this._$AA}get endNode(){return this._$AB}_$AI(e,t=this){e=U(this,e,t),q(e)?e===g||e==null||e===""?(this._$AH!==g&&this._$AR(),this._$AH=g):e!==this._$AH&&e!==D&&this._(e):e._$litType$!==void 0?this.$(e):e.nodeType!==void 0?this.T(e):Ot(e)?this.k(e):this._(e)}O(e){return this._$AA.parentNode.insertBefore(e,this._$AB)}T(e){this._$AH!==e&&(this._$AR(),this._$AH=this.O(e))}_(e){this._$AH!==g&&q(this._$AH)?this._$AA.nextSibling.data=e:this.T(T.createTextNode(e)),this._$AH=e}$(e){let{values:t,_$litType$:s}=e,i=typeof s=="number"?this._$AC(e):(s.el===void 0&&(s.el=R.createElement(xt(s.h,s.h[0]),this.options)),s);if(this._$AH?._$AD===i)this._$AH.p(t);else{let o=new et(i,this),a=o.u(this.options);o.p(t),this.T(a),this._$AH=o}}_$AC(e){let t=bt.get(e.strings);return t===void 0&&bt.set(e.strings,t=new R(e)),t}k(e){at(this._$AH)||(this._$AH=[],this._$AR());let t=this._$AH,s,i=0;for(let o of e)i===t.length?t.push(s=new r(this.O(L()),this.O(L()),this,this.options)):s=t[i],s._$AI(o),i++;i<t.length&&(this._$AR(s&&s._$AB.nextSibling,i),t.length=i)}_$AR(e=this._$AA.nextSibling,t){for(this._$AP?.(!1,!0,t);e!==this._$AB;){let s=e.nextSibling;e.remove(),e=s}}setConnected(e){this._$AM===void 0&&(this._$Cv=e,this._$AP?.(e))}},O=class{get tagName(){return this.element.tagName}get _$AU(){return this._$AM._$AU}constructor(e,t,s,i,o){this.type=1,this._$AH=g,this._$AN=void 0,this.element=e,this.name=t,this._$AM=i,this.options=o,s.length>2||s[0]!==""||s[1]!==""?(this._$AH=Array(s.length-1).fill(new String),this.strings=s):this._$AH=g}_$AI(e,t=this,s,i){let o=this.strings,a=!1;if(o===void 0)e=U(this,e,t,0),a=!q(e)||e!==this._$AH&&e!==D,a&&(this._$AH=e);else{let c=e,l,u;for(e=o[0],l=0;l<o.length-1;l++)u=U(this,c[s+l],t,l),u===D&&(u=this._$AH[l]),a||(a=!q(u)||u!==this._$AH[l]),u===g?e=g:e!==g&&(e+=(u??"")+o[l+1]),this._$AH[l]=u}a&&!i&&this.j(e)}j(e){e===g?this.element.removeAttribute(this.name):this.element.setAttribute(this.name,e??"")}},st=class extends O{constructor(){super(...arguments),this.type=3}j(e){this.element[this.name]=e===g?void 0:e}},it=class extends O{constructor(){super(...arguments),this.type=4}j(e){this.element.toggleAttribute(this.name,!!e&&e!==g)}},rt=class extends O{constructor(e,t,s,i,o){super(e,t,s,i,o),this.type=5}_$AI(e,t=this){if((e=U(this,e,t,0)??g)===D)return;let s=this._$AH,i=e===g&&s!==g||e.capture!==s.capture||e.once!==s.once||e.passive!==s.passive,o=e!==g&&(s===g||i);i&&this.element.removeEventListener(this.name,this,s),o&&this.element.addEventListener(this.name,this,e),this._$AH=e}handleEvent(e){typeof this._$AH=="function"?this._$AH.call(this.options?.host??this.element,e):this._$AH.handleEvent(e)}},ot=class{constructor(e,t,s){this.element=e,this.type=6,this._$AN=void 0,this._$AM=t,this.options=s}get _$AU(){return this._$AM._$AU}_$AI(e){U(this,e)}};var Mt=B.litHtmlPolyfillSupport;Mt?.(R,V),(B.litHtmlVersions??(B.litHtmlVersions=[])).push("3.3.1");var wt=(r,e,t)=>{let s=t?.renderBefore??e,i=s._$litPart$;if(i===void 0){let o=t?.renderBefore??null;s._$litPart$=i=new V(e.insertBefore(L(),o),o,void 0,t??{})}return i._$AI(r),i};var J=globalThis,v=class extends S{constructor(){super(...arguments),this.renderOptions={host:this},this._$Do=void 0}createRenderRoot(){var t;let e=super.createRenderRoot();return(t=this.renderOptions).renderBefore??(t.renderBefore=e.firstChild),e}update(e){let t=this.render();this.hasUpdated||(this.renderOptions.isConnected=this.isConnected),super.update(e),this._$Do=wt(t,this.renderRoot,this.renderOptions)}connectedCallback(){super.connectedCallback(),this._$Do?.setConnected(!0)}disconnectedCallback(){super.disconnectedCallback(),this._$Do?.setConnected(!1)}render(){return D}};v._$litElement$=!0,v.finalized=!0,J.litElementHydrateSupport?.({LitElement:v});var Ft=J.litElementPolyfillSupport;Ft?.({LitElement:v});(J.litElementVersions??(J.litElementVersions=[])).push("4.2.1");var x=r=>(e,t)=>{t!==void 0?t.addInitializer(()=>{customElements.define(r,e)}):customElements.define(r,e)};var Nt={attribute:!0,type:String,converter:F,reflect:!1,hasChanged:Y},Bt=(r=Nt,e,t)=>{let{kind:s,metadata:i}=t,o=globalThis.litPropertyMetadata.get(i);if(o===void 0&&globalThis.litPropertyMetadata.set(i,o=new Map),s==="setter"&&((r=Object.create(r)).wrapped=!0),o.set(t.name,r),s==="accessor"){let{name:a}=t;return{set(c){let l=e.get.call(this);e.set.call(this,c),this.requestUpdate(a,l,r)},init(c){return c!==void 0&&this.C(a,void 0,r,c),c}}}if(s==="setter"){let{name:a}=t;return function(c){let l=this[a];e.call(this,c),this.requestUpdate(a,l,r)}}throw Error("Unsupported decorator location: "+s)};function f(r){return(e,t)=>typeof t=="object"?Bt(r,e,t):((s,i,o)=>{let a=i.hasOwnProperty(o);return i.constructor.createProperty(o,s),a?Object.getOwnPropertyDescriptor(i,o):void 0})(r,e,t)}function h(r){return f({...r,state:!0,attribute:!1})}var C=class extends v{constructor(){super(...arguments);this.rpcBase="";this.stats=null;this.loading=!1}connectedCallback(){super.connectedCallback(),this.loadStats()}async loadStats(){try{await this.waitForPluginAPI();let t=await this.pluginAPI.call("get_statistics",{});t.success&&(this.stats={totalRepos:t.data.total_repos||0,activeRepos:t.data.active_repos||0,totalJobs:t.data.total_jobs||0,chunksIngested:t.data.chunks_ingested||0})}catch(t){console.error("Failed to load stats:",t),this.stats={totalRepos:0,activeRepos:0,totalJobs:0,chunksIngested:0}}}async waitForPluginAPI(){for(let t=0;t<50;t++){if(this.pluginAPI)return;await new Promise(s=>setTimeout(s,100))}throw new Error("Plugin API timeout")}render(){return d`
      <div class="header">
        <h2>GitHub RAG Ingestion</h2>
        <p class="subtitle">Manage GitHub repositories and RAG datasource ingestion</p>
      </div>

      <div class="info-card">
        <h3>Getting Started</h3>
        <ul>
          <li>Navigate to <strong>Repositories</strong> to add GitHub repositories</li>
          <li>Configure chunking strategy and datasource assignment</li>
          <li>Run manual ingestion or set up scheduled syncs</li>
          <li>View ingestion jobs and logs in the <strong>Jobs</strong> tab</li>
        </ul>
      </div>

      <div class="stats-grid">
        <div class="stat-card">
          <div class="stat-label">Repositories</div>
          <div class="stat-value">${this.stats?.totalRepos||0}</div>
        </div>
        <div class="stat-card">
          <div class="stat-label">Active Syncs</div>
          <div class="stat-value">${this.stats?.activeRepos||0}</div>
        </div>
        <div class="stat-card">
          <div class="stat-label">Total Jobs</div>
          <div class="stat-value">${this.stats?.totalJobs||0}</div>
        </div>
        <div class="stat-card">
          <div class="stat-label">Chunks Ingested</div>
          <div class="stat-value">${this.stats?.chunksIngested||0}</div>
        </div>
      </div>
    `}};C.styles=$`
    :host {
      display: block;
      padding: 24px;
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
    }

    .header {
      margin-bottom: 24px;
    }

    h2 {
      margin: 0 0 8px 0;
      font-size: 24px;
      font-weight: 600;
    }

    .subtitle {
      color: #666;
      font-size: 14px;
    }

    .stats-grid {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
      gap: 16px;
      margin-bottom: 24px;
    }

    .stat-card {
      background: white;
      border: 1px solid #e0e0e0;
      border-radius: 8px;
      padding: 16px;
    }

    .stat-label {
      font-size: 12px;
      color: #666;
      text-transform: uppercase;
      letter-spacing: 0.5px;
      margin-bottom: 8px;
    }

    .stat-value {
      font-size: 32px;
      font-weight: 600;
      color: #1976d2;
    }

    .info-card {
      background: #e3f2fd;
      border: 1px solid #90caf9;
      border-radius: 8px;
      padding: 16px;
      margin-bottom: 16px;
    }

    .info-card h3 {
      margin: 0 0 12px 0;
      font-size: 16px;
      color: #0d47a1;
    }

    .info-card ul {
      margin: 0;
      padding-left: 20px;
    }

    .info-card li {
      margin-bottom: 8px;
      color: #1565c0;
    }
  `,n([f({type:String})],C.prototype,"rpcBase",2),n([h()],C.prototype,"stats",2),n([h()],C.prototype,"loading",2),C=n([x("github-rag-dashboard")],C);var b=class extends v{constructor(){super(...arguments);this.repository=null;this.api=null;this.onSave=null;this.onCancel=null;this.datasources=[];this.formData={name:"",owner:"",url:"",branch:"main",auth_type:"public",pat_token:"",ssh_private_key:"",ssh_passphrase:"",datasource_id:0,target_paths:[],file_masks:["*"],ignore_patterns:[],chunking_strategy:"hybrid",chunk_size:1e3,chunk_overlap:200,sync_schedule:"",sync_enabled:!1};this.loading=!1;this.error=""}connectedCallback(){super.connectedCallback(),this.loadDatasources(),this.repository&&(this.formData={...this.repository})}async loadDatasources(){if(!this.api){console.error("No API available to load datasources");return}try{let t=await this.api.call("list_datasources",{});t.success?(this.datasources=t.data.datasources||[],console.log("Loaded datasources:",this.datasources)):console.error("Failed to load datasources:",t.error)}catch(t){console.error("Failed to load datasources:",t)}}handleSubmit(t){t.preventDefault(),this.saveRepository()}async saveRepository(){if(!this.api){this.error="No API available";return}this.loading=!0,this.error="";try{let t=this.repository?"update_repository":"create_repository",s=this.repository?{...this.formData,id:this.repository.id}:this.formData,i=await this.api.call(t,s);i.success?this.onSave&&this.onSave(i.data):this.error=i.error||"Failed to save repository"}catch(t){this.error=`Error: ${t.message}`}finally{this.loading=!1}}handleCancel(){this.onCancel&&this.onCancel()}updateField(t,s){this.formData={...this.formData,[t]:s}}updateArrayField(t,s){let i=s.split(",").map(o=>o.trim()).filter(o=>o);this.formData={...this.formData,[t]:i}}render(){return d`
      <div class="form-container">
        <h3>${this.repository?"Edit Repository":"Add Repository"}</h3>

        ${this.error?d`<div class="error">${this.error}</div>`:""}

        <form @submit=${this.handleSubmit}>
          <div class="form-grid">
            <div class="form-row">
              <div class="form-group">
                <label>Repository Name *</label>
                <input
                  type="text"
                  required
                  .value=${this.formData.name}
                  @input=${t=>this.updateField("name",t.target.value)}
                  placeholder="my-repo"
                />
              </div>

              <div class="form-group">
                <label>Owner *</label>
                <input
                  type="text"
                  required
                  .value=${this.formData.owner}
                  @input=${t=>this.updateField("owner",t.target.value)}
                  placeholder="TykTechnologies"
                />
              </div>
            </div>

            <div class="form-group">
              <label>Repository URL *</label>
              <input
                type="url"
                required
                .value=${this.formData.url}
                @input=${t=>this.updateField("url",t.target.value)}
                placeholder="https://github.com/owner/repo"
              />
            </div>

            <div class="form-row">
              <div class="form-group">
                <label>Branch *</label>
                <input
                  type="text"
                  required
                  .value=${this.formData.branch}
                  @input=${t=>this.updateField("branch",t.target.value)}
                  placeholder="main"
                />
              </div>

              <div class="form-group">
                <label>Authentication</label>
                <select
                  .value=${this.formData.auth_type}
                  @change=${t=>this.updateField("auth_type",t.target.value)}
                >
                  <option value="public">Public (No Auth)</option>
                  <option value="pat">Personal Access Token</option>
                  <option value="ssh">SSH Key</option>
                </select>
              </div>
            </div>

            ${this.formData.auth_type==="pat"?d`
              <div class="form-group">
                <label>Personal Access Token *</label>
                <input
                  type="password"
                  required
                  .value=${this.formData.pat_token}
                  @input=${t=>this.updateField("pat_token",t.target.value)}
                  placeholder="ghp_..."
                />
                <div class="hint">⚠️  Token stored in ${this.formData.secrets_backend||"KV"} storage</div>
              </div>
            `:""}

            ${this.formData.auth_type==="ssh"?d`
              <div class="form-group">
                <label>SSH Private Key *</label>
                <textarea
                  required
                  .value=${this.formData.ssh_private_key}
                  @input=${t=>this.updateField("ssh_private_key",t.target.value)}
                  placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                ></textarea>
              </div>

              <div class="form-group">
                <label>SSH Passphrase (if encrypted)</label>
                <input
                  type="password"
                  .value=${this.formData.ssh_passphrase}
                  @input=${t=>this.updateField("ssh_passphrase",t.target.value)}
                />
              </div>
            `:""}

            <div class="form-group">
              <label>Datasource *</label>
              <select
                required
                .value=${String(this.formData.datasource_id)}
                @change=${t=>this.updateField("datasource_id",parseInt(t.target.value))}
              >
                <option value="0">Select datasource...</option>
                ${this.datasources.map(t=>d`
                  <option value="${t.id}">${t.name} - ${t.short_description}</option>
                `)}
              </select>
              <div class="hint">The datasource must have an embedder configured</div>
            </div>

            <div class="form-group">
              <label>Target Paths (comma-separated)</label>
              <input
                type="text"
                .value=${this.formData.target_paths.join(", ")}
                @input=${t=>this.updateArrayField("target_paths",t.target.value)}
                placeholder="src/, docs/"
              />
              <div class="hint">Leave empty to include all paths</div>
            </div>

            <div class="form-group">
              <label>File Masks (comma-separated)</label>
              <input
                type="text"
                .value=${this.formData.file_masks.join(", ")}
                @input=${t=>this.updateArrayField("file_masks",t.target.value)}
                placeholder="*.go, *.md, *.ts"
              />
            </div>

            <div class="form-row">
              <div class="form-group">
                <label>Chunking Strategy</label>
                <select
                  .value=${this.formData.chunking_strategy}
                  @change=${t=>this.updateField("chunking_strategy",t.target.value)}
                >
                  <option value="simple">Simple</option>
                  <option value="code_aware">Code-Aware</option>
                  <option value="hybrid">Hybrid (Recommended)</option>
                </select>
              </div>

              <div class="form-group">
                <label>Chunk Size</label>
                <input
                  type="number"
                  min="100"
                  max="4000"
                  .value=${String(this.formData.chunk_size)}
                  @input=${t=>this.updateField("chunk_size",parseInt(t.target.value))}
                />
              </div>
            </div>

            <div class="form-group">
              <label>Sync Schedule (cron expression)</label>
              <input
                type="text"
                .value=${this.formData.sync_schedule}
                @input=${t=>this.updateField("sync_schedule",t.target.value)}
                placeholder="0 2 * * * (2 AM daily)"
              />
              <div class="hint">Leave empty for manual sync only</div>
            </div>

            <div class="checkbox-group">
              <input
                type="checkbox"
                id="sync-enabled"
                .checked=${this.formData.sync_enabled}
                @change=${t=>this.updateField("sync_enabled",t.target.checked)}
              />
              <label for="sync-enabled">Enable automatic sync</label>
            </div>
          </div>

          <div class="actions">
            <button type="submit" class="btn btn-primary" ?disabled=${this.loading}>
              ${this.loading?"Saving...":"Save Repository"}
            </button>
            <button type="button" class="btn btn-secondary" @click=${this.handleCancel}>
              Cancel
            </button>
          </div>
        </form>
      </div>
    `}};b.styles=$`
    :host {
      display: block;
    }

    .form-container {
      background: white;
      border-radius: 8px;
      padding: 24px;
      max-width: 800px;
    }

    h3 {
      margin: 0 0 24px 0;
      font-size: 20px;
      font-weight: 600;
    }

    .form-grid {
      display: grid;
      gap: 16px;
    }

    .form-group {
      display: flex;
      flex-direction: column;
      gap: 6px;
    }

    label {
      font-size: 14px;
      font-weight: 500;
      color: #333;
    }

    input, select, textarea {
      padding: 10px;
      border: 1px solid #ddd;
      border-radius: 4px;
      font-size: 14px;
      font-family: inherit;
    }

    input:focus, select:focus, textarea:focus {
      outline: none;
      border-color: #1976d2;
    }

    .form-row {
      display: grid;
      grid-template-columns: 1fr 1fr;
      gap: 16px;
    }

    .actions {
      display: flex;
      gap: 12px;
      margin-top: 24px;
      padding-top: 24px;
      border-top: 1px solid #e0e0e0;
    }

    .btn {
      padding: 10px 20px;
      border-radius: 4px;
      border: none;
      cursor: pointer;
      font-size: 14px;
      font-weight: 500;
    }

    .btn-primary {
      background: #1976d2;
      color: white;
    }

    .btn-primary:hover {
      background: #1565c0;
    }

    .btn-secondary {
      background: #f5f5f5;
      color: #333;
    }

    .btn-secondary:hover {
      background: #e0e0e0;
    }

    .error {
      background: #ffebee;
      border: 1px solid #ef5350;
      padding: 12px;
      border-radius: 4px;
      color: #c62828;
      margin-bottom: 16px;
    }

    .hint {
      font-size: 12px;
      color: #666;
      margin-top: 4px;
    }

    .checkbox-group {
      display: flex;
      align-items: center;
      gap: 8px;
    }

    .checkbox-group input[type="checkbox"] {
      width: auto;
    }

    textarea {
      min-height: 80px;
      resize: vertical;
    }
  `,n([f({type:Object})],b.prototype,"repository",2),n([f({type:Object})],b.prototype,"api",2),n([f({type:Function})],b.prototype,"onSave",2),n([f({type:Function})],b.prototype,"onCancel",2),n([h()],b.prototype,"datasources",2),n([h()],b.prototype,"formData",2),n([h()],b.prototype,"loading",2),n([h()],b.prototype,"error",2),b=n([x("github-rag-repository-form")],b);var _=class extends v{constructor(){super(...arguments);this.rpcBase="";this.repositories=[];this.loading=!1;this.error="";this.showForm=!1;this.editingRepo=null}connectedCallback(){super.connectedCallback(),this.loadRepositories()}async loadRepositories(){this.loading=!0,this.error="";try{await this.waitForPluginAPI();let t=await this.pluginAPI.call("list_repositories",{});t.success?this.repositories=t.data.repositories||[]:this.error=t.error||"Failed to load repositories"}catch(t){this.error=`Error: ${t.message}`}finally{this.loading=!1}}async waitForPluginAPI(){for(let t=0;t<50;t++){if(this.pluginAPI)return;await new Promise(s=>setTimeout(s,100))}throw new Error("Plugin API timeout")}handleAddRepository(){this.showForm=!0,this.editingRepo=null}handleEditRepository(t){this.showForm=!0,this.editingRepo=t}async handleRunIngestion(t,s="incremental"){try{let i=await this.pluginAPI.call("trigger_ingestion",{repo_id:t,type:s,dry_run:!1});i.success?alert(`${s==="full"?"Full":"Incremental"} ingestion started! Job ID: ${i.data.job_id}`):alert(`Error: ${i.error}`)}catch(i){alert(`Error: ${i.message}`)}}async handleDeleteRepository(t){if(confirm(`Delete repository "${t.owner}/${t.name}"? This will also delete all ingested chunks.`))try{let s=await this.pluginAPI.call("delete_repository",{id:t.id,delete_chunks:!0});s.success?this.loadRepositories():alert(`Error: ${s.error}`)}catch(s){alert(`Error: ${s.message}`)}}handleFormSave(t){this.showForm=!1,this.editingRepo=null,this.loadRepositories()}handleFormCancel(){this.showForm=!1,this.editingRepo=null}formatDate(t){return t?new Date(t).toLocaleString():"Never"}render(){return this.showForm?d`
        <github-rag-repository-form
          .repository=${this.editingRepo}
          .api=${this.pluginAPI}
          .onSave=${this.handleFormSave.bind(this)}
          .onCancel=${this.handleFormCancel.bind(this)}
        ></github-rag-repository-form>
      `:d`
      <div class="header">
        <h2>Repositories</h2>
        <button class="btn btn-primary" @click=${this.handleAddRepository}>
          Add Repository
        </button>
      </div>

      ${this.error?d`<div class="error">${this.error}</div>`:""}

      ${this.loading?d`<div>Loading...</div>`:this.repositories.length===0?d`
          <div class="empty-state">
            <p>No repositories configured</p>
            <p>Click "Add Repository" to get started</p>
          </div>
        `:d`
          <div class="table-container">
            <table>
              <thead>
                <tr>
                  <th>Repository</th>
                  <th>Branch</th>
                  <th>Sync Status</th>
                  <th>Last Sync</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                ${this.repositories.map(t=>d`
                  <tr>
                    <td>
                      <strong>${t.owner}/${t.name}</strong><br>
                      <small style="color: #666;">${t.url}</small>
                    </td>
                    <td>${t.branch}</td>
                    <td>
                      <span class="status-badge ${t.sync_enabled?"status-active":"status-inactive"}">
                        ${t.sync_enabled?"Active":"Inactive"}
                      </span>
                    </td>
                    <td>${this.formatDate(t.last_sync_at)}</td>
                    <td>
                      <div class="actions">
                        <button class="btn btn-sm btn-primary" @click=${()=>this.handleRunIngestion(t.id,"incremental")} title="Sync only changed files">
                          Sync
                        </button>
                        <button class="btn btn-sm btn-primary" @click=${()=>this.handleRunIngestion(t.id,"full")} title="Re-process all files">
                          Full Sync
                        </button>
                        <button class="btn btn-sm" @click=${()=>this.handleEditRepository(t)}>
                          Edit
                        </button>
                        <button class="btn btn-sm" @click=${()=>this.handleDeleteRepository(t)}>
                          Delete
                        </button>
                      </div>
                    </td>
                  </tr>
                `)}
              </tbody>
            </table>
          </div>
        `}
    `}};_.styles=$`
    :host {
      display: block;
      padding: 24px;
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
    }

    .header {
      display: flex;
      justify-content: space-between;
      align-items: center;
      margin-bottom: 24px;
    }

    h2 {
      margin: 0;
      font-size: 24px;
      font-weight: 600;
    }

    .btn {
      padding: 10px 20px;
      border-radius: 4px;
      border: none;
      cursor: pointer;
      font-size: 14px;
      font-weight: 500;
    }

    .btn-primary {
      background: #1976d2;
      color: white;
    }

    .btn-primary:hover {
      background: #1565c0;
    }

    .table-container {
      background: white;
      border: 1px solid #e0e0e0;
      border-radius: 8px;
      overflow: hidden;
    }

    table {
      width: 100%;
      border-collapse: collapse;
    }

    th {
      background: #f5f5f5;
      padding: 12px 16px;
      text-align: left;
      font-weight: 600;
      font-size: 13px;
      color: #333;
      border-bottom: 1px solid #e0e0e0;
    }

    td {
      padding: 12px 16px;
      border-bottom: 1px solid #f5f5f5;
    }

    tr:hover {
      background: #f9f9f9;
    }

    .status-badge {
      display: inline-block;
      padding: 4px 8px;
      border-radius: 4px;
      font-size: 12px;
      font-weight: 500;
    }

    .status-active {
      background: #e8f5e9;
      color: #2e7d32;
    }

    .status-inactive {
      background: #fafafa;
      color: #666;
    }

    .actions {
      display: flex;
      gap: 8px;
    }

    .btn-sm {
      padding: 4px 12px;
      font-size: 12px;
    }

    .empty-state {
      text-align: center;
      padding: 64px 24px;
      color: #666;
    }

    .error {
      background: #ffebee;
      border: 1px solid #ef5350;
      padding: 12px;
      border-radius: 4px;
      color: #c62828;
      margin-bottom: 16px;
    }
  `,n([f({type:String})],_.prototype,"rpcBase",2),n([h()],_.prototype,"repositories",2),n([h()],_.prototype,"loading",2),n([h()],_.prototype,"error",2),n([h()],_.prototype,"showForm",2),n([h()],_.prototype,"editingRepo",2),_=n([x("github-rag-repository-list")],_);var A=class extends v{constructor(){super(...arguments);this.rpcBase="";this.jobs=[];this.loading=!1;this.error=""}connectedCallback(){super.connectedCallback(),this.loadJobs()}async loadJobs(){this.loading=!0;try{await this.waitForPluginAPI();let t=await this.pluginAPI.call("list_jobs",{limit:50,offset:0});t.success&&(this.jobs=t.data.jobs||[])}catch(t){this.error=String(t)}finally{this.loading=!1}}async waitForPluginAPI(){for(let t=0;t<50;t++){if(this.pluginAPI)return;await new Promise(s=>setTimeout(s,100))}throw new Error("Plugin API timeout")}getStatusClass(t){return`status-badge status-${t}`}formatDate(t){return new Date(t).toLocaleString()}handleViewJob(t){console.log("View job clicked:",t),this.selectedJobId=t,this.requestUpdate()}handleBackToList(){console.log("Back to list clicked"),this.selectedJobId=null,this.loadJobs()}render(){return this.selectedJobId?d`
        <github-rag-job-detail
          .jobId=${this.selectedJobId}
          .api=${this.pluginAPI}
          .onBack=${this.handleBackToList.bind(this)}
        ></github-rag-job-detail>
      `:d`
      <h2>Ingestion Jobs</h2>

      ${this.loading?d`<div>Loading...</div>`:this.jobs.length===0?d`
          <div class="empty-state">
            <p>No ingestion jobs yet</p>
          </div>
        `:d`
          <div class="table-container">
            <table>
              <thead>
                <tr>
                  <th>Job ID</th>
                  <th>Type</th>
                  <th>Status</th>
                  <th>Files</th>
                  <th>Chunks</th>
                  <th>Started</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                ${this.jobs.map(t=>d`
                  <tr>
                    <td><code>${t.id.substring(0,8)}</code></td>
                    <td>${t.type}</td>
                    <td><span class="${this.getStatusClass(t.status)}">${t.status}</span></td>
                    <td>${t.stats.files_scanned}</td>
                    <td>${t.stats.chunks_written}</td>
                    <td>${this.formatDate(t.started_at)}</td>
                    <td>
                      <button class="btn btn-sm" @click=${()=>this.handleViewJob(t.id)}>
                        View Logs
                      </button>
                    </td>
                  </tr>
                `)}
              </tbody>
            </table>
          </div>
        `}
    `}};A.styles=$`
    :host {
      display: block;
      padding: 24px;
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
    }

    h2 {
      margin: 0 0 24px 0;
      font-size: 24px;
      font-weight: 600;
    }

    .table-container {
      background: white;
      border: 1px solid #e0e0e0;
      border-radius: 8px;
      overflow: hidden;
    }

    table {
      width: 100%;
      border-collapse: collapse;
    }

    th {
      background: #f5f5f5;
      padding: 12px 16px;
      text-align: left;
      font-weight: 600;
      font-size: 13px;
      border-bottom: 1px solid #e0e0e0;
    }

    td {
      padding: 12px 16px;
      border-bottom: 1px solid #f5f5f5;
    }

    .status-badge {
      display: inline-block;
      padding: 4px 8px;
      border-radius: 4px;
      font-size: 12px;
      font-weight: 500;
    }

    .status-success { background: #e8f5e9; color: #2e7d32; }
    .status-running { background: #e3f2fd; color: #1976d2; }
    .status-failed { background: #ffebee; color: #c62828; }
    .status-queued { background: #fff3e0; color: #f57c00; }

    .empty-state {
      text-align: center;
      padding: 64px 24px;
      color: #666;
    }

    .btn {
      padding: 4px 12px;
      border-radius: 4px;
      border: 1px solid #ddd;
      background: white;
      cursor: pointer;
      font-size: 12px;
    }

    .btn:hover {
      background: #f5f5f5;
    }

    .btn-sm {
      padding: 4px 12px;
      font-size: 12px;
    }
  `,n([f({type:String})],A.prototype,"rpcBase",2),n([h()],A.prototype,"jobs",2),n([h()],A.prototype,"loading",2),n([h()],A.prototype,"error",2),A=n([x("github-rag-job-list")],A);var y=class extends v{constructor(){super(...arguments);this.jobId="";this.api=null;this.onBack=null;this.job=null;this.logs=[];this.loading=!1;this.error="";this.selectedLevel=""}connectedCallback(){super.connectedCallback(),this.loadJobDetails()}async loadJobDetails(){if(!(!this.api||!this.jobId)){this.loading=!0;try{let t=await this.api.call("get_job",{id:this.jobId});t.success&&(this.job=t.data);let s=await this.api.call("get_job_logs",{job_id:this.jobId,level:this.selectedLevel,limit:1e3,offset:0});s.success&&(this.logs=s.data.logs||[])}catch(t){this.error=`Error: ${t.message}`}finally{this.loading=!1}}}handleLevelFilter(t){let s=t.target;this.selectedLevel=s.value,this.loadJobDetails()}formatDate(t){return new Date(t).toLocaleString()}render(){return this.job?d`
      <div class="header">
        <h2>Job ${this.job.id.substring(0,8)}</h2>
        <button class="back-btn" @click=${()=>this.onBack&&this.onBack()}>
          ← Back to Jobs
        </button>
      </div>

      <div class="job-info">
        <strong>Repository:</strong> ${this.job.repo_id}<br>
        <strong>Type:</strong> ${this.job.type}<br>
        <strong>Status:</strong> ${this.job.status}<br>
        <strong>Started:</strong> ${this.formatDate(this.job.started_at)}<br>
        ${this.job.completed_at?d`<strong>Completed:</strong> ${this.formatDate(this.job.completed_at)}<br>`:""}

        <div class="stats-grid">
          <div class="stat">
            <div class="stat-label">Files Scanned</div>
            <div class="stat-value">${this.job.stats.files_scanned}</div>
          </div>
          <div class="stat">
            <div class="stat-label">Files Added</div>
            <div class="stat-value">${this.job.stats.files_added}</div>
          </div>
          <div class="stat">
            <div class="stat-label">Files Skipped</div>
            <div class="stat-value">${this.job.stats.files_skipped}</div>
          </div>
          <div class="stat">
            <div class="stat-label">Chunks Written</div>
            <div class="stat-value">${this.job.stats.chunks_written}</div>
          </div>
          <div class="stat">
            <div class="stat-label">Errors</div>
            <div class="stat-value">${this.job.stats.errors}</div>
          </div>
        </div>
      </div>

      <div class="logs-container">
        <div class="logs-header">
          <strong>Execution Logs</strong>
          <div class="filter">
            <label>Level:</label>
            <select @change=${this.handleLevelFilter}>
              <option value="">All</option>
              <option value="info">Info</option>
              <option value="warn">Warn</option>
              <option value="error">Error</option>
              <option value="debug">Debug</option>
            </select>
          </div>
        </div>

        <div class="logs">
          ${this.logs.length===0?d`
            <div class="empty">No logs available</div>
          `:d`
            ${this.logs.map(t=>d`
              <div class="log-entry log-${t.level}">
                <div class="log-header">
                  <span class="log-timestamp">${this.formatDate(t.timestamp)}</span>
                  <span class="log-level">${t.level}</span>
                </div>
                <div class="log-message">${t.message}</div>
                ${t.details?d`<div class="log-details">${t.details}</div>`:""}
              </div>
            `)}
          `}
        </div>
      </div>
    `:d`<div class="empty">Loading job details...</div>`}};y.styles=$`
    :host {
      display: block;
      padding: 24px;
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
    }

    .header {
      display: flex;
      justify-content: space-between;
      align-items: center;
      margin-bottom: 24px;
    }

    h2 {
      margin: 0;
      font-size: 24px;
      font-weight: 600;
    }

    .back-btn {
      background: #f5f5f5;
      color: #333;
      padding: 8px 16px;
      border: none;
      border-radius: 4px;
      cursor: pointer;
    }

    .job-info {
      background: white;
      border: 1px solid #e0e0e0;
      border-radius: 8px;
      padding: 16px;
      margin-bottom: 16px;
    }

    .stats-grid {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
      gap: 12px;
      margin-top: 12px;
    }

    .stat {
      background: #f9f9f9;
      padding: 12px;
      border-radius: 4px;
    }

    .stat-label {
      font-size: 12px;
      color: #666;
      margin-bottom: 4px;
    }

    .stat-value {
      font-size: 20px;
      font-weight: 600;
      color: #1976d2;
    }

    .logs-container {
      background: white;
      border: 1px solid #e0e0e0;
      border-radius: 8px;
      overflow: hidden;
    }

    .logs-header {
      padding: 12px 16px;
      background: #f5f5f5;
      border-bottom: 1px solid #e0e0e0;
      display: flex;
      justify-content: space-between;
      align-items: center;
    }

    .filter {
      display: flex;
      gap: 8px;
      align-items: center;
    }

    select {
      padding: 6px 12px;
      border: 1px solid #ddd;
      border-radius: 4px;
    }

    .logs {
      max-height: 600px;
      overflow-y: auto;
    }

    .log-entry {
      padding: 12px 16px;
      border-bottom: 1px solid #f5f5f5;
      font-family: 'Monaco', 'Consolas', monospace;
      font-size: 13px;
    }

    .log-entry:hover {
      background: #f9f9f9;
    }

    .log-info { border-left: 3px solid #2196f3; }
    .log-warn { border-left: 3px solid #ff9800; background: #fff3e0; }
    .log-error { border-left: 3px solid #f44336; background: #ffebee; }
    .log-debug { border-left: 3px solid #9e9e9e; }

    .log-header {
      display: flex;
      gap: 12px;
      margin-bottom: 4px;
      color: #666;
    }

    .log-timestamp {
      font-size: 11px;
    }

    .log-level {
      font-weight: 600;
      text-transform: uppercase;
      font-size: 11px;
    }

    .log-message {
      color: #333;
      margin-bottom: 4px;
    }

    .log-details {
      color: #666;
      font-size: 12px;
    }

    .empty {
      padding: 48px;
      text-align: center;
      color: #666;
    }
  `,n([f({type:String})],y.prototype,"jobId",2),n([f({type:Object})],y.prototype,"api",2),n([f({type:Function})],y.prototype,"onBack",2),n([h()],y.prototype,"job",2),n([h()],y.prototype,"logs",2),n([h()],y.prototype,"loading",2),n([h()],y.prototype,"error",2),n([h()],y.prototype,"selectedLevel",2),y=n([x("github-rag-job-detail")],y);})();
/*! Bundled license information:

@lit/reactive-element/css-tag.js:
  (**
   * @license
   * Copyright 2019 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)

@lit/reactive-element/reactive-element.js:
  (**
   * @license
   * Copyright 2017 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)

lit-html/lit-html.js:
  (**
   * @license
   * Copyright 2017 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)

lit-element/lit-element.js:
  (**
   * @license
   * Copyright 2017 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)

lit-html/is-server.js:
  (**
   * @license
   * Copyright 2022 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)

@lit/reactive-element/decorators/custom-element.js:
  (**
   * @license
   * Copyright 2017 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)

@lit/reactive-element/decorators/property.js:
  (**
   * @license
   * Copyright 2017 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)

@lit/reactive-element/decorators/state.js:
  (**
   * @license
   * Copyright 2017 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)

@lit/reactive-element/decorators/event-options.js:
  (**
   * @license
   * Copyright 2017 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)

@lit/reactive-element/decorators/base.js:
  (**
   * @license
   * Copyright 2017 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)

@lit/reactive-element/decorators/query.js:
  (**
   * @license
   * Copyright 2017 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)

@lit/reactive-element/decorators/query-all.js:
  (**
   * @license
   * Copyright 2017 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)

@lit/reactive-element/decorators/query-async.js:
  (**
   * @license
   * Copyright 2017 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)

@lit/reactive-element/decorators/query-assigned-elements.js:
  (**
   * @license
   * Copyright 2021 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)

@lit/reactive-element/decorators/query-assigned-nodes.js:
  (**
   * @license
   * Copyright 2017 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)
*/
