// Service Worker: relay messages between popup and devtools panel
const STORAGE_KEY = 'apidoc_captured_requests';
const STATE_KEY = 'apidoc_recording_state';

chrome.runtime.onMessage.addListener((msg, sender, sendResponse) => {
  if (msg.type === 'GET_STATE') {
    chrome.storage.local.get(STATE_KEY, (data) => {
      sendResponse(data[STATE_KEY] || { recording: false, count: 0 });
    });
    return true;
  }

  if (msg.type === 'SET_STATE') {
    chrome.storage.local.set({ [STATE_KEY]: msg.state });
    // Broadcast to all listeners
    chrome.runtime.sendMessage({ type: 'STATE_CHANGED', state: msg.state }).catch(() => {});
    sendResponse({ ok: true });
    return true;
  }

  if (msg.type === 'APPEND_REQUEST') {
    chrome.storage.local.get(STORAGE_KEY, (data) => {
      const requests = data[STORAGE_KEY] || [];
      requests.push(msg.entry);
      chrome.storage.local.set({ [STORAGE_KEY]: requests }, () => {
        // Update count in state
        chrome.storage.local.get(STATE_KEY, (stateData) => {
          const state = stateData[STATE_KEY] || { recording: true, count: 0 };
          state.count = requests.length;
          chrome.storage.local.set({ [STATE_KEY]: state });
          chrome.runtime.sendMessage({ type: 'STATE_CHANGED', state }).catch(() => {});
          sendResponse({ ok: true, count: requests.length });
        });
      });
    });
    return true;
  }

  if (msg.type === 'GET_REQUESTS') {
    chrome.storage.local.get(STORAGE_KEY, (data) => {
      sendResponse(data[STORAGE_KEY] || []);
    });
    return true;
  }

  if (msg.type === 'CLEAR_REQUESTS') {
    chrome.storage.local.set({ [STORAGE_KEY]: [], [STATE_KEY]: { recording: false, count: 0 } }, () => {
      sendResponse({ ok: true });
    });
    return true;
  }
});
