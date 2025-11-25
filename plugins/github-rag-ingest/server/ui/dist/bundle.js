var xt=Object.defineProperty;var At=Object.getOwnPropertyDescriptor;var p=(r,t,e,s)=>{for(var i=s>1?void 0:s?At(t,e):t,o=r.length-1,n;o>=0;o--)(n=r[o])&&(i=(s?n(t,e,i):n(i))||i);return s&&i&&xt(t,e,i),i};var V=globalThis,W=V.ShadowRoot&&(V.ShadyCSS===void 0||V.ShadyCSS.nativeShadow)&&"adoptedStyleSheets"in Document.prototype&&"replace"in CSSStyleSheet.prototype,Q=Symbol(),nt=new WeakMap,M=class{constructor(t,e,s){if(this._$cssResult$=!0,s!==Q)throw Error("CSSResult is not constructable. Use `unsafeCSS` or `css` instead.");this.cssText=t,this.t=e}get styleSheet(){let t=this.o,e=this.t;if(W&&t===void 0){let s=e!==void 0&&e.length===1;s&&(t=nt.get(e)),t===void 0&&((this.o=t=new CSSStyleSheet).replaceSync(this.cssText),s&&nt.set(e,t))}return t}toString(){return this.cssText}},at=r=>new M(typeof r=="string"?r:r+"",void 0,Q),E=(r,...t)=>{let e=r.length===1?r[0]:t.reduce((s,i,o)=>s+(n=>{if(n._$cssResult$===!0)return n.cssText;if(typeof n=="number")return n;throw Error("Value passed to 'css' function must be a 'css' function result: "+n+". Use 'unsafeCSS' to pass non-literal values, but take care to ensure page security.")})(i)+r[o+1],r[0]);return new M(e,r,Q)},lt=(r,t)=>{if(W)r.adoptedStyleSheets=t.map(e=>e instanceof CSSStyleSheet?e:e.styleSheet);else for(let e of t){let s=document.createElement("style"),i=V.litNonce;i!==void 0&&s.setAttribute("nonce",i),s.textContent=e.cssText,r.appendChild(s)}},X=W?r=>r:r=>r instanceof CSSStyleSheet?(t=>{let e="";for(let s of t.cssRules)e+=s.cssText;return at(e)})(r):r;var{is:St,defineProperty:wt,getOwnPropertyDescriptor:Et,getOwnPropertyNames:Pt,getOwnPropertySymbols:Ct,getPrototypeOf:kt}=Object,A=globalThis,dt=A.trustedTypes,Ut=dt?dt.emptyScript:"",It=A.reactiveElementPolyfillSupport,N=(r,t)=>r,R={toAttribute(r,t){switch(t){case Boolean:r=r?Ut:null;break;case Object:case Array:r=r==null?r:JSON.stringify(r)}return r},fromAttribute(r,t){let e=r;switch(t){case Boolean:e=r!==null;break;case Number:e=r===null?null:Number(r);break;case Object:case Array:try{e=JSON.parse(r)}catch{e=null}}return e}},J=(r,t)=>!St(r,t),ct={attribute:!0,type:String,converter:R,reflect:!1,useDefault:!1,hasChanged:J};Symbol.metadata??(Symbol.metadata=Symbol("metadata")),A.litPropertyMetadata??(A.litPropertyMetadata=new WeakMap);var v=class extends HTMLElement{static addInitializer(t){this._$Ei(),(this.l??(this.l=[])).push(t)}static get observedAttributes(){return this.finalize(),this._$Eh&&[...this._$Eh.keys()]}static createProperty(t,e=ct){if(e.state&&(e.attribute=!1),this._$Ei(),this.prototype.hasOwnProperty(t)&&((e=Object.create(e)).wrapped=!0),this.elementProperties.set(t,e),!e.noAccessor){let s=Symbol(),i=this.getPropertyDescriptor(t,s,e);i!==void 0&&wt(this.prototype,t,i)}}static getPropertyDescriptor(t,e,s){let{get:i,set:o}=Et(this.prototype,t)??{get(){return this[e]},set(n){this[e]=n}};return{get:i,set(n){let l=i?.call(this);o?.call(this,n),this.requestUpdate(t,l,s)},configurable:!0,enumerable:!0}}static getPropertyOptions(t){return this.elementProperties.get(t)??ct}static _$Ei(){if(this.hasOwnProperty(N("elementProperties")))return;let t=kt(this);t.finalize(),t.l!==void 0&&(this.l=[...t.l]),this.elementProperties=new Map(t.elementProperties)}static finalize(){if(this.hasOwnProperty(N("finalized")))return;if(this.finalized=!0,this._$Ei(),this.hasOwnProperty(N("properties"))){let e=this.properties,s=[...Pt(e),...Ct(e)];for(let i of s)this.createProperty(i,e[i])}let t=this[Symbol.metadata];if(t!==null){let e=litPropertyMetadata.get(t);if(e!==void 0)for(let[s,i]of e)this.elementProperties.set(s,i)}this._$Eh=new Map;for(let[e,s]of this.elementProperties){let i=this._$Eu(e,s);i!==void 0&&this._$Eh.set(i,e)}this.elementStyles=this.finalizeStyles(this.styles)}static finalizeStyles(t){let e=[];if(Array.isArray(t)){let s=new Set(t.flat(1/0).reverse());for(let i of s)e.unshift(X(i))}else t!==void 0&&e.push(X(t));return e}static _$Eu(t,e){let s=e.attribute;return s===!1?void 0:typeof s=="string"?s:typeof t=="string"?t.toLowerCase():void 0}constructor(){super(),this._$Ep=void 0,this.isUpdatePending=!1,this.hasUpdated=!1,this._$Em=null,this._$Ev()}_$Ev(){this._$ES=new Promise(t=>this.enableUpdating=t),this._$AL=new Map,this._$E_(),this.requestUpdate(),this.constructor.l?.forEach(t=>t(this))}addController(t){(this._$EO??(this._$EO=new Set)).add(t),this.renderRoot!==void 0&&this.isConnected&&t.hostConnected?.()}removeController(t){this._$EO?.delete(t)}_$E_(){let t=new Map,e=this.constructor.elementProperties;for(let s of e.keys())this.hasOwnProperty(s)&&(t.set(s,this[s]),delete this[s]);t.size>0&&(this._$Ep=t)}createRenderRoot(){let t=this.shadowRoot??this.attachShadow(this.constructor.shadowRootOptions);return lt(t,this.constructor.elementStyles),t}connectedCallback(){this.renderRoot??(this.renderRoot=this.createRenderRoot()),this.enableUpdating(!0),this._$EO?.forEach(t=>t.hostConnected?.())}enableUpdating(t){}disconnectedCallback(){this._$EO?.forEach(t=>t.hostDisconnected?.())}attributeChangedCallback(t,e,s){this._$AK(t,s)}_$ET(t,e){let s=this.constructor.elementProperties.get(t),i=this.constructor._$Eu(t,s);if(i!==void 0&&s.reflect===!0){let o=(s.converter?.toAttribute!==void 0?s.converter:R).toAttribute(e,s.type);this._$Em=t,o==null?this.removeAttribute(i):this.setAttribute(i,o),this._$Em=null}}_$AK(t,e){let s=this.constructor,i=s._$Eh.get(t);if(i!==void 0&&this._$Em!==i){let o=s.getPropertyOptions(i),n=typeof o.converter=="function"?{fromAttribute:o.converter}:o.converter?.fromAttribute!==void 0?o.converter:R;this._$Em=i;let l=n.fromAttribute(e,o.type);this[i]=l??this._$Ej?.get(i)??l,this._$Em=null}}requestUpdate(t,e,s){if(t!==void 0){let i=this.constructor,o=this[t];if(s??(s=i.getPropertyOptions(t)),!((s.hasChanged??J)(o,e)||s.useDefault&&s.reflect&&o===this._$Ej?.get(t)&&!this.hasAttribute(i._$Eu(t,s))))return;this.C(t,e,s)}this.isUpdatePending===!1&&(this._$ES=this._$EP())}C(t,e,{useDefault:s,reflect:i,wrapped:o},n){s&&!(this._$Ej??(this._$Ej=new Map)).has(t)&&(this._$Ej.set(t,n??e??this[t]),o!==!0||n!==void 0)||(this._$AL.has(t)||(this.hasUpdated||s||(e=void 0),this._$AL.set(t,e)),i===!0&&this._$Em!==t&&(this._$Eq??(this._$Eq=new Set)).add(t))}async _$EP(){this.isUpdatePending=!0;try{await this._$ES}catch(e){Promise.reject(e)}let t=this.scheduleUpdate();return t!=null&&await t,!this.isUpdatePending}scheduleUpdate(){return this.performUpdate()}performUpdate(){if(!this.isUpdatePending)return;if(!this.hasUpdated){if(this.renderRoot??(this.renderRoot=this.createRenderRoot()),this._$Ep){for(let[i,o]of this._$Ep)this[i]=o;this._$Ep=void 0}let s=this.constructor.elementProperties;if(s.size>0)for(let[i,o]of s){let{wrapped:n}=o,l=this[i];n!==!0||this._$AL.has(i)||l===void 0||this.C(i,void 0,o,l)}}let t=!1,e=this._$AL;try{t=this.shouldUpdate(e),t?(this.willUpdate(e),this._$EO?.forEach(s=>s.hostUpdate?.()),this.update(e)):this._$EM()}catch(s){throw t=!1,this._$EM(),s}t&&this._$AE(e)}willUpdate(t){}_$AE(t){this._$EO?.forEach(e=>e.hostUpdated?.()),this.hasUpdated||(this.hasUpdated=!0,this.firstUpdated(t)),this.updated(t)}_$EM(){this._$AL=new Map,this.isUpdatePending=!1}get updateComplete(){return this.getUpdateComplete()}getUpdateComplete(){return this._$ES}shouldUpdate(t){return!0}update(t){this._$Eq&&(this._$Eq=this._$Eq.forEach(e=>this._$ET(e,this[e]))),this._$EM()}updated(t){}firstUpdated(t){}};v.elementStyles=[],v.shadowRootOptions={mode:"open"},v[N("elementProperties")]=new Map,v[N("finalized")]=new Map,It?.({ReactiveElement:v}),(A.reactiveElementVersions??(A.reactiveElementVersions=[])).push("2.1.1");var z=globalThis,K=z.trustedTypes,ht=K?K.createPolicy("lit-html",{createHTML:r=>r}):void 0,$t="$lit$",S=`lit$${Math.random().toFixed(9).slice(2)}$`,yt="?"+S,Ot=`<${yt}>`,k=document,q=()=>k.createComment(""),B=r=>r===null||typeof r!="object"&&typeof r!="function",rt=Array.isArray,Tt=r=>rt(r)||typeof r?.[Symbol.iterator]=="function",Y=`[ 	
\f\r]`,j=/<(?:(!--|\/[^a-zA-Z])|(\/?[a-zA-Z][^>\s]*)|(\/?$))/g,pt=/-->/g,ut=/>/g,P=RegExp(`>|${Y}(?:([^\\s"'>=/]+)(${Y}*=${Y}*(?:[^ 	
\f\r"'\`<>=]|("|')|))|$)`,"g"),ft=/'/g,gt=/"/g,_t=/^(?:script|style|textarea|title)$/i,ot=r=>(t,...e)=>({_$litType$:r,strings:t,values:e}),f=ot(1),Lt=ot(2),Vt=ot(3),U=Symbol.for("lit-noChange"),h=Symbol.for("lit-nothing"),mt=new WeakMap,C=k.createTreeWalker(k,129);function vt(r,t){if(!rt(r)||!r.hasOwnProperty("raw"))throw Error("invalid template strings array");return ht!==void 0?ht.createHTML(t):t}var Mt=(r,t)=>{let e=r.length-1,s=[],i,o=t===2?"<svg>":t===3?"<math>":"",n=j;for(let l=0;l<e;l++){let a=r[l],c,u,d=-1,_=0;for(;_<a.length&&(n.lastIndex=_,u=n.exec(a),u!==null);)_=n.lastIndex,n===j?u[1]==="!--"?n=pt:u[1]!==void 0?n=ut:u[2]!==void 0?(_t.test(u[2])&&(i=RegExp("</"+u[2],"g")),n=P):u[3]!==void 0&&(n=P):n===P?u[0]===">"?(n=i??j,d=-1):u[1]===void 0?d=-2:(d=n.lastIndex-u[2].length,c=u[1],n=u[3]===void 0?P:u[3]==='"'?gt:ft):n===gt||n===ft?n=P:n===pt||n===ut?n=j:(n=P,i=void 0);let x=n===P&&r[l+1].startsWith("/>")?" ":"";o+=n===j?a+Ot:d>=0?(s.push(c),a.slice(0,d)+$t+a.slice(d)+S+x):a+S+(d===-2?l:x)}return[vt(r,o+(r[e]||"<?>")+(t===2?"</svg>":t===3?"</math>":"")),s]},D=class r{constructor({strings:t,_$litType$:e},s){let i;this.parts=[];let o=0,n=0,l=t.length-1,a=this.parts,[c,u]=Mt(t,e);if(this.el=r.createElement(c,s),C.currentNode=this.el.content,e===2||e===3){let d=this.el.content.firstChild;d.replaceWith(...d.childNodes)}for(;(i=C.nextNode())!==null&&a.length<l;){if(i.nodeType===1){if(i.hasAttributes())for(let d of i.getAttributeNames())if(d.endsWith($t)){let _=u[n++],x=i.getAttribute(d).split(S),L=/([.?@])?(.*)/.exec(_);a.push({type:1,index:o,name:L[2],strings:x,ctor:L[1]==="."?tt:L[1]==="?"?et:L[1]==="@"?st:O}),i.removeAttribute(d)}else d.startsWith(S)&&(a.push({type:6,index:o}),i.removeAttribute(d));if(_t.test(i.tagName)){let d=i.textContent.split(S),_=d.length-1;if(_>0){i.textContent=K?K.emptyScript:"";for(let x=0;x<_;x++)i.append(d[x],q()),C.nextNode(),a.push({type:2,index:++o});i.append(d[_],q())}}}else if(i.nodeType===8)if(i.data===yt)a.push({type:2,index:o});else{let d=-1;for(;(d=i.data.indexOf(S,d+1))!==-1;)a.push({type:7,index:o}),d+=S.length-1}o++}}static createElement(t,e){let s=k.createElement("template");return s.innerHTML=t,s}};function I(r,t,e=r,s){if(t===U)return t;let i=s!==void 0?e._$Co?.[s]:e._$Cl,o=B(t)?void 0:t._$litDirective$;return i?.constructor!==o&&(i?._$AO?.(!1),o===void 0?i=void 0:(i=new o(r),i._$AT(r,e,s)),s!==void 0?(e._$Co??(e._$Co=[]))[s]=i:e._$Cl=i),i!==void 0&&(t=I(r,i._$AS(r,t.values),i,s)),t}var G=class{constructor(t,e){this._$AV=[],this._$AN=void 0,this._$AD=t,this._$AM=e}get parentNode(){return this._$AM.parentNode}get _$AU(){return this._$AM._$AU}u(t){let{el:{content:e},parts:s}=this._$AD,i=(t?.creationScope??k).importNode(e,!0);C.currentNode=i;let o=C.nextNode(),n=0,l=0,a=s[0];for(;a!==void 0;){if(n===a.index){let c;a.type===2?c=new H(o,o.nextSibling,this,t):a.type===1?c=new a.ctor(o,a.name,a.strings,this,t):a.type===6&&(c=new it(o,this,t)),this._$AV.push(c),a=s[++l]}n!==a?.index&&(o=C.nextNode(),n++)}return C.currentNode=k,i}p(t){let e=0;for(let s of this._$AV)s!==void 0&&(s.strings!==void 0?(s._$AI(t,s,e),e+=s.strings.length-2):s._$AI(t[e])),e++}},H=class r{get _$AU(){return this._$AM?._$AU??this._$Cv}constructor(t,e,s,i){this.type=2,this._$AH=h,this._$AN=void 0,this._$AA=t,this._$AB=e,this._$AM=s,this.options=i,this._$Cv=i?.isConnected??!0}get parentNode(){let t=this._$AA.parentNode,e=this._$AM;return e!==void 0&&t?.nodeType===11&&(t=e.parentNode),t}get startNode(){return this._$AA}get endNode(){return this._$AB}_$AI(t,e=this){t=I(this,t,e),B(t)?t===h||t==null||t===""?(this._$AH!==h&&this._$AR(),this._$AH=h):t!==this._$AH&&t!==U&&this._(t):t._$litType$!==void 0?this.$(t):t.nodeType!==void 0?this.T(t):Tt(t)?this.k(t):this._(t)}O(t){return this._$AA.parentNode.insertBefore(t,this._$AB)}T(t){this._$AH!==t&&(this._$AR(),this._$AH=this.O(t))}_(t){this._$AH!==h&&B(this._$AH)?this._$AA.nextSibling.data=t:this.T(k.createTextNode(t)),this._$AH=t}$(t){let{values:e,_$litType$:s}=t,i=typeof s=="number"?this._$AC(t):(s.el===void 0&&(s.el=D.createElement(vt(s.h,s.h[0]),this.options)),s);if(this._$AH?._$AD===i)this._$AH.p(e);else{let o=new G(i,this),n=o.u(this.options);o.p(e),this.T(n),this._$AH=o}}_$AC(t){let e=mt.get(t.strings);return e===void 0&&mt.set(t.strings,e=new D(t)),e}k(t){rt(this._$AH)||(this._$AH=[],this._$AR());let e=this._$AH,s,i=0;for(let o of t)i===e.length?e.push(s=new r(this.O(q()),this.O(q()),this,this.options)):s=e[i],s._$AI(o),i++;i<e.length&&(this._$AR(s&&s._$AB.nextSibling,i),e.length=i)}_$AR(t=this._$AA.nextSibling,e){for(this._$AP?.(!1,!0,e);t!==this._$AB;){let s=t.nextSibling;t.remove(),t=s}}setConnected(t){this._$AM===void 0&&(this._$Cv=t,this._$AP?.(t))}},O=class{get tagName(){return this.element.tagName}get _$AU(){return this._$AM._$AU}constructor(t,e,s,i,o){this.type=1,this._$AH=h,this._$AN=void 0,this.element=t,this.name=e,this._$AM=i,this.options=o,s.length>2||s[0]!==""||s[1]!==""?(this._$AH=Array(s.length-1).fill(new String),this.strings=s):this._$AH=h}_$AI(t,e=this,s,i){let o=this.strings,n=!1;if(o===void 0)t=I(this,t,e,0),n=!B(t)||t!==this._$AH&&t!==U,n&&(this._$AH=t);else{let l=t,a,c;for(t=o[0],a=0;a<o.length-1;a++)c=I(this,l[s+a],e,a),c===U&&(c=this._$AH[a]),n||(n=!B(c)||c!==this._$AH[a]),c===h?t=h:t!==h&&(t+=(c??"")+o[a+1]),this._$AH[a]=c}n&&!i&&this.j(t)}j(t){t===h?this.element.removeAttribute(this.name):this.element.setAttribute(this.name,t??"")}},tt=class extends O{constructor(){super(...arguments),this.type=3}j(t){this.element[this.name]=t===h?void 0:t}},et=class extends O{constructor(){super(...arguments),this.type=4}j(t){this.element.toggleAttribute(this.name,!!t&&t!==h)}},st=class extends O{constructor(t,e,s,i,o){super(t,e,s,i,o),this.type=5}_$AI(t,e=this){if((t=I(this,t,e,0)??h)===U)return;let s=this._$AH,i=t===h&&s!==h||t.capture!==s.capture||t.once!==s.once||t.passive!==s.passive,o=t!==h&&(s===h||i);i&&this.element.removeEventListener(this.name,this,s),o&&this.element.addEventListener(this.name,this,t),this._$AH=t}handleEvent(t){typeof this._$AH=="function"?this._$AH.call(this.options?.host??this.element,t):this._$AH.handleEvent(t)}},it=class{constructor(t,e,s){this.element=t,this.type=6,this._$AN=void 0,this._$AM=e,this.options=s}get _$AU(){return this._$AM._$AU}_$AI(t){I(this,t)}};var Nt=z.litHtmlPolyfillSupport;Nt?.(D,H),(z.litHtmlVersions??(z.litHtmlVersions=[])).push("3.3.1");var bt=(r,t,e)=>{let s=e?.renderBefore??t,i=s._$litPart$;if(i===void 0){let o=e?.renderBefore??null;s._$litPart$=i=new H(t.insertBefore(q(),o),o,void 0,e??{})}return i._$AI(r),i};var F=globalThis,g=class extends v{constructor(){super(...arguments),this.renderOptions={host:this},this._$Do=void 0}createRenderRoot(){var e;let t=super.createRenderRoot();return(e=this.renderOptions).renderBefore??(e.renderBefore=t.firstChild),t}update(t){let e=this.render();this.hasUpdated||(this.renderOptions.isConnected=this.isConnected),super.update(t),this._$Do=bt(e,this.renderRoot,this.renderOptions)}connectedCallback(){super.connectedCallback(),this._$Do?.setConnected(!0)}disconnectedCallback(){super.disconnectedCallback(),this._$Do?.setConnected(!1)}render(){return U}};g._$litElement$=!0,g.finalized=!0,F.litElementHydrateSupport?.({LitElement:g});var Rt=F.litElementPolyfillSupport;Rt?.({LitElement:g});(F.litElementVersions??(F.litElementVersions=[])).push("4.2.1");var T=r=>(t,e)=>{e!==void 0?e.addInitializer(()=>{customElements.define(r,t)}):customElements.define(r,t)};var jt={attribute:!0,type:String,converter:R,reflect:!1,hasChanged:J},zt=(r=jt,t,e)=>{let{kind:s,metadata:i}=e,o=globalThis.litPropertyMetadata.get(i);if(o===void 0&&globalThis.litPropertyMetadata.set(i,o=new Map),s==="setter"&&((r=Object.create(r)).wrapped=!0),o.set(e.name,r),s==="accessor"){let{name:n}=e;return{set(l){let a=t.get.call(this);t.set.call(this,l),this.requestUpdate(n,a,r)},init(l){return l!==void 0&&this.C(n,void 0,r,l),l}}}if(s==="setter"){let{name:n}=e;return function(l){let a=this[n];t.call(this,l),this.requestUpdate(n,a,r)}}throw Error("Unsupported decorator location: "+s)};function w(r){return(t,e)=>typeof e=="object"?zt(r,t,e):((s,i,o)=>{let n=i.hasOwnProperty(o);return i.constructor.createProperty(o,s),n?Object.getOwnPropertyDescriptor(i,o):void 0})(r,t,e)}function m(r){return w({...r,state:!0,attribute:!1})}var b=class extends g{constructor(){super(...arguments);this.rpcBase="";this.stats=null;this.loading=!1}connectedCallback(){super.connectedCallback(),this.loadStats()}async loadStats(){try{await this.waitForPluginAPI();let e=await this.pluginAPI.call("get_statistics",{});e.success&&(this.stats={totalRepos:e.data.total_repos||0,activeRepos:e.data.active_repos||0,totalJobs:e.data.total_jobs||0,chunksIngested:e.data.chunks_ingested||0})}catch(e){console.error("Failed to load stats:",e),this.stats={totalRepos:0,activeRepos:0,totalJobs:0,chunksIngested:0}}}async waitForPluginAPI(){for(let e=0;e<50;e++){if(this.pluginAPI)return;await new Promise(s=>setTimeout(s,100))}throw new Error("Plugin API timeout")}render(){return f`
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
    `}};b.styles=E`
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
  `,p([w({type:String})],b.prototype,"rpcBase",2),p([m()],b.prototype,"stats",2),p([m()],b.prototype,"loading",2),b=p([T("github-rag-dashboard")],b);var $=class extends g{constructor(){super(...arguments);this.rpcBase="";this.repositories=[];this.loading=!1;this.error=""}connectedCallback(){super.connectedCallback(),this.loadRepositories()}async loadRepositories(){this.loading=!0,this.error="";try{await this.waitForPluginAPI();let e=await this.pluginAPI.call("list_repositories",{});e.success?this.repositories=e.data.repositories||[]:this.error=e.error||"Failed to load repositories"}catch(e){this.error=`Error: ${e.message}`}finally{this.loading=!1}}async waitForPluginAPI(){for(let e=0;e<50;e++){if(this.pluginAPI)return;await new Promise(s=>setTimeout(s,100))}throw new Error("Plugin API timeout")}handleAddRepository(){alert("Add repository form will be implemented")}handleRunIngestion(e){alert(`Trigger ingestion for ${e}`)}formatDate(e){return e?new Date(e).toLocaleString():"Never"}render(){return f`
      <div class="header">
        <h2>Repositories</h2>
        <button class="btn btn-primary" @click=${this.handleAddRepository}>
          Add Repository
        </button>
      </div>

      ${this.error?f`<div class="error">${this.error}</div>`:""}

      ${this.loading?f`<div>Loading...</div>`:this.repositories.length===0?f`
          <div class="empty-state">
            <p>No repositories configured</p>
            <p>Click "Add Repository" to get started</p>
          </div>
        `:f`
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
                ${this.repositories.map(e=>f`
                  <tr>
                    <td>
                      <strong>${e.owner}/${e.name}</strong><br>
                      <small style="color: #666;">${e.url}</small>
                    </td>
                    <td>${e.branch}</td>
                    <td>
                      <span class="status-badge ${e.sync_enabled?"status-active":"status-inactive"}">
                        ${e.sync_enabled?"Active":"Inactive"}
                      </span>
                    </td>
                    <td>${this.formatDate(e.last_sync_at)}</td>
                    <td>
                      <div class="actions">
                        <button class="btn btn-sm btn-primary" @click=${()=>this.handleRunIngestion(e.id)}>
                          Run Sync
                        </button>
                      </div>
                    </td>
                  </tr>
                `)}
              </tbody>
            </table>
          </div>
        `}
    `}};$.styles=E`
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
  `,p([w({type:String})],$.prototype,"rpcBase",2),p([m()],$.prototype,"repositories",2),p([m()],$.prototype,"loading",2),p([m()],$.prototype,"error",2),$=p([T("github-rag-repository-list")],$);var y=class extends g{constructor(){super(...arguments);this.rpcBase="";this.jobs=[];this.loading=!1;this.error=""}connectedCallback(){super.connectedCallback(),this.loadJobs()}async loadJobs(){this.loading=!0;try{await this.waitForPluginAPI();let e=await this.pluginAPI.call("list_jobs",{limit:50,offset:0});e.success&&(this.jobs=e.data.jobs||[])}catch(e){this.error=String(e)}finally{this.loading=!1}}async waitForPluginAPI(){for(let e=0;e<50;e++){if(this.pluginAPI)return;await new Promise(s=>setTimeout(s,100))}throw new Error("Plugin API timeout")}getStatusClass(e){return`status-badge status-${e}`}formatDate(e){return new Date(e).toLocaleString()}render(){return f`
      <h2>Ingestion Jobs</h2>

      ${this.loading?f`<div>Loading...</div>`:this.jobs.length===0?f`
          <div class="empty-state">
            <p>No ingestion jobs yet</p>
          </div>
        `:f`
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
                </tr>
              </thead>
              <tbody>
                ${this.jobs.map(e=>f`
                  <tr>
                    <td><code>${e.id.substring(0,8)}</code></td>
                    <td>${e.type}</td>
                    <td><span class="${this.getStatusClass(e.status)}">${e.status}</span></td>
                    <td>${e.stats.files_scanned}</td>
                    <td>${e.stats.chunks_written}</td>
                    <td>${this.formatDate(e.started_at)}</td>
                  </tr>
                `)}
              </tbody>
            </table>
          </div>
        `}
    `}};y.styles=E`
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
  `,p([w({type:String})],y.prototype,"rpcBase",2),p([m()],y.prototype,"jobs",2),p([m()],y.prototype,"loading",2),p([m()],y.prototype,"error",2),y=p([T("github-rag-job-list")],y);export{b as GitHubRAGDashboard,y as GitHubRAGJobList,$ as GitHubRAGRepositoryList};
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
