// Health is polled, not just probed once at boot, so the api is woken as soon as
// the page loads rather than waiting for the first tile merge. Both services run
// at min_replicas: 0, and a cold start costs seconds — paying it up front means
// the first score_update post lands on an already-warm api.
var API_HEALTH_INTERVAL_MS = 30000;

function postEvent(type, payload) {
  try {
    var body = JSON.stringify(Object.assign({ type: type, ts: Date.now() }, payload || {}));
    fetch("/2048/api/event", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: body,
      keepalive: true
    }).catch(function () {});
  } catch (e) {}
}
   
// The api answers /health with 503 when a dependency is down, and fetch does not
// reject on an error status — so the status code is read explicitly rather than
// relying on the promise to reject.
function checkApiHealth() {
  try {
    fetch("/2048/api/health", { cache: "no-store" })
      .then(function (res) {
        return res.json().then(function (body) {
          return { status: res.status, body: body };
        });
      })
      .then(function (result) {
        var b = result.body || {};
        console.log(
          "api health: HTTP " + result.status +
          " status=" + b.status +
          " redis=" + b.redis +
          " postgres=" + b.postgres
        );
      })
      .catch(function (err) {
        console.log("api health: unreachable —", err);
      });
  } catch (e) {}
}

checkApiHealth();
setInterval(checkApiHealth, API_HEALTH_INTERVAL_MS);
