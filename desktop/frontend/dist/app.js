const goApp = () => window.go && window.go.main && window.go.main.App;

let state = {
  status: {},
  engines: [],
  sessions: [],
  models: [],
  engine: "",
  session: null,
  es: null,
};

function $(id) { return document.getElementById(id); }

function shortId(id) {
  if (!id) return "—";
  return id.length > 16 ? id.slice(0, 8) + "…" + id.slice(-4) : id;
}

async function call(name, ...args) {
  const app = goApp();
  if (!app || typeof app[name] !== "function") {
    throw new Error("desktop bindings are not ready");
  }
  return app[name](...args);
}

function appendStream(chunk) {
  const el = $("stream");
  if (!el || !chunk) return;
  const mesh = chunk.agent_mesh || {};
  const delta = (chunk.choices && chunk.choices[0] && chunk.choices[0].delta) || {};
  const text = delta.content || "";
  const tag = [mesh.engine, shortId(mesh.session)].filter(Boolean).join(" ");
  if (!text && !(chunk.choices && chunk.choices[0] && chunk.choices[0].finish_reason)) return;
  const line = (tag ? "[" + tag + "] " : "") + (text || "[done]");
  el.textContent += line;
  if (!text.endsWith("\n")) el.textContent += "\n";
  el.scrollTop = el.scrollHeight;
}

function localListen(listen) {
  const raw = (listen || "").replace(/^https?:\/\//, "");
  const i = raw.lastIndexOf(":");
  const host = i >= 0 ? raw.slice(0, i) : raw;
  const port = i >= 0 ? raw.slice(i + 1) : "7700";
  if (!host || host === "0.0.0.0" || host === "::") return "127.0.0.1:" + port;
  return host + ":" + port;
}

function connectEvents(listen) {
  listen = localListen(listen);
  if (!listen) return;
  if (state.esListen === listen && state.es) return;
  if (state.es) {
    state.es.close();
    state.es = null;
  }
  const url = "http://" + listen + "/v1/openai/events";
  const es = new EventSource(url);
  es.onmessage = (ev) => {
    if (!ev.data || ev.data === "[DONE]") return;
    try {
      appendStream(JSON.parse(ev.data));
    } catch (_) {}
  };
  state.es = es;
  state.esListen = listen;
}

function renderEngines() {
  const ul = $("engineList");
  const sel = $("sendEngine");
  ul.innerHTML = "";
  sel.innerHTML = "";
  state.engines.forEach((e) => {
    const li = document.createElement("li");
    if (state.engine === e.name) li.classList.add("active");
    const led = document.createElement("span");
    led.className = "led" + (e.available && e.enabled ? " on" : "");
    const body = document.createElement("div");
    body.innerHTML = '<div class="eng-name">' + (e.title || e.name) + "</div>" +
      '<div class="eng-sub">' + (e.available ? (e.version || "online") : "not found") + "</div>";
    const sw = document.createElement("input");
    sw.type = "checkbox";
    sw.className = "switch";
    sw.checked = !!e.enabled;
    sw.addEventListener("click", async (ev) => {
      ev.stopPropagation();
      await call("SetEngineEnabled", e.name, sw.checked);
      await refresh();
    });
    li.append(led, body, sw);
    li.addEventListener("click", async () => {
      state.engine = e.name;
      await refresh();
    });
    ul.appendChild(li);
    const opt = document.createElement("option");
    opt.value = e.name;
    opt.textContent = e.title || e.name;
    if (state.engine === e.name) opt.selected = true;
    sel.appendChild(opt);
  });
}

function renderSessions() {
  const tb = $("sessionBody");
  tb.innerHTML = "";
  const rows = state.engine ? state.sessions.filter((s) => s.engine === state.engine) : state.sessions;
  if (!rows.length) {
    const tr = document.createElement("tr");
    tr.innerHTML = '<td class="empty" colspan="5">No sessions. Open local WorkBuddy / Claude / Cursor / Codex, then refresh.</td>';
    tb.appendChild(tr);
    return;
  }
  rows.forEach((s) => {
    const tr = document.createElement("tr");
    if (state.session && state.session.id === s.id && state.session.engine === s.engine) tr.classList.add("sel");
    const cls = s.busy ? "busy" : (s.live ? "live" : "");
    tr.innerHTML =
      '<td><span class="dot ' + cls + '"></span> ' + (s.status || (s.live ? "online" : "history")) + "</td>" +
      "<td>" + s.engine + "</td>" +
      "<td>" + (s.title || "—") + "</td>" +
      '<td class="sid">' + shortId(s.id) + "</td>" +
      '<td class="cwd">' + (s.cwd || "—") + "</td>";
    tr.addEventListener("click", async () => {
      state.session = s;
      state.engine = s.engine;
      $("sendEngine").value = s.engine;
      $("cwdInput").value = s.cwd || "";
      renderSessions();
      await loadHistory(s);
    });
    tb.appendChild(tr);
  });
}

function renderModels() {
  const ul = $("modelList");
  ul.innerHTML = "";
  const rows = state.engine ? state.models.filter((m) => m.engine === state.engine) : state.models;
  if (!rows.length) {
    ul.innerHTML = '<li class="empty">No models</li>';
    return;
  }
  rows.forEach((m) => {
    const li = document.createElement("li");
    li.innerHTML = "<span>" + (m.title || m.name) + (m.default ? " · default" : "") +
      '</span><span class="id">' + m.name + "</span>";
    ul.appendChild(li);
  });
}

function renderDirs() {
  const ul = $("dirList");
  if (!ul) return;
  ul.innerHTML = "";
  const rows = (state.dirs || []).filter((d) => !state.engine || d.engine === state.engine);
  if (!rows.length) {
    ul.innerHTML = '<li class="empty">No workspaces</li>';
    return;
  }
  rows.forEach((d) => {
    const li = document.createElement("li");
    li.innerHTML = "<span>" + d.path + '</span><span class="id">' + (d.engine || "") + "</span>";
    li.addEventListener("click", () => { $("cwdInput").value = d.path; });
    ul.appendChild(li);
  });
}

async function loadHistory(s) {
  try {
    const evs = await call("History", s.engine, s.id, 60);
    const el = $("stream");
    el.textContent = "";
    (evs || []).forEach((ev) => {
      if (ev.text) el.textContent += ev.text + "\n";
    });
    el.scrollTop = el.scrollHeight;
  } catch (err) {
    $("stream").textContent = String(err);
  }
}

async function refresh() {
  try {
    state.status = await call("NodeStatus");
    $("nodeName").textContent = state.status.name || "—";
    $("nodeId").textContent = state.status.node_id || "—";
    $("openaiUrl").textContent = (state.status.openai || "—") + (state.status.ws ? " · " + state.status.ws : "");
    const active = document.activeElement && document.activeElement.id;
    if (active !== "relayInput") {
      $("relayInput").value = (state.status.relays || []).join(",");
    }
    if (active !== "relayPassword") {
      $("relayPassword").value = state.status.password || "";
    }
    if (active !== "advertiseInput") {
      $("advertiseInput").value = state.status.advertise || "";
    }
    const st = $("relayState");
    if (st) {
      if (state.status.linked) {
        st.textContent = "connected";
        st.className = "relay-state ok";
      } else if (state.status.link_error) {
        st.textContent = "failed: " + state.status.link_error;
        st.className = "relay-state bad";
      } else if ((state.status.relays || []).length) {
        st.textContent = "connecting";
        st.className = "relay-state";
      } else {
        st.textContent = "offline";
        st.className = "relay-state";
      }
    }
    connectEvents(state.status.listen || "");
    state.engines = await call("Engines") || [];
    state.sessions = await call("Sessions", "", $("showAll").checked) || [];
    state.models = await call("Models", "") || [];
    try { state.dirs = await call("Directories") || []; } catch (_) { state.dirs = []; }
    if (!state.engine && state.engines[0]) state.engine = state.engines[0].name;
    renderEngines();
    renderSessions();
    renderModels();
    renderDirs();
  } catch (err) {
    $("stream").textContent = String(err);
  }
}

$("refresh").addEventListener("click", refresh);
$("showAll").addEventListener("change", refresh);
$("relayForm").addEventListener("submit", async (ev) => {
  ev.preventDefault();
  const st = $("relayState");
  if (st) { st.textContent = "connecting"; st.className = "relay-state"; }
  try {
    await call("SetMesh", $("relayInput").value, $("relayPassword").value, $("advertiseInput").value);
  } catch (err) {
    if (st) { st.textContent = "failed: " + err; st.className = "relay-state bad"; }
  }
  await refresh();
});
$("sendBtn").addEventListener("click", async () => {
  const text = $("prompt").value.trim();
  if (!text) return;
  const engine = $("sendEngine").value || state.engine;
  try {
    if (state.session && state.session.engine === engine) {
      await call("Reply", engine, state.session.id, text);
    } else {
      await call("Send", engine, $("cwdInput").value, text);
    }
    $("prompt").value = "";
  } catch (err) {
    $("stream").textContent += "\nerror: " + err + "\n";
  }
});

if (window.runtime && window.runtime.EventsOn) {
  window.runtime.EventsOn("openai:chunk", appendStream);
}

async function boot() {
  for (let i = 0; i < 20; i++) {
    try {
      await refresh();
      return;
    } catch (_) {
      await new Promise((r) => setTimeout(r, 300));
    }
  }
}

boot();
setInterval(refresh, 4000);
