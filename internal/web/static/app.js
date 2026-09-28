// Instant-save controls for the Today screen, and small form helpers.
(() => {
  const fmtDuration = (minutes) => {
    const m = Math.round(minutes), h = Math.floor(m / 60), r = m % 60;
    if (!h) return `${r}m`;
    return r ? `${h}h ${r}m` : `${h}h`;
  };

  async function send(method, url, body) {
    const res = await fetch(url, {
      method,
      headers: { 'Content-Type': 'application/json' },
      body: body && JSON.stringify(body),
    });
    if (!res.ok) throw new Error(await res.text());
    return res.status === 204 ? null : res.json();
  }

  // ---- Habits -------------------------------------------------------------
  const list = document.querySelector('.habits[data-date]');
  if (list) {
    const date = list.dataset.date;
    const timers = new Map();

    const read = (row) => {
      let value = 0, breakdown = null;
      if (row.dataset.kind === 'binary' || row.dataset.kind === 'linked') {
        value = Number(row.dataset.value) || 0;
      } else {
        const parts = row.querySelectorAll('.bd-value');
        if (parts.length) {
          breakdown = {};
          parts.forEach((i) => {
            const n = Math.max(0, Number(i.value) || 0);
            breakdown[i.dataset.key] = n;
            value += n;
          });
        } else {
          value = Math.max(0, Number(row.querySelector('.value').value) || 0);
        }
      }
      const note = row.querySelector('.note-input').value;
      return { habit_id: Number(row.dataset.id), date, value, breakdown, note };
    };

    // Update the row immediately; the server response confirms it.
    const paint = (row, value) => {
      const target = Number(row.dataset.target) || 1;
      const p = Math.min(1, Math.max(0, value / target));
      row.querySelector('.bar > span').style.width = `${Math.round(p * 100)}%`;
      row.classList.toggle('done', p >= 1);
      const hint = row.querySelector('.hint');
      if (hint) hint.textContent = fmtDuration(value);
      const sum = row.querySelector('.sum');
      if (sum) sum.textContent = value;
      const toggle = row.querySelector('.toggle');
      if (toggle) {
        toggle.dataset.state = value >= 1 ? 'done' : 'miss';
        toggle.textContent = value >= 1 ? '✓' : '✗';
        toggle.setAttribute('aria-pressed', value >= 1);
      }
    };

    // Apply the server's view of a habit after a change.
    const applyState = (row, d) => {
      row.classList.remove('error');
      row.classList.toggle('done', d.done);
      row.querySelector('.bar > span').style.width = `${Math.round(d.progress * 100)}%`;
      row.querySelector('.streak').textContent = d.streak;
      row.querySelector('.best').textContent = d.best;
      document.getElementById('score-pct').textContent = `${d.score.percent}%`;
      document.getElementById('score-done').textContent = d.score.completed;
      document.getElementById('score-total').textContent = d.score.total;
      document.getElementById('score-bar').style.width = `${d.score.percent}%`;
      if (d.outreach && row.dataset.kind === 'linked') {
        const o = d.outreach;
        row.dataset.value = d.value;
        row.querySelector('.sum').textContent = d.value;
        row.querySelector('.outreach-breakdown').firstChild.textContent =
          `${o.Emails} emails · ${o.LinkedIn} LinkedIn · ${o.Other} other · ${o.Applications} applications · `;
      }
    };

    const save = async (row) => {
      row.classList.add('saving');
      try {
        const d = await send('POST', '/api/entries', read(row));
        applyState(row, d);
      } catch (err) {
        row.classList.add('error');
        console.error('save failed', err);
      } finally {
        row.classList.remove('saving');
      }
    };

    const changed = (row) => {
      paint(row, read(row).value);
      clearTimeout(timers.get(row));
      timers.set(row, setTimeout(() => { timers.delete(row); save(row); }, 350));
    };

    list.addEventListener('click', async (e) => {
      const row = e.target.closest('.habit');
      if (!row) return;
      const quick = e.target.closest('[data-outreach]');
      if (quick) {
        const company = row.querySelector('.qo-company');
        quick.disabled = true;
        try {
          const d = await send('POST', '/api/outreach', {
            habit_id: Number(row.dataset.id), date, type: quick.dataset.outreach, company: company.value.trim(),
          });
          applyState(row, d);
          company.value = '';
        } catch (err) {
          row.classList.add('error');
          console.error('log outreach failed', err);
        } finally {
          quick.disabled = false;
        }
        return;
      }
      if (e.target.closest('.toggle')) {
        row.dataset.value = Number(row.dataset.value) >= 1 ? 0 : 1;
        changed(row);
        return;
      }
      const btn = e.target.closest('[data-delta]');
      if (btn) {
        const input = btn.parentElement.querySelector('input');
        const step = input.classList.contains('bd-value') ? 1 : Number(row.dataset.step) || 1;
        input.value = Math.max(0, (Number(input.value) || 0) + Number(btn.dataset.delta) * step);
        changed(row);
      }
    });
    list.addEventListener('input', (e) => {
      if (e.target.matches('.value, .bd-value')) changed(e.target.closest('.habit'));
    });
    list.addEventListener('change', (e) => {
      if (e.target.matches('.note-input')) changed(e.target.closest('.habit'));
    });
    // Save pending edits if the page is closed mid-debounce.
    window.addEventListener('pagehide', () => {
      timers.forEach((t, row) => {
        clearTimeout(t);
        navigator.sendBeacon('/api/entries', new Blob([JSON.stringify(read(row))], { type: 'application/json' }));
      });
      timers.clear();
    });
  }

  // ---- Tasks --------------------------------------------------------------
  const tasks = document.querySelector('.tasks[data-date]');
  if (tasks) {
    const date = tasks.dataset.date;
    const ul = tasks.querySelector('#task-list');
    const form = tasks.querySelector('#task-form');

    const recount = () => {
      const all = ul.querySelectorAll('.task').length;
      const done = ul.querySelectorAll('.task.done').length;
      tasks.querySelector('#tasks-count').textContent = `${done}/${all}`;
    };

    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      const input = form.elements.title;
      const title = input.value.trim();
      if (!title) return;
      try {
        const t = await send('POST', '/api/tasks', { date, title });
        const li = document.createElement('li');
        li.className = 'task';
        li.dataset.id = t.id;
        li.innerHTML = '<label><input type="checkbox"> <span></span></label><button type="button" class="delete" aria-label="Delete task">×</button>';
        li.querySelector('span').textContent = t.title;
        ul.append(li);
        input.value = '';
        recount();
      } catch (err) {
        console.error('add task failed', err);
      }
    });

    ul.addEventListener('change', async (e) => {
      const li = e.target.closest('.task');
      if (!li || e.target.type !== 'checkbox') return;
      li.classList.toggle('done', e.target.checked);
      recount();
      try {
        await send('POST', `/api/tasks/${li.dataset.id}`, { done: e.target.checked, date });
      } catch (err) {
        e.target.checked = !e.target.checked;
        li.classList.toggle('done', e.target.checked);
        recount();
        console.error('update task failed', err);
      }
    });

    ul.addEventListener('click', async (e) => {
      const btn = e.target.closest('.delete');
      if (!btn) return;
      const li = btn.closest('.task');
      try {
        await send('DELETE', `/api/tasks/${li.dataset.id}`);
        li.remove();
        recount();
      } catch (err) {
        console.error('delete task failed', err);
      }
    });
  }

  // ---- Layout: mobile menu -------------------------------------------------
  const menuBtn = document.getElementById('menu-btn');
  if (menuBtn) {
    const setOpen = (open) => {
      document.body.classList.toggle('menu-open', open);
      menuBtn.setAttribute('aria-expanded', open);
    };
    menuBtn.addEventListener('click', () => setOpen(!document.body.classList.contains('menu-open')));
    document.getElementById('scrim').addEventListener('click', () => setOpen(false));
    document.addEventListener('keydown', (e) => { if (e.key === 'Escape') setOpen(false); });
  }

  // ---- Filters and inline status menus submit on change --------------------
  document.addEventListener('change', (e) => {
    if (e.target.classList.contains('auto-submit') && e.target.form) e.target.form.requestSubmit();
  });

  // ---- Filters fold away on phones ------------------------------------------
  document.addEventListener('click', (e) => {
    const t = e.target.closest('.filter-toggle');
    if (t) t.form.classList.toggle('open');
  });

  // ---- Destructive forms ask first ----------------------------------------
  document.addEventListener('submit', (e) => {
    const msg = e.target.dataset && e.target.dataset.confirm;
    if (msg && !window.confirm(msg)) e.preventDefault();
  });

  // ---- Application form: new-role fields only when no existing role is picked
  const oppSelect = document.getElementById('opportunity-select');
  const newRole = document.getElementById('new-role');
  if (oppSelect && newRole) {
    const sync = () => {
      const picking = oppSelect.value !== '0';
      newRole.hidden = picking;
      newRole.nextElementSibling.hidden = picking;
    };
    oppSelect.addEventListener('change', sync);
    sync();
  }

  // ---- Habit form: emoji and day shortcuts -------------------------------
  document.addEventListener('click', (e) => {
    const pick = e.target.closest('[data-emoji]');
    if (pick) document.getElementById('emoji-input').value = pick.dataset.emoji;
    const preset = e.target.closest('[data-days]');
    if (preset) {
      const on = preset.dataset.days.split(',');
      preset.closest('fieldset').querySelectorAll('input[name="days"]').forEach((c) => { c.checked = on.includes(c.value); });
    }
  });

  // ---- Welcome: the main-skill field only matters for job search -----------
  const careerToggle = document.getElementById('career-toggle');
  if (careerToggle) {
    const sync = () => {
      document.getElementById('skill-field').hidden = !careerToggle.checked;
      const job = document.querySelector('.template-group[data-group="Job search"]');
      if (job) job.hidden = !careerToggle.checked;
    };
    careerToggle.addEventListener('change', sync);
    sync();
  }

  // ---- Habit form: show only the fields that apply to the chosen type -----
  const kind = document.getElementById('kind');
  if (kind) {
    const sync = () => {
      document.querySelectorAll('.measurable').forEach((el) => { el.hidden = kind.value === 'binary'; });
      document.querySelectorAll('.count-only').forEach((el) => { el.hidden = kind.value !== 'count'; });
    };
    kind.addEventListener('change', sync);
    sync();
  }
})();
