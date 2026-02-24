// Popup: export HAR, send to backend
const dot = document.getElementById('dot');
const statusText = document.getElementById('statusText');
const countText = document.getElementById('countText');
const scenarioInput = document.getElementById('scenario');
const backendUrl = document.getElementById('backendUrl');
const btnExportHAR = document.getElementById('btnExportHAR');
const btnSendBackend = document.getElementById('btnSendBackend');
const messageEl = document.getElementById('message');

// Load state
chrome.runtime.sendMessage({ type: 'GET_STATE' }, (state) => {
  if (state) updateUI(state);
});

// Listen for state changes
chrome.runtime.onMessage.addListener((msg) => {
  if (msg.type === 'STATE_CHANGED') updateUI(msg.state);
});

function updateUI(state) {
  dot.className = 'dot ' + (state.recording ? 'on' : 'off');
  statusText.textContent = state.recording ? 'Recording' : 'Idle';
  countText.textContent = state.count || 0;
}

function showMsg(text, type) {
  messageEl.textContent = text;
  messageEl.className = 'msg ' + type;
  messageEl.style.display = 'block';
  setTimeout(() => { messageEl.style.display = 'none'; }, 5000);
}

btnExportHAR.addEventListener('click', () => {
  chrome.runtime.sendMessage({ type: 'GET_REQUESTS' }, (requests) => {
    if (!requests || requests.length === 0) {
      showMsg('No requests to export', 'error');
      return;
    }
    const har = buildHAR(requests);
    const blob = new Blob([JSON.stringify(har, null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `apidoc-${Date.now()}.har`;
    a.click();
    URL.revokeObjectURL(url);
    showMsg(`Exported ${requests.length} requests`, 'success');
  });
});

btnSendBackend.addEventListener('click', () => {
  const scenario = scenarioInput.value.trim();
  if (!scenario) {
    showMsg('Please enter a scenario description', 'error');
    return;
  }
  chrome.runtime.sendMessage({ type: 'GET_REQUESTS' }, async (requests) => {
    if (!requests || requests.length === 0) {
      showMsg('No requests to send', 'error');
      return;
    }
    btnSendBackend.disabled = true;
    btnSendBackend.textContent = '⏳ Sending...';
    try {
      const base = backendUrl.value.trim().replace(/\/$/, '');
      // Step 1: send traffic
      const trafficResp = await fetch(`${base}/api/traffic`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ scenario, logs: requests }),
      });
      if (!trafficResp.ok) throw new Error(`Traffic upload failed: ${trafficResp.status}`);
      const { session_id } = await trafficResp.json();

      // Step 2: trigger generation
      btnSendBackend.textContent = '⏳ Generating docs...';
      const genResp = await fetch(`${base}/api/generate`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ session_id }),
      });
      if (!genResp.ok) throw new Error(`Generation failed: ${genResp.status}`);

      showMsg(`Done! Session: ${session_id}. View at ${base}/docs/`, 'success');
    } catch (e) {
      showMsg(e.message, 'error');
    } finally {
      btnSendBackend.disabled = false;
      btnSendBackend.textContent = '🚀 Send to Backend & Generate';
    }
  });
});

function buildHAR(requests) {
  return {
    log: {
      version: '1.2',
      creator: { name: 'API Doc Recorder', version: '1.0.0' },
      entries: requests.map((r) => ({
        startedDateTime: new Date().toISOString(),
        time: r.latency_ms,
        request: {
          method: r.method,
          url: r.url,
          headers: Object.entries(r.request_headers || {}).map(([name, value]) => ({ name, value })),
          queryString: Object.entries(r.query_params || ).flatMap(([name, vals]) => vals.map(v => ({ name, value: v }))),
          postData: r.request_body ? { mimeType: r.content_type || '', text: r.request_body } : undefined,
        },
        response: {
          status: r.status_code,
          statusText: '',
          headers: Object.entries(r.response_headers || {}).map(([name, value]) => ({ name, value })),
          content: { size: (r.response_body || '').length, mimeType: r.response_content_type || '', text: r.response_body || '' },
        },
      })),
    },
  };
}
