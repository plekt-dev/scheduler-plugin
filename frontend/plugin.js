(function () {
  'use strict';

  function el(tag, attrs, children) {
    const e = document.createElement(tag);
    if (attrs) {
      for (const k in attrs) {
        if (k === 'class') e.className = attrs[k];
        else if (k === 'text') e.textContent = attrs[k];
        else if (k.startsWith('data-')) e.setAttribute(k, attrs[k]);
        else if (k === 'html') e.innerHTML = attrs[k];
        else e.setAttribute(k, attrs[k]);
      }
    }
    if (children) {
      for (const c of children) {
        if (c == null) continue;
        e.appendChild(typeof c === 'string' ? document.createTextNode(c) : c);
      }
    }
    return e;
  }

  function fmtCompact(iso) {
    if (!iso) return '–';
    const months = ['Jan','Feb','Mar','Apr','May','Jun','Jul','Aug','Sep','Oct','Nov','Dec'];
    const y = iso.slice(0, 4), m = Number(iso.slice(5, 7)), d = iso.slice(8, 10);
    const hh = iso.slice(11, 13), mm = iso.slice(14, 16);
    if (!y || !m || !d || !hh) return iso;
    return `${months[m-1] || '???'} ${d} ${hh}:${mm}`;
  }

  function fmtAgo(iso) {
    if (!iso) return '–';
    const then = Date.parse(iso);
    if (isNaN(then)) return iso;
    const sec = Math.floor((Date.now() - then) / 1000);
    if (sec < 60) return `${sec}s ago`;
    if (sec < 3600) return `${Math.floor(sec/60)}m ago`;
    if (sec < 86400) return `${Math.floor(sec/3600)}h ago`;
    return `${Math.floor(sec/86400)}d ago`;
  }

  function statusPill(status) {
    if (!status) return '';
    const cls = 'sched-pill sched-pill-' + MC.esc(status);
    return `<span class="${cls}" data-testid="status-pill">${MC.esc(status)}</span>`;
  }

  // URL helper: same plugin, different page. Preserves query params (anchor).
  function pageUrl(plugin, pageID, params) {
    const base = `/p/${encodeURIComponent(plugin)}/${encodeURIComponent(pageID)}`;
    const search = params ? '?' + new URLSearchParams(params).toString() : '';
    return base + search;
  }

  function getAnchor() {
    const u = new URL(window.location.href);
    return u.searchParams.get('anchor') || '';
  }

  function setAnchor(anchor) {
    const u = new URL(window.location.href);
    if (anchor) u.searchParams.set('anchor', anchor);
    else u.searchParams.delete('anchor');
    window.location.href = u.toString();
  }

  // ---- shared header strip (tabs + anchor nav + New Job) ------------------

  function headerHTML(activeTab, titleText, plugin, anchorLabel, prevAnchor, nextAnchor) {
    const anchor = getAnchor();
    const tabs = [
      { id: 'scheduler_week',  label: MC.t('scheduler.tab.week',  'Week')  },
      { id: 'scheduler_month', label: MC.t('scheduler.tab.month', 'Month') },
      { id: 'scheduler_list',  label: MC.t('scheduler.tab.list',  'List')  },
    ];
    const tabsHTML = tabs.map(t => {
      const cls = 'sched-tab' + (t.id === activeTab ? ' sched-tab-active' : '');
      const href = pageUrl(plugin, t.id, anchor ? { anchor } : null);
      return `<a class="${cls}" href="${MC.esc(href)}" data-testid="sched-tab-${MC.esc(t.id)}">${MC.esc(t.label)}</a>`;
    }).join('');

    const navHTML = (prevAnchor || nextAnchor)
      ? `
        <button class="sched-tab" data-action="sched-prev" data-anchor="${MC.esc(prevAnchor || '')}" data-testid="sched-prev" title="${MC.esc(MC.t('scheduler.btn.prev', 'Previous'))}">&larr;</button>
        <span class="sched-anchor" data-testid="sched-anchor-label">${MC.esc(anchorLabel)}</span>
        <button class="sched-tab" data-action="sched-next" data-anchor="${MC.esc(nextAnchor || '')}" data-testid="sched-next" title="${MC.esc(MC.t('scheduler.btn.next', 'Next'))}">&rarr;</button>
        <button class="sched-tab" data-action="sched-today" data-testid="sched-today">${MC.esc(MC.t('scheduler.btn.today', 'Today'))}</button>
      `
      : '';

    return `
      <div class="sched-header" data-testid="sched-header">
        <div class="sched-title">${MC.esc(titleText)}</div>
        ${navHTML}
        <div class="sched-tabs">${tabsHTML}</div>
        <button class="button button-primary" data-action="sched-new" data-testid="sched-new-job">${MC.esc(MC.t('scheduler.btn.new_job', '+ New Job'))}</button>
      </div>
    `;
  }

  // ---- WEEK renderer ------------------------------------------------------

  function renderWeek(data, ctx) {
    const days = data.days || [];
    const plugin = ctx.plugin;

    // Column head row.
    const heads = ['<div class="sched-week-cell sched-week-head"></div>'];
    for (const day of days) {
      const cls = 'sched-week-cell sched-week-head' + (day.is_today ? ' sched-today' : '');
      heads.push(`<div class="${cls}" data-testid="sched-week-head-${MC.esc(day.date)}">${MC.esc(day.weekday)} ${MC.esc(day.date.slice(5))}</div>`);
    }

    // Body rows: full 24-hour day, 00..23.
    const rows = [];
    const visibleHours = [];
    for (let h = 0; h <= 23; h++) visibleHours.push(h);

    for (const h of visibleHours) {
      const row = [`<div class="sched-week-cell sched-hour-label">${h.toString().padStart(2,'0')}:00</div>`];
      for (const day of days) {
        const slot = (day.slots || []).find(s => s.hour === h) || { jobs: [] };
        const chips = (slot.jobs || []).map(j => {
          const cls = 'sched-chip sched-chip-' + MC.esc(j.status || 'scheduled');
          return `<span class="${cls}" data-action="sched-open" data-job-id="${j.id}" data-testid="sched-chip-${j.id}" title="${MC.esc(j.fire_at)} · ${MC.esc(j.agent_id)}">${MC.esc(j.name)}</span>`;
        }).join('');
        const empty = chips ? '' : ' sched-slot-empty';
        // Body cell is click-to-create: opens the New Job modal pre-filled
        // with a weekly cron at this slot's hour and day-of-week.
        const slotTip = MC.t('scheduler.slot.tooltip', 'Click to schedule a job at {hour}:00').replace('{hour}', h.toString().padStart(2,'0'));
        row.push(`<div class="sched-week-cell sched-week-slot${empty}" data-action="sched-slot-create" data-date="${MC.esc(day.date)}" data-hour="${h}" title="${MC.esc(slotTip)}">${chips}</div>`);
      }
      rows.push(row.join(''));
    }

    const anchor = data.anchor_today || '';
    const titleText = MC.t('scheduler.title.week', 'Week of {start} – {end}')
      .replace('{start}', data.week_start)
      .replace('{end}',   data.week_end);
    const anchorLabelText = `${MC.t('scheduler.label.anchor', 'Anchor')}: ${anchor}`;
    // prev/next anchors: start_of_week - 7 / + 7. Compute via Date() which
    // is fine in the browser (only WASM has the time problem).
    const weekStart = new Date(data.week_start + 'T00:00:00Z');
    const prev = new Date(weekStart.getTime() - 7*86400000).toISOString().slice(0,10);
    const next = new Date(weekStart.getTime() + 7*86400000).toISOString().slice(0,10);

    // Inline grid-template-rows so the body stretches: header row gets a
    // fixed compact height, every hour row gets an equal share of remaining
    // space (Outlook-style "fill the viewport" behavior).
    const gridStyle = `grid-template-rows: 2.25rem repeat(${visibleHours.length}, minmax(2.75rem, 1fr));`;

    return `
      <div class="sched-page">
        ${headerHTML('scheduler_week', titleText, plugin, anchorLabelText, prev, next)}
        <div class="sched-week-wrapper" data-testid="sched-week">
          <div class="sched-week" style="${gridStyle}">
            ${heads.join('')}
            ${rows.join('')}
          </div>
        </div>
      </div>
    `;
  }

  // ---- MONTH renderer -----------------------------------------------------

  function renderMonth(data, ctx) {
    const plugin = ctx.plugin;
    const weeks = data.weeks || [];

    const dowHead = [
      MC.t('scheduler.dow.mon', 'Mon'),
      MC.t('scheduler.dow.tue', 'Tue'),
      MC.t('scheduler.dow.wed', 'Wed'),
      MC.t('scheduler.dow.thu', 'Thu'),
      MC.t('scheduler.dow.fri', 'Fri'),
      MC.t('scheduler.dow.sat', 'Sat'),
      MC.t('scheduler.dow.sun', 'Sun'),
    ].map(d => `<div class="sched-month-head">${MC.esc(d)}</div>`).join('');

    const cells = [];
    for (const week of weeks) {
      for (const day of week) {
        const classes = ['sched-month-cell'];
        if (!day.in_month) classes.push('sched-out-of-month');
        if (day.is_today) classes.push('sched-today');
        const errDot = day.has_errors_today ? '<span class="sched-month-err-dot"></span>' : '';
        const jobsLabel = MC.t('scheduler.month.jobs', 'jobs');
        const countLabel = day.jobs_count > 0
          ? `<span class="sched-month-count">${day.jobs_count} ${MC.esc(jobsLabel)}${errDot}</span>`
          : '<span class="sched-month-count sched-slot-empty">–</span>';
        cells.push(`
          <div class="${classes.join(' ')}" data-action="sched-drill" data-anchor="${MC.esc(day.date)}" data-testid="sched-month-day-${MC.esc(day.date)}">
            <div class="sched-month-day-num">${day.day}</div>
            ${countLabel}
          </div>
        `);
      }
    }

    const monthStart = new Date(data.month_start + 'T00:00:00Z');
    const prev = new Date(monthStart.getUTCFullYear(), monthStart.getUTCMonth() - 1, 1)
      .toISOString().slice(0,10);
    const next = new Date(monthStart.getUTCFullYear(), monthStart.getUTCMonth() + 1, 1)
      .toISOString().slice(0,10);

    return `
      <div class="sched-page">
        ${headerHTML('scheduler_month', data.month_label || 'Month', plugin, data.month_label || '', prev, next)}
        <div class="sched-month-wrapper" data-testid="sched-month">
          <div class="sched-month">
            ${dowHead}
            ${cells.join('')}
          </div>
        </div>
      </div>
    `;
  }

  // ---- LIST renderer ------------------------------------------------------

  function renderList(data, ctx) {
    const jobs = data.jobs || [];
    const plugin = ctx.plugin;

    let body;
    if (jobs.length === 0) {
      body = `<tr><td colspan="7" class="sched-list-empty" data-testid="sched-list-empty">${MC.esc(MC.t('scheduler.list.empty', 'No jobs defined yet. Click + New Job above.'))}</td></tr>`;
    } else {
      const enabledOn  = MC.t('scheduler.list.enabled_on',  'on');
      const enabledOff = MC.t('scheduler.list.enabled_off', 'off');
      const editLbl    = MC.t('scheduler.list.btn.edit',    'Edit');
      const runLbl     = MC.t('scheduler.list.btn.run_now', 'Run now');
      const delLbl     = MC.t('scheduler.list.btn.delete',  'Delete');
      body = jobs.map(j => {
        const lastStatus = j.last_run_status ? statusPill(j.last_run_status) : '';
        const lastWhen = j.last_run_at ? fmtAgo(j.last_run_at) : '–';
        const next = j.next_fire_at ? fmtCompact(j.next_fire_at) : '–';
        const enabled = j.enabled
          ? `<span class="sched-pill sched-pill-success">${MC.esc(enabledOn)}</span>`
          : `<span class="sched-pill sched-pill-scheduled">${MC.esc(enabledOff)}</span>`;
        return `
          <tr data-testid="sched-row-${j.id}">
            <td><a href="#" data-action="sched-open" data-job-id="${j.id}">${MC.esc(j.name)}</a></td>
            <td><code>${MC.esc(j.cron_expr)}</code></td>
            <td>${MC.esc(next)}</td>
            <td>${lastStatus} <span class="sched-anchor">${MC.esc(lastWhen)}</span></td>
            <td>${MC.esc(j.assignee_agent_id || '–')}</td>
            <td>${enabled}</td>
            <td class="sched-list-actions">
              <button class="button button-ghost" data-action="sched-open" data-job-id="${j.id}" data-testid="sched-edit-${j.id}">${MC.esc(editLbl)}</button>
              <button class="button button-ghost" data-action="sched-trigger" data-job-id="${j.id}" data-job-name="${MC.esc(j.name)}" data-testid="sched-trigger-${j.id}">${MC.esc(runLbl)}</button>
              <button class="button button-danger" data-action="sched-delete" data-job-id="${j.id}" data-job-name="${MC.esc(j.name)}" data-testid="sched-delete-${j.id}">${MC.esc(delLbl)}</button>
            </td>
          </tr>
        `;
      }).join('');
    }

    const listTitle = MC.t('scheduler.title.list', 'All jobs ({n})')
      .replace('{n}', String(data.total || jobs.length));

    return `
      <div class="sched-page">
        ${headerHTML('scheduler_list', listTitle, plugin, '', '', '')}
        <div class="sched-list-wrapper">
          <table class="sched-list" data-testid="sched-list">
          <thead>
            <tr>
              <th>${MC.esc(MC.t('scheduler.list.col.name',     'Name'))}</th>
              <th>${MC.esc(MC.t('scheduler.list.col.cron',     'Cron'))}</th>
              <th>${MC.esc(MC.t('scheduler.list.col.next',     'Next'))}</th>
              <th>${MC.esc(MC.t('scheduler.list.col.last_run', 'Last run'))}</th>
              <th>${MC.esc(MC.t('scheduler.list.col.agent',    'Agent'))}</th>
              <th>${MC.esc(MC.t('scheduler.list.col.enabled',  'Enabled'))}</th>
              <th>${MC.esc(MC.t('scheduler.list.col.actions',  'Actions'))}</th>
            </tr>
          </thead>
            <tbody>${body}</tbody>
          </table>
        </div>
      </div>
    `;
  }

  // ---- post-render action binding -----------------------------------------

  function bindActions(container, ctx) {

    // Prev / Next / Today buttons: push a new anchor query param.
    container.querySelectorAll('[data-action="sched-prev"], [data-action="sched-next"]').forEach(btn => {
      btn.addEventListener('click', () => setAnchor(btn.dataset.anchor));
    });
    container.querySelectorAll('[data-action="sched-today"]').forEach(btn => {
      btn.addEventListener('click', () => setAnchor(''));
    });

    // Month cell click → week view anchored on that day.
    container.querySelectorAll('[data-action="sched-drill"]').forEach(cell => {
      cell.addEventListener('click', () => {
        const anchor = cell.dataset.anchor;
        window.location.href = pageUrl(ctx.plugin, 'scheduler_week', anchor ? { anchor } : null);
      });
    });

    // "+ New Job" button → open blank modal.
    container.querySelectorAll('[data-action="sched-new"]').forEach(btn => {
      btn.addEventListener('click', e => { e.preventDefault(); openJobModal(null, ctx); });
    });

    // Click on an empty week-grid time slot → open New Job modal pre-filled
    // with a DAILY cron at that hour (most common case for "schedule something
    // at 09:00"). User can edit the dow field afterwards if they want a
    // weekly schedule. Clicking a chip inside the cell falls through to the
    // chip's own handler instead.
    container.querySelectorAll('[data-action="sched-slot-create"]').forEach(cell => {
      cell.addEventListener('click', e => {
        if (e.target.closest('.sched-chip')) return; // chip handles its own click
        const hour = parseInt(cell.dataset.hour, 10);
        if (isNaN(hour)) return;
        const cron = `0 ${hour} * * *`; // daily at this hour
        openJobModal({ _preset: true, cron_expr: cron }, ctx);
      });
    });

    // Chip or row click → open detail modal.
    container.querySelectorAll('[data-action="sched-open"]').forEach(btn => {
      btn.addEventListener('click', e => {
        e.preventDefault();
        const id = Number(btn.dataset.jobId);
        if (id) openJobDetail(id, ctx);
      });
    });

    // Trigger Now button from list.
    container.querySelectorAll('[data-action="sched-trigger"]').forEach(btn => {
      btn.addEventListener('click', () => {
        const id = Number(btn.dataset.jobId);
        MC.callAction(ctx, 'trigger_job_now', { id })
          .then(res => {
            MC.showToast(`Triggered: run #${res.run_id || '?'}`);
            setTimeout(MC.reloadPage, 500);
          })
          .catch(err => MC.showToast(err.message || 'Trigger failed', 'error'));
      });
    });

    // Delete button from list.
    container.querySelectorAll('[data-action="sched-delete"]').forEach(btn => {
      btn.addEventListener('click', () => {
        const id = Number(btn.dataset.jobId);
        const name = btn.dataset.jobName || 'this job';
        MC.confirm({
          title: MC.t('scheduler.confirm.delete.title', 'Delete job'),
          message: MC.t('scheduler.confirm.delete.message', 'Delete "{name}" and all its run history? This cannot be undone.').replace('{name}', name),
          okText: MC.t('scheduler.confirm.delete.ok', 'Delete'),
          danger: true,
        }).then(ok => {
          if (!ok) return;
          MC.callAction(ctx, 'delete_job', { id })
            .then(() => { MC.showToast(MC.t('scheduler.toast.deleted', 'Job deleted')); MC.reloadPage(); })
            .catch(err => MC.showToast(err.message || MC.t('scheduler.toast.delete_failed', 'Delete failed'), 'error'));
        });
      });
    });
  }

  // ---- New / Edit job modal -----------------------------------------------

  function openJobModal(existing, ctx) {
    let dialog = document.getElementById('sched-job-dialog');
    if (dialog) dialog.remove();
    dialog = document.createElement('dialog');
    dialog.id = 'sched-job-dialog';
    dialog.className = 'mc-dialog';
    document.body.appendChild(dialog);

    const j = existing || {};
    // "Edit" mode only when we have a real persisted job (id present).
    // A preset object from click-to-create has no id and should still
    // produce a "New job" form, just pre-filled with the chosen cron.
    const isEdit = !!(existing && existing.id);

    const t = (k, fb) => MC.t(k, fb);
    dialog.innerHTML = `
      <form class="sched-form" data-testid="sched-form">
        <h3 style="font-size:22px;margin:0 0 0.5rem 0;">${MC.esc(isEdit ? t('scheduler.form.title.edit', 'Edit job') : t('scheduler.form.title.new', 'New job'))}</h3>

        <label>${MC.esc(t('scheduler.form.field.name', 'Name'))}
          <input type="text" name="name" required data-testid="sched-field-name" value="${MC.esc(j.name || '')}"/>
        </label>

        <label>${MC.esc(t('scheduler.form.field.cron', 'Cron expression (5 fields: min hour dom mon dow)'))}
          <input type="text" name="cron_expr" required data-testid="sched-field-cron"
            placeholder="0 9 * * *" value="${MC.esc(j.cron_expr || '')}"/>
        </label>

        <div class="sched-preview" data-testid="sched-preview">${MC.esc(t('scheduler.form.preview.placeholder', 'Enter a cron expression to preview the next 5 fires.'))}</div>

        <div class="sched-form-row">
          <label>${MC.esc(t('scheduler.form.field.timezone', 'Timezone'))}
            <input type="text" name="timezone" data-testid="sched-field-tz"
              placeholder="Europe/Berlin" value="${MC.esc(j.timezone || 'Europe/Berlin')}"/>
          </label>
          <label>${MC.esc(t('scheduler.form.field.agent', 'Agent'))}
            <select name="agent_id" required data-testid="sched-field-agent">
              <option value="">${MC.esc(t('scheduler.form.field.agent_loading', 'Loading…'))}</option>
            </select>
          </label>
        </div>

        <label>${MC.esc(t('scheduler.form.field.prompt', 'Prompt'))}
          <div
            data-editor
            data-field-name="prompt"
            data-required="true"
            data-preview-url="/api/preview-markdown"
            data-csrf-token="${MC.esc((ctx && ctx.csrf) || '')}"
            data-initial-value="${MC.esc(j.prompt || '')}"
            data-testid="sched-field-prompt"
          ></div>
        </label>

        <div class="sched-form-row">
          <label>${MC.esc(t('scheduler.form.field.task_id', 'Task ID (optional)'))}
            <input type="number" name="task_id" data-testid="sched-field-task" value="${j.task_id != null ? j.task_id : ''}"/>
          </label>
          <label style="flex-direction:row;align-items:center;gap:0.5rem;">
            <input type="checkbox" name="enabled" data-testid="sched-field-enabled" ${(!isEdit || j.enabled) ? 'checked' : ''}/>
            <span>${MC.esc(t('scheduler.form.field.enabled', 'Enabled'))}</span>
          </label>
        </div>

        <div class="sched-form-error hidden" data-testid="sched-form-error"></div>

        <div class="sched-form-actions">
          <button type="button" class="button button-ghost" data-sched-cancel data-testid="sched-form-cancel">${MC.esc(t('scheduler.form.btn.cancel', 'Cancel'))}</button>
          <button type="submit" class="button button-primary" data-testid="sched-form-save">${MC.esc(isEdit ? t('scheduler.form.btn.save', 'Save') : t('scheduler.form.btn.create', 'Create'))}</button>
        </div>
      </form>
    `;

    const form = dialog.querySelector('form');
    const preview = dialog.querySelector('[data-testid="sched-preview"]');
    const errBox = dialog.querySelector('[data-testid="sched-form-error"]');
    const cronInput = form.querySelector('[name="cron_expr"]');
    const tzInput = form.querySelector('[name="timezone"]');
    const agentSelect = form.querySelector('[name="agent_id"]');

    // Populate agent combobox from /admin/agents JSON. Falls back to a free-text
    // input if the endpoint is unavailable so the form remains usable. Selected
    // value preserves the existing job's agent (when editing) or "main" default.
    (function loadAgents() {
      const preferred = (j.assignee_agent_id || '').toString();
      fetch('/admin/agents', { headers: { 'Accept': 'application/json' }, credentials: 'same-origin' })
        .then(r => r.ok ? r.json() : Promise.reject(new Error('agents endpoint returned ' + r.status)))
        .then(data => {
          const agents = (data && data.agents) || [];
          if (agents.length === 0) {
            // No agents configured: degrade to free-text input.
            const wrap = agentSelect.parentNode;
            const input = document.createElement('input');
            input.type = 'text';
            input.name = 'agent_id';
            input.required = true;
            input.placeholder = 'main';
            input.value = preferred || 'main';
            input.setAttribute('data-testid', 'sched-field-agent');
            wrap.replaceChild(input, agentSelect);
            return;
          }
          agentSelect.innerHTML = '';
          // Use agent.name as the value: that's what jobs.agent_id stores.
          for (const a of agents) {
            const opt = document.createElement('option');
            opt.value = a.name;
            opt.textContent = a.name;
            if (a.name === preferred) opt.selected = true;
            agentSelect.appendChild(opt);
          }
          // If preferred wasn't found, leave first agent selected (no-op).
          if (!preferred && agentSelect.options.length > 0) agentSelect.options[0].selected = true;
        })
        .catch(err => {
          // Silent degradation, no scary toast.
          console.warn('[scheduler] could not load agents:', err);
          const wrap = agentSelect.parentNode;
          const input = document.createElement('input');
          input.type = 'text';
          input.name = 'agent_id';
          input.required = true;
          input.placeholder = 'main';
          input.value = preferred || 'main';
          input.setAttribute('data-testid', 'sched-field-agent');
          wrap.replaceChild(input, agentSelect);
        });
    })();

    function showFormError(msg) {
      if (!msg) {
        errBox.classList.add('hidden');
        errBox.textContent = '';
      } else {
        errBox.classList.remove('hidden');
        errBox.textContent = msg;
      }
    }

    // Live preview: debounce cron input and call the read-only validate_cron
    // MCP tool, which wraps the host mc_cron::validate host function and
    // returns { valid, error, next_fires[5] }. Authoritative, matches exactly
    // what the engine uses, so no client-side parser drift.
    let previewTimer = null;
    let previewSeq = 0;
    function updatePreview() {
      clearTimeout(previewTimer);
      previewTimer = setTimeout(() => {
        const expr = cronInput.value.trim();
        const tz = tzInput.value.trim();
        if (!expr) {
          preview.classList.remove('sched-preview-error');
          preview.textContent = MC.t('scheduler.form.preview.placeholder', 'Enter a cron expression to preview the next 5 fires.');
          return;
        }
        const mySeq = ++previewSeq;
        MC.callAction(ctx, 'validate_cron', { expression: expr, timezone: tz })
          .then(res => {
            if (mySeq !== previewSeq) return; // stale response, newer request in flight
            if (!res || res.valid === false) {
              preview.classList.add('sched-preview-error');
              preview.textContent = `${MC.t('scheduler.form.preview.error', 'Invalid cron expression')}: ${res && res.error ? res.error : ''}`.trim();
              return;
            }
            preview.classList.remove('sched-preview-error');
            const fires = res.next_fires || [];
            if (fires.length === 0) {
              preview.textContent = MC.t('scheduler.form.preview.no_fires', 'No upcoming fires in range.');
              return;
            }
            const items = fires.map(f => `<li>${MC.esc(fmtCompact(f))} <span style="opacity:0.5;">(${MC.esc(f)})</span></li>`).join('');
            preview.innerHTML = `<div>${MC.esc(MC.t('scheduler.form.preview.next_fires', 'Next 5 fires (UTC):'))}</div><ul>${items}</ul>`;
          })
          .catch(err => {
            if (mySeq !== previewSeq) return;
            preview.classList.add('sched-preview-error');
            preview.textContent = `${MC.t('scheduler.form.preview.error', 'Invalid cron expression')}: ${err.message || ''}`.trim();
          });
      }, 250);
    }
    cronInput.addEventListener('input', updatePreview);
    tzInput.addEventListener('input', updatePreview);
    if (cronInput.value) updatePreview();

    form.querySelectorAll('[data-sched-cancel]').forEach(btn => {
      btn.addEventListener('click', () => dialog.close());
    });

    form.addEventListener('submit', e => {
      e.preventDefault();
      showFormError('');

      const data = {
        name: form.name.value.trim(),
        cron_expr: form.cron_expr.value.trim(),
        prompt: form.prompt.value.trim(),
        agent_id: form.agent_id.value.trim(),
        timezone: form.timezone.value.trim() || 'Europe/Berlin',
        enabled: form.enabled.checked,
      };
      const taskRaw = form.task_id.value.trim();
      if (taskRaw) data.task_id = Number(taskRaw);

      if (!data.name || !data.cron_expr || !data.prompt || !data.agent_id) {
        showFormError('Name, cron, prompt and agent are required.');
        return;
      }

      const tool = isEdit ? 'update_job' : 'create_job';
      const payload = isEdit ? { id: existing.id, ...data } : data;

      MC.callAction(ctx, tool, payload)
        .then(() => { dialog.close(); MC.showToast(MC.t(isEdit ? 'scheduler.toast.updated' : 'scheduler.toast.created', isEdit ? 'Job updated' : 'Job created')); MC.reloadPage(); })
        .catch(err => showFormError(err.message || MC.t('scheduler.toast.save_failed', 'Save failed')));
    });

    dialog.showModal();

    // Mount the shared markdown editor on the prompt field. The editor builds
    // its own <textarea name="prompt"> inside the container, so form.prompt.value
    // keeps working. Must run after showModal() so the editor measures layout
    // against a visible dialog.
    const promptEditorEl = form.querySelector('[data-editor][data-field-name="prompt"]');
    if (promptEditorEl && typeof window.initEditor === 'function') {
      window.initEditor(promptEditorEl);
    }
  }

  // ---- Job detail modal ---------------------------------------------------

  function openJobDetail(jobID, ctx) {
    MC.callAction(ctx, 'get_job', { id: jobID })
      .then(res => renderDetail(res, ctx))
      .catch(err => MC.showToast(err.message || 'Load failed', 'error'));
  }

  function renderDetail(payload, ctx) {
    const job = payload.job || {};
    const runs = payload.recent_runs || [];

    let dialog = document.getElementById('sched-detail-dialog');
    if (dialog) dialog.remove();
    dialog = document.createElement('dialog');
    dialog.id = 'sched-detail-dialog';
    dialog.className = 'mc-dialog';
    document.body.appendChild(dialog);

    const t = (k, fb) => MC.t(k, fb);
    const yesLbl = t('scheduler.bool.yes', 'Yes');
    const noLbl  = t('scheduler.bool.no',  'No');
    const dispatchPill = (s) => {
      if (!s || s === 'pending') return '<span class="sched-pill" style="opacity:0.6;">pending</span>';
      if (s === 'dispatched') return '<span class="sched-pill" style="background:hsl(40 80% 35%);">dispatched</span>';
      if (s === 'delivered')  return '<span class="sched-pill" style="background:hsl(140 60% 35%);">delivered</span>';
      if (s === 'error')      return '<span class="sched-pill" style="background:hsl(0 60% 40%);">error</span>';
      return MC.esc(s);
    };
    const runsHTML = runs.length === 0
      ? `<tr><td colspan="6" class="sched-list-empty">${MC.esc(t('scheduler.detail.no_runs', 'No runs yet.'))}</td></tr>`
      : runs.map((r, i) => `
          <tr data-sched-run-row="${i}" style="cursor:${r.output ? 'pointer' : 'default'};">
            <td>${statusPill(r.status)}</td>
            <td>${dispatchPill(r.dispatch_status)}</td>
            <td>${MC.esc(fmtCompact(r.triggered_at))}</td>
            <td>${r.duration_ms != null ? r.duration_ms + ' ms' : '–'}</td>
            <td>${r.error ? MC.esc(r.error) : '–'}</td>
            <td>${r.output ? '▶' : '–'}</td>
          </tr>
          ${r.output ? `
            <tr data-sched-run-output="${i}" style="display:none;">
              <td colspan="6" style="padding:0.75rem 1rem;background:hsl(var(--card-muted, 220 10% 12%));">
                <div class="sched-detail-label">${MC.esc(t('scheduler.detail.run.output', 'Output'))}</div>
                <pre style="white-space:pre-wrap;word-break:break-word;font-size:11px;margin:0.25rem 0 0;max-height:320px;overflow:auto;">${MC.esc(r.output)}</pre>
              </td>
            </tr>` : ''}
        `).join('');

    dialog.innerHTML = `
      <div class="sched-detail" data-testid="sched-detail">
        <h3 style="font-size:22px;margin:0;">${MC.esc(job.name || '')}</h3>
        <div class="sched-detail-field"><div class="sched-detail-label">${MC.esc(t('scheduler.detail.cron', 'Cron'))}</div><div><code>${MC.esc(job.cron_expr || '')}</code> (${MC.esc(job.timezone || '')})</div></div>
        <div class="sched-detail-field"><div class="sched-detail-label">${MC.esc(t('scheduler.detail.agent', 'Agent'))}</div><div>${MC.esc(job.assignee_agent_id || '–')}</div></div>
        <div class="sched-detail-field"><div class="sched-detail-label">${MC.esc(t('scheduler.detail.next_fire', 'Next fire'))}</div><div>${MC.esc(fmtCompact(job.next_fire_at))}</div></div>
        <div class="sched-detail-field"><div class="sched-detail-label">${MC.esc(t('scheduler.detail.last_run', 'Last run'))}</div><div>${job.last_run_status ? statusPill(job.last_run_status) : '–'} ${job.last_run_at ? '· ' + MC.esc(fmtAgo(job.last_run_at)) : ''}</div></div>
        <div class="sched-detail-field"><div class="sched-detail-label">${MC.esc(t('scheduler.detail.enabled', 'Enabled'))}</div><div>${MC.esc(job.enabled ? yesLbl : noLbl)}</div></div>
        <div class="sched-detail-field"><div class="sched-detail-label">${MC.esc(t('scheduler.detail.prompt', 'Prompt'))}</div><div style="white-space:pre-wrap;">${MC.esc(job.prompt || '')}</div></div>

        <div class="sched-detail-label" style="margin-top:0.5rem;">${MC.esc(t('scheduler.detail.recent_runs', 'Recent runs'))}</div>
        <table class="sched-detail-runs" data-testid="sched-detail-runs">
          <thead><tr><th>${MC.esc(t('scheduler.detail.run.status', 'Status'))}</th><th>${MC.esc(t('scheduler.detail.run.dispatch', 'Dispatch'))}</th><th>${MC.esc(t('scheduler.detail.run.triggered', 'Triggered'))}</th><th>${MC.esc(t('scheduler.detail.run.duration', 'Duration'))}</th><th>${MC.esc(t('scheduler.detail.run.error', 'Error'))}</th><th>${MC.esc(t('scheduler.detail.run.output', 'Output'))}</th></tr></thead>
          <tbody>${runsHTML}</tbody>
        </table>

        <div class="sched-form-actions">
          <button class="button button-ghost" data-sched-close data-testid="sched-detail-close">${MC.esc(t('scheduler.detail.btn.close', 'Close'))}</button>
          <button class="button button-ghost" data-sched-edit data-testid="sched-detail-edit">${MC.esc(t('scheduler.list.btn.edit', 'Edit'))}</button>
          <button class="button button-ghost" data-sched-trigger data-testid="sched-detail-trigger">${MC.esc(t('scheduler.list.btn.run_now', 'Run now'))}</button>
          <button class="button button-danger" data-sched-delete data-testid="sched-detail-delete">${MC.esc(t('scheduler.list.btn.delete', 'Delete'))}</button>
        </div>
      </div>
    `;

    // Toggle output expansion on row click.
    dialog.querySelectorAll('[data-sched-run-row]').forEach(row => {
      row.addEventListener('click', () => {
        const idx = row.getAttribute('data-sched-run-row');
        const out = dialog.querySelector(`[data-sched-run-output="${idx}"]`);
        if (out) out.style.display = out.style.display === 'none' ? '' : 'none';
      });
    });

    dialog.querySelector('[data-sched-close]').addEventListener('click', () => dialog.close());
    dialog.querySelector('[data-sched-edit]').addEventListener('click', () => {
      dialog.close();
      openJobModal(job, ctx);
    });
    dialog.querySelector('[data-sched-trigger]').addEventListener('click', () => {
      MC.callAction(ctx, 'trigger_job_now', { id: job.id })
        .then(res => {
          const msg = MC.t('scheduler.toast.triggered', 'Triggered: run #{id}').replace('{id}', String(res.run_id || '?'));
          MC.showToast(msg); dialog.close(); setTimeout(MC.reloadPage, 500);
        })
        .catch(err => MC.showToast(err.message || MC.t('scheduler.toast.trigger_failed', 'Trigger failed'), 'error'));
    });
    dialog.querySelector('[data-sched-delete]').addEventListener('click', () => {
      MC.confirm({
        title: MC.t('scheduler.confirm.delete.title', 'Delete job'),
        message: MC.t('scheduler.confirm.delete.message', 'Delete "{name}" and all its run history? This cannot be undone.').replace('{name}', job.name || ''),
        okText: MC.t('scheduler.confirm.delete.ok', 'Delete'),
        danger: true,
      }).then(ok => {
        if (!ok) return;
        MC.callAction(ctx, 'delete_job', { id: job.id })
          .then(() => { MC.showToast(MC.t('scheduler.toast.deleted', 'Job deleted')); dialog.close(); MC.reloadPage(); })
          .catch(err => MC.showToast(err.message || MC.t('scheduler.toast.delete_failed', 'Delete failed'), 'error'));
      });
    });

    dialog.showModal();
  }

  // ---- register with MC ---------------------------------------------------

  function wrapRenderer(fn) {
    return function (data, ctx) {
      // main.js assigns el.innerHTML = renderer(data, ctx), then calls
      // bindPageActions which only handles its own generic actions. We run
      // our bindActions via a micro-task so the HTML is already in the DOM.
      const html = fn(data, ctx);
      setTimeout(() => bindActions(ctx.el, ctx), 0);
      return html;
    };
  }

  MC.registerRenderer('scheduler_week',  wrapRenderer(renderWeek));
  MC.registerRenderer('scheduler_month', wrapRenderer(renderMonth));
  MC.registerRenderer('scheduler_list',  wrapRenderer(renderList));
})();
