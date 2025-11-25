"use strict";(()=>{var kt=Object.defineProperty;var At=Object.getOwnPropertyDescriptor;var n=(r,e,t,s)=>{for(var i=s>1?void 0:s?At(e,t):e,o=r.length-1,a;o>=0;o--)(a=r[o])&&(i=(s?a(e,t,i):a(i))||i);return s&&i&&kt(e,t,i),i};var Z=globalThis,Y=Z.ShadowRoot&&(Z.ShadyCSS===void 0||Z.ShadyCSS.nativeShadow)&&"adoptedStyleSheets"in Document.prototype&&"replace"in CSSStyleSheet.prototype,G=Symbol(),dt=new WeakMap,O=class{constructor(e,t,s){if(this._$cssResult$=!0,s!==G)throw Error("CSSResult is not constructable. Use `unsafeCSS` or `css` instead.");this.cssText=e,this.t=t}get styleSheet(){let e=this.o,t=this.t;if(Y&&e===void 0){let s=t!==void 0&&t.length===1;s&&(e=dt.get(t)),e===void 0&&((this.o=e=new CSSStyleSheet).replaceSync(this.cssText),s&&dt.set(t,e))}return e}toString(){return this.cssText}},ct=r=>new O(typeof r=="string"?r:r+"",void 0,G),y=(r,...e)=>{let t=r.length===1?r[0]:e.reduce((s,i,o)=>s+(a=>{if(a._$cssResult$===!0)return a.cssText;if(typeof a=="number")return a;throw Error("Value passed to 'css' function must be a 'css' function result: "+a+". Use 'unsafeCSS' to pass non-literal values, but take care to ensure page security.")})(i)+r[o+1],r[0]);return new O(t,r,G)},pt=(r,e)=>{if(Y)r.adoptedStyleSheets=e.map(t=>t instanceof CSSStyleSheet?t:t.styleSheet);else for(let t of e){let s=document.createElement("style"),i=Z.litNonce;i!==void 0&&s.setAttribute("nonce",i),s.textContent=t.cssText,r.appendChild(s)}},tt=Y?r=>r:r=>r instanceof CSSStyleSheet?(e=>{let t="";for(let s of e.cssRules)t+=s.cssText;return ct(t)})(r):r;var{is:Et,defineProperty:Ct,getOwnPropertyDescriptor:Pt,getOwnPropertyNames:It,getOwnPropertySymbols:jt,getPrototypeOf:Dt}=Object,C=globalThis,ht=C.trustedTypes,zt=ht?ht.emptyScript:"",Tt=C.reactiveElementPolyfillSupport,N=(r,e)=>r,M={toAttribute(r,e){switch(e){case Boolean:r=r?zt:null;break;case Object:case Array:r=r==null?r:JSON.stringify(r)}return r},fromAttribute(r,e){let t=r;switch(e){case Boolean:t=r!==null;break;case Number:t=r===null?null:Number(r);break;case Object:case Array:try{t=JSON.parse(r)}catch{t=null}}return t}},Q=(r,e)=>!Et(r,e),ut={attribute:!0,type:String,converter:M,reflect:!1,useDefault:!1,hasChanged:Q};Symbol.metadata??(Symbol.metadata=Symbol("metadata")),C.litPropertyMetadata??(C.litPropertyMetadata=new WeakMap);var k=class extends HTMLElement{static addInitializer(e){this._$Ei(),(this.l??(this.l=[])).push(e)}static get observedAttributes(){return this.finalize(),this._$Eh&&[...this._$Eh.keys()]}static createProperty(e,t=ut){if(t.state&&(t.attribute=!1),this._$Ei(),this.prototype.hasOwnProperty(e)&&((t=Object.create(t)).wrapped=!0),this.elementProperties.set(e,t),!t.noAccessor){let s=Symbol(),i=this.getPropertyDescriptor(e,s,t);i!==void 0&&Ct(this.prototype,e,i)}}static getPropertyDescriptor(e,t,s){let{get:i,set:o}=Pt(this.prototype,e)??{get(){return this[t]},set(a){this[t]=a}};return{get:i,set(a){let p=i?.call(this);o?.call(this,a),this.requestUpdate(e,p,s)},configurable:!0,enumerable:!0}}static getPropertyOptions(e){return this.elementProperties.get(e)??ut}static _$Ei(){if(this.hasOwnProperty(N("elementProperties")))return;let e=Dt(this);e.finalize(),e.l!==void 0&&(this.l=[...e.l]),this.elementProperties=new Map(e.elementProperties)}static finalize(){if(this.hasOwnProperty(N("finalized")))return;if(this.finalized=!0,this._$Ei(),this.hasOwnProperty(N("properties"))){let t=this.properties,s=[...It(t),...jt(t)];for(let i of s)this.createProperty(i,t[i])}let e=this[Symbol.metadata];if(e!==null){let t=litPropertyMetadata.get(e);if(t!==void 0)for(let[s,i]of t)this.elementProperties.set(s,i)}this._$Eh=new Map;for(let[t,s]of this.elementProperties){let i=this._$Eu(t,s);i!==void 0&&this._$Eh.set(i,t)}this.elementStyles=this.finalizeStyles(this.styles)}static finalizeStyles(e){let t=[];if(Array.isArray(e)){let s=new Set(e.flat(1/0).reverse());for(let i of s)t.unshift(tt(i))}else e!==void 0&&t.push(tt(e));return t}static _$Eu(e,t){let s=t.attribute;return s===!1?void 0:typeof s=="string"?s:typeof e=="string"?e.toLowerCase():void 0}constructor(){super(),this._$Ep=void 0,this.isUpdatePending=!1,this.hasUpdated=!1,this._$Em=null,this._$Ev()}_$Ev(){this._$ES=new Promise(e=>this.enableUpdating=e),this._$AL=new Map,this._$E_(),this.requestUpdate(),this.constructor.l?.forEach(e=>e(this))}addController(e){(this._$EO??(this._$EO=new Set)).add(e),this.renderRoot!==void 0&&this.isConnected&&e.hostConnected?.()}removeController(e){this._$EO?.delete(e)}_$E_(){let e=new Map,t=this.constructor.elementProperties;for(let s of t.keys())this.hasOwnProperty(s)&&(e.set(s,this[s]),delete this[s]);e.size>0&&(this._$Ep=e)}createRenderRoot(){let e=this.shadowRoot??this.attachShadow(this.constructor.shadowRootOptions);return pt(e,this.constructor.elementStyles),e}connectedCallback(){this.renderRoot??(this.renderRoot=this.createRenderRoot()),this.enableUpdating(!0),this._$EO?.forEach(e=>e.hostConnected?.())}enableUpdating(e){}disconnectedCallback(){this._$EO?.forEach(e=>e.hostDisconnected?.())}attributeChangedCallback(e,t,s){this._$AK(e,s)}_$ET(e,t){let s=this.constructor.elementProperties.get(e),i=this.constructor._$Eu(e,s);if(i!==void 0&&s.reflect===!0){let o=(s.converter?.toAttribute!==void 0?s.converter:M).toAttribute(t,s.type);this._$Em=e,o==null?this.removeAttribute(i):this.setAttribute(i,o),this._$Em=null}}_$AK(e,t){let s=this.constructor,i=s._$Eh.get(e);if(i!==void 0&&this._$Em!==i){let o=s.getPropertyOptions(i),a=typeof o.converter=="function"?{fromAttribute:o.converter}:o.converter?.fromAttribute!==void 0?o.converter:M;this._$Em=i;let p=a.fromAttribute(t,o.type);this[i]=p??this._$Ej?.get(i)??p,this._$Em=null}}requestUpdate(e,t,s){if(e!==void 0){let i=this.constructor,o=this[e];if(s??(s=i.getPropertyOptions(e)),!((s.hasChanged??Q)(o,t)||s.useDefault&&s.reflect&&o===this._$Ej?.get(e)&&!this.hasAttribute(i._$Eu(e,s))))return;this.C(e,t,s)}this.isUpdatePending===!1&&(this._$ES=this._$EP())}C(e,t,{useDefault:s,reflect:i,wrapped:o},a){s&&!(this._$Ej??(this._$Ej=new Map)).has(e)&&(this._$Ej.set(e,a??t??this[e]),o!==!0||a!==void 0)||(this._$AL.has(e)||(this.hasUpdated||s||(t=void 0),this._$AL.set(e,t)),i===!0&&this._$Em!==e&&(this._$Eq??(this._$Eq=new Set)).add(e))}async _$EP(){this.isUpdatePending=!0;try{await this._$ES}catch(t){Promise.reject(t)}let e=this.scheduleUpdate();return e!=null&&await e,!this.isUpdatePending}scheduleUpdate(){return this.performUpdate()}performUpdate(){if(!this.isUpdatePending)return;if(!this.hasUpdated){if(this.renderRoot??(this.renderRoot=this.createRenderRoot()),this._$Ep){for(let[i,o]of this._$Ep)this[i]=o;this._$Ep=void 0}let s=this.constructor.elementProperties;if(s.size>0)for(let[i,o]of s){let{wrapped:a}=o,p=this[i];a!==!0||this._$AL.has(i)||p===void 0||this.C(i,void 0,o,p)}}let e=!1,t=this._$AL;try{e=this.shouldUpdate(t),e?(this.willUpdate(t),this._$EO?.forEach(s=>s.hostUpdate?.()),this.update(t)):this._$EM()}catch(s){throw e=!1,this._$EM(),s}e&&this._$AE(t)}willUpdate(e){}_$AE(e){this._$EO?.forEach(t=>t.hostUpdated?.()),this.hasUpdated||(this.hasUpdated=!0,this.firstUpdated(e)),this.updated(e)}_$EM(){this._$AL=new Map,this.isUpdatePending=!1}get updateComplete(){return this.getUpdateComplete()}getUpdateComplete(){return this._$ES}shouldUpdate(e){return!0}update(e){this._$Eq&&(this._$Eq=this._$Eq.forEach(t=>this._$ET(t,this[t]))),this._$EM()}updated(e){}firstUpdated(e){}};k.elementStyles=[],k.shadowRootOptions={mode:"open"},k[N("elementProperties")]=new Map,k[N("finalized")]=new Map,Tt?.({ReactiveElement:k}),(C.reactiveElementVersions??(C.reactiveElementVersions=[])).push("2.1.1");var L=globalThis,X=L.trustedTypes,gt=X?X.createPolicy("lit-html",{createHTML:r=>r}):void 0,$t="$lit$",P=`lit$${Math.random().toFixed(9).slice(2)}$`,xt="?"+P,Ut=`<${xt}>`,z=document,B=()=>z.createComment(""),R=r=>r===null||typeof r!="object"&&typeof r!="function",nt=Array.isArray,Ft=r=>nt(r)||typeof r?.[Symbol.iterator]=="function",et=`[ 	
\f\r]`,q=/<(?:(!--|\/[^a-zA-Z])|(\/?[a-zA-Z][^>\s]*)|(\/?$))/g,ft=/-->/g,mt=/>/g,j=RegExp(`>|${et}(?:([^\\s"'>=/]+)(${et}*=${et}*(?:[^ 	
\f\r"'\`<>=]|("|')|))|$)`,"g"),bt=/'/g,vt=/"/g,_t=/^(?:script|style|textarea|title)$/i,lt=r=>(e,...t)=>({_$litType$:r,strings:e,values:t}),d=lt(1),Kt=lt(2),Zt=lt(3),T=Symbol.for("lit-noChange"),f=Symbol.for("lit-nothing"),yt=new WeakMap,D=z.createTreeWalker(z,129);function wt(r,e){if(!nt(r)||!r.hasOwnProperty("raw"))throw Error("invalid template strings array");return gt!==void 0?gt.createHTML(e):e}var Ot=(r,e)=>{let t=r.length-1,s=[],i,o=e===2?"<svg>":e===3?"<math>":"",a=q;for(let p=0;p<t;p++){let l=r[p],g,b,h=-1,S=0;for(;S<l.length&&(a.lastIndex=S,b=a.exec(l),b!==null);)S=a.lastIndex,a===q?b[1]==="!--"?a=ft:b[1]!==void 0?a=mt:b[2]!==void 0?(_t.test(b[2])&&(i=RegExp("</"+b[2],"g")),a=j):b[3]!==void 0&&(a=j):a===j?b[0]===">"?(a=i??q,h=-1):b[1]===void 0?h=-2:(h=a.lastIndex-b[2].length,g=b[1],a=b[3]===void 0?j:b[3]==='"'?vt:bt):a===vt||a===bt?a=j:a===ft||a===mt?a=q:(a=j,i=void 0);let E=a===j&&r[p+1].startsWith("/>")?" ":"";o+=a===q?l+Ut:h>=0?(s.push(g),l.slice(0,h)+$t+l.slice(h)+P+E):l+P+(h===-2?p:E)}return[wt(r,o+(r[t]||"<?>")+(e===2?"</svg>":e===3?"</math>":"")),s]},V=class r{constructor({strings:e,_$litType$:t},s){let i;this.parts=[];let o=0,a=0,p=e.length-1,l=this.parts,[g,b]=Ot(e,t);if(this.el=r.createElement(g,s),D.currentNode=this.el.content,t===2||t===3){let h=this.el.content.firstChild;h.replaceWith(...h.childNodes)}for(;(i=D.nextNode())!==null&&l.length<p;){if(i.nodeType===1){if(i.hasAttributes())for(let h of i.getAttributeNames())if(h.endsWith($t)){let S=b[a++],E=i.getAttribute(h).split(P),K=/([.?@])?(.*)/.exec(S);l.push({type:1,index:o,name:K[2],strings:E,ctor:K[1]==="."?it:K[1]==="?"?rt:K[1]==="@"?ot:F}),i.removeAttribute(h)}else h.startsWith(P)&&(l.push({type:6,index:o}),i.removeAttribute(h));if(_t.test(i.tagName)){let h=i.textContent.split(P),S=h.length-1;if(S>0){i.textContent=X?X.emptyScript:"";for(let E=0;E<S;E++)i.append(h[E],B()),D.nextNode(),l.push({type:2,index:++o});i.append(h[S],B())}}}else if(i.nodeType===8)if(i.data===xt)l.push({type:2,index:o});else{let h=-1;for(;(h=i.data.indexOf(P,h+1))!==-1;)l.push({type:7,index:o}),h+=P.length-1}o++}}static createElement(e,t){let s=z.createElement("template");return s.innerHTML=e,s}};function U(r,e,t=r,s){if(e===T)return e;let i=s!==void 0?t._$Co?.[s]:t._$Cl,o=R(e)?void 0:e._$litDirective$;return i?.constructor!==o&&(i?._$AO?.(!1),o===void 0?i=void 0:(i=new o(r),i._$AT(r,t,s)),s!==void 0?(t._$Co??(t._$Co=[]))[s]=i:t._$Cl=i),i!==void 0&&(e=U(r,i._$AS(r,e.values),i,s)),e}var st=class{constructor(e,t){this._$AV=[],this._$AN=void 0,this._$AD=e,this._$AM=t}get parentNode(){return this._$AM.parentNode}get _$AU(){return this._$AM._$AU}u(e){let{el:{content:t},parts:s}=this._$AD,i=(e?.creationScope??z).importNode(t,!0);D.currentNode=i;let o=D.nextNode(),a=0,p=0,l=s[0];for(;l!==void 0;){if(a===l.index){let g;l.type===2?g=new W(o,o.nextSibling,this,e):l.type===1?g=new l.ctor(o,l.name,l.strings,this,e):l.type===6&&(g=new at(o,this,e)),this._$AV.push(g),l=s[++p]}a!==l?.index&&(o=D.nextNode(),a++)}return D.currentNode=z,i}p(e){let t=0;for(let s of this._$AV)s!==void 0&&(s.strings!==void 0?(s._$AI(e,s,t),t+=s.strings.length-2):s._$AI(e[t])),t++}},W=class r{get _$AU(){return this._$AM?._$AU??this._$Cv}constructor(e,t,s,i){this.type=2,this._$AH=f,this._$AN=void 0,this._$AA=e,this._$AB=t,this._$AM=s,this.options=i,this._$Cv=i?.isConnected??!0}get parentNode(){let e=this._$AA.parentNode,t=this._$AM;return t!==void 0&&e?.nodeType===11&&(e=t.parentNode),e}get startNode(){return this._$AA}get endNode(){return this._$AB}_$AI(e,t=this){e=U(this,e,t),R(e)?e===f||e==null||e===""?(this._$AH!==f&&this._$AR(),this._$AH=f):e!==this._$AH&&e!==T&&this._(e):e._$litType$!==void 0?this.$(e):e.nodeType!==void 0?this.T(e):Ft(e)?this.k(e):this._(e)}O(e){return this._$AA.parentNode.insertBefore(e,this._$AB)}T(e){this._$AH!==e&&(this._$AR(),this._$AH=this.O(e))}_(e){this._$AH!==f&&R(this._$AH)?this._$AA.nextSibling.data=e:this.T(z.createTextNode(e)),this._$AH=e}$(e){let{values:t,_$litType$:s}=e,i=typeof s=="number"?this._$AC(e):(s.el===void 0&&(s.el=V.createElement(wt(s.h,s.h[0]),this.options)),s);if(this._$AH?._$AD===i)this._$AH.p(t);else{let o=new st(i,this),a=o.u(this.options);o.p(t),this.T(a),this._$AH=o}}_$AC(e){let t=yt.get(e.strings);return t===void 0&&yt.set(e.strings,t=new V(e)),t}k(e){nt(this._$AH)||(this._$AH=[],this._$AR());let t=this._$AH,s,i=0;for(let o of e)i===t.length?t.push(s=new r(this.O(B()),this.O(B()),this,this.options)):s=t[i],s._$AI(o),i++;i<t.length&&(this._$AR(s&&s._$AB.nextSibling,i),t.length=i)}_$AR(e=this._$AA.nextSibling,t){for(this._$AP?.(!1,!0,t);e!==this._$AB;){let s=e.nextSibling;e.remove(),e=s}}setConnected(e){this._$AM===void 0&&(this._$Cv=e,this._$AP?.(e))}},F=class{get tagName(){return this.element.tagName}get _$AU(){return this._$AM._$AU}constructor(e,t,s,i,o){this.type=1,this._$AH=f,this._$AN=void 0,this.element=e,this.name=t,this._$AM=i,this.options=o,s.length>2||s[0]!==""||s[1]!==""?(this._$AH=Array(s.length-1).fill(new String),this.strings=s):this._$AH=f}_$AI(e,t=this,s,i){let o=this.strings,a=!1;if(o===void 0)e=U(this,e,t,0),a=!R(e)||e!==this._$AH&&e!==T,a&&(this._$AH=e);else{let p=e,l,g;for(e=o[0],l=0;l<o.length-1;l++)g=U(this,p[s+l],t,l),g===T&&(g=this._$AH[l]),a||(a=!R(g)||g!==this._$AH[l]),g===f?e=f:e!==f&&(e+=(g??"")+o[l+1]),this._$AH[l]=g}a&&!i&&this.j(e)}j(e){e===f?this.element.removeAttribute(this.name):this.element.setAttribute(this.name,e??"")}},it=class extends F{constructor(){super(...arguments),this.type=3}j(e){this.element[this.name]=e===f?void 0:e}},rt=class extends F{constructor(){super(...arguments),this.type=4}j(e){this.element.toggleAttribute(this.name,!!e&&e!==f)}},ot=class extends F{constructor(e,t,s,i,o){super(e,t,s,i,o),this.type=5}_$AI(e,t=this){if((e=U(this,e,t,0)??f)===T)return;let s=this._$AH,i=e===f&&s!==f||e.capture!==s.capture||e.once!==s.once||e.passive!==s.passive,o=e!==f&&(s===f||i);i&&this.element.removeEventListener(this.name,this,s),o&&this.element.addEventListener(this.name,this,e),this._$AH=e}handleEvent(e){typeof this._$AH=="function"?this._$AH.call(this.options?.host??this.element,e):this._$AH.handleEvent(e)}},at=class{constructor(e,t,s){this.element=e,this.type=6,this._$AN=void 0,this._$AM=t,this.options=s}get _$AU(){return this._$AM._$AU}_$AI(e){U(this,e)}};var Nt=L.litHtmlPolyfillSupport;Nt?.(V,W),(L.litHtmlVersions??(L.litHtmlVersions=[])).push("3.3.1");var St=(r,e,t)=>{let s=t?.renderBefore??e,i=s._$litPart$;if(i===void 0){let o=t?.renderBefore??null;s._$litPart$=i=new W(e.insertBefore(B(),o),o,void 0,t??{})}return i._$AI(r),i};var J=globalThis,m=class extends k{constructor(){super(...arguments),this.renderOptions={host:this},this._$Do=void 0}createRenderRoot(){var t;let e=super.createRenderRoot();return(t=this.renderOptions).renderBefore??(t.renderBefore=e.firstChild),e}update(e){let t=this.render();this.hasUpdated||(this.renderOptions.isConnected=this.isConnected),super.update(e),this._$Do=St(t,this.renderRoot,this.renderOptions)}connectedCallback(){super.connectedCallback(),this._$Do?.setConnected(!0)}disconnectedCallback(){super.disconnectedCallback(),this._$Do?.setConnected(!1)}render(){return T}};m._$litElement$=!0,m.finalized=!0,J.litElementHydrateSupport?.({LitElement:m});var Mt=J.litElementPolyfillSupport;Mt?.({LitElement:m});(J.litElementVersions??(J.litElementVersions=[])).push("4.2.1");var _=r=>(e,t)=>{t!==void 0?t.addInitializer(()=>{customElements.define(r,e)}):customElements.define(r,e)};var qt={attribute:!0,type:String,converter:M,reflect:!1,hasChanged:Q},Lt=(r=qt,e,t)=>{let{kind:s,metadata:i}=t,o=globalThis.litPropertyMetadata.get(i);if(o===void 0&&globalThis.litPropertyMetadata.set(i,o=new Map),s==="setter"&&((r=Object.create(r)).wrapped=!0),o.set(t.name,r),s==="accessor"){let{name:a}=t;return{set(p){let l=e.get.call(this);e.set.call(this,p),this.requestUpdate(a,l,r)},init(p){return p!==void 0&&this.C(a,void 0,r,p),p}}}if(s==="setter"){let{name:a}=t;return function(p){let l=this[a];e.call(this,p),this.requestUpdate(a,l,r)}}throw Error("Unsupported decorator location: "+s)};function u(r){return(e,t)=>typeof t=="object"?Lt(r,e,t):((s,i,o)=>{let a=i.hasOwnProperty(o);return i.constructor.createProperty(o,s),a?Object.getOwnPropertyDescriptor(i,o):void 0})(r,e,t)}function c(r){return u({...r,state:!0,attribute:!1})}var I=class extends m{constructor(){super(...arguments);this.rpcBase="";this.stats=null;this.loading=!1}connectedCallback(){super.connectedCallback(),this.loadStats()}async loadStats(){try{await this.waitForPluginAPI();let t=await this.pluginAPI.call("get_statistics",{});t.success&&(this.stats={totalRepos:t.data.total_repos||0,activeRepos:t.data.active_repos||0,totalJobs:t.data.total_jobs||0,chunksIngested:t.data.chunks_ingested||0})}catch(t){console.error("Failed to load stats:",t),this.stats={totalRepos:0,activeRepos:0,totalJobs:0,chunksIngested:0}}}async waitForPluginAPI(){for(let t=0;t<50;t++){if(this.pluginAPI)return;await new Promise(s=>setTimeout(s,100))}throw new Error("Plugin API timeout")}render(){return d`
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
    `}};I.styles=y`
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
  `,n([u({type:String})],I.prototype,"rpcBase",2),n([c()],I.prototype,"stats",2),n([c()],I.prototype,"loading",2),I=n([_("github-rag-dashboard")],I);var v=class extends m{constructor(){super(...arguments);this.repository=null;this.api=null;this.onSave=null;this.onCancel=null;this.datasources=[];this.showCloneModal=!1;this.selectedDatasourceForClone=null;this.formData={name:"",owner:"",url:"",branch:"main",auth_type:"public",pat_token:"",ssh_private_key:"",ssh_passphrase:"",datasource_id:0,target_paths:[],file_masks:["*"],ignore_patterns:[],chunking_strategy:"hybrid",chunk_size:1e3,chunk_overlap:200,sync_schedule:"",sync_enabled:!1};this.loading=!1;this.error=""}async connectedCallback(){super.connectedCallback(),await this.loadDatasources(),this.repository&&(this.formData={...this.repository},this.requestUpdate())}async loadDatasources(){if(!this.api){console.error("No API available to load datasources");return}try{let t=await this.api.call("list_datasources",{});t.success?(this.datasources=t.data.datasources||[],console.log("Loaded datasources:",this.datasources)):console.error("Failed to load datasources:",t.error)}catch(t){console.error("Failed to load datasources:",t)}}handleCloneClick(){let t=this.formData.datasource_id;if(!t||t===0){alert("Please select a datasource first");return}let s=this.datasources.find(i=>i.id===t);s&&(this.selectedDatasourceForClone=s,this.showCloneModal=!0)}handleCloneSuccess(t){this.loadDatasources().then(()=>{this.updateField("datasource_id",t)})}handleCloseModal(){this.showCloneModal=!1,this.selectedDatasourceForClone=null}handleSubmit(t){t.preventDefault(),this.saveRepository()}async saveRepository(){if(!this.api){this.error="No API available";return}this.loading=!0,this.error="";try{let t=this.repository?"update_repository":"create_repository",s=this.repository?{...this.formData,id:this.repository.id,sync_enabled:!!this.formData.sync_schedule}:{...this.formData,sync_enabled:!!this.formData.sync_schedule},i=await this.api.call(t,s);i.success?this.onSave&&this.onSave(i.data):this.error=i.error||"Failed to save repository"}catch(t){this.error=`Error: ${t.message}`}finally{this.loading=!1}}handleCancel(){this.onCancel&&this.onCancel()}updateField(t,s){this.formData={...this.formData,[t]:s}}updateArrayField(t,s){let i=s.split(",").map(o=>o.trim()).filter(o=>o);this.formData={...this.formData,[t]:i}}render(){return d`
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
              <div style="display: flex; gap: 8px; align-items: start;">
                <select
                  required
                  style="flex: 1;"
                  .value=${String(this.formData.datasource_id)}
                  @change=${t=>this.updateField("datasource_id",parseInt(t.target.value))}
                >
                  <option value="0">Select datasource...</option>
                  ${this.datasources.map(t=>d`
                    <option value="${t.id}">${t.name} - ${t.db_source_type||"Unknown"}</option>
                  `)}
                </select>
                <button
                  type="button"
                  class="btn-clone"
                  @click=${this.handleCloneClick}
                  title="Clone selected datasource with different namespace"
                >
                  Clone
                </button>
              </div>
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
              <div class="hint">Leave empty for manual sync only. When a schedule is provided, automatic sync is enabled automatically.</div>
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

      ${this.showCloneModal&&this.selectedDatasourceForClone?d`
        <datasource-clone-modal
          .sourceDatasource=${this.selectedDatasourceForClone}
          .api=${this.api}
          .onClose=${this.handleCloseModal.bind(this)}
          .onSuccess=${this.handleCloneSuccess.bind(this)}
        ></datasource-clone-modal>
      `:""}
    `}};v.styles=y`
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

    .btn-clone {
      padding: 8px 16px;
      background: #f5f5f5;
      color: #333;
      border: 1px solid #ddd;
      border-radius: 4px;
      cursor: pointer;
      font-size: 14px;
      white-space: nowrap;
      transition: background 0.2s;
    }

    .btn-clone:hover {
      background: #e0e0e0;
    }

    .btn-clone:disabled {
      opacity: 0.5;
      cursor: not-allowed;
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
  `,n([u({type:Object})],v.prototype,"repository",2),n([u({type:Object})],v.prototype,"api",2),n([u({type:Function})],v.prototype,"onSave",2),n([u({type:Function})],v.prototype,"onCancel",2),n([c()],v.prototype,"datasources",2),n([c()],v.prototype,"showCloneModal",2),n([c()],v.prototype,"selectedDatasourceForClone",2),n([c()],v.prototype,"formData",2),n([c()],v.prototype,"loading",2),n([c()],v.prototype,"error",2),v=n([_("github-rag-repository-form")],v);var w=class extends m{constructor(){super(...arguments);this.rpcBase="";this.repositories=[];this.loading=!1;this.error="";this.showForm=!1;this.editingRepo=null}connectedCallback(){super.connectedCallback(),this.loadRepositories()}async loadRepositories(){this.loading=!0,this.error="";try{await this.waitForPluginAPI();let t=await this.pluginAPI.call("list_repositories",{});t.success?this.repositories=t.data.repositories||[]:this.error=t.error||"Failed to load repositories"}catch(t){this.error=`Error: ${t.message}`}finally{this.loading=!1}}async waitForPluginAPI(){for(let t=0;t<50;t++){if(this.pluginAPI)return;await new Promise(s=>setTimeout(s,100))}throw new Error("Plugin API timeout")}handleAddRepository(){this.showForm=!0,this.editingRepo=null}handleEditRepository(t){this.showForm=!0,this.editingRepo=t}async handleRunIngestion(t,s="incremental"){try{let i=await this.pluginAPI.call("trigger_ingestion",{repo_id:t,type:s,dry_run:!1});i.success?alert(`${s==="full"?"Full":"Incremental"} ingestion started! Job ID: ${i.data.job_id}`):alert(`Error: ${i.error}`)}catch(i){alert(`Error: ${i.message}`)}}async handleDeleteRepository(t){if(confirm(`Delete repository "${t.owner}/${t.name}"? This will also delete all ingested chunks.`))try{let s=await this.pluginAPI.call("delete_repository",{id:t.id,delete_chunks:!0});s.success?this.loadRepositories():alert(`Error: ${s.error}`)}catch(s){alert(`Error: ${s.message}`)}}handleFormSave(t){this.showForm=!1,this.editingRepo=null,this.loadRepositories()}handleFormCancel(){this.showForm=!1,this.editingRepo=null}formatDate(t){return t?new Date(t).toLocaleString():"Never"}render(){return this.showForm?d`
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
    `}};w.styles=y`
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
  `,n([u({type:String})],w.prototype,"rpcBase",2),n([c()],w.prototype,"repositories",2),n([c()],w.prototype,"loading",2),n([c()],w.prototype,"error",2),n([c()],w.prototype,"showForm",2),n([c()],w.prototype,"editingRepo",2),w=n([_("github-rag-repository-list")],w);var A=class extends m{constructor(){super(...arguments);this.rpcBase="";this.jobs=[];this.loading=!1;this.error=""}connectedCallback(){super.connectedCallback(),this.loadJobs()}async loadJobs(){this.loading=!0;try{await this.waitForPluginAPI();let t=await this.pluginAPI.call("list_jobs",{limit:50,offset:0});t.success&&(this.jobs=t.data.jobs||[])}catch(t){this.error=String(t)}finally{this.loading=!1}}async waitForPluginAPI(){for(let t=0;t<50;t++){if(this.pluginAPI)return;await new Promise(s=>setTimeout(s,100))}throw new Error("Plugin API timeout")}getStatusClass(t){return`status-badge status-${t}`}formatDate(t){return new Date(t).toLocaleString()}handleViewJob(t){console.log("View job clicked:",t),this.selectedJobId=t,this.requestUpdate()}handleBackToList(){console.log("Back to list clicked"),this.selectedJobId=null,this.loadJobs()}render(){return this.selectedJobId?d`
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
    `}};A.styles=y`
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
  `,n([u({type:String})],A.prototype,"rpcBase",2),n([c()],A.prototype,"jobs",2),n([c()],A.prototype,"loading",2),n([c()],A.prototype,"error",2),A=n([_("github-rag-job-list")],A);var $=class extends m{constructor(){super(...arguments);this.jobId="";this.api=null;this.onBack=null;this.job=null;this.logs=[];this.loading=!1;this.error="";this.selectedLevel=""}connectedCallback(){super.connectedCallback(),this.loadJobDetails()}async loadJobDetails(){if(!(!this.api||!this.jobId)){this.loading=!0;try{let t=await this.api.call("get_job",{id:this.jobId});t.success&&(this.job=t.data);let s=await this.api.call("get_job_logs",{job_id:this.jobId,level:this.selectedLevel,limit:1e3,offset:0});s.success&&(this.logs=s.data.logs||[])}catch(t){this.error=`Error: ${t.message}`}finally{this.loading=!1}}}handleLevelFilter(t){let s=t.target;this.selectedLevel=s.value,this.loadJobDetails()}formatDate(t){return new Date(t).toLocaleString()}render(){return this.job?d`
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
    `:d`<div class="empty">Loading job details...</div>`}};$.styles=y`
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
  `,n([u({type:String})],$.prototype,"jobId",2),n([u({type:Object})],$.prototype,"api",2),n([u({type:Function})],$.prototype,"onBack",2),n([c()],$.prototype,"job",2),n([c()],$.prototype,"logs",2),n([c()],$.prototype,"loading",2),n([c()],$.prototype,"error",2),n([c()],$.prototype,"selectedLevel",2),$=n([_("github-rag-job-detail")],$);var x=class extends m{constructor(){super(...arguments);this.sourceDatasource=null;this.api=null;this.onClose=null;this.onSuccess=null;this.name="";this.namespace="";this.loading=!1;this.error=""}connectedCallback(){super.connectedCallback(),this.sourceDatasource&&(this.name=`Copy of ${this.sourceDatasource.name}`,this.namespace=this.sourceDatasource.db_name||"")}async handleSubmit(t){if(t.preventDefault(),!this.api||!this.sourceDatasource){this.error="API not available";return}if(!this.name.trim()){this.error="Name is required";return}if(!this.namespace.trim()){this.error="Namespace is required";return}this.loading=!0,this.error="";try{let s=await this.api.call("clone_datasource",{source_datasource_id:this.sourceDatasource.id});if(!s.success){this.error=s.error||"Failed to clone datasource",this.loading=!1;return}let i=s.data.datasource_id,o=await this.api.call("update_datasource_fields",{datasource_id:i,name:this.name.trim(),db_name:this.namespace.trim()});o.success?(this.onSuccess&&this.onSuccess(i),this.onClose&&this.onClose()):this.error=o.error||"Failed to update datasource fields"}catch(s){this.error=`Error: ${s.message}`}finally{this.loading=!1}}handleCancel(){this.onClose&&this.onClose()}render(){return this.sourceDatasource?d`
      <div class="modal-overlay" @click=${this.handleCancel}>
        <div class="modal-content" @click=${t=>t.stopPropagation()}>
          <h2 class="modal-header">Clone Datasource</h2>

          <div class="source-info">
            <strong>Cloning from:</strong>
            ${this.sourceDatasource.name}
            <div class="hint">
              All configuration including API keys will be copied
            </div>
          </div>

          ${this.error?d`<div class="error">${this.error}</div>`:""}

          <form @submit=${this.handleSubmit}>
            <div class="form-group">
              <label for="name">Datasource Name *</label>
              <input
                type="text"
                id="name"
                .value=${this.name}
                @input=${t=>this.name=t.target.value}
                required
                ?disabled=${this.loading}
              />
            </div>

            <div class="form-group">
              <label for="namespace">Namespace / Collection Name *</label>
              <input
                type="text"
                id="namespace"
                .value=${this.namespace}
                @input=${t=>this.namespace=t.target.value}
                required
                ?disabled=${this.loading}
              />
              <div class="hint">
                Vector store collection/namespace (db_name field)
              </div>
            </div>

            <div class="button-group">
              <button
                type="button"
                class="btn-cancel"
                @click=${this.handleCancel}
                ?disabled=${this.loading}
              >
                Cancel
              </button>
              <button
                type="submit"
                class="btn-primary"
                ?disabled=${this.loading}
              >
                ${this.loading?"Creating...":"Create Datasource"}
              </button>
            </div>
          </form>
        </div>
      </div>
    `:d``}};x.styles=y`
    .modal-overlay {
      position: fixed;
      top: 0;
      left: 0;
      right: 0;
      bottom: 0;
      background: rgba(0, 0, 0, 0.5);
      display: flex;
      align-items: center;
      justify-content: center;
      z-index: 1000;
    }

    .modal-content {
      background: white;
      border-radius: 8px;
      padding: 24px;
      width: 90%;
      max-width: 500px;
      box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
    }

    .modal-header {
      margin: 0 0 20px 0;
      font-size: 20px;
      font-weight: 600;
    }

    .form-group {
      margin-bottom: 16px;
    }

    label {
      display: block;
      margin-bottom: 6px;
      font-weight: 500;
      font-size: 14px;
    }

    input {
      width: 100%;
      padding: 8px 12px;
      border: 1px solid #ddd;
      border-radius: 4px;
      font-size: 14px;
      box-sizing: border-box;
    }

    input:focus {
      outline: none;
      border-color: #1976d2;
    }

    .hint {
      font-size: 12px;
      color: #666;
      margin-top: 4px;
    }

    .error {
      background: #ffebee;
      color: #c62828;
      padding: 12px;
      border-radius: 4px;
      margin-bottom: 16px;
      font-size: 14px;
    }

    .button-group {
      display: flex;
      gap: 12px;
      justify-content: flex-end;
      margin-top: 24px;
    }

    button {
      padding: 8px 16px;
      border-radius: 4px;
      font-size: 14px;
      cursor: pointer;
      border: none;
      transition: background 0.2s;
    }

    .btn-cancel {
      background: #f5f5f5;
      color: #333;
    }

    .btn-cancel:hover {
      background: #e0e0e0;
    }

    .btn-primary {
      background: #1976d2;
      color: white;
    }

    .btn-primary:hover {
      background: #1565c0;
    }

    .btn-primary:disabled {
      background: #ccc;
      cursor: not-allowed;
    }

    .source-info {
      background: #f5f5f5;
      padding: 12px;
      border-radius: 4px;
      margin-bottom: 16px;
      font-size: 13px;
    }

    .source-info strong {
      display: block;
      margin-bottom: 4px;
    }
  `,n([u({type:Object})],x.prototype,"sourceDatasource",2),n([u({type:Object})],x.prototype,"api",2),n([u({type:Function})],x.prototype,"onClose",2),n([u({type:Function})],x.prototype,"onSuccess",2),n([c()],x.prototype,"name",2),n([c()],x.prototype,"namespace",2),n([c()],x.prototype,"loading",2),n([c()],x.prototype,"error",2),x=n([_("datasource-clone-modal")],x);})();
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
