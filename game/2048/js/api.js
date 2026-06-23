function postEvent(type, payload) {
  try {
    var body = JSON.stringify(Object.assign({ type: type, ts: Date.now() }, payload || {}));
    fetch("/api/event", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: body,
      keepalive: true
    }).catch(function () {});
  } catch (e) {}
}
