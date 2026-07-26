with open('app.js','r') as f: js = f.read()

# 1. Add new element IDs
js = js.replace(
  '"load-history", "interaction-modal"',
  '"load-history", "interaction-modal", "perm-toggle", "new-workspace", "workspace-info", "workspace-list"'
)

# 2. Replace render() call to pass workspace data
js = js.replace(
  'renderSessions(snapshot.sessions || [], snapshot.session || {}, snapshot.capabilities || {});',
  'renderSessions(snapshot.sessions || [], snapshot.session || {}, snapshot.capabilities || {}, snapshot.session_workspaces, snapshot.workspaces);'
)

# 3. Add renderWorkspace to render()
js = js.replace(
  'renderInteraction(snapshot.interaction);\n}',
  'renderInteraction(snapshot.interaction);\n  renderWorkspace(snapshot);\n}'
)

# 4. Add renderWorkspace to renderIncremental runtime.changed
js = js.replace(
  'renderProject(snapshot);\n    return;\n  }\n  if (kind === "interaction.opened"',
  'renderProject(snapshot);\n    renderWorkspace(snapshot);\n    return;\n  }\n  if (kind === "interaction.opened"'
)

# 5. Replace old renderSessions with workspace-grouped version
old = js[js.find('function renderSessions('):js.find('\nfunction renderSessionInteraction')] if 'renderSessionInteraction' in js else js[js.find('function renderSessions('):js.find('\nfunction shortSessionID')]
# Actually let's find the exact boundaries
import re
m = re.search(r'(function renderSessions\([^)]+\) \{[^}]*(?:\{[^}]*\}[^}]*)*\})', js)
if m:
    old_ses = m.group(1)
else:
    # Try span-based approach
    start = js.find('function renderSessions(')
    # Find the matching closing brace by counting
    depth = 0
    i = start
    while i < len(js):
        if js[i] == '{': depth += 1
        elif js[i] == '}':
            depth -= 1
            if depth == 0:
                end = i + 1
                break
        i += 1
    old_ses_str = js[start:end]

# Build new renderSessions
new_ses = '''function renderSessions(sessions, current, capabilities, workspaceBindings, workspaces) {
  const currentID = current.id || "";
  const items = [...sessions];
  if (currentID && !items.some(s => s.id === currentID)) {
    items.unshift({ id: currentID, current: true });
  }
  const bindings = workspaceBindings || {};
  const wsMap = {};
  for (const w of (workspaces || [])) { wsMap[w.id] = w; }
  const grouped = { "__unlinked__": [] };
  for (const w of (workspaces || [])) { grouped[w.id] = []; }
  for (const s of items) {
    const wid = bindings[s.id];
    if (wid && grouped[wid]) { grouped[wid].push(s); }
    else { grouped["__unlinked__"].push(s); }
  }
  let html = "";
  const renderSessionRow = (s, active) => {
    const updated = s.updated_at
      ? new Date(s.updated_at).toLocaleString([], { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" })
      : "\\u5f53\\u524d\\u4f1a\\u8bdd";
    const detail = s.token_count ? updated + " \\u00b7 " + s.token_count + " tokens" : updated;
    return \'<div class="session-row \' + (active ? "active" : "") + \'">\' +
      \'<button class="stack-button session-button" data-session="\' + escapeHtml(s.id) + \'">\' +
      \'<span class="session-name">\' + icon("message", 13) + " " + escapeHtml(shortSessionID(s.id)) + \'</span><small>\' + escapeHtml(detail) + \'</small>\' +
      \'</button>\' +
      \'<button class="session-del" data-del="\' + escapeHtml(s.id) + \'" title="\\u5220\\u9664\\u4f1a\\u8bdd">\\u00d7</button>\' +
      \'</div>\';
  };
  for (const [wid, sessList] of Object.entries(grouped)) {
    if (!sessList.length) continue;
    const ws = wsMap[wid];
    const label = wid === "__unlinked__" ? "\\u672a\\u5173\\u8054\\u5bf9\\u8bdd" : (ws ? ws.name : wid);
    const addBtn = wid !== "__unlinked__"
      ? \'<button class="icon-button subtle ws-add-session" data-ws="\' + escapeHtml(wid) + \'" title="\\u65b0\\u589e\\u5de5\\u4f5c\\u533a\\u5bf9\\u8bdd" style="width:14px;height:14px" data-icon="plus"></button>\'
      : "";
    html += \'<div class="session-group"><div class="session-group-head"><span>\' + escapeHtml(label) + \'</span>\' + addBtn + \'</div>\' + sessList.map(s => renderSessionRow(s, s.id === currentID)).join("") + \'</div>\';
  }
  elements["session-count"].textContent = String(items.length);
  elements["session-list"].innerHTML = html || \'<span class="muted list-empty">\\u6682\\u65e0\\u4f1a\\u8bdd</span>\';
  elements["session-list"].querySelectorAll(".session-del").forEach(btn => {
    btn.addEventListener("click", async (e) => {
      e.stopPropagation();
      if (!window.confirm("\\u5220\\u9664\\u4f1a\\u8bdd " + shortSessionID(btn.dataset.del) + "\\uff1f")) return;
      try { await invoke("DeleteSession", btn.dataset.del); await refresh({ scroll: false }); }
      catch (error) { showToast(error); }
    });
  });
  elements["session-list"].querySelectorAll("button[data-session]").forEach(button => {
    button.addEventListener("click", async () => {
      if (button.dataset.session === currentID) return;
      if (!capabilities.session_resume) {
        showToast(capabilities.session_resume_reason || "\\u5f53\\u524d\\u7248\\u672c\\u6682\\u4e0d\\u652f\\u6301\\u6062\\u590d\\u5386\\u53f2\\u4f1a\\u8bdd");
        return;
      }
      try { await invoke("Submit", "/resume " + button.dataset.session); await refresh({ scroll: "bottom" }); }
      catch (error) { showToast(error); }
    });
  });
  elements["session-list"].querySelectorAll(".ws-add-session").forEach(btn => {
    btn.addEventListener("click", async (e) => {
      e.stopPropagation();
      try {
        await invoke("Submit", "/new");
        await refresh({ scroll: "bottom" });
        await invoke("BindWorkspace", btn.dataset.ws);
        await refresh({ scroll: false });
      } catch (error) { showToast(error); }
    });
  });
}'''

js = js.replace(old_ses_str, new_ses)
with open('app.js','w') as f: f.write(js)
print('app.js updated: sessions replaced')
