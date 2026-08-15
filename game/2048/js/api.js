// Health is polled, not just probed once at boot, so the api is woken as soon as
// the page loads rather than waiting for the first tile merge. Both services run
// at min_replicas: 0, and a cold start costs seconds — paying it up front means
// the first score_update post lands on an already-warm api.
var API_HEALTH_INTERVAL_MS = 30000;

// Resolved against <base href>, not against the origin. The game is served on a
// path prefix, so a root-absolute "/api/..." would leave that prefix and hit
// whatever else answers at the domain root.
var API_BASE = new URL("api/", document.baseURI).pathname;

function postEvent(type, payload) {
  try {
    var body = JSON.stringify(Object.assign({ type: type, ts: Date.now() }, payload || {}));
    fetch(API_BASE + "event", {
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
    fetch(API_BASE + "health", { cache: "no-store" })
      .then(function (res) {
        return res.json().then(function (body) {
          return { status: res.status, body: body };
        });
      })
      .then(function (result) {
        // The body carries only the dependencies the api is actually wired to,
        // so it is logged whole rather than by fixed key — naming redis and
        // postgres here reported them as "undefined" on a deployment that
        // simply does not connect to either.
        console.log(
          "api health: HTTP " + result.status + " " +
          JSON.stringify(result.body || {})
        );
      })
      .catch(function (err) {
        console.log("api health: unreachable —", err);
      });
  } catch (e) {}
}

checkApiHealth();
setInterval(checkApiHealth, API_HEALTH_INTERVAL_MS);
