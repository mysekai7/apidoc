// DevTools Panel: capture network requests
const IGNORE_EXTENSIONS = ['.js', '.css', '.png', '.jpg', '.gif', '.svg', '.woff', '.woff2', '.ico', '.map', '.ttf', '.eot'];
const IGNORE_CONTENT_TYPES = ['text/html', 'text/css', 'image/', 'font/', 'application/javascript', 'text/javascript'];

let recording = false;
let requestQueue = Promise.resolve();

const btnRecord = document.getElementById('btnRecord');
const btnStop = document.getElementById('btnStop');
const btnClear = document.getElementById('btnClear');
const filterInput = document.getElementById('filterInput');
const requestList = document.getElementById('requestList');
const countLabel = document.getElementById('countLabel');
const statusBadge = document.getElementById('statusBadge');
const emptyMsg = document.getElementById('emptyMsg');

// Restore state
chrome.runtime.sendMessage({ type: 'GET_STATE' }, (state) => {
  if (state && state.recording) {
    startRecording(false);
  }
  updateCount(state ? state.count : 0);
});

// Load existing requests
chrome.runtime.sendMessage({ type: 'GET_REQUESTS' }, (requests) => {
  if (requests && requests.length > 0) {
    requests.forEach((req, i) => addRow(i + 1, req));
    emptyMsg.style.display = 'none';
  }
});

btnRecord.addEventListener('click', () => startRecording(true));
btnStop.addEventListener('click', stopRecording);
btnClear.addEventListener('click', clearAll);
filterInput.addEventListener('input', filterRows);

// Listen for state changes from popup
chrome.runtime.onMessage.addListener((msg) => {
  if (msg.type === 'STATE_CHANGED') {
    updateCount(msg.state.count);
  }
});

function startRecording(broadcast) {
  recording = true;
  btnRecord.disabled = true;
  btnStop.disabled = false;
  statusBadge.textContent = 'recording';
  statusBadge.className = 'status recording';
  if (broadcast) {
    chrome.runtime.sendMessage({ type: 'SET_STATE', state: { recording: true, count: 0 } });
  }
}

function stopRecording() {
  recording = false;
  btnRecord.disabled = false;
  btnStop.disabled = true;
  statusBadge.textContent = 'stopped';
  statusBadge.className = 'status idle';
  chrome.runtime.sendMessage({ type: 'SET_STATE', state: { recording: false, count: parseInt(countLabel.textContent) || 0 } });
}

function clearAll() {
  recording = false;
  btnRecord.disabled = false;
  btnStop.disabled = true;
  statusBadge.textContent = 'idle';
  statusBadge.className = 'status idle';
  requestList.innerHTML = '';
  emptyMsg.style.display = '';
  updateCount(0);
  chrome.runtime.sendMessage({ type: 'CLEAR_REQUESTS' });
}

function updateCount(n) {
  countLabel.textContent = `${n} requests`;
}

// Network listener
chrome.devtools.network.onRequestFinished.addListener((request) => {
  if (!recording) return;
  if (shouldIgnore(request)) return;

  requestQueue = requestQueue.then(() => {
    return new Promise((resolve) => {
      request.getContent((body) => {
        const entry = buildEntry(request, body);
        chrome.runtime.sendMessage({ type: 'APPEND_REQUEST', entry }, (resp) => {
          const seq = resp ? resp.count : 0;
          addRow(seq, entry);
          emptyMsg.style.display = 'none';
          updateCount(seq);
          resolve();
        });
      });
    });
  });
});

function shouldIgnore(request) {
  const url = new URL(request.request.url);
  const path = url.pathname.toLowerCase();

  // Ignore static resources by extension
  for (const ext of IGNORE_EXTENSIONS) {
    if (path.endsWith(ext)) return true;
  }

  // Ignore by response content type
  const ct = (request.response.content.mimeType || '').toLowerCase();
  for (const ignore of IGNORE_CONTENT_TYPES) {
    if (ct.startsWith(ignore)) return true;
  }

  // Ignore data: and chrome-extension: URLs
  if (!url.protocol.startsWith('http')) return true;

  return false;
}

function buildEntry(request, responseBody) {
  const url = new URL(request.request.url);
  const queryParams = {};
  url.searchParams.forEach((val, key) => {
    if (!queryParams[key]) queryParams[key] = [];
    queryParams[key].push(val);
  });

  const reqHeaders = {};
  (request.request.headers || []).forEach(h => { reqHeaders[h.name] = h.value; });

  const respHeaders = {};
  (request.response.headers || []).forEach(h => { respHeaders[h.name] = h.value; });

  return {
    method: request.request.method,
    url: request.request.url,
    host: url.host,
    path: url.pathname,
    query_params: queryParams,
    request_headers: reqHeaders,
    request_body: request.request.postData ? request.request.postData.text || '' : '',
    content_type: request.request.postData ? request.request.postData.mimeType || '' : '',
    status_code: request.response.status,
    response_headers: respHeaders,
    response_body: responseBody || '',
    response_content_type: request.response.content.mimeType || '',
    latency_ms: Math.round(request.time || 0),
  };
}

function addRow(seq, entry) {
  const tr = document.createElement('tr');
  tr.dataset.path = entry.path.toLowerCase();

  const statusClass = entry.status_code < 300 ? 's2xx' : entry.status_code < 500 ? 's4xx' : 's5xx';

  tr.innerHTML = `
    <td>${seq}</td>
    <td><span class="method ${entry.method}">${entry.method}</span></td>
    <td title="${entry.url}">${entry.path}</td>
    <td><span class="status-code ${statusClass}">${entry.status_code}</span></td>
    <td>${entry.latency_ms}ms</td>
  `;
  requestList.appendChild(tr);
}

function filterRows() {
  const q = filterInput.value.toLowerCase();
  requestList.querySelectorAll('tr').forEach(tr => {
    tr.style.display = tr.dataset.path.includes(q) ? '' : 'none';
  });
}
