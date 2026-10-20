// Prompt Compressor Dashboard Web Component
//
// Placeholder for the savings dashboard: it shows that the plugin is loaded.
// Every value from the plugin is written with textContent, never innerHTML.
class PromptCompressorDashboard extends HTMLElement {
  constructor() {
    super();
    this.attachShadow({ mode: 'open' });
  }

  connectedCallback() {
    this.render();
    this.waitForPluginAPI();
  }

  async waitForPluginAPI() {
    for (let i = 0; i < 50; i++) {
      if (this.pluginAPI) {
        this.loadStatus();
        return;
      }
      await new Promise(resolve => setTimeout(resolve, 100));
    }
    this.showError('Plugin API initialization timeout - please refresh the page');
  }

  async loadStatus() {
    try {
      const status = await this.pluginAPI.call('getStatus', {});
      this.shadowRoot.querySelector('#version').textContent = status.version || '';
      this.shadowRoot.querySelector('#loading').style.display = 'none';
      this.shadowRoot.querySelector('#content').style.display = 'block';
    } catch (error) {
      this.showError('Failed to load status: ' + error.message);
    }
  }

  showError(message) {
    this.shadowRoot.querySelector('#loading').style.display = 'none';
    const errorEl = this.shadowRoot.querySelector('#error');
    errorEl.textContent = message;
    errorEl.style.display = 'block';
  }

  render() {
    this.shadowRoot.innerHTML = `
      <style>
        :host {
          display: block;
          padding: 24px;
          font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
          color: #1f2937;
        }
        h1 { font-size: 22px; font-weight: 600; margin: 0 0 8px; }
        p { color: #4b5563; margin: 0 0 8px; }
        #error {
          display: none;
          background: #fef2f2;
          color: #b91c1c;
          border: 1px solid #fecaca;
          border-radius: 6px;
          padding: 12px;
        }
        #content { display: none; }
      </style>
      <h1>Prompt Compressor</h1>
      <div id="loading"><p>Loading…</p></div>
      <div id="error"></div>
      <div id="content">
        <p>Version <span id="version"></span> is loaded. It passes every request through unchanged.</p>
      </div>
    `;
  }
}

if (!customElements.get('prompt-compressor-dashboard')) {
  customElements.define('prompt-compressor-dashboard', PromptCompressorDashboard);
}
